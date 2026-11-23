package mobile

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cyandesk/internal/api"
	"cyandesk/internal/boot"
	"cyandesk/internal/clientcore"
	"cyandesk/internal/media"
	"cyandesk/internal/protocol"
)

func TestAndroidEntrypointCarriesH264ThroughBuiltServer(t *testing.T) {
	if RoleHost != int(protocol.RoleHost) || RoleController != int(protocol.RoleController) {
		t.Fatal("mobile roles drifted from the protocol")
	}
	env, publicURL, _, gwAddr := startBuilt(t)
	pass := randomSecret(t)
	admin := clientcore.Profile{ServerURL: publicURL, Fingerprint: env.Material.CertFingerprint}
	token, err := os.ReadFile(env.BootstrapFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Bootstrap(strings.TrimSpace(string(token)), "android-lab", pass); err != nil {
		t.Fatal(err)
	}

	host, err := Connect(publicURL, env.Material.CertFingerprint, "android-lab", pass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Enroll("android-host"); err != nil {
		t.Fatal(err)
	}
	if err := host.Approve(host.DeviceID(), host.Fingerprint()); err != nil {
		t.Fatal(err)
	}
	ctrl, err := Connect(publicURL, env.Material.CertFingerprint, "android-lab", pass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.Enroll("android-controller"); err != nil {
		t.Fatal(err)
	}
	if err := host.Approve(ctrl.DeviceID(), ctrl.Fingerprint()); err != nil {
		t.Fatal(err)
	}
	if err := host.Grant(host.DeviceID(), ctrl.DeviceID(), "control", false); err != nil {
		t.Fatal(err)
	}
	hostFP, err := ctrl.PeerFingerprintOfDevice(host.DeviceID())
	if err != nil {
		t.Fatal(err)
	}
	ctrlFP, err := host.PeerFingerprintOfDevice(ctrl.DeviceID())
	if err != nil {
		t.Fatal(err)
	}

	sessionID, err := ctrl.Request(host.DeviceID(), "control")
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := host.Incoming()
	if err != nil || incoming != sessionID {
		t.Fatalf("incoming %q err %v", incoming, err)
	}
	seen, err := host.PeerFingerprint(sessionID)
	if err != nil || !strings.EqualFold(seen, ctrlFP) {
		t.Fatalf("peer fingerprint was not the enrolled controller")
	}
	if err := host.Consent(sessionID, "control", true); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.WaitActive(sessionID, 5000); err != nil {
		t.Fatal(err)
	}
	allow, err := host.AllowsInput(sessionID)
	if err != nil || !allow {
		t.Fatalf("control ticket did not grant input: %v %v", allow, err)
	}
	if _, err := ctrl.Dial(RoleController, sessionID, strings.Repeat("ab", 32)); err == nil {
		t.Fatal("a fingerprint the user did not confirm was accepted")
	}

	var hostLink, ctrlLink *Link
	var hostErr, ctrlErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		hostLink, hostErr = host.Dial(RoleHost, sessionID, ctrlFP)
	}()
	go func() {
		defer wg.Done()
		ctrlLink, ctrlErr = ctrl.Dial(RoleController, sessionID, hostFP)
	}()
	wg.Wait()
	if hostErr != nil || ctrlErr != nil {
		t.Fatalf("dial host %v ctrl %v", hostErr, ctrlErr)
	}
	defer hostLink.Close()
	defer ctrlLink.Close()
	if hostLink.Path() == "" || ctrlLink.Path() == "" {
		t.Fatal("dial did not select a path")
	}

	annex := redAnnex(t)
	if err := hostLink.SendVideo(7, 16, 16, annex); err != nil {
		t.Fatal(err)
	}
	pkt := recvKind(t, ctrlLink, protocol.AppVideo, 15*time.Second)
	if pkt.Seq != 7 || pkt.W != 16 || pkt.H != 16 || !bytes.Equal(pkt.Annex, annex) {
		t.Fatalf("video payload changed across the server: seq %d %dx%d %d bytes", pkt.Seq, pkt.W, pkt.H, len(pkt.Annex))
	}
	rgb, err := media.DecodeH264(pkt.Annex, 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	if !mostlyRed(rgb) {
		t.Fatal("decoded frame is not the encoded red frame")
	}
	if err := ctrlLink.SendPointer(3, 8, 9); err != nil {
		t.Fatal(err)
	}
	click := recvKind(t, hostLink, protocol.AppInput, 10*time.Second)
	if click.Seq != 3 || click.X != 8 || click.Y != 9 {
		t.Fatalf("pointer changed: %+v", click)
	}

	if err := ctrl.CloseSession(sessionID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		ok, err := host.Active(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed session still active")
		}
		time.Sleep(100 * time.Millisecond)
	}

	viewID, err := ctrl.Request(host.DeviceID(), "view")
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Consent(viewID, "view", true); err != nil {
		t.Fatal(err)
	}
	if err := host.WaitActive(viewID, 5000); err != nil {
		t.Fatal(err)
	}
	viewInput, err := host.AllowsInput(viewID)
	if err != nil || viewInput {
		t.Fatalf("view ticket granted input: %v %v", viewInput, err)
	}
	t.Logf("encrypted session used gateway %s", gwAddr)
}

func (a *Agent) PeerFingerprintOfDevice(id string) (string, error) {
	return a.fingerprint(id)
}

func recvKind(t *testing.T, link *Link, kind byte, d time.Duration) *Packet {
	t.Helper()
	got := make(chan *Packet, 1)
	fail := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			pkt, err := link.Recv()
			if err != nil {
				fail <- err
				return
			}
			if pkt.Kind == int(kind) {
				got <- pkt
				return
			}
		}
		fail <- os.ErrDeadlineExceeded
	}()
	select {
	case pkt := <-got:
		return pkt
	case err := <-fail:
		t.Fatal(err)
	case <-time.After(d + time.Second):
		t.Fatal("timed out waiting for a decrypted packet")
	}
	return nil
}

func redAnnex(t *testing.T) []byte {
	t.Helper()
	rgb := make([]byte, 16*16*3)
	for i := 0; i < len(rgb); i += 3 {
		rgb[i] = 220
		rgb[i+1] = 10
		rgb[i+2] = 10
	}
	annex, err := media.EncodeH264(rgb, 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(annex) < 5 {
		t.Fatal("encoder returned an empty stream")
	}
	return annex
}

func mostlyRed(rgb []byte) bool {
	if len(rgb) < 3 {
		return false
	}
	red, other := 0, 0
	for i := 0; i+2 < len(rgb); i += 3 {
		if rgb[i] > 150 && rgb[i] > rgb[i+1] && rgb[i] > rgb[i+2] {
			red++
		} else {
			other++
		}
	}
	return red > other
}

func randomSecret(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

func startBuilt(t *testing.T) (*boot.Env, string, string, string) {
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
	gwAddr := "127.0.0.1:24110"
	lnTry, err := net.Listen("tcp", gwAddr)
	if err != nil {
		gwAddr = "127.0.0.1:24111"
		lnTry, err = net.Listen("tcp", gwAddr)
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = lnTry.Close()
	apiSrv.GatewayTCP = gwAddr
	apiSrv.GatewayUDP = gwAddr
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
	iln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = iln.Close() })
	go func() { _ = http.Serve(iln, apiSrv.InternalHandler()) }()
	internalURL := "http://" + iln.Addr().String()
	bin := buildRustGateway(t)
	tok := filepath.Join(t.TempDir(), "internal.token")
	if err := os.WriteFile(tok, []byte(env.Material.InternalToken), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--listen", gwAddr, "--internal-url", internalURL, "--internal-token-file", tok)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", gwAddr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(stderr.String(), "listening") && time.Now().After(deadline) {
		t.Fatalf("gateway did not listen: %s", scrub(stderr.String(), env.Material.InternalToken))
	}
	return env, "https://" + ln.Addr().String(), internalURL, gwAddr
}

func buildRustGateway(t *testing.T) string {
	t.Helper()
	dir := "/tmp/cyandesk-gw-target"
	bin := filepath.Join(dir, "release", "cyandesk-gateway")
	cmd := exec.Command("cargo", "build", "--release", "--manifest-path", filepath.Join("..", "gateway-rs", "Cargo.toml"), "--target-dir", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rust gateway build: %v\n%s", err, out)
	}
	return bin
}

func scrub(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "[redacted]")
}
