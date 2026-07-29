package proof

import (
	"context"
	"errors"
	"testing"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	countsports "github.com/vgoats/goatos/backend/internal/counts/ports"
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

func TestMilkPreparationProofMustMatchFarmStepAndLiveCamera(t *testing.T) {
	park := "park-1"
	base := proofdomain.Artifact{
		ProofID: "proof-1", TenantID: "tenant-1", UploadState: "completed", ProofType: "video",
		MimeType: "video/mp4", SubjectType: "park", SubjectID: &park,
		Metadata: map[string]any{"capture_source": "in_app_camera", "milk_preparation_step": countsdomain.MilkPreparationStepUHTMilkQuantity},
	}
	tests := []struct {
		name   string
		mutate func(*proofdomain.Artifact)
		want   error
	}{
		{name: "valid"},
		{name: "gallery", mutate: func(a *proofdomain.Artifact) { a.Metadata["capture_source"] = "gallery_picker" }, want: countsports.ErrMilkPreparationInvalidProof},
		{name: "wrong step", mutate: func(a *proofdomain.Artifact) {
			a.Metadata["milk_preparation_step"] = countsdomain.MilkPreparationStepCitricAcidMixing
		}, want: countsports.ErrMilkPreparationInvalidProof},
		{name: "unfinished", mutate: func(a *proofdomain.Artifact) { a.UploadState = "pending" }, want: countsports.ErrMilkPreparationInvalidProof},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			artifact := base
			artifact.Metadata = map[string]any{"capture_source": base.Metadata["capture_source"], "milk_preparation_step": base.Metadata["milk_preparation_step"]}
			if tc.mutate != nil {
				tc.mutate(&artifact)
			}
			validator := NewValidator(&proofRepoStub{artifacts: map[string]proofdomain.Artifact{"proof-1": artifact}})
			err := validator.ValidateMilkPreparationProofs(context.Background(), "tenant-1", park, []countsdomain.MilkPreparationStepProof{{StepCode: countsdomain.MilkPreparationStepUHTMilkQuantity, ProofRef: "proof-1"}})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}
