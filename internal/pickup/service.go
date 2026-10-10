package pickup

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/yuxy/filechuan/internal/auth"
	"github.com/yuxy/filechuan/internal/file"
	"github.com/yuxy/filechuan/internal/storage"
)

var (
	ErrInvalidCode = errors.New("invalid pickup code")
	ErrRevoked     = errors.New("pickup revoked")
	ErrExpired     = errors.New("pickup expired")
	ErrNotReady    = errors.New("file not ready")
)

type VerifyResult struct {
	ShareToken   string    `json:"share_token"`
	OriginalName string    `json:"original_name"`
	Size         int64     `json:"size"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type DownloadInfo struct {
	FileID       int64
	OriginalName string
	StorageKey   string
	Size         int64
	SHA256       string
}

type Service struct {
	db    *sql.DB
	store *storage.Storage
}

func NewService(db *sql.DB, store *storage.Storage) *Service {
	return &Service{db: db, store: store}
}

func (s *Service) VerifyCode(ctx context.Context, code string) (*VerifyResult, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, ErrInvalidCode
	}
	hash := auth.HashToken(code)
	var token, name, expires, status string
	var size int64
	var revoked sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT p.share_token, f.original_name, f.size, f.expires_at, f.status, p.revoked_at
		FROM pickup_codes p
		JOIN files f ON f.id = p.file_id
		WHERE p.code_hash = ?`, hash).
		Scan(&token, &name, &size, &expires, &status, &revoked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCode
		}
		return nil, err
	}
	if revoked.Valid {
		return nil, ErrRevoked
	}
	if status != "ready" {
		return nil, ErrNotReady
	}
	exp, _ := time.Parse(time.RFC3339, expires)
	if time.Now().UTC().After(exp) {
		return nil, ErrExpired
	}
	return &VerifyResult{ShareToken: token, OriginalName: name, Size: size, ExpiresAt: exp}, nil
}

func (s *Service) ResolveDownload(ctx context.Context, token string) (*DownloadInfo, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInvalidCode
	}
	var info DownloadInfo
	var expires, status string
	var revoked sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT f.id, f.original_name, f.storage_key, f.size, f.sha256, f.expires_at, f.status, p.revoked_at
		FROM pickup_codes p
		JOIN files f ON f.id = p.file_id
		WHERE p.share_token = ?`, token).
		Scan(&info.FileID, &info.OriginalName, &info.StorageKey, &info.Size, &info.SHA256, &expires, &status, &revoked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, file.ErrNotFound
		}
		return nil, err
	}
	if revoked.Valid {
		return nil, ErrRevoked
	}
	if status != "ready" {
		return nil, ErrNotReady
	}
	exp, _ := time.Parse(time.RFC3339, expires)
	if time.Now().UTC().After(exp) {
		return nil, ErrExpired
	}
	return &info, nil
}

// IncrementDownloadCount increments once per successful download start.
// Strategy: count once when serving a full GET or the first byte range (bytes=0-...).
func (s *Service) IncrementDownloadCount(ctx context.Context, fileID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE files SET download_count = download_count + 1 WHERE id = ?`, fileID)
	return err
}

func (s *Service) Storage() *storage.Storage {
	return s.store
}
