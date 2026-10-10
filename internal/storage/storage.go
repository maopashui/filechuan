package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/yuxy/filechuan/internal/config"
)

type Storage struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Storage {
	return &Storage{cfg: cfg}
}

func (s *Storage) NewStorageKey(ext string) string {
	ext = sanitizeExt(ext)
	id := uuid.NewString()
	if ext != "" {
		return id + ext
	}
	return id
}

func (s *Storage) FilePath(storageKey string) (string, error) {
	if err := validateKey(storageKey); err != nil {
		return "", err
	}
	full := filepath.Join(s.cfg.FilesDir, storageKey)
	clean := filepath.Clean(full)
	if !strings.HasPrefix(clean, filepath.Clean(s.cfg.FilesDir)+string(os.PathSeparator)) && clean != filepath.Clean(s.cfg.FilesDir) {
		return "", fmt.Errorf("path escape")
	}
	return clean, nil
}

func (s *Storage) TempUploadDir(uploadID string) (string, error) {
	if err := validateKey(uploadID); err != nil {
		return "", err
	}
	dir := filepath.Join(s.cfg.TempDir, uploadID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Storage) PartPath(uploadID string, part int) (string, error) {
	dir, err := s.TempUploadDir(uploadID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("part-%06d", part)), nil
}

func (s *Storage) WritePart(uploadID string, part int, r io.Reader, max int64) (written int64, etag string, err error) {
	path, err := s.PartPath(uploadID, part)
	if err != nil {
		return 0, "", err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return 0, "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, max+1))
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return 0, "", err
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return 0, "", closeErr
	}
	if n > max {
		_ = os.Remove(tmp)
		return 0, "", fmt.Errorf("part too large")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Storage) MergeParts(uploadID string, totalParts int, storageKey string) (size int64, sha string, err error) {
	dest, err := s.FilePath(storageKey)
	if err != nil {
		return 0, "", err
	}
	tmp := dest + ".publishing"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return 0, "", err
	}
	h := sha256.New()
	mw := io.MultiWriter(out, h)
	var total int64
	for i := 1; i <= totalParts; i++ {
		pp, err := s.PartPath(uploadID, i)
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return 0, "", err
		}
		in, err := os.Open(pp)
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return 0, "", err
		}
		n, copyErr := io.Copy(mw, in)
		_ = in.Close()
		if copyErr != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return 0, "", copyErr
		}
		total += n
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return 0, "", err
	}
	_ = s.RemoveTempUpload(uploadID)
	return total, hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Storage) RemoveTempUpload(uploadID string) error {
	if err := validateKey(uploadID); err != nil {
		return err
	}
	dir := filepath.Join(s.cfg.TempDir, uploadID)
	clean := filepath.Clean(dir)
	root := filepath.Clean(s.cfg.TempDir)
	if clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		return fmt.Errorf("path escape")
	}
	return os.RemoveAll(clean)
}

func (s *Storage) DeleteFile(storageKey string) error {
	path, err := s.FilePath(storageKey)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Storage) OpenFile(storageKey string) (*os.File, os.FileInfo, error) {
	path, err := s.FilePath(storageKey)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

func validateKey(key string) error {
	if key == "" || strings.Contains(key, "..") || strings.ContainsAny(key, `/\`) {
		return fmt.Errorf("invalid storage key")
	}
	return nil
}

func sanitizeExt(name string) string {
	ext := filepath.Ext(name)
	if ext == "" || len(ext) > 32 {
		return ""
	}
	for _, r := range ext {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' {
			continue
		}
		return ""
	}
	return strings.ToLower(ext)
}

func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}
