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

// MilkPreparationUHTRecorder receives the verified UHT-milk consumption fact of
// an approved preparation. It is the feed stock ledger's seam (maintainer
// decision 2026-08-22): the eventwiring composition implements it with the
// feeddirection repository's RecordExternalConsumption, the same
// consumer-forwards-to-producer-service shape the verdict measurement appliers
// use — counts never writes a feed table and feed never reads a counts table.
type MilkPreparationUHTRecorder interface {
	RecordVerifiedUHTConsumption(ctx context.Context, in domain.MilkPreparationUHTConsumption) error
}

// MilkPreparationVerificationHandler is the sole consumer that turns the verifier's one verdict
// into shed-day preparation completion or rework. Duplicate verdict delivery is a store-level no-op.
type MilkPreparationVerificationHandler struct {
	store ports.MilkPreparationCompletionStore
	// uhtRecorder may be nil (a bus built without the feed module still applies
	// verdicts exactly as before — recording consumption is downstream fan-out,
	// never a gate on the verdict itself).
	uhtRecorder MilkPreparationUHTRecorder
}

func NewMilkPreparationVerificationHandler(store ports.MilkPreparationCompletionStore) *MilkPreparationVerificationHandler {
	return &MilkPreparationVerificationHandler{store: store}
}

// WithUHTRecorder attaches the feed stock recorder; returns the handler for chaining.
func (h *MilkPreparationVerificationHandler) WithUHTRecorder(rec MilkPreparationUHTRecorder) *MilkPreparationVerificationHandler {
	h.uhtRecorder = rec
	return h
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
		if _, err := h.store.ApplyVerifiedMilkPreparation(ctx, command); err != nil {
			return err
		}
		return h.recordUHTConsumption(ctx, command.TenantID, command.CompletionID)
	}
	_, err := h.store.BounceMilkPreparationForRework(ctx, command)
	return err
}

// recordUHTConsumption forwards the approved preparation's UHT-milk litres to
// the feed stock ledger. It runs on EVERY approve delivery — duplicates
// included, because ApplyVerifiedMilkPreparation treats a redelivered verdict
// as a no-op replay — so a recorder failure surfaces as a handler error, the
// at-least-once bus redelivers, and the idempotent upsert converges.
func (h *MilkPreparationVerificationHandler) recordUHTConsumption(ctx context.Context, tenantID, completionID string) error {
	if h.uhtRecorder == nil {
		return nil
	}
	consumption, ok, err := h.store.VerifiedUHTConsumption(ctx, tenantID, completionID)
	if err != nil {
		return err
	}
	if !ok || consumption.UHTMilkQuantityLitres <= 0 {
		// Not completed (stale duplicate of a reworked row) or a legacy attempt
		// submitted before the answers carried litres — nothing to record.
		return nil
	}
	return h.uhtRecorder.RecordVerifiedUHTConsumption(ctx, consumption)
}
