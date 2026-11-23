package gateway

import (
	"context"
	"crypto/hmac"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyandesk/internal/e2ee"
	"cyandesk/internal/protocol"
)

func TestRustGatewayRelaysCiphertextAndUDP(t *testing.T) {
	bin := buildRustGateway(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/internal/tickets/redeem", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		body, _ := ioRead(r)
		if !strings.Contains(string(body), `"ticket":"good"`) {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_id":"s1","bytes_limit":1000000,"host_id":"h","controller_id":"c","scopes":["view"],"nonce":"n","host_noise":"hn","controller_noise":"cn"}`))
	})
	mux.HandleFunc("GET /api/v1/internal/sessions/s1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"state":"active"}`))
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, mux)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := gw.Addr().String()
	gw.Close()
	cmd := exec.Command(bin, "--listen", addr, "--internal-url", "http://"+ln.Addr().String(), "--internal-token-file", tokenFile)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	bad, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteFrame(bad, protocol.Frame{Type: protocol.TypeHello, Payload: protocol.EncodeHello(protocol.RoleHost, "nope")}); err != nil {
		t.Fatal(err)
	}
	ok, err := ReadAck(bad)
	if err != nil || ok {
		t.Fatalf("rust gateway accepted a bad ticket ok=%v err=%v", ok, err)
	}
	bad.Close()

	hostKey, _ := e2ee.GenerateKey()
	ctrlKey, _ := e2ee.GenerateKey()
	pro := e2ee.Prologue("s1", "n", "view", "hn", "cn")
	got := make(chan string, 1)
	go func() {
		c, err := Dial(addr, protocol.RoleHost, "good")
		if err != nil {
			t.Errorf("host dial %v", err)
			return
		}
		defer c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := c.WaitReady(ctx); err != nil {
			t.Errorf("host ready %v", err)
			return
		}
		sess, err := e2ee.Handshake(c, false, hostKey, nil, pro)
		if err != nil {
			t.Errorf("host hs %v", err)
			return
		}
		msg, err := sess.Recv(nil)
		if err != nil {
			t.Errorf("host recv %v", err)
			return
		}
		got <- string(msg)
	}()
	c, err := Dial(addr, protocol.RoleController, "good")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
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
			t.Fatalf("tcp relay got %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("rust tcp relay timed out")
	}

	udpHost, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer udpHost.Close()
	udpCtrl, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer udpCtrl.Close()
	gwUDP, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	hc, err := AssociateUDP(udpHost, gwUDP, protocol.RoleHost, "good")
	if err != nil {
		t.Fatal(err)
	}
	cc, err := AssociateUDP(udpCtrl, gwUDP, protocol.RoleController, "good")
	if err != nil {
		t.Fatal(err)
	}
	if len(hc) != 31 || len(cc) != 31 {
		t.Fatalf("cookie lengths %d %d", len(hc), len(cc))
	}
	if err := WriteUDPFrame(udpCtrl, gwUDP, cc, protocol.TypeRelay, []byte("udp-picture")); err != nil {
		t.Fatal(err)
	}
	typ, payload, err := ReadUDPFrame(udpHost, hc, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if typ != protocol.TypeRelay || string(payload) != "udp-picture" {
		t.Fatalf("udp relay %d %q", typ, payload)
	}
	// A packet with a wrong cookie must not be forwarded.
	badCookie := append([]byte(nil), cc...)
	badCookie[0] ^= 0xff
	_ = WriteUDPFrame(udpCtrl, gwUDP, badCookie, protocol.TypeRelay, []byte("nope"))
	udpHost.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 256)
	if _, _, err := udpHost.ReadFromUDP(buf); err == nil && hmac.Equal(buf[1:32], hc) {
		t.Fatal("gateway forwarded a packet with a bad cookie")
	}
}

func ioRead(r *http.Request) ([]byte, error) { return io.ReadAll(r.Body) }

func buildRustGateway(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "gateway-rs")
	bin := filepath.Join(root, "target", "debug", "cyandesk-gateway")
	cmd := exec.Command("cargo", "build", "--manifest-path", filepath.Join(root, "Cargo.toml"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return bin
}
