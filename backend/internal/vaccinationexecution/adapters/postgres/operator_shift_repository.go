package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/ports"
)

// ---- Authored operator shifts (vaccination_operator_shift_config) ----
//
// The only production write path for a park's operator shifts. Before it existed the table was
// filled by the roster seed command alone, and UpdateOperatorAssignmentConfig refuses any operator
// without a shift row -- so a park added on Configuration > Items & settings could never be given
// vaccination operators.
//
// Every write is ONE transaction: reserve the Idempotency-Key with the request fingerprint, check the
// operator really works in that park, write the shift, enqueue vaccination.roster.changed to the
// outbox (the same envelope UpsertOperatorAssignmentConfig emits, consumed by the obligation
// OperatorConfigReplanHandler), write the audit row, complete the key. A failure anywhere rolls all
// of it back. An exact replay returns the original result without a second write or event.

// operatorShiftIdemScope namespaces this write's keys inside the shared idempotency_keys table.
const operatorShiftIdemScope = "vaccination.operator_shift"

const (
	operatorShiftResultSet     = "vaccination_operator_shift_set"
	operatorShiftResultCleared = "vaccination_operator_shift_cleared"
)

const (
	sqlOperatorShiftIdemReserve = `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status)
VALUES ($1, $2::uuid, $3, $4, 'started')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key`

	sqlOperatorShiftIdemRead = `
SELECT request_hash
FROM idempotency_keys
WHERE idempotency_key = $1`

	sqlOperatorShiftIdemComplete = `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, result_id = $3::uuid, completed_at = now()
WHERE idempotency_key = $1`

	// Locks the one (tenant, operator, park) row so the before/after comparison deciding whether the
	// roster actually changed is race-free.
	sqlOperatorShiftLockPrior = `
SELECT shift_label, shift_start_minute, shift_end_minute, COALESCE(week_off_weekday, '')
FROM vaccination_operator_shift_config
WHERE tenant_id = $1::uuid AND operator_id = $2::uuid AND park_id = $3::uuid
FOR UPDATE`

	// Every column the conflict branch writes is real shift state, not just the key.
	sqlOperatorShiftUpsert = `
INSERT INTO vaccination_operator_shift_config
  (tenant_id, operator_id, park_id, shift_label, shift_start_minute, shift_end_minute, week_off_weekday, updated_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, NULLIF($7, ''), now())
ON CONFLICT (tenant_id, operator_id, park_id) DO UPDATE
SET shift_label = EXCLUDED.shift_label,
    shift_start_minute = EXCLUDED.shift_start_minute,
    shift_end_minute = EXCLUDED.shift_end_minute,
    week_off_weekday = EXCLUDED.week_off_weekday,
    updated_at = now()`

	sqlOperatorShiftDisplayName = `
SELECT COALESCE(display_name, '')
FROM workforce_members
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid`

	// Locks the park's assignment config row so a clear cannot interleave with a config write that is
	// about to name this operator.
	sqlOperatorShiftAssignmentUse = `
SELECT default_operator_id = $3::uuid OR $3::uuid = ANY(selected_operator_ids)
FROM vaccination_operator_assignment_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
FOR UPDATE`

	sqlOperatorShiftDelete = `
DELETE FROM vaccination_operator_shift_config
WHERE tenant_id = $1::uuid AND operator_id = $2::uuid AND park_id = $3::uuid
RETURNING shift_label, shift_start_minute, shift_end_minute, COALESCE(week_off_weekday, '')`

	// The ON CONFLICT arbiter is the per-event-type partial unique index (migration 000038); the WHERE
	// names the same event_type literal.
	sqlOperatorShiftEnqueueRosterChanged = `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES ($1::uuid, $2::uuid, 'vaccination.roster.changed', '1.0.0', 'park', $3::uuid,
  'vaccination.events', $4::jsonb, $5::jsonb, $6, $6, 'pending', now())
ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'vaccination.roster.changed' DO NOTHING`
)

