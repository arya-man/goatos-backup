package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/proof/domain"
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
