package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cyandesk/internal/id"
	"cyandesk/internal/identity"
	"cyandesk/internal/store"
)

func (s *Server) challenge(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	nonce, err := randomNonce()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法创建挑战")
		return
	}
	cid, err := s.Store.SaveChallenge(a.User.ID, nonce, time.Now().Add(2*time.Minute))
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法创建挑战")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"challenge_id": cid, "nonce": nonce})
}

func randomNonce() (string, error) {
	return id.New(), nil
}

func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var body struct {
		Name           string         `json:"name"`
		OS             string         `json:"os"`
		Arch           string         `json:"arch"`
		PublicKey      string         `json:"public_key"`
		NoisePublicKey string         `json:"noise_public_key"`
		Capabilities   map[string]any `json:"capabilities"`
		ChallengeID    string         `json:"challenge_id"`
		Signature      string         `json:"signature"`
	}
	if err := readJSON(r, &body); err != nil || body.Name == "" || body.OS == "" || body.Arch == "" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "设备信息不完整")
		return
	}
	nonce, err := s.Store.TakeChallenge(body.ChallengeID, a.User.ID)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "挑战无效或已过期")
		return
	}
	pub, err := base64.RawStdEncoding.DecodeString(body.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "设备公钥无效")
		return
	}
	noisePub, err := base64.RawStdEncoding.DecodeString(body.NoisePublicKey)
	if err != nil || len(noisePub) != 32 {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "传输公钥无效")
		return
	}
	sig, err := base64.RawStdEncoding.DecodeString(body.Signature)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "签名无效")
		return
	}
	msg := identity.EnrollMessage(nonce, body.PublicKey, body.NoisePublicKey, body.Name)
	if !ed25519.Verify(ed25519.PublicKey(pub), msg, sig) {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "设备签名无法通过")
		return
	}
	fp := identity.Fingerprint(pub, noisePub)
	caps := `{}`
	if body.Capabilities != nil {
		if b, err := json.Marshal(body.Capabilities); err == nil {
			caps = string(b)
		}
	}
	d := store.Device{
		ID: id.New(), OwnerID: a.User.ID, Name: body.Name, OS: body.OS, Arch: body.Arch,
		PublicKey: body.PublicKey, NoisePublicKey: body.NoisePublicKey, Fingerprint: fp,
		KeyVersion: 1, TrustState: "pending", Capabilities: caps,
	}
	if err := s.Store.InsertDevice(d); err != nil {
		s.fail(w, r, http.StatusConflict, "CONFLICT", "这台设备的公钥已经登记")
		return
	}
	s.audit(a.User.ID, "device.enroll", d.ID, "pending", reqID(r), map[string]string{"fingerprint": fp, "name": body.Name})
	s.hub.Publish(a.User.ID, Event{Type: "device.pending", DeviceID: d.ID, State: "pending"})
	writeJSON(w, http.StatusCreated, map[string]any{
		"device_id": d.ID, "trust_state": "pending", "fingerprint": fp,
	})
}

