package proof

import (
	"context"
	"errors"
	"testing"

	fdports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

type proofRepoStub struct {
	proofports.Repository
	artifacts map[string]proofdomain.Artifact
}

func (r *proofRepoStub) GetProofsByIDs(context.Context, string, []string) (map[string]proofdomain.Artifact, error) {
	return r.artifacts, nil
}

func TestValidateLiveCameraVideoRejectsGalleryAndWrongShed(t *testing.T) {
	shedA, shedB := "shed-a", "shed-b"
	base := proofdomain.Artifact{ProofID: "proof-1", TenantID: "tenant-1", UploadState: "completed", ProofType: "video", MimeType: "video/mp4", SubjectType: "shed", SubjectID: &shedA, Metadata: map[string]any{"capture_source": "in_app_camera"}}
	tests := []struct {
		name string
		art  proofdomain.Artifact
		shed string
		want error
	}{
		{name: "camera exact shed", art: base, shed: shedA},
		{name: "gallery", art: func() proofdomain.Artifact {
			x := base
			x.Metadata = map[string]any{"capture_source": "gallery_picker"}
			return x
		}(), shed: shedA, want: fdports.ErrInvalidProof},
		{name: "wrong shed", art: base, shed: shedB, want: fdports.ErrInvalidProof},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			validator := NewValidator(&proofRepoStub{artifacts: map[string]proofdomain.Artifact{"proof-1": tc.art}})
			err := validator.ValidateLiveCameraVideo(context.Background(), "tenant-1", "proof-1", tc.shed)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
}

// ValidateFeedProofMedia is what makes "the weight is a PHOTO and the other two are VIDEOS" a rule
// rather than a comment. It is asserted on BOTH halves of the artifact -- the declared proof_type and
// the stored mime -- because those are written by two different steps of the upload
// (/app/proofs/uploads then /app/proofs/{id}/complete) and can genuinely disagree.
//
// The step-specific error matters as much as the rejection: an operator who filmed the water instead
// of photographing the scale must be told WHICH capture to redo, not that "a proof is invalid".
func TestValidateFeedProofMediaEnforcesKindPerStep(t *testing.T) {
	photo := proofdomain.Artifact{ProofID: "weight", TenantID: "tenant-1", UploadState: "completed", ProofType: "photo", MimeType: "image/jpeg"}
	video := proofdomain.Artifact{ProofID: "water", TenantID: "tenant-1", UploadState: "completed", ProofType: "video", MimeType: "video/mp4"}

	tests := []struct {
		name string
		art  proofdomain.Artifact
		kind fdports.MediaKind
		want error
	}{
		{name: "photo where a photo belongs", art: photo, kind: fdports.MediaKindPhoto},
		{name: "video where a video belongs", art: video, kind: fdports.MediaKindVideo},
		// The 2026-08-11 water rule: a still of a full trough is no longer acceptable evidence.
		{name: "photo where a video belongs", art: photo, kind: fdports.MediaKindVideo, want: fdports.ErrWaterProofRequired},
		{name: "video where a photo belongs", art: video, kind: fdports.MediaKindPhoto, want: fdports.ErrWaterProofRequired},
		// Declared type and actual bytes disagreeing is a rejection, not a coin flip. Checking only one
		// half would let each of these through.
		{name: "declared video, image bytes", art: func() proofdomain.Artifact {
			x := video
			x.MimeType = "image/jpeg"
			return x
		}(), kind: fdports.MediaKindVideo, want: fdports.ErrWaterProofRequired},
		{name: "declared photo, video bytes", art: func() proofdomain.Artifact {
			x := photo
			x.MimeType = "video/mp4"
			return x
		}(), kind: fdports.MediaKindPhoto, want: fdports.ErrWaterProofRequired},
		// Presence/tenancy/completeness still fail as ErrInvalidProof, not as a kind mismatch.
		{name: "not yet uploaded", art: func() proofdomain.Artifact {
			x := photo
			x.UploadState = "pending"
			return x
		}(), kind: fdports.MediaKindPhoto, want: fdports.ErrInvalidProof},
		{name: "another tenant", art: func() proofdomain.Artifact {
			x := photo
			x.TenantID = "tenant-2"
			return x
		}(), kind: fdports.MediaKindPhoto, want: fdports.ErrInvalidProof},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			validator := NewValidator(&proofRepoStub{artifacts: map[string]proofdomain.Artifact{"p": tc.art}})
			err := validator.ValidateFeedProofMedia(context.Background(), "tenant-1", []fdports.ExpectedProofMedia{
				{ProofID: "p", Kind: tc.kind, OnAbsent: fdports.ErrWaterProofRequired},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
}

// An unknown expectation rejects rather than waving the proof through. A default-allow switch would
// mean a future step added with a typo'd kind silently accepts anything.
func TestValidateFeedProofMediaRejectsUnknownKind(t *testing.T) {
	art := proofdomain.Artifact{ProofID: "p", TenantID: "tenant-1", UploadState: "completed", ProofType: "photo", MimeType: "image/jpeg"}
	validator := NewValidator(&proofRepoStub{artifacts: map[string]proofdomain.Artifact{"p": art}})
	err := validator.ValidateFeedProofMedia(context.Background(), "tenant-1", []fdports.ExpectedProofMedia{
		{ProofID: "p", Kind: fdports.MediaKind("attachment")},
	})
	if !errors.Is(err, fdports.ErrProofMediaKind) {
		t.Fatalf("err=%v want ErrProofMediaKind", err)
	}
}

// The weight photo must be a LIVE capture, not a gallery pick.
//
// It is the step that carries a NUMBER, and a still of a scale from the gallery is a reading from
// some other day — the one proof where provenance changes what the verifier is actually judging. The
// two videos are deliberately NOT held to this (they are self-evidently live work), so this test also
// pins that RequireLiveCamera is opt-in rather than global.
func TestValidateFeedProofMediaRequiresLiveCameraOnlyWhereAsked(t *testing.T) {
	live := map[string]any{"capture_source": "in_app_camera"}
	gallery := map[string]any{"capture_source": "gallery_picker"}
	photo := func(meta map[string]any) proofdomain.Artifact {
		return proofdomain.Artifact{
			ProofID: "p", TenantID: "tenant-1", UploadState: "completed",
			ProofType: "photo", MimeType: "image/jpeg", Metadata: meta,
		}
	}

	tests := []struct {
		name    string
		art     proofdomain.Artifact
		require bool
		want    error
	}{
		{name: "live capture when required", art: photo(live), require: true},
		{name: "gallery pick when required", art: photo(gallery), require: true, want: fdports.ErrFeedWeightProofRequired},
		{name: "absent capture_source when required", art: photo(nil), require: true, want: fdports.ErrFeedWeightProofRequired},
		// Not required -> provenance is not consulted at all.
		{name: "gallery pick when not required", art: photo(gallery), require: false},
		{name: "absent capture_source when not required", art: photo(nil), require: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			validator := NewValidator(&proofRepoStub{artifacts: map[string]proofdomain.Artifact{"p": tc.art}})
			err := validator.ValidateFeedProofMedia(context.Background(), "tenant-1", []fdports.ExpectedProofMedia{
				{
					ProofID:           "p",
					Kind:              fdports.MediaKindPhoto,
					RequireLiveCamera: tc.require,
					OnAbsent:          fdports.ErrFeedWeightProofRequired,
				},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
}
