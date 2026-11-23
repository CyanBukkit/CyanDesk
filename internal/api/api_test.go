package api

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cyandesk/internal/e2ee"
	"cyandesk/internal/identity"
	"cyandesk/internal/secret"
	"cyandesk/internal/store"
)

type harness struct {
	srv      *Server
	public   *httptest.Server
	internal *httptest.Server
	logs     *bytes.Buffer
	token    string
	admin    string
	password string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "cyandesk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mat, _, err := secret.Generate([]string{"localhost", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := secret.RandomToken(24)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMeta("bootstrap_hash", secret.HashToken(raw)); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	srv, err := New(st, mat, filepath.Join(dir, "artifacts"), log.New(&logs, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	pub := httptest.NewTLSServer(srv.PublicHandler())
	in := httptest.NewServer(srv.InternalHandler())
	t.Cleanup(pub.Close)
	t.Cleanup(in.Close)
	return &harness{srv: srv, public: pub, internal: in, logs: &logs, token: raw, admin: "root-admin", password: "correct-horse-battery"}
}

func (h *harness) do(t *testing.T, method, path, bearer string, body any) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, h.public.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := h.public.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

func (h *harness) bootstrap(t *testing.T) {
	t.Helper()
	code, body := h.do(t, http.MethodPost, "/api/v1/admin/bootstrap", "", map[string]string{
		"token": h.token, "login_name": h.admin, "password": h.password,
	})
	if code != http.StatusCreated {
		t.Fatalf("bootstrap %d %v", code, body)
	}
}

func (h *harness) login(t *testing.T, user, pass string) string {
	t.Helper()
	code, body := h.do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"login_name": user, "password": pass,
	})
	if code != http.StatusOK {
		t.Fatalf("login %d %v", code, body)
	}
	tok, _ := body["access_token"].(string)
	if tok == "" {
		t.Fatalf("no access token: %v", body)
	}
	return tok
}

type deviceKeys struct {
	sign      ed25519.PrivateKey
	noisePriv []byte
	pub       string
	noise     string
}

func makeDevice(t *testing.T) deviceKeys {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	nk, err := e2ee.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return deviceKeys{
		sign: priv, noisePriv: nk.Private,
		pub:   base64.RawStdEncoding.EncodeToString(pub),
		noise: base64.RawStdEncoding.EncodeToString(nk.Public),
	}
}

func (d deviceKeys) signEnroll(nonce, name string) string {
	sig := ed25519.Sign(d.sign, identity.EnrollMessage(nonce, d.pub, d.noise, name))
	return base64.RawStdEncoding.EncodeToString(sig)
}

func (d deviceKeys) proof(method, path string, body []byte) (ts, nonce, sig string) {
	ts = strconv.FormatInt(time.Now().Unix(), 10)
	nonce, err := secret.RandomToken(9)
	if err != nil {
		nonce = ts
	}
	msg := identity.ProofMessage(ts, nonce, method, path, body)
	return ts, nonce, base64.RawStdEncoding.EncodeToString(ed25519.Sign(d.sign, msg))
}

func TestBootstrapLoginAndNoDefaultPassword(t *testing.T) {
	h := newHarness(t)
	code, body := h.do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"login_name": "admin", "password": "admin",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("default password was accepted or unexpected status %d %v", code, body)
	}
	h.bootstrap(t)
	code, body = h.do(t, http.MethodPost, "/api/v1/admin/bootstrap", "", map[string]string{
		"token": h.token, "login_name": "other-admin", "password": h.password,
	})
	if code != http.StatusConflict {
		t.Fatalf("bootstrap reuse %d %v", code, body)
	}
	if strings.Contains(h.logs.String(), h.password) || strings.Contains(h.logs.String(), h.token) {
		t.Fatalf("secret leaked into logs: %s", h.logs.String())
	}
	code, bad := h.do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"login_name": h.admin, "password": "not-the-password",
	})
	if code != http.StatusUnauthorized || bad["error"].(map[string]any)["message"] != "账号或密码错误" {
		t.Fatalf("bad login %d %v", code, bad)
	}
	access := h.login(t, h.admin, h.password)
	code, _ = h.do(t, http.MethodGet, "/api/v1/admin/users", access, nil)
	if code != http.StatusOK {
		t.Fatalf("admin list %d", code)
	}
}

func TestRefreshReuseRevokesFamily(t *testing.T) {
	h := newHarness(t)
	h.bootstrap(t)
	code, body := h.do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"login_name": h.admin, "password": h.password,
	})
	if code != http.StatusOK {
		t.Fatal(body)
	}
	refresh := body["refresh_token"].(string)
	access := body["access_token"].(string)
	code, body = h.do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh})
	if code != http.StatusOK {
		t.Fatal(body)
	}
	next := body["refresh_token"].(string)
	code, _ = h.do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh})
	if code != http.StatusUnauthorized {
		t.Fatalf("reused refresh status %d", code)
	}
	code, _ = h.do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refresh_token": next})
	if code != http.StatusUnauthorized {
		t.Fatalf("family was not revoked, status %d", code)
	}
	code, _ = h.do(t, http.MethodPost, "/api/v1/auth/logout", access, map[string]string{})
	if code != http.StatusOK && code != http.StatusUnauthorized {
		t.Fatalf("logout %d", code)
	}
}

