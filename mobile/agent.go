// Package mobile is the Android session entrypoint. It uses the same account,
// ticket and Noise_IK path as the desktop client. Gomobile binds this package;
// the phone does not carry a second cipher.
package mobile

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"time"

	"cyandesk/internal/clientcore"
	"cyandesk/internal/desktop"
	"cyandesk/internal/protocol"
)

const (
	// RoleHost is the controlled side. It matches protocol.RoleHost.
	RoleHost = 1
	// RoleController is the controlling side. It matches protocol.RoleController.
	RoleController = 2
)

// Agent is one logged-in device. Private keys stay inside the Go profile.
type Agent struct {
	profile clientcore.Profile
}

// Connect logs in with the pinned server certificate. It does not enroll a device.
func Connect(server, fingerprint, user, password string) (*Agent, error) {
	if !strings.HasPrefix(server, "https://") {
		return nil, errors.New("服务器地址必须以 https:// 开头")
	}
	p := clientcore.Profile{ServerURL: server, Fingerprint: strings.ToLower(strings.TrimSpace(fingerprint))}
	if err := p.Login(user, password); err != nil {
		return nil, err
	}
	return &Agent{profile: p}, nil
}

// Enroll registers this process as an Android device. The reported engine is
// MediaCodec, not OpenXR. A new device stays pending until Approve.
func (a *Agent) Enroll(name string) (string, error) {
	if name == "" {
		name = "android"
	}
	if err := a.profile.Enroll(name, "android", runtime.GOARCH); err != nil {
		return "", err
	}
	rec, err := a.profile.Device(a.profile.DeviceID)
	if err != nil {
		return "", err
	}
	trust, _ := rec["trust_state"].(string)
	return trust, nil
}

// Fingerprint is this device's identity fingerprint after Enroll.
func (a *Agent) Fingerprint() string { return a.profile.DeviceFP }

// Approve confirms a fingerprint. The first device on an account can confirm
// itself. Later devices need an already trusted device.
func (a *Agent) Approve(id, fingerprint string) error {
	return a.profile.Approve(id, fingerprint)
}

// DeviceID is the server id of this device after Enroll.
func (a *Agent) DeviceID() string { return a.profile.DeviceID }

// Devices lists visible devices as id, name, trust, fingerprint separated by tabs.
func (a *Agent) Devices() (string, error) {
	list, err := a.profile.Devices()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, item := range list {
		id, _ := item["id"].(string)
		name, _ := item["name"].(string)
		trust, _ := item["trust_state"].(string)
		fp, _ := item["fingerprint"].(string)
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(id)
		b.WriteByte('\t')
		b.WriteString(name)
		b.WriteByte('\t')
		b.WriteString(trust)
		b.WriteByte('\t')
		b.WriteString(fp)
	}
	return b.String(), nil
}

// Request asks the server for a session. An empty ticket is not an error:
// the host still has to consent. mode is "view" or "control".
func (a *Agent) Request(hostID, mode string) (string, error) {
	scopes := scopesFor(mode)
	id, _, _, err := a.profile.CreateSession(hostID, scopes)
	if id == "" {
		if err != nil {
			return "", err
		}
		return "", errors.New("服务器没有返回会话")
	}
	return id, nil
}

// Poll returns the session state. It does not return the ticket.
func (a *Agent) Poll(sessionID string) (string, error) {
	state, _, err := a.profile.Session(sessionID)
	return state, err
}

// Consent grants or refuses the session. mode selects view, or view plus input.
func (a *Agent) Consent(sessionID, mode string, accept bool) error {
	return a.profile.Consent(sessionID, accept, scopesFor(mode))
}

// Grant stores an account ACL. The phone UI uses Consent for each session.
func (a *Agent) Grant(hostID, controllerID, mode string, unattended bool) error {
	return a.profile.Grant(hostID, controllerID, scopesFor(mode), unattended)
}

// Incoming returns a session waiting for this device to consent, or an empty string.
func (a *Agent) Incoming() (string, error) {
	list, err := a.profile.Sessions()
	if err != nil {
		return "", err
	}
	for _, item := range list {
		state, _ := item["state"].(string)
		hostID, _ := item["host_id"].(string)
		id, _ := item["id"].(string)
		if state == "waiting_consent" && hostID == a.profile.DeviceID && id != "" {
			return id, nil
		}
	}
	return "", nil
}

