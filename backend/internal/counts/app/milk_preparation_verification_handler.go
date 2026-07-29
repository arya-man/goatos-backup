package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

const (
	eventMilkPreparationVerdictApproved = "verification.verdict.approved"
	eventMilkPreparationVerdictRework   = "verification.verdict.rework"
)

type milkPreparationVerdictPayload struct {
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

// MilkPreparationVerificationHandler is the sole consumer that turns the verifier's one verdict
// into park-day preparation completion or rework. Duplicate verdict delivery is a store-level no-op.
type MilkPreparationVerificationHandler struct {
	store ports.MilkPreparationCompletionStore
}

func NewMilkPreparationVerificationHandler(store ports.MilkPreparationCompletionStore) *MilkPreparationVerificationHandler {
	return &MilkPreparationVerificationHandler{store: store}
}

var _ eventbus.Handler = (*MilkPreparationVerificationHandler)(nil)

func (h *MilkPreparationVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventMilkPreparationVerdictApproved, h)
	bus.Subscribe(eventMilkPreparationVerdictRework, h)
}

func (h *MilkPreparationVerificationHandler) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if event.Type != eventMilkPreparationVerdictApproved && event.Type != eventMilkPreparationVerdictRework {
		return nil
	}
	var payload milkPreparationVerdictPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}
	if payload.Source.Module != domain.VerificationModuleMilkPreparation ||
		payload.Source.RefType != domain.VerificationRefTypeMilkPreparation {
		return nil
	}
	if strings.TrimSpace(event.TenantID) == "" || strings.TrimSpace(payload.Source.RefID) == "" {
		return nil
	}
	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	command := domain.MilkPreparationVerdictCommand{
		TenantID: event.TenantID, CompletionID: strings.TrimSpace(payload.Source.RefID),
		VerifiedBy: strings.TrimSpace(payload.VerifiedBy), Reason: strings.TrimSpace(payload.Reason),
		TraceID: event.ID, OccurredAt: occurredAt,
	}
	if event.Type == eventMilkPreparationVerdictApproved {
		_, err := h.store.ApplyVerifiedMilkPreparation(ctx, command)
		return err
	}
	_, err := h.store.BounceMilkPreparationForRework(ctx, command)
	return err
}
