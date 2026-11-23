package desktop

import (
	"context"
	"crypto/rand"
	"sync"
	"time"

	"cyandesk/internal/input"
	"cyandesk/internal/link"
	"cyandesk/internal/protocol"
)

// Endpoint is one already-authenticated encrypted transport.
// Sends and receives are application messages, not handshake bytes.
type Endpoint struct {
	Name       string
	Send       func(app byte, payload []byte) error
	Recv       func() (byte, []byte, error)
	InitialRTT time.Duration
}

type videoNote struct {
	Path string
	Seq  uint32
}

// Live keeps every authenticated path open and moves media and input between
// them. Input sequence state stays on this object across a move.
type Live struct {
	mu       sync.Mutex
	paths    []*Endpoint
	byName   map[string]*Endpoint
	active   string
	state    link.State
	cfg      link.Config
	order    *input.Ordered
	wait     map[string]chan struct{}
	videos   []videoNote
	switches []string
	sink     input.Sink
	rejected *int
	granted  bool
	upgrade  bool
}

func NewLive(paths []Endpoint, cfg link.Config, allowInput bool) *Live {
	l := &Live{
		byName:  map[string]*Endpoint{},
		cfg:     cfg,
		order:   &input.Ordered{AllowInput: allowInput},
		wait:    map[string]chan struct{}{},
		granted: allowInput,
	}
	now := time.Now()
	observed := make([]link.Path, 0, len(paths))
	for i := range paths {
		p := &paths[i]
		l.paths = append(l.paths, p)
		l.byName[p.Name] = p
		rtt := p.InitialRTT
		if rtt <= 0 {
			rtt = time.Millisecond
		}
		observed = append(observed, link.Path{
			Name: p.Name, Started: now, Ready: true, ReadyAt: now, RTT: rtt,
		})
	}
	l.state = link.Step(now, link.State{}, observed, cfg)
	l.active = l.state.Selected
	return l
}

func (l *Live) Active() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active
}

func (l *Live) Switches() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.switches...)
}

func (l *Live) Videos() []videoNote {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]videoNote(nil), l.videos...)
}

func (l *Live) Injected() int { return l.order.Injected() }

// Start reads every path until ctx is done. The host uses Serve, which includes this.
func (l *Live) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range l.paths {
		wg.Add(1)
		go func(p *Endpoint) {
			defer wg.Done()
			l.readPath(ctx, p, false)
		}(p)
	}
	<-ctx.Done()
	wg.Wait()
}

// Serve captures and sends on the active path, and reads input on every path.
func (l *Live) Serve(ctx context.Context, cfg HostConfig) error {
	l.sink = cfg.Inject
	l.rejected = cfg.InputsRejected
	l.granted = cfg.GrantedInput
	l.upgrade = cfg.AllowUpgrade
	if cfg.Gate != nil {
		l.order.AllowInput = cfg.Gate.AllowInput
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- l.sendLoop(ctx, cfg) }()
	var wg sync.WaitGroup
	for _, p := range l.paths {
		wg.Add(1)
		go func(p *Endpoint) {
			defer wg.Done()
			l.readPath(ctx, p, true)
		}(p)
	}
	err := <-errc
	cancel()
	wg.Wait()
	return err
}

func (l *Live) sendLoop(ctx context.Context, cfg HostConfig) error {
	if cfg.Interval <= 0 {
		cfg.Interval = 50 * time.Millisecond
	}
	tick := time.NewTicker(cfg.Interval)
	defer tick.Stop()
	var seq uint32 = 1
	encode := cfg.Encode
	if encode == nil {
		encode = func(rgb []byte, w, h int) ([]byte, error) { return rgb, nil }
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if cfg.Alive != nil && !cfg.Alive() {
				return ErrRevoked
			}
			if cfg.Grab == nil {
				continue
			}
			rgb, w, h, err := cfg.Grab()
			if err != nil || w == 0 || h == 0 || len(rgb) < w*h*3 {
				continue
			}
			annex, err := encode(rgb[:w*h*3], w, h)
			if err != nil {
				continue
			}
			if err := l.Send(protocol.AppVideo, EncodeVideo(seq, w, h, annex)); err != nil {
				continue
			}
			if cfg.FramesSent != nil {
				*cfg.FramesSent++
			}
			seq++
		}
	}
}

