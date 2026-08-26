// Package proof adapts the proof module's repository to toxin's ProofValidator port.
//
// A toxin submit is not a SOP task, so it cannot use proofapp.ResolveProofRefs. This
// validator asserts the honesty the strip photo needs: the referenced proof exists in
// the caller's tenant, is a finished upload, is declared AND stored as a photo, and was
// captured by the in-app camera at read time — a gallery pick is not evidence of what
// the strip showed inside its one-minute read window.
package proof

import (
	"context"
	"strings"

	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
	toxinports "github.com/vgoats/goatos/backend/internal/toxin/ports"
)

const (
	uploadStateCompleted     = "completed"
	captureSourceInAppCamera = "in_app_camera"
)

// Validator checks a strip-photo ref against the proof store.
type Validator struct {
	repo proofports.Repository
}

// NewValidator wires the validator over the proof repository.
func NewValidator(repo proofports.Repository) *Validator {
	return &Validator{repo: repo}
}

var _ toxinports.ProofValidator = (*Validator)(nil)

// ValidateToxinStripPhoto fails closed: any gap rejects the submit with ErrInvalidProof.
// Both the declared proof_type and the stored mime prefix are asserted — they are
// written by different upload steps and can genuinely disagree.
func (v *Validator) ValidateToxinStripPhoto(ctx context.Context, tenantID, proofRef string) error {
	return v.validate(ctx, tenantID, proofRef, "photo", "image/")
}

// ValidateToxinStepVideo asserts a working step's capture is a completed in-app-camera
// VIDEO: each of steps 1/2/3/5/6 is proved by filming the work as it happens.
func (v *Validator) ValidateToxinStepVideo(ctx context.Context, tenantID, proofRef string) error {
	return v.validate(ctx, tenantID, proofRef, "video", "video/")
}

func (v *Validator) validate(ctx context.Context, tenantID, proofRef, declaredType, mimePrefix string) error {
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, []string{proofRef})
	if err != nil {
		return err
	}
	art, ok := found[proofRef]
	if !ok || art.TenantID != tenantID || art.UploadState != uploadStateCompleted ||
		strings.ToLower(strings.TrimSpace(art.ProofType)) != declaredType ||
		!strings.HasPrefix(strings.ToLower(strings.TrimSpace(art.MimeType)), mimePrefix) ||
		art.Metadata["capture_source"] != captureSourceInAppCamera {
		return toxinports.ErrInvalidProof
	}
	return nil
}
