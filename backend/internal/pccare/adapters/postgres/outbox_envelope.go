package postgres

import (
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// One place that builds a PC Care domain-event envelope (feeddirection outbox_envelope clone —
// see that file for the incident that makes a single builder mandatory: hand-built maps drifted
// from contracts/jsonschema/domain-event-envelope.schema.json and every event was rejected by
// the relay's validator AFTER the transaction committed, invisibly).
//
// Schema constraints encoded here so a caller cannot drift: subject_type is "location" (the
// enum has no "shed" member), and a system actor is "system_rule", never "system".
type pcCareEventEnvelope struct {
	EventID        string
	EventType      string
	SchemaVersion  string
	SchemaRef      string
	AggregateType  string
	AggregateID    string
	IdempotencyKey string
	TenantID       string
	ParkID         string
	ShedID         string
	// ActorID is the person the event is attributed to, empty for a system transition.
	ActorID    string
	OccurredAt time.Time
	Payload    map[string]any
	TraceID    string
}

// build returns the complete envelope map. Every field the schema requires is present.
func (e pcCareEventEnvelope) build() map[string]any {
	occurred := e.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	stamp := occurred.UTC().Format("2006-01-02T15:04:05.000000Z")
	recorded := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")

	actor := map[string]any{"actor_type": "system_rule", "actor_id": nil}
	if e.ActorID != "" {
		actor = map[string]any{"actor_type": "human", "actor_id": e.ActorID}
	}

	scope := map[string]any{"tenant_id": e.TenantID}
	if e.ParkID != "" {
		scope["park_id"] = e.ParkID
	}
	if e.ShedID != "" {
		scope["shed_id"] = e.ShedID
	}

	return map[string]any{
		"event_id":        e.EventID,
		"event_type":      e.EventType,
		"schema_version":  e.SchemaVersion,
		"schema_ref":      e.SchemaRef,
		"aggregate_type":  e.AggregateType,
		"aggregate_id":    e.AggregateID,
		"occurred_at":     stamp,
		"recorded_at":     recorded,
		"producer":        map[string]any{"service": "goatos-api", "module": "pccare", "version": nil},
		"idempotency_key": e.IdempotencyKey,
		"actor":           actor,
		"subject_type":    "location",
		"subject_id":      e.ShedID,
		// The task's proof videos live on the verification item that approved it; the event
		// itself carries none. An empty array is the honest value and is REQUIRED by the schema.
		"evidence_refs":    []any{},
		"visibility_scope": scope,
		"payload":          e.Payload,
		"trace_id":         e.TraceID,
	}
}

// businessInstant renders a business date string ("2026-08-21") as an instant at the start of
// that India business day, for use as OccurredAt when the event's fact is dated rather than
// timestamped.
func businessInstant(businessDate string) time.Time {
	day, err := time.ParseInLocation("2006-01-02", businessDate, biztime.DefaultLocation())
	if err != nil {
		return time.Time{}
	}
	return day
}
