package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// EventGoatStageChanged is identity's per-animal stage event, published by every path that writes
// a management_stage (a completed shifting, the admin re-tag, a pen reclassify).
const EventGoatStageChanged = "goat.stage_changed"

// litterGoatPayload is the subset of goat.stage_changed / goat.exited the litter consumer reads.
type litterGoatPayload struct {
	GoatID string `json:"goat_id"`
}

// LitterShiftWorkflowHandler completes a litter's kid-shift steps from the herd register (KID
// STAGE SHIFT TASKS, maintainer decision 2026-09-30, docs/decisions/kid-stage-shift-tasks.md).
//
//	goat.stage_changed -> a kid moved: the step whose target the whole litter now holds completes,
//	                      and the next step's "N days after" clock starts from this instant
//	goat.exited        -> a kid left the farm: the remaining kids may now all be there; a litter
//	                      with no live kid left owes no move and its workflow is canceled
//
// Idempotent: a redelivered event re-judges the same register and changes nothing.
type LitterShiftWorkflowHandler struct{ svc *Service }

// NewLitterShiftWorkflowHandler constructs the consumer.
func NewLitterShiftWorkflowHandler(svc *Service) *LitterShiftWorkflowHandler {
	return &LitterShiftWorkflowHandler{svc: svc}
}

var _ eventbus.Handler = (*LitterShiftWorkflowHandler)(nil)

// Register subscribes both events.
func (h *LitterShiftWorkflowHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatStageChanged, h)
	bus.Subscribe(EventGoatExited, h)
}

// HandleEvent re-judges the litter of the goat the event names.
func (h *LitterShiftWorkflowHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p litterGoatPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	goatID := strings.TrimSpace(p.GoatID)
	if goatID == "" {
		goatID = strings.TrimSpace(e.Key)
	}
	if goatID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	at := e.OccurredAt
	if at.IsZero() {
		at = time.Now()
	}
	return h.svc.ReconcileLitterShift(ctx, e.TenantID, goatID, at.UTC())
}
