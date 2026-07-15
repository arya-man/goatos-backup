// Package ports defines notification delivery dependencies.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/notification/domain"
)

var ErrChannelNotConfigured = errors.New("notification channel is not configured")

type ClaimParams struct {
	TenantID     string
	Limit        int
	MaxAttempts  int
	Now          time.Time
	LeaseTimeout time.Duration
}

type Repository interface {
	ReclaimStaleSending(ctx context.Context, tenantID string, now time.Time, leaseTimeout time.Duration) (int, error)
	ClaimDue(ctx context.Context, params ClaimParams) ([]domain.Request, error)
	MarkSent(ctx context.Context, tenantID, notificationRequestID, leaseToken, deliveredBy string, now time.Time) error
	MarkFailed(ctx context.Context, tenantID, notificationRequestID, leaseToken, deliveredBy, failureReason string, nextAttemptAt *time.Time, now time.Time) error
	// OldestDueRequestedAt returns the requested_at of the GLOBALLY oldest
	// currently-due, still-undelivered request (status queued/failed with
	// COALESCE(next_attempt_at, requested_at) <= now) — not just within a claimed
	// batch, so a newer batch cannot hide an hour-old request whose retry just
	// became due. Returns (_, false, nil) for an empty backlog. Bounded to the
	// due rows via the notification_requests_queue_idx range (future-scheduled
	// retries are excluded, never scanned). This is the ADR-defined backlog-age
	// signal the kernel worker's 1-minute fast-lane stage exports.
	OldestDueRequestedAt(ctx context.Context, tenantID string, now time.Time) (time.Time, bool, error)
}

type Gateway interface {
	Name() string
	Send(ctx context.Context, request domain.Request) error
}
