// Package proof adapts the proof module's repository to pccare's ProofValidator port.
//
// A PC Care slot registration is not a SOP task, so it cannot use proofapp.ResolveProofRefs.
// This validator asserts what a per-animal care video needs to be honest evidence: the proof
// exists, belongs to the caller's tenant, is a FINISHED upload, is a VIDEO on both the declared
// proof_type and the stored mime (mime-aware — the two are written by different upload steps
// and can genuinely disagree), and was captured by the in-app LIVE camera (a gallery pick is
// not evidence of work done now).
package proof

import (
	"context"
	"strings"

	pccareports "github.com/vgoats/goatos/backend/internal/pccare/ports"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

const (
	uploadStateCompleted     = "completed"
	captureSourceInAppCamera = "in_app_camera"
)

// Validator implements pccareports.ProofValidator over the proof repository.
type Validator struct {
	repo proofports.Repository
}

// NewValidator constructs the validator.
func NewValidator(repo proofports.Repository) *Validator {
	return &Validator{repo: repo}
}

var _ pccareports.ProofValidator = (*Validator)(nil)

// ValidateLiveCameraVideos fails closed: an unknown, wrong-tenant, unfinished, non-video, or
// non-live-camera proof rejects the write with pccareports.ErrInvalidProof. One round trip for
// the whole set.
func (v *Validator) ValidateLiveCameraVideos(ctx context.Context, tenantID string, proofIDs []string) error {
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
			return pccareports.ErrInvalidProof
		}
		declared := strings.ToLower(strings.TrimSpace(art.ProofType))
		mime := strings.ToLower(strings.TrimSpace(art.MimeType))
		if declared != "video" || !strings.HasPrefix(mime, "video/") {
			return pccareports.ErrInvalidProof
		}
		if art.Metadata["capture_source"] != captureSourceInAppCamera {
			return pccareports.ErrInvalidProof
		}
	}
	return nil
}
