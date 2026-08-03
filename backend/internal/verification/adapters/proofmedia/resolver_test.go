package proofmedia

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	localstorage "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
	vports "github.com/vgoats/goatos/backend/internal/verification/ports"
)

func TestResolveMediaIncludesProofMetadataForVideoPlayback(t *testing.T) {
	duration := int64(4200)
	resolver := NewResolver(richDownloader{
		proof: proofdomain.Artifact{
			ProofID:    "10000000-0000-4000-8000-000000000001",
			MimeType:   "video/mp4",
			DurationMS: &duration,
			Metadata: map[string]any{
				"action_id": "20000000-0000-4000-8000-000000000001",
			},
		},
		url: "/app/proofs/10000000-0000-4000-8000-000000000001/download/signed?tenant_id=00000000-0000-4000-8000-000000000001&expires=1&sig=ok",
	}).WithActionPresentationResolver(fakeActionPresentationResolver{
		labels:  map[string]string{"20000000-0000-4000-8000-000000000001": "Are any babies still inside?"},
		answers: map[string]string{"20000000-0000-4000-8000-000000000001": "No"},
	})

	media, err := resolver.ResolveMedia(context.Background(), "00000000-0000-4000-8000-000000000001", []string{"10000000-0000-4000-8000-000000000001"})
	if err != nil {
		t.Fatalf("ResolveMedia() error = %v", err)
	}
	if len(media) != 1 {
		t.Fatalf("media len=%d, want 1", len(media))
	}
	if media[0].MimeType != "video/mp4" || media[0].DurationMS == nil || *media[0].DurationMS != duration {
		t.Fatalf("media metadata not populated: %#v", media[0])
	}
	if media[0].Label != "Are any babies still inside?" {
		t.Fatalf("media label=%q, want Are any babies still inside?", media[0].Label)
	}
	if media[0].Answer != "No" {
		t.Fatalf("media answer=%q, want No", media[0].Answer)
	}
}

type fakeActionPresentationResolver struct {
	labels  map[string]string
	answers map[string]string
}

func (f fakeActionPresentationResolver) ResolveActionPresentations(_ context.Context, _ string, actionIDs []string) (map[string]string, map[string]string, error) {
	labels := make(map[string]string, len(actionIDs))
	answers := make(map[string]string, len(actionIDs))
	for _, id := range actionIDs {
		if label, ok := f.labels[id]; ok {
			labels[id] = label
		}
		if answer, ok := f.answers[id]; ok {
			answers[id] = answer
		}
	}
	return labels, answers, nil
}

func TestLocalUploadCompletePreservesDurationForVerificationMedia(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryProofRepo()
	service := proofapp.NewService(repo, localstorage.New(t.TempDir(), "local-proof-secret"))
	subjectID := "30000000-0000-4000-8000-000000000001"
	uploadedBy := "40000000-0000-4000-8000-000000000001"

	target, err := service.CreateUpload(ctx, proofdomain.CreateUpload{
		TenantID:    "00000000-0000-4000-8000-000000000001",
		ProofType:   "video",
		MimeType:    "video/mp4",
		ScopeType:   "task",
		ScopeID:     "20000000-0000-4000-8000-000000000001",
		SubjectType: "shed",
		SubjectID:   &subjectID,
		UploadedBy:  &uploadedBy,
		Metadata: map[string]any{
			"capture_source":    "in_app_camera",
			"captured_start_ms": int64(1000),
			"captured_end_ms":   int64(5200),
		},
	})
	if err != nil {
		t.Fatalf("CreateUpload() error = %v", err)
	}

	if _, err := service.StoreUpload(ctx, target.Proof.TenantID, target.Proof.ProofID, "video/mp4", strings.NewReader("proof-video-bytes")); err != nil {
		t.Fatalf("StoreUpload() error = %v", err)
	}
	storedAfterPut, err := repo.GetProof(ctx, target.Proof.TenantID, target.Proof.ProofID)
	if err != nil {
		t.Fatalf("GetProof() after PUT error = %v", err)
	}
	if storedAfterPut.UploadState == "completed" {
		t.Fatalf("local PUT completed proof before mobile metadata arrived: %#v", storedAfterPut)
	}

	duration := int64(4200)
	completed, err := service.CompleteUpload(ctx, proofdomain.CompleteUpload{
		TenantID:   target.Proof.TenantID,
		ProofID:    target.Proof.ProofID,
		MimeType:   "video/mp4",
		DurationMS: &duration,
	})
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	if completed.DurationMS == nil || *completed.DurationMS != duration {
		t.Fatalf("completed proof DurationMS=%v, want %d", completed.DurationMS, duration)
	}

	resolver := NewResolver(service)
	media, err := resolver.ResolveMedia(ctx, target.Proof.TenantID, []string{target.Proof.ProofID})
	if err != nil {
		t.Fatalf("ResolveMedia() error = %v", err)
	}
	if len(media) != 1 {
		t.Fatalf("media len=%d, want 1", len(media))
	}
	if media[0].DurationMS == nil || *media[0].DurationMS != duration {
		t.Fatalf("resolved media DurationMS=%v, want %d", media[0].DurationMS, duration)
	}
}

