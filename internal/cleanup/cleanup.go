package cleanup

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/yuxy/filechuan/internal/config"
	"github.com/yuxy/filechuan/internal/storage"
)

type Cleaner struct {
	db    *sql.DB
	cfg   *config.Config
	store *storage.Storage
	log   *slog.Logger
}

func New(db *sql.DB, cfg *config.Config, store *storage.Storage, log *slog.Logger) *Cleaner {
	if log == nil {
		log = slog.Default()
	}
	return &Cleaner{db: db, cfg: cfg, store: store, log: log}
}

func (c *Cleaner) Start(ctx context.Context) {
	c.RunOnce(ctx)
	t := time.NewTicker(c.cfg.CleanupInterval)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.RunOnce(ctx)
			}
		}
	}()
}

func (c *Cleaner) RunOnce(ctx context.Context) {
	c.cleanExpiredFiles(ctx)
	c.cleanTempUploads(ctx)
	c.cleanSessions(ctx)
}

func (c *Cleaner) cleanExpiredFiles(ctx context.Context) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := c.db.QueryContext(ctx, `SELECT id, storage_key FROM files WHERE status = 'ready' AND expires_at < ?`, now)
	if err != nil {
		c.log.Error("cleanup query files", "err", err)
		return
	}
	defer rows.Close()
	type item struct {
		id  int64
		key string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.key); err != nil {
			c.log.Error("cleanup scan", "err", err)
			continue
		}
		items = append(items, it)
	}
	for _, it := range items {
		if err := c.deleteFile(ctx, it.id, it.key); err != nil {
			c.log.Error("cleanup delete failed, will retry", "file_id", it.id, "err", err)
		}
	}
}

func (c *Cleaner) deleteFile(ctx context.Context, id int64, key string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE pickup_codes SET revoked_at = COALESCE(revoked_at, ?) WHERE file_id = ?`, now, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE files SET status = 'deleted' WHERE id = ?`, id); err != nil {
		return err
	}
	if err := c.store.DeleteFile(key); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Cleaner) cleanTempUploads(ctx context.Context) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := c.db.QueryContext(ctx, `SELECT id FROM upload_sessions WHERE expires_at < ? OR status IN ('aborted','completed')`, now)
	if err != nil {
		c.log.Error("cleanup temp query", "err", err)
		return
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		_ = c.store.RemoveTempUpload(id)
		_, _ = c.db.ExecContext(ctx, `DELETE FROM upload_parts WHERE upload_id = ?`, id)
		_, _ = c.db.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id = ?`, id)
	}

	// orphan temp dirs
	entries, err := os.ReadDir(c.cfg.TempDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-c.cfg.TempTTL)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(c.cfg.TempDir, e.Name()))
		}
	}
}

func (c *Cleaner) cleanSessions(ctx context.Context) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = c.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now)
}
