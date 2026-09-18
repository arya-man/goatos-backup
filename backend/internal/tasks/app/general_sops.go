package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// GENERAL SOPs (maintainer decision 2026-09-18, SOP studio phase 2). A general SOP is farm-wide
// work tied to no module -- the gate visitor check -- authored like every other work
// instruction and STARTED BY HAND: each start is a new workflow instance keyed on a fresh
// subject_ref_id, compiled from the SOP's published "main" track by the same opener the herd
// operations use, so it pins its version, runs its answer-driven branches, and reports the
// same card. No animal is involved, so subject_goat_id stays empty.

// ListGeneralSOPs is what the phone's "Start a work instruction" list shows.
func (s *Service) ListGeneralSOPs(ctx context.Context, tenantID string) ([]ports.GeneralSOP, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, domain.ErrMissingRequiredField
	}
	return s.repo.ListGeneralSOPs(ctx, tenantID)
}

// StartGeneralWorkflowInput opens one run of a general SOP.
type StartGeneralWorkflowInput struct {
	TenantID string
	SOPCode  string
	ParkID   string
	ActorID  string
	// RunID is the client's idempotency key for THIS start: the same key replays the same
	// workflow rather than opening a second one.
	RunID string
}

// StartGeneralWorkflow opens (or replays) a general SOP run and returns its workflow id.
func (s *Service) StartGeneralWorkflow(ctx context.Context, in StartGeneralWorkflowInput) (string, error) {
	code := strings.TrimSpace(in.SOPCode)
	if strings.TrimSpace(in.TenantID) == "" || code == "" {
		return "", domain.ErrMissingRequiredField
	}
	if !strings.HasPrefix(code, "general.") {
		return "", domain.ErrUnknownTemplate
	}
	ref := strings.TrimSpace(in.RunID)
	if ref == "" {
		ref = newRunID()
	}
	return s.OpenSubjectWorkflow(ctx, OpenSubjectWorkflowInput{
		TenantID:     in.TenantID,
		TemplateKey:  domain.GeneralTemplateKey(code),
		SubjectRefID: ref,
		EventAt:      s.now(),
		ParkID:       in.ParkID,
	})
}

func newRunID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
