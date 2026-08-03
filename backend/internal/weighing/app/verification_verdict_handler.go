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
		// EvidenceID is the proof the verifier actually reviewed. The store has always
		// had a stale-evidence guard keyed on it, but this consumer never read the field
		// and passed an empty id, which the guard treats as "check skipped" -- so in
		// production a verdict rendered against an older video was applied to whatever
		// video was attached by the time it landed, approving or rejecting evidence
		// nobody reviewed. Reading it here is what arms the guard that already exists.
		EvidenceID string `json:"evidence_id"`
	} `json:"source"`
}

// VerificationVerdictHandler applies a verifier's verdict to the weighing
// observation it verified.
type VerificationVerdictHandler struct {
	store ports.VerificationVerdictStore
	log   *slog.Logger
}

func NewVerificationVerdictHandler(store ports.VerificationVerdictStore, log *slog.Logger) *VerificationVerdictHandler {
	return &VerificationVerdictHandler{store: store, log: log}
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
		TenantID:        tenantID,
		ObservationID:   observationID,
		RefType:         refType,
		Status:          status,
		VerifiedBy:      strings.TrimSpace(payload.VerifiedBy),
		Reason:          strings.TrimSpace(payload.Reason),
		EventID:         event.ID,
		EvidenceProofID: strings.TrimSpace(payload.Source.EvidenceID),
	})
	switch {
	case err == nil:
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
	case errors.Is(err, ports.ErrStaleEvidence):
		// The verdict named a proof the observation no longer carries. Redelivering it
		// can only make things worse -- the attached proof moves further away from what
		// was reviewed, never back -- so this fails permanently to the DLQ instead of
		// retrying, and the observation keeps its current state until somebody reviews
		// the CURRENT proof. Deliberately kept distinct from the not-found branch: the
		// observation exists and is perfectly writable, it is the evidence that moved on,
		// and an operator chasing a "missing observation" log line would be chasing the
		// wrong thing.
		if h.log != nil {
			h.log.WarnContext(ctx, "weighing_verdict_stale_evidence",
				"tenant_id", tenantID,
				"observation_id", observationID,
				"ref_type", refType,
				"evidence_id", strings.TrimSpace(payload.Source.EvidenceID),
				"event_id", event.ID,
			)
		}
		return eventbus.PermanentError(fmt.Errorf("weighing verdict: observation %s evidence superseded: %w", observationID, err))
	default:
		return fmt.Errorf("weighing verdict: apply %s: %w", status, err)
	}
}
