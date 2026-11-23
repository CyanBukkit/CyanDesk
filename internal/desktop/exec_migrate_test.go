//go:build linux

package desktop

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cyandesk/internal/capture"
	"cyandesk/internal/clientcore"
)

func TestExecutableAgentMigratesThroughRustGateway(t *testing.T) {
	hostDisplay, stopHost := startXvfb(t, "320x240x24")
	defer stopHost()
	viewDisplay, stopView := startXvfb(t, "320x240x24")
	defer stopView()
	dir := t.TempDir()
	bin := filepath.Join(dir, "cyandesk")
	view := filepath.Join(dir, "cyandesk-view")
	cycler := filepath.Join(dir, "cycler")
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, "../../cmd/cyandesk")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build client: %v %s", err, out)
	}
	buildX(t, "../../packaging/linux/cyandesk-view.c", view)
	buildX(t, "../../packaging/linux/color-cycler.c", cycler)
	startX(t, cycler, hostDisplay)
	gwBin := "../../gateway-rs/target/release/cyandesk-gateway"
	if _, err := os.Stat(gwBin); err != nil {
		t.Fatal(err)
	}

	proxyAddr, stopProxy, drop := startDropProxy(t)
	defer stopProxy()
	relayPublic := "127.0.0.1:23113"
	gwAddr := "127.0.0.1:23110"
	relayListen, err := net.Listen("tcp", relayPublic)
	if err != nil {
		t.Fatal(err)
	}

	data := filepath.Join(dir, "server")
	control := exec.Command(bin, "control", "--data", data, "--listen", "127.0.0.1:23111", "--internal", "127.0.0.1:23112", "--admin", "../../admin-web/dist", "--gateway-advertise", relayPublic, "--cert-host", "127.0.0.1")
	var controlLog logBuf
	control.Stdout = &controlLog
	control.Stderr = &controlLog
	if err := control.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = control.Process.Kill(); _ = control.Wait() }()
	if !waitFile(filepath.Join(data, "internal.token"), 5*time.Second) {
		t.Fatalf("control did not stay up: %s", controlLog.String())
	}
	if !waitLog(&controlLog, "cert fingerprint:", 5*time.Second) {
		t.Fatalf("control did not print a cert fingerprint: %s", controlLog.String())
	}
	st, err := os.Stat(filepath.Join(data, "bootstrap.token"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("bootstrap file mode %v", st.Mode().Perm())
	}
	fp := fingerprintFrom(controlLog.String())
	gateway := exec.Command(gwBin, "--listen", gwAddr, "--internal-url", "http://127.0.0.1:23112", "--internal-token-file", filepath.Join(data, "internal.token"))
	var gwLog logBuf
	gateway.Stdout = &gwLog
	gateway.Stderr = &gwLog
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gateway.Process.Kill(); _ = gateway.Wait() }()
	if !waitLog(&gwLog, "listening ", 5*time.Second) {
		t.Fatalf("gateway did not listen: %s", gwLog.String())
	}
	go serveDelay(relayListen, gwAddr, 25*time.Millisecond)

	pass := filepath.Join(dir, "password")
	if err := os.WriteFile(pass, []byte("session-password-ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hostDir := filepath.Join(dir, "host")
	ctrlDir := filepath.Join(dir, "ctrl")
	runCLI(t, bin, "client", "--config", hostDir, "server", "set", "https://127.0.0.1:23111")
	runCLI(t, bin, "client", "--config", hostDir, "server", "trust", fp)
	runCLI(t, bin, "client", "--config", hostDir, "bootstrap", "--user", "ada", "--password-file", pass, "--token-file", filepath.Join(data, "bootstrap.token"))
	runCLI(t, bin, "client", "--config", hostDir, "login", "--user", "ada", "--password-file", pass)
	hostOut := runCLI(t, bin, "client", "--config", hostDir, "enroll", "--name", "office-host")
	hostID, hostFP := parseEnroll(t, hostOut)
	runCLI(t, bin, "client", "--config", hostDir, "approve", hostID, "--fingerprint", hostFP)
	runCLI(t, bin, "client", "--config", ctrlDir, "server", "set", "https://127.0.0.1:23111")
	runCLI(t, bin, "client", "--config", ctrlDir, "server", "trust", fp)
	runCLI(t, bin, "client", "--config", ctrlDir, "login", "--user", "ada", "--password-file", pass)
	ctrlOut := runCLI(t, bin, "client", "--config", ctrlDir, "enroll", "--name", "office-view")
	ctrlID, ctrlFP := parseEnroll(t, ctrlOut)
	runCLI(t, bin, "client", "--config", hostDir, "approve", ctrlID, "--fingerprint", ctrlFP)
	runCLI(t, bin, "client", "--config", hostDir, "grant", "--host", hostID, "--controller", ctrlID, "--scope", "view,input", "--unattended")

	agent := exec.Command(bin, "agent", "--config", hostDir, "--display", hostDisplay, "--width", "320", "--height", "240", "--fps", "5", "--direct-advertise", proxyAddr)
	agent.Env = append(os.Environ(), "DISPLAY="+hostDisplay)
	var agentLog logBuf
	agent.Stdout = &agentLog
	agent.Stderr = &agentLog
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agent.Process.Kill(); _ = agent.Wait() }()

	connect := exec.Command(bin, "connect", "--config", ctrlDir, "--host", hostID, "--fingerprint", hostFP, "--mode", "control")
	connect.Env = append(os.Environ(), "DISPLAY="+viewDisplay, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var connectLog logBuf
	connect.Stdout = &connectLog
	connect.Stderr = &connectLog
	if err := connect.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connect.Process.Kill(); _ = connect.Wait() }()

	if !waitLog(&connectLog, "path direct-udp", 25*time.Second) {
		t.Fatalf("executable did not move onto direct udp\nconnect:\n%s\nagent:\n%s\ngateway:\n%s", connectLog.String(), agentLog.String(), gwLog.String())
	}
	if !waitColorDisplay(t, viewDisplay, 8*time.Second) {
		t.Fatalf("viewer stayed empty\n%s", connectLog.String())
	}
	drop.Store(true)
	if !waitLog(&connectLog, "path relay", 8*time.Second) {
		t.Fatalf("loss did not move the executable onto the relay\n%s", connectLog.String())
	}
	if !waitColorChange(t, viewDisplay, 4*time.Second) {
		t.Fatal("viewer stopped changing after the direct path was dropped")
	}
	click := exec.Command("xdotool", "mousemove", "40", "40", "click", "1")
	click.Env = capture.WithDisplay(viewDisplay)
	if err := click.Run(); err != nil {
		t.Fatal(err)
	}
	if !waitClass(t, viewDisplay, "yellow", 6*time.Second) {
		t.Fatal("click during relay did not reach the host")
	}
	drop.Store(false)
	if !waitLog(&connectLog, "path direct-udp", 16*time.Second) {
		t.Fatalf("direct path did not return\n%s", connectLog.String())
	}
	profile, err := clientcore.Load(ctrlDir)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := profile.Sessions()
	if err != nil || len(sessions) == 0 {
		t.Fatal(err)
	}
	sid, _ := sessions[0]["id"].(string)
	if err := profile.CloseSession(sid); err != nil {
		t.Fatal(err)
	}
	if !waitLog(&agentLog, "授权已撤销，会话结束", 6*time.Second) {
		t.Fatalf("revoke did not stop the agent session\n%s", agentLog.String())
	}
	if !strings.Contains(gwLog.String(), "tcp+udp") {
		t.Fatalf("gateway log %s", gwLog.String())
	}
}

