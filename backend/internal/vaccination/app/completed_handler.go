package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

const EventVaccinationCompleted = "vaccination.completed"

type VaccinationCompletedEvent struct {
	TenantID     string `json:"tenant_id,omitempty"`
	ObligationID string `json:"obligation_id"`
	Status       string `json:"status,omitempty"`
}

type BoosterContextReader interface {
	GetBoosterContext(ctx context.Context, tenantID, obligationID string) (versionID, scopeType, scopeID string, sequence int32, err error)
}

// VaccinationCompletedHandler schedules SM-7 follow-up doses from the durable vaccination.completed
// event, after the primary obligation is committed completed.
type VaccinationCompletedHandler struct {
	vacc    *Service
	obl     BoosterContextReader
	booster *BoosterService
}

func NewVaccinationCompletedHandler(vacc *Service, obl BoosterContextReader, booster *BoosterService) *VaccinationCompletedHandler {
	return &VaccinationCompletedHandler{vacc: vacc, obl: obl, booster: booster}
}

var _ eventbus.Handler = (*VaccinationCompletedHandler)(nil)

func (h *VaccinationCompletedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVaccinationCompleted, h)
}

func (h *VaccinationCompletedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if h == nil || h.vacc == nil || h.obl == nil || h.booster == nil {
		return fmt.Errorf("vaccination completed handler is not configured")
	}
	var p VaccinationCompletedEvent
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
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
	if p.Status != "" && p.Status != "completed" {
		return nil
	}
	completion, found, err := h.vacc.GetAcceptedCompletionForObligation(ctx, tenantID, obligationID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	versionID, scopeType, scopeID, sequence, err := h.obl.GetBoosterContext(ctx, tenantID, obligationID)
	if err != nil {
		return err
	}
	_, err = h.booster.ScheduleNextDose(ctx, ScheduleNextInput{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		GoatID:            completion.GoatID,
		ScopeType:         scopeType,
		ScopeID:           scopeID,
		PrevSequence:      sequence,
		// The obligation just completed IS the cause of the next cycle. It was already in
		// hand here and thrown away, which left the successor with nothing stable to be
		// identified by -- so a moved due date produced a second row instead of moving one.
		CompletedObligationID: obligationID,
		AdministeredAt:        completion.AdministeredAt,
	})
	return err
}
