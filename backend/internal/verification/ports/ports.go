// Package ports declares the Verification module's storage and media-resolution boundaries.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

var (
	ErrNotFound            = errors.New("verification: not found")
	ErrConflict            = errors.New("verification: write conflict")
	ErrIdempotencyConflict = errors.New("verification: idempotency key reused with different payload")
)

// ListQueueParams filters + keysets one page of the verifier queue.
type ListQueueParams struct {
	TenantID string
	Category string
	Vertical string
	Module   string
	Status   string // defaults to domain.StatusPending in the app layer.
	ParkID   string
	ShedID   string
	Cursor   *domain.Cursor
	Limit    int
	// ParkIDs is applied only when ScopeRestricted is true. An empty ParkIDs slice with a
	// restricted scope intentionally returns zero rows.
	ParkIDs         []string
	ScopeRestricted bool
	// ReadyForClosure restricts the page to open items whose whole source submission is approved.
	// It is used by leadership; verifier pages leave it false.
	ReadyForClosure bool
	// IncludeAllStatuses leaves Status empty and returns pending/approved/rejected rows. It is used
	// by leadership review, where the Director must see pending and rejected proof videos too.
	IncludeAllStatuses bool
	// SubmissionScopedOnly restricts leadership review to proof rows emitted by an operator
	// submission, excluding any future ad-hoc verification items.
	SubmissionScopedOnly bool
	// OpenOnly hides rows already closed by leadership.
	OpenOnly bool
}

// Repository is the Verification module's persistence boundary. Adapters own the outbox insert for
// the status-event seam (item pending / verdict approved / verdict rework) in the SAME transaction
// as the state change, matching the vaccination/sop precedent.
type Repository interface {
	// CreateItem enqueues one verification item. A replay with the same (tenant_id, idempotency_key)
	// is a no-op that returns the original row (Created=false).
	CreateItem(ctx context.Context, in domain.CreateItem) (domain.CreateItemResult, error)
	GetItem(ctx context.Context, tenantID, itemID string) (domain.Item, error)
	GetSubmissionItems(ctx context.Context, tenantID, submissionID string) ([]domain.Item, error)
	// ListQueue returns Limit+1 rows (the app layer trims to Limit and derives next_cursor) ordered
	// by (captured_at, item_id) ascending — keyset, never OFFSET.
	ListQueue(ctx context.Context, params ListQueueParams) ([]domain.Item, error)
	ListQueueFilterOptions(ctx context.Context, params ListQueueParams) (domain.QueueFilterOptions, error)
	// RecordVerdict applies an approve/reject decision with optimistic concurrency on RowVersion.
	// Returns ErrConflict on a stale RowVersion, ErrNotFound when the item does not exist.
	RecordVerdict(ctx context.Context, in domain.Verdict) (domain.Item, error)
	// CloseItem applies the leadership closure after verifier approval and emits the durable close
	// event in the same transaction.
	CloseItem(ctx context.Context, in domain.CloseAction) (domain.Item, error)
	// CloseSubmission atomically closes every independently approved item in one source submission.
	CloseSubmission(ctx context.Context, in domain.CloseSubmissionAction) ([]domain.Item, error)
	// ListReadyVaccinationBatchClosures returns batch-level close candidates that are ready now.
	ListReadyVaccinationBatchClosures(ctx context.Context, params ListQueueParams) ([]domain.VaccinationBatchClosure, error)
	// CloseVaccinationBatch closes a whole vaccination batch/drive, which may span multiple days.
	CloseVaccinationBatch(ctx context.Context, in domain.CloseVaccinationBatchAction) ([]domain.Item, error)
	// WithdrawItemsBySource retires the still-pending items raised for source records the producing
	// module has superseded (e.g. a weighing bucket reopened for rework: the submission those items
	// point at is no longer the bucket's work). Withdrawal is NOT a verdict — it decides nothing, it
	// only stops an item being decidable, so a verifier can never approve superseded work and have
	// the UI report that non-decision as success. Already-decided items are left untouched.
	WithdrawItemsBySource(ctx context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string) (int, error)
}

// MediaResolver resolves proof IDs to streamed, signed download URLs via the EXISTING proof storage
// port (proof.Service.DownloadURL) — verification never proxies or duplicates media bytes.
type MediaResolver interface {
	ResolveMedia(ctx context.Context, tenantID string, proofIDs []string) ([]domain.MediaItem, error)
}

// ErrEvidenceMissing means the item's proof rows resolve but at least one stored object is gone.
// It is terminal: retrying the same approve can never succeed.
var ErrEvidenceMissing = errors.New("verification: proof evidence object is missing")

// EvidenceAvailabilityChecker proves the item's proof BYTES still exist, not merely that a link
// could be signed for them.
//
// This is the verdict-time gate ONLY, for one item. It is deliberately NOT part of MediaResolver
// and is deliberately not called from ListQueue: statting every proof of every row on a 20-row page
// is the N+1 the queue read correctly refuses (see Service.resolveMedia). One irreversible approve
// paying one stat per proof is a completely different cost shape from a hot list read paying
// page_size x proofs_per_row.
type EvidenceAvailabilityChecker interface {
	// EnsureEvidenceAvailable returns ErrEvidenceMissing when any object is gone, or another error
	// when the check itself could not be completed (unknown, not proof of absence).
	EnsureEvidenceAvailable(ctx context.Context, tenantID string, proofIDs []string) error
}
