package handlers

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/sitesync"
	"github.com/gin-gonic/gin"
)

const recoveryCandidateRequestsPerMinute = 30

type recoveryRateWindow struct {
	startedAt time.Time
	count     int
}

type recoveryCandidateLimiter struct {
	mu      sync.Mutex
	entries map[string]recoveryRateWindow
	limit   int
	window  time.Duration
	now     func() time.Time
}

var siteRecoveryCandidateLimiter = newRecoveryCandidateLimiter(recoveryCandidateRequestsPerMinute, time.Minute)

func newRecoveryCandidateLimiter(limit int, window time.Duration) *recoveryCandidateLimiter {
	return &recoveryCandidateLimiter{
		entries: make(map[string]recoveryRateWindow),
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

func (limiter *recoveryCandidateLimiter) allow(key string) (bool, int) {
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	for entryKey, entry := range limiter.entries {
		if now.Sub(entry.startedAt) >= limiter.window {
			delete(limiter.entries, entryKey)
		}
	}

	entry, ok := limiter.entries[key]
	if !ok {
		limiter.entries[key] = recoveryRateWindow{startedAt: now, count: 1}
		return true, 0
	}
	entry.count++
	limiter.entries[key] = entry
	if entry.count <= limiter.limit {
		return true, 0
	}

	retryAfter := int(math.Ceil(entry.startedAt.Add(limiter.window).Sub(now).Seconds()))
	if retryAfter < 1 {
		retryAfter = 1
	}
	return false, retryAfter
}

func recoveryCandidateGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin != "" && origin != middleware.RecoveryExtensionOrigin {
			resp.ErrorWithAppError(c, http.StatusForbidden, apperror.New(
				sitesync.CodeSiteRecoveryForbidden,
				"recovery candidate origin is not allowed",
			).WithStatus(http.StatusForbidden))
			return
		}

		allowed, retryAfter := siteRecoveryCandidateLimiter.allow(recoveryRateLimitKey(c.Request.RemoteAddr))
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			resp.ErrorWithAppError(c, http.StatusTooManyRequests, apperror.New(
				sitesync.CodeSiteRecoveryRateLimited,
				"too many recovery candidate requests; try again later",
			).WithStatus(http.StatusTooManyRequests).WithParam("retryAfter", retryAfter))
			return
		}
		c.Next()
	}
}

func recoveryRateLimitKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(remoteAddr)
}