func hexEncode(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	own, err := s.Store.ListDevicesByOwner(a.User.ID)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法读取设备")
		return
	}
	ids := make([]string, 0, len(own))
	for _, d := range own {
		ids = append(ids, d.ID)
	}
	granted, err := s.Store.HostsGrantedTo(ids)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法读取设备")
		return
	}
	seen := map[string]bool{}
	out := make([]map[string]any, 0, len(own)+len(granted))
	for _, d := range append(own, granted...) {
		if seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		out = append(out, s.deviceJSON(d, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (s *Server) getDevice(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	d, err := s.Store.Device(r.PathValue("id"))
	if err != nil || !s.canSeeDevice(a, d) {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这台设备")
		return
	}
	writeJSON(w, http.StatusOK, s.deviceJSON(d, false))
}

func (s *Server) canSeeDevice(a actor, d store.Device) bool {
	if d.OwnerID == a.User.ID || a.User.Role == "admin" {
		return true
	}
	own, err := s.Store.ListDevicesByOwner(a.User.ID)
	if err != nil {
		return false
	}
	for _, c := range own {
		if _, err := s.Store.GetGrant(d.ID, c.ID); err == nil {
			return true
		}
	}
	return false
}

func (s *Server) deviceJSON(d store.Device, admin bool) map[string]any {
	online := false
	if d.LastSeen != "" && d.TrustState == "trusted" {
		if t, err := time.Parse(time.RFC3339Nano, d.LastSeen); err == nil && time.Since(t) <= s.OnlineTTL {
			online = true
		}
	}
	item := map[string]any{
		"id": d.ID, "owner_id": d.OwnerID, "name": d.Name, "os": d.OS, "arch": d.Arch,
		"fingerprint": d.Fingerprint, "trust_state": d.TrustState, "online": online,
		"last_seen": d.LastSeen, "reported_capabilities": jsonRaw(d.Capabilities),
	}
	if !admin {
		item["public_key"] = d.PublicKey
		item["noise_public_key"] = d.NoisePublicKey
	}
	return item
}

func (s *Server) approveDevice(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	d, err := s.Store.Device(r.PathValue("id"))
	if err != nil || d.OwnerID != a.User.ID {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这台设备")
		return
	}
	var body struct {
		ConfirmFingerprint string `json:"confirm_fingerprint"`
	}
	if err := readJSON(r, &body); err != nil || body.ConfirmFingerprint == "" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "需要确认指纹")
		return
	}
	if body.ConfirmFingerprint != d.Fingerprint {
		s.fail(w, r, http.StatusConflict, "CONFLICT", "指纹不一致，已拒绝信任")
		return
	}
	trusted, err := s.Store.CountTrusted(a.User.ID)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法核验信任")
		return
	}
	if trusted > 0 {
		dev, ok := s.requireDeviceProof(w, r, a, a.RawBody)
		if !ok {
			return
		}
		if dev.TrustState != "trusted" || dev.OwnerID != a.User.ID {
			s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "需要已信任的设备确认")
			return
		}
	}
	if err := s.Store.ApproveDevice(d.ID, d.Fingerprint); err != nil {
		s.fail(w, r, http.StatusConflict, "CONFLICT", "设备不在待批准状态")
		return
	}
	s.audit(a.User.ID, "device.approve", d.ID, "ok", reqID(r), map[string]string{"fingerprint": d.Fingerprint})
	s.hub.Publish(a.User.ID, Event{Type: "device.trusted", DeviceID: d.ID, State: "trusted"})
	writeJSON(w, http.StatusOK, map[string]string{"trust_state": "trusted", "fingerprint": d.Fingerprint})
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	d, err := s.Store.Device(r.PathValue("id"))
	if err != nil || d.OwnerID != a.User.ID {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这台设备")
		return
	}
	s.finishRevoke(w, r, a.User.ID, d)
}

func (s *Server) finishRevoke(w http.ResponseWriter, r *http.Request, actorID string, d store.Device) {
	if err := s.Store.RevokeDevice(d.ID); err != nil {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "设备已撤销或不存在")
		return
	}
	s.audit(actorID, "device.revoke", d.ID, "ok", reqID(r), nil)
	s.hub.Publish(d.OwnerID, Event{Type: "device.revoked", DeviceID: d.ID, State: "revoked"})
	writeJSON(w, http.StatusOK, map[string]string{"trust_state": "revoked"})
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	dev, ok := s.requireDeviceProof(w, r, a, a.RawBody)
	if !ok {
		return
	}
	if dev.ID != r.PathValue("id") {
		s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "设备身份不匹配")
		return
	}
	if dev.TrustState != "trusted" {
		s.fail(w, r, http.StatusForbidden, "AUTH_REVOKED", "设备未被信任")
		return
	}
	if err := s.Store.TouchDevice(dev.ID); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法更新在线状态")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listGrants(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	d, err := s.Store.Device(r.PathValue("id"))
	if err != nil || d.OwnerID != a.User.ID {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这台设备")
		return
	}
	grants, err := s.Store.ListGrants(d.ID)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法读取授权")
		return
	}
	out := make([]map[string]any, 0, len(grants))
	for _, g := range grants {
		out = append(out, grantJSON(g))
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": out})
}

func grantJSON(g store.Grant) map[string]any {
	return map[string]any{
		"id": g.ID, "host_id": g.HostID, "controller_id": g.ControllerID,
		"scopes": splitScopes(g.Scopes), "unattended": g.Unattended,
		"expires_at": g.ExpiresAt, "policy_version": g.PolicyVersion,
	}
}

