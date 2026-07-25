package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
)

func TestCompleteProofDoesNotMutateCompletedArtifact(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	subjectID := "30000000-0000-4000-8000-000000000001"
	proof, err := repo.CreateProof(ctx, domain.CreateUpload{
		TenantID:    "00000000-0000-4000-8000-000000000001",
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     "20000000-0000-4000-8000-000000000001",
		SubjectType: "shed",
		SubjectID:   &subjectID,
		Metadata:    map[string]any{"created": "true"},
	}, "local")
	if err != nil {
		t.Fatalf("CreateProof() error = %v", err)
	}

	firstDuration := int64(1000)
	first, err := repo.CompleteProof(ctx, domain.CompleteUpload{
		TenantID:    proof.TenantID,
		ProofID:     proof.ProofID,
		ContentHash: "sha256:first",
		MimeType:    "video/mp4",
		SizeBytes:   123,
		DurationMS:  &firstDuration,
		Metadata:    map[string]any{"completion_note": "first"},
	})
	if err != nil {
		t.Fatalf("first CompleteProof() error = %v", err)
	}

	secondDuration := int64(2000)
	second, err := repo.CompleteProof(ctx, domain.CompleteUpload{
		TenantID:    proof.TenantID,
		ProofID:     proof.ProofID,
		ContentHash: "sha256:second",
		MimeType:    "video/quicktime",
		SizeBytes:   999,
		DurationMS:  &secondDuration,
		Metadata:    map[string]any{"completion_note": "second", "late": "mutation"},
	})
	if err != nil {
		t.Fatalf("second CompleteProof() error = %v", err)
	}
	if second.ContentHash != first.ContentHash || second.MimeType != first.MimeType || second.SizeBytes != first.SizeBytes {
		t.Fatalf("completed artifact mutated: first=%#v second=%#v", first, second)
	}
	if second.RowVersion != first.RowVersion {
		t.Fatalf("row version changed on idempotent replay: first=%d second=%d", first.RowVersion, second.RowVersion)
	}
	if second.Metadata["completion_note"] != "first" {
		t.Fatalf("completion metadata mutated: %#v", second.Metadata)
	}
	if _, ok := second.Metadata["late"]; ok {
		t.Fatalf("late metadata should not be merged into completed proof: %#v", second.Metadata)
	}
}

func TestCompleteProofConcurrentReplayAfterWaitingUpdate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	proof := createProofArtifact(t, ctx, repo)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `
UPDATE proof_artifacts
SET content_hash = 'sha256:first',
    mime_type = 'video/mp4',
    size_bytes = 123,
    duration_ms = 1000,
    metadata = metadata || '{"completion_note":"first"}'::jsonb,
    upload_state = 'completed',
    uploaded_at = now(),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND proof_id = $2::uuid
  AND upload_state <> 'completed'`, proof.TenantID, proof.ProofID); err != nil {
		t.Fatalf("hold completion update: %v", err)
	}

	type completeResult struct {
		proof domain.Artifact
		err   error
	}
	done := make(chan completeResult, 1)
	go func() {
		duration := int64(2000)
		artifact, err := repo.CompleteProof(ctx, domain.CompleteUpload{
			TenantID:    proof.TenantID,
			ProofID:     proof.ProofID,
			ContentHash: "sha256:second",
			MimeType:    "video/quicktime",
			SizeBytes:   999,
			DurationMS:  &duration,
			Metadata:    map[string]any{"completion_note": "second", "late": "mutation"},
		})
		done <- completeResult{proof: artifact, err: err}
	}()

	waitForBlockedProofUpdate(t, ctx, pool)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit held completion: %v", err)
	}

	var got completeResult
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent CompleteProof did not finish after lock release")
	}
	if got.err != nil {
		t.Fatalf("concurrent CompleteProof() error = %v", got.err)
	}
	if got.proof.ContentHash != "sha256:first" || got.proof.MimeType != "video/mp4" || got.proof.SizeBytes != 123 {
		t.Fatalf("concurrent replay mutated or missed winner: %#v", got.proof)
	}
	if got.proof.RowVersion != 2 {
		t.Fatalf("row version = %d, want winner row version 2", got.proof.RowVersion)
	}
	if got.proof.Metadata["completion_note"] != "first" {
		t.Fatalf("metadata = %#v, want first completion metadata", got.proof.Metadata)
	}
	if _, ok := got.proof.Metadata["late"]; ok {
		t.Fatalf("late metadata should not be merged into completed proof: %#v", got.proof.Metadata)
	}
}

