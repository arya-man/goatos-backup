// Package postgres implements proof artifact persistence.
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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
)

const defaultQueryTimeout = 3 * time.Second
const abandonedUploadRetention = 24 * time.Hour

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

func (r *Repository) CreateProof(ctx context.Context, in domain.CreateUpload, provider string) (domain.Artifact, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	metadata, err := json.Marshal(nonNilMap(in.Metadata))
	if err != nil {
		return domain.Artifact{}, err
	}
	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	fingerprint := createFingerprint(in)
	row := r.pool.QueryRow(ctx, `
WITH new_proof AS (
  SELECT gen_random_uuid() AS proof_id
)
INSERT INTO proof_artifacts (
  proof_id, tenant_id, storage_provider, object_key, mime_type, scope_type, scope_id,
  subject_type, subject_id, proof_type, uploaded_by, metadata, idempotency_key, request_fingerprint,
  upload_expires_at
)
SELECT
  proof_id,
  $1::uuid,
  $2,
  $1::text || '/' || to_char(now(), 'YYYY/MM/DD') || '/' || proof_id::text,
  $3,
  $4,
  $5::uuid,
  $6,
  nullif($7, '')::uuid,
  $8,
  nullif($9, '')::uuid,
  $10::jsonb,
  nullif($11, ''),
  $12,
  now() + $13::interval
FROM new_proof
-- A repeat call with the SAME (tenant, idempotency_key) — the mobile outbox retries the whole
-- registration+upload dispatch with its stored key verbatim — must never mint a second object.
-- The partial unique index only covers non-NULL keys, so a caller with no key always inserts.
ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING
  proof_id::text, tenant_id::text, storage_provider, object_key, content_hash, mime_type,
  size_bytes, duration_ms, upload_state, scope_type, scope_id::text, subject_type,
  subject_id::text, proof_type, uploaded_by::text, metadata, created_at, uploaded_at,
  retention_policy, retention_expires_at, upload_expires_at, updated_at, row_version`,
		in.TenantID,
		provider,
		in.MimeType,
		in.ScopeType,
		in.ScopeID,
		in.SubjectType,
		ptrValue(in.SubjectID),
		in.ProofType,
		ptrValue(in.UploadedBy),
		metadata,
		idempotencyKey,
		fingerprint,
		abandonedUploadRetention.String(),
	)
	artifact, err := scanArtifact(row)
	if errors.Is(err, pgx.ErrNoRows) {
		if idempotencyKey == "" {
			// Should not happen (the partial index never fires for a NULL key), but never
			// swallow an unexpected zero-row insert into a false "replay".
			return domain.Artifact{}, err
		}
		return r.replayByIdempotencyKey(ctx, in.TenantID, idempotencyKey, fingerprint)
	}
	return artifact, err
}

// replayByIdempotencyKey resolves the CreateProof ON CONFLICT branch: the row that already
// owns (tenant_id, idempotency_key). An exact replay (same request_fingerprint) returns the
// original Artifact untouched — no second object, no mutation. A same-key/different-payload
// replay is rejected so it can never silently return an unrelated proof.
func (r *Repository) replayByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey, fingerprint string) (domain.Artifact, error) {
	row := r.pool.QueryRow(ctx, `
SELECT
  proof_id::text, tenant_id::text, storage_provider, object_key, content_hash, mime_type,
  size_bytes, duration_ms, upload_state, scope_type, scope_id::text, subject_type,
  subject_id::text, proof_type, uploaded_by::text, metadata, created_at, uploaded_at,
  retention_policy, retention_expires_at, upload_expires_at, updated_at, row_version, request_fingerprint
FROM proof_artifacts
WHERE tenant_id = $1::uuid
  AND idempotency_key = $2
LIMIT 1`, tenantID, idempotencyKey)
	artifact, existingFingerprint, err := scanArtifactWithFingerprint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// Lost a genuine race with a concurrent inserter that has not committed yet — from the
		// caller's perspective this looks like "not found"; the mobile outbox retries and will
		// see the now-committed row on the next attempt.
		return domain.Artifact{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Artifact{}, err
	}
	if existingFingerprint != fingerprint {
		return domain.Artifact{}, ports.ErrIdempotencyConflict
	}
	return artifact, nil
}

