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
}

type Gateway interface {
	Name() string
	Send(ctx context.Context, request domain.Request) error
}
