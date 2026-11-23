package migrate

import (
	"sync"
	"time"

	"cyandesk/internal/input"
	"cyandesk/internal/link"
)

// Path is one already-authenticated encrypted transport.
type Path struct {
	Name   string
	Send   func([]byte) error
	Recv   func() ([]byte, error)
	Down   func() bool
	Probe  func() time.Duration
}

// Session carries media and input on the selected path and can move to
// another authenticated path. Input sequence state does not reset on a move.
type Session struct {
	mu       sync.Mutex
	paths    map[string]*Path
	order    []string
	active   string
	state    link.State
	cfg      link.Config
	gate     *input.Ordered
	onSwitch func(name, reason string)
	switches []string
}

func New(paths []*Path, cfg link.Config, gate *input.Ordered) *Session {
	s := &Session{
		paths: map[string]*Path{},
		cfg:   cfg,
		gate:  gate,
	}
	if s.gate == nil {
		s.gate = &input.Ordered{AllowInput: true}
	}
	now := time.Now()
	observed := make([]link.Path, 0, len(paths))
	for _, p := range paths {
		s.paths[p.Name] = p
		s.order = append(s.order, p.Name)
		rtt := time.Millisecond
		if p.Probe != nil {
			rtt = p.Probe()
		}
		observed = append(observed, link.Path{
			Name: p.Name, Started: now, Ready: true, ReadyAt: now, RTT: rtt,
		})
	}
	s.state = link.Step(now, link.State{}, observed, cfg)
	s.active = s.state.Selected
	return s
}

func (s *Session) Active() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

func (s *Session) Switches() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.switches...)
	return out
}

func (s *Session) Gate() *input.Ordered { return s.gate }

// Observe remeasures every path and applies hysteresis. A failed active path
// moves immediately. A better path waits for MinHold.
func (s *Session) Observe(now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	observed := make([]link.Path, 0, len(s.order))
	for _, name := range s.order {
		p := s.paths[name]
		failed := p.Down != nil && p.Down()
		rtt := s.state.SelectedRTT
		if !failed && p.Probe != nil {
			rtt = p.Probe()
			if rtt <= 0 {
				failed = true
			}
		}
		observed = append(observed, link.Path{
			Name: name, Started: now.Add(-time.Second), Ready: !failed, ReadyAt: now, RTT: rtt, Failed: failed,
		})
	}
	next := link.Step(now, s.state, observed, s.cfg)
	if next.Selected != s.active && next.Selected != "" {
		s.active = next.Selected
		s.switches = append(s.switches, next.Selected)
		if s.onSwitch != nil {
			s.onSwitch(next.Selected, next.Reason)
		}
		for _, p := range s.paths {
			_ = p.Send(append([]byte("S"), next.Selected...))
		}
	}
	s.state = next
	return s.active
}

// SendMedia sends one frame on the active path only.
func (s *Session) SendMedia(frame []byte) error {
	s.mu.Lock()
	p := s.paths[s.active]
	s.mu.Unlock()
	if p == nil {
		return errNoPath
	}
	body := append([]byte("V"), frame...)
	return p.Send(body)
}

// SendInput sends one input on the active path. The host gate, not this send,
// decides whether a duplicate or reordered copy is injected.
func (s *Session) SendInput(seq uint64) error {
	s.mu.Lock()
	p := s.paths[s.active]
	s.mu.Unlock()
	if p == nil {
		return errNoPath
	}
	body := []byte{'I', byte(seq)}
	return p.Send(body)
}

// HostPump reads every path. Probes are answered. Switch notices update the
// active name. Inputs go through the ordered gate.
func (s *Session) HostPump(stop <-chan struct{}, sink input.Sink) {
	var wg sync.WaitGroup
	for _, p := range s.paths {
		wg.Add(1)
		go func(p *Path) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				msg, err := p.Recv()
				if err != nil {
					return
				}
				if len(msg) == 0 {
					continue
				}
				switch msg[0] {
				case 'P':
					_ = p.Send(append([]byte{'A'}, msg[1:]...))
				case 'S':
					s.mu.Lock()
					s.active = string(msg[1:])
					s.mu.Unlock()
				case 'I':
					if len(msg) < 2 {
						continue
					}
					_ = s.gate.Apply(input.Event{Epoch: 1, Seq: uint64(msg[1])}, sink)
				}
			}
		}(p)
	}
	wg.Wait()
}

var errNoPath = errText("migrate: no active path")

type errText string

func (e errText) Error() string { return string(e) }