// PeerFingerprint is the other device's current fingerprint for this session.
func (a *Agent) PeerFingerprint(sessionID string) (string, error) {
	peer, err := a.peerID(sessionID, 0)
	if err != nil {
		return "", err
	}
	return a.fingerprint(peer)
}

// AllowsInput reports the scopes on the live ticket, not the button the user pressed.
func (a *Agent) AllowsInput(sessionID string) (bool, error) {
	_, ticket, err := a.profile.SessionTicket(sessionID)
	if err != nil {
		return false, err
	}
	if ticket == "" {
		return false, nil
	}
	info, err := desktop.ParseTicket(ticket)
	if err != nil {
		return false, err
	}
	return desktop.HasInput(info.Scopes), nil
}

// Active is false after revoke or close. Callers stop capture and injection.
func (a *Agent) Active(sessionID string) (bool, error) {
	state, _, err := a.profile.Session(sessionID)
	if err != nil {
		return false, err
	}
	return state == "active", nil
}

// CloseSession ends the session on the server.
func (a *Agent) CloseSession(sessionID string) error {
	return a.profile.CloseSession(sessionID)
}

// Dial opens the encrypted relay. pinned is the fingerprint the user already
// confirmed. A different record is refused.
func (a *Agent) Dial(role int, sessionID, pinned string) (*Link, error) {
	if role != int(protocol.RoleHost) && role != int(protocol.RoleController) {
		return nil, errors.New("角色不正确")
	}
	state, ticket, err := a.profile.SessionTicket(sessionID)
	if err != nil {
		return nil, err
	}
	if state != "active" || ticket == "" {
		return nil, errors.New("会话还没有开始")
	}
	peerRole := role
	peer, err := a.peerID(sessionID, peerRole)
	if err != nil {
		return nil, err
	}
	rec, err := a.profile.Device(peer)
	if err != nil {
		return nil, err
	}
	fp, _ := rec["fingerprint"].(string)
	noiseKey, _ := rec["noise_public_key"].(string)
	if pinned == "" || !strings.EqualFold(pinned, fp) {
		return nil, errors.New("指纹与已确认的记录不一致，连接已停止")
	}
	self, err := a.profile.NoiseKey()
	if err != nil {
		return nil, err
	}
	gateway, err := a.profile.GatewayAddr()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(40*time.Second, cancel)
	// Direct UDP from Android is not a supported path. The relay is the session.
	res, err := desktop.OpenAuthorized(ctx, gateway, byte(role), ticket, pinned, fp, noiseKey, self, desktop.Direct{Skip: true})
	if !timer.Stop() {
		cancel()
		if res != nil {
			res.Close()
		}
		return nil, errors.New("连接超时")
	}
	if err != nil {
		cancel()
		return nil, err
	}
	return &Link{res: res, stop: cancel, session: sessionID}, nil
}

func (a *Agent) peerID(sessionID string, role int) (string, error) {
	list, err := a.profile.Sessions()
	if err != nil {
		return "", err
	}
	for _, item := range list {
		id, _ := item["id"].(string)
		if id != sessionID {
			continue
		}
		hostID, _ := item["host_id"].(string)
		ctrlID, _ := item["controller_id"].(string)
		if role == int(protocol.RoleController) {
			if hostID == "" {
				return "", errors.New("会话没有主机")
			}
			return hostID, nil
		}
		if role == int(protocol.RoleHost) {
			if ctrlID == "" {
				return "", errors.New("会话没有主控")
			}
			return ctrlID, nil
		}
		if a.profile.DeviceID == hostID {
			return ctrlID, nil
		}
		return hostID, nil
	}
	return "", errors.New("没有这个会话")
}

func (a *Agent) fingerprint(deviceID string) (string, error) {
	rec, err := a.profile.Device(deviceID)
	if err != nil {
		return "", err
	}
	fp, _ := rec["fingerprint"].(string)
	if fp == "" {
		return "", errors.New("设备没有指纹")
	}
	return fp, nil
}

func scopesFor(mode string) []string {
	if mode == "control" {
		return []string{"view", "input"}
	}
	return []string{"view"}
}

// WaitActive polls until the session is active or the deadline passes.
func (a *Agent) WaitActive(sessionID string, millis int) error {
	if millis < 1 {
		millis = 1000
	}
	deadline := time.Now().Add(time.Duration(millis) * time.Millisecond)
	for {
		ok, err := a.Active(sessionID)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("会话没有在时限内开始")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
