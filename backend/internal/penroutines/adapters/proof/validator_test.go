package proof

import (
	"context"
	"errors"
	"testing"
	"time"

	proutdomain "github.com/vgoats/goatos/backend/internal/penroutines/domain"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

type fakeProofRepo struct {
	proofports.Repository
	found map[string]proofdomain.Artifact
}

func (f *fakeProofRepo) GetProofsByIDs(_ context.Context, _ string, _ []string) (map[string]proofdomain.Artifact, error) {
	return f.found, nil
}

func art(id, proofType, mime, source string) proofdomain.Artifact {
	return proofdomain.Artifact{ProofID: id, TenantID: "t1", UploadState: "completed", ProofType: proofType, MimeType: mime, Metadata: map[string]any{"capture_source": source}}
}

// TestValidatorRefusesMislabelledAndGalleryCaptures pins the mixed-kind rule: a photo claimed
// as a video (or vice versa) is refused, a gallery pick is refused, an unfinished or foreign
// proof is refused, and a correctly labelled in-app photo plus video passes in one round trip.
func TestValidatorRefusesMislabelledAndGalleryCaptures(t *testing.T) {
	repo := &fakeProofRepo{found: map[string]proofdomain.Artifact{
		"photo":   art("photo", "photo", "image/jpeg", "in_app_camera"),
		"video":   art("video", "video", "video/mp4", "in_app_camera"),
		"gallery": art("gallery", "photo", "image/jpeg", "gallery"),
		"pending": {ProofID: "pending", TenantID: "t1", UploadState: "pending", ProofType: "photo", MimeType: "image/jpeg", Metadata: map[string]any{"capture_source": "in_app_camera"}},
		"foreign": {ProofID: "foreign", TenantID: "t2", UploadState: "completed", ProofType: "photo", MimeType: "image/jpeg", Metadata: map[string]any{"capture_source": "in_app_camera"}},
		"mime":    art("mime", "video", "image/jpeg", "in_app_camera"),
	}}
	v := NewValidator(repo)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := v.ValidateLiveCameraProofs(ctx, "t1", []proutdomain.ProofItem{{Ref: "photo", Kind: "photo"}, {Ref: "video", Kind: "video"}}); err != nil {
		t.Fatalf("matching kinds must pass: %v", err)
	}
	for name, items := range map[string][]proutdomain.ProofItem{
		"photo claimed as video": {{Ref: "photo", Kind: "video"}},
		"video claimed as photo": {{Ref: "video", Kind: "photo"}},
		"gallery pick":           {{Ref: "gallery", Kind: "photo"}},
		"unfinished upload":      {{Ref: "pending", Kind: "photo"}},
		"another tenant's proof": {{Ref: "foreign", Kind: "photo"}},
		"mime disagrees":         {{Ref: "mime", Kind: "video"}},
		"unknown ref":            {{Ref: "nope", Kind: "photo"}},
	} {
		if err := v.ValidateLiveCameraProofs(ctx, "t1", items); !errors.Is(err, proutdomain.ErrInvalidProof) {
			t.Errorf("%s: err = %v, want ErrInvalidProof", name, err)
		}
	}
}
