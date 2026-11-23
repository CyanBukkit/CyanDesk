//go:build linux

package desktop

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"cyandesk/internal/api"
	"cyandesk/internal/boot"
	"cyandesk/internal/capture"
	"cyandesk/internal/clientcore"
	"cyandesk/internal/e2ee"
	"cyandesk/internal/gateway"
	"cyandesk/internal/input"
	"cyandesk/internal/protocol"
	"cyandesk/internal/remote"
)

func TestContinuousRemoteDesktop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	hostDisplay, stopHost := startXvfb(t, "320x240x24")
	defer stopHost()
	viewDisplay, stopView := startXvfb(t, "320x240x24")
	defer stopView()
	binDir := t.TempDir()
	cycler := filepath.Join(binDir, "cycler")
	viewer := filepath.Join(binDir, "cyandesk-view")
	buildX(t, filepath.Join("..", "..", "packaging", "linux", "color-cycler.c"), cycler)
	buildX(t, filepath.Join("..", "..", "packaging", "linux", "cyandesk-view.c"), viewer)
	startX(t, cycler, hostDisplay)
	viewCmd, viewIn := startViewer(t, viewer, viewDisplay)
	defer func() {
		_ = viewIn.Close()
		_ = viewCmd.Process.Kill()
	}()

	env, srv := startControl(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	gw := gateway.New(gateway.HTTPVerifier{BaseURL: srv.internal, Token: env.Material.InternalToken})
	gw.RecheckEvery = 200 * time.Millisecond
	go func() { _ = gw.Serve(ln) }()

	pass := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(pass, []byte("session-password-ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := clientcore.Profile{ServerURL: srv.publicURL, Fingerprint: env.Material.CertFingerprint}
	token, err := os.ReadFile(env.BootstrapFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Bootstrap(strings.TrimSpace(string(token)), "hostuser", "session-password-ok"); err != nil {
		t.Fatal(err)
	}
	if err := host.Login("hostuser", "session-password-ok"); err != nil {
		t.Fatal(err)
	}
	if err := host.Enroll("office-host", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	if err := host.Approve(host.DeviceID, host.DeviceFP); err != nil {
		t.Fatal(err)
	}
	ctrl := host
	ctrl.DeviceID, ctrl.PublicKey, ctrl.PrivateKey = "", "", ""
	ctrl.NoisePublic, ctrl.NoisePrivate, ctrl.DeviceFP = "", "", ""
	if err := ctrl.Enroll("office-view", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	if err := host.Approve(ctrl.DeviceID, ctrl.DeviceFP); err != nil {
		t.Fatal(err)
	}
	if err := host.Grant(host.DeviceID, ctrl.DeviceID, []string{"view", "input"}, true); err != nil {
		t.Fatal(err)
	}
	sessionID, ticket, _, err := ctrl.CreateSession(host.DeviceID, []string{"view", "input"})
	if err != nil {
		t.Fatal(err)
	}
	if err := assertPeer(host, ctrl, ticket); err != nil {
		t.Fatal(err)
	}
	if err := CheckPeer(host.DeviceFP+"00", host.DeviceFP, "n", "n"); err != ErrIdentity {
		t.Fatal("a substituted fingerprint was not rejected")
	}

	gate := &input.Gate{AllowInput: true}
	sink := &countingClick{display: hostDisplay}
	var framesSent int
	var rejected int
	alive := func() bool {
		state, _, err := host.Session(sessionID)
		return err == nil && state == "active"
	}
	cfg := HostConfig{
		Gate: gate, GrantedInput: true, AllowUpgrade: false,
		Grab: func() ([]byte, int, int, error) {
			rgb, err := capture.GrabRGB(hostDisplay, 320, 240)
			return rgb, 320, 240, err
		},
		Inject: sink, Alive: alive, Interval: 200 * time.Millisecond,
		Queue: NewVideoQueue(2), FramesSent: &framesSent, InputsRejected: &rejected,
	}
	runHost := func(ticket string) <-chan error {
		ch := make(chan error, 1)
		go func() {
			link, err := openSide(ctx, ln.Addr().String(), protocol.RoleHost, ticket, host, ctrl)
			if err != nil {
				ch <- err
				return
			}
			ch <- ServeHost(ctx, link, cfg)
		}()
		return ch
	}
	local, err := capture.GrabRGB(hostDisplay, 320, 240)
	if err != nil {
		t.Fatal(err)
	}
	if classify(local) == "" {
		nonzero := 0
		for _, b := range local {
			if b != 0 {
				nonzero++
			}
		}
		t.Fatalf("display %s grab class empty, nonzero=%d first=%v", hostDisplay, nonzero, local[:12])
	}
	hostDone := runHost(ticket)
	clink, err := openSide(ctx, ln.Addr().String(), protocol.RoleController, ticket, ctrl, host)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayTicket(ln.Addr().String(), ticket); err == nil {
		t.Fatal("a redeemed relay ticket was accepted again")
	}

	colors := map[string]int{}
	var lastClass string
	var shown bool
	var msgs int
	var lastKind byte
	var decodeErr string
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) && len(colors) < 2 {
		kind, payload, err := clink.Recv()
		if err != nil {
			t.Fatal(err)
		}
		msgs++
		lastKind = kind
		seq, rgb, _, _, ok, err := DecodeDeliveredFrame(kind, payload)
		if err != nil {
			decodeErr = err.Error()
			continue
		}
		if !ok {
			continue
		}
		class := classify(rgb)
		if class == "" {
			continue
		}
		colors[class]++
		lastClass = class
		writeViewerFrame(t, viewIn, rgb)
		if seq > 1 {
			shown = true
		}
	}
	if len(colors) < 2 || !shown {
		t.Fatalf("remote frames did not change over time: %v shown=%v msgs=%d kind=%d sent=%d decode=%s", colors, shown, msgs, lastKind, framesSent, decodeErr)
	}
	time.Sleep(300 * time.Millisecond)
	shot, err := capture.GrabRGB(viewDisplay, 320, 240)
	if err != nil {
		t.Fatal(err)
	}
	if classify(shot) == "" {
		t.Fatal("viewer window did not show a remote frame")
	}
	savePNG(t, shot, 320, 240)

	before := sink.n
	if err := clink.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 1, Kind: 2, X: 40, Y: 40, Button: 1})); err != nil {
		t.Fatal(err)
	}
	if !waitColor(t, clink, viewIn, "yellow", 6*time.Second) {
		t.Fatalf("authorized click did not change the remote screen, injections=%d", sink.n)
	}
	if sink.n != before+1 {
		t.Fatalf("injections=%d want %d", sink.n, before+1)
	}

	clink.Close()
	select {
	case <-hostDone:
	case <-time.After(5 * time.Second):
		t.Fatal("host did not notice the dropped path")
	}
	stateTicket, again, err := ctrl.SessionTicket(sessionID)
	if err != nil || stateTicket != "active" || again == "" || again == ticket {
		t.Fatal("reconnect did not receive a fresh ticket")
	}
	_, same, err := host.SessionTicket(sessionID)
	if err != nil || same != again {
		t.Fatal("host and controller were given different reconnect tickets")
	}
	hostDone = runHost(again)
	clink, err = openSide(ctx, ln.Addr().String(), protocol.RoleController, again, ctrl, host)
	if err != nil {
		t.Fatal(err)
	}
	defer clink.Close()
	held := sink.n
	if err := clink.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 1, Kind: 2, X: 30, Y: 30, Button: 1})); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	drain(clink, 300*time.Millisecond, viewIn)
	if sink.n != held {
		t.Fatalf("replayed input seq was injected again, count=%d", sink.n)
	}
	if err := clink.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 2, Kind: 2, X: 50, Y: 50, Button: 1})); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	drain(clink, 200*time.Millisecond, viewIn)
	if sink.n != held+1 {
		t.Fatalf("new input after reconnect was not delivered once, count=%d", sink.n)
	}
	if err := clink.Send(protocol.AppPolicy, []byte{PolicyView}); err != nil {
		t.Fatal(err)
	}
	if !waitAck(t, clink, viewIn, PolicyView, 4*time.Second) {
		t.Fatal("host did not acknowledge view-only")
	}
	held = sink.n
	if err := clink.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 3, Kind: 2, X: 40, Y: 40, Button: 1})); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	drain(clink, 300*time.Millisecond, viewIn)
	if sink.n != held {
		t.Fatalf("view-only still injected, count %d to %d", held, sink.n)
	}
	if err := clink.Send(protocol.AppPolicy, []byte{PolicyControl}); err != nil {
		t.Fatal(err)
	}
	if !waitReject(t, clink, viewIn, 4*time.Second) {
		t.Fatal("host accepted a control upgrade without permission")
	}
	if sink.n != held {
		t.Fatal("upgrade attempt injected input")
	}

	if err := ctrl.CloseSession(sessionID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-hostDone:
		if err != nil && err != ErrRevoked && ctx.Err() == nil {
			t.Logf("host ended: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("revocation did not stop the host")
	}
	stopped := sink.n
	_ = clink.Send(protocol.AppInput, EncodeInput(input.Event{Epoch: 1, Seq: 4, Kind: 2, X: 10, Y: 10, Button: 1}))
	time.Sleep(300 * time.Millisecond)
	if sink.n != stopped {
		t.Fatal("input was injected after revocation")
	}
	logPath := filepath.Join(t.TempDir(), "desktop-session.log")
	text := "frames_sent=" + strconv.Itoa(framesSent) + " colors=" + strconv.Itoa(len(colors)) + " last=" + lastClass + " injections=" + strconv.Itoa(sink.n) + " rejected=" + strconv.Itoa(rejected) + " path=tcp-localhost replay=rejected reconnect=no-duplicate-input revoke=stopped\n"
	if err := os.WriteFile(logPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "session-password") || strings.Contains(text, strings.TrimSpace(string(token))) {
		t.Fatal("log contains a credential")
	}
	_ = os.MkdirAll("/opt/cursor/artifacts/cyandesk", 0o755)
	_ = os.WriteFile("/opt/cursor/artifacts/cyandesk/desktop-session.log", []byte(text), 0o644)
	t.Log(strings.TrimSpace(text))
}

func openSide(ctx context.Context, gatewayAddr string, role byte, ticket string, self, peer clientcore.Profile) (*remote.Result, error) {
	key, err := self.NoiseKey()
	if err != nil {
		return nil, err
	}
	var peerStatic []byte
	if role == protocol.RoleController {
		other, err := peer.NoiseKey()
		if err != nil {
			return nil, err
		}
		peerStatic = other.Public
	}
	return remote.Open(ctx, remote.Side{
		Gateway: gatewayAddr, Role: role, Ticket: ticket, Static: key, PeerStatic: peerStatic,
		Prologue: e2ee.Prologue("desktop", "n", "view,input", "h", "c"),
	})
}

func assertPeer(host, ctrl clientcore.Profile, ticket string) error {
	hn, cn, err := TicketNoise(ticket)
	if err != nil {
		return err
	}
	hrec, err := host.Device(host.DeviceID)
	if err != nil {
		return err
	}
	crec, err := ctrl.Device(ctrl.DeviceID)
	if err != nil {
		return err
	}
	if err := CheckPeer(host.DeviceFP, hrec["fingerprint"].(string), hn, hrec["noise_public_key"].(string)); err != nil {
		return err
	}
	return CheckPeer(ctrl.DeviceFP, crec["fingerprint"].(string), cn, crec["noise_public_key"].(string))
}

func replayTicket(addr, ticket string) error {
	c, err := gateway.Dial(addr, protocol.RoleController, ticket)
	if err != nil {
		return err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return c.WaitReady(ctx)
}

type countingClick struct {
	mu      sync.Mutex
	n       int
	display string
}

func (c *countingClick) Inject(ev input.Event) error {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	cmd := exec.Command("xdotool", "mousemove", "--sync", strconv.Itoa(int(ev.X)), strconv.Itoa(int(ev.Y)), "click", "1")
	cmd.Env = capture.WithDisplay(c.display)
	return cmd.Run()
}

func waitColor(t *testing.T, link *remote.Result, view io.Writer, want string, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		kind, payload, err := link.Recv()
		if err != nil {
			continue
		}
		_, rgb, _, _, ok, err := DecodeDeliveredFrame(kind, payload)
		if err != nil || !ok {
			continue
		}
		writeViewerFrame(t, view, rgb)
		if classify(rgb) == want {
			return true
		}
	}
	return false
}

func waitAck(t *testing.T, link *remote.Result, view io.Writer, policy byte, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		kind, payload, err := link.Recv()
		if err != nil {
			return false
		}
		if kind == protocol.AppInputAck && len(payload) > 0 && payload[0] == policy {
			return true
		}
		if _, rgb, _, _, ok, _ := DecodeDeliveredFrame(kind, payload); ok {
			writeViewerFrame(t, view, rgb)
		}
	}
	return false
}

