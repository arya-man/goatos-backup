package proof

import (
	"context"
	"fmt"
	"strings"

	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	toxinports "github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// Downloader is the slice of proof/app.Service this adapter needs; proofapp.Service
// already satisfies it. Declared as an interface here so the toxin module keeps no
// compile-time dependency on the proof application service.
type Downloader interface {
	DownloadArtifact(ctx context.Context, tenantID, proofID string) (proofdomain.Artifact, string, error)
}

// MediaResolver adapts the proof module's signed-download path to toxin's MediaResolver.
//
// It resolves the URL through the SAME storage seam the proof HTTP route uses, so a
// GCS-backed deployment yields the absolute signed URL directly instead of the 307 that
// left the review drawer with no playable evidence at all.
type MediaResolver struct {
	proof Downloader
}

// NewMediaResolver wires the resolver over the proof service.
func NewMediaResolver(proof Downloader) *MediaResolver {
	return &MediaResolver{proof: proof}
}

var _ toxinports.MediaResolver = (*MediaResolver)(nil)

// ResolveProofMedia returns the signed URL and stored mime type for one proof reference.
func (r *MediaResolver) ResolveProofMedia(ctx context.Context, tenantID, proofRef string) (string, string, error) {
	if r == nil || r.proof == nil {
		return "", "", fmt.Errorf("toxin media resolver is unavailable")
	}
	ref := strings.TrimSpace(proofRef)
	if ref == "" {
		return "", "", fmt.Errorf("toxin media reference is empty")
	}
	artifact, url, err := r.proof.DownloadArtifact(ctx, tenantID, ref)
	if err != nil {
		return "", "", err
	}
	return url, artifact.MimeType, nil
}
