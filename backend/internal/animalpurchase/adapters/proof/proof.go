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

func (v *Validator) ValidateCandidateMedia(ctx context.Context, tenantID string, proofRefs []string) error {
	allowed := make(map[string][]string, len(proofRefs))
	for _, ref := range proofRefs {
		allowed[ref] = []string{"photo", "video"}
	}
	return v.ValidateCandidateMediaKinds(ctx, tenantID, allowed)
}

func (v *Validator) ValidateCandidateMediaKinds(ctx context.Context, tenantID string, allowedByRef map[string][]string) error {
	proofRefs := make([]string, 0, len(allowedByRef))
	for ref := range allowedByRef {
		proofRefs = append(proofRefs, ref)
	}
	if len(proofRefs) == 0 {
		return nil
	}
	for _, ref := range proofRefs {
		// A reference that is not even a proof id is the same answer as a missing proof: refuse
		// it with the form's message rather than letting the uuid cast surface as a 500.
		if !uuidutil.IsUUIDString(strings.TrimSpace(ref)) {
			return ports.ErrInvalidVideo
		}
	}
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, proofRefs)
	if err != nil {
		return err
	}
	for _, ref := range proofRefs {
		art, ok := found[ref]
		if !ok || art.TenantID != tenantID || art.UploadState != uploadStateCompleted ||
			art.Metadata["capture_source"] != captureSourceInAppCamera {
			return ports.ErrInvalidVideo
		}
		kind := strings.ToLower(strings.TrimSpace(art.ProofType))
		mime := strings.ToLower(strings.TrimSpace(art.MimeType))
		switch {
		case kind == "video" && strings.HasPrefix(mime, "video/"):
		case kind == "photo" && strings.HasPrefix(mime, "image/"):
		default:
			return ports.ErrInvalidVideo
		}
		accepted := false
		for _, a := range allowedByRef[ref] {
			if a == kind {
				accepted = true
			}
		}
		if !accepted {
			return ports.ErrMediaKindNotAccepted
		}
	}
	return nil
}

// Media returns proof metadata with stable backend download routes. The proof download endpoint
// owns signing and attribution when the client actually opens media.
type Media struct {
	service *proofapp.Service
}

func NewMedia(service *proofapp.Service) *Media { return &Media{service: service} }

var _ ports.MediaResolver = (*Media)(nil)

func (m *Media) ResolveMedia(ctx context.Context, tenantID string, proofRefs []string) (map[string]ports.Media, error) {
	artifacts, err := m.service.ArtifactMetadataByIDs(ctx, tenantID, proofRefs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ports.Media, len(artifacts))
	for id, artifact := range artifacts {
		out[id] = ports.Media{URL: "/app/proofs/" + id + "/download", MimeType: artifact.MimeType}
	}
	return out, nil
}