func waitReject(t *testing.T, link *remote.Result, view io.Writer, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		kind, payload, err := link.Recv()
		if err != nil {
			return false
		}
		if kind == protocol.AppReject {
			return true
		}
		if _, rgb, _, _, ok, _ := DecodeDeliveredFrame(kind, payload); ok {
			writeViewerFrame(t, view, rgb)
		}
	}
	return false
}

func drain(link *remote.Result, d time.Duration, view io.Writer) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		kind, payload, err := link.Recv()
		if err != nil {
			return
		}
		if _, rgb, _, _, ok, _ := DecodeDeliveredFrame(kind, payload); ok {
			var hdr [4]byte
			binary.BigEndian.PutUint32(hdr[:], uint32(len(rgb)))
			_, _ = view.Write(hdr[:])
			_, _ = view.Write(rgb)
		}
	}
}

func writeViewerFrame(t *testing.T, w io.Writer, rgb []byte) {
	t.Helper()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(rgb)))
	if _, err := w.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(rgb); err != nil {
		t.Fatal(err)
	}
}

func classify(rgb []byte) string {
	if len(rgb) < 320*240*3 {
		return ""
	}
	counts := map[string]int{}
	w := 320
	for _, pt := range [][2]int{{8, 8}, {160, 16}, {16, 120}, {300, 200}} {
		i := (pt[1]*w + pt[0]) * 3
		name := classifyPixel(int(rgb[i]), int(rgb[i+1]), int(rgb[i+2]))
		if name != "" {
			counts[name]++
		}
	}
	best, n := "", 0
	for name, c := range counts {
		if c > n {
			best, n = name, c
		}
	}
	return best
}

