package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"cyandesk/internal/id"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
	ErrReuse    = errors.New("store: refresh reuse")
)

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

type User struct {
	ID           string
	LoginName    string
	PasswordHash string
	Role         string
	Status       string
	TokenEpoch   int64
	CreatedAt    string
}

type Device struct {
	ID             string
	OwnerID        string
	Name           string
	OS             string
	Arch           string
	PublicKey      string
	NoisePublicKey string
	Fingerprint    string
	KeyVersion     int64
	TrustState     string
	Capabilities   string
	LastSeen       string
	RevokedAt      string
}

type Grant struct {
	ID            string
	HostID        string
	ControllerID  string
	Scopes        string
	Unattended    bool
	ExpiresAt     string
	ApprovedBy    string
	PolicyVersion int64
}

type Refresh struct {
	ID          string
	UserID      string
	DeviceID    string
	RefreshHash string
	FamilyID    string
	ExpiresAt   time.Time
}

type RemoteSession struct {
	ID              string
	HostID          string
	ControllerID    string
	RequestedScopes string
	GrantedScopes   string
	State           string
	TicketJTIHash   string
	Nonce           string
	StartedAt       string
	EndedAt         string
	PathSummary     string
	BytesUp         int64
	BytesDown       int64
	CloseReason     string
	CreatedAt       string
}

type Release struct {
	ID              string
	Channel         string
	Version         string
	MinSafeVersion  string
	EngineVersion   string
	ProtocolVersion string
	CreatedAt       string
}

type Artifact struct {
	ID           string
	ReleaseID    string
	OS           string
	Arch         string
	Size         int64
	SHA256       string
	Signature    string
	SigningKeyID string
	Status       string
	Path         string
	CreatedAt    string
}

