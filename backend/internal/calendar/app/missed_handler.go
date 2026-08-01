package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

const (
	EventObligationMissed           = "obligation.missed"
	missedObligationEscalationLimit = 1
)

type obligationMissedPayload struct {
	TenantID     string `json:"tenant_id"`
	ObligationID string `json:"obligation_id"`
	Status       string `json:"status"`
}

// MissedWorkNotifier is the seam that turns a missed obligation into a message someone actually
// receives. It is an interface here (implemented in internal/notificationbridge) because resolving
// devices is workforce truth and calendar must not depend on the workforce module.
//
// Why it exists: a missed obligation is the exact failure the operational kernel is built to catch,
// and until this seam it was the ONE lifecycle state that told nobody anything -- the handler's only
// terminal action was opening an escalation row, which is a screen state, not a message. The work
// went missed in silence.
type MissedWorkNotifier interface {
	// NotifyObligationMissed pushes the missed work DOWN to the operator whose work it was and UP to
	// the park head and the owning module's director. Implementations must be idempotent: this
	// handler is on an at-least-once durable bus.
	NotifyObligationMissed(ctx context.Context, tenantID, obligationID string) error
}

type ObligationMissedHandler struct {
	calendar *Service
	notifier MissedWorkNotifier
}

func NewObligationMissedHandler(calendar *Service) *ObligationMissedHandler {
	return &ObligationMissedHandler{calendar: calendar}
}

// WithNotifier attaches the missed-work notifier. The durable buses
// (kernelstages.BuildDomainBus and cmd/domain-event-consumer) MUST pass one; without it a missed
// obligation still escalates but nobody is told, which is the defect this seam closes.
func (h *ObligationMissedHandler) WithNotifier(notifier MissedWorkNotifier) *ObligationMissedHandler {
	h.notifier = notifier
	return h
}

var _ eventbus.Handler = (*ObligationMissedHandler)(nil)

func (h *ObligationMissedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventObligationMissed, h)
}

func (h *ObligationMissedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if h == nil || h.calendar == nil {
		return fmt.Errorf("calendar obligation missed handler is not configured")
	}
	var p obligationMissedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.Status != "" && p.Status != "missed" {
		return nil
	}
	tenantID := e.TenantID
	if tenantID == "" {
		tenantID = p.TenantID
	}
	obligationID := p.ObligationID
	if obligationID == "" {
		obligationID = e.Key
	}
	if tenantID == "" || obligationID == "" {
		return nil
	}
	now := h.calendar.now().UTC()
	// 5k-50k envelope: no projection to refresh -- canonical reads are never stale relative to the
	// canonical write, so the missed obligation is already visible to the escalation sweep below.
	// A durable missed event is already overdue, so the first escalation opens
	// immediately while later levels keep the service's configured thresholds.
	if _, err := h.calendar.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     tenantID,
		ObligationID: obligationID,
		Limit:        missedObligationEscalationLimit,
		Now:          now,
	}); err != nil {
		return err
	}
	// Opening an escalation changes a screen; it does not tell anyone. Push the miss to the people
	// who have to act on it. Returning the error keeps the event on the durable bus for redelivery;
	// the notification write is idempotent per (event, device), so a retry never duplicates.
	if h.notifier != nil {
		return h.notifier.NotifyObligationMissed(ctx, tenantID, obligationID)
	}
	return nil
}
