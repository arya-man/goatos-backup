package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

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

// fpStrPtr safely renders an optional string for fingerprinting.
func fpStrPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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
	if err := tx.QueryRow(ctx, `
SELECT request_hash, COALESCE(result_id::text, '')
FROM idempotency_keys
WHERE idempotency_key = $1`, scoped).Scan(&existingHash, &resultID); err != nil {
		return idemReservation{}, err
	}
	if existingHash != fingerprint {
		return idemReservation{}, ports.ErrIdempotencyConflict
	}
	return idemReservation{proceed: false, resultID: resultID}, nil
}

// completeIdempotency marks the key completed with the produced result so future replays return it.
func completeIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string) error {
	if strings.TrimSpace(key) == "" {
		// No idempotency key; skip (not idempotent)
		return nil
	}
	scoped := idemScopedKey(tenantID, scope, key)
	_, err := tx.Exec(ctx, `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, result_id = nullif($3::text, '')::uuid, completed_at = now()
WHERE idempotency_key = $1`, scoped, resultType, resultID)
	return err
}
