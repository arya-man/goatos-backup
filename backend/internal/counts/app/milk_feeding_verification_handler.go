package app

import (
	"context"
	"encoding/json"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"strings"
	"time"
)

type milkFeedingVerdictPayload struct {
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}
type MilkFeedingVerificationHandler struct{ store ports.MilkFeedingStore }

func NewMilkFeedingVerificationHandler(store ports.MilkFeedingStore) *MilkFeedingVerificationHandler {
	return &MilkFeedingVerificationHandler{store: store}
}

var _ eventbus.Handler = (*MilkFeedingVerificationHandler)(nil)

func (h *MilkFeedingVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventMilkPreparationVerdictApproved, h)
	bus.Subscribe(eventMilkPreparationVerdictRework, h)
}
func (h *MilkFeedingVerificationHandler) HandleEvent(ctx context.Context, event eventbus.Event) error {
	var p milkFeedingVerdictPayload
	if err := json.Unmarshal(event.Payload, &p); err != nil {
		return err
	}
	if p.Source.Module != domain.VerificationModuleMilkFeeding || p.Source.RefType != domain.VerificationRefTypeMilkFeeding || strings.TrimSpace(event.TenantID) == "" || strings.TrimSpace(p.Source.RefID) == "" {
		return nil
	}
	at := event.OccurredAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	cmd := domain.MilkFeedingVerdictCommand{TenantID: event.TenantID, CompletionID: strings.TrimSpace(p.Source.RefID), VerifiedBy: strings.TrimSpace(p.VerifiedBy), Reason: strings.TrimSpace(p.Reason), TraceID: event.ID, OccurredAt: at}
	if event.Type == eventMilkPreparationVerdictApproved {
		_, err := h.store.ApplyVerifiedMilkFeeding(ctx, cmd)
		return err
	}
	if event.Type == eventMilkPreparationVerdictRework {
		_, err := h.store.BounceMilkFeedingForRework(ctx, cmd)
		return err
	}
	return nil
}
