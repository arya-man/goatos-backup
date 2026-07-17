package proofmedia

import (
	"context"
	"testing"

	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
)

func TestResolveMediaIncludesProofMetadataForVideoPlayback(t *testing.T) {
	duration := int64(4200)
	resolver := NewResolver(richDownloader{
		proof: proofdomain.Artifact{
			ProofID:    "10000000-0000-4000-8000-000000000001",
			MimeType:   "video/mp4",
			DurationMS: &duration,
		},
		url: "/app/proofs/10000000-0000-4000-8000-000000000001/download/signed?tenant_id=00000000-0000-4000-8000-000000000001&expires=1&sig=ok",
	})

	media, err := resolver.ResolveMedia(context.Background(), "00000000-0000-4000-8000-000000000001", []string{"10000000-0000-4000-8000-000000000001"})
	if err != nil {
		t.Fatalf("ResolveMedia() error = %v", err)
	}
	if len(media) != 1 {
		t.Fatalf("media len=%d, want 1", len(media))
	}
	if media[0].MimeType != "video/mp4" || media[0].DurationMS == nil || *media[0].DurationMS != duration {
		t.Fatalf("media metadata not populated: %#v", media[0])
	}
}

type richDownloader struct {
	proof proofdomain.Artifact
	url   string
}

func (d richDownloader) DownloadURL(context.Context, string, string) (string, error) {
	return d.url, nil
}

func (d richDownloader) DownloadArtifact(context.Context, string, string) (proofdomain.Artifact, string, error) {
	return d.proof, d.url, nil
}
