package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// PROCUREMENT IS SOP-DRIVEN END TO END (maintainer decision 2026-09-20). Opening a purchase load
// is followed by work -- record the animals, get the office's decision, receive them at the farm
// -- that lived nowhere at all: no step, no owner, and no proof the animals ever arrived. It is
// now ONE workflow per load, opened from the published `procurement.animal_purchase_intake` SOP's
// track by the same opener the sale and the herd operations use, keyed on the load
// (subject_ref_id = animal_purchase_loads.load_id, no animal), pinned to the version in force,
// with each step carrying the designation that does it.
//
// Two consumers, both registered in eventwiring.RegisterWorkflowConsumers so every bus process
// behaves alike:
//
//	procurement.animal_purchase.load_recorded -> open the load's workflow (idempotent on the load)
//	procurement.animal_purchase.decided       -> complete its decision step, once nothing is waiting
const (
	EventAnimalPurchaseLoadRecorded = "procurement.animal_purchase.load_recorded"
	EventAnimalPurchaseDecided      = "procurement.animal_purchase.decided"
)

// animalPurchaseLoadRecordedPayload is the subset of the producer's payload the opener reads.
type animalPurchaseLoadRecordedPayload struct {
	LoadID string `json:"load_id"`
	ParkID string `json:"park_id"`
}

// AnimalPurchaseLoadRecordedWorkflowHandler opens a purchase load's intake workflow.
type AnimalPurchaseLoadRecordedWorkflowHandler struct{ svc *Service }

// NewAnimalPurchaseLoadRecordedWorkflowHandler constructs the opener.
func NewAnimalPurchaseLoadRecordedWorkflowHandler(svc *Service) *AnimalPurchaseLoadRecordedWorkflowHandler {
	return &AnimalPurchaseLoadRecordedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*AnimalPurchaseLoadRecordedWorkflowHandler)(nil)

// Register subscribes the load-recorded event.
func (h *AnimalPurchaseLoadRecordedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventAnimalPurchaseLoadRecorded, h)
}

// HandleEvent opens (or replays) the load's workflow. A redelivery finds the workflow by its
// subject ref and opens nothing.
func (h *AnimalPurchaseLoadRecordedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p animalPurchaseLoadRecordedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	loadID := strings.TrimSpace(p.LoadID)
	if loadID == "" {
		loadID = strings.TrimSpace(e.Key)
	}
	if loadID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	eventAt := e.OccurredAt
	if eventAt.IsZero() {
		eventAt = time.Now()
	}
	_, err := h.svc.OpenSubjectWorkflow(ctx, OpenSubjectWorkflowInput{
		TenantID:     e.TenantID,
		TemplateKey:  domain.TemplateKeyAnimalPurchaseIntake,
		SubjectRefID: loadID,
		EventAt:      eventAt,
		ParkID:       strings.TrimSpace(p.ParkID),
	})
	return err
}

// animalPurchaseDecidedPayload is the subset of the decision payload the completer reads. The
// PENDING COUNT is the producer's own, computed inside the decision transaction -- the consumer
// never re-counts the load, so the step cannot disagree with the row that moved.
type animalPurchaseDecidedPayload struct {
	LoadID     string `json:"load_id"`
	Pending    int    `json:"load_pending"`
	Accepted   int    `json:"load_accepted"`
	Rejected   int    `json:"load_rejected"`
	OccurredAt string `json:"occurred_at"`
}

// AnimalPurchaseDecidedWorkflowHandler completes the intake workflow's decision step once no
// animal in the load is still waiting.
type AnimalPurchaseDecidedWorkflowHandler struct{ svc *Service }

// NewAnimalPurchaseDecidedWorkflowHandler constructs the completer.
func NewAnimalPurchaseDecidedWorkflowHandler(svc *Service) *AnimalPurchaseDecidedWorkflowHandler {
	return &AnimalPurchaseDecidedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*AnimalPurchaseDecidedWorkflowHandler)(nil)

// Register subscribes the decision event.
func (h *AnimalPurchaseDecidedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventAnimalPurchaseDecided, h)
}

// HandleEvent completes the decision step when the decision just taken was the LAST one owed. A
// load still holding a waiting animal leaves the step open -- that is the whole point of the
// engine owning it, rather than a tap that could claim a load was decided over an unanswered
// animal.
func (h *AnimalPurchaseDecidedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p animalPurchaseDecidedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	loadID := strings.TrimSpace(p.LoadID)
	if loadID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	if p.Pending > 0 {
		return nil
	}
	// A load whose counts are all zero has no animal at all: nothing was decided, so nothing is
	// finished. Completing the step there would report a decision on an empty load.
	if p.Accepted+p.Rejected == 0 {
		return nil
	}
	at := e.OccurredAt
	if raw := strings.TrimSpace(p.OccurredAt); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			at = parsed
		}
	}
	if at.IsZero() {
		at = time.Now()
	}
	return h.svc.CompleteAnimalPurchaseDecisionStep(ctx, e.TenantID, loadID, at.UTC())
}

// The FEED purchase's own workflow (same decision, same shape as the animal load's). Three
// consumers, all registered in eventwiring.RegisterWorkflowConsumers:
//
//	procurement.feed_purchase.recorded -> open the load's workflow (idempotent on the load)
//	procurement.feed_purchase.reached  -> complete its arrival step
//	procurement.toxin_test.accepted    -> complete its aflatoxin step
const (
	EventFeedPurchaseRecorded = "procurement.feed_purchase.recorded"
	EventFeedPurchaseReached  = "procurement.feed_purchase.reached"
	EventToxinTestAccepted    = "procurement.toxin_test.accepted"
)