func runCLI(t *testing.T, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func parseEnroll(t *testing.T, out string) (id, fp string) {
	t.Helper()
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && f[0] == "设备" {
			id = f[1]
		}
		if len(f) >= 2 && f[0] == "指纹" {
			fp = f[1]
		}
	}
	if id == "" || fp == "" {
		t.Fatalf("enroll output %q", out)
	}
	return id, fp
}

func serveDelay(ln net.Listener, target string, delay time.Duration) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			d, err := net.Dial("tcp", target)
			if err != nil {
				return
			}
			defer d.Close()
			go copyDelay(d, c, delay)
			copyDelay(c, d, delay)
		}(c)
	}
}

func copyDelay(dst, src net.Conn, delay time.Duration) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			time.Sleep(delay)
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func fingerprintFrom(log string) string {
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "cert fingerprint: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "cert fingerprint: "))
		}
	}
	return ""
}

func waitFile(path string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

type logBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func waitLog(l *logBuf, needle string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if strings.Contains(l.String(), needle) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func waitColorDisplay(t *testing.T, display string, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		rgb, err := capture.GrabRGB(display, 320, 240)
		if err == nil && classify(rgb) != "" {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func waitColorChange(t *testing.T, display string, d time.Duration) bool {
	t.Helper()
	first := ""
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		rgb, err := capture.GrabRGB(display, 320, 240)
		if err == nil {
			class := classify(rgb)
			if class != "" {
				if first == "" {
					first = class
				} else if class != first {
					return true
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

func waitClass(t *testing.T, display, want string, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		rgb, err := capture.GrabRGB(display, 320, 240)
		if err == nil && classify(rgb) == want {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func startDropProxy(t *testing.T) (string, func(), *atomic.Bool) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	drop := &atomic.Bool{}
	p := &dropProxy{conn: conn, drop: drop}
	go p.loop()
	return conn.LocalAddr().String(), func() { _ = conn.Close() }, drop
}

type dropProxy struct {
	conn *net.UDPConn
	drop *atomic.Bool
	mu   sync.Mutex
	host *net.UDPAddr
	ctrl *net.UDPAddr
}

func (p *dropProxy) loop() {
	buf := make([]byte, 65535)
	for {
		n, from, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		p.mu.Lock()
		if n == 1 && buf[0] == 0 {
			cp := *from
			p.host = &cp
			p.mu.Unlock()
			continue
		}
		var dest *net.UDPAddr
		if p.host != nil && from.String() == p.host.String() {
			dest = p.ctrl
		} else if p.host != nil {
			cp := *from
			p.ctrl = &cp
			dest = p.host
		}
		drop := p.drop.Load()
		p.mu.Unlock()
		if drop || dest == nil {
			continue
		}
		_, _ = p.conn.WriteToUDP(buf[:n], dest)
	}
}
