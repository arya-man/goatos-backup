package app

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

const (
	proofTestTenant = "00000000-0000-4000-8000-000000000001"
	proofTestID     = "10000000-0000-4000-8000-000000000001"
	proofTestID2    = "10000000-0000-4000-8000-000000000002"
	proofTestTask   = "20000000-0000-4000-8000-000000000001"
	proofTestShed   = "30000000-0000-4000-8000-000000000001"
	proofTestActor  = "40000000-0000-4000-8000-000000000001"
)

func TestCompleteUploadUsesStorageFinalization(t *testing.T) {
	proof := baseProof()
	proof.UploadState = "pending"
	repo := &fakeProofRepo{proof: proof}
	storage := &fakeProofStorage{stored: domain.StoredObject{
		ContentHash: "sha256:actual",
		MimeType:    "video/mp4",
		SizeBytes:   123,
	}}
	service := NewService(repo, storage)

	proof, err := service.CompleteUpload(context.Background(), domain.CompleteUpload{
		TenantID:    proofTestTenant,
		ProofID:     proofTestID,
		ContentHash: "sha256:forged",
		MimeType:    "video/mp4",
		SizeBytes:   123,
	})
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	if repo.completed.ContentHash != "sha256:actual" || proof.ContentHash != "sha256:actual" {
		t.Fatalf("completion used client metadata: repo=%#v proof=%#v", repo.completed, proof)
	}
	if !storage.finalized {
		t.Fatal("expected storage finalization before DB completion")
	}
}

func TestCompleteUploadRejectsStorageMismatch(t *testing.T) {
	proof := baseProof()
	proof.UploadState = "pending"
	repo := &fakeProofRepo{proof: proof}
	service := NewService(repo, &fakeProofStorage{finalizeErr: ports.ErrIntegrityMismatch})

	_, err := service.CompleteUpload(context.Background(), domain.CompleteUpload{
		TenantID: proofTestTenant,
		ProofID:  proofTestID,
	})
	if !errors.Is(err, ports.ErrIntegrityMismatch) {
		t.Fatalf("CompleteUpload() error = %v, want integrity mismatch", err)
	}
	if repo.completed.ProofID != "" {
		t.Fatalf("proof was completed despite storage mismatch: %#v", repo.completed)
	}
}

