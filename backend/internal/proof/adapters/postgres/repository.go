// Package postgres implements proof artifact persistence.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
)

const defaultQueryTimeout = 3 * time.Second

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
	row := r.pool.QueryRow(ctx, `
WITH new_proof AS (
  SELECT gen_random_uuid() AS proof_id
)
INSERT INTO proof_artifacts (
  proof_id, tenant_id, storage_provider, object_key, mime_type, scope_type, scope_id,
  subject_type, subject_id, proof_type, uploaded_by, metadata
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
  $10::jsonb
FROM new_proof
RETURNING
  proof_id::text, tenant_id::text, storage_provider, object_key, content_hash, mime_type,
  size_bytes, duration_ms, upload_state, scope_type, scope_id::text, subject_type,
  subject_id::text, proof_type, uploaded_by::text, metadata, created_at, uploaded_at,
  updated_at, row_version`,
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
	)
	return scanArtifact(row)
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
	    updated_at = now(),
	    row_version = row_version + 1
	WHERE tenant_id = $1::uuid
	  AND proof_id = $2::uuid
	  AND upload_state <> 'completed'
	RETURNING
	  proof_id::text, tenant_id::text, storage_provider, object_key, content_hash, mime_type,
	  size_bytes, duration_ms, upload_state, scope_type, scope_id::text, subject_type,
	  subject_id::text, proof_type, uploaded_by::text, metadata, created_at, uploaded_at,
	  updated_at, row_version`,
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
  updated_at, row_version
FROM proof_artifacts
` + where
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanArtifact(row rowScanner) (domain.Artifact, error) {
	var out domain.Artifact
	var duration pgtype.Int8
	var subjectID, uploadedBy pgtype.Text
	var metadata []byte
	var uploadedAt pgtype.Timestamptz
	if err := row.Scan(
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
		&out.UpdatedAt,
		&out.RowVersion,
	); err != nil {
		return domain.Artifact{}, err
	}
	if duration.Valid {
		v := duration.Int64
		out.DurationMS = &v
	}
	out.SubjectID = textPtr(subjectID)
	out.UploadedBy = textPtr(uploadedBy)
	out.UploadedAt = timePtr(uploadedAt)
	out.Metadata = decodeMap(metadata)
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	return out, nil
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
