package api

import (
	"net/http"
	"time"

	"cyandesk/internal/id"
	"cyandesk/internal/secret"
	"cyandesk/internal/store"
)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.lim.allow("login|ip|"+ip, 10, time.Minute, time.Now()) {
		s.fail(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "尝试过于频繁")
		return
	}
	var body struct {
		LoginName string `json:"login_name"`
		Password  string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "请求格式不正确")
		return
	}
	if !s.lim.allow("login|user|"+body.LoginName, 8, time.Minute, time.Now()) {
		s.fail(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "尝试过于频繁")
		return
	}
	u, err := s.Store.UserByLogin(body.LoginName)
	hash := s.dummyPassword
	if err == nil {
		hash = u.PasswordHash
	}
	if secret.VerifyPassword(hash, body.Password) != nil || err != nil || u.Status != "active" {
		s.audit("", "login", "", "denied", reqID(r), map[string]string{"login_name": body.LoginName})
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "账号或密码错误")
		return
	}
	s.issue(w, r, u)
	s.audit(u.ID, "login", u.ID, "ok", reqID(r), map[string]string{"login_name": u.LoginName})
}

func (s *Server) issue(w http.ResponseWriter, r *http.Request, u store.User) {
	now := time.Now()
	jti, err := secret.RandomToken(16)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法签发令牌")
		return
	}
	access, err := secret.SignAccess(s.MAC, secret.AccessClaims{
		Subject: u.ID, Role: u.Role, Epoch: u.TokenEpoch,
		Issued: now.Unix(), Expires: now.Add(s.AccessTTL).Unix(), JTI: jti,
	})
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法签发令牌")
		return
	}
	refresh, err := secret.RandomToken(32)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法签发令牌")
		return
	}
	family, err := secret.RandomToken(16)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法签发令牌")
		return
	}
	if err := s.Store.InsertRefresh(store.Refresh{
		ID: id.New(), UserID: u.ID, RefreshHash: secret.HashToken(refresh), FamilyID: family,
		ExpiresAt: now.Add(s.RefreshTTL),
	}); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法保存会话")
		return
	}
	web := r.Header.Get("X-CyanDesk-Client") == "web"
	if web {
		csrf, err := secret.RandomToken(18)
		if err != nil {
			s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法签发令牌")
			return
		}
		s.setCookie(w, accessCookie, access, s.AccessTTL, true)
		s.setCookie(w, refreshCookie, refresh, s.RefreshTTL, true)
		s.setCookie(w, csrfCookie, csrf, s.RefreshTTL, false)
		writeJSON(w, http.StatusOK, map[string]any{
			"role": u.Role, "user_id": u.ID, "expires_in": int(s.AccessTTL.Seconds()),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "refresh_token": refresh, "token_type": "Bearer",
		"expires_in": int(s.AccessTTL.Seconds()), "role": u.Role, "user_id": u.ID,
	})
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: httpOnly, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = readJSON(r, &body)
	token := body.RefreshToken
	if token == "" {
		if c, err := r.Cookie(refreshCookie); err == nil {
			token = c.Value
		}
	}
	if token == "" {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "登录已失效")
		return
	}
	nextRaw, err := secret.RandomToken(32)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法轮换令牌")
		return
	}
	next := store.Refresh{ID: id.New(), RefreshHash: secret.HashToken(nextRaw), ExpiresAt: time.Now().Add(s.RefreshTTL)}
	userID, err := s.Store.RotateRefresh(secret.HashToken(token), next)
	if err != nil {
		s.audit("", "refresh", "", "denied", reqID(r), nil)
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "登录已失效")
		return
	}
	u, err := s.Store.UserByID(userID)
	if err != nil || u.Status != "active" {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "登录已失效")
		return
	}
	now := time.Now()
	jti, _ := secret.RandomToken(16)
	access, err := secret.SignAccess(s.MAC, secret.AccessClaims{
		Subject: u.ID, Role: u.Role, Epoch: u.TokenEpoch, Issued: now.Unix(), Expires: now.Add(s.AccessTTL).Unix(), JTI: jti,
	})
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法签发令牌")
		return
	}
	if r.Header.Get("X-CyanDesk-Client") == "web" || body.RefreshToken == "" {
		s.setCookie(w, accessCookie, access, s.AccessTTL, true)
		s.setCookie(w, refreshCookie, nextRaw, s.RefreshTTL, true)
		writeJSON(w, http.StatusOK, map[string]any{"expires_in": int(s.AccessTTL.Seconds()), "role": u.Role, "user_id": u.ID})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "refresh_token": nextRaw, "token_type": "Bearer",
		"expires_in": int(s.AccessTTL.Seconds()), "role": u.Role, "user_id": u.ID,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = readJSON(r, &body)
	token := body.RefreshToken
	if token == "" {
		if c, err := r.Cookie(refreshCookie); err == nil {
			token = c.Value
		}
	}
	if token != "" {
		_ = s.Store.RevokeRefreshHash(secret.HashToken(token))
	}
	_ = s.Store.RevokeAccess(a.Claims.JTI, time.Unix(a.Claims.Expires, 0))
	s.clearCookie(w, accessCookie)
	s.clearCookie(w, refreshCookie)
	s.clearCookie(w, csrfCookie)
	s.audit(a.User.ID, "logout", a.User.ID, "ok", reqID(r), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed_out"})
}

func (s *Server) logoutAll(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if err := s.Store.BumpEpoch(a.User.ID); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法注销")
		return
	}
	_ = s.Store.RevokeAccess(a.Claims.JTI, time.Unix(a.Claims.Expires, 0))
	s.clearCookie(w, accessCookie)
	s.clearCookie(w, refreshCookie)
	s.audit(a.User.ID, "logout_all", a.User.ID, "ok", reqID(r), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed_out"})
}
