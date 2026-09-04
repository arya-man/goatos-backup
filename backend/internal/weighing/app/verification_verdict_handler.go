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
	store   ports.VerificationVerdictStore
	fasting ports.FastingStore
	acker   VerificationApplyAcker
	log     *slog.Logger
}

func NewVerificationVerdictHandler(store ports.VerificationVerdictStore, log *slog.Logger) *VerificationVerdictHandler {
	return &VerificationVerdictHandler{store: store, log: log}
}

// WithFastingStore wires the feed & water removal verdict apply. Optional like
// the acker: a handler built without it keeps applying observation verdicts
// exactly as before and simply skips fasting verdicts.
func (h *VerificationVerdictHandler) WithFastingStore(store ports.FastingStore) *VerificationVerdictHandler {
	h.fasting = store
	return h
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
	if refType != domain.VerificationRefTypeAnimal && refType != domain.VerificationRefTypeShed && refType != domain.VerificationRefTypeFasting {
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
	if refType == domain.VerificationRefTypeFasting {
		return h.handleFastingVerdict(ctx, tenantID, observationID, status, event.ID, payload)
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
	case errors.Is(err, ports.ErrIdempotencyConflict):
		// The stored fingerprint for this event id does not match the one we just computed.
		//
		// This branch exists because adding evidence_proof_id to the fingerprint made the
		// conflict REACHABLE for events that were already applied. A verdict applied BEFORE
		// that change stored a fingerprint computed without the field; redelivered after it --
		// which an at-least-once bus does routinely -- it now recomputes to something
		// different and conflicts. Stored fingerprints are not versioned and are not migrated.
		//
		// Without this case the error fell to default and was returned bare, i.e. RETRYABLE.
		// A replay that used to be a free no-op became a poison message: retried forever,
		// never succeeding, and never reaching the DLQ where somebody would see it. Failing
		// permanently is right on the merits too -- a genuine same-id-different-evidence
		// verdict is a contradiction that only a human can resolve, and retrying cannot.
		if h.log != nil {
			h.log.WarnContext(ctx, "weighing_verdict_idempotency_conflict",
				"tenant_id", tenantID,
				"observation_id", observationID,
				"ref_type", refType,
				"evidence_id", strings.TrimSpace(payload.Source.EvidenceID),
				"event_id", event.ID,
			)
		}
		return eventbus.PermanentError(fmt.Errorf("weighing verdict: observation %s idempotency fingerprint conflict: %w", observationID, err))
	default:
		return fmt.Errorf("weighing verdict: apply %s: %w", status, err)
	}
}

// handleFastingVerdict applies a verifier's decision to the feed & water
// removal task. It NEVER touches submitted_at — the midnight gate reads
// submission, and a rework must not un-run a weighing that already happened;
// the operator simply re-records and re-submits the removal evidence.
func (h *VerificationVerdictHandler) handleFastingVerdict(ctx context.Context, tenantID, fastingTaskID, status, eventID string, payload weighingVerdictPayload) error {
	if h.fasting == nil {
		// Built without the fasting seam: skip rather than fail — the verdict
		// stays pending on the verification side until a wired consumer runs.
		if h.log != nil {
			h.log.WarnContext(ctx, "weighing_fasting_verdict_skipped_no_store",
				"tenant_id", tenantID, "fasting_task_id", fastingTaskID)
		}
		return nil
	}
	err := h.fasting.ApplyFastingVerdict(ctx, domain.FastingVerdict{
		TenantID:      tenantID,
		FastingShedID: fastingTaskID,
		Status:        status,
		VerifiedBy:    strings.TrimSpace(payload.VerifiedBy),
		Reason:        strings.TrimSpace(payload.Reason),
		EventID:       eventID,
	})
	switch {
	case err == nil:
		h.ackApplied(ctx, tenantID, domain.VerificationRefTypeFasting, fastingTaskID)
		return nil
	case errors.Is(err, ports.ErrNotFound):
		if h.log != nil {
			h.log.WarnContext(ctx, "weighing_fasting_verdict_task_missing",
				"tenant_id", tenantID, "fasting_task_id", fastingTaskID)
		}
		return eventbus.PermanentError(fmt.Errorf("weighing fasting verdict: task %s not found: %w", fastingTaskID, err))
	case errors.Is(err, ports.ErrIdempotencyConflict):
		if h.log != nil {
			h.log.WarnContext(ctx, "weighing_fasting_verdict_idempotency_conflict",
				"tenant_id", tenantID, "fasting_task_id", fastingTaskID, "event_id", eventID)
		}
		return eventbus.PermanentError(fmt.Errorf("weighing fasting verdict: task %s idempotency conflict: %w", fastingTaskID, err))
	default:
		return fmt.Errorf("weighing fasting verdict: apply %s: %w", status, err)
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
