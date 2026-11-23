package link

import (
	"testing"
	"time"
)

func TestSelectsProbeWinnerNotFirstTCP(t *testing.T) {
	t0 := time.Now()
	cfg := Config{Improve: 0.20, MinHold: 3 * time.Second, Cooldown: 10 * time.Second}
	// Relay TCP finishes first. Its authenticated probe is slower.
	// Direct TCP is later, but its probe finishes earlier and is the first usable path.
	paths := []Path{
		{Name: "relay", Started: t0, TCPReady: t0.Add(1 * time.Millisecond), Ready: true, ReadyAt: t0.Add(51 * time.Millisecond), RTT: 50 * time.Millisecond},
		{Name: "direct", Started: t0.Add(1 * time.Millisecond), TCPReady: t0.Add(20 * time.Millisecond), Ready: true, ReadyAt: t0.Add(22 * time.Millisecond), RTT: 2 * time.Millisecond},
	}
	if !StartedTogether(paths, 20*time.Millisecond) {
		t.Fatal("paths were not started together")
	}
	st := Step(t0.Add(22*time.Millisecond), State{}, []Path{
		paths[0],
		{Name: "direct", Started: paths[1].Started, TCPReady: paths[1].TCPReady, Ready: true, ReadyAt: paths[1].ReadyAt, RTT: paths[1].RTT},
	}, cfg)
	if st.Selected != "direct" || st.Reason != "first-usable" {
		t.Fatalf("selected %s (%s), want direct as first probed path", st.Selected, st.Reason)
	}
	if !paths[0].TCPReady.Before(paths[1].TCPReady) {
		t.Fatal("fixture no longer has relay TCP first")
	}
}

func TestHysteresisThenFailover(t *testing.T) {
	t0 := time.Now()
	cfg := Config{Improve: 0.20, MinHold: 100 * time.Millisecond, Cooldown: 50 * time.Millisecond}
	relay := Path{Name: "relay", Started: t0, Ready: true, ReadyAt: t0.Add(5 * time.Millisecond), RTT: 40 * time.Millisecond}
	st := Step(t0.Add(5*time.Millisecond), State{}, []Path{relay}, cfg)
	if st.Selected != "relay" {
		t.Fatal(st)
	}
	direct := Path{Name: "direct", Started: t0, Ready: true, ReadyAt: t0.Add(40 * time.Millisecond), RTT: 5 * time.Millisecond}
	st = Step(t0.Add(40*time.Millisecond), st, []Path{relay, direct}, cfg)
	if st.Selected != "relay" || st.Pending != "direct" {
		t.Fatalf("should watch, not switch immediately: %+v", st)
	}
	st = Step(t0.Add(80*time.Millisecond), st, []Path{relay, direct}, cfg)
	if st.Selected != "relay" {
		t.Fatalf("hold not elapsed: %+v", st)
	}
	st = Step(t0.Add(150*time.Millisecond), st, []Path{relay, direct}, cfg)
	if st.Selected != "direct" || st.Reason != "hysteresis" {
		t.Fatalf("want hysteresis to direct, got %+v", st)
	}
	relay.Failed = true
	st = Step(t0.Add(200*time.Millisecond), st, []Path{relay, direct}, cfg)
	if st.Selected != "direct" {
		t.Fatalf("direct should remain, got %+v", st)
	}
	direct.RTT = 6 * time.Millisecond
	st = Step(t0.Add(160*time.Millisecond), st, []Path{relay, direct}, cfg)
	if st.Selected != "direct" {
		t.Fatalf("a later sample of the same better path must not undo the switch: %+v", st)
	}
	direct.Failed = true
	st = Step(t0.Add(220*time.Millisecond), st, []Path{relay, direct}, cfg)
	if st.Selected != "" || st.Reason != "selected-failed" {
		t.Fatalf("both failed: %+v", st)
	}
}
