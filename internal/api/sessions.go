package api

import (
	"net/http"
	"strings"
	"time"

	"cyandesk/internal/id"
	"cyandesk/internal/secret"
	"cyandesk/internal/store"
	"github.com/coder/websocket"
)

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	ctrl, ok := s.requireDeviceProof(w, r, a, a.RawBody)
	if !ok {
		return
	}
	var body struct {
		HostID          string   `json:"host_id"`
		ControllerID    string   `json:"controller_id"`
		RequestedScopes []string `json:"requested_scopes"`
		Nonce           string   `json:"nonce"`
	}
	if err := readJSON(r, &body); err != nil || body.HostID == "" || body.Nonce == "" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "会话请求不完整")
		return
	}
	if body.ControllerID != "" && body.ControllerID != ctrl.ID {
		s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "不能替其他设备发起会话")
		return
	}
	if ctrl.TrustState != "trusted" {
		s.fail(w, r, http.StatusForbidden, "AUTH_REVOKED", "主控设备未被信任")
		return
	}
	host, err := s.Store.Device(body.HostID)
	if err != nil || host.TrustState != "trusted" {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "主机不存在或未信任")
		return
	}
	scopes, err := normalizeScopes(body.RequestedScopes)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "权限项无效")
		return
	}
	sameOwner := host.OwnerID == a.User.ID
	grant, gerr := s.Store.GetGrant(host.ID, ctrl.ID)
	grantedByPolicy := gerr == nil && grantCovers(grant, scopes)
	if !sameOwner && !grantedByPolicy {
		s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "没有连接这台主机的授权")
		return
	}
	if err := s.withinConcurrency(host.OwnerID); err != nil {
		s.fail(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "已达到并发会话上限")
		return
	}
	unattended := grantedByPolicy && grant.Unattended && !grantExpired(grant)
	state := "waiting_consent"
	granted := ""
	if unattended {
		state = "active"
		granted = strings.Join(scopes, ",")
	}
	sess := store.RemoteSession{
		ID: id.New(), HostID: host.ID, ControllerID: ctrl.ID,
		RequestedScopes: strings.Join(scopes, ","), GrantedScopes: granted,
		State: state, Nonce: body.Nonce, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if state == "active" {
		sess.StartedAt = sess.CreatedAt
	}
	if err := s.Store.CreateSession(sess); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法创建会话")
		return
	}
	resp := map[string]any{"session_id": sess.ID, "state": state, "consent_required": state != "active"}
	if state == "active" {
		ticket, err := s.ensureTicket(sess, host, ctrl, scopes)
		if err != nil {
			s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法签发票据")
			return
		}
		resp["ticket"] = ticket
		resp["granted_scopes"] = scopes
	}
	s.audit(a.User.ID, "session.create", sess.ID, state, reqID(r), map[string]string{"host_id": host.ID, "controller_id": ctrl.ID})
	s.hub.Publish(host.OwnerID, Event{Type: "session." + state, SessionID: sess.ID, DeviceID: host.ID, State: state})
	s.hub.Publish(ctrl.OwnerID, Event{Type: "session." + state, SessionID: sess.ID, DeviceID: ctrl.ID, State: state})
	writeJSON(w, http.StatusCreated, resp)
}

func grantCovers(g store.Grant, scopes []string) bool {
	have := map[string]bool{}
	for _, s := range splitScopes(g.Scopes) {
		have[s] = true
	}
	for _, s := range scopes {
		if !have[s] {
			return false
		}
	}
	return true
}

func grantExpired(g store.Grant) bool {
	if g.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, g.ExpiresAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339, g.ExpiresAt)
		if err != nil {
			return true
		}
	}
	return time.Now().After(t)
}

func (s *Server) withinConcurrency(owner string) error {
	ids, err := s.Store.DeviceIDsByOwner(owner)
	if err != nil {
		return err
	}
	n, err := s.Store.ActiveSessionsFor(ids)
	if err != nil {
		return err
	}
	_, conc, _, err := s.Store.Quota(owner)
	if err != nil {
		return err
	}
	if int64(n) >= conc {
		return errText("limit")
	}
	return nil
}