func TestDeviceTrustGrantAndSessionTicket(t *testing.T) {
	h := newHarness(t)
	h.bootstrap(t)
	access := h.login(t, h.admin, h.password)
	host := enrollDevice(t, h, access, "office-host")
	ctrl := enrollDevice(t, h, access, "office-controller")

	code, denied := h.do(t, http.MethodPost, "/api/v1/sessions", access, map[string]any{
		"host_id": host.id, "controller_id": ctrl.id, "requested_scopes": []string{"view", "input"}, "nonce": "n1",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("session without device proof %d %v", code, denied)
	}
	code, waiting := signedJSON(t, h, access, ctrl, http.MethodPost, "/api/v1/sessions", map[string]any{
		"host_id": host.id, "controller_id": ctrl.id, "requested_scopes": []string{"view"}, "nonce": "n1",
	})
	if code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("untrusted devices should not connect, got %d %v", code, waiting)
	}

	approve(t, h, access, host, nil)
	approve(t, h, access, ctrl, &host)
	code, noGrant := signedJSON(t, h, access, ctrl, http.MethodPost, "/api/v1/sessions", map[string]any{
		"host_id": host.id, "controller_id": ctrl.id, "requested_scopes": []string{"view", "input"}, "nonce": "n2",
	})
	if code != http.StatusCreated || noGrant["consent_required"] != true || noGrant["ticket"] != nil {
		t.Fatalf("expected consent without ticket, %d %v", code, noGrant)
	}
	sid := noGrant["session_id"].(string)
	code, consented := signedJSON(t, h, access, host, http.MethodPost, "/api/v1/sessions/"+sid+"/consent", map[string]any{
		"accept": true, "granted_scopes": []string{"view"},
	})
	if code != http.StatusOK || consented["ticket"] == "" {
		t.Fatalf("consent %d %v", code, consented)
	}
	if scopes, _ := consented["granted_scopes"].([]any); len(scopes) != 1 {
		t.Fatalf("host narrowed scopes: %v", consented)
	}

	code, grant := h.do(t, http.MethodPut, "/api/v1/devices/"+host.id+"/grants", access, map[string]any{
		"controller_id": ctrl.id, "scopes": []string{"view", "input"}, "unattended": true, "policy_version": 0,
	})
	if code != http.StatusOK || grant["unattended"] != true {
		t.Fatalf("grant %d %v", code, grant)
	}
	code, active := signedJSON(t, h, access, ctrl, http.MethodPost, "/api/v1/sessions", map[string]any{
		"host_id": host.id, "controller_id": ctrl.id, "requested_scopes": []string{"view", "input"}, "nonce": "n3",
	})
	if code != http.StatusCreated || active["consent_required"] != false {
		t.Fatalf("unattended %d %v", code, active)
	}
	ticket := active["ticket"].(string)
	if code := redeem(t, h, ticket); code != http.StatusOK {
		t.Fatalf("redeem %d", code)
	}
	if code := redeem(t, h, ticket); code == http.StatusOK {
		t.Fatal("ticket redeemed twice")
	}
	pubCode, _ := h.do(t, http.MethodPost, "/api/v1/internal/tickets/redeem", "", map[string]string{"ticket": ticket})
	if pubCode != http.StatusNotFound {
		t.Fatalf("internal route on public port: %d", pubCode)
	}

	code, _ = h.do(t, http.MethodPost, "/api/v1/devices/"+ctrl.id+"/revoke", access, map[string]string{})
	if code != http.StatusOK {
		t.Fatalf("revoke %d", code)
	}
	code, again := signedJSON(t, h, access, ctrl, http.MethodPost, "/api/v1/sessions", map[string]any{
		"host_id": host.id, "controller_id": ctrl.id, "requested_scopes": []string{"view"}, "nonce": "n4",
	})
	if code == http.StatusCreated {
		t.Fatalf("revoked controller still created a session: %v", again)
	}
}

