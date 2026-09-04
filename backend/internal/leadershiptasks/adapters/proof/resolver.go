// Package proof adapts the proof module's repository to the Leadership Tasks
// AttachmentResolver port.
//
// A task attachment is not evidence of field work, so none of the capture-honesty rules a
// weighing or toxin proof carries apply: a gallery pick, a file from a drive, a voice note
// are all exactly what the director meant to attach. What IS asserted is that the bytes are
// really there -- the proof exists in this tenant, was registered as an `attachment`, and
// its upload finished -- so a task never points at a file nobody can open. Mime, size and
// duration are read from the stored artifact, never trusted from the request.
package proof

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
)

const (
	uploadStateCompleted = "completed"
	proofTypeAttachment  = "attachment"
)

// Resolver checks attachment refs against the proof store.
type Resolver struct {
	repo proofports.Repository
}

// NewResolver wires the resolver over the proof repository.
func NewResolver(repo proofports.Repository) *Resolver {
	return &Resolver{repo: repo}
}

var _ ports.AttachmentResolver = (*Resolver)(nil)

// ResolveAttachments fails closed: any gap rejects the whole raise/edit with
// ErrInvalidAttachment, because a task with one dead attachment is a task the CXO cannot
// fully read.
func (v *Resolver) ResolveAttachments(ctx context.Context, tenantID, uploaderID string, refs []domain.AttachmentRef) ([]domain.Attachment, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.ProofID)
	}
	found, err := v.repo.GetProofsByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Attachment, 0, len(refs))
	for i, ref := range refs {
		art, ok := found[ref.ProofID]
		if !ok || art.TenantID != tenantID ||
			art.UploadState != uploadStateCompleted ||
			strings.ToLower(strings.TrimSpace(art.ProofType)) != proofTypeAttachment {
			return nil, ports.ErrInvalidAttachment
		}
		// The uploader must be the raiser: an attachment uploaded by someone else is not
		// this director's to attach, and a guessed proof id must not surface another
		// module's media on a task.
		if art.UploadedBy == nil || strings.TrimSpace(*art.UploadedBy) != strings.TrimSpace(uploaderID) {
			return nil, ports.ErrInvalidAttachment
		}
		name := strings.TrimSpace(ref.FileName)
		if name == "" {
			if stored, ok := art.Metadata["file_name"].(string); ok {
				name = strings.TrimSpace(stored)
			}
		}
		out = append(out, domain.Attachment{
			ProofID:    art.ProofID,
			Kind:       ref.Kind,
			MimeType:   strings.TrimSpace(art.MimeType),
			FileName:   name,
			SizeBytes:  art.SizeBytes,
			DurationMS: art.DurationMS,
			Position:   i,
		})
	}
	return out, nil
}
