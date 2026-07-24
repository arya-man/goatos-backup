package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// Idempotency guard for the roster module's REQUEST-level write paths (POST /admin/roster/positions,
// POST /admin/roster/leave, etc.), modeled on the obligation module's proven implementation
// (backend/internal/obligation/adapters/postgres/idempotency.go). Every mutating write reserves
// its client-supplied idempotency key together with a semantic request fingerprint in the SAME
// transaction as its side effects. An exact replay (same key + same fingerprint) returns the original
// result without rerunning any side effects; a same-key/different-payload replay is rejected with
// ports.ErrIdempotencyConflict; and once the key is reserved no further state mutation runs on replay.

// idemReservation is the outcome of reserving an idempotency key inside a write transaction.
type idemReservation struct {
	// proceed is true on the FIRST claim of the key — the caller must run its side effects and then call
	// completeIdempotency. When false, the key was already reserved: this is a replay, the caller must run
	// NO side effects and instead return the original result keyed by resultID.
	proceed  bool
	resultID string
	// snapshot is the ORIGINAL response body recorded by the first call (idempotency_keys.
	// result_snapshot). On a replay the caller must return THIS, not a fresh read of the record: the
	// record is mutable, so a later unrelated edit would otherwise leak out under this key (BUG-037).
	// Empty for keys written before migration 000043, where the caller falls back to a read by
	// resultID.
	snapshot []byte
}

// idemScopedKey namespaces a client idempotency key by tenant + operation so the global idempotency_keys
// primary key cannot collide across tenants, nor across different roster operations that happen to
// receive the same client key.
func idemScopedKey(tenantID, scope, key string) string {
	return tenantID + ":roster:" + scope + ":" + strings.TrimSpace(key)
}

// requestFingerprint is the semantic request hash used to detect same-key/different-payload replays.
// Build it from the fields that define the operation's effect (order matters).
func requestFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// reserveIdempotency claims (scope:key) for this write inside tx. On first claim it returns proceed=true.
// On replay with a matching fingerprint it returns proceed=false plus the stored result id so the caller
// can re-read and return the original result. On replay with a DIFFERENT fingerprint it returns
// ports.ErrIdempotencyConflict so the caller rejects the request without mutating state.
func reserveIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, fingerprint string) (idemReservation, error) {
	if strings.TrimSpace(key) == "" {
		// No idempotency key provided; skip reservation (not idempotent)
		return idemReservation{proceed: true}, nil
	}
	scoped := idemScopedKey(tenantID, scope, key)
	var claimed string
	err := tx.QueryRow(ctx, `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status)
VALUES ($1, $2::uuid, $3, $4, 'started')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key`, scoped, tenantID, scope, fingerprint).Scan(&claimed)
	if err == nil {
		return idemReservation{proceed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return idemReservation{}, err
	}
	// Already reserved: this is a replay. Compare the fingerprint and surface the original result.
	var existingHash, resultID string
	var snapshot []byte
	if err := tx.QueryRow(ctx, `
SELECT request_hash, COALESCE(result_id::text, ''), result_snapshot
FROM idempotency_keys
WHERE idempotency_key = $1`, scoped).Scan(&existingHash, &resultID, &snapshot); err != nil {
		return idemReservation{}, err
	}
	if existingHash != fingerprint {
		return idemReservation{}, ports.ErrIdempotencyConflict
	}
	return idemReservation{proceed: false, resultID: resultID, snapshot: snapshot}, nil
}

// completeIdempotency marks the key completed with the produced result so future replays return it.
// snapshot is the response body this call produced, serialized in the SAME transaction as the side
// effects. Persisting it (rather than only result_id) is what makes an exact replay return the
// ORIGINAL result instead of the record's current state (BUG-037). Pass nil only for a result that
// is genuinely immutable after creation.
func completeIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string, snapshot any) error {
	if strings.TrimSpace(key) == "" {
		// No idempotency key; skip (not idempotent)
		return nil
	}
	var encoded []byte
	if snapshot != nil {
		var err error
		encoded, err = json.Marshal(snapshot)
		if err != nil {
			return err
		}
	}
	scoped := idemScopedKey(tenantID, scope, key)
	_, err := tx.Exec(ctx, `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, result_id = nullif($3::text, '')::uuid,
    result_snapshot = $4::jsonb, completed_at = now()
WHERE idempotency_key = $1`, scoped, resultType, resultID, encoded)
	return err
}

// txPosition reads one position INSIDE the write transaction, so the response recorded as the
// idempotency snapshot is exactly the state this transaction produced -- not a post-commit re-read
// that a concurrent writer could have already moved on from.
func txPosition(ctx context.Context, tx pgx.Tx, tenantID, positionID string) (domain.Position, error) {
	rows, err := tx.Query(ctx, positionSelectSQL(`
WHERE p.tenant_id = $1::uuid AND p.position_id = $2::uuid
LIMIT 1`), tenantID, positionID)
	if err != nil {
		return domain.Position{}, err
	}
	items, err := scanPositions(rows)
	if err != nil {
		return domain.Position{}, err
	}
	if len(items) == 0 {
		return domain.Position{}, ports.ErrNotFound
	}
	return items[0], nil
}

// txLeave is the workforce_absences twin of txPosition.
func txLeave(ctx context.Context, tx pgx.Tx, tenantID, absenceID string) (domain.StaffLeave, error) {
	rows, err := tx.Query(ctx, leaveSelectSQL(`
WHERE tenant_id = $1::uuid AND absence_id = $2::uuid
LIMIT 1`), tenantID, absenceID)
	if err != nil {
		return domain.StaffLeave{}, err
	}
	items, err := scanLeaves(rows)
	if err != nil {
		return domain.StaffLeave{}, err
	}
	if len(items) == 0 {
		return domain.StaffLeave{}, ports.ErrNotFound
	}
	return items[0], nil
}

// replayLeave decodes the ORIGINAL leave response recorded for an exact replay. ok is false for
// pre-000043 keys with no snapshot, where the caller falls back to reading by result id.
func replayLeave(res idemReservation) (domain.StaffLeave, bool, error) {
	if len(res.snapshot) == 0 {
		return domain.StaffLeave{}, false, nil
	}
	var leave domain.StaffLeave
	if err := json.Unmarshal(res.snapshot, &leave); err != nil {
		return domain.StaffLeave{}, false, err
	}
	return leave, true, nil
}

// replayPosition decodes the ORIGINAL position response recorded for an exact replay. ok is false for
// pre-000043 keys with no snapshot, where the caller falls back to reading by result id.
func replayPosition(res idemReservation) (domain.Position, bool, error) {
	if len(res.snapshot) == 0 {
		return domain.Position{}, false, nil
	}
	var pos domain.Position
	if err := json.Unmarshal(res.snapshot, &pos); err != nil {
		return domain.Position{}, false, err
	}
	return pos, true, nil
}
