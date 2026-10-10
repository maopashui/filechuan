package server

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuxy/filechuan/internal/api"
	"github.com/yuxy/filechuan/internal/auth"
	"github.com/yuxy/filechuan/internal/cleanup"
	"github.com/yuxy/filechuan/internal/config"
	"github.com/yuxy/filechuan/internal/database"
	"github.com/yuxy/filechuan/internal/file"
	"github.com/yuxy/filechuan/internal/middleware"
	"github.com/yuxy/filechuan/internal/pickup"
	"github.com/yuxy/filechuan/internal/signaling"
	"github.com/yuxy/filechuan/internal/storage"
	"github.com/yuxy/filechuan/webembed"
)

type App struct {
	Cfg    *config.Config
	DB     *database.DB
	Auth   *auth.Service
	Files  *file.Service
	Pickup *pickup.Service
	Hub    *signaling.Hub
	Engine *gin.Engine
	Log    *slog.Logger
}

func New(cfg *config.Config, log *slog.Logger) (*App, error) {
	if log == nil {
		log = slog.Default()
	}
	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	store := storage.New(cfg)
	authSvc := auth.NewService(db.DB, cfg)
	fileSvc := file.NewService(db.DB, cfg, store)
	pickupSvc := pickup.NewService(db.DB, store)
	hub := signaling.NewHub(cfg)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	if len(cfg.TrustedProxies) > 0 {
		if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
			return nil, err
		}
	} else {
		_ = r.SetTrustedProxies(nil)
	}
	r.Use(gin.Recovery())
	r.Use(requestLogger(log))

	h := &api.Handlers{
		Cfg:      cfg,
		Auth:     authSvc,
		Files:    fileSvc,
		Pickup:   pickupSvc,
		Hub:      hub,
		LoginRL:  middleware.NewRateLimiter(20, time.Minute),
		PickupRL: middleware.NewRateLimiter(30, time.Minute),
		UploadRL: middleware.NewRateLimiter(60, time.Minute),
		RoomRL:   middleware.NewRateLimiter(cfg.RoomCreatePerMin, time.Minute),
	}
	h.Register(r)
	mountFrontend(r)

	app := &App{
		Cfg: cfg, DB: db, Auth: authSvc, Files: fileSvc, Pickup: pickupSvc,
		Hub: hub, Engine: r, Log: log,
	}
	return app, nil
}

func (a *App) StartCleanup(ctx context.Context) {
	c := cleanup.New(a.DB.DB, a.Cfg, storage.New(a.Cfg), a.Log)
	c.Start(ctx)
}

func (a *App) Close() error {
	return a.DB.Close()
}

func requestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || path == "/healthz" || strings.HasPrefix(path, "/ws/") {
			log.Info("request",
				"method", c.Request.Method,
				"path", path,
				"status", c.Writer.Status(),
				"latency_ms", time.Since(start).Milliseconds(),
			)
		}
	}
}

func mountFrontend(r *gin.Engine) {
	staticFS, err := webembed.FS()
	if err != nil {
		r.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/ws/") {
				middleware.JSONError(c, 404, "not_found", "not found")
				return
			}
			c.String(http.StatusServiceUnavailable, "frontend not embedded; run make build")
		})
		return
	}

	fileServer := http.FileServer(http.FS(staticFS))
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws/") {
			middleware.JSONError(c, 404, "not_found", "not found")
			return
		}
		// try static file
		trimmed := strings.TrimPrefix(path, "/")
		if trimmed != "" {
			if f, err := staticFS.Open(trimmed); err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		// SPA fallback
		data, err := fs.ReadFile(staticFS, "index.html")
		if err != nil {
			c.String(500, "index missing")
			return
		}
		c.Data(200, "text/html; charset=utf-8", data)
	})
}
