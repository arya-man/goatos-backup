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
	EventSalesDealRecorded = "sales.deal.recorded"
	EventGoatSaleAllocated = "goat.sale_allocated"
)

// salesDealRecordedPayload is the subset of the sales producer's payload the opener reads.
type salesDealRecordedPayload struct {
	SalesDealID string `json:"sales_deal_id"`
	ParkID      string `json:"park_id"`
	SaleDate    string `json:"sale_date"`
}

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
	eventAt := e.OccurredAt
	if eventAt.IsZero() {
		eventAt = time.Now()
	}
	_, err := h.svc.OpenSubjectWorkflow(ctx, OpenSubjectWorkflowInput{
		TenantID:     e.TenantID,
		TemplateKey:  domain.TemplateKeySalesDeal,
		SubjectRefID: dealID,
		EventAt:      eventAt,
		ParkID:       strings.TrimSpace(p.ParkID),
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
