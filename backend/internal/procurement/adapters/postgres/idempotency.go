package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// Idempotency guard for procurement write paths.
//
// Contract (per AGENTS.md write-path idempotency rule): every mutating procurement write reserves its
// idempotency key together with a semantic request fingerprint in the SAME transaction as its side
// effects. An exact replay (same key + same fingerprint) returns the original result without rerunning any
// side effects; a same-key/different-payload replay is rejected; and once the key is reserved no further
// state mutation runs on replay. The shared idempotency_keys table (request_hash, status, result_type,
// result_id) backs all of this; the per-domain ON CONFLICT (tenant_id, idempotency_key) constraints remain
// as defense-in-depth but are no longer the primary guard.

// idemReservation is the outcome of reserving an idempotency key inside a write transaction.
type idemReservation struct {
	// proceed is true on the FIRST claim of the key — the caller must run its side effects and then call
	// completeIdempotency. When false, the key was already reserved: this is a replay, the caller must run NO
	// side effects and instead return the original result keyed by resultType/resultID.
	proceed    bool
	resultType string
	resultID   string
}

// idemScopedKey namespaces a client idempotency key by tenant + operation so the global idempotency_keys
// primary key cannot collide across tenants, nor across different procurement operations that happen to
// receive the same client key.
func idemScopedKey(tenantID, scope, key string) string {
	return tenantID + ":" + scope + ":" + strings.TrimSpace(key)
}

// requestFingerprint is the semantic request hash used to detect same-key/different-payload replays. Build
// it from the fields that define the operation's effect (order matters).
func requestFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// fpTime renders an optional timestamp as a stable, nil-safe fingerprint part (empty when absent).
func fpTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// canonicalJSON normalizes a JSON value for fingerprinting: object keys are sorted and insignificant
// whitespace is dropped, so the same payload with reordered keys hashes identically. Array order is
// preserved (it is semantic). UseNumber keeps numeric literals exact (no float rounding). Empty or invalid
// JSON falls back to the raw bytes so a non-JSON field still contributes to the fingerprint.
func canonicalJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

// reserveIdempotency claims (scope:key) for this write inside tx. On first claim it returns proceed=true.
// On replay with a matching fingerprint it returns proceed=false plus the stored result_type/result_id so
// the caller can re-read and return the original result. On replay with a DIFFERENT fingerprint it returns
// ports.ErrIdempotencyConflict so the caller rejects the request without mutating state.
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
	// Already reserved: this is a replay. Compare the fingerprint and surface the original result.
	var existingHash, status, resultType, resultID string
	if err := tx.QueryRow(ctx, `
SELECT request_hash, status, COALESCE(result_type, ''), COALESCE(result_id::text, '')
FROM idempotency_keys
WHERE idempotency_key = $1`, scoped).Scan(&existingHash, &status, &resultType, &resultID); err != nil {
		return idemReservation{}, err
	}
	if existingHash != fingerprint {
		return idemReservation{}, ports.ErrIdempotencyConflict
	}
	return idemReservation{proceed: false, resultType: resultType, resultID: resultID}, nil
}

// completeIdempotency marks the key completed with the produced result so future replays return it.
func completeIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string) error {
	scoped := idemScopedKey(tenantID, scope, key)
	_, err := tx.Exec(ctx, `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, result_id = nullif($3::text, '')::uuid, completed_at = now()
WHERE idempotency_key = $1`, scoped, resultType, resultID)
	return err
}
