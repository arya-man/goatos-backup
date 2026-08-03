package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// Weighing verification verdict consumer.
//
// Weighing produced a verification item for every observation
// (weighing/adapters/verificationbridge) and NOTHING consumed the verdict: a
// producer with no consumer is a silent drop, so every verifier approve/reject on
// a weighing proof vanished. This is the missing half.
//
//	verification.verdict.approved (our weighing observation) -> observation verified
//	verification.verdict.rework   (our weighing observation) -> observation rework,
//	                                                            bucket actionable again
//
// It filters STRICTLY on source.module=weighing AND
// source.ref_type IN (weighing_observation, weighing_shed_observation), so a
// vaccination / shifting / feed / birth-death verdict passes through untouched.
const (
	eventVerificationVerdictApproved = "verification.verdict.approved"
	eventVerificationVerdictRework   = "verification.verdict.rework"
)

// weighingVerdictPayload is the subset of the generic verdict payload this
// consumer reads. Every key here is parsed; nothing is accepted-and-discarded.
type weighingVerdictPayload struct {
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

// VerificationVerdictHandler applies a verifier's verdict to the weighing
// observation it verified.
type VerificationVerdictHandler struct {
	store ports.VerificationVerdictStore
	acker VerificationApplyAcker
	log   *slog.Logger
}

func NewVerificationVerdictHandler(store ports.VerificationVerdictStore, log *slog.Logger) *VerificationVerdictHandler {
	return &VerificationVerdictHandler{store: store, log: log}
}

// WithApplyAcker wires the receipt weighing sends back to the verification module
// once a verdict has actually landed on the observation.
//
// Optional on purpose. Without it the apply still happens exactly as before -- the
// ack is a visibility signal, never a correctness gate, and a handler built
// without one must not start dropping verdicts.
func (h *VerificationVerdictHandler) WithApplyAcker(acker VerificationApplyAcker) *VerificationVerdictHandler {
	h.acker = acker
	return h
}

var _ eventbus.Handler = (*VerificationVerdictHandler)(nil)

func (h *VerificationVerdictHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventVerificationVerdictApproved, h)
	bus.Subscribe(eventVerificationVerdictRework, h)
}

func (h *VerificationVerdictHandler) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if h == nil || h.store == nil {
		return nil
	}
	if event.Type != eventVerificationVerdictApproved && event.Type != eventVerificationVerdictRework {
		return nil
	}
	var payload weighingVerdictPayload
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return eventbus.PermanentError(fmt.Errorf("weighing verdict: decode payload: %w", err))
		}
	}
	// Only OUR module's observation verdicts.
	if payload.Source.Module != domain.VerificationModuleWeighing {
		return nil
	}
	refType := strings.TrimSpace(payload.Source.RefType)
	if refType != domain.VerificationRefTypeAnimal && refType != domain.VerificationRefTypeShed {
		return nil
	}
	observationID := strings.TrimSpace(payload.Source.RefID)
	tenantID := strings.TrimSpace(event.TenantID)
	if observationID == "" || tenantID == "" {
		return nil
	}

	status := domain.VerificationStatusVerified
	if event.Type == eventVerificationVerdictRework {
		status = domain.VerificationStatusRework
	}
	// Idempotency is keyed on the event id inside the store, so an at-least-once
	// redelivery replays to the original result with no new side effects.
	_, err := h.store.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:      tenantID,
		ObservationID: observationID,
		RefType:       refType,
		Status:        status,
		VerifiedBy:    strings.TrimSpace(payload.VerifiedBy),
		Reason:        strings.TrimSpace(payload.Reason),
		EventID:       event.ID,
	})
	switch {
	case err == nil:
		h.ackApplied(ctx, tenantID, refType, observationID)
		return nil
	case errors.Is(err, ports.ErrNotFound):
		// The verdict names an observation this tenant does not have. Retrying
		// cannot fix that, so fail permanently to the DLQ instead of spinning.
		if h.log != nil {
			h.log.WarnContext(ctx, "weighing_verdict_observation_missing",
				"tenant_id", tenantID,
				"observation_id", observationID,
				"ref_type", refType,
			)
		}
		return eventbus.PermanentError(fmt.Errorf("weighing verdict: observation %s not found: %w", observationID, err))
	default:
		return fmt.Errorf("weighing verdict: apply %s: %w", status, err)
	}
}

// ackApplied tells the verification module the verdict has landed, so the item
// stops reading as "decided, not yet in effect" on the verifier's surface.
//
// Deliberately AFTER the apply transaction committed and deliberately NOT fatal.
// The ack is a receipt for a write that already succeeded; failing the event here
// would roll the whole delivery back and re-apply an outcome that was already
// applied, trading a visible stale signal for real duplicate work. If it fails,
// the item simply stays in the awaiting-application state -- visibly unfinished
// rather than invisibly wrong -- and the next at-least-once redelivery of the same
// verdict re-runs the (idempotent) apply and re-attempts the ack.
func (h *VerificationVerdictHandler) ackApplied(ctx context.Context, tenantID, refType, observationID string) {
	if h.acker == nil {
		return
	}
	if err := h.acker.AckWeighingVerificationApplied(ctx, tenantID, refType, []string{observationID}); err != nil && h.log != nil {
		h.log.WarnContext(ctx, "weighing_verdict_apply_ack_failed",
			"tenant_id", tenantID,
			"observation_id", observationID,
			"ref_type", refType,
			"error", err,
		)
	}
}
