package proofmedia

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	vports "github.com/vgoats/goatos/backend/internal/verification/ports"
)

var _ vports.ProofMediaKindReader = (*Resolver)(nil)

// ProofMediaKinds implements verification/ports.ProofMediaKindReader: the declared mime of each
// proof whose item did not name its kind, from ONE batched metadata read.
//
// It never signs a URL and never stats stored bytes -- this runs on the verifier's queue page.
// Only canonical UUID ids are asked for: the register casts ids to uuid, so one legacy non-uuid ref
// would otherwise fail the whole page's batch. A proof store without a batch reader answers an
// empty map (unknown kinds, not an error).
//
// Mime order: the RECORDED mime when it is an image/ or video/ type; otherwise the proof type
// (photo -> image/jpeg, video -> video/mp4); otherwise blank (an attachment, or a register that
// cannot tell), which the renderers show as an openable proof rather than guessing a player.
func (r *Resolver) ProofMediaKinds(ctx context.Context, tenantID string, proofIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if r == nil || r.proof == nil {
		return out, nil
	}
	reader, ok := r.proof.(ArtifactMetadataBatchReader)
	if !ok {
		return out, nil
	}
	ids := make([]string, 0, len(proofIDs))
	seen := make(map[string]struct{}, len(proofIDs))
	for _, id := range proofIDs {
		id = strings.TrimSpace(id)
		if !uuidutil.IsUUIDString(id) {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}
	artifacts, err := reader.ArtifactMetadataByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	for id, artifact := range artifacts {
		out[id] = declaredMime(artifact.MimeType, artifact.ProofType)
	}
	return out, nil
}

func declaredMime(recorded, proofType string) string {
	recorded = strings.ToLower(strings.TrimSpace(recorded))
	if strings.HasPrefix(recorded, "image/") || strings.HasPrefix(recorded, "video/") {
		return recorded
	}
	switch strings.ToLower(strings.TrimSpace(proofType)) {
	case "photo":
		return "image/jpeg"
	case "video":
		return "video/mp4"
	default:
		return ""
	}
}
