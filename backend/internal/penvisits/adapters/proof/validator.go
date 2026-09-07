// Package proof adapts the proof module's repository to penvisits' ProofValidator port.
//
// A pen visit is not a SOP task, so it cannot use proofapp.ResolveProofRefs.
// This validator asserts what a pen visit video needs to be honest evidence: the proof
// exists, belongs to the caller's tenant, is a FINISHED upload, has matching declared proof_type
// and stored mime (mime-aware — the two are written by different upload steps
// and can genuinely disagree), and was captured by the in-app LIVE camera (a gallery pick is
// not evidence of work done now).
package proof

import (
	"context"
	"strings"

	penvisitdomain "github.com/vgoats/goatos/backend/internal/penvisits/domain"
	penvisitports "github.com/vgoats/goatos/backend/internal/penvisits/ports"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

const (
	uploadStateCompleted     = "completed"
	captureSourceInAppCamera = "in_app_camera"
)

// Validator implements penvisitports.ProofValidator over the proof repository.
type Validator struct {
	repo proofports.Repository
}

// NewValidator constructs the validator.
func NewValidator(repo proofports.Repository) *Validator {
	return &Validator{repo: repo}
}

var _ penvisitports.ProofValidator = (*Validator)(nil)

// ValidateLiveCameraVideos fails closed: an unknown, wrong-tenant, unfinished, non-video, or
// non-live-camera proof rejects the write with penvisitdomain.ErrInvalidProof. One round trip for
// the whole set.
func (v *Validator) ValidateLiveCameraVideos(ctx context.Context, tenantID string, proofIDs []string) error {
	return v.validateLiveCameraProofs(ctx, tenantID, proofIDs, "video")
}

func (v *Validator) validateLiveCameraProofs(ctx context.Context, tenantID string, proofIDs []string, requiredKind string) error {
	if len(proofIDs) == 0 {
		return nil
	}
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, proofIDs)
	if err != nil {
		return err
	}
	for _, id := range proofIDs {
		art, ok := found[id]
		if !ok || art.TenantID != tenantID || art.UploadState != uploadStateCompleted {
			return penvisitdomain.ErrInvalidProof
		}
		declared := strings.ToLower(strings.TrimSpace(art.ProofType))
		mime := strings.ToLower(strings.TrimSpace(art.MimeType))
		if requiredKind != "" && declared != requiredKind {
			return penvisitdomain.ErrInvalidProof
		}
		if declared != "video" && declared != "photo" {
			return penvisitdomain.ErrInvalidProof
		}
		if declared == "video" && !strings.HasPrefix(mime, "video/") {
			return penvisitdomain.ErrInvalidProof
		}
		if declared == "photo" && !strings.HasPrefix(mime, "image/") {
			return penvisitdomain.ErrInvalidProof
		}
		if art.Metadata["capture_source"] != captureSourceInAppCamera {
			return penvisitdomain.ErrInvalidProof
		}
	}
	return nil
}