// SetOperatorShift creates or replaces one operator's shift for a park. See ports.Repository.
func (r *Repository) SetOperatorShift(ctx context.Context, w ports.OperatorShiftWrite) (domain.OperatorShift, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	shift := w.Shift
	fp := operatorShiftFingerprint("set", shift.ParkID, shift.OperatorID, shift.ShiftLabel,
		fmt.Sprint(shift.ShiftStartMinute), fmt.Sprint(shift.ShiftEndMinute), shift.WeekOffWeekday)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.OperatorShift{}, false, fmt.Errorf("vaccination execution: begin operator shift tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	proceed, err := reserveOperatorShiftKey(ctx, tx, w.TenantID, w.IdempotencyKey, fp)
	if err != nil {
		return domain.OperatorShift{}, false, err
	}
	if !proceed {
		// Exact replay: the fingerprint matched, so the request IS the shift that was written.
		if err := tx.QueryRow(ctx, sqlOperatorShiftDisplayName, w.TenantID, shift.OperatorID).Scan(&shift.DisplayName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return domain.OperatorShift{}, false, fmt.Errorf("vaccination execution: read operator name: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.OperatorShift{}, false, fmt.Errorf("vaccination execution: commit operator shift replay: %w", err)
		}
		return shift, true, nil
	}

	if err := validateVaccinationOperatorPark(ctx, tx, w.TenantID, shift.ParkID, shift.OperatorID); err != nil {
		return domain.OperatorShift{}, false, err
	}

	var prior domain.OperatorShift
	found := true
	if err := tx.QueryRow(ctx, sqlOperatorShiftLockPrior, w.TenantID, shift.OperatorID, shift.ParkID).
		Scan(&prior.ShiftLabel, &prior.ShiftStartMinute, &prior.ShiftEndMinute, &prior.WeekOffWeekday); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.OperatorShift{}, false, fmt.Errorf("vaccination execution: read prior operator shift: %w", err)
		}
		found = false
	}

	if _, err := tx.Exec(ctx, sqlOperatorShiftUpsert, w.TenantID, shift.OperatorID, shift.ParkID, shift.ShiftLabel,
		shift.ShiftStartMinute, shift.ShiftEndMinute, shift.WeekOffWeekday); err != nil {
		return domain.OperatorShift{}, false, fmt.Errorf("vaccination execution: write operator shift: %w", err)
	}

	changed := !found || prior.ShiftLabel != shift.ShiftLabel || prior.ShiftStartMinute != shift.ShiftStartMinute ||
		prior.ShiftEndMinute != shift.ShiftEndMinute || prior.WeekOffWeekday != shift.WeekOffWeekday
	if changed {
		// The roster the planner reads changed (a new operator, a new week-off, a new label that
		// moves the PM-cover order), so future drives must be re-planned.
		if err := enqueueOperatorShiftRosterChanged(ctx, tx, w.TenantID, w.ActorID, shift.ParkID, shift.OperatorID, fp, w.IdempotencyKey); err != nil {
			return domain.OperatorShift{}, false, err
		}
	}

	var before any
	if found {
		before = operatorShiftAuditState(prior)
	}
	if err := recordOperatorShiftAudit(ctx, tx, w.TenantID, w.ActorID, w.TraceID, w.IdempotencyKey, "vaccination.operator_shift.set", shift.ParkID, shift.OperatorID, before, operatorShiftAuditState(shift)); err != nil {
		return domain.OperatorShift{}, false, err
	}

	if err := tx.QueryRow(ctx, sqlOperatorShiftDisplayName, w.TenantID, shift.OperatorID).Scan(&shift.DisplayName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.OperatorShift{}, false, fmt.Errorf("vaccination execution: read operator name: %w", err)
	}
	if err := completeOperatorShiftKey(ctx, tx, w.TenantID, w.IdempotencyKey, operatorShiftResultSet, shift.OperatorID); err != nil {
		return domain.OperatorShift{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.OperatorShift{}, false, fmt.Errorf("vaccination execution: commit operator shift: %w", err)
	}
	r.invalidateVaccinationReadCache(ctx, w.TenantID)
	return shift, false, nil
}

// ClearOperatorShift deletes one operator's shift for a park. See ports.Repository.
func (r *Repository) ClearOperatorShift(ctx context.Context, c ports.OperatorShiftClear) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	fp := operatorShiftFingerprint("clear", c.ParkID, c.OperatorID)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("vaccination execution: begin operator shift clear tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	proceed, err := reserveOperatorShiftKey(ctx, tx, c.TenantID, c.IdempotencyKey, fp)
	if err != nil {
		return false, err
	}
	if !proceed {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("vaccination execution: commit operator shift clear replay: %w", err)
		}
		return true, nil
	}

	// Refuse while the park's drive-operator assignment still names this operator: without the
	// shift the planner cannot resolve them and the park's drive planning fails closed.
	var inUse bool
	if err := tx.QueryRow(ctx, sqlOperatorShiftAssignmentUse, c.TenantID, c.ParkID, c.OperatorID).Scan(&inUse); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("vaccination execution: read operator assignment use: %w", err)
		}
		inUse = false
	}
	if inUse {
		return false, ports.ErrOperatorShiftInUse
	}

	var prior domain.OperatorShift
	if err := tx.QueryRow(ctx, sqlOperatorShiftDelete, c.TenantID, c.OperatorID, c.ParkID).
		Scan(&prior.ShiftLabel, &prior.ShiftStartMinute, &prior.ShiftEndMinute, &prior.WeekOffWeekday); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ports.ErrOperatorShiftNotFound
		}
		return false, fmt.Errorf("vaccination execution: delete operator shift: %w", err)
	}
	prior.OperatorID = c.OperatorID
	prior.ParkID = c.ParkID

	if err := enqueueOperatorShiftRosterChanged(ctx, tx, c.TenantID, c.ActorID, c.ParkID, c.OperatorID, fp, c.IdempotencyKey); err != nil {
		return false, err
	}
	if err := recordOperatorShiftAudit(ctx, tx, c.TenantID, c.ActorID, c.TraceID, c.IdempotencyKey, "vaccination.operator_shift.cleared", c.ParkID, c.OperatorID, operatorShiftAuditState(prior), nil); err != nil {
		return false, err
	}
	if err := completeOperatorShiftKey(ctx, tx, c.TenantID, c.IdempotencyKey, operatorShiftResultCleared, c.OperatorID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("vaccination execution: commit operator shift clear: %w", err)
	}
	r.invalidateVaccinationReadCache(ctx, c.TenantID)
	return false, nil
}

