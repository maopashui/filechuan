package cleanup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yuxy/filechuan/internal/cleanup"
	"github.com/yuxy/filechuan/internal/config"
	"github.com/yuxy/filechuan/internal/database"
	"github.com/yuxy/filechuan/internal/storage"
)

func TestCleanupExpiredFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir: dir, DBPath: filepath.Join(dir, "app.db"),
		FilesDir: filepath.Join(dir, "files"), TempDir: filepath.Join(dir, "temp"),
		LogsDir: filepath.Join(dir, "logs"), TempTTL: time.Hour, CleanupInterval: time.Hour,
	}
	require.NoError(t, cfg.EnsureDirs())
	db, err := database.Open(cfg.DBPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	key := "testfile.bin"
	require.NoError(t, os.WriteFile(filepath.Join(cfg.FilesDir, key), []byte("x"), 0o640))
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO files(original_name, storage_key, size, sha256, status, created_at, expires_at, download_count)
		VALUES ('a.bin', ?, 1, 'x', 'ready', ?, ?, 0)`, key, past, past)
	require.NoError(t, err)
	id, _ := res.LastInsertId()
	_, err = db.Exec(`INSERT INTO pickup_codes(file_id, code_hash, share_token, created_at, expires_at) VALUES (?, 'h', 'tok', ?, ?)`, id, past, past)
	require.NoError(t, err)

	c := cleanup.New(db.DB, cfg, storage.New(cfg), nil)
	c.RunOnce(context.Background())

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM files WHERE id = ?`, id).Scan(&status))
	require.Equal(t, "deleted", status)
	_, err = os.Stat(filepath.Join(cfg.FilesDir, key))
	require.True(t, os.IsNotExist(err))
}