func classifyPixel(r, g, b int) string {
	switch {
	case r > 180 && g > 180 && b < 90:
		return "yellow"
	case r > 150 && g < 100 && b < 100:
		return "red"
	case g > 100 && r < 100 && b < 100:
		return "green"
	case b > 100 && r < 100 && g < 140:
		return "blue"
	default:
		return ""
	}
}

func savePNG(t *testing.T, rgb []byte, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			o := img.PixOffset(x, y)
			img.Pix[o] = rgb[i]
			img.Pix[o+1] = rgb[i+1]
			img.Pix[o+2] = rgb[i+2]
			img.Pix[o+3] = 255
		}
	}
	path := "/opt/cursor/artifacts/cyandesk/remote-desktop.png"
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

var displaySeq int

func nextDisplay() int {
	displaySeq++
	return displaySeq
}

func startXvfb(t *testing.T, geom string) (string, func()) {
	t.Helper()
	for attempt := 0; attempt < 15; attempt++ {
		display := fmt.Sprintf(":%d", 410+os.Getpid()%200+nextDisplay()+attempt*17)
		cmd := exec.Command("Xvfb", display, "-screen", "0", geom)
		if err := cmd.Start(); err != nil {
			continue
		}
		time.Sleep(200 * time.Millisecond)
		if err := exec.Command("xwininfo", "-display", display, "-root").Run(); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			continue
		}
		return display, func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
	}
	t.Fatal("Xvfb did not stay up")
	return "", func() {}
}