type Audit struct {
	ID        string
	ActorID   string
	Action    string
	TargetID  string
	Result    string
	RequestID string
	Timestamp string
	Metadata  string
	Integrity string
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			login_name TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL,
			status TEXT NOT NULL,
			token_epoch INTEGER NOT NULL,
			created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS device_identities (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL,
			os TEXT NOT NULL,
			arch TEXT NOT NULL,
			public_key TEXT NOT NULL,
			noise_public_key TEXT NOT NULL,
			fingerprint TEXT NOT NULL UNIQUE,
			key_version INTEGER NOT NULL,
			trust_state TEXT NOT NULL,
			capabilities_json TEXT NOT NULL,
			last_seen TEXT,
			revoked_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS device_grants (
			id TEXT PRIMARY KEY,
			host_id TEXT NOT NULL REFERENCES device_identities(id),
			controller_id TEXT NOT NULL,
			scopes TEXT NOT NULL,
			unattended INTEGER NOT NULL,
			expires_at TEXT,
			approved_by TEXT,
			policy_version INTEGER NOT NULL,
			UNIQUE(host_id, controller_id))`,
		`CREATE TABLE IF NOT EXISTS auth_sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			device_id TEXT,
			refresh_hash TEXT NOT NULL UNIQUE,
			family_id TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT,
			created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS remote_sessions (
			id TEXT PRIMARY KEY,
			host_id TEXT NOT NULL,
			controller_id TEXT NOT NULL,
			requested_scopes TEXT NOT NULL,
			granted_scopes TEXT NOT NULL,
			state TEXT NOT NULL,
			ticket_jti_hash TEXT,
			nonce TEXT NOT NULL,
			started_at TEXT,
			ended_at TEXT,
			path_summary TEXT,
			bytes_up INTEGER NOT NULL DEFAULT 0,
			bytes_down INTEGER NOT NULL DEFAULT 0,
			close_reason TEXT,
			created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS releases (
			id TEXT PRIMARY KEY,
			channel TEXT NOT NULL,
			version TEXT NOT NULL,
			min_safe_version TEXT NOT NULL,
			engine_version TEXT NOT NULL,
			protocol_version TEXT NOT NULL,
			created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS artifacts (
			id TEXT PRIMARY KEY,
			release_id TEXT NOT NULL REFERENCES releases(id),
			os TEXT NOT NULL,
			arch TEXT NOT NULL,
			size INTEGER NOT NULL,
			sha256 TEXT NOT NULL,
			signature TEXT NOT NULL,
			signing_key_id TEXT NOT NULL,
			status TEXT NOT NULL,
			path TEXT NOT NULL,
			created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id TEXT PRIMARY KEY,
			actor_id TEXT,
			action TEXT NOT NULL,
			target_id TEXT,
			result TEXT NOT NULL,
			request_id TEXT,
			timestamp TEXT NOT NULL,
			redacted_metadata TEXT NOT NULL,
			integrity_hash TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS relay_quotas (
			account_id TEXT PRIMARY KEY,
			rate_bytes_per_sec INTEGER NOT NULL,
			concurrency INTEGER NOT NULL,
			bytes_limit INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS server_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS used_jti (jti_hash TEXT PRIMARY KEY, expires_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS revoked_access (jti TEXT PRIMARY KEY, expires_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS idempotency_keys (
			user_id TEXT NOT NULL,
			key TEXT NOT NULL,
			request_hash TEXT NOT NULL,
			status INTEGER NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (user_id, key))`,
		`CREATE TABLE IF NOT EXISTS enroll_challenges (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			nonce TEXT NOT NULL,
			expires_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS proof_nonces (
			nonce TEXT PRIMARY KEY,
			expires_at TEXT NOT NULL)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (1, ?)
		ON CONFLICT(version) DO NOTHING`, now())
	if err != nil {
		return err
	}
	for _, q := range []string{
		`ALTER TABLE remote_sessions ADD COLUMN relay_ticket TEXT`,
		`ALTER TABLE remote_sessions ADD COLUMN ticket_expires TEXT`,
	} {
		if _, alterErr := s.db.Exec(q); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column") {
			return alterErr
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (s *Store) SetMeta(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO server_meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) Meta(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v string
	err := s.db.QueryRow(`SELECT value FROM server_meta WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) CreateUser(login, hash, role string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := User{ID: id.New(), LoginName: login, PasswordHash: hash, Role: role, Status: "active", TokenEpoch: 1, CreatedAt: now()}
	_, err := s.db.Exec(`INSERT INTO users(id, login_name, password_hash, role, status, token_epoch, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, u.ID, u.LoginName, u.PasswordHash, u.Role, u.Status, u.TokenEpoch, u.CreatedAt)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return User{}, ErrConflict
	}
	if err == nil {
		_, _ = s.db.Exec(`INSERT INTO relay_quotas(account_id, rate_bytes_per_sec, concurrency, bytes_limit)
			VALUES (?, ?, ?, ?)`, u.ID, 50_000_000, 3, 5_000_000_000)
	}
	return u, err
}

func (s *Store) UserByLogin(login string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userWhere("login_name=?", login)
}

func (s *Store) UserByID(id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userWhere("id=?", id)
}

func (s *Store) userWhere(clause string, arg any) (User, error) {
	var u User
	err := s.db.QueryRow(`SELECT id, login_name, password_hash, role, status, token_epoch, created_at FROM users WHERE `+clause, arg).
		Scan(&u.ID, &u.LoginName, &u.PasswordHash, &u.Role, &u.Status, &u.TokenEpoch, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) ListUsers() ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, login_name, role, status, token_epoch, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.LoginName, &u.Role, &u.Status, &u.TokenEpoch, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetUserStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE users SET status=? WHERE id=?`, status, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if status != "active" {
		_, err = s.db.Exec(`UPDATE users SET token_epoch=token_epoch+1 WHERE id=?`, id)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, now(), id)
	}
	return err
}

func (s *Store) BumpEpoch(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE users SET token_epoch=token_epoch+1 WHERE id=?`, userID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, now(), userID)
	return err
}

func (s *Store) InsertRefresh(r Refresh) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO auth_sessions(id, user_id, device_id, refresh_hash, family_id, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, r.ID, r.UserID, r.DeviceID, r.RefreshHash, r.FamilyID, r.ExpiresAt.UTC().Format(time.RFC3339Nano), now())
	return err
}

func (s *Store) RotateRefresh(oldHash string, next Refresh) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var id, userID, family, exp, revoked sql.NullString
	err = tx.QueryRow(`SELECT id, user_id, family_id, expires_at, revoked_at FROM auth_sessions WHERE refresh_hash=?`, oldHash).
		Scan(&id, &userID, &family, &exp, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if revoked.Valid && revoked.String != "" {
		if _, err := tx.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE family_id=? AND revoked_at IS NULL`, now(), family.String); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return "", ErrReuse
	}
	t, err := time.Parse(time.RFC3339Nano, exp.String)
	if err != nil {
		return "", err
	}
	if time.Now().After(t) {
		return "", errors.New("store: refresh expired")
	}
	if _, err := tx.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE id=?`, now(), id.String); err != nil {
		return "", err
	}
	next.UserID = userID.String
	next.FamilyID = family.String
	if _, err := tx.Exec(`INSERT INTO auth_sessions(id, user_id, device_id, refresh_hash, family_id, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, next.ID, next.UserID, next.DeviceID, next.RefreshHash, next.FamilyID, next.ExpiresAt.UTC().Format(time.RFC3339Nano), now()); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return next.UserID, nil
}

func (s *Store) RevokeRefreshHash(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE refresh_hash=? AND revoked_at IS NULL`, now(), hash)
	return err
}

func (s *Store) RevokeAccess(jti string, exp time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO revoked_access(jti, expires_at) VALUES (?, ?) ON CONFLICT(jti) DO NOTHING`, jti, exp.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) AccessRevoked(jti string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var exp string
	err := s.db.QueryRow(`SELECT expires_at FROM revoked_access WHERE jti=?`, jti).Scan(&exp)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	t, err := time.Parse(time.RFC3339Nano, exp)
	if err != nil {
		return true, nil
	}
	if time.Now().After(t) {
		return false, nil
	}
	return true, nil
}

func (s *Store) InsertDevice(d Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO device_identities(id, owner_id, name, os, arch, public_key, noise_public_key, fingerprint, key_version, trust_state, capabilities_json, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.OwnerID, d.Name, d.OS, d.Arch, d.PublicKey, d.NoisePublicKey, d.Fingerprint, d.KeyVersion, d.TrustState, d.Capabilities, nil)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrConflict
	}
	return err
}

func (s *Store) Device(id string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanDevice(`SELECT id, owner_id, name, os, arch, public_key, noise_public_key, fingerprint, key_version, trust_state, capabilities_json, COALESCE(last_seen,''), COALESCE(revoked_at,'') FROM device_identities WHERE id=?`, id)
}

func (s *Store) scanDevice(q string, arg any) (Device, error) {
	var d Device
	err := s.db.QueryRow(q, arg).Scan(&d.ID, &d.OwnerID, &d.Name, &d.OS, &d.Arch, &d.PublicKey, &d.NoisePublicKey, &d.Fingerprint, &d.KeyVersion, &d.TrustState, &d.Capabilities, &d.LastSeen, &d.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

func (s *Store) ListDevices() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, owner_id, name, os, arch, public_key, noise_public_key, fingerprint, key_version, trust_state, capabilities_json, COALESCE(last_seen,''), COALESCE(revoked_at,'') FROM device_identities ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDevices(rows)
}

func (s *Store) ListDevicesByOwner(owner string) ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, owner_id, name, os, arch, public_key, noise_public_key, fingerprint, key_version, trust_state, capabilities_json, COALESCE(last_seen,''), COALESCE(revoked_at,'') FROM device_identities WHERE owner_id=? ORDER BY name`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDevices(rows)
}

func scanDevices(rows *sql.Rows) ([]Device, error) {
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.OwnerID, &d.Name, &d.OS, &d.Arch, &d.PublicKey, &d.NoisePublicKey, &d.Fingerprint, &d.KeyVersion, &d.TrustState, &d.Capabilities, &d.LastSeen, &d.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) CountTrusted(owner string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM device_identities WHERE owner_id=? AND trust_state='trusted'`, owner).Scan(&n)
	return n, err
}

func (s *Store) ApproveDevice(id, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE device_identities SET trust_state='trusted', revoked_at=NULL WHERE id=? AND fingerprint=? AND trust_state='pending'`, id, fingerprint)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) RevokeDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE device_identities SET trust_state='revoked', revoked_at=? WHERE id=? AND trust_state!='revoked'`, now(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	_, err = s.db.Exec(`UPDATE remote_sessions SET state='closed', ended_at=?, close_reason='device_revoked' WHERE state!='closed' AND (host_id=? OR controller_id=?)`, now(), id, id)
	return err
}

func (s *Store) TouchDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE device_identities SET last_seen=? WHERE id=?`, now(), id)
	return err
}

func (s *Store) UpsertGrant(g Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var current int64
	err := s.db.QueryRow(`SELECT policy_version FROM device_grants WHERE host_id=? AND controller_id=?`, g.HostID, g.ControllerID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		if g.PolicyVersion != 0 {
			return ErrConflict
		}
		g.ID = id.New()
		g.PolicyVersion = 1
		un := 0
		if g.Unattended {
			un = 1
		}
		_, err = s.db.Exec(`INSERT INTO device_grants(id, host_id, controller_id, scopes, unattended, expires_at, approved_by, policy_version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, g.ID, g.HostID, g.ControllerID, g.Scopes, un, nullIfEmpty(g.ExpiresAt), g.ApprovedBy, g.PolicyVersion)
		return err
	}
	if err != nil {
		return err
	}
	if g.PolicyVersion != current {
		return ErrConflict
	}
	un := 0
	if g.Unattended {
		un = 1
	}
	_, err = s.db.Exec(`UPDATE device_grants SET scopes=?, unattended=?, expires_at=?, approved_by=?, policy_version=? WHERE host_id=? AND controller_id=?`,
		g.Scopes, un, nullIfEmpty(g.ExpiresAt), g.ApprovedBy, current+1, g.HostID, g.ControllerID)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) GetGrant(host, controller string) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var g Grant
	var un int
	var exp sql.NullString
	err := s.db.QueryRow(`SELECT id, host_id, controller_id, scopes, unattended, expires_at, COALESCE(approved_by,''), policy_version FROM device_grants WHERE host_id=? AND controller_id=?`, host, controller).
		Scan(&g.ID, &g.HostID, &g.ControllerID, &g.Scopes, &un, &exp, &g.ApprovedBy, &g.PolicyVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	g.Unattended = un == 1
	if exp.Valid {
		g.ExpiresAt = exp.String
	}
	return g, err
}

func (s *Store) ListGrants(host string) ([]Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, host_id, controller_id, scopes, unattended, expires_at, COALESCE(approved_by,''), policy_version FROM device_grants WHERE host_id=?`, host)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		var un int
		var exp sql.NullString
		if err := rows.Scan(&g.ID, &g.HostID, &g.ControllerID, &g.Scopes, &un, &exp, &g.ApprovedBy, &g.PolicyVersion); err != nil {
			return nil, err
		}
		g.Unattended = un == 1
		if exp.Valid {
			g.ExpiresAt = exp.String
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CreateSession(sess RemoteSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO remote_sessions(id, host_id, controller_id, requested_scopes, granted_scopes, state, ticket_jti_hash, nonce, started_at, path_summary, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.HostID, sess.ControllerID, sess.RequestedScopes, sess.GrantedScopes, sess.State, sess.TicketJTIHash, sess.Nonce, nullIfEmpty(sess.StartedAt), sess.PathSummary, sess.CreatedAt)
	return err
}

func (s *Store) Session(id string) (RemoteSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanSession(id)
}

func (s *Store) scanSession(id string) (RemoteSession, error) {
	var sess RemoteSession
	var started, ended, reason, jti sql.NullString
	err := s.db.QueryRow(`SELECT id, host_id, controller_id, requested_scopes, granted_scopes, state, ticket_jti_hash, nonce, started_at, ended_at, COALESCE(path_summary,''), bytes_up, bytes_down, close_reason, created_at FROM remote_sessions WHERE id=?`, id).
		Scan(&sess.ID, &sess.HostID, &sess.ControllerID, &sess.RequestedScopes, &sess.GrantedScopes, &sess.State, &jti, &sess.Nonce, &started, &ended, &sess.PathSummary, &sess.BytesUp, &sess.BytesDown, &reason, &sess.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RemoteSession{}, ErrNotFound
	}
	if jti.Valid {
		sess.TicketJTIHash = jti.String
	}
	if started.Valid {
		sess.StartedAt = started.String
	}
	if ended.Valid {
		sess.EndedAt = ended.String
	}
	if reason.Valid {
		sess.CloseReason = reason.String
	}
	return sess, err
}

func (s *Store) ListSessions() ([]RemoteSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id FROM remote_sessions ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]RemoteSession, 0, len(ids))
	for _, id := range ids {
		sess, err := s.scanSession(id)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, nil
}

func (s *Store) UpdateSession(id, state, granted, jtiHash, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	started := ""
	ended := ""
	if state == "active" {
		started = now()
	}
	if state == "closed" || state == "denied" {
		ended = now()
	}
	res, err := s.db.Exec(`UPDATE remote_sessions SET state=?, granted_scopes=?, ticket_jti_hash=COALESCE(NULLIF(?,''), ticket_jti_hash), started_at=COALESCE(started_at, NULLIF(?, '')), ended_at=COALESCE(NULLIF(?,''), ended_at), close_reason=COALESCE(NULLIF(?,''), close_reason) WHERE id=?`,
		state, granted, jtiHash, started, ended, reason, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UseJTI(hash string, exp time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`INSERT INTO used_jti(jti_hash, expires_at) VALUES (?, ?) ON CONFLICT(jti_hash) DO NOTHING`, hash, exp.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) ActiveSessionsFor(accountDeviceIDs []string) (int, error) {
	if len(accountDeviceIDs) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	placeholders := make([]string, len(accountDeviceIDs))
	args := make([]any, len(accountDeviceIDs))
	for i, id := range accountDeviceIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT COUNT(*) FROM remote_sessions WHERE state='active' AND host_id IN (` + strings.Join(placeholders, ",") + `)`
	var n int
	err := s.db.QueryRow(q, args...).Scan(&n)
	return n, err
}

func (s *Store) HostsGrantedTo(controllerIDs []string) ([]Device, error) {
	if len(controllerIDs) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	placeholders := make([]string, len(controllerIDs))
	args := make([]any, len(controllerIDs))
	for i, id := range controllerIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT d.id, d.owner_id, d.name, d.os, d.arch, d.public_key, d.noise_public_key, d.fingerprint, d.key_version, d.trust_state, d.capabilities_json, COALESCE(d.last_seen,''), COALESCE(d.revoked_at,'')
		FROM device_identities d JOIN device_grants g ON g.host_id=d.id
		WHERE g.controller_id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDevices(rows)
}

func (s *Store) DeviceIDsByOwner(owner string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id FROM device_identities WHERE owner_id=?`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) Quota(account string) (rate, conc, bytes int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.db.QueryRow(`SELECT rate_bytes_per_sec, concurrency, bytes_limit FROM relay_quotas WHERE account_id=?`, account).Scan(&rate, &conc, &bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return 50_000_000, 3, 5_000_000_000, nil
	}
	return rate, conc, bytes, err
}

func (s *Store) SetQuota(account string, rate, conc, bytes int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO relay_quotas(account_id, rate_bytes_per_sec, concurrency, bytes_limit) VALUES (?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET rate_bytes_per_sec=excluded.rate_bytes_per_sec, concurrency=excluded.concurrency, bytes_limit=excluded.bytes_limit`,
		account, rate, conc, bytes)
	return err
}

func (s *Store) AppendAudit(actor, action, target, result, requestID string, meta map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	redacted := map[string]string{}
	for k, v := range meta {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "password") || strings.Contains(lk, "secret") || strings.Contains(lk, "token") || strings.Contains(lk, "private") || strings.Contains(lk, "authorization") {
			continue
		}
		redacted[k] = v
	}
	buf, _ := json.Marshal(redacted)
	var prev string
	_ = s.db.QueryRow(`SELECT integrity_hash FROM audit_events ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&prev)
	sum := sha256.Sum256([]byte(prev + "|" + actor + "|" + action + "|" + target + "|" + result + "|" + requestID + "|" + string(buf)))
	_, err := s.db.Exec(`INSERT INTO audit_events(id, actor_id, action, target_id, result, request_id, timestamp, redacted_metadata, integrity_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id.New(), actor, action, target, result, requestID, now(), string(buf), hex.EncodeToString(sum[:]))
	return err
}

func (s *Store) ListAudit(limit int) ([]Audit, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, COALESCE(actor_id,''), action, COALESCE(target_id,''), result, COALESCE(request_id,''), timestamp, redacted_metadata, integrity_hash FROM audit_events ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Audit
	for rows.Next() {
		var a Audit
		if err := rows.Scan(&a.ID, &a.ActorID, &a.Action, &a.TargetID, &a.Result, &a.RequestID, &a.Timestamp, &a.Metadata, &a.Integrity); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) PutIdempotency(userID, key, hash string, status int, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO idempotency_keys(user_id, key, request_hash, status, body, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, key, hash, status, string(body), now())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrConflict
	}
	return err
}

func (s *Store) Idempotency(userID, key string) (hash string, status int, body string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.db.QueryRow(`SELECT request_hash, status, body FROM idempotency_keys WHERE user_id=? AND key=?`, userID, key).Scan(&hash, &status, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, "", ErrNotFound
	}
	return hash, status, body, err
}

func (s *Store) SaveChallenge(userID, nonce string, exp time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cid := id.New()
	_, err := s.db.Exec(`INSERT INTO enroll_challenges(id, user_id, nonce, expires_at) VALUES (?, ?, ?, ?)`, cid, userID, nonce, exp.UTC().Format(time.RFC3339Nano))
	return cid, err
}

func (s *Store) TakeChallenge(id, userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var nonce, exp string
	err := s.db.QueryRow(`SELECT nonce, expires_at FROM enroll_challenges WHERE id=? AND user_id=?`, id, userID).Scan(&nonce, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	t, err := time.Parse(time.RFC3339Nano, exp)
	if err != nil {
		return "", err
	}
	if time.Now().After(t) {
		return "", errors.New("store: challenge expired")
	}
	if _, err := s.db.Exec(`DELETE FROM enroll_challenges WHERE id=?`, id); err != nil {
		return "", err
	}
	return nonce, nil
}

func (s *Store) SeenProofNonce(nonce string, exp time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`INSERT INTO proof_nonces(nonce, expires_at) VALUES (?, ?) ON CONFLICT(nonce) DO NOTHING`, nonce, exp.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) CreateRelease(r Release) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO releases(id, channel, version, min_safe_version, engine_version, protocol_version, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Channel, r.Version, r.MinSafeVersion, r.EngineVersion, r.ProtocolVersion, r.CreatedAt)
	return err
}

func (s *Store) AddArtifact(a Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO artifacts(id, release_id, os, arch, size, sha256, signature, signing_key_id, status, path, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ReleaseID, a.OS, a.Arch, a.Size, a.SHA256, a.Signature, a.SigningKeyID, a.Status, a.Path, a.CreatedAt)
	return err
}

func (s *Store) Artifact(id string) (Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a Artifact
	err := s.db.QueryRow(`SELECT id, release_id, os, arch, size, sha256, signature, signing_key_id, status, path, created_at FROM artifacts WHERE id=?`, id).
		Scan(&a.ID, &a.ReleaseID, &a.OS, &a.Arch, &a.Size, &a.SHA256, &a.Signature, &a.SigningKeyID, &a.Status, &a.Path, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	return a, err
}

func (s *Store) ListArtifacts() ([]Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, release_id, os, arch, size, sha256, signature, signing_key_id, status, path, created_at FROM artifacts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.ReleaseID, &a.OS, &a.Arch, &a.Size, &a.SHA256, &a.Signature, &a.SigningKeyID, &a.Status, &a.Path, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) FindArtifact(osName, arch, channel string) (Artifact, Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a Artifact
	var r Release
	err := s.db.QueryRow(`SELECT a.id, a.release_id, a.os, a.arch, a.size, a.sha256, a.signature, a.signing_key_id, a.status, a.path, a.created_at,
		r.id, r.channel, r.version, r.min_safe_version, r.engine_version, r.protocol_version, r.created_at
		FROM artifacts a JOIN releases r ON r.id=a.release_id
		WHERE a.os=? AND a.arch=? AND r.channel=? AND a.status!='revoked'
		ORDER BY r.created_at DESC LIMIT 1`, osName, arch, channel).Scan(
		&a.ID, &a.ReleaseID, &a.OS, &a.Arch, &a.Size, &a.SHA256, &a.Signature, &a.SigningKeyID, &a.Status, &a.Path, &a.CreatedAt,
		&r.ID, &r.Channel, &r.Version, &r.MinSafeVersion, &r.EngineVersion, &r.ProtocolVersion, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, Release{}, ErrNotFound
	}
	return a, r, err
}

func (s *Store) Release(id string) (Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r Release
	err := s.db.QueryRow(`SELECT id, channel, version, min_safe_version, engine_version, protocol_version, created_at FROM releases WHERE id=?`, id).
		Scan(&r.ID, &r.Channel, &r.Version, &r.MinSafeVersion, &r.EngineVersion, &r.ProtocolVersion, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Release{}, ErrNotFound
	}
	return r, err
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) JTIUsed(hash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM used_jti WHERE jti_hash=?`, hash).Scan(&n)
	return n > 0, err
}

func (s *Store) RelayTicket(id string) (raw, hash, exp string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ticket, jti, expires sql.NullString
	err = s.db.QueryRow(`SELECT relay_ticket, ticket_jti_hash, ticket_expires FROM remote_sessions WHERE id=?`, id).Scan(&ticket, &jti, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", ErrNotFound
	}
	if err != nil {
		return "", "", "", err
	}
	if ticket.Valid {
		raw = ticket.String
	}
	if jti.Valid {
		hash = jti.String
	}
	if expires.Valid {
		exp = expires.String
	}
	return raw, hash, exp, nil
}

func (s *Store) SaveRelayTicket(id, raw, hash, exp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE remote_sessions SET relay_ticket=?, ticket_jti_hash=?, ticket_expires=? WHERE id=?`, raw, hash, exp, id)
	return err
}