type richDownloader struct {
	proof proofdomain.Artifact
	url   string
}

func (d richDownloader) DownloadURL(context.Context, string, string) (string, error) {
	return d.url, nil
}

func (d richDownloader) DownloadArtifact(context.Context, string, string) (proofdomain.Artifact, string, error) {
	return d.proof, d.url, nil
}

type memoryProofRepo struct {
	mu     sync.Mutex
	proofs map[string]proofdomain.Artifact
	next   int
}

func newMemoryProofRepo() *memoryProofRepo {
	return &memoryProofRepo{proofs: make(map[string]proofdomain.Artifact)}
}

func (r *memoryProofRepo) CreateProof(_ context.Context, in proofdomain.CreateUpload, provider string) (proofdomain.Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	proofID := fmt.Sprintf("10000000-0000-4000-8000-%012d", r.next)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	proof := proofdomain.Artifact{
		ProofID:         proofID,
		TenantID:        in.TenantID,
		StorageProvider: provider,
		ObjectKey:       in.TenantID + "/" + proofID + ".mp4",
		MimeType:        in.MimeType,
		UploadState:     "pending",
		ScopeType:       in.ScopeType,
		ScopeID:         in.ScopeID,
		SubjectType:     in.SubjectType,
		SubjectID:       in.SubjectID,
		ProofType:       in.ProofType,
		Metadata:        map[string]any{},
		CreatedAt:       now,
		UpdatedAt:       now,
		RowVersion:      1,
	}
	r.proofs[proofID] = proof
	return proof, nil
}

func (r *memoryProofRepo) GetProof(_ context.Context, _ string, proofID string) (proofdomain.Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	proof, ok := r.proofs[proofID]
	if !ok {
		return proofdomain.Artifact{}, ports.ErrNotFound
	}
	return proof, nil
}

func (r *memoryProofRepo) GetProofsByIDs(_ context.Context, _ string, proofIDs []string) (map[string]proofdomain.Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]proofdomain.Artifact, len(proofIDs))
	for _, proofID := range proofIDs {
		if proof, ok := r.proofs[proofID]; ok {
			out[proofID] = proof
		}
	}
	return out, nil
}

func (r *memoryProofRepo) DeleteUnattachedProof(_ context.Context, _ string, proofID string) (proofdomain.Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	proof, ok := r.proofs[proofID]
	if !ok {
		return proofdomain.Artifact{}, ports.ErrNotFound
	}
	delete(r.proofs, proofID)
	return proof, nil
}

func (r *memoryProofRepo) CompleteProof(_ context.Context, in proofdomain.CompleteUpload) (proofdomain.Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	proof, ok := r.proofs[in.ProofID]
	if !ok {
		return proofdomain.Artifact{}, ports.ErrNotFound
	}
	if proof.UploadState == "completed" {
		return proof, nil
	}
	now := time.Date(2026, 7, 17, 10, 1, 0, 0, time.UTC)
	proof.ContentHash = in.ContentHash
	proof.MimeType = in.MimeType
	proof.SizeBytes = in.SizeBytes
	proof.DurationMS = in.DurationMS
	proof.UploadState = "completed"
	proof.UploadedAt = &now
	proof.UpdatedAt = now
	proof.RowVersion++
	if proof.Metadata == nil {
		proof.Metadata = map[string]any{}
	}
	for key, value := range in.Metadata {
		proof.Metadata[key] = value
	}
	r.proofs[in.ProofID] = proof
	return proof, nil
}

