package file

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yuxy/filechuan/internal/auth"
	"github.com/yuxy/filechuan/internal/config"
	"github.com/yuxy/filechuan/internal/storage"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrInvalidExpire = errors.New("invalid expire days")
	ErrTooLarge      = errors.New("file too large")
	ErrBadState      = errors.New("invalid upload state")
	ErrExpired       = errors.New("expired")
)

type FileRecord struct {
	ID            int64     `json:"id"`
	OriginalName  string    `json:"original_name"`
	StorageKey    string    `json:"-"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	DownloadCount int64     `json:"download_count"`
	PickupCode    string    `json:"pickup_code,omitempty"`
	ShareToken    string    `json:"share_token,omitempty"`
	PickupURL     string    `json:"pickup_url,omitempty"`
}

type UploadSession struct {
	ID             string    `json:"id"`
	OriginalName   string    `json:"original_name"`
	TotalSize      int64     `json:"total_size"`
	PartSize       int64     `json:"part_size"`
	TotalParts     int       `json:"total_parts"`
	ExpiresDays    int       `json:"expires_days"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	CompletedParts []int     `json:"completed_parts"`
}

type Service struct {
	db    *sql.DB
	cfg   *config.Config
	store *storage.Storage
}

func NewService(db *sql.DB, cfg *config.Config, store *storage.Storage) *Service {
	return &Service{db: db, cfg: cfg, store: store}
}

func allowedExpireDays(days int) bool {
	switch days {
	case 1, 3, 7, 15, 30:
		return true
	default:
		return false
	}
}

func (s *Service) CreateUpload(ctx context.Context, name string, totalSize int64, expiresDays int) (*UploadSession, error) {
	name = sanitizeFilename(name)
	if name == "" {
		return nil, fmt.Errorf("%w: empty name", ErrBadState)
	}
	if expiresDays == 0 {
		expiresDays = 30
	}
	if !allowedExpireDays(expiresDays) {
		return nil, ErrInvalidExpire
	}
	if totalSize <= 0 || totalSize > s.cfg.MaxFileSize {
		return nil, ErrTooLarge
	}
	partSize := s.cfg.PartSize
	totalParts := int(math.Ceil(float64(totalSize) / float64(partSize)))
	id := uuid.NewString()
	tempDir, err := s.store.TempUploadDir(id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	exp := now.Add(s.cfg.TempTTL)
	_, err = s.db.ExecContext(ctx, `INSERT INTO upload_sessions(id, original_name, total_size, part_size, total_parts, expires_days, status, created_at, expires_at, temp_dir)
		VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)`,
		id, name, totalSize, partSize, totalParts, expiresDays, now.Format(time.RFC3339), exp.Format(time.RFC3339), tempDir)
	if err != nil {
		return nil, err
	}
	for i := 1; i <= totalParts; i++ {
		_, err = s.db.ExecContext(ctx, `INSERT INTO upload_parts(upload_id, part_number) VALUES (?, ?)`, id, i)
		if err != nil {
			return nil, err
		}
	}
	return &UploadSession{
		ID: id, OriginalName: name, TotalSize: totalSize, PartSize: partSize,
		TotalParts: totalParts, ExpiresDays: expiresDays, Status: "active",
		CreatedAt: now, ExpiresAt: exp, CompletedParts: []int{},
	}, nil
}

func (s *Service) PutPart(ctx context.Context, uploadID string, part int, r io.Reader) (int64, string, error) {
	var totalParts int
	var partSize int64
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT total_parts, part_size, status FROM upload_sessions WHERE id = ?`, uploadID).
		Scan(&totalParts, &partSize, &status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", ErrNotFound
		}
		return 0, "", err
	}
	if status != "active" {
		return 0, "", ErrBadState
	}
	if part < 1 || part > totalParts {
		return 0, "", ErrBadState
	}
	max := partSize
	if part == totalParts {
		max = partSize // last part may be smaller; still capped by partSize
	}
	n, etag, err := s.store.WritePart(uploadID, part, r, max)
	if err != nil {
		return 0, "", err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE upload_parts SET size = ?, etag = ?, completed = 1 WHERE upload_id = ? AND part_number = ?`,
		n, etag, uploadID, part)
	return n, etag, err
}

func (s *Service) GetUpload(ctx context.Context, uploadID string) (*UploadSession, error) {
	var us UploadSession
	var created, expires string
	err := s.db.QueryRowContext(ctx, `SELECT id, original_name, total_size, part_size, total_parts, expires_days, status, created_at, expires_at
		FROM upload_sessions WHERE id = ?`, uploadID).
		Scan(&us.ID, &us.OriginalName, &us.TotalSize, &us.PartSize, &us.TotalParts, &us.ExpiresDays, &us.Status, &created, &expires)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	us.CreatedAt, _ = time.Parse(time.RFC3339, created)
	us.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	rows, err := s.db.QueryContext(ctx, `SELECT part_number FROM upload_parts WHERE upload_id = ? AND completed = 1 ORDER BY part_number`, uploadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	us.CompletedParts = []int{}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		us.CompletedParts = append(us.CompletedParts, p)
	}
	return &us, nil
}