// createFingerprint captures the fields that define "the same logical create request" — enough
// to detect a same-key/different-payload replay without over-fingerprinting free-form metadata.
func createFingerprint(in domain.CreateUpload) string {
	h := sha256.New()
	h.Write([]byte(strings.Join([]string{
		in.ScopeType, in.ScopeID, in.SubjectType, ptrValue(in.SubjectID), in.ProofType, in.MimeType,
	}, "\x1f")))
	return hex.EncodeToString(h.Sum(nil))
}

func (r *Repository) GetProof(ctx context.Context, tenantID, proofID string) (domain.Artifact, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	row := r.pool.QueryRow(ctx, artifactSelectSQL(`
WHERE tenant_id = $1::uuid
  AND proof_id = $2::uuid
LIMIT 1`), tenantID, proofID)
	artifact, err := scanArtifact(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Artifact{}, ports.ErrNotFound
	}
	return artifact, err
}

func (r *Repository) GetProofsByIDs(ctx context.Context, tenantID string, proofIDs []string) (map[string]domain.Artifact, error) {
	out := make(map[string]domain.Artifact, len(proofIDs))
	if len(proofIDs) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	ids, err := pgconv.UUIDs(proofIDs)
	if err != nil {
		return nil, fmt.Errorf("proof: proof ids: %w", err)
	}
	rows, err := r.pool.Query(ctx, artifactSelectSQL(`
WHERE tenant_id = $1::uuid
  AND proof_id = ANY($2::uuid[])`), tenantID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out[artifact.ProofID] = artifact
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) CompleteProof(ctx context.Context, in domain.CompleteUpload) (domain.Artifact, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	metadata, err := json.Marshal(nonNilMap(in.Metadata))
	if err != nil {
		return domain.Artifact{}, err
	}
	row := r.pool.QueryRow(ctx, `
	UPDATE proof_artifacts
	SET content_hash = COALESCE(NULLIF($3, ''), content_hash),
	    mime_type = COALESCE(NULLIF($4, ''), mime_type),
	    size_bytes = CASE WHEN $5::bigint > 0 THEN $5::bigint ELSE size_bytes END,
	    duration_ms = COALESCE($6::bigint, duration_ms),
	    metadata = metadata || $7::jsonb,
	    upload_state = 'completed',
	    uploaded_at = COALESCE(uploaded_at, now()),
	    upload_expires_at = NULL,
	    updated_at = now(),
	    row_version = row_version + 1
	WHERE tenant_id = $1::uuid
	  AND proof_id = $2::uuid
	  AND upload_state <> 'completed'
	RETURNING
	  proof_id::text, tenant_id::text, storage_provider, object_key, content_hash, mime_type,
	  size_bytes, duration_ms, upload_state, scope_type, scope_id::text, subject_type,
	  subject_id::text, proof_type, uploaded_by::text, metadata, created_at, uploaded_at,
	  retention_policy, retention_expires_at, upload_expires_at, updated_at, row_version`,
		in.TenantID,
		in.ProofID,
		in.ContentHash,
		in.MimeType,
		in.SizeBytes,
		in.DurationMS,
		metadata,
	)
	artifact, err := scanArtifact(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.getCompletedProof(ctx, in.TenantID, in.ProofID)
	}
	return artifact, err
}

func (r *Repository) getCompletedProof(ctx context.Context, tenantID, proofID string) (domain.Artifact, error) {
	row := r.pool.QueryRow(ctx, artifactSelectSQL(`
WHERE tenant_id = $1::uuid
  AND proof_id = $2::uuid
  AND upload_state = 'completed'
LIMIT 1`), tenantID, proofID)
	artifact, err := scanArtifact(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Artifact{}, ports.ErrNotFound
	}
	return artifact, err
}

func artifactSelectSQL(where string) string {
	return `
SELECT
  proof_id::text, tenant_id::text, storage_provider, object_key, content_hash, mime_type,
  size_bytes, duration_ms, upload_state, scope_type, scope_id::text, subject_type,
  subject_id::text, proof_type, uploaded_by::text, metadata, created_at, uploaded_at,
  retention_policy, retention_expires_at, upload_expires_at, updated_at, row_version
FROM proof_artifacts
` + where
}

func (r *Repository) ApplyRetention(ctx context.Context, tenantID string, proofIDs []string, policy string, expiresAt *time.Time) (int, error) {
	if len(proofIDs) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	ids, err := pgconv.UUIDs(proofIDs)
	if err != nil {
		return 0, fmt.Errorf("proof: proof ids: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
UPDATE proof_artifacts
SET retention_policy = $3,
    retention_expires_at = $4,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND proof_id = ANY($2::uuid[])
  AND upload_state = 'completed'
  AND retention_policy <> 'legal_hold'`,
		tenantID, ids, strings.TrimSpace(policy), expiresAt)
	return int(tag.RowsAffected()), err
}

func (r *Repository) PurgeExpired(ctx context.Context, before time.Time, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
WITH doomed AS (
  SELECT tenant_id, proof_id
  FROM proof_artifacts
  WHERE retention_expires_at IS NOT NULL
    AND retention_expires_at <= $1
    AND retention_policy <> 'legal_hold'
  ORDER BY retention_expires_at, proof_id
  LIMIT $2
)
DELETE FROM proof_artifacts p
USING doomed d
WHERE p.tenant_id = d.tenant_id
  AND p.proof_id = d.proof_id`, before.UTC(), limit)
	return int(tag.RowsAffected()), err
}

func (r *Repository) PurgeAbandonedUploads(ctx context.Context, before time.Time, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
WITH doomed AS (
  SELECT tenant_id, proof_id
  FROM proof_artifacts
  WHERE upload_state IN ('pending', 'uploading')
    AND upload_expires_at IS NOT NULL
    AND upload_expires_at <= $1
  ORDER BY upload_expires_at, proof_id
  LIMIT $2
)
DELETE FROM proof_artifacts p
USING doomed d
WHERE p.tenant_id = d.tenant_id
  AND p.proof_id = d.proof_id`, before.UTC(), limit)
	return int(tag.RowsAffected()), err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanArtifact(row rowScanner) (domain.Artifact, error) {
	out, _, err := scanArtifactRow(row, false)
	return out, err
}

// scanArtifactWithFingerprint scans a row selected with an extra trailing request_fingerprint
// column (see replayByIdempotencyKey) — used only for the idempotent-replay comparison, never
// exposed on domain.Artifact.
func scanArtifactWithFingerprint(row rowScanner) (domain.Artifact, string, error) {
	return scanArtifactRow(row, true)
}

func scanArtifactRow(row rowScanner, withFingerprint bool) (domain.Artifact, string, error) {
	var out domain.Artifact
	var duration pgtype.Int8
	var subjectID, uploadedBy pgtype.Text
	var metadata []byte
	var uploadedAt, retentionExpiresAt, uploadExpiresAt pgtype.Timestamptz
	var fingerprint string
	dest := []any{
		&out.ProofID,
		&out.TenantID,
		&out.StorageProvider,
		&out.ObjectKey,
		&out.ContentHash,
		&out.MimeType,
		&out.SizeBytes,
		&duration,
		&out.UploadState,
		&out.ScopeType,
		&out.ScopeID,
		&out.SubjectType,
		&subjectID,
		&out.ProofType,
		&uploadedBy,
		&metadata,
		&out.CreatedAt,
		&uploadedAt,
		&out.RetentionPolicy,
		&retentionExpiresAt,
		&uploadExpiresAt,
		&out.UpdatedAt,
		&out.RowVersion,
	}
	if withFingerprint {
		dest = append(dest, &fingerprint)
	}
	if err := row.Scan(dest...); err != nil {
		return domain.Artifact{}, "", err
	}
	if duration.Valid {
		v := duration.Int64
		out.DurationMS = &v
	}
	out.SubjectID = textPtr(subjectID)
	out.UploadedBy = textPtr(uploadedBy)
	out.UploadedAt = timePtr(uploadedAt)
	out.RetentionExpiresAt = timePtr(retentionExpiresAt)
	out.UploadExpiresAt = timePtr(uploadExpiresAt)
	out.Metadata = decodeMap(metadata)
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, fingerprint, nil
}

func decodeMap(raw []byte) map[string]any {
	var out map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func nonNilMap(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	return v
}

func textPtr(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	value := v.String
	return &value
}

func timePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	value := v.Time.UTC()
	return &value
}

func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
