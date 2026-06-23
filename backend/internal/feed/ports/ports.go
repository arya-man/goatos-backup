// Package ports declares the feed-direction module's repository boundary.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/feed/domain"
)

// ErrNotFound is returned when a requested feed row does not exist.
var ErrNotFound = errors.New("feed: not found")

// Repository is the persistence boundary for the feed-direction module. Implementations wrap
// generated sqlc queries; no hand-written SQL leaks above this interface.
type Repository interface {
	Ping(ctx context.Context) error

	// RecordDirection appends a shed feed-direction execution. Idempotent on
	// (tenant_id, idempotency_key): applied is false on replay.
	RecordDirection(ctx context.Context, in domain.NewDirection) (completionID string, applied bool, err error)

	// AcceptDirection / RejectDirection are the verification outcomes (SM-5). Both act only on a
	// direction still in 'recorded' state (idempotent: applied is false on replay). AcceptDirection
	// returns the verification context (obligation/shed/batch/lot/quantity) so the caller can
	// complete the obligation and (later) consume the reserved feed stock.
	AcceptDirection(ctx context.Context, tenantID, completionID string, verifiedBy *string) (domain.AcceptedDirection, bool, error)
	RejectDirection(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (bool, error)

	// ListDirectionsByShed returns a shed's feed history (most recent first).
	ListDirectionsByShed(ctx context.Context, tenantID, shedID string, limit int32) ([]domain.DirectionHistoryItem, error)

	// ListRecordedDirections returns directions awaiting review (status='recorded'), earliest fed
	// first (the Verification queue).
	ListRecordedDirections(ctx context.Context, tenantID string, limit int32) ([]domain.RecordedDirection, error)
}
