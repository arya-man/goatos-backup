// Package proof adapts proof artifacts to Counts milk-preparation evidence validation.
package proof

import (
	"context"
	"strings"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	countsports "github.com/vgoats/goatos/backend/internal/counts/ports"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

type Validator struct {
	repo proofports.Repository
}

func NewValidator(repo proofports.Repository) *Validator { return &Validator{repo: repo} }

// milkStepFromMetadata resolves the step code a proof was captured for. The canonical capture
// pipeline stamps the slot's field_key ("milk_feeding_clean_bottles"); the step code is that key
// with the flow prefix stripped. The legacy explicit key ("milk_feeding_step") is read first for
// compatibility, though no shipped client ever wrote it — submits were impossible before the
// subject-type contradiction fix, so field_key is the only shape that exists in the wild.
func milkStepFromMetadata(metadata map[string]any, legacyKey, fieldPrefix string) string {
	if legacy, _ := metadata[legacyKey].(string); legacy != "" {
		return legacy
	}
	fieldKey, _ := metadata["field_key"].(string)
	if strings.HasPrefix(fieldKey, fieldPrefix) {
		return strings.TrimPrefix(fieldKey, fieldPrefix)
	}
	return ""
}

var _ countsapp.MilkPreparationProofValidator = (*Validator)(nil)
var _ countsapp.MilkFeedingProofValidator = (*Validator)(nil)

// ValidateMilkPreparationProofs binds each distinct video to the exact farm and preparation step.
// This prevents one upload from being relabelled client-side to satisfy multiple process controls.
// Client sends subject_type="other" with subject_id=parkID for prep proofs (since prep has no backend task uuid).
func (v *Validator) ValidateMilkPreparationProofs(ctx context.Context, tenantID, parkID string, steps []countsdomain.MilkPreparationStepProof) error {
	ids := make([]string, 0, len(steps))
	for _, step := range steps {
		ids = append(ids, step.ProofRef)
	}
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	for _, step := range steps {
		artifact, ok := found[step.ProofRef]
		stepCode := milkStepFromMetadata(artifact.Metadata, "milk_preparation_step", "milk_preparation_")
		if !ok || artifact.TenantID != tenantID || artifact.UploadState != "completed" ||
			artifact.ProofType != "video" || !strings.HasPrefix(strings.ToLower(artifact.MimeType), "video/") ||
			artifact.SubjectType != "other" || artifact.SubjectID == nil || *artifact.SubjectID != parkID ||
			artifact.Metadata["capture_source"] != "in_app_camera" || stepCode != step.StepCode {
			return countsports.ErrMilkPreparationInvalidProof
		}
	}
	return nil
}

// ValidateMilkFeedingProofs binds each distinct video to the exact task and feeding step.
// Client sends subject_type="task" with subject_id=taskID for feeding proofs.
func (v *Validator) ValidateMilkFeedingProofs(ctx context.Context, tenantID, taskID string, steps []countsdomain.MilkPreparationStepProof) error {
	ids := make([]string, 0, len(steps))
	for _, step := range steps {
		ids = append(ids, step.ProofRef)
	}
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	for _, step := range steps {
		artifact, ok := found[step.ProofRef]
		stepCode := milkStepFromMetadata(artifact.Metadata, "milk_feeding_step", "milk_feeding_")
		if !ok || artifact.TenantID != tenantID || artifact.UploadState != "completed" ||
			artifact.ProofType != "video" || !strings.HasPrefix(strings.ToLower(artifact.MimeType), "video/") ||
			artifact.SubjectType != "task" || artifact.SubjectID == nil || *artifact.SubjectID != taskID ||
			artifact.Metadata["capture_source"] != "in_app_camera" || stepCode != step.StepCode {
			return countsports.ErrMilkFeedingInvalidProof
		}
	}
	return nil
}