// operatorShiftFingerprint hashes what defines the write's effect.
func operatorShiftFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func operatorShiftScopedKey(tenantID, key string) string {
	return tenantID + ":" + operatorShiftIdemScope + ":" + strings.TrimSpace(key)
}

// reserveOperatorShiftKey claims the key for this request. It returns proceed=false for an exact
// replay (same key, same fingerprint) and ErrOperatorShiftIdempotencyConflict for a same key with a
// different request.
func reserveOperatorShiftKey(ctx context.Context, tx pgx.Tx, tenantID, key, fp string) (bool, error) {
	scoped := operatorShiftScopedKey(tenantID, key)
	var claimed string
	err := tx.QueryRow(ctx, sqlOperatorShiftIdemReserve, scoped, tenantID, operatorShiftIdemScope, fp).Scan(&claimed)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("vaccination execution: reserve operator shift key: %w", err)
	}
	var existing string
	if err := tx.QueryRow(ctx, sqlOperatorShiftIdemRead, scoped).Scan(&existing); err != nil {
		return false, fmt.Errorf("vaccination execution: read operator shift key: %w", err)
	}
	if existing != fp {
		return false, ports.ErrOperatorShiftIdempotencyConflict
	}
	return false, nil
}

func completeOperatorShiftKey(ctx context.Context, tx pgx.Tx, tenantID, key, resultType, operatorID string) error {
	if _, err := tx.Exec(ctx, sqlOperatorShiftIdemComplete, operatorShiftScopedKey(tenantID, key), resultType, operatorID); err != nil {
		return fmt.Errorf("vaccination execution: complete operator shift key: %w", err)
	}
	return nil
}

