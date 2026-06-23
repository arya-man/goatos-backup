// Package ports declares the vaccination module's repository boundary.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// ErrNotFound is returned when a requested vaccination row does not exist.
var ErrNotFound = errors.New("vaccination: not found")

// Repository is the persistence boundary for the vaccination module. Implementations wrap
// generated sqlc queries; no hand-written SQL leaks above this interface.
type Repository interface {
	Ping(ctx context.Context) error

	// RecordCompletion appends a dose-administered record. Idempotent on
	// (tenant_id, idempotency_key): applied is false on replay.
	RecordCompletion(ctx context.Context, in domain.NewCompletion) (completionID string, applied bool, err error)

	// AcceptCompletion / RejectCompletion are the verification outcomes (SM-5). Both only act on a
	// completion still in 'recorded' state.
	AcceptCompletion(ctx context.Context, tenantID, completionID string, verifiedBy *string, withdrawalUntil *time.Time) error
	RejectCompletion(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) error

	// ListCompletionsByGoat returns the goat's vaccination history (most recent first).
	ListCompletionsByGoat(ctx context.Context, tenantID, goatID string, limit int32) ([]domain.CompletionHistoryItem, error)

	// GetLastAcceptedForGoat returns the most recent accepted administration; found is false when none.
	GetLastAcceptedForGoat(ctx context.Context, tenantID, goatID string) (rec domain.LastAccepted, found bool, err error)

	// Impact-preview counts (live). All scoped by tenant + the eligibility filter.
	CountEligibleGoats(ctx context.Context, f domain.ImpactFilter) (int64, error)
	CountCatchupGoats(ctx context.Context, f domain.ImpactFilter) (int64, error)
	CountEligibleShedScopes(ctx context.Context, f domain.ImpactFilter) (int64, error)
	// SumAvailableStock returns available (unreserved) quantity + earliest expiry for an item.
	SumAvailableStock(ctx context.Context, tenantID, itemID string, locationID *string) (available string, earliestExpiry *time.Time, err error)
}
