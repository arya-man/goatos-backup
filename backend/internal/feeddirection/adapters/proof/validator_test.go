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
