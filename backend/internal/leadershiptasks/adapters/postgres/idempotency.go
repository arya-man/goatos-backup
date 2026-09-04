package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
)

// Idempotency guard for the write paths -- the shared idempotency_keys contract: reserve
// key + semantic fingerprint in the SAME transaction as the side effects; an exact replay
// returns the original result with no new side effects; a same-key different-payload
// replay is rejected.

type idemReservation struct {
	proceed    bool
	resultType string
	resultID   string
}

func idemScopedKey(tenantID, scope, key string) string {
	return tenantID + ":" + scope + ":" + strings.TrimSpace(key)
}

// requestFingerprint hashes the fields that define the operation's effect (order matters).
func requestFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func reserveIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, fingerprint string) (idemReservation, error) {
	scoped := idemScopedKey(tenantID, scope, key)
	var claimed string
	err := tx.QueryRow(ctx, sqlIdempotency1, scoped, tenantID, scope, fingerprint).Scan(&claimed)
	if err == nil {
		return idemReservation{proceed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return idemReservation{}, err
	}
	var existingHash, status, resultType, resultID string
	if err := tx.QueryRow(ctx, sqlIdempotency2, scoped).Scan(&existingHash, &status, &resultType, &resultID); err != nil {
		return idemReservation{}, err
	}
	if existingHash != fingerprint {
		return idemReservation{}, ports.ErrIdempotencyConflict
	}
	return idemReservation{proceed: false, resultType: resultType, resultID: resultID}, nil
}

func completeIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string) error {
	scoped := idemScopedKey(tenantID, scope, key)
	_, err := tx.Exec(ctx, sqlIdempotency3, scoped, resultType, resultID)
	return err
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
const (
	sqlIdempotency1 = `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status)
VALUES ($1, $2::uuid, $3, $4, 'started')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key`
	sqlIdempotency2 = `
SELECT request_hash, status, COALESCE(result_type, ''), COALESCE(result_id::text, '')
FROM idempotency_keys
WHERE idempotency_key = $1`
	sqlIdempotency3 = `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, result_id = nullif($3::text, '')::uuid, completed_at = now()
WHERE idempotency_key = $1`
)
