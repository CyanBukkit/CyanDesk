package input

import "errors"

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
	AllowInput bool
	lastEpoch  uint32
	lastSeq    uint64
	seen       bool
	injected   int
}

func (g *Gate) Injected() int { return g.injected }

// Apply is the host-side check. The UI is not consulted here.
func (g *Gate) Apply(e Event, sink Sink) error {
	if !g.AllowInput {
		return ErrViewOnly
	}
	if g.seen && e.Epoch == g.lastEpoch && e.Seq <= g.lastSeq {
		return nil
	}
	if sink != nil {
		if err := sink.Inject(e); err != nil {
			return err
		}
	}
	g.injected++
	g.seen = true
	g.lastEpoch = e.Epoch
	g.lastSeq = e.Seq
	return nil
}

type CountingSink struct{ N int }

func (c *CountingSink) Inject(Event) error {
	c.N++
	return nil
}
