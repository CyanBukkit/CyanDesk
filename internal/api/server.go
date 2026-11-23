package api

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"cyandesk/internal/secret"
	"cyandesk/internal/store"
)

const (
	accessCookie  = "cyandesk_access"
	refreshCookie = "cyandesk_refresh"
	csrfCookie    = "cyandesk_csrf"
	csrfHeader    = "X-CyanDesk-CSRF"
)

type Server struct {
	Store         *store.Store
	Material      *secret.Material
	SignPriv      ed25519.PrivateKey
	SignPub       ed25519.PublicKey
	UpdatePriv    ed25519.PrivateKey
	UpdatePub     ed25519.PublicKey
	MAC           []byte
	GatewayTCP    string
	GatewayUDP    string
	ArtifactsDir  string
	Log           *log.Logger
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	TicketTTL     time.Duration
	OnlineTTL     time.Duration
	dummyPassword string
	lim           *limiter
	hub           *Hub
	ticketMu      sync.Mutex
}

func New(st *store.Store, mat *secret.Material, artifacts string, logger *log.Logger) (*Server, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	priv, pub, err := mat.SignKey()
	if err != nil {
		return nil, err
	}
	up, uq, err := mat.UpdateKey()
	if err != nil {
		return nil, err
	}
	mac, err := mat.MAC()
	if err != nil {
		return nil, err
	}
	dummy, err := secret.HashPassword("dummy-password-not-used")
	if err != nil {
		return nil, err
	}
	return &Server{
		Store: st, Material: mat, SignPriv: priv, SignPub: pub, UpdatePriv: up, UpdatePub: uq, MAC: mac,
		ArtifactsDir: artifacts, Log: logger,
		AccessTTL: 15 * time.Minute, RefreshTTL: 30 * 24 * time.Hour, TicketTTL: 2 * time.Minute, OnlineTTL: 30 * time.Second,
		dummyPassword: dummy, lim: newLimiter(), hub: newHub(),
	}, nil
}

func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /api/v1/server-info", s.serverInfo)
	mux.HandleFunc("GET /api/v1/setup-status", s.setupStatus)
	mux.HandleFunc("POST /api/v1/admin/bootstrap", s.bootstrap)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", s.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("POST /api/v1/auth/logout-all", s.logoutAll)
	mux.HandleFunc("POST /api/v1/devices/challenge", s.challenge)
	mux.HandleFunc("POST /api/v1/devices/enroll", s.enroll)
	mux.HandleFunc("GET /api/v1/devices", s.listDevices)
	mux.HandleFunc("GET /api/v1/devices/{id}", s.getDevice)
	mux.HandleFunc("POST /api/v1/devices/{id}/approve", s.approveDevice)
	mux.HandleFunc("POST /api/v1/devices/{id}/revoke", s.revokeDevice)
	mux.HandleFunc("POST /api/v1/devices/{id}/heartbeat", s.heartbeat)
	mux.HandleFunc("GET /api/v1/devices/{id}/grants", s.listGrants)
	mux.HandleFunc("PUT /api/v1/devices/{id}/grants", s.putGrant)
	mux.HandleFunc("POST /api/v1/sessions", s.createSession)
	mux.HandleFunc("GET /api/v1/sessions", s.listMySessions)
	mux.HandleFunc("GET /api/v1/sessions/{id}", s.getSession)
	mux.HandleFunc("POST /api/v1/sessions/{id}/consent", s.consent)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", s.closeSession)
	mux.HandleFunc("GET /api/v1/events", s.events)
	mux.HandleFunc("GET /api/v1/releases/check", s.releaseCheck)
	mux.HandleFunc("GET /api/v1/releases/trust", s.releaseTrust)
	mux.HandleFunc("GET /downloads/{id}", s.download)
	mux.HandleFunc("GET /api/v1/admin/users", s.adminUsers)
	mux.HandleFunc("POST /api/v1/admin/users", s.adminCreateUser)
	mux.HandleFunc("POST /api/v1/admin/users/{id}/status", s.adminUserStatus)
	mux.HandleFunc("GET /api/v1/admin/devices", s.adminDevices)
	mux.HandleFunc("POST /api/v1/admin/devices/{id}/revoke", s.adminRevokeDevice)
	mux.HandleFunc("GET /api/v1/admin/sessions", s.adminSessions)
	mux.HandleFunc("DELETE /api/v1/admin/sessions/{id}", s.adminCloseSession)
	mux.HandleFunc("GET /api/v1/admin/audit", s.adminAudit)
	mux.HandleFunc("GET /api/v1/admin/quotas/{id}", s.adminGetQuota)
	mux.HandleFunc("PUT /api/v1/admin/quotas/{id}", s.adminPutQuota)
	mux.HandleFunc("POST /api/v1/admin/releases", s.adminUploadRelease)
	mux.HandleFunc("GET /api/v1/admin/releases", s.adminListReleases)
	return s.wrap(mux)
}

