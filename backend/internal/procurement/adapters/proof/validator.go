// Package proof adapts the proof module's repository to procurement's VoiceNoteValidator port.
//
// A vendor's voice note (maintainer decision 2026-09-03) is not a SOP task, so it cannot use
// proofapp.ResolveProofRefs. This validator asserts what the note needs to be trusted: the
// referenced proof exists in the caller's tenant, is a finished upload, is declared AND stored as
// audio, and was recorded by the in-app microphone -- the toxin validator's shape, for the same
// reason it asserts both halves (proof_type and mime are written by different upload steps).
package proof

import (
	"context"
	"strings"

	procurementports "github.com/vgoats/goatos/backend/internal/procurement/ports"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

const (
	uploadStateCompleted         = "completed"
	captureSourceInAppMicrophone = "in_app_microphone"
)

// Validator checks a voice-note ref against the proof store.
type Validator struct {
	repo proofports.Repository
}

// NewValidator wires the validator over the proof repository.
func NewValidator(repo proofports.Repository) *Validator {
	return &Validator{repo: repo}
}

var _ procurementports.VoiceNoteValidator = (*Validator)(nil)

// ValidateVendorVoiceNote fails closed: any gap rejects the write with ErrInvalidVoiceNote.
func (v *Validator) ValidateVendorVoiceNote(ctx context.Context, tenantID, proofRef string) error {
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, []string{proofRef})
	if err != nil {
		return err
	}
	art, ok := found[proofRef]
	if !ok || art.TenantID != tenantID || art.UploadState != uploadStateCompleted ||
		strings.ToLower(strings.TrimSpace(art.ProofType)) != "audio" ||
		!strings.HasPrefix(strings.ToLower(strings.TrimSpace(art.MimeType)), "audio/") ||
		art.Metadata["capture_source"] != captureSourceInAppMicrophone {
		return procurementports.ErrInvalidVoiceNote
	}
	return nil
}