// Measure probes every path and applies hysteresis. A failed probe moves off
// that path immediately. A better path waits for MinHold. The returned durations
// are only inputs to that choice.
func (l *Live) Measure(timeout time.Duration) string {
	if timeout <= 0 {
		timeout = 300 * time.Millisecond
	}
	now := time.Now()
	observed := make([]link.Path, 0, len(l.paths))
	for _, p := range l.paths {
		rtt := l.probe(p, timeout)
		failed := rtt <= 0
		observed = append(observed, link.Path{
			Name: p.Name, Started: now.Add(-time.Second), Ready: !failed, ReadyAt: now, RTT: rtt, Failed: failed,
		})
	}
	l.mu.Lock()
	next := link.Step(now, l.state, observed, l.cfg)
	changed := next.Selected != l.active && next.Selected != ""
	if changed {
		l.active = next.Selected
		l.switches = append(l.switches, next.Selected)
	}
	l.state = next
	name := l.active
	l.mu.Unlock()
	if changed {
		for _, p := range l.paths {
			_ = p.Send(protocol.AppPath, []byte(name))
		}
	}
	return name
}

func (l *Live) probe(p *Endpoint, timeout time.Duration) time.Duration {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return 0
	}
	ch := make(chan struct{}, 1)
	l.mu.Lock()
	l.wait[string(nonce[:])] = ch
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.wait, string(nonce[:]))
		l.mu.Unlock()
	}()
	start := time.Now()
	if err := p.Send(protocol.AppProbe, nonce[:]); err != nil {
		return 0
	}
	select {
	case <-ch:
		return time.Since(start)
	case <-time.After(timeout):
		return 0
	}
}

// Replace swaps one path for a newly authenticated transport and reads it.
// A lost datagram breaks the previous Noise transport, so the path has to be
// handshake again before it can carry media.
func (l *Live) Replace(ctx context.Context, e Endpoint, host bool) {
	p := e
	l.mu.Lock()
	found := false
	for i := range l.paths {
		if l.paths[i].Name == p.Name {
			l.paths[i] = &p
			found = true
			break
		}
	}
	if !found {
		l.paths = append(l.paths, &p)
	}
	l.byName[p.Name] = &p
	l.mu.Unlock()
	go l.readPath(ctx, &p, host)
}

// Send writes one application message on the active path.
func (l *Live) Send(app byte, payload []byte) error {
	l.mu.Lock()
	p := l.byName[l.active]
	l.mu.Unlock()
	if p == nil {
		return errNoPath
	}
	return p.Send(app, payload)
}

func (l *Live) readPath(ctx context.Context, p *Endpoint, host bool) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		app, payload, err := p.Recv()
		if err != nil {
			return
		}
		if host {
			l.hostMessage(p, app, payload)
			continue
		}
		l.ctrlMessage(p, app, payload)
	}
}

func (l *Live) hostMessage(p *Endpoint, app byte, payload []byte) {
	switch app {
	case protocol.AppProbe:
		_ = p.Send(protocol.AppProbeAck, payload)
	case protocol.AppPath:
		l.mu.Lock()
		l.active = string(payload)
		l.mu.Unlock()
	case protocol.AppInput:
		ev, err := DecodeInput(payload)
		if err != nil {
			return
		}
		l.mu.Lock()
		err = l.order.Apply(ev, l.sink)
		l.mu.Unlock()
		if err != nil && l.rejected != nil {
			*l.rejected++
		}
	case protocol.AppPolicy:
		if len(payload) == 0 {
			return
		}
		l.mu.Lock()
		switch payload[0] {
		case PolicyView:
			l.order.AllowInput = false
		case PolicyControl:
			if l.granted && l.upgrade {
				l.order.AllowInput = true
			} else if l.rejected != nil {
				*l.rejected++
			}
		}
		l.mu.Unlock()
	}
}

func (l *Live) ctrlMessage(p *Endpoint, app byte, payload []byte) {
	switch app {
	case protocol.AppProbeAck:
		if len(payload) != 8 {
			return
		}
		l.mu.Lock()
		ch := l.wait[string(payload)]
		l.mu.Unlock()
		if ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	case protocol.AppVideo:
		seq, rgb, _, _, ok, err := DecodeDeliveredFrame(protocol.AppVideo, payload)
		if err != nil || !ok || len(rgb) == 0 {
			return
		}
		l.mu.Lock()
		l.videos = append(l.videos, videoNote{Path: p.Name, Seq: seq})
		l.mu.Unlock()
	}
}

var errNoPath = errText("desktop: no active path")

type errText string

func (e errText) Error() string { return string(e) }
