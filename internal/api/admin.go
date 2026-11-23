package api

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"cyandesk/internal/id"
	"cyandesk/internal/secret"
	"cyandesk/internal/store"
)

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	_ = a
	users, err := s.Store.ListUsers()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法读取账户")
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]any{
			"id": u.ID, "login_name": u.LoginName, "role": u.Role, "status": u.Status, "created_at": u.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		LoginName string `json:"login_name"`
		Password  string `json:"password"`
		Role      string `json:"role"`
	}
	if err := readJSON(r, &body); err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "账户内容不正确")
		return
	}
	if body.Role == "" {
		body.Role = "user"
	}
	if body.Role != "user" && body.Role != "admin" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "角色无效")
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
	u, err := s.Store.CreateUser(body.LoginName, hash, body.Role)
	if err != nil {
		s.fail(w, r, http.StatusConflict, "CONFLICT", "账号已存在")
		return
	}
	s.audit(a.User.ID, "admin.user.create", u.ID, "ok", reqID(r), map[string]string{"login_name": u.LoginName, "role": u.Role})
	writeJSON(w, http.StatusCreated, map[string]string{"user_id": u.ID, "role": u.Role})
}

func (s *Server) adminUserStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := readJSON(r, &body); err != nil || (body.Status != "active" && body.Status != "disabled") {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "状态无效")
		return
	}
	id := r.PathValue("id")
	if id == a.User.ID && body.Status != "active" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "不能停用当前管理员自己")
		return
	}
	if err := s.Store.SetUserStatus(id, body.Status); err != nil {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这个账户")
		return
	}
	s.audit(a.User.ID, "admin.user.status", id, body.Status, reqID(r), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": body.Status})
}

func (s *Server) adminDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	list, err := s.Store.ListDevices()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法读取设备")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, d := range list {
		out = append(out, s.deviceJSON(d, true))
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (s *Server) adminRevokeDevice(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	d, err := s.Store.Device(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这台设备")
		return
	}
	s.finishRevoke(w, r, a.User.ID, d)
}

func (s *Server) adminSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	list, err := s.Store.ListSessions()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法读取会话")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, sess := range list {
		out = append(out, s.sessionJSON(sess, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) adminCloseSession(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	sess, err := s.Store.Session(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这个会话")
		return
	}
	if err := s.Store.UpdateSession(sess.ID, "closed", sess.GrantedScopes, "", "admin_closed"); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法结束会话")
		return
	}
	s.audit(a.User.ID, "admin.session.close", sess.ID, "ok", reqID(r), nil)
	writeJSON(w, http.StatusOK, map[string]string{"state": "closed"})
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.Store.ListAudit(limit)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法读取审计")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": rows})
}

func (s *Server) adminGetQuota(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	rate, conc, bytes, err := s.Store.Quota(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法读取限额")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rate_bytes_per_sec": rate, "concurrency": conc, "bytes_limit": bytes})
}

