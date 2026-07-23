// Package proof adapts the proof module's repository to feeddirection's ProofValidator port.
//
// Feed completion is NOT a SOP task, so it cannot use proofapp.ResolveProofRefs (which requires a
// task binding). This validator asserts only the honesty a feed completion needs: each referenced
// proof exists, belongs to the caller's tenant, and is a finished upload. The video bytes and the
// authoritative artifact stay owned by the proof module.
package proof

import (
	"context"

	fdports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

const uploadStateCompleted = "completed"

type Validator struct {
	repo proofports.Repository
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
