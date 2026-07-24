package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// EventGoatCreated is the event type that triggers per-goat SM-1 generation.
const EventGoatCreated = "goat.created"

// EventGoatStageChanged re-evaluates rules when age/stage classification changes.
const EventGoatStageChanged = "goat.stage_changed"

// EventGoatLocationChanged re-evaluates rules after a shed/park move because the goat's stage
// can be location-derived. Obligation re-scoping remains owned by the obligation handler. It also
// covers ICU/quarantine recovery: a goat leaving an ICU/quarantine location fires this event, so its
// held (deferred) obligations are reopened on recheck.
const EventGoatLocationChanged = "goat.location.changed"

// EventGoatHealthChanged re-evaluates rules after a goat's health status changes (e.g. sick →
// recovered) WITHOUT a stage/location move. Pure health recovery is not covered by stage/location
// events, so the recheck must consume this dedicated signal to reopen health-deferred obligations.
// The identity health-status mutation command durably emits this event through goat_identity_events
// and the outbox, so local and Pub/Sub delivery both drive the same recheck path.
const EventGoatHealthChanged = "goat.health.changed"

// EventGoatReproductiveChanged re-evaluates rules after a goat's reproductive status changes (e.g.
// non_pregnant → pregnant, or mother → milking). Pregnancy/lactation defer and post-delivery
// catch-up windows depend on reproductive_status plus breeding_date/last_delivery_date, so the
// recheck must consume this dedicated signal to reopen or re-hold obligations immediately. The
// identity reproductive-status mutation command durably emits this event through
// goat_identity_events and the outbox, so local and Pub/Sub delivery drive the same recheck path.
// Species-agnostic: goat and sheep both flow through the same per-goat recompute.
const EventGoatReproductiveChanged = "goat.reproductive.changed"

// EventGoatIdentityChanged re-evaluates rules after a goat's DOB or entry_date is corrected (B7:
// dynamic recompute). A DOB/entry-date update changes schedulePathForGoat's age-based routing and
// dueAt's birth_age/post_arrival anchors, so a corrected anchor must recompute exactly like a
// stage/location/health/reproductive change: cancel obligations that are no longer effective and
// reopen deferred ones once the blocking data changes. NOTE (dependency, not faked): as of this
// change there is no identity mutation command that corrects DOB/entry_date on an existing goat
// (MoveGoat/ExitGoat/StageGoat/HealthGoat/ReproductiveGoat exist; no equivalent IdentityGoat command
// emits this event yet) — this constant and its subscription only ensure vaccination generation is
// ready to recompute the moment that identity-side command ships. See the runbook's B7 dependency
// note; do not wire a synthetic producer here.
const EventGoatIdentityChanged = "goat.identity.changed"

// EventProtocolVersionPublished is the durable protocol publish event. Vaccination consumes this
// event to generate existing-cohort obligations after a source-backed version is published.
const EventProtocolVersionPublished = "protocol.version.published"

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
//
// BUG-017: a procured animal's `goat.created` payload carries the supplier-attested pre-arrival
// vaccination card in `trusted_vaccination_history`. Those claims are reviewed and durably
// persisted as accepted/rejected pre-arrival history FIRST, so generation — which is already
// correct once history exists — sees the accepted anchors instead of scheduling the animal from
// scratch and re-injecting doses it already received. Persisting (rather than passing the claims
// through one generation run) is what keeps the suppression alive across the later
// stage/location/health recheck passes.
func (h *GoatCreatedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	asOf := e.OccurredAt
	if asOf.IsZero() {
		asOf = time.Now()
	}
	if err := h.gen.ingestPreArrivalHistory(ctx, e.TenantID, e.Key, e.ID, e.Payload, asOf); err != nil {
		return err
	}
	_, err := h.gen.GenerateForGoat(ctx, e.TenantID, e.Key, asOf)
	return err
}

// GoatRecheckHandler runs the same per-goat generation after stage/location changes. It keeps
// goat.created, goat.stage_changed, and location-derived stage changes on one idempotent path.
type GoatRecheckHandler struct {
	gen *GenerationService
}

func NewGoatRecheckHandler(gen *GenerationService) *GoatRecheckHandler {
	return &GoatRecheckHandler{gen: gen}
}

var _ eventbus.Handler = (*GoatRecheckHandler)(nil)

func (h *GoatRecheckHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatStageChanged, h)
	bus.Subscribe(EventGoatLocationChanged, h)
	bus.Subscribe(EventGoatHealthChanged, h)
	bus.Subscribe(EventGoatReproductiveChanged, h)
	bus.Subscribe(EventGoatIdentityChanged, h)
}

func (h *GoatRecheckHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	asOf := e.OccurredAt
	if asOf.IsZero() {
		asOf = time.Now()
	}
	_, err := h.gen.generateForGoat(ctx, e.TenantID, e.Key, asOf, generationOptions{healthRecoveryAlign: true})
	return err
}

type protocolPublishedPayload struct {
	ProtocolVersionID string `json:"protocol_version_id"`
	Category          string `json:"category"`
}

// ProtocolPublishedHandler runs publish-triggered existing-cohort generation from the durable
// outbox/domain-event path. It is idempotent through GenerateForVersionWithRun.
type ProtocolPublishedHandler struct {
	gen *GenerationService
}

func NewProtocolPublishedHandler(gen *GenerationService) *ProtocolPublishedHandler {
	return &ProtocolPublishedHandler{gen: gen}
}

var _ eventbus.Handler = (*ProtocolPublishedHandler)(nil)

func (h *ProtocolPublishedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventProtocolVersionPublished, h)
}

func (h *ProtocolPublishedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p protocolPublishedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	versionID := p.ProtocolVersionID
	if versionID == "" {
		versionID = e.Key
	}
	if versionID == "" {
		return nil
	}
	if p.Category != "" && p.Category != "vaccination" {
		return nil
	}
	if p.Category == "" {
		version, err := h.gen.proto.GetVersion(ctx, e.TenantID, versionID)
		if err != nil {
			return err
		}
		if version.Category != "vaccination" {
			return nil
		}
	}
	asOf := e.OccurredAt
	if asOf.IsZero() {
		asOf = time.Now()
	}
	_, _, err := h.gen.GenerateForVersionWithRun(ctx, e.TenantID, versionID, asOf, "publish", versionID)
	return err
}
