package gateway

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"cyandesk/internal/e2ee"
	"cyandesk/internal/protocol"
)

type memVerify struct {
	mu     sync.Mutex
	used   map[string]int
	open   map[string]bool
	redeem int
}

func (m *memVerify) Redeem(_ context.Context, ticket string) (Claims, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.used == nil {
		m.used = map[string]int{}
		m.open = map[string]bool{}
	}
	if ticket != "good" {
		return Claims{}, ErrDenied
	}
	m.used[ticket]++
	m.redeem++
	c := Claims{SessionID: "s1", HostID: "h", ControllerID: "c", BytesLimit: 1 << 20, Nonce: "n", HostNoise: "hn", ControllerNoise: "cn"}
	m.open[c.SessionID] = true
	return c, nil
}

func (m *memVerify) SessionOpen(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.open[id], nil
}

func TestRelayRequiresTicketAndCarriesCiphertext(t *testing.T) {
	v := &memVerify{}
	srv := New(v)
	srv.ForwardDelay = 20 * time.Millisecond
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = srv.Serve(ln) }()

	bad, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteFrame(bad, protocol.Frame{Type: protocol.TypeHello, Payload: protocol.EncodeHello(protocol.RoleHost, "nope")}); err != nil {
		t.Fatal(err)
	}
	ok, err := ReadAck(bad)
	if err != nil || ok {
		t.Fatalf("bad ticket ok=%v err=%v", ok, err)
	}
	bad.Close()

	hostKey, err := e2ee.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	ctrlKey, err := e2ee.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pro := e2ee.Prologue("s1", "n", "view", "hn", "cn")
	got := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		c, err := Dial(ln.Addr().String(), protocol.RoleHost, "good")
		if err != nil {
			errCh <- err
			return
		}
		defer c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := c.WaitReady(ctx); err != nil {
			errCh <- err
			return
		}
		sess, err := e2ee.Handshake(c, false, hostKey, nil, pro)
		if err != nil {
			errCh <- err
			return
		}
		msg, err := sess.Recv(nil)
		if err != nil {
			errCh <- err
			return
		}
		got <- string(msg)
	}()

	c, err := Dial(ln.Addr().String(), protocol.RoleController, "good")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	sess, err := e2ee.Handshake(c, true, ctrlKey, hostKey.Public, pro)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Send(nil, []byte("picture")); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		if msg != "picture" {
			t.Fatalf("host saw %q", msg)
		}
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ciphertext")
	}
	if time.Since(started) < srv.ForwardDelay {
		t.Fatal("ciphertext was not delayed by the relay")
	}
	if v.redeem != 1 {
		t.Fatalf("ticket redeemed %d times", v.redeem)
	}
}