func (s *Server) putGrant(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	host, err := s.Store.Device(r.PathValue("id"))
	if err != nil || host.OwnerID != a.User.ID {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这台设备")
		return
	}
	var body struct {
		ControllerID  string   `json:"controller_id"`
		Scopes        []string `json:"scopes"`
		Unattended    bool     `json:"unattended"`
		ExpiresAt     string   `json:"expires_at"`
		PolicyVersion int64    `json:"policy_version"`
	}
	if err := readJSON(r, &body); err != nil || body.ControllerID == "" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "授权内容不完整")
		return
	}
	if match := r.Header.Get("If-Match"); match != "" {
		v, err := strconv.ParseInt(strings.Trim(match, `"`), 10, 64)
		if err != nil {
			s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "If-Match 无效")
			return
		}
		body.PolicyVersion = v
	}
	ctrl, err := s.Store.Device(body.ControllerID)
	if err != nil || ctrl.TrustState != "trusted" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "主控设备尚未信任")
		return
	}
	scopes, err := normalizeScopes(body.Scopes)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "权限项无效")
		return
	}
	g := store.Grant{
		HostID: host.ID, ControllerID: ctrl.ID, Scopes: strings.Join(scopes, ","),
		Unattended: body.Unattended, ExpiresAt: body.ExpiresAt, ApprovedBy: a.User.ID,
		PolicyVersion: body.PolicyVersion,
	}
	if err := s.Store.UpsertGrant(g); err != nil {
		s.fail(w, r, http.StatusConflict, "CONFLICT", "授权版本冲突，请刷新后再提交")
		return
	}
	saved, _ := s.Store.GetGrant(host.ID, ctrl.ID)
	s.audit(a.User.ID, "grant.update", host.ID, "ok", reqID(r), map[string]string{"controller_id": ctrl.ID, "scopes": g.Scopes})
	s.hub.Publish(host.OwnerID, Event{Type: "grant.updated", DeviceID: host.ID})
	if ctrl.OwnerID != host.OwnerID {
		s.hub.Publish(ctrl.OwnerID, Event{Type: "grant.updated", DeviceID: host.ID})
	}
	writeJSON(w, http.StatusOK, grantJSON(saved))
}

func normalizeScopes(in []string) ([]string, error) {
	allow := map[string]bool{"view": true, "input": true, "audio": true, "file": true, "clipboard": true}
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !allow[s] || seen[s] {
			if s == "" || seen[s] {
				continue
			}
			return nil, errBadScope
		}
		seen[s] = true
		out = append(out, s)
	}
	if !seen["view"] {
		return nil, errBadScope
	}
	return out, nil
}

var errBadScope = errText("bad scope")

type errText string

func (e errText) Error() string { return string(e) }

func splitScopes(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

func (s *Server) requireDeviceProof(w http.ResponseWriter, r *http.Request, a actor, body []byte) (store.Device, bool) {
	devID := r.Header.Get("X-CyanDesk-Device")
	ts := r.Header.Get("X-CyanDesk-Timestamp")
	nonce := r.Header.Get("X-CyanDesk-Nonce")
	sigB64 := r.Header.Get("X-CyanDesk-Signature")
	if devID == "" || ts == "" || nonce == "" || sigB64 == "" {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "缺少设备签名")
		return store.Device{}, false
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || abs(time.Now().Unix()-unix) > 120 {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "设备签名已过期")
		return store.Device{}, false
	}
	if err := s.Store.SeenProofNonce(nonce, time.Now().Add(5*time.Minute)); err != nil {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "设备签名重放")
		return store.Device{}, false
	}
	dev, err := s.Store.Device(devID)
	if err != nil || dev.OwnerID != a.User.ID || dev.TrustState == "revoked" {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "设备不可用")
		return store.Device{}, false
	}
	pub, err := base64.RawStdEncoding.DecodeString(dev.PublicKey)
	if err != nil {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "设备不可用")
		return store.Device{}, false
	}
	sig, err := base64.RawStdEncoding.DecodeString(sigB64)
	if err != nil {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "设备签名无效")
		return store.Device{}, false
	}
	msg := identity.ProofMessage(ts, nonce, r.Method, r.URL.Path, body)
	if !ed25519.Verify(ed25519.PublicKey(pub), msg, sig) {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "设备签名无法通过")
		return store.Device{}, false
	}
	return dev, true
}

func jsonRaw(s string) any {
	if s == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return map[string]any{}
	}
	return v
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
