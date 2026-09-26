package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// SALES SOP (maintainer instruction 2026-09-19, docs/decisions/sales-sop.md). A recorded sale is
// followed by work -- tag the animals, load them, settle the money -- that used to be hard-coded
// on the phone. It is now ONE workflow per sale, opened from the published `sales.deal` SOP's
// `sales_deal` track by the same opener the herd operations and general runs use, keyed on the
// deal (subject_ref_id = sales_deals.id, no animal), pinned to the version in force, with each
// step carrying the designation that does it.
//
// Two consumers, both registered in eventwiring.RegisterWorkflowConsumers so every bus process
// behaves alike:
//
//	sales.deal.recorded   -> open the sale's workflow (idempotent on the deal)
//	goat.sale_allocated   -> complete its sale_tag_animals step (the confirm IS the proof)
const (
	EventSalesDealRecorded      = "sales.deal.recorded"
	EventGoatSaleAllocated      = "goat.sale_allocated"
	EventSalesDealStatusChanged = "sales.deal.status_changed"
)

// salesDealRecordedPayload is the subset of the sales producer's payload the opener reads.
type salesDealRecordedPayload struct {
	SalesDealID string `json:"sales_deal_id"`
	ParkID      string `json:"park_id"`
	SaleDate    string `json:"sale_date"`
	// Status is the deal's status at record time. A sale recorded already failed owes no work.
	Status string `json:"status"`
	// HasLiveAnimals decides the `sale_has_animals` steps (tag, loading video, gate pass). A
	// POINTER on purpose: an event written before the key existed (still in the outbox on deploy)
	// decodes as nil and opens exactly as it did before -- with every step.
	HasLiveAnimals *bool `json:"has_live_animals"`
}

// dealStatusFailed is the sales ledger's failed-deal word (sales/domain.StatusDealFailed). The
// tasks module names it rather than importing sales: the payload is the contract.
const dealStatusFailed = "Deal Failed"

// SaleRecordedWorkflowHandler opens the sale's workflow when a sale is recorded.
type SaleRecordedWorkflowHandler struct{ svc *Service }

// NewSaleRecordedWorkflowHandler constructs the opener.
func NewSaleRecordedWorkflowHandler(svc *Service) *SaleRecordedWorkflowHandler {
	return &SaleRecordedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*SaleRecordedWorkflowHandler)(nil)

// Register subscribes the recorded event.
func (h *SaleRecordedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventSalesDealRecorded, h)
}

// HandleEvent opens (or replays) the sale's workflow. A redelivery finds the workflow by its
// subject ref and opens nothing.
func (h *SaleRecordedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p salesDealRecordedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	dealID := strings.TrimSpace(p.SalesDealID)
	if dealID == "" {
		dealID = strings.TrimSpace(e.Key)
	}
	if dealID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(p.Status), dealStatusFailed) {
		// Recorded as already failed: nothing is owed on it, and a card opened here would be
		// work nobody does.
		return nil
	}
	eventAt := e.OccurredAt
	if eventAt.IsZero() {
		eventAt = time.Now()
	}
	_, err := h.svc.OpenSubjectWorkflow(ctx, OpenSubjectWorkflowInput{
		TenantID:       e.TenantID,
		TemplateKey:    domain.TemplateKeySalesDeal,
		SubjectRefID:   dealID,
		EventAt:        eventAt,
		ParkID:         strings.TrimSpace(p.ParkID),
		SaleHasAnimals: p.HasLiveAnimals,
		// The sale's work is owed on the sale's own day: a sale planned for a later day is not
		// overdue the moment it is recorded (2026-09-26). Dated today or earlier: the recording.
		ClockAnchor: domain.SaleClockAnchor(eventAt, p.SaleDate),
	})
	return err
}

// goatSaleAllocatedPayload is the subset of identity's goat.sale_allocated payload the tag-step
// completion reads.
type goatSaleAllocatedPayload struct {
	SalesDealID string `json:"sales_deal_id"`
	AllocatedAt string `json:"allocated_at"`
}

// SaleAllocatedWorkflowHandler completes the sale workflow's tag-animals step when the allocation
// confirm lands. The confirm transaction already tagged the animals and exited them; the step
// records that the work is done, never the other way round.
type SaleAllocatedWorkflowHandler struct{ svc *Service }

// NewSaleAllocatedWorkflowHandler constructs the completer.
func NewSaleAllocatedWorkflowHandler(svc *Service) *SaleAllocatedWorkflowHandler {
	return &SaleAllocatedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*SaleAllocatedWorkflowHandler)(nil)

// Register subscribes the allocation confirm.
func (h *SaleAllocatedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatSaleAllocated, h)
}

// HandleEvent completes the tag step; a sale without a workflow yet, or one whose step is already
// done, is a no-op (idempotent on the step's own completion).
func (h *SaleAllocatedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p goatSaleAllocatedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	dealID := strings.TrimSpace(p.SalesDealID)
	if dealID == "" {
		dealID = strings.TrimSpace(e.Key)
	}
	if dealID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	at := e.OccurredAt
	if raw := strings.TrimSpace(p.AllocatedAt); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			at = parsed
		}
	}
	if at.IsZero() {
		at = time.Now()
	}
	return h.svc.CompleteSaleTagStep(ctx, e.TenantID, dealID, at.UTC())
}

// salesDealStatusChangedPayload is the subset of the sales producer's status-change payload the
// cancel consumer reads.
type salesDealStatusChangedPayload struct {
	SalesDealID string `json:"sales_deal_id"`
	Status      string `json:"status"`
	// SaleDate is the deal's sale_date after the change: on a close it is the restamped close day,
	// which a planned sale's workflow clock follows. Absent on an event written before it was carried.
	SaleDate string `json:"sale_date"`
}

// dealStatusClosed is the sales ledger's closed-deal word (sales/domain.StatusDealClosed).
const dealStatusClosed = "Deal Closed"

// SaleStatusChangedWorkflowHandler cancels a sale's workflow when the deal is marked Deal Failed
// (maintainer decision 2026-09-25): the work a failed sale owed -- tag, load, gate pass, collect
// the balance -- will never be done, and an open card for it would read overdue forever. On Deal
// Closed a PLANNED sale's step clocks follow the restamped close date (2026-09-26). Any other
// status change leaves the workflow alone.
type SaleStatusChangedWorkflowHandler struct{ svc *Service }

// NewSaleStatusChangedWorkflowHandler constructs the canceller.
func NewSaleStatusChangedWorkflowHandler(svc *Service) *SaleStatusChangedWorkflowHandler {
	return &SaleStatusChangedWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*SaleStatusChangedWorkflowHandler)(nil)

// Register subscribes the status-change event.
func (h *SaleStatusChangedWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventSalesDealStatusChanged, h)
}

// HandleEvent cancels the failed deal's workflow; idempotent (a cancelled workflow stays so).
func (h *SaleStatusChangedWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p salesDealStatusChangedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	dealID := strings.TrimSpace(p.SalesDealID)
	if dealID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	switch {
	case strings.EqualFold(strings.TrimSpace(p.Status), dealStatusFailed):
		return h.svc.CancelSaleWorkflow(ctx, e.TenantID, dealID)
	case strings.EqualFold(strings.TrimSpace(p.Status), dealStatusClosed) && strings.TrimSpace(p.SaleDate) != "":
		return h.svc.ReanchorSaleWorkflow(ctx, e.TenantID, dealID, strings.TrimSpace(p.SaleDate))
	}
	return nil
}
