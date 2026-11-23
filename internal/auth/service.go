package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"coursefin/internal/id"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrAlreadySetup       = errors.New("admin user already configured")
	ErrNotSetup           = errors.New("admin user not configured")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidInput       = errors.New("invalid input")
)

// User represents the single CourseFin administrator account.
type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// DeviceInfo captures Jellyfin/Web client device identification metadata.
type DeviceInfo struct {
	DeviceID      string `json:"device_id"`
	DeviceName    string `json:"device_name"`
	ClientName    string `json:"client_name"`
	ClientVersion string `json:"client_version"`
}

// Session represents an authenticated device session.
type Session struct {
	Token         string `json:"token"`
	UserID        string `json:"user_id"`
	DeviceID      string `json:"device_id"`
	DeviceName    string `json:"device_name"`
	ClientName    string `json:"client_name"`
	ClientVersion string `json:"client_version"`
	LastActiveAt  string `json:"last_active_at"`
	CreatedAt     string `json:"created_at"`
}

// Service manages single-admin setup, authentication, and multi-device sessions.
type Service struct {
	db *sql.DB
}

// NewService creates an auth Service backed by the given SQLite connection.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// NeedsSetup returns true when no admin account exists yet.
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

// SetupAdmin creates the single admin account on a fresh database.
func (s *Service) SetupAdmin(ctx context.Context, username, password string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, ErrInvalidInput
	}

	hashBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrAlreadySetup
	}

	now := time.Now().UTC().Format(time.RFC3339)
	u := &User{
		ID:           id.New(),
		Username:     username,
		PasswordHash: string(hashBytes),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.PasswordHash, u.CreatedAt, u.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return u, nil
}

// EnsureEnvAdmin seeds the admin user from environment credentials when the database
// has no users yet. If an admin already exists or credentials are empty, it is a no-op.
func (s *Service) EnsureEnvAdmin(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil
	}
	needs, err := s.NeedsSetup(ctx)
	if err != nil {
		return err
	}
	if !needs {
		return nil
	}
	_, err = s.SetupAdmin(ctx, username, password)
	if errors.Is(err, ErrAlreadySetup) {
		return nil
	}
	return err
}