func TestAdminSeesMetadataNotTicketsAndReleaseSignature(t *testing.T) {
	h := newHarness(t)
	h.bootstrap(t)
	access := h.login(t, h.admin, h.password)
	code, created := h.do(t, http.MethodPost, "/api/v1/admin/users", access, map[string]string{
		"login_name": "ada", "password": "ada-password-ok",
	})
	if code != http.StatusCreated {
		t.Fatalf("create user %d %v", code, created)
	}
	var buf bytes.Buffer
	w := multipartRelease(&buf, "hello-artifact")
	req, err := http.NewRequest(http.MethodPost, h.public.URL+"/api/v1/admin/releases", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w)
	req.Header.Set("Authorization", "Bearer "+access)
	res, err := h.public.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var man map[string]any
	if err := json.NewDecoder(res.Body).Decode(&man); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("upload %d %v", res.StatusCode, man)
	}
	if !VerifyArtifact(h.srv.UpdatePub, man["sha256"].(string), man["artifact_id"].(string), man["signature"].(string)) {
		t.Fatal("signature did not verify")
	}
	if VerifyArtifact(h.srv.UpdatePub, man["sha256"].(string)+"00", man["artifact_id"].(string), man["signature"].(string)) {
		t.Fatal("tampered hash still verified")
	}
	code, _ = h.do(t, http.MethodGet, "/downloads/"+man["artifact_id"].(string), "", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("private download without auth %d", code)
	}
	code, _ = h.do(t, http.MethodGet, "/downloads/"+man["artifact_id"].(string), access, nil)
	if code != http.StatusOK {
		t.Fatalf("private download with auth %d", code)
	}
	audCode, aud := h.do(t, http.MethodGet, "/api/v1/admin/audit", access, nil)
	if audCode != http.StatusOK {
		t.Fatal(aud)
	}
	if strings.Contains(string(mustJSON(aud)), h.password) || strings.Contains(string(mustJSON(aud)), "ada-password-ok") {
		t.Fatalf("audit contains a password: %v", aud)
	}
}

type enrolled struct {
	id   string
	keys deviceKeys
}

func enrollDevice(t *testing.T, h *harness, access, name string) enrolled {
	t.Helper()
	keys := makeDevice(t)
	code, ch := h.do(t, http.MethodPost, "/api/v1/devices/challenge", access, map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("challenge %d %v", code, ch)
	}
	body := map[string]any{
		"name": name, "os": "linux", "arch": "amd64",
		"public_key": keys.pub, "noise_public_key": keys.noise,
		"capabilities": map[string]any{"virtual_display": false, "vr_send": false, "vr_receive": false},
		"challenge_id": ch["challenge_id"], "signature": keys.signEnroll(ch["nonce"].(string), name),
	}
	code, got := h.do(t, http.MethodPost, "/api/v1/devices/enroll", access, body)
	if code != http.StatusCreated || got["trust_state"] != "pending" {
		t.Fatalf("enroll %d %v", code, got)
	}
	return enrolled{id: got["device_id"].(string), keys: keys}
}

func approve(t *testing.T, h *harness, access string, dev enrolled, trusted *enrolled) {
	t.Helper()
	code, info := h.do(t, http.MethodGet, "/api/v1/devices/"+dev.id, access, nil)
	if code != http.StatusOK {
		t.Fatal(info)
	}
	fp := info["fingerprint"].(string)
	payload := map[string]any{"confirm_fingerprint": "not-the-fingerprint"}
	if trusted == nil {
		if code, body := h.do(t, http.MethodPost, "/api/v1/devices/"+dev.id+"/approve", access, payload); code != http.StatusConflict {
			t.Fatalf("wrong fingerprint %d %v", code, body)
		}
	}
	payload["confirm_fingerprint"] = fp
	var code2 int
	var body map[string]any
	if trusted == nil {
		code2, body = h.do(t, http.MethodPost, "/api/v1/devices/"+dev.id+"/approve", access, payload)
	} else {
		code2, body = signedJSON(t, h, access, *trusted, http.MethodPost, "/api/v1/devices/"+dev.id+"/approve", payload)
	}
	if code2 != http.StatusOK {
		t.Fatalf("approve %d %v", code2, body)
	}
}

func signedJSON(t *testing.T, h *harness, access string, dev enrolled, method, path string, payload any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	ts, nonce, sig := dev.keys.proof(method, path, raw)
	req, err := http.NewRequest(method, h.public.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("X-CyanDesk-Device", dev.id)
	req.Header.Set("X-CyanDesk-Timestamp", ts)
	req.Header.Set("X-CyanDesk-Nonce", nonce)
	req.Header.Set("X-CyanDesk-Signature", sig)
	res, err := h.public.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out
}

func redeem(t *testing.T, h *harness, ticket string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.internal.URL+"/api/v1/internal/tickets/redeem", strings.NewReader(`{"ticket":`+strconv.Quote(ticket)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.srv.Material.InternalToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

func multipartRelease(buf *bytes.Buffer, content string) string {
	w := multipart.NewWriter(buf)
	_ = w.WriteField("channel", "stable")
	_ = w.WriteField("version", "0.1.0")
	_ = w.WriteField("min_safe_version", "0.1.0")
	_ = w.WriteField("engine_version", "linux-ffmpeg")
	_ = w.WriteField("os", "linux")
	_ = w.WriteField("arch", "amd64")
	_ = w.WriteField("status", "private")
	part, err := w.CreateFormFile("file", "cyandesk.txt")
	if err != nil {
		panic(err)
	}
	_, _ = part.Write([]byte(content))
	_ = w.Close()
	return w.FormDataContentType()
}
