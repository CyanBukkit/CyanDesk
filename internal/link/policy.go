package link

import "time"

// Config holds path-selection thresholds. Production defaults are not a claim
// that they have been tuned on a real network.
type Config struct {
	Improve  float64
	MinHold  time.Duration
	Cooldown time.Duration
}

func ProductionConfig() Config {
	return Config{Improve: 0.20, MinHold: 3 * time.Second, Cooldown: 10 * time.Second}
}

type Path struct {
	Name      string
	Started   time.Time
	TCPReady  time.Time
	Ready     bool
	ReadyAt   time.Time
	RTT       time.Duration
	Failed    bool
	Cancelled bool
}

type State struct {
	Selected     string
	SelectedRTT  time.Duration
	SelectedAt   time.Time
	LastSwitch   time.Time
	Pending      string
	PendingSince time.Time
	PendingRTT   time.Duration
	Reason       string
}

func (s State) changed(prev State) bool {
	return s.Selected != prev.Selected
}

// Step applies one observation. A path is usable only after an authenticated
// probe (Ready), never merely because TCPReady is set.
func Step(now time.Time, st State, paths []Path, cfg Config) State {
	prev := st
	usable := make([]Path, 0, len(paths))
	selectedFailed := false
	haveSelected := false
	for _, p := range paths {
		if p.Name == st.Selected && (p.Failed || p.Cancelled) {
			selectedFailed = true
		}
		if p.Name == st.Selected && p.Ready && !p.Failed && !p.Cancelled {
			haveSelected = true
			st.SelectedRTT = p.RTT
		}
		if p.Ready && !p.Failed && !p.Cancelled && p.RTT > 0 {
			usable = append(usable, p)
		}
	}
	if len(usable) == 0 {
		if st.Selected != "" && selectedFailed {
			st.Selected = ""
			st.Reason = "selected-failed"
			st.Pending = ""
		}
		return st
	}
	if st.Selected == "" || selectedFailed || !haveSelected {
		best := earliest(usable)
		st.Selected = best.Name
		st.SelectedRTT = best.RTT
		st.SelectedAt = now
		st.LastSwitch = now
		st.Pending = ""
		if selectedFailed {
			st.Reason = "failover"
		} else {
			st.Reason = "first-usable"
		}
		return st
	}
	best := lowestRTT(usable)
	threshold := time.Duration(float64(st.SelectedRTT) * (1 - cfg.Improve))
	better := best.Name != st.Selected && best.RTT <= threshold && best.RTT < st.SelectedRTT
	if !better {
		st.Pending = ""
		st.Reason = prev.Reason
		return st
	}
	if st.Pending != best.Name {
		st.Pending = best.Name
		st.PendingSince = now
		st.PendingRTT = best.RTT
		st.Reason = "watching"
		return st
	}
	st.PendingRTT = best.RTT
	if now.Sub(st.PendingSince) >= cfg.MinHold && (st.LastSwitch.IsZero() || now.Sub(st.LastSwitch) >= cfg.Cooldown) {
		st.Selected = best.Name
		st.SelectedRTT = best.RTT
		st.SelectedAt = now
		st.LastSwitch = now
		st.Pending = ""
		st.Reason = "hysteresis"
	}
	return st
}

func earliest(paths []Path) Path {
	best := paths[0]
	for _, p := range paths[1:] {
		if p.ReadyAt.Before(best.ReadyAt) {
			best = p
		}
	}
	return best
}

func lowestRTT(paths []Path) Path {
	best := paths[0]
	for _, p := range paths[1:] {
		if p.RTT < best.RTT {
			best = p
		}
	}
	return best
}

// StartedTogether reports whether every path was started within slack of the
// first one. Callers use it to show that relay and direct were not sequenced
// behind each other.
func StartedTogether(paths []Path, slack time.Duration) bool {
	if len(paths) == 0 {
		return false
	}
	min := paths[0].Started
	max := paths[0].Started
	for _, p := range paths[1:] {
		if p.Started.Before(min) {
			min = p.Started
		}
		if p.Started.After(max) {
			max = p.Started
		}
	}
	return max.Sub(min) <= slack
}
