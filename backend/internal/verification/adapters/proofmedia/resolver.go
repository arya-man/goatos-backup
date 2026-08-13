// Package proofmedia adapts the EXISTING proof module's signed-URL download port to Verification's
// ports.MediaResolver — verification never proxies or duplicates media bytes, it only resolves
// streamed, signed download URLs at read time (verification-module-design.md §2.5).
package proofmedia

import (
	"context"
	"errors"
	"fmt"
	"strings"

	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	vports "github.com/vgoats/goatos/backend/internal/verification/ports"
)

// Downloader is the minimal slice of proof/app.Service this adapter needs. proofapp.Service already
// satisfies this signature (DownloadURL(ctx, tenantID, proofID) (string, error)).
type Downloader interface {
	DownloadURL(ctx context.Context, tenantID, proofID string) (string, error)
}

type ArtifactDownloader interface {
	DownloadArtifact(ctx context.Context, tenantID, proofID string) (proofdomain.Artifact, string, error)
}

// ObjectAvailabilityChecker is the slice of proof/app.Service that proves a stored object still
// exists. proofapp.Service satisfies it via EnsureObjectAvailable.
type ObjectAvailabilityChecker interface {
	EnsureObjectAvailable(ctx context.Context, tenantID, proofID string) error
}

// ActionPresentationResolver resolves backend-authored task titles and recorded operator answers
// referenced by proof metadata.
type ActionPresentationResolver interface {
	ResolveActionPresentations(ctx context.Context, tenantID string, actionIDs []string) (map[string]string, map[string]string, error)
}

type Resolver struct {
	proof               Downloader
	actionPresentations ActionPresentationResolver
}

func NewResolver(proof Downloader) *Resolver {
	return &Resolver{proof: proof}
}

func (r *Resolver) WithActionPresentationResolver(presentations ActionPresentationResolver) *Resolver {
	r.actionPresentations = presentations
	return r
}

// ResolveMedia resolves every requested proof and reports per-ID failures. A missing or unsignable
// individual proof is reported as an empty entry for that ID with DownloadURL="", allowing items
// whose own refs all resolved to keep their media and evidence_available=true while items with
// unresolvable refs get marked evidence_available=false. The verifier UI and verdict path use this
// per-ID granularity to fail-closed only on the items that actually have missing evidence.
func (r *Resolver) ResolveMedia(ctx context.Context, tenantID string, proofIDs []string) ([]domain.MediaItem, error) {
	if r == nil || r.proof == nil {
		return nil, fmt.Errorf("verification proof resolver is unavailable")
	}
	out := make([]domain.MediaItem, len(proofIDs)) // Fixed-size array preserves ID order
	actionIDByProof := make(map[string]string, len(proofIDs))
	actionIDs := make([]string, 0, len(proofIDs))
	seenActionIDs := make(map[string]struct{}, len(proofIDs))
	idToIndex := make(map[string]int, len(proofIDs))

	for i, id := range proofIDs {
		idToIndex[id] = i
		out[i].ProofID = id // Pre-populate every slot with its ID for fail-closed detection
		if id == "" {
			continue // Empty ID stays as empty MediaItem; callers check DownloadURL=""
		}
		if rich, ok := r.proof.(ArtifactDownloader); ok {
			proof, url, err := rich.DownloadArtifact(ctx, tenantID, id)
			if err != nil {
				continue // Per-ID failure: leave out[i] with DownloadURL="" (zero value)
			}
			verificationLabel, _ := proof.Metadata["verification_label"].(string)
			if actionID, ok := proof.Metadata["action_id"].(string); ok && actionID != "" {
				actionIDByProof[id] = actionID
				if _, seen := seenActionIDs[actionID]; !seen {
					seenActionIDs[actionID] = struct{}{}
					actionIDs = append(actionIDs, actionID)
				}
			}
			out[i] = domain.MediaItem{ProofID: id, DownloadURL: url, MimeType: proof.MimeType, DurationMS: proof.DurationMS, Label: verificationLabel}
			continue
		}
		url, err := r.proof.DownloadURL(ctx, tenantID, id)
		if err != nil {
			continue // Per-ID failure: leave out[i] with DownloadURL="" (zero value)
		}
		out[i] = domain.MediaItem{ProofID: id, DownloadURL: url}
	}
	if r.actionPresentations != nil && len(actionIDs) > 0 {
		labels, answers, err := r.actionPresentations.ResolveActionPresentations(ctx, tenantID, actionIDs)
		if err != nil {
			// Action presentation fetch failure doesn't unset previously resolved media URLs;
			// labels and answers simply stay empty for those items.
		} else {
			for i := range out {
				if out[i].DownloadURL == "" {
					continue // Skip unresolved items
				}
				actionID, ok := actionIDByProof[out[i].ProofID]
				if !ok {
					continue
				}
				label := labels[actionID]
				// Missing label for a resolved action is non-fatal; out[i].Label stays empty
				out[i].Label = label
				out[i].Answer = answers[actionID]
			}
		}
	}
	return out, nil
}

// EnsureEvidenceAvailable stats each of this ONE item's proof objects (verification/ports.
// EvidenceAvailabilityChecker). It is called only from RecordVerdict on an approve — never from the
// queue read, where one stat per proof per row would be an N+1 on a hot operator page. Do not
// "optimise" this back into ResolveMedia: signing a URL proves nothing about the bytes, which is
// exactly how an approve could previously be recorded against evidence that no longer existed.
func (r *Resolver) EnsureEvidenceAvailable(ctx context.Context, tenantID string, proofIDs []string) error {
	if r == nil || r.proof == nil {
		return fmt.Errorf("verification proof resolver is unavailable")
	}
	checker, ok := r.proof.(ObjectAvailabilityChecker)
	if !ok {
		return fmt.Errorf("verification proof storage cannot confirm evidence availability")
	}
	if len(proofIDs) == 0 {
		return vports.ErrEvidenceMissing
	}
	for _, id := range proofIDs {
		if strings.TrimSpace(id) == "" {
			return vports.ErrEvidenceMissing
		}
		switch err := checker.EnsureObjectAvailable(ctx, tenantID, id); {
		case err == nil:
		case errors.Is(err, proofports.ErrObjectMissing), errors.Is(err, proofports.ErrNotFound):
			return fmt.Errorf("%w: proof %s", vports.ErrEvidenceMissing, id)
		default:
			return fmt.Errorf("confirm verification proof %s: %w", id, err)
		}
	}
	return nil
}
