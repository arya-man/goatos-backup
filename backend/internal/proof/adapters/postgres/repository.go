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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Artifact{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	row := tx.QueryRow(ctx, `
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
	completedNow := true
	if errors.Is(err, pgx.ErrNoRows) {
		completedNow = false
		artifact, err = r.getCompletedProof(ctx, in.TenantID, in.ProofID)
	}
	if err != nil {
		return domain.Artifact{}, err
	}
	if completedNow {
		if err := r.supersedeOlderTaskGoatVideos(ctx, tx, artifact); err != nil {
			return domain.Artifact{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Artifact{}, err
	}
	return artifact, nil
}

// supersedeOlderTaskGoatVideos keeps exactly the AUTHORSHIP-newest completed
// task/goat video marked current for a (tenant, task, goat) scope, regardless
// of which upload happened to finish LAST.
//
// B04: the old predicate was blind to time entirely -- any other completed
// video for the same (tenant, task, goat) was superseded by whichever upload
// completed LAST. Uploads can interleave (operator re-shoots a replacement B
// while a delayed original A is still retrying its upload): if A completes
// AFTER B, the OLDER video A used to win and the newer replacement B was
// marked superseded -- the wrong video stood as evidence.
//
// The fix orders by `created_at`, stamped once at CreateProof (the
// upload-INTENT/authorship instant), never by when CompleteProof happens to
// run -- completion timing is pure network/retry jitter and must never decide
// which proof is current. (created_at, proof_id) is used as a total order so
// two rows can never supersede each other.
//
// This is deliberately symmetric, not just "don't let the older one win":
// whichever proof JUST completed looks up the true authorship-newest
// completed video for the scope.
//   - If the one that just completed IS the newest, it supersedes every
//     older completed video (the original behaviour, now ordered correctly).
//   - If something authorship-newer already completed earlier (the B04
//     interleave case), the one that just completed is ITSELF marked
//     superseded by that newer video, instead of silently sitting as a second
//     unmarked "completed" video. This is the conservative choice for
//     evidence integrity: exactly one completed video per scope is ever left
//     unsuperseded.
//
// No new column is required: `created_at` is already on proof_artifacts and
// already scanned into domain.Artifact. If the maintainer later wants an
// explicit `replaces_proof_id` lineage captured at upload-intent time, that is
// a schema addition owned by the migrations agent, not this repository.
func (r *Repository) supersedeOlderTaskGoatVideos(ctx context.Context, tx pgx.Tx, artifact domain.Artifact) error {
	if artifact.ScopeType != "task" || artifact.SubjectType != "goat" || artifact.ProofType != "video" || artifact.SubjectID == nil {
		return nil
	}

	var newestProofID string
	var newestCreatedAt time.Time
	err := tx.QueryRow(ctx, `
SELECT proof_id::text, created_at
FROM proof_artifacts
WHERE tenant_id = $1::uuid
  AND scope_type = 'task'
  AND scope_id = $2::uuid
  AND subject_type = 'goat'
  AND subject_id = $3::uuid
  AND proof_type = 'video'
  AND upload_state = 'completed'
ORDER BY created_at DESC, proof_id DESC
LIMIT 1`,
		artifact.TenantID,
		artifact.ScopeID,
		*artifact.SubjectID,
	).Scan(&newestProofID, &newestCreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// The artifact that just completed is itself the only completed row
		// (the WHERE above cannot miss it: it just transitioned to
		// 'completed' in this same transaction), so nothing to do.
		return nil
	}
	if err != nil {
		return err
	}

	if newestProofID == artifact.ProofID {
		// artifact IS the authorship-newest completed video: supersede every
		// other completed video for this scope, whatever order they completed in.
		_, err := tx.Exec(ctx, `
UPDATE proof_artifacts
SET metadata = metadata || jsonb_build_object(
      'superseded_by_proof_id', $3::text,
      'superseded_at', now(),
      'superseded_reason', 'replacement_video'
    ),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND scope_type = 'task'
  AND scope_id = $2::uuid
  AND subject_type = 'goat'
  AND subject_id = $4::uuid
  AND proof_type = 'video'
  AND upload_state = 'completed'
  AND proof_id <> $3::uuid`,
			artifact.TenantID,
			artifact.ScopeID,
			artifact.ProofID,
			*artifact.SubjectID,
		)
		return err
	}

	// artifact is NOT the newest: an authorship-newer video already completed
	// earlier (the B04 interleave). Mark the artifact that just completed as
	// the superseded one -- it must never overwrite the genuinely newer video.
	_, err = tx.Exec(ctx, `
UPDATE proof_artifacts
SET metadata = metadata || jsonb_build_object(
      'superseded_by_proof_id', $3::text,
      'superseded_at', now(),
      'superseded_reason', 'replacement_video'
    ),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND proof_id = $2::uuid`,
		artifact.TenantID,
		artifact.ProofID,
		newestProofID,
	)
	return err
}

func (r *Repository) DeleteUnattachedProof(ctx context.Context, tenantID, proofID string) (domain.Artifact, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	row := r.pool.QueryRow(ctx, `
WITH doomed AS (
  SELECT *
  FROM proof_artifacts p
  WHERE p.tenant_id = $1::uuid
    AND p.proof_id = $2::uuid
    AND p.retention_policy <> 'legal_hold'
    AND NOT EXISTS (
      SELECT 1
      FROM sop_submissions s
      CROSS JOIN LATERAL jsonb_array_elements(s.proof_refs) ref(value)
      WHERE s.tenant_id = p.tenant_id
        AND ref.value->>'proof_id' = p.proof_id::text
    )
    AND NOT EXISTS (SELECT 1 FROM arrival_intake_review_goats rg WHERE rg.proof_ref_id = p.proof_id)
    AND NOT EXISTS (SELECT 1 FROM arrival_intake_reviews ar WHERE ar.media_proof_id = p.proof_id)
    AND NOT EXISTS (SELECT 1 FROM procurement_hf_vaccination_evidence pe WHERE pe.proof_ref_id = p.proof_id)
    AND NOT EXISTS (SELECT 1 FROM procurement_source_health_checks ph WHERE ph.proof_ref_id = p.proof_id)
    AND NOT EXISTS (SELECT 1 FROM source_entry_decisions sd WHERE sd.proof_ref_id = p.proof_id)
    AND NOT EXISTS (SELECT 1 FROM transit_handoffs th WHERE th.proof_ref_id = p.proof_id)
  LIMIT 1
),
deleted AS (
  DELETE FROM proof_artifacts p
  USING doomed d
  WHERE p.tenant_id = d.tenant_id
    AND p.proof_id = d.proof_id
  RETURNING
    p.proof_id::text, p.tenant_id::text, p.storage_provider, p.object_key, p.content_hash,
    p.mime_type, p.size_bytes, p.duration_ms, p.upload_state, p.scope_type, p.scope_id::text,
    p.subject_type, p.subject_id::text, p.proof_type, p.uploaded_by::text, p.metadata,
    p.created_at, p.uploaded_at, p.retention_policy, p.retention_expires_at,
    p.upload_expires_at, p.updated_at, p.row_version
)
SELECT * FROM deleted`, tenantID, proofID)
	artifact, err := scanArtifact(row)
	if err == nil {
		return artifact, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Artifact{}, err
	}
	if _, getErr := r.GetProof(ctx, tenantID, proofID); errors.Is(getErr, ports.ErrNotFound) {
		return domain.Artifact{}, ports.ErrNotFound
	} else if getErr != nil {
		return domain.Artifact{}, getErr
	}
	return domain.Artifact{}, ports.ErrInUse
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
  AND retention_policy <> 'legal_hold'
  AND (retention_policy, retention_expires_at) IS DISTINCT FROM ($3::text, $4::timestamptz)`,
		tenantID, ids, strings.TrimSpace(policy), expiresAt)
	return int(tag.RowsAffected()), err
}

