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

// --- Shifting SOP captures (maintainer decision 2026-09-16) ---------------------------------

var _ countsports.ShiftingProofMedia = (*Validator)(nil)

// ValidateShiftingProofMedia asserts each capture a shifting card names is a real, completed,
// tenant-owned upload OF THE SLOT'S KIND (a photo in a video slot is refused by name), and a
// live-camera capture where the slot demands it. One round trip for the whole set.
func (v *Validator) ValidateShiftingProofMedia(ctx context.Context, tenantID string, expected []countsports.ExpectedShiftingProofMedia) error {
	if len(expected) == 0 {
		return nil
	}
	ids := make([]string, 0, len(expected))
	for _, exp := range expected {
		ids = append(ids, exp.ProofID)
	}
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	for _, exp := range expected {
		art, ok := found[exp.ProofID]
		if !ok || art.TenantID != tenantID || art.UploadState != "completed" ||
			!shiftingKindMatches(art.ProofType, art.MimeType, exp.Kind) ||
			(exp.RequireLiveCamera && art.Metadata["capture_source"] != "in_app_camera") {
			if exp.OnAbsent != nil {
				return exp.OnAbsent
			}
			return countsports.ErrShiftingProofSlotInvalid
		}
	}
	return nil
}

// DescribeShiftingProofMedia reports each proof's stored kind (photo / video) for the ids that
// resolve, so an `either` slot records what it actually received.
func (v *Validator) DescribeShiftingProofMedia(ctx context.Context, tenantID string, proofIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(proofIDs) == 0 {
		return out, nil
	}
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, proofIDs)
	if err != nil {
		return nil, err
	}
	for id, art := range found {
		switch strings.ToLower(strings.TrimSpace(art.ProofType)) {
		case "photo", "video":
			out[id] = strings.ToLower(strings.TrimSpace(art.ProofType))
		}
	}
	return out, nil
}

// shiftingKindMatches: the declared proof_type and the stored mime prefix must BOTH agree with the
// slot's kind; an unknown kind is refused rather than waved through.
func shiftingKindMatches(proofType, mimeType, kind string) bool {
	declared := strings.ToLower(strings.TrimSpace(proofType))
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	photo := declared == "photo" && strings.HasPrefix(mime, "image/")
	video := declared == "video" && strings.HasPrefix(mime, "video/")
	switch kind {
	case "photo":
		return photo
	case "video":
		return video
	case "either":
		return photo || video
	default:
		return false
	}
}
