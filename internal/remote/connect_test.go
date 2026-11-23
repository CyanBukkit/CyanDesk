package remote

import (
	"context"
	"net"
	"testing"
	"time"

	"cyandesk/internal/e2ee"
	"cyandesk/internal/gateway"
	"cyandesk/internal/media"
	"cyandesk/internal/protocol"
)

func TestParallelPathsCarryARealFrame(t *testing.T) {
	v := &allowOnce{}
	gw := gateway.New(v)
	gw.ForwardDelay = 25 * time.Millisecond
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = gw.Serve(ln) }()

	hostKey, err := e2ee.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	ctrlKey, err := e2ee.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pro := e2ee.Prologue("s1", "n", "view", "hn", "cn")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	hostCh := make(chan *Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := Open(ctx, Side{
			Gateway: ln.Addr().String(), Role: protocol.RoleHost, Ticket: "good",
			Static: hostKey, Prologue: pro,
		})
		if err != nil {
			errCh <- err
			return
		}
		hostCh <- res
	}()
	ctrl, err := Open(ctx, Side{
		Gateway: ln.Addr().String(), Role: protocol.RoleController, Ticket: "good",
		Static: ctrlKey, PeerStatic: hostKey.Public, Prologue: pro,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	var host *Result
	select {
	case host = <-hostCh:
	case err := <-errCh:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("host did not finish path setup")
	}
	defer host.Close()
	if ctrl.Selected == "" {
		t.Fatal("controller did not select a path")
	}
	names := map[string]bool{}
	var relayRTT, directRTT time.Duration
	var starts []time.Time
	for _, p := range ctrl.Paths {
		names[p.Name] = true
		starts = append(starts, p.Started)
		if p.Name == "relay" {
			relayRTT = p.RTT
		}
		if p.Name == "direct" || p.Name == "direct-accept" {
			if directRTT == 0 || p.RTT < directRTT {
				directRTT = p.RTT
			}
		}
	}
	if !names["relay"] || directRTT == 0 {
		t.Fatalf("expected relay and a direct candidate, paths=%v", ctrl.Paths)
	}
	if relayRTT <= directRTT {
		t.Fatalf("relay rtt %s was not slower than direct %s; the delay was not measured", relayRTT, directRTT)
	}
	if ctrl.Selected == "relay" {
		t.Fatalf("selected the slower relay (%s) over direct (%s)", relayRTT, directRTT)
	}
	rgb := redFrame(32, 32)
	annex, err := media.EncodeH264(rgb, 32, 32)
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Send(protocol.AppVideo, annex); err != nil {
		t.Fatal(err)
	}
	kind, got, err := host.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if kind != protocol.AppVideo {
		t.Fatalf("kind %d", kind)
	}
	decoded, err := media.DecodeH264(got, 32, 32)
	if err != nil {
		t.Fatal(err)
	}
	if decoded[0] < 150 {
		t.Fatalf("red channel %d", decoded[0])
	}
	if v.n != 1 {
		t.Fatalf("gateway redeemed the ticket %d times", v.n)
	}
}

func redFrame(w, h int) []byte {
	b := make([]byte, w*h*3)
	for i := 0; i < w*h; i++ {
		b[i*3] = 230
		b[i*3+1] = 8
		b[i*3+2] = 8
	}
	return b
}

type allowOnce struct {
	n int
}

func (a *allowOnce) Redeem(context.Context, string) (gateway.Claims, error) {
	a.n++
	if a.n > 1 {
		return gateway.Claims{}, gateway.ErrDenied
	}
	return gateway.Claims{SessionID: "s1", HostID: "h", ControllerID: "c", BytesLimit: 8 << 20, Nonce: "n", HostNoise: "hn", ControllerNoise: "cn"}, nil
}

func (a *allowOnce) SessionOpen(context.Context, string) (bool, error) { return true, nil }
