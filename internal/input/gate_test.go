package input

import "testing"

func TestViewOnlyDoesNotInjectAndDuplicatesAreIgnored(t *testing.T) {
	sink := &CountingSink{}
	g := Gate{AllowInput: false}
	if err := g.Apply(Event{Epoch: 1, Seq: 1, Kind: 2}, sink); err != ErrViewOnly {
		t.Fatalf("err=%v", err)
	}
	if sink.N != 0 || g.Injected() != 0 {
		t.Fatalf("view-only injected sink=%d gate=%d", sink.N, g.Injected())
	}
	g.AllowInput = true
	if err := g.Apply(Event{Epoch: 1, Seq: 1}, sink); err != nil {
		t.Fatal(err)
	}
	if err := g.Apply(Event{Epoch: 1, Seq: 1}, sink); err != nil {
		t.Fatal(err)
	}
	if err := g.Apply(Event{Epoch: 1, Seq: 2}, sink); err != nil {
		t.Fatal(err)
	}
	if sink.N != 2 {
		t.Fatalf("injections=%d, duplicate was not suppressed", sink.N)
	}
}
