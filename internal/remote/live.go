package remote

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"cyandesk/internal/e2ee"
	"cyandesk/internal/gateway"
	"cyandesk/internal/link"
	"cyandesk/internal/protocol"
)

// Direct tells OpenLive which address to bind and which address to tell the peer.
// Advertise is the address the peer can dial. It defaults to the bound socket.
type Direct struct {
	Bind      string
	Advertise string
}

// OpenLive keeps the authenticated relay and, when it comes up, a direct UDP
// path. Both already finished a Noise_IK handshake with the pinned static key.
// Probes after that only choose a path. They do not accept a new peer.
func OpenLive(ctx context.Context, side Side, direct Direct) (*Result, error) {
	client, err := gateway.Dial(side.Gateway, side.Role, side.Ticket)
	if err != nil {
		return nil, err
	}
	if err := client.WaitReady(ctx); err != nil {
		client.Close()
		return nil, err
	}
	relay, err := e2ee.Handshake(client, side.Role == protocol.RoleController, side.Static, side.PeerStatic, side.Prologue)
	if err != nil {
		client.Close()
		return nil, err
	}
	m := newMux(side.Role)
	m.add(lane{
		name: "relay",
		send: func(b []byte) error { return relay.Send(nil, b) },
		recv: func() ([]byte, error) { return relay.Recv(nil) },
		stop: client.Close,
	})
	go func() {
		if err := m.attachDirect(ctx, side, client, direct); err != nil {
			fmt.Fprintf(os.Stderr, "direct: %v\n", err)
		}
	}()
	if side.Role == protocol.RoleController {
		go m.steer(ctx)
	}
	return &Result{
		Selected: "relay",
		Paths:    []link.Path{{Name: "relay", Started: time.Now(), Ready: true, ReadyAt: time.Now(), RTT: time.Millisecond}},
		send:     m.send,
		recv:     m.recv,
		close:    m.close,
		current:  m.currentName,
	}, nil
}

func (m *mux) currentName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

type lane struct {
	name string
	send func([]byte) error
	recv func() ([]byte, error)
	stop func() error
	mu   sync.Mutex
}

func (l *lane) Send(b []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.send(b)
}

type mux struct {
	role   byte
	mu     sync.Mutex
	lanes  map[string]*lane
	order  []string
	active string
	state  link.State
	inbox  chan []byte
	wait   map[string]chan struct{}
	closed chan struct{}
	once   sync.Once
}

func newMux(role byte) *mux {
	return &mux{
		role:   role,
		lanes:  map[string]*lane{},
		inbox:  make(chan []byte, 32),
		wait:   map[string]chan struct{}{},
		closed: make(chan struct{}),
	}
}

func (m *mux) add(l lane) {
	m.mu.Lock()
	m.lanes[l.name] = &l
	m.order = append(m.order, l.name)
	if m.active == "" {
		m.active = l.name
		fmt.Printf("path %s\n", l.name)
	}
	m.mu.Unlock()
	go m.readLane(&l)
}

func (m *mux) readLane(l *lane) {
	for {
		buf, err := l.recv()
		if err != nil {
			return
		}
		if len(buf) == 0 {
			continue
		}
		switch buf[0] {
		case protocol.AppProbe:
			_ = l.Send(append([]byte{protocol.AppProbeAck}, buf[1:]...))
		case protocol.AppProbeAck:
			if len(buf) == 9 {
				m.mu.Lock()
				ch := m.wait[string(buf[1:])]
				m.mu.Unlock()
				if ch != nil {
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		case protocol.AppPath:
			if m.role == protocol.RoleHost {
				name := string(buf[1:])
				m.mu.Lock()
				if _, ok := m.lanes[name]; ok && name != m.active {
					m.active = name
					fmt.Printf("path %s\n", name)
				}
				m.mu.Unlock()
			}
		default:
			m.deliver(buf)
		}
	}
}

func (m *mux) deliver(buf []byte) {
	select {
	case m.inbox <- append([]byte(nil), buf...):
		return
	default:
	}
	if len(buf) > 0 && buf[0] == protocol.AppVideo {
		return
	}
	select {
	case m.inbox <- append([]byte(nil), buf...):
	default:
	}
}

func (m *mux) send(b []byte) error {
	m.mu.Lock()
	l := m.lanes[m.active]
	if l == nil {
		for _, item := range m.lanes {
			l = item
			break
		}
	}
	m.mu.Unlock()
	if l == nil {
		return errNoPath
	}
	return l.Send(b)
}

func (m *mux) recv() ([]byte, error) {
	select {
	case b := <-m.inbox:
		return b, nil
	case <-m.closed:
		return nil, errClosed
	}
}

func (m *mux) close() {
	m.once.Do(func() {
		close(m.closed)
		m.mu.Lock()
		lanes := m.lanes
		m.mu.Unlock()
		for _, l := range lanes {
			if l.stop != nil {
				_ = l.stop()
			}
		}
	})
}

func (m *mux) steer(ctx context.Context) {
	cfg := link.ProductionConfig()
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.closed:
			return
		case now := <-tick.C:
			m.observe(now, cfg)
		}
	}
}