func (s *Server) mintTicket(sess store.RemoteSession, host, ctrl store.Device, scopes []string) (string, error) {
	jti := id.New()
	t := secret.Ticket{
		V: 1, JTI: jti, SID: sess.ID, HostID: host.ID, ControllerID: ctrl.ID,
		HostNoise: host.NoisePublicKey, ControllerNoise: ctrl.NoisePublicKey,
		Scopes: scopes, Nonce: sess.Nonce, Expires: time.Now().Add(s.TicketTTL).Unix(),
		ServerID: s.Material.ServerID,
	}
	raw, err := secret.SignTicket(s.SignPriv, t)
	if err != nil {
		return "", err
	}
	if err := s.Store.UpdateSession(sess.ID, "active", strings.Join(scopes, ","), secret.JTIHash(jti), ""); err != nil {
		return "", err
	}
	exp := time.Unix(t.Expires, 0).UTC().Format(time.RFC3339Nano)
	if err := s.Store.SaveRelayTicket(sess.ID, raw, secret.JTIHash(jti), exp); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Server) ensureTicket(sess store.RemoteSession, host, ctrl store.Device, scopes []string) (string, error) {
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
	raw, hash, exp, err := s.Store.RelayTicket(sess.ID)
	if err == nil && raw != "" && hash != "" {
		used, uerr := s.Store.JTIUsed(hash)
		deadline, perr := time.Parse(time.RFC3339Nano, exp)
		if uerr == nil && perr == nil && !used && time.Now().Add(5*time.Second).Before(deadline) {
			return raw, nil
		}
	}
	return s.mintTicket(sess, host, ctrl, scopes)
}

func (s *Server) consent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	dev, ok := s.requireDeviceProof(w, r, a, a.RawBody)
	if !ok {
		return
	}
	sess, err := s.Store.Session(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这个会话")
		return
	}
	if dev.ID != sess.HostID || dev.TrustState != "trusted" {
		s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "只有主机可以同意这次连接")
		return
	}
	if sess.State != "waiting_consent" {
		s.fail(w, r, http.StatusConflict, "CONFLICT", "会话不在等待同意")
		return
	}
	var body struct {
		Accept        bool     `json:"accept"`
		GrantedScopes []string `json:"granted_scopes"`
	}
	if err := readJSON(r, &body); err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "同意内容不正确")
		return
	}
	if !body.Accept {
		_ = s.Store.UpdateSession(sess.ID, "denied", "", "", "denied")
		s.audit(a.User.ID, "session.consent", sess.ID, "denied", reqID(r), nil)
		s.hub.Publish(a.User.ID, Event{Type: "session.denied", SessionID: sess.ID, State: "denied"})
		writeJSON(w, http.StatusOK, map[string]string{"state": "denied"})
		return
	}
	scopes, err := normalizeScopes(body.GrantedScopes)
	if err != nil || !subset(scopes, splitScopes(sess.RequestedScopes)) {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "同意的权限不能超过请求")
		return
	}
	host, err := s.Store.Device(sess.HostID)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "主机不存在")
		return
	}
	ctrl, err := s.Store.Device(sess.ControllerID)
	if err != nil || ctrl.TrustState != "trusted" {
		s.fail(w, r, http.StatusForbidden, "AUTH_REVOKED", "主控设备已被撤销")
		return
	}
	sess.Nonce = sess.Nonce
	ticket, err := s.ensureTicket(sess, host, ctrl, scopes)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法签发票据")
		return
	}
	s.audit(a.User.ID, "session.consent", sess.ID, "active", reqID(r), map[string]string{"scopes": strings.Join(scopes, ",")})
	s.hub.Publish(host.OwnerID, Event{Type: "session.active", SessionID: sess.ID, State: "active"})
	s.hub.Publish(ctrl.OwnerID, Event{Type: "session.active", SessionID: sess.ID, State: "active"})
	writeJSON(w, http.StatusOK, map[string]any{"state": "active", "ticket": ticket, "granted_scopes": scopes})
}

func subset(got, allowed []string) bool {
	ok := map[string]bool{}
	for _, s := range allowed {
		ok[s] = true
	}
	for _, s := range got {
		if !ok[s] {
			return false
		}
	}
	return true
}

