package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
)

const (
	deleteOwnerTenant = "00000000-0000-4000-8000-000000000001"
	deleteOwnerActor  = "91000000-0000-4000-8000-0000000000a1"
	deleteOtherActor  = "91000000-0000-4000-8000-0000000000b2"
)

// createOwnedProofArtifact mirrors createProofArtifact but pins uploaded_by, which is what the
// re-record delete path scopes on.
func createOwnedProofArtifact(t *testing.T, ctx context.Context, repo *Repository, uploadedBy *string) domain.Artifact {
	t.Helper()
	subjectID := "30000000-0000-4000-8000-000000000001"
	proof, err := repo.CreateProof(ctx, domain.CreateUpload{
		TenantID:    deleteOwnerTenant,
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     "20000000-0000-4000-8000-000000000001",
		SubjectType: "shed",
		SubjectID:   &subjectID,
		UploadedBy:  uploadedBy,
		Metadata:    map[string]any{"created": "true"},
	}, "local")
	if err != nil {
		t.Fatalf("CreateProof() error = %v", err)
	}
	return proof
}

// The re-record flow: the operator who captured a proof re-shoots and the app deletes the
// discarded one. This must keep working.
func TestDeleteUnattachedProofAllowsUploader(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	owner := deleteOwnerActor
	proof := createOwnedProofArtifact(t, ctx, repo, &owner)

	deleted, err := repo.DeleteUnattachedProof(ctx, deleteOwnerTenant, proof.ProofID, owner)
	if err != nil {
		t.Fatalf("DeleteUnattachedProof() by uploader error = %v, want nil", err)
	}
	if deleted.ProofID != proof.ProofID {
		t.Fatalf("deleted proof = %q, want %q", deleted.ProofID, proof.ProofID)
	}
	if _, err := repo.GetProof(ctx, deleteOwnerTenant, proof.ProofID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetProof() after delete error = %v, want ErrNotFound", err)
	}
}

// A different authenticated user in the same tenant must not be able to delete someone else's
// unattached proof, and must not learn whether it exists.
func TestDeleteUnattachedProofRefusesNonUploader(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	owner := deleteOwnerActor
	proof := createOwnedProofArtifact(t, ctx, repo, &owner)

	_, err := repo.DeleteUnattachedProof(ctx, deleteOwnerTenant, proof.ProofID, deleteOtherActor)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("DeleteUnattachedProof() by non-uploader error = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetProof(ctx, deleteOwnerTenant, proof.ProofID); err != nil {
		t.Fatalf("proof should survive a non-uploader delete: GetProof() error = %v", err)
	}
}

// uploaded_by is nullable in the schema. Legacy/seeded rows with no uploader have no owner to
// match, so the owner-scoped delete must fail closed rather than fall open to everyone.
func TestDeleteUnattachedProofRefusesWhenUploaderIsNull(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	proof := createOwnedProofArtifact(t, ctx, repo, nil)

	_, err := repo.DeleteUnattachedProof(ctx, deleteOwnerTenant, proof.ProofID, deleteOwnerActor)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("DeleteUnattachedProof() on null-uploader proof error = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetProof(ctx, deleteOwnerTenant, proof.ProofID); err != nil {
		t.Fatalf("null-uploader proof should survive: GetProof() error = %v", err)
	}
}

// The attached-proof protection is unchanged: the uploader still gets ErrInUse, and a
// non-uploader still gets the not-found shape.
func TestDeleteUnattachedProofKeepsAttachedProtection(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	owner := deleteOwnerActor
	proof := createOwnedProofArtifact(t, ctx, repo, &owner)
	if _, err := repo.CompleteProof(ctx, domain.CompleteUpload{
		TenantID:    deleteOwnerTenant,
		ProofID:     proof.ProofID,
		ContentHash: "sha256:attached-owner-scope",
		MimeType:    "video/mp4",
		SizeBytes:   321,
	}); err != nil {
		t.Fatalf("CompleteProof() error = %v", err)
	}

	sopID := "61000000-0000-4000-8000-0000000001a1"
	versionID := "62000000-0000-4000-8000-0000000001a1"
	taskID := "63000000-0000-4000-8000-0000000001a1"
	scopeID := "64000000-0000-4000-8000-0000000001a1"
	submissionID := "65000000-0000-4000-8000-0000000001a1"
	anchor := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	seed := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.drive.owner_scope', 'Vaccination Drive Owner Scope', 'active')`, []any{sopID, deleteOwnerTenant}},
		{`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'Vaccination Drive v1', 'published', '{}'::jsonb, '{}'::jsonb)`, []any{versionID, deleteOwnerTenant, sopID}},
		{`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Vaccination task', 'accepted', 'shed', $5::uuid)`, []any{taskID, deleteOwnerTenant, sopID, versionID, scopeID}},
		{`INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, proof_refs, state, submitted_at, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'owner-scope-attached-test', '{}'::jsonb,
        jsonb_build_array(jsonb_build_object('proof_id', $6::text, 'proof_type', 'video')), 'accepted', $7, $7)`,
			[]any{submissionID, deleteOwnerTenant, taskID, versionID, owner, proof.ProofID, anchor}},
	}
	for i, stmt := range seed {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed attached submission step %d: %v", i, err)
		}
	}

	if _, err := repo.DeleteUnattachedProof(ctx, deleteOwnerTenant, proof.ProofID, owner); !errors.Is(err, ports.ErrInUse) {
		t.Fatalf("uploader deleting an attached proof error = %v, want ErrInUse", err)
	}
	if _, err := repo.DeleteUnattachedProof(ctx, deleteOwnerTenant, proof.ProofID, deleteOtherActor); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("non-uploader deleting an attached proof error = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetProof(ctx, deleteOwnerTenant, proof.ProofID); err != nil {
		t.Fatalf("attached proof should survive: GetProof() error = %v", err)
	}
}
