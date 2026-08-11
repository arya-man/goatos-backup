// Package proof adapts the proof module's repository to feeddirection's ProofValidator port.
//
// Feed completion is NOT a SOP task, so it cannot use proofapp.ResolveProofRefs (which requires a
// task binding). This validator asserts only the honesty a feed completion needs: each referenced
// proof exists, belongs to the caller's tenant, and is a finished upload. The video bytes and the
// authoritative artifact stay owned by the proof module.
package proof

import (
	"context"
	"strings"

	fdports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

const (
	uploadStateCompleted = "completed"
	// captureSourceInAppCamera is the metadata value a LIVE capture carries. Anything else (a gallery
	// pick, an absent value) is not evidence of work done now.
	captureSourceInAppCamera = "in_app_camera"
)

type Validator struct {
	repo proofports.Repository
}

// ValidateLiveCameraVideo enforces the stronger transport contract: a completed video created by
// the in-app camera for the exact shed addressed by the task. A gallery artifact or a proof for a
// different shed is not interchangeable evidence.
func (v *Validator) ValidateLiveCameraVideo(ctx context.Context, tenantID, proofID, shedID string) error {
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, []string{proofID})
	if err != nil {
		return err
	}
	art, ok := found[proofID]
	if !ok || art.TenantID != tenantID || art.UploadState != uploadStateCompleted ||
		art.ProofType != "video" || !strings.HasPrefix(strings.ToLower(art.MimeType), "video/") ||
		art.SubjectType != "shed" || art.SubjectID == nil || *art.SubjectID != shedID ||
		art.Metadata["capture_source"] != "in_app_camera" {
		return fdports.ErrInvalidProof
	}
	return nil
}

func NewValidator(repo proofports.Repository) *Validator {
	return &Validator{repo: repo}
}

var _ fdports.ProofValidator = (*Validator)(nil)

// ValidateFeedProofs fails closed: an unknown, wrong-tenant, or not-yet-completed proof id rejects
// the whole completion with fdports.ErrInvalidProof.
func (v *Validator) ValidateFeedProofs(ctx context.Context, tenantID string, proofIDs []string) error {
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
			return fdports.ErrInvalidProof
		}
	}
	return nil
}

// ValidateFeedProofMedia is ValidateFeedProofs plus the MEDIA KIND each step demands.
//
// Both halves are asserted, and both matter: proof_type is what the client DECLARED at upload, while
// mime_type is what the stored bytes actually are. Checking only the declaration would let a client
// label a still image "video" and pass; checking only the mime would pass an artifact whose record
// says something else than its bytes. They are written by different steps of the upload
// (/app/proofs/uploads then /app/proofs/{id}/complete), so they can genuinely disagree.
//
// One round trip for the whole set -- the refs are read together, not per step, so adding a third
// proof did not add a query.
func (v *Validator) ValidateFeedProofMedia(ctx context.Context, tenantID string, expected []fdports.ExpectedProofMedia) error {
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
		if !ok || art.TenantID != tenantID || art.UploadState != uploadStateCompleted {
			return fdports.ErrInvalidProof
		}
		if !matchesKind(art.ProofType, art.MimeType, exp.Kind) ||
			(exp.RequireLiveCamera && art.Metadata["capture_source"] != captureSourceInAppCamera) {
			// The step-specific error, not a generic one: the operator is told WHICH capture to redo.
			if exp.OnAbsent != nil {
				return exp.OnAbsent
			}
			return fdports.ErrProofMediaKind
		}
	}
	return nil
}

// matchesKind reports whether a stored artifact is the requested capture kind. The declared
// proof_type and the actual mime prefix must BOTH agree with the expectation.
func matchesKind(proofType, mimeType string, kind fdports.MediaKind) bool {
	declared := strings.ToLower(strings.TrimSpace(proofType))
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	switch kind {
	case fdports.MediaKindPhoto:
		return declared == "photo" && strings.HasPrefix(mime, "image/")
	case fdports.MediaKindVideo:
		return declared == "video" && strings.HasPrefix(mime, "video/")
	default:
		// An unknown expectation is a programming error, and the safe reading of "I do not know what
		// this step wants" is to reject rather than to wave the proof through.
		return false
	}
}
