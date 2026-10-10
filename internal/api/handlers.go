package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuxy/filechuan/internal/auth"
	"github.com/yuxy/filechuan/internal/config"
	"github.com/yuxy/filechuan/internal/file"
	"github.com/yuxy/filechuan/internal/middleware"
	"github.com/yuxy/filechuan/internal/pickup"
	"github.com/yuxy/filechuan/internal/signaling"
)

type Handlers struct {
	Cfg      *config.Config
	Auth     *auth.Service
	Files    *file.Service
	Pickup   *pickup.Service
	Hub      *signaling.Hub
	LoginRL  *middleware.RateLimiter
	PickupRL *middleware.RateLimiter
	UploadRL *middleware.RateLimiter
	RoomRL   *middleware.RateLimiter
}

func (h *Handlers) Register(r *gin.Engine) {
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/ws/signaling", func(c *gin.Context) {
		h.Hub.HandleWS(c.Writer, c.Request)
	})

	v1 := r.Group("/api/v1")
	{
		v1.GET("/setup/status", h.SetupStatus)
		v1.POST("/setup/register", middleware.RateLimit(h.LoginRL, middleware.ClientIP), h.SetupRegister)

		v1.POST("/auth/login", middleware.RateLimit(h.LoginRL, middleware.ClientIP), h.Login)
		v1.GET("/auth/me", h.Me)
		v1.POST("/auth/logout", h.Logout)

		v1.GET("/config/public", h.PublicConfig)

		authz := v1.Group("/")
		authz.Use(middleware.RequireAuth(h.Auth))
		{
			authz.GET("/files", h.ListFiles)
			authz.GET("/files/:id", h.GetFile)
			authz.DELETE("/files/:id", h.DeleteFile)
			authz.POST("/files/:id/pickup-code", h.RegeneratePickup)
			authz.DELETE("/files/:id/pickup-code", h.RevokePickup)

			authz.POST("/uploads", middleware.RateLimit(h.UploadRL, middleware.ClientIP), h.CreateUpload)
			authz.PUT("/uploads/:id/parts/:part", h.PutPart)
			authz.GET("/uploads/:id", h.GetUpload)
			authz.POST("/uploads/:id/complete", h.CompleteUpload)
			authz.DELETE("/uploads/:id", h.AbortUpload)
		}

		v1.POST("/pickup/verify", middleware.RateLimit(h.PickupRL, middleware.ClientIP), h.VerifyPickup)
		v1.GET("/pickup/:token/download", h.DownloadPickup)

		v1.POST("/rooms", middleware.RateLimit(h.RoomRL, middleware.ClientIP), h.CreateRoom)
	}
}

func (h *Handlers) SetupStatus(c *gin.Context) {
	needed, err := h.Auth.SetupNeeded(c.Request.Context())
	if err != nil {
		middleware.JSONError(c, 500, "internal", "internal error")
		return
	}
	c.JSON(200, gin.H{"setup_needed": needed})
}

func (h *Handlers) SetupRegister(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.JSONError(c, 400, "bad_request", "invalid body")
		return
	}
	user, err := h.Auth.RegisterAdmin(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrAlreadySetup):
			middleware.JSONError(c, 409, "already_setup", "admin already registered")
		case errors.Is(err, auth.ErrValidation):
			middleware.JSONError(c, 400, "validation", err.Error())
		default:
			middleware.JSONError(c, 500, "internal", "internal error")
		}
		return
	}
	token, csrf, _, err := h.Auth.Login(c.Request.Context(), req.Username, req.Password, c.ClientIP())
	if err == nil {
		h.Auth.SetSessionCookies(c.Writer, token, csrf)
	}
	c.JSON(201, gin.H{"username": user.Username})
}

func (h *Handlers) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.JSONError(c, 400, "bad_request", "invalid body")
		return
	}
	token, csrf, user, err := h.Auth.Login(c.Request.Context(), req.Username, req.Password, c.ClientIP())
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrRateLimited):
			middleware.JSONError(c, 429, "rate_limited", "too many attempts")
		case errors.Is(err, auth.ErrInvalidCreds):
			middleware.JSONError(c, 401, "invalid_credentials", "invalid username or password")
		default:
			middleware.JSONError(c, 500, "internal", "internal error")
		}
		return
	}
	h.Auth.SetSessionCookies(c.Writer, token, csrf)
	c.JSON(200, gin.H{"username": user.Username})
}

