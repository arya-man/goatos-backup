// Package proofmedia adapts the EXISTING proof module's signed-URL download port to Verification's
// ports.MediaResolver — verification never proxies or duplicates media bytes, it only resolves
// streamed, signed download URLs at read time (verification-module-design.md §2.5).
package proofmedia

import (
	"context"
	"fmt"

	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// Downloader is the minimal slice of proof/app.Service this adapter needs. proofapp.Service already
// satisfies this signature (DownloadURL(ctx, tenantID, proofID) (string, error)).
type Downloader interface {
	DownloadURL(ctx context.Context, tenantID, proofID string) (string, error)
}

type ArtifactDownloader interface {
	DownloadArtifact(ctx context.Context, tenantID, proofID string) (proofdomain.Artifact, string, error)
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

// ResolveMedia resolves every requested proof. Missing or unsignable evidence fails closed so the
// verifier UI and verdict path cannot mistake a partial media list for complete proof.
func (r *Resolver) ResolveMedia(ctx context.Context, tenantID string, proofIDs []string) ([]domain.MediaItem, error) {
	if r == nil || r.proof == nil {
		return nil, fmt.Errorf("verification proof resolver is unavailable")
	}
	out := make([]domain.MediaItem, 0, len(proofIDs))
	actionIDByProof := make(map[string]string, len(proofIDs))
	actionIDs := make([]string, 0, len(proofIDs))
	seenActionIDs := make(map[string]struct{}, len(proofIDs))
	for _, id := range proofIDs {
		if id == "" {
			return nil, fmt.Errorf("verification proof id is empty")
		}
		if rich, ok := r.proof.(ArtifactDownloader); ok {
			proof, url, err := rich.DownloadArtifact(ctx, tenantID, id)
			if err != nil {
				return nil, fmt.Errorf("resolve verification proof %s: %w", id, err)
			}
			if actionID, ok := proof.Metadata["action_id"].(string); ok && actionID != "" {
				actionIDByProof[id] = actionID
				if _, seen := seenActionIDs[actionID]; !seen {
					seenActionIDs[actionID] = struct{}{}
					actionIDs = append(actionIDs, actionID)
				}
			}
			out = append(out, domain.MediaItem{ProofID: id, DownloadURL: url, MimeType: proof.MimeType, DurationMS: proof.DurationMS})
			continue
		}
		url, err := r.proof.DownloadURL(ctx, tenantID, id)
		if err != nil {
			return nil, fmt.Errorf("resolve verification proof %s: %w", id, err)
		}
		out = append(out, domain.MediaItem{ProofID: id, DownloadURL: url})
	}
	if r.actionPresentations != nil && len(actionIDs) > 0 {
		labels, answers, err := r.actionPresentations.ResolveActionPresentations(ctx, tenantID, actionIDs)
		if err != nil {
			return nil, fmt.Errorf("resolve verification task labels: %w", err)
		}
		for i := range out {
			actionID, ok := actionIDByProof[out[i].ProofID]
			if !ok {
				continue
			}
			label := labels[actionID]
			if label == "" {
				return nil, fmt.Errorf("resolve verification task label for action %s: label missing", actionID)
			}
			out[i].Label = label
			out[i].Answer = answers[actionID]
		}
	}
	return out, nil
}
