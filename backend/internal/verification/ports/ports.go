// Package ports declares the Verification module's storage and media-resolution boundaries.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

var (
	ErrNotFound = errors.New("verification: not found")
	ErrConflict = errors.New("verification: write conflict")
)

// ListQueueParams filters + keysets one page of the verifier queue.
type ListQueueParams struct {
	TenantID string
	Category string
	Vertical string
	Module   string
	Status   string // defaults to domain.StatusPending in the app layer.
	Cursor   *domain.Cursor
	Limit    int
	// ParkIDs is applied only when ScopeRestricted is true. An empty ParkIDs slice with a
	// restricted scope intentionally returns zero rows.
	ParkIDs         []string
	ScopeRestricted bool
	// ReadyForClosure restricts the page to open items whose whole source submission is approved.
	// It is used by leadership; verifier pages leave it false.
	ReadyForClosure bool
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
	// RecordVerdict applies an approve/reject decision with optimistic concurrency on RowVersion.
	// Returns ErrConflict on a stale RowVersion, ErrNotFound when the item does not exist.
	RecordVerdict(ctx context.Context, in domain.Verdict) (domain.Item, error)
	// CloseItem applies the leadership closure after verifier approval and emits the durable close
	// event in the same transaction.
	CloseItem(ctx context.Context, in domain.CloseAction) (domain.Item, error)
	// CloseSubmission atomically closes every independently approved item in one source submission.
	CloseSubmission(ctx context.Context, in domain.CloseSubmissionAction) ([]domain.Item, error)
}

// MediaResolver resolves proof IDs to streamed, signed download URLs via the EXISTING proof storage
// port (proof.Service.DownloadURL) — verification never proxies or duplicates media bytes.
type MediaResolver interface {
	ResolveMedia(ctx context.Context, tenantID string, proofIDs []string) ([]domain.MediaItem, error)
}
