package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Pen reconciliation verdict consumer (maintainer decision 2026-09-02). The verifier's verdict
// is the ONLY gate on a submitted card — there is no approver — so:
//
//	verification.verdict.approved -> card completed (the animal's return is accepted)
//	verification.verdict.rework   -> card back to rework (operator re-shoots)
//
// It filters strictly on source.module + source.ref_type so shifting/birth/death verdicts,
// which share the "counts" module, pass through untouched.

// penReconciliationVerdictPayload is the subset of verificationVerdictPayload this consumer
// reads. The verification module owns the full payload.
type penReconciliationVerdictPayload struct {
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

// penReconciliationVerdictRepo is the slice of ports.PenReconciliationRepository this handler
// drives.
type penReconciliationVerdictRepo interface {
	ApplyVerifiedPenReconciliation(ctx context.Context, in domain.PenReconciliationVerdictCommand) error
	BouncePenReconciliationForRework(ctx context.Context, in domain.PenReconciliationVerdictCommand) error
}

// PenReconciliationVerificationHandler applies a verifier's verdict to the card it verified.
type PenReconciliationVerificationHandler struct {
	repo penReconciliationVerdictRepo
	now  func() time.Time
}

// NewPenReconciliationVerificationHandler constructs the consumer over the counts repository.
func NewPenReconciliationVerificationHandler(repo penReconciliationVerdictRepo, now func() time.Time) *PenReconciliationVerificationHandler {
	if now == nil {
		now = time.Now
	}
	return &PenReconciliationVerificationHandler{repo: repo, now: now}
}

var _ eventbus.Handler = (*PenReconciliationVerificationHandler)(nil)

// Register subscribes the handler to both verdict event types.
func (h *PenReconciliationVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVerificationVerdictApproved, h)
	bus.Subscribe(EventVerificationVerdictRework, h)
}

// HandleEvent routes an approve/reject verdict for a pen-reconciliation item to the matching
// card write.
func (h *PenReconciliationVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != EventVerificationVerdictApproved && e.Type != EventVerificationVerdictRework {
		return nil
	}
	var p penReconciliationVerdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	// Only OUR ref_type. Shifting, birth, and death share the counts module and must pass
	// through untouched.
	if p.Source.Module != domain.VerificationModulePenReconciliation ||
		p.Source.RefType != domain.VerificationRefTypePenReconciliation {
		return nil
	}
	cardID := strings.TrimSpace(p.Source.RefID)
	if cardID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}

	occurredAt := e.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = h.now().UTC()
	}
	cmd := domain.PenReconciliationVerdictCommand{
		TenantID:   e.TenantID,
		CardID:     cardID,
		VerifiedBy: strings.TrimSpace(p.VerifiedBy),
		VerifiedAt: occurredAt,
		Reason:     strings.TrimSpace(p.Reason),
	}
	switch e.Type {
	case EventVerificationVerdictApproved:
		return h.repo.ApplyVerifiedPenReconciliation(ctx, cmd)
	case EventVerificationVerdictRework:
		return h.repo.BouncePenReconciliationForRework(ctx, cmd)
	}
	return nil
}
