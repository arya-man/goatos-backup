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
	// ListSubmissionCompletions returns every completion materialized for one submission. The
	// bounded result is capped by the submission fan-out limit (1,000 goats) and is used by the
	// composition bridge to create one verification item per goat, not one per vaccine.
	ListSubmissionCompletions(ctx context.Context, tenantID, submissionID string) ([]domain.SubmissionCompletion, error)

	// ListRecordedCompletions returns completions awaiting review (status='recorded'), earliest
	// administered first (the Verification queue). parkID is an optional park scope (empty = all parks).
	ListRecordedCompletions(ctx context.Context, tenantID, parkID string, cursor *domain.RecordedCompletionCursor, limit int32) (domain.RecordedCompletionPage, error)

	// GetLastAcceptedForGoat returns the most recent accepted administration; found is false when none.
	GetLastAcceptedForGoat(ctx context.Context, tenantID, goatID string) (rec domain.LastAccepted, found bool, err error)

	// Impact-preview aggregate (READ MODEL). Reads ONLY vaccination_eligibility_rollups — never scans
	// goats — so the config preview stays cheap across the current 5,000-50,000-animal release envelope
	// (up to the ~500k obligation-row upper bound; 1-5M is the future certification bar — see
	// docs/decisions/operational-kernel-5k-50k-scale-envelope.md). Scoped by tenant + the eligibility
	// filter dims (usable animals only).
	SumEligibilityRollup(ctx context.Context, f domain.ImpactFilter) (domain.EligibilityRollupAggregate, error)
	// CapacityMaxPerDay returns the tenant's configured animals/operator/day cap (default when unset).
	// Used to compute estimated_days = ceil(eligible_animals / cap).
	CapacityMaxPerDay(ctx context.Context, tenantID string) (int64, error)
	// SumAvailableStock returns available (unreserved) quantity + earliest expiry for an item.
	SumAvailableStock(ctx context.Context, tenantID, itemID string, locationID *string) (available string, earliestExpiry *time.Time, err error)

	// Live eligibility counts. NOT used by the config impact preview (which reads the rollup); retained
	// for the generation/listing path and direct integration coverage.
	CountEligibleGoats(ctx context.Context, f domain.ImpactFilter) (int64, error)
	CountCatchupGoats(ctx context.Context, f domain.ImpactFilter) (int64, error)
	CountEligibleShedScopes(ctx context.Context, f domain.ImpactFilter) (int64, error)

	// RecomputeEligibilityRollup fully rebuilds the tenant's vaccination_eligibility_rollups from source
	// tables (goats, locations/operational attributes, shed profiles, animal stage lookup). Projector
	// write path only; delete-then-insert per tenant inside one transaction.
	RecomputeEligibilityRollup(ctx context.Context, tenantID string) (domain.RollupRecomputeResult, error)

	// ListEligibleGoatsForGeneration returns a chunked (keyset by goat_id) page of the in-care
	// cohort matching the filter; afterGoatID is the cursor ("" starts at the beginning).
	ListEligibleGoatsForGeneration(ctx context.Context, f domain.ImpactFilter, afterGoatID string, limit int32) ([]domain.EligibleGoat, error)

	// GetGoatForGeneration loads one goat's generation fields (incl sex/breed/stage). found is
	// false when the goat does not exist.
	GetGoatForGeneration(ctx context.Context, tenantID, goatID string) (g domain.EligibleGoat, found bool, err error)

	// ShedCompletionSummary computes the FROZEN read-only shed-completion/submit summary (scan +
	// proof + obligation state only, never submitted answers) for one vaccination task. shedID is
	// optional for legacy task-wide readers; mobile submit passes it so park/drive tasks stay
	// narrowed to the exact shed the operator scanned.
	ShedCompletionSummary(ctx context.Context, tenantID, taskID, shedID string, partitionLabel ...string) (domain.ShedCompletionSummary, error)
}
