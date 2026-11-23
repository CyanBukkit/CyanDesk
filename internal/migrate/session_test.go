package migrate

import (
	"sync"
	"testing"
	"time"

	"cyandesk/internal/input"
	"cyandesk/internal/link"
)

type memPath struct {
	name string
	in   chan []byte
	out  *memPath
	down bool
	rtt  time.Duration
	mu   sync.Mutex
}

func pair(name string, rtt time.Duration) (*memPath, *memPath) {
	a := &memPath{name: name, in: make(chan []byte, 8), rtt: rtt}
	b := &memPath{name: name, in: make(chan []byte, 8), rtt: rtt}
	a.out = b
	b.out = a
	return a, b
}

func (m *memPath) asPath() *Path {
	return &Path{
		Name: m.name,
		Send: func(b []byte) error {
			m.mu.Lock()
			down := m.down
			m.mu.Unlock()
			if down {
				return errNoPath
			}
			m.out.in <- append([]byte(nil), b...)
			return nil
		},
		Recv: func() ([]byte, error) {
			return <-m.in, nil
		},
		Down: func() bool {
			m.mu.Lock()
			defer m.mu.Unlock()
			return m.down
		},
		Probe: func() time.Duration {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.down {
				return 0
			}
			return m.rtt
		},
	}
}

func TestLiveMigrationKeepsInputExactlyOnce(t *testing.T) {
	dC, dH := pair("direct-udp", 2*time.Millisecond)
	rC, rH := pair("relay", 40*time.Millisecond)
	cfg := link.Config{Improve: 0.20, MinHold: 150 * time.Millisecond, Cooldown: 0}
	sink := &input.CountingSink{}
	host := New([]*Path{dH.asPath(), rH.asPath()}, cfg, &input.Ordered{AllowInput: true})
	ctrl := New([]*Path{dC.asPath(), rC.asPath()}, cfg, nil)
	stop := make(chan struct{})
	defer close(stop)
	go host.HostPump(stop, sink)
	if ctrl.Active() != "direct-udp" {
		t.Fatalf("initial path %s", ctrl.Active())
	}
	for seq := uint64(1); seq <= 3; seq++ {
		if err := ctrl.SendInput(seq); err != nil {
			t.Fatal(err)
		}
	}
	waitInjected(t, sink, 3)
	dC.mu.Lock()
	dC.down = true
	dH.mu.Lock()
	dH.down = true
	dC.mu.Unlock()
	dH.mu.Unlock()
	now := time.Now()
	if got := ctrl.Observe(now); got != "relay" {
		t.Fatalf("loss did not move the session to relay, active=%s", got)
	}
	if err := ctrl.SendInput(4); err != nil {
		t.Fatal(err)
	}
	// The same input is also offered on the recovered duplicate path later.
	waitInjected(t, sink, 4)
	dC.mu.Lock()
	dC.down = false
	dH.mu.Lock()
	dH.down = false
	dC.rtt = time.Millisecond
	dH.rtt = time.Millisecond
	dC.mu.Unlock()
	dH.mu.Unlock()
	ctrl.Observe(now.Add(10 * time.Millisecond))
	if ctrl.Active() == "direct-udp" {
		t.Fatal("hysteresis moved back before the hold elapsed")
	}
	if got := ctrl.Observe(now.Add(200 * time.Millisecond)); got != "direct-udp" {
		t.Fatalf("direct path did not return after the hold, active=%s switches=%v", got, ctrl.Switches())
	}
	if err := ctrl.SendInput(4); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SendInput(6); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SendInput(5); err != nil {
		t.Fatal(err)
	}
	waitInjected(t, sink, 6)
	if sink.N != 6 {
		t.Fatalf("injected %d, want 6 with no duplicates", sink.N)
	}
}

func waitInjected(t *testing.T, sink *input.CountingSink, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sink.N >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("injected %d, want at least %d", sink.N, n)
}
