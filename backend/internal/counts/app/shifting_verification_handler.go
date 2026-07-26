package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Shifting verification consumer (maintainer decision, 2026-07-26). A shed move is applied only when
// an independent verifier approves the operator's video. The verification module emits a generic
// verdict event on every approve/reject; this handler is the counts-side consumer of those events
// for shifting-move items:
//
//	verification.verdict.approved (our shifting_move item) -> ApplyVerifiedShiftingEvent  (relocate, count moves NOW)
//	verification.verdict.rework   (our shifting_move item) -> BounceShiftingEventForRework (back to authorized)
//
// It filters strictly on source.module + source.ref_type so a vaccination/feed/etc. verdict is
// ignored. This is the same in-process bus code path a future durable Pub/Sub consumer would use.
const (
	EventVerificationVerdictApproved = "verification.verdict.approved"
	EventVerificationVerdictRework   = "verification.verdict.rework"
)

// shiftingVerdictPayload is the subset of verificationVerdictPayload this consumer reads. The
// verification module owns the full payload; only these fields matter to shifting.
type shiftingVerdictPayload struct {
	Status     string `json:"status"`
	Decision   string `json:"decision"`
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

// shiftingVerificationRepo is the slice of ports.Repository this handler drives.
type shiftingVerificationRepo interface {
	ApplyVerifiedShiftingEvent(ctx context.Context, in domain.ShiftingVerifiedApplyCommand) (domain.ShiftingExecutionResult, bool, error)
	BounceShiftingEventForRework(ctx context.Context, in domain.ShiftingReworkCommand) error
}

// ShiftingVerificationHandler applies a verifier's verdict to the movement it verified.
type ShiftingVerificationHandler struct {
	repo shiftingVerificationRepo
	now  func() time.Time
}

// NewShiftingVerificationHandler constructs the consumer over the counts repository.
func NewShiftingVerificationHandler(repo shiftingVerificationRepo, now func() time.Time) *ShiftingVerificationHandler {
	if now == nil {
		now = time.Now
	}
	return &ShiftingVerificationHandler{repo: repo, now: now}
}

var _ eventbus.Handler = (*ShiftingVerificationHandler)(nil)

// Register subscribes the handler to both verdict event types.
func (h *ShiftingVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVerificationVerdictApproved, h)
	bus.Subscribe(EventVerificationVerdictRework, h)
}

// HandleEvent routes an approve/reject verdict for a shifting-move item to the matching kernel write.
func (h *ShiftingVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != EventVerificationVerdictApproved && e.Type != EventVerificationVerdictRework {
		return nil
	}
	var p shiftingVerdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	// Only OUR module's shifting-move verdicts. Everything else (vaccination, feed, diagnosis) is a
	// different producer's item and must pass through untouched.
	if p.Source.Module != domain.VerificationModuleShifting || p.Source.RefType != domain.VerificationRefTypeShifting {
		return nil
	}
	shiftingEventID := strings.TrimSpace(p.Source.RefID)
	if shiftingEventID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}

	switch e.Type {
	case EventVerificationVerdictApproved:
		occurredAt := e.OccurredAt
		if occurredAt.IsZero() {
			occurredAt = h.now().UTC()
		}
		_, _, err := h.repo.ApplyVerifiedShiftingEvent(ctx, domain.ShiftingVerifiedApplyCommand{
			TenantID:         e.TenantID,
			ShiftingEventID:  shiftingEventID,
			VerifiedByUserID: strings.TrimSpace(p.VerifiedBy),
			VerifiedAt:       occurredAt,
			// Thread the verification event id as the relocation's trace id so the goat.location.changed
			// / goat.stage_changed outbox envelopes carry a non-empty trace_id (envelope requires it).
			TraceID: e.ID,
		})
		return err
	case EventVerificationVerdictRework:
		return h.repo.BounceShiftingEventForRework(ctx, domain.ShiftingReworkCommand{
			TenantID:        e.TenantID,
			ShiftingEventID: shiftingEventID,
			VerifiedBy:      strings.TrimSpace(p.VerifiedBy),
			Reason:          strings.TrimSpace(p.Reason),
		})
	}
	return nil
}
