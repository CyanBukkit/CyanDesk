package input

import "testing"

func TestOrderedExactlyOnceUnderReorder(t *testing.T) {
	o := Ordered{AllowInput: true}
	sink := &CountingSink{}
	events := []Event{
		{Seq: 1},
		{Seq: 3},
		{Seq: 1},
		{Seq: 2},
		{Seq: 2},
		{Seq: 3},
	}
	for _, e := range events {
		if err := o.Apply(e, sink); err != nil {
			t.Fatal(err)
		}
	}
	if sink.N != 3 || o.Injected() != 3 {
		t.Fatalf("injected %d", sink.N)
	}
	o.AllowInput = false
	if err := o.Apply(Event{Seq: 4}, sink); err != ErrViewOnly {
		t.Fatal(err)
	}
	if sink.N != 3 {
		t.Fatal("view-only injected")
	}
}