func (h *Handlers) Me(c *gin.Context) {
	token, err := c.Cookie(auth.SessionCookie)
	if err != nil || token == "" {
		middleware.JSONError(c, 401, "unauthorized", "authentication required")
		return
	}
	user, _, err := h.Auth.UserFromSession(c.Request.Context(), token)
	if err != nil {
		middleware.JSONError(c, 401, "unauthorized", "authentication required")
		return
	}
	c.JSON(200, gin.H{"username": user.Username, "id": user.ID})
}

func (h *Handlers) Logout(c *gin.Context) {
	token, _ := c.Cookie(auth.SessionCookie)
	_ = h.Auth.Logout(c.Request.Context(), token)
	h.Auth.ClearSessionCookies(c.Writer)
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handlers) PublicConfig(c *gin.Context) {
	turn := gin.H{"url": "", "username": "", "credential": ""}
	// Browser ICE needs TURN credentials when configured. Prefer leaving TURN unset
	// on public deployments unless a dedicated TURN with short-lived auth is used.
	if h.Cfg.TURNURL != "" {
		turn = gin.H{
			"url":        h.Cfg.TURNURL,
			"username":   h.Cfg.TURNUsername,
			"credential": h.Cfg.TURNCredential,
		}
	}
	c.JSON(200, gin.H{
		"max_file_size":      h.Cfg.MaxFileSize,
		"part_size":          h.Cfg.PartSize,
		"stun_servers":       h.Cfg.STUNServers,
		"turn":               turn,
		"room_max_age_sec":   int(h.Cfg.RoomMaxAge.Seconds()),
		"expire_day_options": []int{1, 3, 7, 15, 30},
	})
}