func (s *Server) InternalHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/internal/tickets/redeem", s.redeem)
	mux.HandleFunc("GET /api/v1/internal/sessions/{id}", s.internalSession)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.internalAuth(r) {
			s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "内部凭据无效")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = secretID()
		}
		w.Header().Set("X-Request-Id", id)
		r = r.WithContext(withReqID(r.Context(), id))
		ww := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		path := r.URL.Path
		s.Log.Printf("request_id=%s method=%s path=%s status=%d dur=%s", id, r.Method, path, ww.status, time.Since(start).Round(time.Millisecond))
	})
}

func (s *Server) internalAuth(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == r.Header.Get("Authorization") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.Material.InternalToken)) == 1
}

func secretID() string {
	tok, err := secret.RandomToken(12)
	if err != nil {
		return "req"
	}
	return tok
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("api: connection cannot be hijacked")
	}
	return h.Hijack()
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "live"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Ping(r.Context()); err != nil {
		s.fail(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "数据库不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) serverInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id":        s.Material.ServerID,
		"api_version":      "v1",
		"protocol_version": "cyandesk/1",
		"min_safe_client":  "0.1.0",
		"cert_fingerprint": s.Material.CertFingerprint,
		"endpoints": []map[string]any{
			{"proto": "tcp", "port": 13110, "addr": s.GatewayTCP},
			{"proto": "udp", "port": 13110, "addr": s.GatewayUDP},
		},
	})
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.Meta("bootstrapped")
	writeJSON(w, http.StatusOK, map[string]bool{"bootstrapped": err == nil && v == "1"})
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	if !s.lim.allow("bootstrap|"+clientIP(r), 5, time.Minute, time.Now()) {
		s.fail(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "尝试过于频繁")
		return
	}
	flag, err := s.Store.Meta("bootstrapped")
	if err == nil && flag == "1" {
		s.fail(w, r, http.StatusConflict, "CONFLICT", "服务器已经初始化")
		return
	}
	var body struct {
		Token     string `json:"token"`
		LoginName string `json:"login_name"`
		Password  string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "请求格式不正确")
		return
	}
	want, err := s.Store.Meta("bootstrap_hash")
	if err != nil || subtle.ConstantTimeCompare([]byte(secret.HashToken(body.Token)), []byte(want)) != 1 {
		s.audit("", "bootstrap", "", "denied", reqID(r), nil)
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "引导凭据无效")
		return
	}
	if !validLogin(body.LoginName) || !validPassword(body.Password) {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "账号需 3–64 位，密码至少 12 位")
		return
	}
	hash, err := secret.HashPassword(body.Password)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法保存密码")
		return
	}
	u, err := s.Store.CreateUser(body.LoginName, hash, "admin")
	if err != nil {
		s.fail(w, r, http.StatusConflict, "CONFLICT", "无法创建管理员")
		return
	}
	_ = s.Store.SetMeta("bootstrapped", "1")
	_ = s.Store.SetMeta("bootstrap_hash", "")
	s.audit(u.ID, "bootstrap", u.ID, "ok", reqID(r), map[string]string{"login_name": u.LoginName})
	writeJSON(w, http.StatusCreated, map[string]string{"user_id": u.ID, "role": "admin"})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func readJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{
		"code": code, "message": msg, "request_id": reqID(r),
	}})
}

func (s *Server) audit(actor, action, target, result, requestID string, meta map[string]string) {
	_ = s.Store.AppendAudit(actor, action, target, result, requestID, meta)
}

func validLogin(s string) bool {
	if len([]rune(s)) < 3 || len([]rune(s)) > 64 {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

func validPassword(s string) bool {
	n := len([]rune(s))
	return n >= 12 && n <= 128
}

type actor struct {
	User    store.User
	Claims  secret.AccessClaims
	RawBody []byte
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (actor, bool) {
	token := bearer(r)
	viaCookie := false
	if token == "" {
		if c, err := r.Cookie(accessCookie); err == nil {
			token = c.Value
			viaCookie = true
		}
	}
	if token == "" {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "需要登录")
		return actor{}, false
	}
	claims, err := secret.ParseAccess(s.MAC, token, time.Now())
	if err != nil {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "登录已失效")
		return actor{}, false
	}
	revoked, err := s.Store.AccessRevoked(claims.JTI)
	if err != nil || revoked {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "登录已失效")
		return actor{}, false
	}
	u, err := s.Store.UserByID(claims.Subject)
	if err != nil || u.Status != "active" || u.TokenEpoch != claims.Epoch {
		s.fail(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "登录已失效")
		return actor{}, false
	}
	if viaCookie {
		csrf, _ := r.Cookie(csrfCookie)
		if csrf == nil || subtle.ConstantTimeCompare([]byte(csrf.Value), []byte(r.Header.Get(csrfHeader))) != 1 {
			s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "缺少 CSRF 校验")
			return actor{}, false
		}
	}
	if u.Role != "admin" && strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
		s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "需要管理员")
		return actor{}, false
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	r.Body = io.NopCloser(bytes.NewReader(body))
	return actor{User: u, Claims: claims, RawBody: body}, true
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (actor, bool) {
	a, ok := s.authenticate(w, r)
	if !ok {
		return actor{}, false
	}
	if a.User.Role != "admin" {
		s.fail(w, r, http.StatusForbidden, "FORBIDDEN", "需要管理员")
		return actor{}, false
	}
	return a, true
}
