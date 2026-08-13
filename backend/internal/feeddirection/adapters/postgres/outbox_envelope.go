package postgres

import (
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// One place that builds a feed domain-event envelope, because three copies of it were wrong in the
// same way.
//
// THE DEFECT THIS FIXES. contracts/jsonschema/domain-event-envelope.schema.json requires seventeen
// fields and sets additionalProperties:false. All three feeddirection producers --
// feed.packing.completed, feed.distribution.completed and the inert feed.direction.completed --
// hand-built a map carrying only eleven of them, omitting occurred_at, recorded_at, actor,
// visibility_scope and evidence_refs, and spelling `producer` as a bare string where the schema
// wants an object.
//
// The failure mode is the reason this is worth a file of its own: the INSERT into outbox_messages
// succeeds, the transaction commits, the operator's screen flips to completed, and the event is
// only rejected LATER, by the relay's envelope validator, as `invalid_event_envelope`. It never
// reaches a consumer, and nothing on any screen says so. A feed packing approval therefore looked
// entirely healthy while emitting an event that could not be delivered.
//
// Hand-building the map is what allowed three independent copies to drift from the schema and stay
// wrong. Callers now pass only what genuinely differs per event; every schema-mandated field is
// filled here, once.
type feedEventEnvelope struct {
	EventID       string
	EventType     string
	SchemaVersion string
	SchemaRef     string
	AggregateType string
	AggregateID   string
	// IdempotencyKey is the event's stable identity for redelivery.
	IdempotencyKey string
	// There is deliberately NO SubjectType field. Every feed event is ABOUT a shed, and a shed's
	// schema subject_type is "location" -- the enum has no "shed" member. Leaving it as a per-caller
	// string meant three call sites each spelling it, and all three spelled it "shed"; a
	// mutation test proved a single call site could drift back without any test noticing, because a
	// builder test constructs its own value. The subject is now derived from ShedID below, so the
	// mistake is unavailable rather than merely discouraged -- the same reasoning as platform/oploc.
	// TenantID/ParkID/ShedID become the visibility scope a consumer filters on.
	TenantID string
	ParkID   string
	ShedID   string
	// ActorID is the person the event is attributed to, empty for a system transition. An empty id
	// is emitted as actor_type "system" rather than as a human with no name.
	ActorID string
	// OccurredAt is when the business fact happened. Zero falls back to now; callers that have a real
	// instant should pass it, because this is the field every downstream time bucket reads.
	OccurredAt time.Time
	Payload    map[string]any
	TraceID    string
}

// build returns the complete envelope map. Every field the schema requires is present, so a caller
// cannot forget one.
func (e feedEventEnvelope) build() map[string]any {
	occurred := e.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	// RFC3339 in UTC with microseconds, matching the format the identity producers emit and the
	// schema's date-time constraint accepts.
	stamp := occurred.UTC().Format("2006-01-02T15:04:05.000000Z")
	recorded := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")

	// "system_rule", NOT "system". The schema's actor_type enum is human | system_rule | import_job |
	// device | ai_proposal, and it sets additionalProperties:false -- a plausible-sounding value that
	// is not in the enum fails validation exactly as a missing field does.
	actor := map[string]any{"actor_type": "system_rule", "actor_id": nil}
	if e.ActorID != "" {
		actor = map[string]any{"actor_type": "human", "actor_id": e.ActorID}
	}

	// jsonb_strip_nulls equivalent: an absent park or shed is OMITTED rather than sent as an empty
	// string, so a consumer filtering on scope cannot match "" against a real id.
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
		"producer":        map[string]any{"service": "goatos-api", "module": "feeddirection", "version": nil},
		"idempotency_key": e.IdempotencyKey,
		"actor":           actor,
		"subject_type":    "location",
		"subject_id":      e.ShedID,
		// The feed events carry no media of their own: the proof video lives on the verification item
		// that approved the completion. An empty array is the honest value and is REQUIRED -- the
		// field being absent is what fails validation.
		"evidence_refs":    []any{},
		"visibility_scope": scope,
		"payload":          e.Payload,
		"trace_id":         e.TraceID,
	}
}

// businessInstant renders a business date string ("2026-07-30") as an instant at the start of that
// India business day, for use as OccurredAt when the event's fact is dated rather than timestamped.
func businessInstant(businessDate string) time.Time {
	day, err := time.ParseInLocation("2006-01-02", businessDate, biztime.DefaultLocation())
	if err != nil {
		// An unparseable date is not worth failing a commit over; the caller's fallback is now.
		return time.Time{}
	}
	return day
}
