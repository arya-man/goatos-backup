package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

const EventObligationMissed = "obligation.missed"

type obligationMissedPayload struct {
	TenantID     string `json:"tenant_id"`
	ObligationID string `json:"obligation_id"`
	Status       string `json:"status"`
}

type ObligationMissedHandler struct {
	calendar *Service
}

func NewObligationMissedHandler(calendar *Service) *ObligationMissedHandler {
	return &ObligationMissedHandler{calendar: calendar}
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
	_, err := h.calendar.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID: tenantID,
		Limit:    100,
		Now:      h.calendar.now().UTC(),
	})
	return err
}
