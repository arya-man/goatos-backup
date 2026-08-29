package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// Request-level idempotency guard for the feed-direction module's ONE write path (feed completion).
// It mirrors the obligation/procurement request-level guard exactly: every mutating write reserves
// its client-supplied idempotency key together with a semantic request fingerprint in the SAME
// transaction as its side effects. An exact replay (same key + same fingerprint) returns the original
// result and reruns no side effects; a same-key/different-payload replay is rejected with
// ports.ErrIdempotencyConflict; once the key is reserved no further mutation runs on replay.

type idemReservation struct {
	// proceed is true on the FIRST claim -- run side effects then completeIdempotency. When false the
	// key was already reserved (replay): run NO side effects, return the original result via resultID.
	proceed  bool
	resultID string
	// snapshot is the original response body recorded by the first call. Empty for older keys and
	// immutable result shapes that can still safely be re-read by resultID.
	snapshot []byte
}

func idemScopedKey(tenantID, scope, key string) string {
	return tenantID + ":" + scope + ":" + strings.TrimSpace(key)
}

// requestFingerprint hashes the fields that define the completion's effect (order matters).
func requestFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func reserveIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, fingerprint string) (idemReservation, error) {
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

func completeIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string) error {
	return completeIdempotencyWithSnapshot(ctx, tx, tenantID, scope, key, resultType, resultID, nil)
}

func completeIdempotencyWithSnapshot(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string, snapshot any) error {
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
