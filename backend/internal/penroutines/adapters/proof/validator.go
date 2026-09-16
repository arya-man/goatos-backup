// Package proof adapts the proof module's repository to penroutines' ProofValidator port.
//
// A routine task is not a SOP task, so it cannot use proofapp.ResolveProofRefs. This
// validator asserts what a routine capture needs to be honest evidence: the proof exists,
// belongs to the caller's tenant, is a FINISHED upload, was captured by the in-app LIVE camera
// (a gallery pick is not evidence of work done now), and -- the routine-specific half -- its
// declared proof_type matches the KIND the submit claims for it and its stored mime matches
// that type. A submit naming a photo as a video is refused, because the routine's min/max
// counts are per kind and a mislabelled capture would satisfy the wrong one.
package proof

import (
	"context"
	"strings"

	proutdomain "github.com/vgoats/goatos/backend/internal/penroutines/domain"
	proutports "github.com/vgoats/goatos/backend/internal/penroutines/ports"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

const (
	uploadStateCompleted     = "completed"
	captureSourceInAppCamera = "in_app_camera"
)

// Validator implements proutports.ProofValidator over the proof repository.
type Validator struct {
	repo proofports.Repository
}

// NewValidator constructs the validator.
func NewValidator(repo proofports.Repository) *Validator {
	return &Validator{repo: repo}
}

var _ proutports.ProofValidator = (*Validator)(nil)

// ValidateLiveCameraProofs fails closed: an unknown, wrong-tenant, unfinished, mislabelled or
// non-live-camera proof rejects the write with proutdomain.ErrInvalidProof. One round trip
// for the whole set.
func (v *Validator) ValidateLiveCameraProofs(ctx context.Context, tenantID string, proofs []proutdomain.ProofItem) error {
	if len(proofs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(proofs))
	for _, p := range proofs {
		ids = append(ids, p.Ref)
	}
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	for _, p := range proofs {
		art, ok := found[p.Ref]
		if !ok || art.TenantID != tenantID || art.UploadState != uploadStateCompleted {
			return proutdomain.ErrInvalidProof
		}
		declared := strings.ToLower(strings.TrimSpace(art.ProofType))
		mime := strings.ToLower(strings.TrimSpace(art.MimeType))
		if declared != p.Kind {
			return proutdomain.ErrInvalidProof
		}
		switch declared {
		case proutdomain.ProofKindVideo:
			if !strings.HasPrefix(mime, "video/") {
				return proutdomain.ErrInvalidProof
			}
		case proutdomain.ProofKindPhoto:
			if !strings.HasPrefix(mime, "image/") {
				return proutdomain.ErrInvalidProof
			}
		default:
			return proutdomain.ErrInvalidProof
		}
		if art.Metadata["capture_source"] != captureSourceInAppCamera {
			return proutdomain.ErrInvalidProof
		}
	}
	return nil
}
