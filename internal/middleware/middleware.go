package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuxy/filechuan/internal/auth"
)

type contextKey string

const UserKey contextKey = "user"

func JSONError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}

func RequireAuth(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(auth.SessionCookie)
		if err != nil || token == "" {
			JSONError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		user, csrf, err := svc.UserFromSession(c.Request.Context(), token)
		if err != nil {
			JSONError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
			header := c.GetHeader(auth.CSRFHeader)
			cookieCSRF, _ := c.Cookie(auth.CSRFCookie)
			if header == "" || cookieCSRF == "" || header != csrf || header != cookieCSRF {
				JSONError(c, http.StatusForbidden, "csrf_failed", "invalid csrf token")
				return
			}
		}
		c.Set(string(UserKey), user)
		c.Next()
	}
}

type RateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{hits: make(map[string][]time.Time), limit: limit, window: window}
}

func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-r.window)
	list := r.hits[key]
	kept := list[:0]
	for _, t := range list {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= r.limit {
		r.hits[key] = kept
		return false
	}
	kept = append(kept, now)
	r.hits[key] = kept
	return true
}

func RateLimit(limiter *RateLimiter, keyFn func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := keyFn(c)
		if !limiter.Allow(key) {
			JSONError(c, http.StatusTooManyRequests, "rate_limited", "too many requests")
			return
		}
		c.Next()
	}
}

func ClientIP(c *gin.Context) string {
	return c.ClientIP()
}
