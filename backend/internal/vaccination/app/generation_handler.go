package app

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// EventGoatCreated is the event type that triggers per-goat SM-1 generation.
const EventGoatCreated = "goat.created"

// GoatCreatedHandler runs event-driven SM-1: on goat.created it generates obligations for that goat
// across published vaccination versions. Idempotent (safe under at-least-once delivery). It is an
// eventbus.Handler, so the in-process bus and the future Pub/Sub consumer invoke the same code.
type GoatCreatedHandler struct {
	gen *GenerationService
}

// NewGoatCreatedHandler constructs the handler.
func NewGoatCreatedHandler(gen *GenerationService) *GoatCreatedHandler {
	return &GoatCreatedHandler{gen: gen}
}

var _ eventbus.Handler = (*GoatCreatedHandler)(nil)

// Register subscribes the handler to goat.created on a bus.
func (h *GoatCreatedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatCreated, h)
}

// HandleEvent generates for the goat identified by the event Key (goat_id) within e.TenantID.
func (h *GoatCreatedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	asOf := e.OccurredAt
	if asOf.IsZero() {
		asOf = time.Now()
	}
	_, err := h.gen.GenerateForGoat(ctx, e.TenantID, e.Key, asOf)
	return err
}