func (h *Handlers) CreateUpload(c *gin.Context) {
	var req struct {
		Filename    string `json:"filename"`
		Size        int64  `json:"size"`
		ExpiresDays int    `json:"expires_days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.JSONError(c, 400, "bad_request", "invalid body")
		return
	}
	us, err := h.Files.CreateUpload(c.Request.Context(), req.Filename, req.Size, req.ExpiresDays)
	if err != nil {
		mapFileErr(c, err)
		return
	}
	c.JSON(201, us)
}

func (h *Handlers) PutPart(c *gin.Context) {
	id := c.Param("id")
	part, err := strconv.Atoi(c.Param("part"))
	if err != nil {
		middleware.JSONError(c, 400, "bad_request", "invalid part")
		return
	}
	n, etag, err := h.Files.PutPart(c.Request.Context(), id, part, c.Request.Body)
	if err != nil {
		mapFileErr(c, err)
		return
	}
	c.JSON(200, gin.H{"size": n, "etag": etag})
}

func (h *Handlers) GetUpload(c *gin.Context) {
	us, err := h.Files.GetUpload(c.Request.Context(), c.Param("id"))
	if err != nil {
		mapFileErr(c, err)
		return
	}
	c.JSON(200, us)
}

func (h *Handlers) CompleteUpload(c *gin.Context) {
	rec, code, err := h.Files.CompleteUpload(c.Request.Context(), c.Param("id"))
	if err != nil {
		mapFileErr(c, err)
		return
	}
	rec.PickupCode = code
	rec.PickupURL = "/pickup?token=" + url.QueryEscape(rec.ShareToken)
	c.JSON(200, rec)
}

func (h *Handlers) AbortUpload(c *gin.Context) {
	if err := h.Files.AbortUpload(c.Request.Context(), c.Param("id")); err != nil {
		mapFileErr(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handlers) ListFiles(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := h.Files.ListFiles(c.Request.Context(), page, pageSize)
	if err != nil {
		middleware.JSONError(c, 500, "internal", "internal error")
		return
	}
	for i := range items {
		if items[i].ShareToken != "" {
			items[i].PickupURL = "/pickup?token=" + url.QueryEscape(items[i].ShareToken)
		}
	}
	c.JSON(200, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handlers) GetFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		middleware.JSONError(c, 400, "bad_request", "invalid id")
		return
	}
	rec, err := h.Files.GetFile(c.Request.Context(), id)
	if err != nil {
		mapFileErr(c, err)
		return
	}
	if rec.ShareToken != "" {
		rec.PickupURL = "/pickup?token=" + url.QueryEscape(rec.ShareToken)
	}
	c.JSON(200, rec)
}

func (h *Handlers) DeleteFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		middleware.JSONError(c, 400, "bad_request", "invalid id")
		return
	}
	if err := h.Files.DeleteFile(c.Request.Context(), id); err != nil {
		mapFileErr(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handlers) RegeneratePickup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		middleware.JSONError(c, 400, "bad_request", "invalid id")
		return
	}
	code, token, err := h.Files.RegeneratePickup(c.Request.Context(), id)
	if err != nil {
		mapFileErr(c, err)
		return
	}
	c.JSON(200, gin.H{
		"pickup_code": code,
		"share_token": token,
		"pickup_url":  "/pickup?token=" + url.QueryEscape(token),
	})
}

func (h *Handlers) RevokePickup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		middleware.JSONError(c, 400, "bad_request", "invalid id")
		return
	}
	if err := h.Files.RevokePickup(c.Request.Context(), id); err != nil {
		mapFileErr(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handlers) VerifyPickup(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.JSONError(c, 400, "bad_request", "invalid body")
		return
	}
	res, err := h.Pickup.VerifyCode(c.Request.Context(), req.Code)
	if err != nil {
		mapPickupErr(c, err)
		return
	}
	c.JSON(200, gin.H{
		"share_token":   res.ShareToken,
		"original_name": res.OriginalName,
		"size":          res.Size,
		"expires_at":    res.ExpiresAt,
		"download_url":  "/api/v1/pickup/" + url.PathEscape(res.ShareToken) + "/download",
	})
}

func (h *Handlers) DownloadPickup(c *gin.Context) {
	token := c.Param("token")
	info, err := h.Pickup.ResolveDownload(c.Request.Context(), token)
	if err != nil {
		mapPickupErr(c, err)
		return
	}
	f, fi, err := h.Pickup.Storage().OpenFile(info.StorageKey)
	if err != nil {
		middleware.JSONError(c, 404, "not_found", "file missing")
		return
	}
	defer f.Close()

	rangeHeader := c.GetHeader("Range")
	shouldCount := rangeHeader == "" || strings.HasPrefix(rangeHeader, "bytes=0-")
	if shouldCount {
		_ = h.Pickup.IncrementDownloadCount(c.Request.Context(), info.FileID)
	}

	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", contentDisposition(info.OriginalName))
	c.Header("Accept-Ranges", "bytes")
	http.ServeContent(c.Writer, c.Request, info.OriginalName, time.Time{}, f)
	_ = fi
}

func (h *Handlers) CreateRoom(c *gin.Context) {
	h.Hub.CreateRoomForIP(c.Writer, c.Request, c.ClientIP())
}

func contentDisposition(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\x00' {
			return -1
		}
		return r
	}, name)
	escaped := strings.ReplaceAll(name, `"`, `'`)
	return `attachment; filename="` + escaped + `"; filename*=UTF-8''` + url.PathEscape(name)
}

func mapFileErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, file.ErrNotFound):
		middleware.JSONError(c, 404, "not_found", "not found")
	case errors.Is(err, file.ErrInvalidExpire):
		middleware.JSONError(c, 400, "invalid_expire", "expires_days must be 1,3,7,15,or 30")
	case errors.Is(err, file.ErrTooLarge):
		middleware.JSONError(c, 400, "too_large", "file too large")
	case errors.Is(err, file.ErrBadState):
		middleware.JSONError(c, 400, "bad_state", err.Error())
	case errors.Is(err, file.ErrExpired):
		middleware.JSONError(c, 410, "expired", "expired")
	default:
		middleware.JSONError(c, 500, "internal", "internal error")
	}
}

func mapPickupErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, pickup.ErrInvalidCode), errors.Is(err, file.ErrNotFound):
		middleware.JSONError(c, 404, "invalid_code", "invalid or unknown pickup code")
	case errors.Is(err, pickup.ErrRevoked):
		middleware.JSONError(c, 410, "revoked", "pickup code revoked")
	case errors.Is(err, pickup.ErrExpired):
		middleware.JSONError(c, 410, "expired", "file expired")
	case errors.Is(err, pickup.ErrNotReady):
		middleware.JSONError(c, 409, "not_ready", "file not available")
	default:
		middleware.JSONError(c, 500, "internal", "internal error")
	}
}

// DrainBody helper for tests
func DrainBody(r io.ReadCloser) {
	_, _ = io.Copy(io.Discard, r)
	_ = r.Close()
}
