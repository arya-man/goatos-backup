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
		MimeType: "video/mp4", SubjectType: "other", SubjectID: &park,
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

// RED-FIRST: TestMilkPreparationProofAcceptsOtherSubjectType fails with current code (requires SubjectType="park"),
// but client now sends subject_type='other' for prep proofs (since prep has no backend task uuid).
// Fix: ValidateMilkPreparationProofs must accept SubjectType="other" with SubjectID=parkID.
func TestMilkPreparationProofAcceptsOtherSubjectType(t *testing.T) {
	park := "park-1"
	artifact := proofdomain.Artifact{
		ProofID: "proof-1", TenantID: "tenant-1", UploadState: "completed", ProofType: "video",
		MimeType: "video/mp4", SubjectType: "other", SubjectID: &park,
		Metadata: map[string]any{"capture_source": "in_app_camera", "milk_preparation_step": countsdomain.MilkPreparationStepUHTMilkQuantity},
	}
	validator := NewValidator(&proofRepoStub{artifacts: map[string]proofdomain.Artifact{"proof-1": artifact}})
	err := validator.ValidateMilkPreparationProofs(context.Background(), "tenant-1", park, []countsdomain.MilkPreparationStepProof{{StepCode: countsdomain.MilkPreparationStepUHTMilkQuantity, ProofRef: "proof-1"}})
	if err != nil {
		t.Fatalf("prep proof with subject_type='other' should be valid, got err=%v", err)
	}
}

// RED-FIRST: TestMilkFeedingProofAcceptsTaskSubjectType fails with current code (requires SubjectType="park"),
// but client now sends subject_type='task' with subject_id=taskID for feeding proofs.
// Fix: ValidateMilkFeedingProofs must accept SubjectType="task" with SubjectID=taskID.
func TestMilkFeedingProofAcceptsTaskSubjectType(t *testing.T) {
	taskID := "task-123"
	artifact := proofdomain.Artifact{
		ProofID: "proof-2", TenantID: "tenant-1", UploadState: "completed", ProofType: "video",
		MimeType: "video/mp4", SubjectType: "task", SubjectID: &taskID,
		Metadata: map[string]any{"capture_source": "in_app_camera", "milk_feeding_step": countsdomain.MilkFeedingStepCleanBottles},
	}
	validator := NewValidator(&proofRepoStub{artifacts: map[string]proofdomain.Artifact{"proof-2": artifact}})
	// Now the validator signature passes taskID (not parkID)
	err := validator.ValidateMilkFeedingProofs(context.Background(), "tenant-1", taskID, []countsdomain.MilkPreparationStepProof{{StepCode: countsdomain.MilkFeedingStepCleanBottles, ProofRef: "proof-2"}})
	if err != nil {
		t.Fatalf("feeding proof with subject_type='task' should be valid, got err=%v", err)
	}
}

// TestMilkFeedingProofRejectsWrongTaskID: adversarial negative — must reject if task ID doesn't match.
func TestMilkFeedingProofRejectsWrongTaskID(t *testing.T) {
	artifactTaskID := "task-123"
	wrongTaskID := "task-999"
	artifact := proofdomain.Artifact{
		ProofID: "proof-2", TenantID: "tenant-1", UploadState: "completed", ProofType: "video",
		MimeType: "video/mp4", SubjectType: "task", SubjectID: &artifactTaskID,
		Metadata: map[string]any{"capture_source": "in_app_camera", "milk_feeding_step": countsdomain.MilkFeedingStepCleanBottles},
	}
	validator := NewValidator(&proofRepoStub{artifacts: map[string]proofdomain.Artifact{"proof-2": artifact}})
	// Call with different task ID — should fail
	err := validator.ValidateMilkFeedingProofs(context.Background(), "tenant-1", wrongTaskID, []countsdomain.MilkPreparationStepProof{{StepCode: countsdomain.MilkFeedingStepCleanBottles, ProofRef: "proof-2"}})
	if err == nil {
		t.Fatalf("feeding proof with mismatched task ID should fail, but passed")
	}
}

// TestMilkPreparationProofRejectsOldParkSubjectType: adversarial negative — the old "park" shape is now invalid.
// This documents the breaking change from subject_type='park' to subject_type='other'.
func TestMilkPreparationProofRejectsOldParkSubjectType(t *testing.T) {
	park := "park-1"
	artifact := proofdomain.Artifact{
		ProofID: "proof-1", TenantID: "tenant-1", UploadState: "completed", ProofType: "video",
		MimeType: "video/mp4", SubjectType: "park", SubjectID: &park,
		Metadata: map[string]any{"capture_source": "in_app_camera", "milk_preparation_step": countsdomain.MilkPreparationStepUHTMilkQuantity},
	}
	validator := NewValidator(&proofRepoStub{artifacts: map[string]proofdomain.Artifact{"proof-1": artifact}})
	err := validator.ValidateMilkPreparationProofs(context.Background(), "tenant-1", park, []countsdomain.MilkPreparationStepProof{{StepCode: countsdomain.MilkPreparationStepUHTMilkQuantity, ProofRef: "proof-1"}})
	if err == nil {
		t.Fatalf("old park subject type should now fail (validator requires subject_type='other')")
	}
}