func (r *Repository) BackfillSubmissionRetention(ctx context.Context, before time.Time, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
WITH raw_candidates AS (
  SELECT
    s.tenant_id,
    ref.value->>'proof_id' AS proof_id_text,
    btrim(v.proof_policy->>'retention_policy') AS retention_policy,
    COALESCE(s.accepted_at, s.submitted_at) AS anchor_at
  FROM sop_submissions s
  JOIN sop_versions v ON v.tenant_id = s.tenant_id AND v.sop_version_id = s.sop_version_id
  CROSS JOIN LATERAL jsonb_array_elements(s.proof_refs) AS ref(value)
  WHERE s.submitted_at <= $1
    AND s.state IN ('accepted', 'needs_review', 'submitted')
    AND jsonb_typeof(s.proof_refs) = 'array'
    AND btrim(v.proof_policy->>'retention_policy') IN ('operational_90d', 'standard_1y', 'critical_7y', 'legal_hold')
    AND ref.value ? 'proof_id'
    AND ref.value->>'proof_id' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
),
resolved AS (
  SELECT
    raw.tenant_id,
    raw.proof_id_text::uuid AS proof_id,
    raw.retention_policy,
    raw.anchor_at,
    CASE raw.retention_policy
      WHEN 'operational_90d' THEN raw.anchor_at + interval '90 days'
      WHEN 'standard_1y' THEN raw.anchor_at + interval '1 year'
      WHEN 'critical_7y' THEN raw.anchor_at + interval '7 years'
      ELSE NULL::timestamptz
    END AS retention_expires_at,
    CASE raw.retention_policy
      WHEN 'legal_hold' THEN 4
      WHEN 'critical_7y' THEN 3
      WHEN 'standard_1y' THEN 2
      WHEN 'operational_90d' THEN 1
      ELSE 0
    END AS policy_rank
  FROM raw_candidates raw
  JOIN proof_artifacts p ON p.tenant_id = raw.tenant_id AND p.proof_id = raw.proof_id_text::uuid
  WHERE true
    AND p.upload_state = 'completed'
    AND p.retention_policy <> 'legal_hold'
    AND (p.retention_policy, p.retention_expires_at) IS DISTINCT FROM (
      raw.retention_policy,
      CASE raw.retention_policy
        WHEN 'operational_90d' THEN raw.anchor_at + interval '90 days'
        WHEN 'standard_1y' THEN raw.anchor_at + interval '1 year'
        WHEN 'critical_7y' THEN raw.anchor_at + interval '7 years'
        ELSE NULL::timestamptz
      END
    )
),
deduped AS (
  SELECT DISTINCT ON (tenant_id, proof_id)
    tenant_id, proof_id, retention_policy, retention_expires_at
  FROM resolved
  ORDER BY tenant_id, proof_id, policy_rank DESC, retention_expires_at DESC NULLS FIRST, anchor_at DESC
  LIMIT $2
)
UPDATE proof_artifacts p
SET retention_policy = r.retention_policy,
    retention_expires_at = r.retention_expires_at,
    updated_at = now(),
    row_version = row_version + 1
FROM deduped r
WHERE p.tenant_id = r.tenant_id
  AND p.proof_id = r.proof_id
  AND p.upload_state = 'completed'
  AND p.retention_policy <> 'legal_hold'
  AND (p.retention_policy, p.retention_expires_at) IS DISTINCT FROM (r.retention_policy, r.retention_expires_at)`, before.UTC(), limit)
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
    AND NOT EXISTS (SELECT 1 FROM arrival_intake_review_goats rg WHERE rg.proof_ref_id = proof_artifacts.proof_id)
    AND NOT EXISTS (SELECT 1 FROM arrival_intake_reviews ar WHERE ar.media_proof_id = proof_artifacts.proof_id)
    AND NOT EXISTS (SELECT 1 FROM procurement_hf_vaccination_evidence pe WHERE pe.proof_ref_id = proof_artifacts.proof_id)
    AND NOT EXISTS (SELECT 1 FROM procurement_source_health_checks ph WHERE ph.proof_ref_id = proof_artifacts.proof_id)
    AND NOT EXISTS (SELECT 1 FROM source_entry_decisions sd WHERE sd.proof_ref_id = proof_artifacts.proof_id)
    AND NOT EXISTS (SELECT 1 FROM transit_handoffs th WHERE th.proof_ref_id = proof_artifacts.proof_id)
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
    AND NOT EXISTS (SELECT 1 FROM arrival_intake_review_goats rg WHERE rg.proof_ref_id = proof_artifacts.proof_id)
    AND NOT EXISTS (SELECT 1 FROM arrival_intake_reviews ar WHERE ar.media_proof_id = proof_artifacts.proof_id)
    AND NOT EXISTS (SELECT 1 FROM procurement_hf_vaccination_evidence pe WHERE pe.proof_ref_id = proof_artifacts.proof_id)
    AND NOT EXISTS (SELECT 1 FROM procurement_source_health_checks ph WHERE ph.proof_ref_id = proof_artifacts.proof_id)
    AND NOT EXISTS (SELECT 1 FROM source_entry_decisions sd WHERE sd.proof_ref_id = proof_artifacts.proof_id)
    AND NOT EXISTS (SELECT 1 FROM transit_handoffs th WHERE th.proof_ref_id = proof_artifacts.proof_id)
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
