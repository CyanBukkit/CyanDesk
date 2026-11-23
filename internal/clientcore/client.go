package clientcore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cyandesk/internal/e2ee"
	"cyandesk/internal/identity"
	"cyandesk/internal/secret"
	"github.com/flynn/noise"
)

// Profile is the on-disk client state. Private keys stay in this directory at mode 0600.
type Profile struct {
	ServerURL    string            `json:"server_url"`
	ServerID     string            `json:"server_id,omitempty"`
	Fingerprint  string            `json:"fingerprint,omitempty"`
	AccessToken  string            `json:"access_token,omitempty"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	UserID       string            `json:"user_id,omitempty"`
	LoginName    string            `json:"login_name,omitempty"`
	DeviceID     string            `json:"device_id,omitempty"`
	PublicKey    string            `json:"public_key,omitempty"`
	PrivateKey   string            `json:"private_key,omitempty"`
	NoisePublic  string            `json:"noise_public,omitempty"`
	NoisePrivate string            `json:"noise_private,omitempty"`
	DeviceFP     string            `json:"device_fingerprint,omitempty"`
	Pins         map[string]string `json:"pins,omitempty"`
}

func Load(dir string) (Profile, error) {
	b, err := os.ReadFile(filepath.Join(dir, "profile.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, nil
		}
		return Profile{}, err
	}
	var p Profile
	err = json.Unmarshal(b, &p)
	return p, err
}

func (p Profile) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "profile.json"), append(b, '\n'), 0o600)
}

func (p Profile) client() (*http.Client, error) {
	if p.Fingerprint == "" {
		return nil, errors.New("还没有信任服务器证书指纹")
	}
	u, err := url.Parse(p.ServerURL)
	if err != nil {
		return nil, err
	}
	name := u.Hostname()
	pin := strings.ToLower(strings.TrimSpace(p.Fingerprint))
	return &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         name,
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("服务器没有出示证书")
			}
			cert, err := x509.ParseCertificate(raw[0])
			if err != nil {
				return err
			}
			if err := cert.VerifyHostname(name); err != nil {
				return fmt.Errorf("主机名与证书不符: %w", err)
			}
			now := time.Now()
			if now.After(cert.NotAfter) || now.Before(cert.NotBefore) {
				return errors.New("证书不在有效期内")
			}
			sum := sha256.Sum256(raw[0])
			if hex.EncodeToString(sum[:]) != pin {
				return errors.New("证书指纹与已固定的值不一致，连接已停止")
			}
			return nil
		},
	}}}, nil
}

func (p Profile) Call(method, path string, body any, auth bool) (int, map[string]any, error) {
	return p.CallBytes(method, path, body, nil, auth)
}

func (p Profile) CallBytes(method, path string, body any, raw []byte, auth bool) (int, map[string]any, error) {
	httpc, err := p.client()
	if err != nil {
		return 0, nil, err
	}
	var rdr io.Reader
	if raw != nil {
		rdr = bytes.NewReader(raw)
	} else if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		raw = buf
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, strings.TrimRight(p.ServerURL, "/")+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	if raw != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		if p.AccessToken == "" {
			return 0, nil, errors.New("尚未登录")
		}
		req.Header.Set("Authorization", "Bearer "+p.AccessToken)
	}
	res, err := httpc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(buf, &out)
	return res.StatusCode, out, nil
}

func (p *Profile) Bootstrap(token, login, password string) error {
	code, body, err := p.Call(http.MethodPost, "/api/v1/admin/bootstrap", map[string]string{
		"token": token, "login_name": login, "password": password,
	}, false)
	if err != nil {
		return err
	}
	if code != http.StatusCreated {
		return fmt.Errorf("初始化失败 (%d): %v", code, body["error"])
	}
	return nil
}

func (p *Profile) Login(login, password string) error {
	code, body, err := p.Call(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"login_name": login, "password": password,
	}, false)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("登录失败 (%d): %v", code, body["error"])
	}
	p.AccessToken, _ = body["access_token"].(string)
	p.RefreshToken, _ = body["refresh_token"].(string)
	p.UserID, _ = body["user_id"].(string)
	p.LoginName = login
	if p.AccessToken == "" {
		return errors.New("服务器没有返回访问令牌")
	}
	return nil
}

func (p *Profile) EnsureKeys() error {
	if p.PrivateKey != "" && p.NoisePrivate != "" {
		return nil
	}
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	nk, err := e2ee.GenerateKey()
	if err != nil {
		return err
	}
	p.PublicKey = base64.RawStdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))
	p.PrivateKey = base64.RawStdEncoding.EncodeToString(priv)
	p.NoisePublic = base64.RawStdEncoding.EncodeToString(nk.Public)
	p.NoisePrivate = base64.RawStdEncoding.EncodeToString(nk.Private)
	return nil
}

func reportedEngine(osName string) (string, bool) {
	switch osName {
	case "linux":
		return "linux-ffmpeg", true
	case "windows":
		return "windows-gdi", true
	case "android":
		// MediaProjection plus MediaCodec. Not an OpenXR or Pico runtime.
		return "android-mediacodec", true
	default:
		return "unimplemented", false
	}
}

func (p *Profile) Enroll(name, osName, arch string) error {
	if err := p.EnsureKeys(); err != nil {
		return err
	}
	code, ch, err := p.Call(http.MethodPost, "/api/v1/devices/challenge", map[string]any{}, true)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("挑战失败 (%d): %v", code, ch["error"])
	}
	nonce, _ := ch["nonce"].(string)
	priv, err := base64.RawStdEncoding.DecodeString(p.PrivateKey)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(ed25519.PrivateKey(priv), identity.EnrollMessage(nonce, p.PublicKey, p.NoisePublic, name))
	engine, captureOK := reportedEngine(osName)
	payload := map[string]any{
		"name": name, "os": osName, "arch": arch,
		"public_key": p.PublicKey, "noise_public_key": p.NoisePublic,
		"capabilities": map[string]any{
			"capture": captureOK, "input": captureOK, "audio": false,
			"virtual_display": false, "vr_send": false, "vr_receive": false,
			"engine": engine,
		},
		"challenge_id": ch["challenge_id"], "signature": base64.RawStdEncoding.EncodeToString(sig),
	}
	code, got, err := p.Call(http.MethodPost, "/api/v1/devices/enroll", payload, true)
	if err != nil {
		return err
	}
	if code != http.StatusCreated {
		return fmt.Errorf("登记失败 (%d): %v", code, got["error"])
	}
	p.DeviceID, _ = got["device_id"].(string)
	p.DeviceFP, _ = got["fingerprint"].(string)
	return nil
}

func (p Profile) Devices() ([]map[string]any, error) {
	code, body, err := p.Call(http.MethodGet, "/api/v1/devices", nil, true)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("读取设备失败 (%d)", code)
	}
	raw, _ := body["devices"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (p *Profile) Approve(id, fingerprint string) error {
	payload := map[string]string{"confirm_fingerprint": fingerprint}
	code, body, err := p.Call(http.MethodPost, "/api/v1/devices/"+id+"/approve", payload, true)
	if err != nil {
		return err
	}
	if code == http.StatusOK {
		return nil
	}
	raw, _ := json.Marshal(payload)
	req, signErr := p.signed(http.MethodPost, "/api/v1/devices/"+id+"/approve", raw)
	if signErr != nil {
		return fmt.Errorf("批准失败 (%d): %v", code, body["error"])
	}
	code, body, err = p.doSigned(req)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("批准失败 (%d): %v", code, body["error"])
	}
	if p.Pins == nil {
		p.Pins = map[string]string{}
	}
	p.Pins[id] = fingerprint
	return nil
}

func (p Profile) signed(method, path string, raw []byte) (*http.Request, error) {
	if p.DeviceID == "" || p.PrivateKey == "" {
		return nil, errors.New("本机还没有设备密钥")
	}
	priv, err := base64.RawStdEncoding.DecodeString(p.PrivateKey)
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, err := secret.RandomToken(9)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(ed25519.PrivateKey(priv), identity.ProofMessage(ts, nonce, method, path, raw))
	req, err := http.NewRequest(method, strings.TrimRight(p.ServerURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.AccessToken)
	req.Header.Set("X-CyanDesk-Device", p.DeviceID)
	req.Header.Set("X-CyanDesk-Timestamp", ts)
	req.Header.Set("X-CyanDesk-Nonce", nonce)
	req.Header.Set("X-CyanDesk-Signature", base64.RawStdEncoding.EncodeToString(sig))
	return req, nil
}

func (p Profile) doSigned(req *http.Request) (int, map[string]any, error) {
	httpc, err := p.client()
	if err != nil {
		return 0, nil, err
	}
	res, err := httpc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(buf, &out)
	return res.StatusCode, out, nil
}

func (p Profile) Grant(hostID, controllerID string, scopes []string, unattended bool) error {
	code, body, err := p.Call(http.MethodPut, "/api/v1/devices/"+hostID+"/grants", map[string]any{
		"controller_id": controllerID, "scopes": scopes, "unattended": unattended, "policy_version": 0,
	}, true)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("授权失败 (%d): %v", code, body["error"])
	}
	return nil
}

func (p Profile) CreateSession(hostID string, scopes []string) (sessionID, ticket string, granted []any, err error) {
	raw, err := json.Marshal(map[string]any{
		"host_id": hostID, "controller_id": p.DeviceID, "requested_scopes": scopes, "nonce": strconv.FormatInt(time.Now().UnixNano(), 10),
	})
	if err != nil {
		return "", "", nil, err
	}
	req, err := p.signed(http.MethodPost, "/api/v1/sessions", raw)
	if err != nil {
		return "", "", nil, err
	}
	code, body, err := p.doSigned(req)
	if err != nil {
		return "", "", nil, err
	}
	if code != http.StatusCreated {
		return "", "", nil, fmt.Errorf("会话失败 (%d): %v", code, body["error"])
	}
	sessionID, _ = body["session_id"].(string)
	ticket, _ = body["ticket"].(string)
	granted, _ = body["granted_scopes"].([]any)
	if ticket == "" {
		return sessionID, "", granted, errors.New("会话还没有票据，主机尚未同意")
	}
	return sessionID, ticket, granted, nil
}

func (p Profile) Session(id string) (state, ticket string, err error) {
	return p.session(id, false)
}

func (p Profile) SessionTicket(id string) (state, ticket string, err error) {
	return p.session(id, true)
}

func (p Profile) session(id string, ticket bool) (state, raw string, err error) {
	path := "/api/v1/sessions/" + id
	if ticket {
		path += "?ticket=1"
	}
	code, body, err := p.Call(http.MethodGet, path, nil, true)
	if err != nil {
		return "", "", err
	}
	if code != http.StatusOK {
		return "", "", fmt.Errorf("读取会话失败 (%d): %v", code, body["error"])
	}
	state, _ = body["state"].(string)
	raw, _ = body["ticket"].(string)
	return state, raw, nil
}

func (p Profile) Sessions() ([]map[string]any, error) {
	code, body, err := p.Call(http.MethodGet, "/api/v1/sessions", nil, true)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("读取会话失败 (%d): %v", code, body["error"])
	}
	raw, _ := body["sessions"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (p Profile) Consent(id string, accept bool, scopes []string) error {
	raw, err := json.Marshal(map[string]any{"accept": accept, "granted_scopes": scopes})
	if err != nil {
		return err
	}
	req, err := p.signed(http.MethodPost, "/api/v1/sessions/"+id+"/consent", raw)
	if err != nil {
		return err
	}
	code, body, err := p.doSigned(req)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("同意失败 (%d): %v", code, body["error"])
	}
	return nil
}

// GatewayAddr reads the TCP gateway from server-info. A loopback advertisement
// is rewritten to the host already used for the API, so a client is not sent
// back to its own machine.
func (p Profile) GatewayAddr() (string, error) {
	code, body, err := p.Call(http.MethodGet, "/api/v1/server-info", nil, false)
	if err != nil {
		return "", err
	}
	if code != http.StatusOK {
		return "", fmt.Errorf("服务器信息失败 (%d)", code)
	}
	raw, _ := body["endpoints"].([]any)
	advertised := ""
	for _, item := range raw {
		m, _ := item.(map[string]any)
		if m["proto"] == "tcp" {
			advertised, _ = m["addr"].(string)
			break
		}
	}
	if advertised == "" {
		return "", errors.New("服务器没有公布网关地址")
	}
	u, err := url.Parse(p.ServerURL)
	if err != nil {
		return advertised, nil
	}
	host, port, err := net.SplitHostPort(advertised)
	if err != nil {
		return advertised, nil
	}
	if host == "127.0.0.1" || host == "localhost" || host == "0.0.0.0" || host == "::1" {
		if u.Hostname() != "" && u.Hostname() != host {
			return net.JoinHostPort(u.Hostname(), port), nil
		}
	}
	return advertised, nil
}

func (p Profile) CloseSession(id string) error {
	code, body, err := p.Call(http.MethodDelete, "/api/v1/sessions/"+id, nil, true)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("结束会话失败 (%d): %v", code, body["error"])
	}
	return nil
}

func (p Profile) Device(id string) (map[string]any, error) {
	code, body, err := p.Call(http.MethodGet, "/api/v1/devices/"+id, nil, true)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("读取设备失败 (%d): %v", code, body["error"])
	}
	return body, nil
}

func (p Profile) NoiseKey() (noise.DHKey, error) {
	pub, err := base64.RawStdEncoding.DecodeString(p.NoisePublic)
	if err != nil {
		return noise.DHKey{}, err
	}
	priv, err := base64.RawStdEncoding.DecodeString(p.NoisePrivate)
	if err != nil {
		return noise.DHKey{}, err
	}
	return noise.DHKey{Public: pub, Private: priv}, nil
}
