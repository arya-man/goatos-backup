package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// Verification event types: a SOP verify outcome for a recorded vaccination completion. The SOP
// review path (or a composition-layer bridge) publishes one event per verified completion; this
// handler applies the SM-5 outcome. Same code path for the in-process bus and a future Pub/Sub
// consumer.
const (
	EventVaccinationVerifyAccepted = "vaccination.verify.accepted"
	EventVaccinationVerifyRejected = "vaccination.verify.rejected"
	EventGenericVerificationRework = "verification.verdict.rework"
	EventGenericVerificationClosed = "verification.item.closed"
)

// VerificationEvent is the payload for the verify events. CompletionID is required; booster work is
// driven later by the durable vaccination.completed consumer, so booster context is not carried here.
type VerificationEvent struct {
	CompletionID string `json:"completion_id"`
	VerifiedBy   string `json:"verified_by,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type genericVerificationEvent struct {
	// Status is the verification item's terminal status as the producer recorded it. It is
	// the DISAMBIGUATOR for verification.item.closed, which is polysemous: the verification
	// module reuses that one event type for both an operational CLOSURE (a verifier finished
	// with the item) and a RETRACTION (WithdrawItemsBySource -- the producing module
	// superseded its own source record, so the review is cancelled). A new event type was not
	// minted because the outbox partial unique index enumerates types and would need a
	// migration; the cost of that choice is that EVERY consumer must read this field.
	Status     string `json:"status"`
	Reason     string `json:"reason"`
	VerifiedBy string `json:"verified_by"`
	ClosedBy   string `json:"closed_by"`
	Source     struct {
		Module       string `json:"module"`
		SubmissionID string `json:"submission_id"`
		RefType      string `json:"ref_type"`
		RefID        string `json:"ref_id"`
	} `json:"source"`
}

// VerificationHandler applies a SOP verify outcome to a recorded completion: accept → AcceptExisting
// (complete obligation + consume dose + vaccination.completed outbox), reject → RejectExisting
// (rework, obligation stays open). Idempotent — both delegate to accept/reject-only-when-recorded.
// eventbus.Handler.
type VerificationHandler struct {
	completion verificationCompletionService
	closure    VerificationClosureProjector
}

type verificationCompletionService interface {
	AcceptExisting(ctx context.Context, in AcceptExistingInput) (AcceptResult, error)
	RejectExisting(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (RejectResult, error)
	ApplyGoatVerification(ctx context.Context, tenantID, submissionID, goatID, outcome, reason string, actorID *string) error
	ApplySubmissionVerification(ctx context.Context, tenantID, submissionID, outcome, reason string, actorID *string) ([]string, error)
}

// VerificationClosureProjector is the SOP-owned terminal-state projection invoked only after the
// vaccination module has accepted every completion for the closed goat. Composition wiring
// supplies the implementation, keeping this module free of SOP storage details.
type VerificationClosureProjector interface {
	AcceptSubmissionItemVerification(ctx context.Context, tenantID, submissionID, goatID, actorID string) error
}

// NewVerificationHandler constructs the handler over a CompletionService.
func NewVerificationHandler(completion verificationCompletionService) *VerificationHandler {
	return &VerificationHandler{completion: completion}
}

// WithClosureProjector wires the SOP aggregate roll-up for generic verification closure events.
func (h *VerificationHandler) WithClosureProjector(projector VerificationClosureProjector) *VerificationHandler {
	h.closure = projector
	return h
}

var _ eventbus.Handler = (*VerificationHandler)(nil)

// Register subscribes the handler to both verify event types on a bus.
func (h *VerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVaccinationVerifyAccepted, h)
	bus.Subscribe(EventVaccinationVerifyRejected, h)
	bus.Subscribe(EventGenericVerificationRework, h)
	bus.Subscribe(EventGenericVerificationClosed, h)
}

// HandleEvent routes the event by type to the matching SM-5 verification outcome.
func (h *VerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type == EventGenericVerificationRework || e.Type == EventGenericVerificationClosed {
		return h.handleGenericEvent(ctx, e)
	}
	var p VerificationEvent
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.CompletionID == "" {
		return nil
	}
	var verifiedBy *string
	if p.VerifiedBy != "" {
		verifiedBy = &p.VerifiedBy
	}
	switch e.Type {
	case EventVaccinationVerifyAccepted:
		_, err := h.completion.AcceptExisting(ctx, AcceptExistingInput{
			TenantID:     e.TenantID,
			CompletionID: p.CompletionID,
			VerifiedBy:   verifiedBy,
		})
		return err
	case EventVaccinationVerifyRejected:
		_, err := h.completion.RejectExisting(ctx, e.TenantID, p.CompletionID, p.Reason, verifiedBy)
		return err
	}
	return nil
}

func (h *VerificationHandler) handleGenericEvent(ctx context.Context, e eventbus.Event) error {
	var p genericVerificationEvent
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.Source.Module != "vaccination" || p.Source.SubmissionID == "" {
		return nil
	}
	// A WITHDRAWN item is a retraction of the review request, NOT a verdict. Treating it as
	// one would apply outcome "closed" to the goat/submission and then tell the SOP closure
	// projector to ACCEPT the submission item -- accepting work that was explicitly taken
	// back, with an empty actor (a withdrawal carries no closed_by). There is nothing for
	// vaccination to apply: the producing module already superseded its own record and will
	// raise a fresh verification item for the replacement.
	if strings.TrimSpace(p.Status) == verificationdomain.StatusWithdrawn {
		return nil
	}
	outcome := "rejected"
	actor := p.VerifiedBy
	if e.Type == EventGenericVerificationClosed {
		outcome = "closed"
		actor = p.ClosedBy
	}
	var actorID *string
	if actor != "" {
		actorID = &actor
	}
	switch p.Source.RefType {
	case "vaccination_goat":
		if p.Source.RefID == "" {
			return nil
		}
		if err := h.completion.ApplyGoatVerification(
			ctx,
			e.TenantID,
			p.Source.SubmissionID,
			p.Source.RefID,
			outcome,
			p.Reason,
			actorID,
		); err != nil {
			return err
		}
		if e.Type == EventGenericVerificationClosed && h.closure != nil {
			return h.closure.AcceptSubmissionItemVerification(ctx, e.TenantID, p.Source.SubmissionID, p.Source.RefID, actor)
		}
	case "sop_submission":
		goatIDs, err := h.completion.ApplySubmissionVerification(
			ctx,
			e.TenantID,
			p.Source.SubmissionID,
			outcome,
			p.Reason,
			actorID,
		)
		if err != nil {
			return err
		}
		if e.Type == EventGenericVerificationClosed && h.closure != nil {
			for _, goatID := range goatIDs {
				if err := h.closure.AcceptSubmissionItemVerification(ctx, e.TenantID, p.Source.SubmissionID, goatID, actor); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