func buildX(t *testing.T, src, out string) {
	t.Helper()
	cmd := exec.Command("gcc", "-o", out, src, "-lX11")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile %s: %v %s", src, err, b)
	}
}

func startX(t *testing.T, bin, display string) {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = capture.WithDisplay(display)
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	time.Sleep(500 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("cycler exited on %s: %s", display, errb.String())
	}
}

func startViewer(t *testing.T, bin, display string) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	cmd := exec.Command(bin, "320", "240")
	cmd.Env = capture.WithDisplay(display)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	return cmd, in
}

type controlBox struct {
	publicURL string
	internal  string
}

func startControl(t *testing.T) (*boot.Env, controlBox) {
	t.Helper()
	dir := t.TempDir()
	env, err := boot.Open(dir, []string{"127.0.0.1", "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.DB.Close() })
	apiSrv, err := api.New(env.DB, env.Material, env.Artifacts, log.New(os.Stderr, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair([]byte(env.Material.CertPEM), []byte(env.Material.KeyPEM))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = http.Serve(ln, apiSrv.PublicHandler()) }()
	internal := "127.0.0.1:0"
	iln, err := net.Listen("tcp", internal)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = iln.Close() })
	go func() { _ = http.Serve(iln, apiSrv.InternalHandler()) }()
	return env, controlBox{publicURL: "https://" + ln.Addr().String(), internal: "http://" + iln.Addr().String()}
}