func (r *memoryProofRepo) ApplyRetention(_ context.Context, _ string, proofIDs []string, policy string, expiresAt *time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	updated := 0
	for _, proofID := range proofIDs {
		proof, ok := r.proofs[proofID]
		if !ok {
			continue
		}
		proof.RetentionPolicy = policy
		proof.RetentionExpiresAt = expiresAt
		proof.UpdatedAt = time.Date(2026, 7, 17, 10, 2, 0, 0, time.UTC)
		proof.RowVersion++
		r.proofs[proofID] = proof
		updated++
	}
	return updated, nil
}

func (r *memoryProofRepo) BackfillSubmissionRetention(_ context.Context, _ time.Time, _ int) (int, error) {
	return 0, nil
}

func (r *memoryProofRepo) PurgeExpired(_ context.Context, _ time.Time, _ int) (int, error) {
	return 0, nil
}

func (r *memoryProofRepo) PurgeAbandonedUploads(_ context.Context, _ time.Time, _ int) (int, error) {
	return 0, nil
}

var _ ports.Repository = (*memoryProofRepo)(nil)

// This is the whole bug in one test, through the REAL stack (proof service + local storage +
// this resolver): move the stored object aside and the link still resolves — signing a URL says
// nothing about the bytes — but the verdict-time availability check must say the evidence is gone.
func TestEnsureEvidenceAvailableSeesThroughAResolvableLink(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repo := newMemoryProofRepo()
	service := proofapp.NewService(repo, localstorage.New(dir, "local-proof-secret"))
	tenantID := "00000000-0000-4000-8000-000000000001"
	scopeID := "20000000-0000-4000-8000-000000000001"
	subjectID := "30000000-0000-4000-8000-000000000001"
	uploadedBy := "40000000-0000-4000-8000-000000000001"

	target, err := service.CreateUpload(ctx, proofdomain.CreateUpload{
		TenantID: tenantID, ProofType: "video", MimeType: "video/mp4",
		ScopeType: "task", ScopeID: scopeID, SubjectType: "shed", SubjectID: &subjectID, UploadedBy: &uploadedBy,
		Metadata: map[string]any{
			"capture_source":    "in_app_camera",
			"captured_start_ms": int64(1000),
			"captured_end_ms":   int64(5200),
		},
	})
	if err != nil {
		t.Fatalf("CreateUpload: %v", err)
	}
	if _, err := service.StoreUpload(ctx, tenantID, target.Proof.ProofID, "video/mp4", strings.NewReader("proof-video-bytes")); err != nil {
		t.Fatalf("StoreUpload: %v", err)
	}

	resolver := NewResolver(service)
	proofIDs := []string{target.Proof.ProofID}

	if err := resolver.EnsureEvidenceAvailable(ctx, tenantID, proofIDs); err != nil {
		t.Fatalf("EnsureEvidenceAvailable with the object present = %v, want nil", err)
	}

	// The exact production incident: the object goes away, the DB rows do not.
	stored, err := repo.GetProof(ctx, tenantID, target.Proof.ProofID)
	if err != nil {
		t.Fatalf("GetProof: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, stored.ObjectKey)); err != nil {
		t.Fatalf("remove stored object: %v", err)
	}

	// A link is STILL issued — which is precisely why the old link-only gate was a tautology.
	media, err := resolver.ResolveMedia(ctx, tenantID, proofIDs)
	if err != nil || len(media) != 1 || media[0].DownloadURL == "" {
		t.Fatalf("ResolveMedia after the object vanished: media=%#v err=%v — the premise of this test is that a link still resolves", media, err)
	}

	err = resolver.EnsureEvidenceAvailable(ctx, tenantID, proofIDs)
	if !errors.Is(err, vports.ErrEvidenceMissing) {
		t.Fatalf("EnsureEvidenceAvailable with the object gone = %v, want vports.ErrEvidenceMissing", err)
	}
}

var _ vports.EvidenceAvailabilityChecker = (*Resolver)(nil)
