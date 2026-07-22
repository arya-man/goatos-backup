// Package ratelimit adapts the shared authaudit token-bucket limiter to the
// ceoai ports.RateLimiter, keyed per (tenant,user).
package ratelimit

import (
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/authaudit"
)

// Limiter keys the shared limiter by tenant:user.
type Limiter struct {
	inner *authaudit.RateLimiter
}

// New builds a per-(tenant,user) limiter: `limit` requests per `window`.
func New(limit int, window time.Duration, maxKeys int) *Limiter {
	return &Limiter{inner: authaudit.NewRateLimiter(limit, window, maxKeys)}
}

// Allow reports whether this (tenant,user) may make another request now.
func (l *Limiter) Allow(tenantID, userID string) bool {
	return l.inner.Allow(tenantID + ":" + userID)
}
