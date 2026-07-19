package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

type idemReservation struct {
	proceed  bool
	resultID string
}

func requestFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func scopedIdempotencyKey(tenantID, scope, key string) string {
	return tenantID + ":" + scope + ":" + strings.TrimSpace(key)
}

func reserveIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, fingerprint string) (idemReservation, error) {
	if strings.TrimSpace(key) == "" {
		return idemReservation{proceed: true}, nil
	}
	scoped := scopedIdempotencyKey(tenantID, scope, key)
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
	if err := tx.QueryRow(ctx, `
SELECT request_hash, COALESCE(result_id::text, '')
FROM idempotency_keys
WHERE idempotency_key = $1`, scoped).Scan(&existingHash, &resultID); err != nil {
		return idemReservation{}, err
	}
	if existingHash != fingerprint {
		return idemReservation{}, ports.ErrIdempotencyConflict
	}
	return idemReservation{resultID: resultID}, nil
}

func completeIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, result_id = $3::uuid, completed_at = now()
WHERE idempotency_key = $1`, scopedIdempotencyKey(tenantID, scope, key), resultType, resultID)
	return err
}
