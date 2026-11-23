package input

import (
	"sync"
	"time"
)

// Ordered delivers events exactly once and in sequence order.
// A reordered packet waits until the missing sequence numbers arrive.
// Expire skips a hole that has already waited, so a lost event cannot block
// later input forever. A repeated sequence number is ignored.
type Ordered struct {
	AllowInput bool
	mu         sync.Mutex
	next       uint64
	buf        map[uint64]pending
	injected   int
	started    bool
}

type pending struct {
	ev Event
	at time.Time
}

func (o *Ordered) SetAllow(v bool) {
	o.mu.Lock()
	o.AllowInput = v
	o.mu.Unlock()
}

func (o *Ordered) Injected() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.injected
}

func (o *Ordered) Apply(e Event, sink Sink) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.apply(e, time.Now(), sink)
}

// Expire releases a held gap once the oldest buffered event is older than gap.
func (o *Ordered) Expire(now time.Time, gap time.Duration, sink Sink) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.started || len(o.buf) == 0 || gap <= 0 {
		return
	}
	oldestSeq := uint64(0)
	var oldest time.Time
	first := true
	for seq, item := range o.buf {
		if first || item.at.Before(oldest) {
			oldest = item.at
			oldestSeq = seq
			first = false
		}
	}
	if now.Sub(oldest) < gap {
		return
	}
	if oldestSeq > o.next {
		o.next = oldestSeq
	}
	o.flush(sink)
}

func (o *Ordered) apply(e Event, now time.Time, sink Sink) error {
	if !o.AllowInput {
		return ErrViewOnly
	}
	if !o.started {
		o.started = true
		o.next = e.Seq
	}
	if e.Seq < o.next {
		return nil
	}
	if o.buf == nil {
		o.buf = map[uint64]pending{}
	}
	if _, exists := o.buf[e.Seq]; exists {
		return nil
	}
	o.buf[e.Seq] = pending{ev: e, at: now}
	o.flush(sink)
	return nil
}

func (o *Ordered) flush(sink Sink) {
	for {
		item, ok := o.buf[o.next]
		if !ok {
			return
		}
		delete(o.buf, o.next)
		if sink != nil {
			_ = sink.Inject(item.ev)
		}
		o.injected++
		o.next++
	}
}
