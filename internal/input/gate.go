package input

import (
	"errors"
	"sync"
	"time"
)

var ErrViewOnly = errors.New("input: view-only session rejected injection")

type Event struct {
	Epoch  uint32
	Seq    uint64
	Kind   uint8
	X      int32
	Y      int32
	Button uint8
	Key    uint32
}

type Sink interface {
	Inject(Event) error
}

type Gate struct {
	mu         sync.Mutex
	AllowInput bool
	lastEpoch  uint32
	lastSeq    uint64
	seen       bool
	injected   int
	buf        map[uint64]pendingInput
}

type pendingInput struct {
	ev Event
	at time.Time
}

func (g *Gate) SetAllow(v bool) {
	g.mu.Lock()
	g.AllowInput = v
	g.mu.Unlock()
}

func (g *Gate) Injected() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.injected
}

// Apply is the host-side check. The UI is not consulted here.
func (g *Gate) Apply(e Event, sink Sink) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.AllowInput {
		return ErrViewOnly
	}
	if g.seen && e.Epoch == g.lastEpoch && e.Seq <= g.lastSeq {
		return nil
	}
	if g.buf == nil {
		g.buf = map[uint64]pendingInput{}
	}
	if _, exists := g.buf[e.Seq]; exists {
		return nil
	}
	if !g.seen {
		g.seen = true
		g.lastSeq = e.Seq - 1
		g.lastEpoch = e.Epoch
	}
	g.buf[e.Seq] = pendingInput{ev: e, at: time.Now()}
	g.flush(sink)
	return nil
}

// Expire skips a hole after gap so a missing sequence cannot block later input.
func (g *Gate) Expire(now time.Time, gap time.Duration, sink Sink) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.seen || len(g.buf) == 0 || gap <= 0 {
		return
	}
	var oldest time.Time
	var seq uint64
	first := true
	for n, item := range g.buf {
		if first || item.at.Before(oldest) {
			oldest = item.at
			seq = n
			first = false
		}
	}
	if now.Sub(oldest) < gap || seq <= g.lastSeq {
		return
	}
	g.lastSeq = seq - 1
	g.flush(sink)
}

func (g *Gate) flush(sink Sink) {
	for {
		item, ok := g.buf[g.lastSeq+1]
		if !ok {
			return
		}
		delete(g.buf, item.ev.Seq)
		if sink != nil {
			_ = sink.Inject(item.ev)
		}
		g.injected++
		g.lastSeq = item.ev.Seq
		g.lastEpoch = item.ev.Epoch
	}
}

type CountingSink struct{ N int }

func (c *CountingSink) Inject(Event) error {
	c.N++
	return nil
}
