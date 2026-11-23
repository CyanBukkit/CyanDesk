package remote

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"cyandesk/internal/e2ee"
	"cyandesk/internal/gateway"
	"cyandesk/internal/link"
	"cyandesk/internal/protocol"
	"github.com/flynn/noise"
)

const (
	msgProbe  byte = 1
	msgAck    byte = 2
	msgSelect byte = 14
)

// Side is one authenticated peer.
type Side struct {
	Gateway    string
	Role       byte
	Ticket     string
	Static     noise.DHKey
	PeerStatic []byte
	Prologue   []byte
}

// Result is the path that finished an authenticated probe first.
type Result struct {
	Selected string
	Paths    []link.Path
	send     func([]byte) error
	recv     func() ([]byte, error)
	close    func()
	current  func() string
}

func (r *Result) Current() string {
	if r != nil && r.current != nil {
		if name := r.current(); name != "" {
			return name
		}
	}
	if r == nil {
		return ""
	}
	return r.Selected
}

func (r *Result) Send(app byte, payload []byte) error {
	buf := make([]byte, 1+len(payload))
	buf[0] = app
	copy(buf[1:], payload)
	return r.send(buf)
}

func (r *Result) Recv() (byte, []byte, error) {
	buf, err := r.recv()
	if err != nil {
		return 0, nil, err
	}
	if len(buf) == 0 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	return buf[0], buf[1:], nil
}

func (r *Result) Close() {
	if r.close != nil {
		r.close()
	}
}

type pathConn struct {
	name    string
	sess    *e2ee.Session
	closer  io.Closer
	started time.Time
	tcpAt   time.Time
	ready   time.Time
	rtt     time.Duration
}

// Open starts relay and direct TCP together. The controller probes and picks
// the earliest usable path. The host only answers probes and follows that choice.
func Open(ctx context.Context, side Side) (*Result, error) {
	started := time.Now()
	client, err := gateway.Dial(side.Gateway, side.Role, side.Ticket)
	if err != nil {
		return nil, err
	}
	relayTCP := time.Now()
	if err := client.WaitReady(ctx); err != nil {
		client.Close()
		return nil, err
	}
	if side.Role == protocol.RoleController {
		return openController(ctx, side, client, started, relayTCP)
	}
	return openHost(ctx, side, client, started, relayTCP)
}

func openController(ctx context.Context, side Side, client *gateway.Client, started, relayTCP time.Time) (*Result, error) {
	found := make(chan pathConn, 2)
	go func() {
		sess, err := e2ee.Handshake(client, true, side.Static, side.PeerStatic, side.Prologue)
		if err != nil {
			return
		}
		rtt, err := sendProbe(sess)
		if err != nil {
			return
		}
		found <- pathConn{name: "relay", sess: sess, closer: client, started: started, tcpAt: relayTCP, ready: time.Now(), rtt: rtt}
	}()
	go func() {
		dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		sig, err := client.ReadSignal(dctx)
		if err != nil {
			return
		}
		conn, err := net.DialTimeout("tcp", string(sig), 2*time.Second)
		if err != nil {
			return
		}
		tcpAt := time.Now()
		sess, err := e2ee.Handshake(&streamPipe{conn: conn}, true, side.Static, side.PeerStatic, side.Prologue)
		if err != nil {
			conn.Close()
			return
		}
		rtt, err := sendProbe(sess)
		if err != nil {
			conn.Close()
			return
		}
		found <- pathConn{name: "direct", sess: sess, closer: conn, started: started, tcpAt: tcpAt, ready: time.Now(), rtt: rtt}
	}()
	var ready []pathConn
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for len(ready) < 2 {
		select {
		case p := <-found:
			ready = append(ready, p)
			if len(ready) == 1 {
				// Give the slower candidate a short chance to finish its probe.
				time.Sleep(200 * time.Millisecond)
			}
		case <-timeout.C:
			if len(ready) == 0 {
				client.Close()
				return nil, errors.New("remote: no usable path")
			}
			goto choose
		case <-ctx.Done():
			client.Close()
			return nil, ctx.Err()
		}
	}
choose:
	recorded := make([]link.Path, len(ready))
	for i, p := range ready {
		recorded[i] = link.Path{Name: p.name, Started: p.started, TCPReady: p.tcpAt, Ready: true, ReadyAt: p.ready, RTT: p.rtt}
	}
	st := link.Step(time.Now(), link.State{}, recorded, link.ProductionConfig())
	chosen := ready[0]
	for _, p := range ready {
		if p.name == st.Selected {
			chosen = p
		}
	}
	if err := chosen.sess.Send(nil, []byte{msgSelect}); err != nil {
		return nil, err
	}
	for _, p := range ready {
		if p.name != chosen.name {
			_ = p.closer.Close()
		}
	}
	return bind(chosen, recorded, st.Selected), nil
}

