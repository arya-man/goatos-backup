// Package proofmedia adapts the EXISTING proof module's signed-URL download port to Verification's
// ports.MediaResolver — verification never proxies or duplicates media bytes, it only resolves
// streamed, signed download URLs at read time (verification-module-design.md §2.5).
package proofmedia

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// Downloader is the minimal slice of proof/app.Service this adapter needs. proofapp.Service already
// satisfies this signature (DownloadURL(ctx, tenantID, proofID) (string, error)).
type Downloader interface {
	DownloadURL(ctx context.Context, tenantID, proofID string) (string, error)
}

type Resolver struct {
	proof Downloader
}

func NewResolver(proof Downloader) *Resolver {
	return &Resolver{proof: proof}
}

// ResolveMedia resolves each proof id to a signed download URL. A single proof failing to resolve
// (e.g. an artifact deleted after capture) is skipped rather than failing the whole queue page --
// bounded by the caller's page size x media-per-item, never a per-row DB query.
func (r *Resolver) ResolveMedia(ctx context.Context, tenantID string, proofIDs []string) ([]domain.MediaItem, error) {
	if r == nil || r.proof == nil {
		return nil, nil
	}
	out := make([]domain.MediaItem, 0, len(proofIDs))
	for _, id := range proofIDs {
		if id == "" {
			continue
		}
		url, err := r.proof.DownloadURL(ctx, tenantID, id)
		if err != nil {
			continue
		}
		out = append(out, domain.MediaItem{ProofID: id, DownloadURL: url})
	}
	return out, nil
}
