package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// EventGoatExited is the event type (death/sale/transfer) that triggers SM-3 cancellation.
const EventGoatExited = "goat.exited"

// GoatExitedHandler runs SM-3: on goat.exited it cancels the goat's open obligations. Idempotent
// (safe under at-least-once delivery). eventbus.Handler → same code for in-process and Pub/Sub.
type GoatExitedHandler struct {
	repo ports.Repository
}

// NewGoatExitedHandler constructs the handler.
func NewGoatExitedHandler(repo ports.Repository) *GoatExitedHandler {
	return &GoatExitedHandler{repo: repo}
}

var _ eventbus.Handler = (*GoatExitedHandler)(nil)

// Register subscribes the handler to goat.exited on a bus.
func (h *GoatExitedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatExited, h)
}

// HandleEvent cancels open obligations for the exited goat (event Key = goat_id).
func (h *GoatExitedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	_, err := h.repo.CancelOpenForGoat(ctx, e.TenantID, e.Key, "ineligible_after_exit")
	return err
}