func (s *Service) CompleteUpload(ctx context.Context, uploadID string) (*FileRecord, string, error) {
	us, err := s.GetUpload(ctx, uploadID)
	if err != nil {
		return nil, "", err
	}
	if us.Status != "active" {
		return nil, "", ErrBadState
	}
	if len(us.CompletedParts) != us.TotalParts {
		return nil, "", fmt.Errorf("%w: missing parts", ErrBadState)
	}
	key := s.store.NewStorageKey(us.OriginalName)
	size, sha, err := s.store.MergeParts(uploadID, us.TotalParts, key)
	if err != nil {
		return nil, "", err
	}
	if size != us.TotalSize {
		_ = s.store.DeleteFile(key)
		return nil, "", fmt.Errorf("%w: size mismatch", ErrBadState)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(us.ExpiresDays) * 24 * time.Hour)
	code, err := auth.RandomPickupCode()
	if err != nil {
		return nil, "", err
	}
	shareToken, err := auth.RandomToken(24)
	if err != nil {
		return nil, "", err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `INSERT INTO files(original_name, storage_key, size, sha256, status, created_at, expires_at, download_count)
		VALUES (?, ?, ?, ?, 'ready', ?, ?, 0)`,
		us.OriginalName, key, size, sha, now.Format(time.RFC3339), expiresAt.Format(time.RFC3339))
	if err != nil {
		return nil, "", err
	}
	fileID, _ := res.LastInsertId()
	_, err = tx.ExecContext(ctx, `INSERT INTO pickup_codes(file_id, code_hash, share_token, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		fileID, auth.HashToken(strings.ToUpper(code)), shareToken, now.Format(time.RFC3339), expiresAt.Format(time.RFC3339))
	if err != nil {
		return nil, "", err
	}
	_, err = tx.ExecContext(ctx, `UPDATE upload_sessions SET status = 'completed' WHERE id = ?`, uploadID)
	if err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}

	rec := &FileRecord{
		ID: fileID, OriginalName: us.OriginalName, StorageKey: key, Size: size, SHA256: sha,
		Status: "ready", CreatedAt: now, ExpiresAt: expiresAt, DownloadCount: 0,
		PickupCode: code, ShareToken: shareToken,
	}
	return rec, code, nil
}

func (s *Service) AbortUpload(ctx context.Context, uploadID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE upload_sessions SET status = 'aborted' WHERE id = ?`, uploadID)
	if err != nil {
		return err
	}
	_ = s.store.RemoveTempUpload(uploadID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM upload_parts WHERE upload_id = ?`, uploadID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id = ?`, uploadID)
	return nil
}

func (s *Service) ListFiles(ctx context.Context, page, pageSize int) ([]FileRecord, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE status = 'ready'`).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.original_name, f.storage_key, f.size, f.sha256, f.status, f.created_at, f.expires_at, f.download_count,
		       p.share_token
		FROM files f
		LEFT JOIN pickup_codes p ON p.file_id = f.id AND p.revoked_at IS NULL
		WHERE f.status = 'ready'
		ORDER BY f.created_at DESC
		LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []FileRecord{}
	for rows.Next() {
		var r FileRecord
		var created, expires string
		var token sql.NullString
		if err := rows.Scan(&r.ID, &r.OriginalName, &r.StorageKey, &r.Size, &r.SHA256, &r.Status, &created, &expires, &r.DownloadCount, &token); err != nil {
			return nil, 0, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		r.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
		if token.Valid {
			r.ShareToken = token.String
		}
		out = append(out, r)
	}
	return out, total, nil
}

func (s *Service) GetFile(ctx context.Context, id int64) (*FileRecord, error) {
	var r FileRecord
	var created, expires string
	var token sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT f.id, f.original_name, f.storage_key, f.size, f.sha256, f.status, f.created_at, f.expires_at, f.download_count, p.share_token
		FROM files f LEFT JOIN pickup_codes p ON p.file_id = f.id
		WHERE f.id = ?`, id).
		Scan(&r.ID, &r.OriginalName, &r.StorageKey, &r.Size, &r.SHA256, &r.Status, &created, &expires, &r.DownloadCount, &token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	r.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	if token.Valid {
		r.ShareToken = token.String
	}
	return &r, nil
}

func (s *Service) DeleteFile(ctx context.Context, id int64) error {
	rec, err := s.GetFile(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE pickup_codes SET revoked_at = ? WHERE file_id = ? AND revoked_at IS NULL`, now, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE files SET status = 'deleted' WHERE id = ?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.store.DeleteFile(rec.StorageKey)
	return nil
}

func (s *Service) RegeneratePickup(ctx context.Context, id int64) (code, token string, err error) {
	rec, err := s.GetFile(ctx, id)
	if err != nil {
		return "", "", err
	}
	if rec.Status != "ready" || time.Now().UTC().After(rec.ExpiresAt) {
		return "", "", ErrExpired
	}
	code, err = auth.RandomPickupCode()
	if err != nil {
		return "", "", err
	}
	token, err = auth.RandomToken(24)
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO pickup_codes(file_id, code_hash, share_token, created_at, expires_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, NULL)
		ON CONFLICT(file_id) DO UPDATE SET
			code_hash = excluded.code_hash,
			share_token = excluded.share_token,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at,
			revoked_at = NULL`,
		id, auth.HashToken(strings.ToUpper(code)), token, now.Format(time.RFC3339), rec.ExpiresAt.Format(time.RFC3339))
	return code, token, err
}

func (s *Service) RevokePickup(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `UPDATE pickup_codes SET revoked_at = ? WHERE file_id = ? AND revoked_at IS NULL`, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\x00' {
			return -1
		}
		return r
	}, name)
	if name == "." || name == ".." || name == "" {
		return ""
	}
	if len(name) > 255 {
		name = name[:255]
	}
	return name
}