func (s *Server) adminPutQuota(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Rate        int64 `json:"rate_bytes_per_sec"`
		Concurrency int64 `json:"concurrency"`
		BytesLimit  int64 `json:"bytes_limit"`
	}
	if err := readJSON(r, &body); err != nil || body.Concurrency < 1 || body.BytesLimit < 1 || body.Rate < 1 {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "限额无效")
		return
	}
	if err := s.Store.SetQuota(r.PathValue("id"), body.Rate, body.Concurrency, body.BytesLimit); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法保存限额")
		return
	}
	s.audit(a.User.ID, "admin.quota", r.PathValue("id"), "ok", reqID(r), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) adminUploadRelease(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "上传格式不正确")
		return
	}
	status := r.FormValue("status")
	if status != "public" && status != "private" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "发布状态只能是 public 或 private")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "缺少文件")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64<<20))
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "无法读取文件")
		return
	}
	rel := store.Release{
		ID: id.New(), Channel: r.FormValue("channel"), Version: r.FormValue("version"),
		MinSafeVersion: r.FormValue("min_safe_version"), EngineVersion: r.FormValue("engine_version"),
		ProtocolVersion: "cyandesk/1", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if rel.Channel == "" || rel.Version == "" || rel.MinSafeVersion == "" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "版本信息不完整")
		return
	}
	if err := s.Store.CreateRelease(rel); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法登记版本")
		return
	}
	if err := os.MkdirAll(s.ArtifactsDir, 0o750); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法保存制品")
		return
	}
	artID := id.New()
	path := filepath.Join(s.ArtifactsDir, artID+".bin")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法保存制品")
		return
	}
	sum := sha256.Sum256(data)
	shaHex := hexEncode(sum[:])
	sig := ed25519.Sign(s.UpdatePriv, artifactMessage(shaHex, artID))
	art := store.Artifact{
		ID: artID, ReleaseID: rel.ID, OS: r.FormValue("os"), Arch: r.FormValue("arch"),
		Size: int64(len(data)), SHA256: shaHex, Signature: base64.RawURLEncoding.EncodeToString(sig),
		SigningKeyID: s.Material.UpdateKeyID, Status: status, Path: path,
		CreatedAt: rel.CreatedAt,
	}
	if art.OS == "" || art.Arch == "" {
		s.fail(w, r, http.StatusBadRequest, "BAD_REQUEST", "需要操作系统和架构")
		return
	}
	if err := s.Store.AddArtifact(art); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法登记制品")
		return
	}
	s.audit(a.User.ID, "admin.release", art.ID, "ok", reqID(r), map[string]string{"sha256": shaHex, "status": status, "version": rel.Version})
	writeJSON(w, http.StatusCreated, manifest(rel, art))
}

func artifactMessage(sha, id string) []byte {
	return []byte("cyandesk-artifact-v1\n" + sha + "\n" + id + "\n")
}

func manifest(rel store.Release, art store.Artifact) map[string]any {
	return map[string]any{
		"product": "cyandesk", "version": rel.Version, "channel": rel.Channel,
		"os": art.OS, "arch": art.Arch, "protocol_version": rel.ProtocolVersion,
		"engine_version": rel.EngineVersion, "min_safe_version": rel.MinSafeVersion,
		"artifact_id": art.ID, "size": art.Size, "sha256": art.SHA256,
		"signature": art.Signature, "signing_key_id": art.SigningKeyID, "status": art.Status,
	}
}

func (s *Server) adminListReleases(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	arts, err := s.Store.ListArtifacts()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "INTERNAL", "无法读取版本")
		return
	}
	out := make([]map[string]any, 0, len(arts))
	for _, art := range arts {
		rel, err := s.Store.Release(art.ReleaseID)
		if err != nil {
			continue
		}
		out = append(out, manifest(rel, art))
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": out})
}

func (s *Server) releaseCheck(w http.ResponseWriter, r *http.Request) {
	osName := r.URL.Query().Get("os")
	arch := r.URL.Query().Get("arch")
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "stable"
	}
	art, rel, err := s.Store.FindArtifact(osName, arch, channel)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有匹配的版本")
		return
	}
	item := manifest(rel, art)
	item["download_path"] = "/downloads/" + art.ID
	if art.Status != "public" {
		if _, ok := s.authenticate(w, r); !ok {
			return
		}
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) releaseTrust(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"update_key_id":     s.Material.UpdateKeyID,
		"update_public_key": s.Material.UpdatePublic,
	})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	art, err := s.Store.Artifact(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "没有这个文件")
		return
	}
	if art.Status != "public" {
		if _, ok := s.authenticate(w, r); !ok {
			return
		}
	}
	if art.Status == "revoked" {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "文件已撤回")
		return
	}
	clean := filepath.Clean(art.Path)
	root := filepath.Clean(s.ArtifactsDir)
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == "../" {
		s.fail(w, r, http.StatusNotFound, "NOT_FOUND", "文件不在制品目录")
		return
	}
	http.ServeFile(w, r, clean)
}

// VerifyArtifact reports whether sig is the update key's signature over sha and id.
func VerifyArtifact(pub ed25519.PublicKey, shaHex, id, sigB64 string) bool {
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, artifactMessage(shaHex, id), sig)
}
