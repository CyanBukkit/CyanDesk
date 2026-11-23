package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cyandesk/internal/e2ee"
	"cyandesk/internal/input"
	"cyandesk/internal/link"
	"cyandesk/internal/media"
	"cyandesk/internal/protocol"
	"github.com/flynn/noise"
)

func TestLiveUDPMigration(t *testing.T) {
	cKey, err := e2ee.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	hKey, err := e2ee.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	hostNoise := base64.RawStdEncoding.EncodeToString(hKey.Public)
	if err := CheckPeer("pinned", "pinned", hostNoise, hostNoise); err != nil {
		t.Fatal(err)
	}
	if err := CheckPeer("pinned", "changed", hostNoise, hostNoise); err == nil {
		t.Fatal("a changed fingerprint was accepted")
	}
	pro := e2ee.Prologue("live-1", "nonce", "view,input", hostNoise, base64.RawStdEncoding.EncodeToString(cKey.Public))
	if _, _, err := handshakeUDPSides(cKey, hKey, []byte("cyandesk/1\nwrong\n"), pro); err == nil {
		t.Fatal("a mismatched prologue completed the handshake")
	}

	drop := &atomic.Bool{}
	cDirect, hDirect := mustPair(t, cKey, hKey, pro, drop, 0)
	cRelay, hRelay := mustPair(t, cKey, hKey, pro, nil, 90*time.Millisecond)
	defer cDirect.close()
	defer hDirect.close()
	defer cRelay.close()
	defer hRelay.close()

	cfg := link.Config{Improve: 0.20, MinHold: 150 * time.Millisecond, Cooldown: 0}
	host := NewLive([]Endpoint{
		hDirect.endpoint("direct-udp", time.Millisecond),
		hRelay.endpoint("relay", 40*time.Millisecond),
	}, cfg, true)
	ctrl := NewLive([]Endpoint{
		cDirect.endpoint("direct-udp", time.Millisecond),
		cRelay.endpoint("relay", 40*time.Millisecond),
	}, cfg, true)
	if host.Active() != "direct-udp" || ctrl.Active() != "direct-udp" {
		t.Fatalf("initial host=%s ctrl=%s", host.Active(), ctrl.Active())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &seqSink{}
	var rejected int
	go host.Serve(ctx, HostConfig{
		GrantedInput: true,
		Inject:       sink,
		Interval:     30 * time.Millisecond,
		Encode:       media.EncodeJPEG,
		Grab: func() ([]byte, int, int, error) {
			return []byte{220, 20, 20, 20, 20, 220, 20, 180, 20, 240, 240, 20}, 2, 2, nil
		},
		InputsRejected: &rejected,
	})
	go ctrl.Start(ctx)

	before := waitVideos(t, ctrl, "direct-udp", 2)
	for _, seq := range []uint64{1, 2, 3} {
		if err := ctrl.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: seq, Kind: 2})); err != nil {
			t.Fatal(err)
		}
	}
	waitSeqs(t, sink, 3)

	drop.Store(true)
	if got := ctrl.Measure(250 * time.Millisecond); got != "relay" {
		t.Fatalf("direct loss left the session on %s", got)
	}
	if err := ctrl.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 4, Kind: 2})); err != nil {
		t.Fatal(err)
	}
	waitSeqs(t, sink, 4)
	if err := ctrl.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 4, Kind: 2})); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 6, Kind: 2})); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 5, Kind: 2})); err != nil {
		t.Fatal(err)
	}
	waitSeqs(t, sink, 6)
	onRelay := waitVideos(t, ctrl, "relay", 1)
	if onRelay < 1 {
		t.Fatal("no decoded frame arrived on the relay after the move")
	}
	directBeforeReturn := 0
	for _, v := range ctrl.Videos() {
		if v.Path == "direct-udp" {
			directBeforeReturn++
		}
	}

	cDirect.close()
	hDirect.close()
	cBack, hBack := mustPair(t, cKey, hKey, pro, nil, 0)
	defer cBack.close()
	defer hBack.close()
	ctrl.Replace(ctx, cBack.endpoint("direct-udp", time.Millisecond), false)
	host.Replace(ctx, hBack.endpoint("direct-udp", time.Millisecond), true)
	time.Sleep(50 * time.Millisecond)
	if got := ctrl.Measure(400 * time.Millisecond); got != "relay" {
		t.Fatalf("moved back before the hold: %s", got)
	}
	time.Sleep(200 * time.Millisecond)
	if got := ctrl.Measure(400 * time.Millisecond); got != "direct-udp" {
		t.Fatalf("direct path did not return after the hold, active=%s switches=%v", got, ctrl.Switches())
	}
	if err := ctrl.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 7, Kind: 2})); err != nil {
		t.Fatal(err)
	}
	waitSeqs(t, sink, 7)
	waitVideos(t, ctrl, "direct-udp", directBeforeReturn+1)
	if err := ctrl.Send(protocol.AppPolicy, []byte{PolicyView}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := ctrl.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 8, Kind: 2})); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	got := sink.snapshot()
	if len(got) != 7 || got[0] != 1 || got[6] != 7 {
		t.Fatalf("input seqs = %v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] == got[i-1] {
			t.Fatalf("duplicate injection %v", got)
		}
	}
	raw, err := json.Marshal(map[string]any{
		"network":                      "localhost-udp",
		"nat":                          "not-nat",
		"public_nat":                   false,
		"e2ee":                         e2ee.SuiteName,
		"changed_fingerprint_rejected": true,
		"mismatched_prologue_rejected": true,
		"switches":                     ctrl.Switches(),
		"input_seqs":                   got,
		"frames_on_direct_before_loss": before,
		"frames_decoded_on_relay":      onRelay,
		"duplicate_injected":           false,
		"direct_recovery":              "new-handshake-after-loss",
		"rtt_role":                     "relative selector input only",
		"performance_claim":            false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("/opt/cursor/artifacts/cyandesk", 0o755); err == nil {
		_ = os.WriteFile("/opt/cursor/artifacts/cyandesk/migration.json", append(raw, '\n'), 0o644)
	}
}

type seqSink struct {
	mu   sync.Mutex
	seqs []uint64
}

func (s *seqSink) Inject(e input.Event) error {
	s.mu.Lock()
	s.seqs = append(s.seqs, e.Seq)
	s.mu.Unlock()
	return nil
}

func (s *seqSink) snapshot() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uint64(nil), s.seqs...)
}

