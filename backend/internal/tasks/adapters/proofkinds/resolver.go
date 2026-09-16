// Package proofkinds adapts the proof register to the tasks ProofKindResolver port: a verifier
// item names the kind the STORE judged a capture to be (its proof_type), never the kind a client
// claimed. Read-only, composition-layer glue; tasks never names proof's tables.
package proofkinds

import (
	"context"
	"strings"

	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// proofReader is the slice of proof's repository this adapter needs.
type proofReader interface {
	GetProofsByIDs(ctx context.Context, tenantID string, proofIDs []string) (map[string]proofdomain.Artifact, error)
}

// Resolver answers ref -> video | photo from the register. A ref the register does not know, or
// holds under a kind that is neither (attachment, audio), is absent from the map so the caller
// keeps the client's kind.
type Resolver struct {
	proofs proofReader
}

// New constructs the resolver over the proof repository.
func New(proofs proofReader) *Resolver { return &Resolver{proofs: proofs} }

var _ tasksapp.ProofKindResolver = (*Resolver)(nil)

// ResolveProofKinds is one batched read for every ref of the write (never per ref).
func (r *Resolver) ResolveProofKinds(ctx context.Context, tenantID string, refs []string) (map[string]string, error) {
	if r == nil || r.proofs == nil || len(refs) == 0 {
		return map[string]string{}, nil
	}
	found, err := r.proofs.GetProofsByIDs(ctx, tenantID, refs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(found))
	for ref, artifact := range found {
		if artifact.TenantID != "" && artifact.TenantID != tenantID {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(artifact.ProofType)) {
		case "video":
			out[ref] = tasksdomain.ProofKindVideo
		case "photo", "image":
			out[ref] = tasksdomain.ProofKindPhoto
		}
	}
	return out, nil
}