func TestResolveProofRefsRequiresCompletedTaskBoundProof(t *testing.T) {
	repo := &fakeProofRepo{proof: baseProof()}
	service := NewService(repo, &fakeProofStorage{})
	binding := sopdomain.ProofBinding{TaskID: proofTestTask, ScopeType: "shed", ScopeID: proofTestShed}

	refs, err := service.ResolveProofRefs(context.Background(), proofTestTenant, binding, []sopdomain.ProofReference{{ProofID: proofTestID}})
	if err != nil {
		t.Fatalf("ResolveProofRefs() error = %v", err)
	}
	if refs[0].SubjectType != "shed" || refs[0].UploadState != "completed" {
		t.Fatalf("unexpected ref = %#v", refs[0])
	}

	// A shed-level video (shed_level_video) is captured against the task's own shed scope
	// (scope_type='shed', scope_id=<the task's shed>). It is bound to the task when its scope
	// matches the binding's scope — consistent with ShedCompletionReadiness, which accepts a
	// shed-scoped proof for the same task. So this proof resolves rather than being rejected.
	repo.proof.ScopeType = "shed"
	repo.proof.ScopeID = proofTestShed
	repo.proof.Metadata = map[string]any{"task_id": proofTestTask, "sop_task_id": proofTestTask}
	if _, err := service.ResolveProofRefs(context.Background(), proofTestTenant, binding, []sopdomain.ProofReference{{ProofID: proofTestID}}); err != nil {
		t.Fatalf("shed-scoped proof matching the binding scope should resolve, got err = %v", err)
	}

	// A proof scoped to a DIFFERENT shed than the binding is still unbound and rejected.
	repo.proof.ScopeType = "shed"
	repo.proof.ScopeID = proofTestID2 // any shed id that is not binding.ScopeID
	repo.proof.SubjectID = nil        // isolate the scope-binding check from the subject check
	if _, err := service.ResolveProofRefs(context.Background(), proofTestTenant, binding, []sopdomain.ProofReference{{ProofID: proofTestID}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbound proof error = %v, want ErrInvalid", err)
	}

	repo.proof.ScopeType = "task"
	repo.proof.ScopeID = proofTestTask
	repo.proof.UploadState = "pending"
	if _, err := service.ResolveProofRefs(context.Background(), proofTestTenant, binding, []sopdomain.ProofReference{{ProofID: proofTestID}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pending proof error = %v, want ErrInvalid", err)
	}
}

func TestResolveProofRefsFetchesProofsInBulk(t *testing.T) {
	first := baseProof()
	second := baseProof()
	second.ProofID = proofTestID2
	second.ProofType = "photo"
	repo := &fakeProofRepo{proofs: map[string]domain.Artifact{
		first.ProofID:  first,
		second.ProofID: second,
	}}
	service := NewService(repo, &fakeProofStorage{})
	binding := sopdomain.ProofBinding{TaskID: proofTestTask, ScopeType: "shed", ScopeID: proofTestShed}

	refs, err := service.ResolveProofRefs(context.Background(), proofTestTenant, binding, []sopdomain.ProofReference{
		{ProofID: proofTestID},
		{ProofID: proofTestID2},
		{ProofID: proofTestID},
	})
	if err != nil {
		t.Fatalf("ResolveProofRefs() error = %v", err)
	}
	if len(refs) != 2 || refs[0].ProofID != proofTestID || refs[1].ProofID != proofTestID2 {
		t.Fatalf("refs=%#v, want two deduped refs in request order", refs)
	}
	if repo.getProofCalls != 0 || repo.getProofsCalls != 1 {
		t.Fatalf("repo calls get=%d bulk=%d, want one bulk lookup", repo.getProofCalls, repo.getProofsCalls)
	}
}

func TestCreateAndCompleteStripReservedMetadata(t *testing.T) {
	pending := baseProof()
	pending.UploadState = "pending"
	repo := &fakeProofRepo{proof: pending}
	storage := &fakeProofStorage{stored: domain.StoredObject{ContentHash: "sha256:actual", MimeType: "video/mp4", SizeBytes: 1}}
	service := NewService(repo, storage)

	_, err := service.CreateUpload(context.Background(), domain.CreateUpload{
		TenantID:    proofTestTenant,
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     proofTestTask,
		SubjectType: "shed",
		SubjectID:   stringPtr(proofTestShed),
		UploadedBy:  stringPtr(proofTestActor),
		Metadata: map[string]any{
			"task_id":           proofTestTask,
			"scope_type":        "shed",
			"note":              "operator clip",
			"capture_source":    "in_app_camera",
			"captured_start_ms": float64(1000),
			"captured_end_ms":   float64(2000),
		},
	})
	if err != nil {
		t.Fatalf("CreateUpload() error = %v", err)
	}
	if _, ok := repo.created.Metadata["task_id"]; ok {
		t.Fatalf("reserved task_id metadata persisted: %#v", repo.created.Metadata)
	}
	if repo.created.Metadata["note"] != "operator clip" {
		t.Fatalf("non-reserved metadata lost: %#v", repo.created.Metadata)
	}

	_, err = service.CompleteUpload(context.Background(), domain.CompleteUpload{
		TenantID: proofTestTenant,
		ProofID:  proofTestID,
		Metadata: map[string]any{
			"sop_task_id": proofTestTask,
			"review_note": "clear",
		},
	})
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	if _, ok := repo.completed.Metadata["sop_task_id"]; ok {
		t.Fatalf("reserved sop_task_id metadata persisted: %#v", repo.completed.Metadata)
	}
	if repo.completed.Metadata["review_note"] != "clear" {
		t.Fatalf("non-reserved completion metadata lost: %#v", repo.completed.Metadata)
	}
}

func TestCreateUploadRejectsVideoWithoutCameraAttestation(t *testing.T) {
	repo := &fakeProofRepo{proof: baseProof()}
	service := NewService(repo, &fakeProofStorage{})

	_, err := service.CreateUpload(context.Background(), domain.CreateUpload{
		TenantID:    proofTestTenant,
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     proofTestTask,
		SubjectType: "goat",
		SubjectID:   stringPtr(proofTestShed),
		UploadedBy:  stringPtr(proofTestActor),
		Metadata:    map[string]any{},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("CreateUpload() error = %v, want ErrInvalid", err)
	}
	if repo.created.ProofType != "" {
		t.Fatalf("invalid upload reached repository: %#v", repo.created)
	}
}

func TestCreateUploadAllowsGalleryForShedSubject(t *testing.T) {
	repo := &fakeProofRepo{proof: baseProof()}
	service := NewService(repo, &fakeProofStorage{})

	_, err := service.CreateUpload(context.Background(), domain.CreateUpload{
		TenantID:    proofTestTenant,
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     proofTestTask,
		SubjectType: "shed",
		SubjectID:   stringPtr(proofTestShed),
		UploadedBy:  stringPtr(proofTestActor),
		Metadata: map[string]any{
			"capture_source":    "gallery_picker",
			"captured_start_ms": float64(1000),
			"captured_end_ms":   float64(2000),
		},
	})
	if err != nil {
		t.Fatalf("CreateUpload() error = %v", err)
	}
	if repo.created.SubjectType != "shed" || repo.created.Metadata["capture_source"] != "gallery_picker" {
		t.Fatalf("gallery shed upload not persisted correctly: %#v", repo.created)
	}
}

func TestCreateUploadRejectsGalleryForGoatSubject(t *testing.T) {
	repo := &fakeProofRepo{proof: baseProof()}
	service := NewService(repo, &fakeProofStorage{})

	_, err := service.CreateUpload(context.Background(), domain.CreateUpload{
		TenantID:    proofTestTenant,
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     proofTestTask,
		SubjectType: "goat",
		SubjectID:   stringPtr(proofTestShed),
		UploadedBy:  stringPtr(proofTestActor),
		Metadata: map[string]any{
			"capture_source":    "gallery_picker",
			"captured_start_ms": float64(1000),
			"captured_end_ms":   float64(2000),
		},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("CreateUpload() error = %v, want ErrInvalid", err)
	}
	if repo.created.ProofType != "" {
		t.Fatalf("invalid upload reached repository: %#v", repo.created)
	}
}

func baseProof() domain.Artifact {
	now := time.Now().UTC()
	return domain.Artifact{
		ProofID:         proofTestID,
		TenantID:        proofTestTenant,
		StorageProvider: "local",
		ObjectKey:       proofTestTenant + "/proof",
		MimeType:        "video/mp4",
		UploadState:     "completed",
		ScopeType:       "task",
		ScopeID:         proofTestTask,
		SubjectType:     "shed",
		SubjectID:       stringPtr(proofTestShed),
		ProofType:       "video",
		Metadata:        map[string]any{},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

type fakeProofRepo struct {
	proof          domain.Artifact
	proofs         map[string]domain.Artifact
	created        domain.CreateUpload
	completed      domain.CompleteUpload
	getProofCalls  int
	getProofsCalls int
}

func (r *fakeProofRepo) CreateProof(_ context.Context, in domain.CreateUpload, _ string) (domain.Artifact, error) {
	r.created = in
	return r.proof, nil
}

func (r *fakeProofRepo) GetProof(_ context.Context, _ string, proofID string) (domain.Artifact, error) {
	r.getProofCalls++
	if r.proofs != nil {
		proof, ok := r.proofs[proofID]
		if !ok {
			return domain.Artifact{}, ports.ErrNotFound
		}
		return proof, nil
	}
	if r.proof.ProofID == "" {
		return domain.Artifact{}, ports.ErrNotFound
	}
	return r.proof, nil
}

func (r *fakeProofRepo) GetProofsByIDs(_ context.Context, _ string, proofIDs []string) (map[string]domain.Artifact, error) {
	r.getProofsCalls++
	out := make(map[string]domain.Artifact, len(proofIDs))
	for _, proofID := range proofIDs {
		if r.proofs != nil {
			if proof, ok := r.proofs[proofID]; ok {
				out[proofID] = proof
			}
			continue
		}
		if r.proof.ProofID == proofID {
			out[proofID] = r.proof
		}
	}
	return out, nil
}

func (r *fakeProofRepo) CompleteProof(_ context.Context, in domain.CompleteUpload) (domain.Artifact, error) {
	r.completed = in
	out := r.proof
	out.ContentHash = in.ContentHash
	out.MimeType = in.MimeType
	out.SizeBytes = in.SizeBytes
	out.UploadState = "completed"
	return out, nil
}

type fakeProofStorage struct {
	stored      domain.StoredObject
	finalizeErr error
	finalized   bool
}

func (s *fakeProofStorage) Provider() string { return "local" }

func (s *fakeProofStorage) PrepareUpload(context.Context, domain.Artifact, time.Duration) (domain.UploadTarget, error) {
	return domain.UploadTarget{}, nil
}

func (s *fakeProofStorage) PrepareDownload(context.Context, domain.Artifact, time.Duration) (string, error) {
	return "", nil
}

func (s *fakeProofStorage) FinalizeUpload(context.Context, domain.Artifact, domain.CompleteUpload) (domain.StoredObject, error) {
	s.finalized = true
	return s.stored, s.finalizeErr
}

func (s *fakeProofStorage) Store(context.Context, domain.Artifact, io.Reader, string) (domain.StoredObject, error) {
	return s.stored, nil
}

func stringPtr(v string) *string { return &v }
