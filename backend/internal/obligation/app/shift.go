package app

import (
	"context"
	"encoding/json"

	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// EventGoatShifted is the event type (goat moved sheds/parks) that triggers SM-2 re-scoping.
const EventGoatShifted = "goat.shifted"

// ShiftPayload is the goat.shifted event body: the goat's new scope.
type ShiftPayload struct {
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
}

// GoatShiftedHandler runs SM-2 (minimal): on goat.shifted it re-scopes the goat's open, unbatched
// obligations to the new scope so the next drive picks them up at the right shed. Completed work is
// never touched. Idempotent (safe under at-least-once delivery). eventbus.Handler → same code for
// in-process and a future Pub/Sub consumer.
type GoatShiftedHandler struct {
	repo ports.Repository
}

// NewGoatShiftedHandler constructs the handler.
func NewGoatShiftedHandler(repo ports.Repository) *GoatShiftedHandler {
	return &GoatShiftedHandler{repo: repo}
}

var _ eventbus.Handler = (*GoatShiftedHandler)(nil)

// Register subscribes the handler to goat.shifted on a bus.
func (h *GoatShiftedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatShifted, h)
}

// HandleEvent re-scopes the shifted goat's open obligations (event Key = goat_id, Payload = scope).
// A missing scope_id is a no-op (nothing safe to move).
func (h *GoatShiftedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p ShiftPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.ScopeType == "" || p.ScopeID == "" {
		return nil
	}
	_, err := h.repo.ReScopeOpenForGoat(ctx, e.TenantID, e.Key, p.ScopeType, p.ScopeID)
	return err
}
