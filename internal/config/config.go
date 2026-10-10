package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr       string
	DataDir          string
	DBPath           string
	FilesDir         string
	TempDir          string
	LogsDir          string
	SessionTTL       time.Duration
	CookieSecure     bool
	CookieSameSite   string
	TrustedProxies   []string
	MaxFileSize      int64
	PartSize         int64
	MaxUploadConcurr int
	RoomMaxPeers     int
	RoomIdleTimeout  time.Duration
	RoomMaxAge       time.Duration
	RoomCreatePerMin int
	STUNServers      []string
	TURNURL          string
	TURNUsername     string
	TURNCredential   string
	CleanupInterval  time.Duration
	TempTTL          time.Duration
}

func Load() (*Config, error) {
	_ = loadDotEnv(".env")

	dataDir := getEnv("DATA_DIR", "./data")
	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}

	cfg := &Config{
		ListenAddr:       getEnv("LISTEN_ADDR", ":8080"),
		DataDir:          absData,
		DBPath:           filepath.Join(absData, "app.db"),
		FilesDir:         filepath.Join(absData, "files"),
		TempDir:          filepath.Join(absData, "temp"),
		LogsDir:          filepath.Join(absData, "logs"),
		SessionTTL:       getDuration("SESSION_TTL", 7*24*time.Hour),
		CookieSecure:     getBool("COOKIE_SECURE", false),
		CookieSameSite:   getEnv("COOKIE_SAMESITE", "Lax"),
		TrustedProxies:   splitCSV(getEnv("TRUSTED_PROXIES", "127.0.0.1,::1")),
		MaxFileSize:      getInt64("MAX_FILE_SIZE", 2*1024*1024*1024),
		PartSize:         getInt64("PART_SIZE", 5*1024*1024),
		MaxUploadConcurr: int(getInt64("MAX_UPLOAD_CONCURRENCY", 3)),
		RoomMaxPeers:     int(getInt64("ROOM_MAX_PEERS", 8)),
		RoomIdleTimeout:  getDuration("ROOM_IDLE_TIMEOUT", 30*time.Minute),
		RoomMaxAge:       getDuration("ROOM_MAX_AGE", 24*time.Hour),
		RoomCreatePerMin: int(getInt64("ROOM_CREATE_PER_MIN", 10)),
		STUNServers:      splitCSV(getEnv("STUN_SERVERS", "stun:stun.l.google.com:19302")),
		TURNURL:          getEnv("TURN_URL", ""),
		TURNUsername:     getEnv("TURN_USERNAME", ""),
		TURNCredential:   getEnv("TURN_CREDENTIAL", ""),
		CleanupInterval:  getDuration("CLEANUP_INTERVAL", time.Minute),
		TempTTL:          getDuration("TEMP_TTL", 24*time.Hour),
	}
	return cfg, nil
}

func (c *Config) EnsureDirs() error {
	for _, d := range []string{c.DataDir, c.FilesDir, c.TempDir, c.LogsDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return err
		}
	}
	return nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getInt64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func getDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// Minimal .env loader without requiring godotenv at import time in tests.
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, rerr := f.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	for _, line := range strings.Split(string(buf), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
	return nil
}