func waitSeqs(t *testing.T, sink *seqSink, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(sink.snapshot()) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("injected %v, want %d", sink.snapshot(), n)
}

func waitVideos(t *testing.T, live *Live, path string, n int) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		count := 0
		for _, v := range live.Videos() {
			if v.Path == path {
				count++
			}
		}
		if count >= n {
			return count
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("decoded frames on %s did not reach %d (have %d)", path, n, len(live.Videos()))
	return 0
}

type securePath struct {
	conn *net.UDPConn
	sess *e2ee.Session
	pipe *udpEP
	mu   sync.Mutex
}

func (s *securePath) endpoint(name string, initial time.Duration) Endpoint {
	return Endpoint{
		Name: name, InitialRTT: initial,
		Send: func(app byte, payload []byte) error {
			buf := make([]byte, 1+len(payload))
			buf[0] = app
			copy(buf[1:], payload)
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.sess.Send(nil, buf)
		},
		Recv: func() (byte, []byte, error) {
			buf, err := s.sess.Recv(nil)
			if err != nil {
				return 0, nil, err
			}
			if len(buf) == 0 {
				return 0, nil, errors.New("empty")
			}
			return buf[0], append([]byte(nil), buf[1:]...), nil
		},
	}
}

func (s *securePath) close() { _ = s.conn.Close() }

func mustPair(t *testing.T, cKey, hKey noise.DHKey, pro []byte, drop *atomic.Bool, delay time.Duration) (*securePath, *securePath) {
	t.Helper()
	c, h, err := handshakeUDPSides(cKey, hKey, pro, pro)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.conn.SetReadDeadline(time.Time{})
	_ = h.conn.SetReadDeadline(time.Time{})
	c.pipe.drop = drop
	h.pipe.drop = drop
	c.pipe.delay = delay
	h.pipe.delay = delay
	return c, h
}

func handshakeUDPSides(cKey, hKey noise.DHKey, initPro, respPro []byte) (*securePath, *securePath, error) {
	a, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, nil, err
	}
	b, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		a.Close()
		return nil, nil, err
	}
	_ = a.SetReadDeadline(time.Now().Add(2 * time.Second))
	_ = b.SetReadDeadline(time.Now().Add(2 * time.Second))
	left := &udpEP{conn: a, peer: b.LocalAddr().(*net.UDPAddr)}
	right := &udpEP{conn: b, peer: a.LocalAddr().(*net.UDPAddr)}
	var cs, hs *e2ee.Session
	var e1, e2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		cs, e1 = e2ee.Handshake(left, true, cKey, hKey.Public, initPro)
	}()
	go func() {
		defer wg.Done()
		hs, e2 = e2ee.Handshake(right, false, hKey, nil, respPro)
	}()
	wg.Wait()
	if e1 != nil || e2 != nil {
		a.Close()
		b.Close()
		if e1 != nil {
			return nil, nil, e1
		}
		return nil, nil, e2
	}
	return &securePath{conn: a, sess: cs, pipe: left}, &securePath{conn: b, sess: hs, pipe: right}, nil
}

type udpEP struct {
	conn  *net.UDPConn
	peer  *net.UDPAddr
	drop  *atomic.Bool
	delay time.Duration
}

func (u *udpEP) WriteMsg(b []byte) error {
	if u.drop != nil && u.drop.Load() {
		return nil
	}
	if u.delay > 0 {
		time.Sleep(u.delay)
	}
	_, err := u.conn.WriteToUDP(b, u.peer)
	return err
}

func (u *udpEP) ReadMsg() ([]byte, error) {
	buf := make([]byte, 65535)
	n, _, err := u.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), buf[:n]...), nil
}

func (u *udpEP) Close() error { return u.conn.Close() }
