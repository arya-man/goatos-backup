// Package ports declares the Verification module's storage and media-resolution boundaries.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

var (
	ErrNotFound            = errors.New("verification: not found")
	ErrConflict            = errors.New("verification: write conflict")
	ErrIdempotencyConflict = errors.New("verification: idempotency key reused with different payload")
)

// ErrBatchNotFullyVerified is CloseVaccinationBatch's specific refusal when at least one animal in
// the drive is still pending or was rejected: leadership cannot sign off on a drive with unverified
// work. Blocking names the animals still standing between the batch and closure (verification_items
// subject_label, falling back to the ref_id when no label was captured) so the caller can render
// "N animals still awaiting verification: G-00X, G-00Y..." instead of a bare conflict.
type ErrBatchNotFullyVerified struct {
	Blocking []string
}

func (e *ErrBatchNotFullyVerified) Error() string {
	return "verification: batch has unverified or rejected animals"
}

// ListQueueParams filters + keysets one page of the verifier queue.
type ListQueueParams struct {
	TenantID   string
	Category   string   // Single category (backward compatible; when Categories is set, this is ignored)
	Categories []string // Multiple categories for cross-category verifier queries (mutually exclusive with Category)
	Vertical   string
	Module     string
	Status     string // defaults to domain.StatusPending in the app layer.
	// BusinessDate is the requested Asia/Kolkata capture date (YYYY-MM-DD). MissedOnly selects
	// pending items captured before today's business-day start; the two scopes are exclusive.
	BusinessDate string
	MissedOnly   bool
	// CapturedFrom/CapturedBefore are app-normalized UTC instants. Repositories keep captured_at
	// bare in predicates so verification_items_queue_idx remains usable.
	CapturedFrom   *time.Time
	CapturedBefore *time.Time
	MissedBefore   *time.Time
	ParkID         string
	ShedID         string
	Cursor         *domain.Cursor
	Limit          int
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
	// AwaitingApplicationOnly narrows the page to items a verifier already decided but whose
	// producing module has NOT yet confirmed it applied the outcome (domain.VerdictStateApplying).
	// It exists so the verifier's own surface can show "you decided this, it has not landed yet"
	// instead of letting the item vanish out of the pending queue with no trace. Only producers
	// that opted into the ack protocol (applier_ack_expected) can ever appear here.
	AwaitingApplicationOnly bool
	// IsVerifierQueueRead indicates this is a verifier queue read (verification.review path).
	// For verifier queue reads, do not clamp BusinessDate to today — return the full pending
	// backlog ordered oldest-first with the existing keyset cursor.
	IsVerifierQueueRead bool
}

// Repository is the Verification module's persistence boundary. Adapters own the outbox insert for
// the status-event seam (item pending / verdict approved / verdict rework) in the SAME transaction
// as the state change, matching the vaccination/sop precedent.
type Repository interface {
	// CreateItem enqueues one verification item. A replay with the same (tenant_id, idempotency_key)
	// is a no-op that returns the original row (Created=false).
	CreateItem(ctx context.Context, in domain.CreateItem) (domain.CreateItemResult, error)
	GetItem(ctx context.Context, tenantID, itemID string) (domain.Item, error)
	// GetItemCategories batch-resolves item_id -> category for every id in itemIDs in ONE query
	// (= ANY($1)), never one GetItem per id in a loop -- see the review-event batch validator,
	// which authorizes every event's item against the caller's categories and would otherwise be
	// an n-plus-one-fanout over a batch that can legitimately span many items.
	GetItemCategories(ctx context.Context, tenantID string, itemIDs []string) (map[string]string, error)
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
	// MarkVerdictApplied is the producing module's receipt that its applier wrote the verdict's
	// outcome onto its OWN record. It writes no outcome state and publishes no event -- it only
	// stamps applied_at/applied_by_module so a decided item stops reading as "still being applied".
	// See the adapter for why a verdict needs an ack at all (the applier runs on the durable bus,
	// so the verdict's submission and its application are different moments).
	MarkVerdictApplied(ctx context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string, appliedByModule string) (int, error)
}

// ReviewEventRepository is the video-review-analytics ingest + read boundary
// (verification_review_events, migration 000116). Kept as its own interface rather than folded into
// Repository so a category producer package cannot accidentally depend on write-side verdict
// methods it has no business calling.
type ReviewEventRepository interface {
	// InsertReviewEvents appends one batch of client review-telemetry events. Idempotent per event:
	// a row whose (tenant_id, client_event_id) already exists is skipped, so a replayed batch (retry
	// after a network blip) inserts nothing new and returns the count of ACTUALLY new rows.
	InsertReviewEvents(ctx context.Context, batch domain.ReviewEventBatch) (inserted int, err error)
	// ItemReviewFacts computes the derived per-actor watch/timing facts for one item from its raw
	// event stream (bounded by that item's event count; see review_facts.go for the computation).
	ItemReviewFacts(ctx context.Context, tenantID, itemID string) ([]domain.ItemReviewFacts, error)
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

// AlreadyDecidedError is a verdict write refused because the item ALREADY carries a verdict, as
// distinct from losing a row_version race. It stays errors.Is-comparable to ErrConflict so every
// existing branch (and the R50-016 regression that pins "non-pending must fail with ErrConflict")
// keeps matching, while the HTTP layer can tell the reader the decision is final instead of
// blaming a colleague who never touched the item.
type AlreadyDecidedError struct {
	CurrentStatus string
}

func (e *AlreadyDecidedError) Error() string {
	return "verification: item already decided (" + e.CurrentStatus + ")"
}

func (e *AlreadyDecidedError) Is(target error) bool { return target == ErrConflict }

// AlreadyDecided wraps the current status, degrading to the bare sentinel when it is unknown.
func AlreadyDecided(currentStatus string) error {
	if currentStatus == "" {
		return ErrConflict
	}
	return &AlreadyDecidedError{CurrentStatus: currentStatus}
}