// Login authenticates the admin user and creates (or replaces for the same device_id)
// a persistent session without invalidating sessions on other devices.
func (s *Service) Login(ctx context.Context, username, password string, device DeviceInfo) (*Session, *User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, nil, ErrInvalidCredentials
	}

	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at, updated_at FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	token, err := generateToken()
	if err != nil {
		return nil, nil, err
	}

	deviceID := strings.TrimSpace(device.DeviceID)
	if deviceID == "" {
		deviceID = id.New()
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sess := &Session{
		Token:         token,
		UserID:        u.ID,
		DeviceID:      deviceID,
		DeviceName:    strings.TrimSpace(device.DeviceName),
		ClientName:    strings.TrimSpace(device.ClientName),
		ClientVersion: strings.TrimSpace(device.ClientVersion),
		LastActiveAt:  now,
		CreatedAt:     now,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND device_id = ?`,
		u.ID, sess.DeviceID,
	); err != nil {
		return nil, nil, err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions (token, user_id, device_id, device_name, client_name, client_version, last_active_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.Token, sess.UserID, sess.DeviceID, sess.DeviceName, sess.ClientName, sess.ClientVersion, sess.LastActiveAt, sess.CreatedAt,
	); err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	return sess, &u, nil
}

// Authenticate validates a bearer/API token and refreshes its last_active_at timestamp.
func (s *Service) Authenticate(ctx context.Context, token string) (*Session, *User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil, ErrUnauthorized
	}

	var sess Session
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT s.token, s.user_id, s.device_id, s.device_name, s.client_name, s.client_version, s.last_active_at, s.created_at,
		        u.id, u.username, u.password_hash, u.created_at, u.updated_at
		 FROM sessions s
		 JOIN users u ON u.id = s.user_id
		 WHERE s.token = ?`,
		token,
	).Scan(
		&sess.Token, &sess.UserID, &sess.DeviceID, &sess.DeviceName, &sess.ClientName, &sess.ClientVersion, &sess.LastActiveAt, &sess.CreatedAt,
		&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrUnauthorized
	}
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_active_at = ? WHERE token = ?`, now, token); err == nil {
		sess.LastActiveAt = now
	}

	return &sess, &u, nil
}

// ChangePassword verifies oldPass and updates the admin account password hash.
func (s *Service) ChangePassword(ctx context.Context, oldPass, newPass string) error {
	if oldPass == "" || newPass == "" {
		return ErrInvalidInput
	}

	u, err := s.GetAdminUser(ctx)
	if err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPass)); err != nil {
		return ErrInvalidCredentials
	}

	hashBytes, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		string(hashBytes), now, u.ID,
	)
	return err
}

// ListSessions returns all active sessions ordered by most recently active first.
func (s *Service) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT token, user_id, device_id, device_name, client_name, client_version, last_active_at, created_at
		 FROM sessions
		 ORDER BY last_active_at DESC, created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(
			&sess.Token, &sess.UserID, &sess.DeviceID, &sess.DeviceName,
			&sess.ClientName, &sess.ClientVersion, &sess.LastActiveAt, &sess.CreatedAt,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// RevokeSession deletes the session matching token.
func (s *Service) RevokeSession(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// GetAdminUser returns the single admin account or ErrNotSetup if not initialized.
func (s *Service) GetAdminUser(ctx context.Context) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at, updated_at FROM users LIMIT 1`,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotSetup
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ParseAuthHeader extracts the authentication token and device metadata from
// Jellyfin/Emby headers (`Authorization: MediaBrowser ...`, `Authorization: Emby ...`,
// `X-Emby-Authorization`, `X-Emby-Token`, `X-MediaBrowser-Token`) or `?api_key=...`.
func ParseAuthHeader(r *http.Request) (token string, device DeviceInfo) {
	if r == nil {
		return "", DeviceInfo{}
	}

	for _, hdrName := range []string{"Authorization", "X-Emby-Authorization"} {
		if raw := strings.TrimSpace(r.Header.Get(hdrName)); raw != "" {
			if tok, dev, ok := parseMediaBrowserHeader(raw); ok {
				if device == (DeviceInfo{}) {
					device = dev
				}
				if token == "" && tok != "" {
					token = tok
				}
			}
		}
	}

	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-Emby-Token"))
	}
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-MediaBrowser-Token"))
	}
	if token == "" && r.URL != nil {
		token = strings.TrimSpace(r.URL.Query().Get("api_key"))
	}

	return token, device
}

func parseMediaBrowserHeader(raw string) (token string, device DeviceInfo, ok bool) {
	lower := strings.ToLower(raw)
	var rest string
	switch {
	case strings.HasPrefix(lower, "mediabrowser "):
		rest = raw[len("mediabrowser "):]
	case strings.HasPrefix(lower, "emby "):
		rest = raw[len("emby "):]
	default:
		return "", DeviceInfo{}, false
	}

	fields := parseKeyValuePairs(rest)
	for k, v := range fields {
		switch strings.ToLower(k) {
		case "token":
			token = v
		case "client":
			device.ClientName = v
		case "device":
			device.DeviceName = v
		case "deviceid":
			device.DeviceID = v
		case "version":
			device.ClientVersion = v
		}
	}
	return token, device, true
}

func parseKeyValuePairs(input string) map[string]string {
	out := make(map[string]string)
	i := 0
	n := len(input)

	for i < n {
		for i < n && (input[i] == ' ' || input[i] == '\t' || input[i] == ',') {
			i++
		}
		if i >= n {
			break
		}

		eq := strings.IndexByte(input[i:], '=')
		if eq < 0 {
			break
		}
		key := strings.TrimSpace(input[i : i+eq])
		i += eq + 1

		for i < n && (input[i] == ' ' || input[i] == '\t') {
			i++
		}

		var val string
		if i < n && input[i] == '"' {
			i++
			var sb strings.Builder
			for i < n {
				if input[i] == '\\' && i+1 < n {
					sb.WriteByte(input[i+1])
					i += 2
					continue
				}
				if input[i] == '"' {
					i++
					break
				}
				sb.WriteByte(input[i])
				i++
			}
			val = sb.String()
		} else {
			start := i
			for i < n && input[i] != ',' {
				i++
			}
			val = strings.TrimSpace(input[start:i])
		}

		if key != "" {
			out[key] = val
		}
	}

	return out
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
