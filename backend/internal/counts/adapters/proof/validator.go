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

var _ countsapp.MilkPreparationProofValidator = (*Validator)(nil)

// ValidateMilkPreparationProofs binds each distinct video to the exact park and preparation step.
// This prevents one upload from being relabelled client-side to satisfy multiple process controls.
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
		stepCode, _ := artifact.Metadata["milk_preparation_step"].(string)
		if !ok || artifact.TenantID != tenantID || artifact.UploadState != "completed" ||
			artifact.ProofType != "video" || !strings.HasPrefix(strings.ToLower(artifact.MimeType), "video/") ||
			artifact.SubjectType != "park" || artifact.SubjectID == nil || *artifact.SubjectID != parkID ||
			artifact.Metadata["capture_source"] != "in_app_camera" || stepCode != step.StepCode {
			return countsports.ErrMilkPreparationInvalidProof
		}
	}
	return nil
}