func openHost(ctx context.Context, side Side, client *gateway.Client, started, relayTCP time.Time) (*Result, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		client.Close()
		return nil, err
	}
	defer ln.Close()
	if err := client.WriteSignal([]byte(ln.Addr().String())); err != nil {
		client.Close()
		return nil, err
	}
	chosen := make(chan pathConn, 2)
	go func() {
		sess, err := e2ee.Handshake(client, false, side.Static, nil, side.Prologue)
		if err != nil {
			return
		}
		if err := answerUntilSelect(ctx, sess); err != nil {
			return
		}
		chosen <- pathConn{name: "relay", sess: sess, closer: client, started: started, tcpAt: relayTCP, ready: time.Now()}
	}()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		tcpAt := time.Now()
		sess, err := e2ee.Handshake(&streamPipe{conn: conn}, false, side.Static, nil, side.Prologue)
		if err != nil {
			conn.Close()
			return
		}
		if err := answerUntilSelect(ctx, sess); err != nil {
			conn.Close()
			return
		}
		chosen <- pathConn{name: "direct", sess: sess, closer: conn, started: started, tcpAt: tcpAt, ready: time.Now()}
	}()
	select {
	case p := <-chosen:
		return bind(p, []link.Path{{Name: p.name, Started: p.started, TCPReady: p.tcpAt, Ready: true, ReadyAt: p.ready, RTT: time.Millisecond}}, p.name), nil
	case <-ctx.Done():
		client.Close()
		return nil, ctx.Err()
	}
}

func bind(p pathConn, recorded []link.Path, name string) *Result {
	var mu sync.Mutex
	return &Result{
		Selected: name,
		Paths:    recorded,
		send: func(b []byte) error {
			mu.Lock()
			defer mu.Unlock()
			return p.sess.Send(nil, b)
		},
		recv:  func() ([]byte, error) { return p.sess.Recv(nil) },
		close: func() { _ = p.closer.Close() },
	}
}

func sendProbe(sess *e2ee.Session) (time.Duration, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return 0, err
	}
	start := time.Now()
	if err := sess.Send(nil, append([]byte{msgProbe}, nonce[:]...)); err != nil {
		return 0, err
	}
	buf, err := sess.Recv(nil)
	if err != nil {
		return 0, err
	}
	if len(buf) != 9 || buf[0] != msgAck || string(buf[1:]) != string(nonce[:]) {
		return 0, errors.New("remote: bad probe ack")
	}
	return time.Since(start), nil
}

func answerUntilSelect(ctx context.Context, sess *e2ee.Session) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		buf, err := sess.Recv(nil)
		if err != nil {
			return err
		}
		if len(buf) == 0 {
			continue
		}
		switch buf[0] {
		case msgProbe:
			if err := sess.Send(nil, append([]byte{msgAck}, buf[1:]...)); err != nil {
				return err
			}
		case msgSelect:
			return nil
		default:
			return errors.New("remote: unexpected setup message")
		}
	}
}

type streamPipe struct {
	conn net.Conn
	mu   sync.Mutex
}

func (p *streamPipe) WriteMsg(b []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := p.conn.Write(hdr[:]); err != nil {
		return err
	}
	_, err := p.conn.Write(b)
	return err
}

func (p *streamPipe) ReadMsg() ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(p.conn, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > 8<<20 {
		return nil, errors.New("remote: frame too large")
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(p.conn, buf)
	return buf, err
}

func (p *streamPipe) Close() error { return p.conn.Close() }
