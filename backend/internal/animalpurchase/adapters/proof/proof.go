// Package proof adapts the proof module to the Animal purchases seams: the video validator the
// write path fails closed on, and the playback signer the reads use.
package proof

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

const (
	uploadStateCompleted     = "completed"
	captureSourceInAppCamera = "in_app_camera"
)

// Validator checks a candidate video against the proof store: same tenant, finished upload,
// declared AND stored as video, recorded by the in-app camera (the toxin validator's shape).
type Validator struct {
	repo proofports.Repository
}

func NewValidator(repo proofports.Repository) *Validator { return &Validator{repo: repo} }

var _ ports.ProofValidator = (*Validator)(nil)

func (v *Validator) ValidateCandidateVideo(ctx context.Context, tenantID, proofRef string) error {
	// A reference that is not even a proof id is the same answer as a missing proof: refuse it
	// with the form's message rather than letting the uuid cast surface as a 500.
	if !uuidutil.IsUUIDString(strings.TrimSpace(proofRef)) {
		return ports.ErrInvalidVideo
	}
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, []string{proofRef})
	if err != nil {
		return err
	}
	art, ok := found[proofRef]
	if !ok || art.TenantID != tenantID || art.UploadState != uploadStateCompleted ||
		strings.ToLower(strings.TrimSpace(art.ProofType)) != "video" ||
		!strings.HasPrefix(strings.ToLower(strings.TrimSpace(art.MimeType)), "video/") ||
		art.Metadata["capture_source"] != captureSourceInAppCamera {
		return ports.ErrInvalidVideo
	}
	return nil
}

// Media signs playback URLs through the proof service (local: a signed /app/proofs path; GCS: a
// signed object URL).
type Media struct {
	service *proofapp.Service
}

func NewMedia(service *proofapp.Service) *Media { return &Media{service: service} }

var _ ports.MediaResolver = (*Media)(nil)

func (m *Media) ResolveMedia(ctx context.Context, tenantID string, proofRefs []string) (map[string]ports.Media, error) {
	signed, err := m.service.DownloadArtifactsByIDs(ctx, tenantID, proofRefs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ports.Media, len(signed))
	for id, s := range signed {
		out[id] = ports.Media{URL: s.URL, MimeType: s.Artifact.MimeType}
	}
	return out, nil
}
