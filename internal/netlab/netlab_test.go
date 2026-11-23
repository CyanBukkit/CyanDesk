package netlab

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cyandesk/internal/e2ee"
	"cyandesk/internal/gateway"
	"cyandesk/internal/input"
	"cyandesk/internal/protocol"
)

// TestPrivateNetworkCandidates runs two processes in separate network namespaces
// with a delaying, lossy relay in between. It is a private lab network, not the public internet.
func TestPrivateNetworkCandidates(t *testing.T) {
	switch os.Getenv("CYANDESK_NET_ROLE") {
	case "router":
		if err := runRouter(); err != nil {
			t.Fatal(err)
		}
		return
	case "host":
		if err := runHost(); err != nil {
			t.Fatal(err)
		}
		return
	case "ctrl":
		if err := runCtrlBody(); err != nil {
			t.Fatal(err)
		}
		return
	}
	bin := buildGateway(t)
	self := os.Args[0]
	dir := t.TempDir()
	out := filepath.Join(dir, "result.json")
	script := `
set -e
ip link set lo up
ip link add va type veth peer name vb
ip link add vc type veth peer name vd
unshare -n bash -c 'ip link set lo up; sleep 40' &
pidA=$!
unshare -n bash -c 'ip link set lo up; sleep 40' &
pidB=$!
sleep 0.3
ip link set va netns $pidA
ip link set vc netns $pidB
nsenter -t $pidA -n ip link set va up
nsenter -t $pidA -n ip addr add 10.1.0.2/24 dev va
nsenter -t $pidA -n ip route add default via 10.1.0.1
nsenter -t $pidB -n ip link set vc up
nsenter -t $pidB -n ip addr add 10.2.0.2/24 dev vc
nsenter -t $pidB -n ip route add default via 10.2.0.1
ip link set vb up
ip link set vd up
ip addr add 10.1.0.1/24 dev vb
ip addr add 10.2.0.1/24 dev vd
echo 1 > /proc/sys/net/ipv4/ip_forward
CYANDESK_NET_ROLE=router CYANDESK_RESULT="$RESULT" "$BIN" -test.run '^TestPrivateNetworkCandidates$' -test.count=1 &
rp=$!
sleep 0.6
nsenter -t $pidA -n env CYANDESK_NET_ROLE=host CYANDESK_GW=10.1.0.1:13110 CYANDESK_RESULT="$RESULT" "$BIN" -test.run '^TestPrivateNetworkCandidates$' -test.count=1 &
hp=$!
sleep 0.5
nsenter -t $pidB -n env CYANDESK_NET_ROLE=ctrl CYANDESK_GW=10.1.0.1:13110 CYANDESK_RESULT="$RESULT" "$BIN" -test.run '^TestPrivateNetworkCandidates$' -test.count=1 &
cp=$!
wait $hp
wait $cp
kill $rp $pidA $pidB || true
`
	cmd := exec.Command("unshare", "-r", "-n", "bash", "-c", script)
	cmd.Env = append(os.Environ(), "BIN="+self, "RESULT="+out, "CYANDESK_GWBIN="+bin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var rep report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatalf("result %s: %v", raw, err)
	}
	t.Logf("private-network report selected=%s direct_rtt=%s relay_rtt=%s both=%v start_skew=%s injections=%d dups=%d udp_loss_delivered=%v",
		rep.Selected, rep.DirectRTT, rep.RelayRTT, rep.BothReady, rep.StartSkew, rep.Injections, rep.Dups, rep.LossDelivered)
	if !rep.BothReady {
		t.Fatalf("both candidates were not established: %+v", rep)
	}
	if rep.Selected != "direct-udp" {
		t.Fatalf("expected the lower-latency direct path, got %s (direct %s relay %s)", rep.Selected, rep.DirectRTT, rep.RelayRTT)
	}
	if rep.Dups != 1 {
		t.Fatalf("replayed input was not reported as a duplicate, dups=%d", rep.Dups)
	}
	if rep.Injections != 2 {
		t.Fatalf("injections %d", rep.Injections)
	}
	if !rep.LossDelivered {
		t.Fatal("lossy udp proxy delivered nothing after the forced drops")
	}
	if rep.Network != "private-namespace" {
		t.Fatal(rep.Network)
	}
	_ = os.MkdirAll("/opt/cursor/artifacts/cyandesk", 0o755)
	_ = os.WriteFile("/opt/cursor/artifacts/cyandesk/netlab.json", raw, 0o644)
}

