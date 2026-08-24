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

// ErrDueDateTaken means the requested due date already holds a row for this
// (version, rule, animal). obligation_instances_dup_guard spans EVERY status, so the
// occupant may be open work, a dose already given, or a canceled row -- in each case the
// date is spoken for and the caller's row cannot move onto it.
//
// It is a sentinel rather than a raw 23505 because callers have to tell it apart from a
// genuine failure: a generation pass that treats "that date is taken" as an error poisons
// the animal for every later pass too.
var ErrDueDateTaken = errors.New("obligation: due date already taken for this rule and animal")

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
	ListPlannedBatchesNeedingFinalization(ctx context.Context, tenantID, versionID string, needsTask, needsStock bool, after *domain.PlannedBatchFinalizationCursor, limit int32) ([]domain.PlannedBatchFinalization, error)

	// SM-4 sweeper: list unbatched due obligations for a version (idempotent input) + attach a set
	// to a batch (only still-unbatched rows; returns count attached).
	ListUnbatchedDueForVersion(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32) ([]domain.UnbatchedDue, error)
	ListUnbatchedShedDueForParkConsolidation(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32, after *domain.ParkConsolidationCursor) ([]domain.ParkConsolidationCandidate, error)
	CountAttachedObligationsByRule(ctx context.Context, tenantID, batchID string) ([]domain.RuleAttachmentCount, error)
	AttachObligationsToBatch(ctx context.Context, tenantID, batchID string, obligationIDs []string) (int64, error)

	// MarkCompleted marks an obligation completed (SM-5) + writes a 'completed' event, in one txn.
	// Returns false (no-op) when already terminal. Idempotent. Also recomputes the owning batch's
	// status in the same tx (completed when no sibling obligation remains open, else planned ->
	// in_progress) so batch status stays live as a drive closes out (PEND-1 drive-close trigger).
	// When the batch stays open, every still-open (scheduled/due) sibling obligation on that batch is
	// ALSO flipped to in_progress in the same tx + one 'in_progress' status event/outbox row per
	// newly-transitioned sibling -- this is the reachable PEND-1 in_progress trigger (the first real
	// completion in a multi-obligation drive), replacing an earlier, unreachable SOP-submit-time
	// MarkInProgress writer that this Repository no longer exposes.
	MarkCompleted(ctx context.Context, tenantID, obligationID string) (bool, error)

	// ReopenObligation reverses MarkCompleted: a verification rejection sends a completed obligation
	// back to 'due' (maintainer state-model -- obligation reopens on rejection, closes on record).
	// Idempotent: only a currently-'completed' row is touched, so a stale replay or an obligation
	// already moved on to some other terminal status is left alone. Returns false (no-op) otherwise.
	ReopenObligation(ctx context.Context, tenantID, obligationID string) (bool, error)

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

	// SyncPartitionMoveForGoat updates a goat's goat_shed_partitions.partition_label for a
	// same-shed partition move (shed_id unchanged) and, in the SAME transaction, re-derives the
	// goat's vaccination drive-assignment membership for every batch its open obligations belong
	// to, so the goat's member row moves off its OLD partition's assignment arm onto the new one.
	// Returns the number of open obligations whose membership was re-derived. A no-op (0, nil) when
	// the partition label did not actually change. Returns an error if shed_id differs from the
	// goat's current goat_shed_partitions row -- cross-shed moves go through ReScopeOpenForGoatShift.
	SyncPartitionMoveForGoat(ctx context.Context, tenantID, goatID, shedID, partitionLabel, sourceShedName string) (int, error)

	// RecordStatusEvent appends a status event guarded by a reserve-before-insert against the
	// shared idempotency_keys table, inside one transaction. applied is false on retry (the key
	// was already reserved), so retries never duplicate status events.
	RecordStatusEvent(ctx context.Context, ev domain.NewStatusEvent) (eventID string, applied bool, err error)

	// CancelOpenObligationByIdempotencyKey closes one open obligation by deterministic key. Used when
	// a placeholder/generated row is superseded by better source data.
	CancelOpenObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey, reason string, occurredAt time.Time) (obligationID string, changed bool, err error)

	// NextSuccessorSuffix computes the next available numeric successor suffix for a base idempotency
	// key, e.g. for baseKey "goat:123:cancel_reason", returns the lowest integer N where
	// "goat:123:cancel_reason:successor:NN" does not yet exist. Returns 1 if no successors exist yet.
	// One bounded query, never O(N) round trips.
	NextSuccessorSuffix(ctx context.Context, tenantID, baseKey string) (int, error)
}

// ErrAmbiguousOpenWork means an animal holds MORE THAN ONE open obligation for a single rule
// identity that no label covers -- work predating rule_identity_key, or work the backfill left
// alone precisely because it could not tell which row was real.
//
// Generation cannot proceed for that animal: adopting one row guesses which of two scheduled
// vaccinations to keep, inserting books a third. Both decide somebody's medical work, so the pass
// fails for this animal, reports which obligations to look at, and leaves every other animal in
// the run untouched.
var ErrAmbiguousOpenWork = errors.New("obligation: animal holds more than one unlabelled open obligation for this rule identity")
