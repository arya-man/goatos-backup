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

	// SM-4 sweeper: list unbatched due obligations for a version (idempotent input) + attach a set
	// to a batch (only still-unbatched rows; returns count attached).
	ListUnbatchedDueForVersion(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32) ([]domain.UnbatchedDue, error)
	AttachObligationsToBatch(ctx context.Context, tenantID, batchID string, obligationIDs []string) (int64, error)

	// CancelOpenForGoat cancels a goat's scheduled/due obligations (SM-3) + writes 'canceled'
	// events, in one txn. Idempotent; completed/accepted history untouched. Returns count canceled.
	CancelOpenForGoat(ctx context.Context, tenantID, goatID, reason string) (int, error)

	// RecordStatusEvent appends a status event guarded by a reserve-before-insert against the
	// shared idempotency_keys table, inside one transaction. applied is false on retry (the key
	// was already reserved), so retries never duplicate status events.
	RecordStatusEvent(ctx context.Context, ev domain.NewStatusEvent) (eventID string, applied bool, err error)
}