type report struct {
	Network       string `json:"network"`
	Selected      string `json:"selected"`
	DirectRTT     string `json:"direct_rtt"`
	RelayRTT      string `json:"relay_rtt"`
	BothReady     bool   `json:"both_ready"`
	StartSkew     string `json:"start_skew"`
	Injections    int    `json:"injections"`
	Dups          int    `json:"dups"`
	LossDelivered bool   `json:"loss_delivered"`
}

func runHost() error {
	key, err := e2ee.GenerateKey()
	if err != nil {
		return err
	}
	if err := os.WriteFile(os.Getenv("CYANDESK_RESULT")+".hostkey", key.Public, 0o644); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := gateway.Dial(os.Getenv("CYANDESK_GW"), protocol.RoleHost, "good")
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.WaitReady(ctx); err != nil {
		return err
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("10.1.0.2")})
	if err != nil {
		return err
	}
	defer udp.Close()
	if err := c.WriteSignal([]byte("udp " + udp.LocalAddr().String())); err != nil {
		return err
	}
	pro := e2ee.Prologue("s1", "n", "view,input", "h", "c")
	errCh := make(chan error, 1)
	go func() {
		sess, err := e2ee.Handshake(c, false, key, nil, pro)
		if err != nil {
			errCh <- err
			return
		}
		msg, err := sess.Recv(nil)
		if err != nil {
			errCh <- err
			return
		}
		if len(msg) > 0 && msg[0] == 'P' {
			errCh <- sess.Send(nil, append([]byte{'A'}, msg[1:]...))
		}
	}()
	buf := make([]byte, 2048)
	_ = udp.SetReadDeadline(time.Now().Add(8 * time.Second))
	n, from, err := udp.ReadFromUDP(buf)
	if err != nil {
		return err
	}
	pipe := &udpPipe{conn: udp, peer: from, first: append([]byte(nil), buf[:n]...)}
	sess, err := e2ee.Handshake(pipe, false, key, nil, pro)
	if err != nil {
		return err
	}
	gate := &input.Gate{AllowInput: true}
	sink := &input.CountingSink{}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && sink.N < 2 {
		msg, err := sess.Recv(nil)
		if err != nil {
			return err
		}
		if len(msg) > 0 && msg[0] == 'P' {
			_ = sess.Send(nil, append([]byte{'A'}, msg[1:]...))
			continue
		}
		if len(msg) == 0 || msg[0] != 'I' {
			continue
		}
		seq, _ := strconv.ParseUint(string(msg[1:]), 10, 64)
		before := sink.N
		_ = gate.Apply(input.Event{Epoch: 1, Seq: seq, Kind: 2}, sink)
		if sink.N == before {
			_ = sess.Send(nil, []byte("dup"))
		} else {
			_ = sess.Send(nil, []byte("ack"))
		}
	}
	b, _ := json.Marshal(map[string]int{"injections": sink.N})
	return os.WriteFile(os.Getenv("CYANDESK_RESULT")+".host", b, 0o644)
}

