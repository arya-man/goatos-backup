// Package ports declares the vaccination module's repository boundary.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// ErrNotFound is returned when a requested vaccination row does not exist.
var (
	ErrNotFound            = errors.New("vaccination: not found")
	ErrIdempotencyConflict = errors.New("vaccination: idempotency key reused with different request")
)

// Repository is the persistence boundary for the vaccination module. Implementations wrap
// generated sqlc queries; no hand-written SQL leaks above this interface.
type Repository interface {
	Ping(ctx context.Context) error

	// RecordCompletion appends a dose-administered record. Idempotent on
	// (tenant_id, idempotency_key): applied is false on replay.
	RecordCompletion(ctx context.Context, in domain.NewCompletion) (completionID string, applied bool, err error)

	// AcceptCompletion / RejectCompletion are the verification outcomes (SM-5). Both only act on a
	// completion still in 'recorded' state (idempotent: applied is false on replay). AcceptCompletion
	// returns the verification context (obligation/goat/batch/lot/doses) so the caller can complete
	// the obligation and consume the reserved dose.
	GetRecordedCompletion(ctx context.Context, tenantID, completionID string) (domain.AcceptedCompletion, bool, error)
	GetAcceptableCompletion(ctx context.Context, tenantID, completionID string) (domain.AcceptedCompletion, bool, error)
	GetAcceptableCompletionByIdempotency(ctx context.Context, tenantID, idempotencyKey string) (domain.AcceptedCompletion, bool, error)
	AcceptCompletion(ctx context.Context, tenantID, completionID string, verifiedBy *string, withdrawalUntil *time.Time) (domain.AcceptedCompletion, bool, error)
	RejectCompletion(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (bool, error)

	// ListCompletionsByGoat returns the goat's vaccination history (most recent first).
	ListCompletionsByGoat(ctx context.Context, tenantID, goatID string, limit int32) ([]domain.CompletionHistoryItem, error)

	// ListRecordedCompletionsByTask returns the still-recorded completion ids under a SOP task's
	// submissions (the SOP verify fan-out source).
	ListRecordedCompletionsByTask(ctx context.Context, tenantID, taskID string) ([]string, error)

	// RecordCompletionsFromSubmission materializes vaccination_completions from a vaccination SOP
	// submission's per-goat items. Idempotent on the submission-item idempotency key.
	RecordCompletionsFromSubmission(ctx context.Context, tenantID, taskID, submissionID, recordedBy string) (int, error)

	// ListRecordedCompletions returns completions awaiting review (status='recorded'), earliest
	// administered first (the Verification queue). parkID is an optional park scope (empty = all parks).
	ListRecordedCompletions(ctx context.Context, tenantID, parkID string, limit int32) ([]domain.RecordedCompletion, error)

	// GetLastAcceptedForGoat returns the most recent accepted administration; found is false when none.
	GetLastAcceptedForGoat(ctx context.Context, tenantID, goatID string) (rec domain.LastAccepted, found bool, err error)

	// Impact-preview counts (live). All scoped by tenant + the eligibility filter.
	CountEligibleGoats(ctx context.Context, f domain.ImpactFilter) (int64, error)
	CountCatchupGoats(ctx context.Context, f domain.ImpactFilter) (int64, error)
	CountEligibleShedScopes(ctx context.Context, f domain.ImpactFilter) (int64, error)
	// SumAvailableStock returns available (unreserved) quantity + earliest expiry for an item.
	SumAvailableStock(ctx context.Context, tenantID, itemID string, locationID *string) (available string, earliestExpiry *time.Time, err error)

	// ListEligibleGoatsForGeneration returns a chunked (keyset by goat_id) page of the in-care
	// cohort matching the filter; afterGoatID is the cursor ("" starts at the beginning).
	ListEligibleGoatsForGeneration(ctx context.Context, f domain.ImpactFilter, afterGoatID string, limit int32) ([]domain.EligibleGoat, error)

	// GetGoatForGeneration loads one goat's generation fields (incl sex/breed/stage). found is
	// false when the goat does not exist.
	GetGoatForGeneration(ctx context.Context, tenantID, goatID string) (g domain.EligibleGoat, found bool, err error)
}