func TestCreateProofIsIdempotentByKey(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	subjectID := "30000000-0000-4000-8000-000000000001"
	create := domain.CreateUpload{
		TenantID:       "00000000-0000-4000-8000-000000000001",
		ProofType:      "video",
		MimeType:       "video/mp4",
		ScopeType:      "task",
		ScopeID:        "20000000-0000-4000-8000-000000000001",
		SubjectType:    "shed",
		SubjectID:      &subjectID,
		Metadata:       map[string]any{"created": "true"},
		IdempotencyKey: "proof-upload:task-1:capture-1",
	}

	first, err := repo.CreateProof(ctx, create, "gcs")
	if err != nil {
		t.Fatalf("first CreateProof() error = %v", err)
	}

	// A retried outbox dispatch reuses the SAME stored idempotency key verbatim (see
	// SyncEngine's dispatch kdoc) — this must return the ORIGINAL proof, never mint a second
	// object/row for the same capture.
	second, err := repo.CreateProof(ctx, create, "gcs")
	if err != nil {
		t.Fatalf("replayed CreateProof() error = %v", err)
	}
	if second.ProofID != first.ProofID || second.ObjectKey != first.ObjectKey {
		t.Fatalf("idempotent replay minted a different proof/object: first=%#v second=%#v", first, second)
	}

	var rowCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM proof_artifacts WHERE idempotency_key = $1`, create.IdempotencyKey).Scan(&rowCount); err != nil {
		t.Fatalf("count proof_artifacts: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("proof_artifacts rows for idempotency key = %d, want 1", rowCount)
	}

	// Same key, different logical request (different mime type) must be rejected rather than
	// silently returning the unrelated original proof.
	conflicting := create
	conflicting.MimeType = "video/quicktime"
	if _, err := repo.CreateProof(ctx, conflicting, "gcs"); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key different-payload CreateProof() error = %v, want ErrIdempotencyConflict", err)
	}

	// No idempotency key at all (a non-mobile/legacy caller) always inserts a fresh row —
	// unchanged pre-existing behavior.
	noKey := create
	noKey.IdempotencyKey = ""
	third, err := repo.CreateProof(ctx, noKey, "gcs")
	if err != nil {
		t.Fatalf("CreateProof() without idempotency key error = %v", err)
	}
	if third.ProofID == first.ProofID {
		t.Fatalf("caller without an idempotency key must always get a fresh proof")
	}
}

func TestBackfillSubmissionRetentionAppliesCommittedSOPPolicy(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := "00000000-0000-4000-8000-000000000001"
	proof := createProofArtifact(t, ctx, repo)
	if _, err := repo.CompleteProof(ctx, domain.CompleteUpload{
		TenantID:    tenantID,
		ProofID:     proof.ProofID,
		ContentHash: "sha256:retention-backfill",
		MimeType:    "video/mp4",
		SizeBytes:   321,
	}); err != nil {
		t.Fatalf("CompleteProof() error = %v", err)
	}

	sopID := "61000000-0000-4000-8000-000000000101"
	versionID := "62000000-0000-4000-8000-000000000101"
	taskID := "63000000-0000-4000-8000-000000000101"
	submissionID := "65000000-0000-4000-8000-000000000101"
	actorID := "90000000-0000-4000-8000-000000000101"
	scopeID := "64000000-0000-4000-8000-000000000101"
	anchor := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.drive', 'Vaccination Drive', 'active');
INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy)
VALUES ($3::uuid, $2::uuid, $1::uuid, 1, 'Vaccination Drive v1', 'published', '{}'::jsonb, '{"retention_policy":"operational_90d"}'::jsonb);
INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id)
VALUES ($4::uuid, $2::uuid, $1::uuid, $3::uuid, 'vaccination', 'Vaccination task', 'accepted', 'shed', $5::uuid);
INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, proof_refs, state, submitted_at, accepted_at)
VALUES ($6::uuid, $2::uuid, $4::uuid, $3::uuid, $7::uuid, 'retention-backfill-test', '{}'::jsonb,
        jsonb_build_array(jsonb_build_object('proof_id', $8::text, 'proof_type', 'video')), 'accepted', $9, $9)`,
		sopID, tenantID, versionID, taskID, scopeID, submissionID, actorID, proof.ProofID, anchor); err != nil {
		t.Fatalf("seed committed SOP submission: %v", err)
	}

	updated, err := repo.BackfillSubmissionRetention(ctx, anchor.Add(time.Hour), 100)
	if err != nil {
		t.Fatalf("BackfillSubmissionRetention() error = %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}
	got, err := repo.GetProof(ctx, tenantID, proof.ProofID)
	if err != nil {
		t.Fatalf("GetProof() error = %v", err)
	}
	if got.RetentionPolicy != "operational_90d" {
		t.Fatalf("retention policy = %q, want operational_90d", got.RetentionPolicy)
	}
	if got.RetentionExpiresAt == nil || !got.RetentionExpiresAt.Equal(anchor.Add(90*24*time.Hour)) {
		t.Fatalf("retention expiry = %v, want %v", got.RetentionExpiresAt, anchor.Add(90*24*time.Hour))
	}
}

func TestBackfillSubmissionRetentionDedupesReusedProofRefsConservatively(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := "00000000-0000-4000-8000-000000000001"
	proof := createProofArtifact(t, ctx, repo)
	if _, err := repo.CompleteProof(ctx, domain.CompleteUpload{
		TenantID:    tenantID,
		ProofID:     proof.ProofID,
		ContentHash: "sha256:retention-dedupe",
		MimeType:    "video/mp4",
		SizeBytes:   654,
	}); err != nil {
		t.Fatalf("CompleteProof() error = %v", err)
	}

	sopID := "61000000-0000-4000-8000-000000000201"
	version90ID := "62000000-0000-4000-8000-000000000201"
	version1YID := "62000000-0000-4000-8000-000000000202"
	taskID := "63000000-0000-4000-8000-000000000201"
	actorID := "90000000-0000-4000-8000-000000000201"
	scopeID := "64000000-0000-4000-8000-000000000201"
	firstAnchor := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	secondAnchor := firstAnchor.Add(24 * time.Hour)
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.drive', 'Vaccination Drive', 'active');
INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy)
VALUES ($3::uuid, $2::uuid, $1::uuid, 1, 'Operational proof policy', 'published', '{}'::jsonb, '{"retention_policy":"operational_90d"}'::jsonb),
       ($4::uuid, $2::uuid, $1::uuid, 2, 'Standard proof policy', 'published', '{}'::jsonb, '{"retention_policy":"standard_1y"}'::jsonb);
INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id)
VALUES ($5::uuid, $2::uuid, $1::uuid, $3::uuid, 'vaccination', 'Vaccination task', 'accepted', 'shed', $6::uuid);
INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, proof_refs, state, submitted_at, accepted_at)
VALUES ('65000000-0000-4000-8000-000000000201'::uuid, $2::uuid, $5::uuid, $3::uuid, $7::uuid, 'retention-dedupe-90d', '{}'::jsonb,
        jsonb_build_array(jsonb_build_object('proof_id', $8::text, 'proof_type', 'video')), 'accepted', $9, $9),
       ('65000000-0000-4000-8000-000000000202'::uuid, $2::uuid, $5::uuid, $4::uuid, $7::uuid, 'retention-dedupe-1y', '{}'::jsonb,
        jsonb_build_array(jsonb_build_object('proof_id', $8::text, 'proof_type', 'video')), 'accepted', $10, $10)`,
		sopID, tenantID, version90ID, version1YID, taskID, scopeID, actorID, proof.ProofID, firstAnchor, secondAnchor); err != nil {
		t.Fatalf("seed duplicate proof submissions: %v", err)
	}

	updated, err := repo.BackfillSubmissionRetention(ctx, secondAnchor.Add(time.Hour), 100)
	if err != nil {
		t.Fatalf("BackfillSubmissionRetention() error = %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want one proof row update", updated)
	}
	got, err := repo.GetProof(ctx, tenantID, proof.ProofID)
	if err != nil {
		t.Fatalf("GetProof() error = %v", err)
	}
	if got.RetentionPolicy != "standard_1y" {
		t.Fatalf("retention policy = %q, want strongest standard_1y policy", got.RetentionPolicy)
	}
	if got.RetentionExpiresAt == nil || !got.RetentionExpiresAt.Equal(secondAnchor.AddDate(1, 0, 0)) {
		t.Fatalf("retention expiry = %v, want %v", got.RetentionExpiresAt, secondAnchor.AddDate(1, 0, 0))
	}
}

func createProofArtifact(t *testing.T, ctx context.Context, repo *Repository) domain.Artifact {
	t.Helper()
	subjectID := "30000000-0000-4000-8000-000000000001"
	proof, err := repo.CreateProof(ctx, domain.CreateUpload{
		TenantID:    "00000000-0000-4000-8000-000000000001",
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     "20000000-0000-4000-8000-000000000001",
		SubjectType: "shed",
		SubjectID:   &subjectID,
		Metadata:    map[string]any{"created": "true"},
	}, "local")
	if err != nil {
		t.Fatalf("CreateProof() error = %v", err)
	}
	return proof
}

func waitForBlockedProofUpdate(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		err := pool.QueryRow(ctx, `
SELECT count(*)
FROM pg_stat_activity
WHERE datname = current_database()
  AND wait_event_type = 'Lock'
  AND query ILIKE '%UPDATE proof_artifacts%'`).Scan(&count)
		if err != nil {
			t.Fatalf("inspect blocked proof update: %v", err)
		}
		if count > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for concurrent proof update to block on row lock")
}