func runCtrlBody() error {
	deadline := time.Now().Add(5 * time.Second)
	var pub []byte
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(os.Getenv("CYANDESK_RESULT") + ".hostkey")
		if err == nil && len(b) == 32 {
			pub = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(pub) != 32 {
		return errString("missing host key")
	}
	key, err := e2ee.GenerateKey()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := time.Now()
	c, err := gateway.Dial(os.Getenv("CYANDESK_GW"), protocol.RoleController, "good")
	if err != nil {
		return err
	}
	defer c.Close()
	relayTCP := time.Now()
	if err := c.WaitReady(ctx); err != nil {
		return err
	}
	pro := e2ee.Prologue("s1", "n", "view,input", "h", "c")
	type sample struct {
		name string
		rtt  time.Duration
		at   time.Time
		sess *e2ee.Session
	}
	ch := make(chan sample, 2)
	go func() {
		sess, err := e2ee.Handshake(c, true, key, pub, pro)
		if err != nil {
			return
		}
		nonce := []byte("Prelay")
		t0 := time.Now()
		if err := sess.Send(nil, nonce); err != nil {
			return
		}
		msg, err := sess.Recv(nil)
		if err != nil || string(msg) != "Arelay" {
			return
		}
		ch <- sample{name: "relay-tcp", rtt: time.Since(t0), at: time.Now(), sess: sess}
	}()
	var udpAddr string
	sigCtx, sigCancel := context.WithTimeout(ctx, 5*time.Second)
	sig, err := c.ReadSignal(sigCtx)
	sigCancel()
	if err != nil {
		return err
	}
	udpAddr = string(sig)
	if len(udpAddr) > 4 && udpAddr[:4] == "udp " {
		udpAddr = udpAddr[4:]
	}
	peer, err := net.ResolveUDPAddr("udp", udpAddr)
	if err != nil {
		return err
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("10.2.0.2")})
	if err != nil {
		return err
	}
	defer udp.Close()
	go func() {
		pipe := &udpPipe{conn: udp, peer: peer}
		sess, err := e2ee.Handshake(pipe, true, key, pub, pro)
		if err != nil {
			return
		}
		nonce := []byte("Pdirect")
		t0 := time.Now()
		if err := sess.Send(nil, nonce); err != nil {
			return
		}
		msg, err := sess.Recv(nil)
		if err != nil || len(msg) == 0 || msg[0] != 'A' {
			return
		}
		ch <- sample{name: "direct-udp", rtt: time.Since(t0), at: time.Now(), sess: sess}
	}()
	var ready []sample
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for len(ready) < 2 {
		select {
		case s := <-ch:
			ready = append(ready, s)
		case <-timer.C:
			return errString("candidates timed out")
		}
	}
	direct, relay := ready[0], ready[1]
	for _, s := range ready {
		if s.name == "direct-udp" {
			direct = s
		}
		if s.name == "relay-tcp" {
			relay = s
		}
	}
	selected := direct
	if relay.at.Before(direct.at) && relay.rtt < direct.rtt {
		selected = relay
	}
	if direct.rtt <= relay.rtt {
		selected = direct
	}
	sess := selected.sess
	if _, err := roundInput(sess, "1"); err != nil {
		return err
	}
	dup, err := roundInput(sess, "1")
	if err != nil {
		return err
	}
	if _, err := roundInput(sess, "2"); err != nil {
		return err
	}
	loss := lossCheck()
	hostRaw, _ := os.ReadFile(os.Getenv("CYANDESK_RESULT") + ".host")
	var hostRep map[string]int
	_ = json.Unmarshal(hostRaw, &hostRep)
	rep := report{
		Network: "private-namespace", Selected: selected.name,
		DirectRTT: direct.rtt.String(), RelayRTT: relay.rtt.String(),
		BothReady: true, StartSkew: relayTCP.Sub(started).String(),
		Injections: hostRep["injections"], Dups: boolToInt(dup == "dup"), LossDelivered: loss,
	}
	b, _ := json.Marshal(rep)
	return os.WriteFile(os.Getenv("CYANDESK_RESULT"), b, 0o644)
}

