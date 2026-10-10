package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yuxy/filechuan/internal/config"
)

func TestRemoveTempUploadRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:  dir,
		FilesDir: filepath.Join(dir, "files"),
		TempDir:  filepath.Join(dir, "temp"),
	}
	require.NoError(t, os.MkdirAll(cfg.TempDir, 0o750))
	require.NoError(t, os.MkdirAll(cfg.FilesDir, 0o750))
	marker := filepath.Join(dir, "keep.txt")
	require.NoError(t, os.WriteFile(marker, []byte("x"), 0o640))

	s := New(cfg)
	require.Error(t, s.RemoveTempUpload(".."))
	require.Error(t, s.RemoveTempUpload("../files"))
	require.Error(t, s.RemoveTempUpload("a/b"))
	_, err := os.Stat(marker)
	require.NoError(t, err)
}
