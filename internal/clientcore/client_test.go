package clientcore

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyandesk/internal/api"
	"cyandesk/internal/boot"
)

func TestClientTrustLoginAndEnroll(t *testing.T) {
	dir := t.TempDir()
	env, err := boot.Open(dir, []string{"127.0.0.1", "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.DB.Close() })
	var logs strings.Builder
	srv, err := api.New(env.DB, env.Material, env.Artifacts, log.New(&logs, "", 0))
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
	defer ln.Close()
	go http.Serve(ln, srv.PublicHandler())

	token, err := os.ReadFile(env.BootstrapFile)
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := t.TempDir()
	p := Profile{ServerURL: "https://" + ln.Addr().String(), Fingerprint: env.Material.CertFingerprint}
	if err := p.Save(cfgDir); err != nil {
		t.Fatal(err)
	}
	wrong := p
	wrong.Fingerprint = strings.Repeat("ab", 32)
	if err := wrong.Bootstrap(strings.TrimSpace(string(token)), "ada", "ada-password-ok"); err == nil {
		t.Fatal("mismatched fingerprint was accepted")
	}
	if err := p.Bootstrap(strings.TrimSpace(string(token)), "ada", "ada-password-ok"); err != nil {
		t.Fatal(err)
	}
	if err := p.Login("ada", "ada-password-ok"); err != nil {
		t.Fatal(err)
	}
	if err := p.Enroll("desk", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	if err := p.Approve(p.DeviceID, p.DeviceFP); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(cfgDir); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	devs, err := loaded.Devices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 || devs[0]["trust_state"] != "trusted" {
		t.Fatalf("devices %#v", devs)
	}
	info, err := os.Stat(filepath.Join(cfgDir, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode %v", info.Mode().Perm())
	}
	if strings.Contains(logs.String(), "ada-password-ok") || strings.Contains(logs.String(), strings.TrimSpace(string(token))) {
		t.Fatal("server log contains a secret")
	}
}