// feedPurchasePayload is the subset every feed-purchase consumer here reads.
type feedPurchasePayload struct {
	FeedPurchaseID string `json:"feed_purchase_id"`
	ParkID         string `json:"park_id"`
	ReachedOn      string `json:"reached_on"`
	AcceptedAt     string `json:"accepted_at"`
}

// FeedPurchaseRecordedWorkflowHandler opens a bought feed load's workflow.
type FeedPurchaseRecordedWorkflowHandler struct{ svc *Service }

// NewFeedPurchaseRecordedWorkflowHandler constructs the opener.
func NewFeedPurchaseRecordedWorkflowHandler(svc *Service) *FeedPurchaseRecordedWorkflowHandler {
	return &FeedPurchaseRecordedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*FeedPurchaseRecordedWorkflowHandler)(nil)

// Register subscribes the purchase event.
func (h *FeedPurchaseRecordedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventFeedPurchaseRecorded, h)
}

// HandleEvent opens (or replays) the load's workflow. If the purchase was recorded after it had
// already reached the farm, reconcile the arrival hook here too; the outbox orders same-transaction
// events by timestamp and UUID, so the separate reached event may run first and find no workflow.
func (h *FeedPurchaseRecordedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	p, ok := decodeFeedPurchase(e)
	if !ok {
		return nil
	}
	eventAt := e.OccurredAt
	if eventAt.IsZero() {
		eventAt = time.Now()
	}
	_, err := h.svc.OpenSubjectWorkflow(ctx, OpenSubjectWorkflowInput{
		TenantID:     e.TenantID,
		TemplateKey:  domain.TemplateKeyFeedPurchaseIntake,
		SubjectRefID: p.FeedPurchaseID,
		EventAt:      eventAt,
		ParkID:       strings.TrimSpace(p.ParkID),
	})
	if err != nil || strings.TrimSpace(p.ReachedOn) == "" {
		return err
	}
	return h.svc.CompleteFeedPurchaseReachedStep(ctx, e.TenantID, p.FeedPurchaseID, eventAt.UTC())
}

// FeedPurchaseReachedWorkflowHandler completes the arrival step when the ledger marks the load
// delivered. The ledger's write is the truth; the step follows it, never the other way round.
type FeedPurchaseReachedWorkflowHandler struct{ svc *Service }

// NewFeedPurchaseReachedWorkflowHandler constructs the completer.
func NewFeedPurchaseReachedWorkflowHandler(svc *Service) *FeedPurchaseReachedWorkflowHandler {
	return &FeedPurchaseReachedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*FeedPurchaseReachedWorkflowHandler)(nil)

// Register subscribes the arrival event.
func (h *FeedPurchaseReachedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventFeedPurchaseReached, h)
}

// HandleEvent completes the arrival step; a load with no workflow yet, or a step already done, is
// a no-op.
func (h *FeedPurchaseReachedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	p, ok := decodeFeedPurchase(e)
	if !ok {
		return nil
	}
	at := e.OccurredAt
	if at.IsZero() {
		at = time.Now()
	}
	return h.svc.CompleteFeedPurchaseReachedStep(ctx, e.TenantID, p.FeedPurchaseID, at.UTC())
}

// ToxinTestAcceptedWorkflowHandler completes the feed load's aflatoxin step when a round on it is
// accepted. This is the whole mapping between the toxin module and the purchase: the toxin round
// keeps its own state machine, its own retests and its own CEO verdict, and the purchase's step
// simply records that the load has been screened.
type ToxinTestAcceptedWorkflowHandler struct{ svc *Service }

// NewToxinTestAcceptedWorkflowHandler constructs the completer.
func NewToxinTestAcceptedWorkflowHandler(svc *Service) *ToxinTestAcceptedWorkflowHandler {
	return &ToxinTestAcceptedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*ToxinTestAcceptedWorkflowHandler)(nil)

// Register subscribes the accepted round.
func (h *ToxinTestAcceptedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventToxinTestAccepted, h)
}

// HandleEvent completes the aflatoxin step for the load the accepted round belongs to.
func (h *ToxinTestAcceptedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	p, ok := decodeFeedPurchase(e)
	if !ok {
		return nil
	}
	at := e.OccurredAt
	if raw := strings.TrimSpace(p.AcceptedAt); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			at = parsed
		}
	}
	if at.IsZero() {
		at = time.Now()
	}
	return h.svc.CompleteToxinTestStep(ctx, e.TenantID, p.FeedPurchaseID, at.UTC())
}

// decodeFeedPurchase reads the load id off an event, falling back to the bus key. A payload that
// names no load, or an event with no tenant, is ignored rather than guessed at.
func decodeFeedPurchase(e eventbus.Event) (feedPurchasePayload, bool) {
	var p feedPurchasePayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return p, false
		}
	}
	p.FeedPurchaseID = strings.TrimSpace(p.FeedPurchaseID)
	if p.FeedPurchaseID == "" || strings.TrimSpace(e.TenantID) == "" {
		return p, false
	}
	return p, true
}
