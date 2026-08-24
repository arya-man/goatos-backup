package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
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

func TestCompletingReplacementTaskGoatVideoKeepsSupersededProofAuditable(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := "00000000-0000-4000-8000-000000000001"
	taskID := "20000000-0000-4000-8000-000000000001"
	goatID := "30000000-0000-4000-8000-000000000001"
	create := domain.CreateUpload{
		TenantID:    tenantID,
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     taskID,
		SubjectType: "goat",
		SubjectID:   &goatID,
	}
	first, err := repo.CreateProof(ctx, create, "local")
	if err != nil {
		t.Fatalf("first CreateProof() error = %v", err)
	}
	if _, err := repo.CompleteProof(ctx, domain.CompleteUpload{
		TenantID:    tenantID,
		ProofID:     first.ProofID,
		ContentHash: "sha256:first",
		MimeType:    "video/mp4",
		SizeBytes:   123,
	}); err != nil {
		t.Fatalf("first CompleteProof() error = %v", err)
	}
	retentionExpires := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
UPDATE proof_artifacts
SET retention_policy = 'operational_90d',
    retention_expires_at = $3
WHERE tenant_id = $1::uuid
  AND proof_id = $2::uuid`, tenantID, first.ProofID, retentionExpires); err != nil {
		t.Fatalf("seed first retention: %v", err)
	}

	second, err := repo.CreateProof(ctx, create, "local")
	if err != nil {
		t.Fatalf("second CreateProof() error = %v", err)
	}
	if _, err := repo.CompleteProof(ctx, domain.CompleteUpload{
		TenantID:    tenantID,
		ProofID:     second.ProofID,
		ContentHash: "sha256:second",
		MimeType:    "video/mp4",
		SizeBytes:   456,
	}); err != nil {
		t.Fatalf("second CompleteProof() error = %v", err)
	}

	superseded, err := repo.GetProof(ctx, tenantID, first.ProofID)
	if err != nil {
		t.Fatalf("GetProof(first) error = %v", err)
	}
	if superseded.UploadState != "completed" {
		t.Fatalf("superseded upload_state = %q, want completed", superseded.UploadState)
	}
	if superseded.RetentionExpiresAt == nil || !superseded.RetentionExpiresAt.Equal(retentionExpires) {
		t.Fatalf("retention expiry = %v, want preserved %v", superseded.RetentionExpiresAt, retentionExpires)
	}
	if superseded.Metadata["superseded_by_proof_id"] != second.ProofID {
		t.Fatalf("metadata = %#v, want superseded_by_proof_id %q", superseded.Metadata, second.ProofID)
	}
	if superseded.Metadata["superseded_reason"] != "replacement_video" {
		t.Fatalf("metadata = %#v, want replacement reason", superseded.Metadata)
	}

	if _, err := repo.CompleteProof(ctx, domain.CompleteUpload{
		TenantID:    tenantID,
		ProofID:     first.ProofID,
		ContentHash: "sha256:first-replay",
		MimeType:    "video/mp4",
		SizeBytes:   999,
	}); err != nil {
		t.Fatalf("replay first CompleteProof() error = %v", err)
	}
	current, err := repo.GetProof(ctx, tenantID, second.ProofID)
	if err != nil {
		t.Fatalf("GetProof(second) error = %v", err)
	}
	if current.Metadata["superseded_by_proof_id"] != nil {
		t.Fatalf("second metadata = %#v, older replay must not supersede current proof", current.Metadata)
	}
}

// B04: a delayed OLDER upload completing AFTER a newer replacement must never
// supersede the newer one. This models the exact interleave from the bug
// report: operator uploads original video A, then re-shoots and uploads
// replacement B; B's upload finishes first (A is still retrying), and A
// finally completes LAST. The old completion-ordered predicate let A (older)
// supersede B (newer) simply because A completed second. The fix orders by
// `created_at` (authorship/upload-intent time, stamped at CreateProof), so:
//   - completing B (authorship-newest, completes first here) must not
//     mark anything superseded yet (A isn't completed yet);
//   - completing A afterwards must NOT supersede B; A is authorship-older, so
//     the fix marks A ITSELF as superseded by B instead.
//
// TestCompletingReplacementTaskGoatVideoKeepsSupersededProofAuditable above is
// the reverse (normal) order -- newer completes second -- and already asserts
// that direction still works (older gets superseded by the newer one).
func TestCompletingOlderDelayedTaskGoatVideoDoesNotSupersedeNewerReplacement(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := "00000000-0000-4000-8000-000000000001"
	taskID := "20000000-0000-4000-8000-000000000002"
	goatID := "30000000-0000-4000-8000-000000000002"
	create := domain.CreateUpload{
		TenantID:    tenantID,
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     taskID,
		SubjectType: "goat",
		SubjectID:   &goatID,
	}

	// A is authored (uploaded) FIRST -- the original video.
	older, err := repo.CreateProof(ctx, create, "local")
	if err != nil {
		t.Fatalf("older CreateProof() error = %v", err)
	}
	// B is authored SECOND -- the operator's re-shoot/replacement.
	newer, err := repo.CreateProof(ctx, create, "local")
	if err != nil {
		t.Fatalf("newer CreateProof() error = %v", err)
	}
	// Force a deterministic, unambiguous authorship gap between the two
	// upload-intent timestamps (real requests would naturally differ by the
	// re-shoot time, but pin it explicitly so the test cannot flake on clock
	// resolution).
	olderCreatedAt := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	newerCreatedAt := time.Date(2026, 7, 1, 8, 5, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `UPDATE proof_artifacts SET created_at=$2 WHERE tenant_id=$1::uuid AND proof_id=$3::uuid`,
		tenantID, olderCreatedAt, older.ProofID); err != nil {
		t.Fatalf("pin older created_at: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE proof_artifacts SET created_at=$2 WHERE tenant_id=$1::uuid AND proof_id=$3::uuid`,
		tenantID, newerCreatedAt, newer.ProofID); err != nil {
		t.Fatalf("pin newer created_at: %v", err)
	}

	// INTERLEAVE: the newer (B) upload's proof-processing finishes FIRST.
	if _, err := repo.CompleteProof(ctx, domain.CompleteUpload{
		TenantID:    tenantID,
		ProofID:     newer.ProofID,
		ContentHash: "sha256:newer",
		MimeType:    "video/mp4",
		SizeBytes:   456,
	}); err != nil {
		t.Fatalf("complete newer (B) error = %v", err)
	}
	// The delayed older (A) upload finally completes SECOND.
	if _, err := repo.CompleteProof(ctx, domain.CompleteUpload{
		TenantID:    tenantID,
		ProofID:     older.ProofID,
		ContentHash: "sha256:older",
		MimeType:    "video/mp4",
		SizeBytes:   123,
	}); err != nil {
		t.Fatalf("complete older (A), delayed = %v", err)
	}

	newerAfter, err := repo.GetProof(ctx, tenantID, newer.ProofID)
	if err != nil {
		t.Fatalf("GetProof(newer) error = %v", err)
	}
	if newerAfter.Metadata["superseded_by_proof_id"] != nil {
		t.Fatalf("newer (authorship-current) proof was superseded by the older, delayed upload: metadata=%#v", newerAfter.Metadata)
	}

	olderAfter, err := repo.GetProof(ctx, tenantID, older.ProofID)
	if err != nil {
		t.Fatalf("GetProof(older) error = %v", err)
	}
	if olderAfter.Metadata["superseded_by_proof_id"] != newer.ProofID {
		t.Fatalf("older proof superseded_by = %#v, want %q (the authorship-newer replacement)", olderAfter.Metadata["superseded_by_proof_id"], newer.ProofID)
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
VALUES ($1::uuid, $2::uuid, 'proof.retention.policy', 'Vaccination Drive', 'active');
INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy)
VALUES ($3::uuid, $2::uuid, $1::uuid, 1, 'Vaccination Drive v1', 'published', '{}'::jsonb, '{"retention_policy":"operational_90d"}'::jsonb);
INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id)
VALUES ($4::uuid, $2::uuid, $1::uuid, $3::uuid, 'vaccination', 'Vaccination task', 'accepted', 'shed', $5::uuid);
INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, proof_refs, state, submitted_at, accepted_at)
VALUES ($6::uuid, $2::uuid, $4::uuid, $3::uuid, $7::uuid, 'retention-backfill-test', '{}'::jsonb,
        jsonb_build_array(jsonb_build_object('proof_id', $8::text, 'proof_type', 'video')), 'accepted', $9, $9)`,
		pgx.QueryExecModeSimpleProtocol,
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
VALUES ($1::uuid, $2::uuid, 'proof.retention.dedupe', 'Vaccination Drive', 'active');
INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy)
VALUES ($3::uuid, $2::uuid, $1::uuid, 1, 'Operational proof policy', 'retired', '{}'::jsonb, '{"retention_policy":"operational_90d"}'::jsonb),
       ($4::uuid, $2::uuid, $1::uuid, 2, 'Standard proof policy', 'published', '{}'::jsonb, '{"retention_policy":"standard_1y"}'::jsonb);
INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id)
VALUES ($5::uuid, $2::uuid, $1::uuid, $3::uuid, 'vaccination', 'Vaccination task', 'accepted', 'shed', $6::uuid);
INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, proof_refs, state, submitted_at, accepted_at)
VALUES ('65000000-0000-4000-8000-000000000201'::uuid, $2::uuid, $5::uuid, $3::uuid, $7::uuid, 'retention-dedupe-90d', '{}'::jsonb,
        jsonb_build_array(jsonb_build_object('proof_id', $8::text, 'proof_type', 'video')), 'accepted', $9, $9),
       ('65000000-0000-4000-8000-000000000202'::uuid, $2::uuid, $5::uuid, $4::uuid, $7::uuid, 'retention-dedupe-1y', '{}'::jsonb,
        jsonb_build_array(jsonb_build_object('proof_id', $8::text, 'proof_type', 'video')), 'accepted', $10, $10)`,
		pgx.QueryExecModeSimpleProtocol,
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