func (m *mux) observe(now time.Time, cfg link.Config) {
	m.mu.Lock()
	names := append([]string(nil), m.order...)
	m.mu.Unlock()
	observed := make([]link.Path, 0, len(names))
	for _, name := range names {
		rtt := m.probe(name)
		failed := rtt <= 0
		observed = append(observed, link.Path{
			Name: name, Started: now.Add(-time.Second), Ready: !failed, ReadyAt: now, RTT: rtt, Failed: failed,
		})
	}
	m.mu.Lock()
	next := link.Step(now, m.state, observed, cfg)
	changed := next.Selected != "" && next.Selected != m.active
	if changed {
		m.active = next.Selected
	}
	m.state = next
	name := m.active
	m.mu.Unlock()
	if changed {
		fmt.Printf("path %s\n", name)
		m.mu.Lock()
		lanes := make([]*lane, 0, len(m.lanes))
		for _, l := range m.lanes {
			lanes = append(lanes, l)
		}
		m.mu.Unlock()
		body := append([]byte{protocol.AppPath}, name...)
		for _, l := range lanes {
			_ = l.Send(body)
		}
	}
}

func (m *mux) probe(name string) time.Duration {
	m.mu.Lock()
	l := m.lanes[name]
	m.mu.Unlock()
	if l == nil {
		return 0
	}
	var nonce [8]byte
	if _, err := randRead(nonce[:]); err != nil {
		return 0
	}
	ch := make(chan struct{}, 1)
	m.mu.Lock()
	m.wait[string(nonce[:])] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.wait, string(nonce[:]))
		m.mu.Unlock()
	}()
	if err := l.Send(append([]byte{protocol.AppProbe}, nonce[:]...)); err != nil {
		return 0
	}
	start := time.Now()
	select {
	case <-ch:
		return time.Since(start)
	case <-time.After(350 * time.Millisecond):
		return 0
	}
}

func (m *mux) attachDirect(ctx context.Context, side Side, client *gateway.Client, direct Direct) error {
	d, err := dialDirect(ctx, side, client, direct)
	if err != nil {
		return err
	}
	m.add(lane{
		name: "direct-udp",
		send: d.Send,
		recv: d.Recv,
		stop: d.Close,
	})
	if side.Role == protocol.RoleHost {
		fmt.Printf("direct-listen %s\n", d.local)
	}
	return nil
}

type directPath struct {
	local string
	d     *e2ee.Datagram
	conn  *net.UDPConn
}

func (d *directPath) Send(b []byte) error   { return d.d.Send(b) }
func (d *directPath) Recv() ([]byte, error) { return d.d.Recv() }
func (d *directPath) Close() error {
	_ = d.d.Close()
	return d.conn.Close()
}

func dialDirect(ctx context.Context, side Side, client *gateway.Client, direct Direct) (*directPath, error) {
	bind := direct.Bind
	if bind == "" {
		bind = "0.0.0.0:0"
	} else if _, _, err := net.SplitHostPort(bind); err != nil {
		bind = net.JoinHostPort(bind, "0")
	}
	addr, err := net.ResolveUDPAddr("udp4", bind)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}
	pipe := &udpPipe{conn: conn, attempt: e2ee.HandshakeAttempt}
	initiator := side.Role == protocol.RoleController
	if initiator {
		sig, err := client.ReadSignal(ctx)
		if err != nil || len(sig) < 5 || string(sig[:4]) != "udp " {
			conn.Close()
			if err == nil {
				err = errNoPath
			}
			return nil, err
		}
		peer, err := net.ResolveUDPAddr("udp4", string(sig[4:]))
		if err != nil {
			conn.Close()
			return nil, err
		}
		pipe.peer = peer
	} else {
		local := conn.LocalAddr().String()
		if h, p, err := net.SplitHostPort(local); err == nil && (h == "0.0.0.0" || h == "::" || h == "") {
			local = net.JoinHostPort("127.0.0.1", p)
		}
		adv := direct.Advertise
		if adv == "" {
			adv = local
		}
		if adv != conn.LocalAddr().String() {
			if peer, err := net.ResolveUDPAddr("udp4", adv); err == nil {
				_, _ = conn.WriteToUDP([]byte{0}, peer)
			}
		}
		if err := client.WriteSignal([]byte("udp " + adv)); err != nil {
			conn.Close()
			return nil, err
		}
	}
	dg, err := e2ee.HandshakeUDP(pipe, initiator, side.Static, side.PeerStatic, side.Prologue)
	pipe.attempt = 0
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &directPath{local: conn.LocalAddr().String(), d: dg, conn: conn}, nil
}

type udpPipe struct {
	conn    *net.UDPConn
	peer    *net.UDPAddr
	attempt time.Duration
	mu      sync.Mutex
}

func (u *udpPipe) WriteMsg(b []byte) error {
	u.mu.Lock()
	peer := u.peer
	u.mu.Unlock()
	if peer == nil {
		return errNoPath
	}
	_, err := u.conn.WriteToUDP(b, peer)
	return err
}

func (u *udpPipe) ReadMsg() ([]byte, error) {
	buf := make([]byte, 65535)
	if u.attempt > 0 {
		_ = u.conn.SetReadDeadline(time.Now().Add(u.attempt))
	}
	n, addr, err := u.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	if u.peer == nil {
		u.peer = addr
	}
	u.mu.Unlock()
	return append([]byte(nil), buf[:n]...), nil
}

func (u *udpPipe) Close() error { return u.conn.Close() }

func randRead(b []byte) (int, error) {
	return rand.Read(b)
}

var errNoPath = errText("remote: no path")
var errClosed = errText("remote: closed")

type errText string

func (e errText) Error() string { return string(e) }
