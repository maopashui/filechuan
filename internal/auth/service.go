package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yuxy/filechuan/internal/config"
)

var (
	ErrAlreadySetup = errors.New("admin already registered")
	ErrInvalidCreds = errors.New("invalid credentials")
	ErrUnauthorized = errors.New("unauthorized")
	ErrRateLimited  = errors.New("rate limited")
	ErrValidation   = errors.New("validation error")
)

const (
	SessionCookie = "fc_session"
	CSRFCookie    = "fc_csrf"
	CSRFHeader    = "X-CSRF-Token"
)

type User struct {
	ID          int64
	Username    string
	CreatedAt   time.Time
	LastLoginAt *time.Time
}

type Session struct {
	ID        int64
	UserID    int64
	TokenHash string
	CSRFToken string
	ExpiresAt time.Time
}

type Service struct {
	db  *sql.DB
	cfg *config.Config

	loginMu    sync.Mutex
	loginFails map[string][]time.Time
}

func NewService(db *sql.DB, cfg *config.Config) *Service {
	return &Service{
		db:         db,
		cfg:        cfg,
		loginFails: make(map[string][]time.Time),
	}
}

func (s *Service) SetupNeeded(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

func (s *Service) RegisterAdmin(ctx context.Context, username, password string) (*User, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return nil, fmt.Errorf("%w: username length", ErrValidation)
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("%w: password too short", ErrValidation)
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrAlreadySetup
	}

	res, err := tx.ExecContext(ctx, `INSERT INTO users(username, password_hash, created_at) VALUES (?, ?, ?)`, username, hash, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrAlreadySetup
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &User{ID: id, Username: username, CreatedAt: time.Now().UTC()}, nil
}

func (s *Service) Login(ctx context.Context, username, password, clientIP string) (sessionToken string, csrfToken string, user *User, err error) {
	if s.isLoginLimited(clientIP) {
		return "", "", nil, ErrRateLimited
	}

	var u User
	var hash string
	var createdAt string
	var lastLogin sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, created_at, last_login_at FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &hash, &createdAt, &lastLogin)
	if err != nil {
		s.recordLoginFail(clientIP)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", nil, ErrInvalidCreds
		}
		return "", "", nil, err
	}
	if !VerifyPassword(hash, password) {
		s.recordLoginFail(clientIP)
		return "", "", nil, ErrInvalidCreds
	}

	sessionToken, err = RandomToken(32)
	if err != nil {
		return "", "", nil, err
	}
	csrfToken, err = RandomToken(32)
	if err != nil {
		return "", "", nil, err
	}
	expires := time.Now().UTC().Add(s.cfg.SessionTTL)
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions(user_id, token_hash, csrf_token, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		u.ID, HashToken(sessionToken), csrfToken, expires.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return "", "", nil, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, now.Format(time.RFC3339), u.ID)
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return sessionToken, csrfToken, &u, nil
}

func (s *Service) Logout(ctx context.Context, sessionToken string) error {
	if sessionToken == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, HashToken(sessionToken))
	return err
}

func (s *Service) UserFromSession(ctx context.Context, sessionToken string) (*User, string, error) {
	if sessionToken == "" {
		return nil, "", ErrUnauthorized
	}
	var u User
	var csrf string
	var createdAt string
	var expiresAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.created_at, s.csrf_token, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?`, HashToken(sessionToken)).
		Scan(&u.ID, &u.Username, &createdAt, &csrf, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrUnauthorized
		}
		return nil, "", err
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().UTC().After(exp) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, HashToken(sessionToken))
		return nil, "", ErrUnauthorized
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &u, csrf, nil
}

func (s *Service) SetSessionCookies(w http.ResponseWriter, sessionToken, csrfToken string) {
	maxAge := int(s.cfg.SessionTTL.Seconds())
	sameSite := http.SameSiteLaxMode
	switch strings.ToLower(s.cfg.CookieSameSite) {
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: sameSite,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookie,
		Value:    csrfToken,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: false,
		Secure:   s.cfg.CookieSecure,
		SameSite: sameSite,
	})
}

func (s *Service) ClearSessionCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: CSRFCookie, Value: "", Path: "/", MaxAge: -1})
}

func (s *Service) ResetAdmin(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) isLoginLimited(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	cutoff := time.Now().Add(-15 * time.Minute)
	fails := s.loginFails[ip]
	kept := fails[:0]
	for _, t := range fails {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	s.loginFails[ip] = kept
	return len(kept) >= 10
}

func (s *Service) recordLoginFail(ip string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	s.loginFails[ip] = append(s.loginFails[ip], time.Now())
}
