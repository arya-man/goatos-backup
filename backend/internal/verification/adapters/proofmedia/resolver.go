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

type Resolver struct {
	proof Downloader
}

func NewResolver(proof Downloader) *Resolver {
	return &Resolver{proof: proof}
}

// ResolveMedia resolves every requested proof. Missing or unsignable evidence fails closed so the
// verifier UI and verdict path cannot mistake a partial media list for complete proof.
func (r *Resolver) ResolveMedia(ctx context.Context, tenantID string, proofIDs []string) ([]domain.MediaItem, error) {
	if r == nil || r.proof == nil {
		return nil, fmt.Errorf("verification proof resolver is unavailable")
	}
	out := make([]domain.MediaItem, 0, len(proofIDs))
	for _, id := range proofIDs {
		if id == "" {
			return nil, fmt.Errorf("verification proof id is empty")
		}
		if rich, ok := r.proof.(ArtifactDownloader); ok {
			proof, url, err := rich.DownloadArtifact(ctx, tenantID, id)
			if err != nil {
				return nil, fmt.Errorf("resolve verification proof %s: %w", id, err)
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
	return out, nil
}