// enqueueOperatorShiftRosterChanged writes vaccination.roster.changed in the caller's transaction,
// in the envelope UpsertOperatorAssignmentConfig already emits for that event (park aggregate,
// payload {"park_id"}), so OperatorConfigReplanHandler consumes both producers identically. The
// deterministic key carries the park, the operator, the fingerprint of the new state and the
// request's own key: a relay retry dedupes, while every distinct write -- including setting a shift
// back to an earlier value -- re-plans.
func enqueueOperatorShiftRosterChanged(ctx context.Context, tx pgx.Tx, tenantID, actorID, parkID, operatorID, fp, requestKey string) error {
	reqSum := sha256.Sum256([]byte(strings.TrimSpace(requestKey)))
	idempotencyKey := fmt.Sprintf("vaccination.operator-shift.roster:%s:%s:%s:%s", parkID, operatorID, fp[:16], hex.EncodeToString(reqSum[:])[:16])
	eventID := platformoutbox.DeterministicUUID(idempotencyKey)
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
	actor := map[string]any{"actor_type": "human", "actor_id": nil, "actor_ref": nil}
	if uuidutil.IsUUIDString(actorID) {
		actor["actor_id"] = actorID
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     "vaccination.roster.changed",
		"schema_version": "1.0.0",
		"schema_ref":     "domain-event-envelope.v1",
		"aggregate_type": "park",
		"aggregate_id":   parkID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "vaccination-execution",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor":           actor,
		"subject_type":    "location",
		"subject_id":      parkID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
			"park_id":   parkID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "location",
			"evidence_id":   parkID,
		}},
		"payload":  map[string]any{"park_id": parkID},
		"trace_id": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("vaccination execution: marshal operator shift roster envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "vaccination-execution.OperatorShift",
		"schema_version":  "1.0.0",
		"park_id":         parkID,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("vaccination execution: marshal operator shift roster headers: %w", err)
	}
	if _, err := tx.Exec(ctx, sqlOperatorShiftEnqueueRosterChanged, tenantID, eventID, parkID, envelope, headers, idempotencyKey); err != nil {
		return fmt.Errorf("vaccination execution: enqueue operator shift roster change: %w", err)
	}
	return nil
}

func recordOperatorShiftAudit(ctx context.Context, tx pgx.Tx, tenantID, actorID, traceID, requestKey, action, parkID, operatorID string, before, after any) error {
	actor := ""
	if uuidutil.IsUUIDString(actorID) {
		actor = actorID
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actor,
		ActorType:    "human",
		Action:       action,
		ResourceType: "vaccination_operator_shift",
		ResourceID:   operatorID,
		ScopeType:    "park",
		ScopeID:      parkID,
		BeforeState:  before,
		AfterState:   after,
		TraceID:      traceID,
		Metadata: map[string]any{
			"module":          "vaccination-execution",
			"park_id":         parkID,
			"operator_id":     operatorID,
			"idempotency_key": requestKey,
		},
	}); err != nil {
		return fmt.Errorf("vaccination execution: audit operator shift: %w", err)
	}
	return nil
}

func operatorShiftAuditState(s domain.OperatorShift) map[string]any {
	return map[string]any{
		"shift_label":        s.ShiftLabel,
		"shift_start_minute": s.ShiftStartMinute,
		"shift_end_minute":   s.ShiftEndMinute,
		"week_off_weekday":   s.WeekOffWeekday,
	}
}