func (s *Server) listMySessions(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	all, err := s.Store.ListSessions()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法读取会话")
		return
	}
	out := make([]map[string]any, 0)
	for _, sess := range all {
		if !s.sessionVisible(a, sess) {
			continue
		}
		out = append(out, s.sessionJSON(sess, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	sess, err := s.Store.Session(r.PathValue("id"))
	if err != nil || !s.sessionVisible(a, sess) {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这个会话")
		return
	}
	writeJSON(w, http.StatusOK, s.sessionJSON(sess, r.URL.Query().Get("ticket") == "1"))
}

func (s *Server) sessionVisible(a actor, sess store.RemoteSession) bool {
	host, err1 := s.Store.Device(sess.HostID)
	ctrl, err2 := s.Store.Device(sess.ControllerID)
	if err1 != nil || err2 != nil {
		return false
	}
	return host.OwnerID == a.User.ID || ctrl.OwnerID == a.User.ID
}

func (s *Server) sessionJSON(sess store.RemoteSession, includeTicket bool) map[string]any {
	item := map[string]any{
		"id": sess.ID, "host_id": sess.HostID, "controller_id": sess.ControllerID,
		"requested_scopes": splitScopes(sess.RequestedScopes),
		"granted_scopes":   splitScopes(sess.GrantedScopes),
		"state":            sess.State, "started_at": sess.StartedAt, "ended_at": sess.EndedAt,
		"path_summary": sess.PathSummary, "bytes_up": sess.BytesUp, "bytes_down": sess.BytesDown,
		"close_reason": sess.CloseReason,
	}
	if includeTicket && sess.State == "active" {
		host, err1 := s.Store.Device(sess.HostID)
		ctrl, err2 := s.Store.Device(sess.ControllerID)
		if err1 == nil && err2 == nil && host.TrustState == "trusted" && ctrl.TrustState == "trusted" {
			if ticket, err := s.ensureTicket(sess, host, ctrl, splitScopes(sess.GrantedScopes)); err == nil {
				item["ticket"] = ticket
			}
		}
	}
	return item
}

func (s *Server) closeSession(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	sess, err := s.Store.Session(r.PathValue("id"))
	if err != nil || !s.sessionVisible(a, sess) {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这个会话")
		return
	}
	if err := s.Store.UpdateSession(sess.ID, "closed", sess.GrantedScopes, "", "closed"); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法结束会话")
		return
	}
	s.audit(a.User.ID, "session.close", sess.ID, "ok", reqID(r), nil)
	writeJSON(w, http.StatusOK, map[string]string{"state": "closed"})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	ch, cancel := s.hub.Subscribe(a.User.ID)
	defer cancel()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			if err := conn.Write(ctx, websocket.MessageText, mustJSON(ev)); err != nil {
				return
			}
		}
	}
}

func (s *Server) redeem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ticket string `json:"ticket"`
	}
	if err := readJSON(r, &body); err != nil || body.Ticket == "" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "缺少票据")
		return
	}
	t, err := secret.VerifyTicket(s.SignPub, body.Ticket, time.Now())
	if err != nil {
		s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "票据无效")
		return
	}
	sess, err := s.Store.Session(t.SID)
	if err != nil || sess.State != "active" {
		s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "会话不可用")
		return
	}
	if sess.HostID != t.HostID || sess.ControllerID != t.ControllerID || sess.Nonce != t.Nonce {
		s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "票据与会话不符")
		return
	}
	host, err := s.Store.Device(t.HostID)
	ctrl, err2 := s.Store.Device(t.ControllerID)
	if err != nil || err2 != nil || host.TrustState != "trusted" || ctrl.TrustState != "trusted" {
		s.fail(w, r, http.StatusForbidden, "AUTH_REVOKED", "设备已被撤销")
		return
	}
	if host.NoisePublicKey != t.HostNoise || ctrl.NoisePublicKey != t.ControllerNoise {
		s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "设备密钥已变化")
		return
	}
	if err := s.Store.UseJTI(secret.JTIHash(t.JTI), time.Unix(t.Expires, 0)); err != nil {
		s.fail(w, r, http.StatusConflict, "CONFLICT", "票据已使用")
		return
	}
	_, conc, bytesLimit, _ := s.Store.Quota(host.OwnerID)
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": t.SID, "host_id": t.HostID, "controller_id": t.ControllerID,
		"scopes": t.Scopes, "expires": t.Expires, "concurrency": conc, "bytes_limit": bytesLimit,
		"host_noise": t.HostNoise, "controller_noise": t.ControllerNoise, "nonce": t.Nonce,
	})
}

func (s *Server) internalSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Store.Session(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这个会话")
		return
	}
	host, err := s.Store.Device(sess.HostID)
	if err != nil || host.TrustState != "trusted" {
		writeJSON(w, http.StatusOK, map[string]string{"state": "closed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": sess.State, "granted_scopes": splitScopes(sess.GrantedScopes)})
}