// F7: two CompleteProof calls for the SAME (tenant, task, goat) scope racing
// concurrently must never both survive as "current". Before the
// pg_advisory_xact_lock fix in supersedeOlderTaskGoatVideos, each
// transaction's newest-wins lookup ran under the pool's default READ
// COMMITTED isolation against its own snapshot: neither could see the
// other's uncommitted completion, so both concluded they were the sole
// completed video and neither superseded the other, leaving two live
// "current" videos for one goat.
//
// This test forces the true overlapping-transaction shape of that race
// deterministically instead of hoping two goroutines happen to interleave:
// it manually holds proof A's slot in the advisory lock (by taking the SAME
// pg_advisory_xact_lock key supersedeOlderTaskGoatVideos takes, in an
// uncommitted transaction) while a concurrent repo.CompleteProof(B) call for
// the same scope runs. If the fix is in place, B's supersede step must block
// on that lock until A's holder commits; if the fix is absent (or a
// regression reintroduces the old unlocked lookup), B proceeds immediately
// and the final state can leave two current videos.
func TestCompleteProofConcurrentSameScopeLeavesExactlyOneCurrentVideo(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := "00000000-0000-4000-8000-000000000001"
	taskID := "20000000-0000-4000-8000-000000000001"
	goatID := "30000000-0000-4000-8000-000000000001"
	create := domain.CreateUpload{
		TenantID:    tenantID,
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     taskID,
		SubjectType: "goat",
		SubjectID:   &goatID,
	}

	first, err := repo.CreateProof(ctx, create, "local")
	if err != nil {
		t.Fatalf("first CreateProof() error = %v", err)
	}
	second, err := repo.CreateProof(ctx, create, "local")
	if err != nil {
		t.Fatalf("second CreateProof() error = %v", err)
	}

	// Manually complete `first` and hold the SAME advisory lock key
	// supersedeOlderTaskGoatVideos takes for this (tenant, task, goat)
	// scope, uncommitted, to deterministically force the overlap.
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	if _, err := holder.Exec(ctx, `
UPDATE proof_artifacts
SET content_hash = 'sha256:first',
    mime_type = 'video/mp4',
    size_bytes = 111,
    upload_state = 'completed',
    uploaded_at = now(),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND proof_id = $2::uuid`, tenantID, first.ProofID); err != nil {
		t.Fatalf("holder: complete first: %v", err)
	}
	lockKey := fmt.Sprintf("proof:task-goat-video:%s:%s:%s", tenantID, taskID, goatID)
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		t.Fatalf("holder: take advisory lock: %v", err)
	}

	type completeResult struct {
		proof domain.Artifact
		err   error
	}
	done := make(chan completeResult, 1)
	go func() {
		artifact, err := repo.CompleteProof(ctx, domain.CompleteUpload{
			TenantID:    tenantID,
			ProofID:     second.ProofID,
			ContentHash: "sha256:second",
			MimeType:    "video/mp4",
			SizeBytes:   222,
		})
		done <- completeResult{proof: artifact, err: err}
	}()

	waitForBlockedAdvisoryLock(t, ctx, pool)
	select {
	case <-done:
		t.Fatal("concurrent CompleteProof(second) finished before the advisory-lock holder committed; the supersede race is not serialised")
	case <-time.After(200 * time.Millisecond):
	}

	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit holder: %v", err)
	}

	var got completeResult
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent CompleteProof(second) did not finish after lock release")
	}
	if got.err != nil {
		t.Fatalf("concurrent CompleteProof(second) error = %v", got.err)
	}

	gotFirst, err := repo.GetProof(ctx, tenantID, first.ProofID)
	if err != nil {
		t.Fatalf("GetProof(first) error = %v", err)
	}
	gotSecond, err := repo.GetProof(ctx, tenantID, second.ProofID)
	if err != nil {
		t.Fatalf("GetProof(second) error = %v", err)
	}

	firstCurrent := gotFirst.Metadata["superseded_by_proof_id"] == nil
	secondCurrent := gotSecond.Metadata["superseded_by_proof_id"] == nil
	if firstCurrent == secondCurrent {
		t.Fatalf("want exactly one current proof, got first current=%v (metadata=%#v) second current=%v (metadata=%#v)",
			firstCurrent, gotFirst.Metadata, secondCurrent, gotSecond.Metadata)
	}
	if !secondCurrent {
		t.Fatalf("second (authorship-newest, created after first) should be the current video, metadata=%#v", gotSecond.Metadata)
	}
}

func waitForBlockedAdvisoryLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		err := pool.QueryRow(ctx, `
SELECT count(*)
FROM pg_stat_activity
WHERE datname = current_database()
  AND wait_event_type = 'Lock'
  AND query ILIKE '%pg_advisory_xact_lock%'`).Scan(&count)
		if err != nil {
			t.Fatalf("inspect blocked advisory lock: %v", err)
		}
		if count > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for concurrent CompleteProof to block on advisory lock")
}
