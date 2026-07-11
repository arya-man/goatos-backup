// Package ports declares the obligation domain's repository boundary.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// ErrNotFound is returned when a requested obligation row does not exist.
var ErrNotFound = errors.New("obligation: not found")

// ErrIdempotencyConflict is returned when a client-supplied Idempotency-Key is replayed with a
// different request payload than the one it was first reserved with (mirrors procurement's
// ports.ErrIdempotencyConflict for the same shared idempotency_keys contract).
var ErrIdempotencyConflict = errors.New("obligation: idempotency key reused with different payload")

// Repository is the persistence boundary for the obligation (due-state) layer. Implementations
// wrap generated sqlc queries; no hand-written SQL leaks above this interface.
type Repository interface {
	Ping(ctx context.Context) error

	// InsertObligation generates one obligation. applied is false when an obligation with the
	// same (tenant, idempotency_key) already exists (replay no-op).
	InsertObligation(ctx context.Context, in domain.NewObligation) (obligationID string, applied bool, err error)
	GetByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (domain.ObligationRef, error)

	// ListDue returns scheduled/due obligations whose due_at <= dueBefore (due-window scan).
	ListDue(ctx context.Context, tenantID, status string, dueBefore time.Time, limit int32) ([]domain.DueObligation, error)
	CountByScope(ctx context.Context, tenantID, scopeType, scopeID, status string) (int64, error)

	CreateBatch(ctx context.Context, in domain.NewBatch) (batchID string, err error)
	CreateBatchWithObligations(ctx context.Context, in domain.NewBatch, obligationIDs []string) (batchID string, attached int64, err error)
	SetBatchSOPTask(ctx context.Context, tenantID, batchID, taskID string) error
	MarkBatchStockBlocked(ctx context.Context, tenantID, batchID, itemID string, requiredQty int64, reason string) error
	ClearBatchStockBlock(ctx context.Context, tenantID, batchID string) error
	ListPlannedBatchesNeedingFinalization(ctx context.Context, tenantID, versionID string, needsTask, needsStock bool, limit int32) ([]domain.PlannedBatchFinalization, error)

	// SM-4 sweeper: list unbatched due obligations for a version (idempotent input) + attach a set
	// to a batch (only still-unbatched rows; returns count attached).
	ListUnbatchedDueForVersion(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32) ([]domain.UnbatchedDue, error)
	ListUnbatchedShedDueForParkConsolidation(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32, after *domain.ParkConsolidationCursor) ([]domain.ParkConsolidationCandidate, error)
	CountAttachedObligationsByRule(ctx context.Context, tenantID, batchID string) ([]domain.RuleAttachmentCount, error)
	AttachObligationsToBatch(ctx context.Context, tenantID, batchID string, obligationIDs []string) (int64, error)

	// MarkCompleted marks an obligation completed (SM-5) + writes a 'completed' event, in one txn.
	// Returns false (no-op) when already terminal. Idempotent.
	MarkCompleted(ctx context.Context, tenantID, obligationID string) (bool, error)

	// MarkMissedBefore marks open obligations whose deadline/window has crossed as missed and
	// writes one 'missed' event per transition. Idempotent and batch-limited for sweepers.
	MarkMissedBefore(ctx context.Context, tenantID string, missedBefore time.Time, limit int32) (int, error)

	// GetBoosterContext returns an obligation's protocol version, scope, and sequence (SM-7 basis on
	// the verify path). Returns ErrNotFound when the obligation does not exist.
	GetBoosterContext(ctx context.Context, tenantID, obligationID string) (versionID, scopeType, scopeID string, sequence int32, err error)

	// ListOpenByGoat returns a goat's still-open obligations, earliest due first (Goat Passport).
	ListOpenByGoat(ctx context.Context, tenantID, goatID string, limit int32) ([]domain.OpenObligation, error)

	// ReScopeOpenForGoat moves a shifted goat's open, unbatched obligations to a new scope (SM-2)
	// + writes 'rescoped' events, in one txn. Completed/in-progress/batched rows untouched.
	// Idempotent; returns count re-scoped.
	ReScopeOpenForGoat(ctx context.Context, tenantID, goatID, scopeType, scopeID string) (int, error)

	// CancelOpenForGoat cancels a goat's scheduled/due obligations (SM-3) + writes 'canceled'
	// events, in one txn. Idempotent; completed/accepted history untouched. Returns count canceled.
	CancelOpenForGoat(ctx context.Context, tenantID, goatID, reason string) (int, error)

	// RecordStatusEvent appends a status event guarded by a reserve-before-insert against the
	// shared idempotency_keys table, inside one transaction. applied is false on retry (the key
	// was already reserved), so retries never duplicate status events.
	RecordStatusEvent(ctx context.Context, ev domain.NewStatusEvent) (eventID string, applied bool, err error)

	// CancelOpenObligationByIdempotencyKey closes one open obligation by deterministic key. Used when
	// a placeholder/generated row is superseded by better source data.
	CancelOpenObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey, reason string, occurredAt time.Time) (obligationID string, changed bool, err error)
}
