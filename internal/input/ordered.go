package input

// Ordered delivers events exactly once and in sequence order.
// A reordered packet waits until the missing sequence numbers arrive.
// A repeated sequence number is ignored. The epoch must stay the same
// for the life of the session, including path changes.
type Ordered struct {
	AllowInput bool
	next       uint64
	buf        map[uint64]Event
	injected   int
	started    bool
}

func (o *Ordered) Injected() int { return o.injected }

func (o *Ordered) Apply(e Event, sink Sink) error {
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
		o.buf = map[uint64]Event{}
	}
	if _, exists := o.buf[e.Seq]; exists {
		return nil
	}
	o.buf[e.Seq] = e
	for {
		ev, ok := o.buf[o.next]
		if !ok {
			return nil
		}
		delete(o.buf, o.next)
		if sink != nil {
			if err := sink.Inject(ev); err != nil {
				return err
			}
		}
		o.injected++
		o.next++
	}
}