func roundInput(sess *e2ee.Session, seq string) (string, error) {
	if err := sess.Send(nil, []byte("I"+seq)); err != nil {
		return "", err
	}
	msg, err := sess.Recv(nil)
	if err != nil {
		return "", err
	}
	return string(msg), nil
}

func lossCheck() bool {
	conn, err := net.DialUDP("udp", &net.UDPAddr{IP: net.ParseIP("10.2.0.2")}, &net.UDPAddr{IP: net.ParseIP("10.1.0.1"), Port: 24110})
	if err != nil {
		return false
	}
	defer conn.Close()
	got := 0
	buf := make([]byte, 32)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for i := 0; i < 4; i++ {
		_, _ = conn.Write([]byte{byte(i)})
		time.Sleep(30 * time.Millisecond)
	}
	for {
		_, err := conn.Read(buf)
		if err != nil {
			break
		}
		got++
	}
	return got > 0 && got < 4
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

type errString string

func (e errString) Error() string { return string(e) }

func ioReadAll(r *http.Request) ([]byte, error) { return io.ReadAll(r.Body) }

func stringsContains(s, sub string) bool { return strings.Contains(s, sub) }

func runRouter() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/internal/tickets/redeem", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioReadAll(r)
		if !stringsContains(string(body), `"ticket":"good"`) {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"session_id":"s1","bytes_limit":1000000}`))
	})
	mux.HandleFunc("/api/v1/internal/sessions/s1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"state":"active"}`))
	})
	cl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	go http.Serve(cl, mux)
	token := filepath.Join(os.TempDir(), "cyandesk-netlab-token")
	if err := os.WriteFile(token, []byte("test-token\n"), 0o600); err != nil {
		return err
	}
	gwBin := os.Getenv("CYANDESK_GWBIN")
	cmd := exec.Command(gwBin, "--listen", "127.0.0.1:23110", "--internal-url", "http://"+cl.Addr().String(), "--internal-token-file", token)
	if err := cmd.Start(); err != nil {
		return err
	}
	defer cmd.Process.Kill()
	time.Sleep(200 * time.Millisecond)
	pln, err := net.Listen("tcp", "10.1.0.1:13110")
	if err != nil {
		return err
	}
	go TCPProxy{Target: "127.0.0.1:23110", Delay: 25 * time.Millisecond}.Serve(pln)
	lossIn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("10.1.0.1"), Port: 24110})
	if err != nil {
		return err
	}
	lossOut, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("10.1.0.1"), Port: 24111})
	if err != nil {
		return err
	}
	go func() {
		b := make([]byte, 64)
		for {
			n, from, err := lossOut.ReadFromUDP(b)
			if err != nil {
				return
			}
			_, _ = lossOut.WriteToUDP(append([]byte(nil), b[:n]...), from)
		}
	}()
	go UDPProxy{Delay: 5 * time.Millisecond, DropFirst: 2}.Serve(lossIn, lossOut.LocalAddr().(*net.UDPAddr))
	result := os.Getenv("CYANDESK_RESULT")
	for i := 0; i < 40; i++ {
		if _, err := os.Stat(result); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errString("timed out waiting for peer result")
}

func buildGateway(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "gateway-rs")
	cmd := exec.Command("cargo", "build", "--manifest-path", filepath.Join(root, "Cargo.toml"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "target", "debug", "cyandesk-gateway")
}

type udpPipe struct {
	conn  *net.UDPConn
	peer  *net.UDPAddr
	first []byte
}

func (p *udpPipe) WriteMsg(b []byte) error {
	_, err := p.conn.WriteToUDP(b, p.peer)
	return err
}

func (p *udpPipe) ReadMsg() ([]byte, error) {
	if p.first != nil {
		b := p.first
		p.first = nil
		return b, nil
	}
	buf := make([]byte, 2048)
	_ = p.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := p.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), buf[:n]...), nil
}

func (p *udpPipe) Close() error { return p.conn.Close() }
