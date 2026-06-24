package postgres

import (
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
)

const (
	eventSchemaVersion = "1.0.0"
	eventSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
)

type eventEnvelope struct {
	EventID         string               `json:"event_id"`
	EventType       string               `json:"event_type"`
	SchemaVersion   string               `json:"schema_version"`
	SchemaRef       string               `json:"schema_ref"`
	AggregateType   string               `json:"aggregate_type"`
	AggregateID     string               `json:"aggregate_id"`
	OccurredAt      string               `json:"occurred_at"`
	RecordedAt      string               `json:"recorded_at"`
	Producer        eventProducer        `json:"producer"`
	IdempotencyKey  string               `json:"idempotency_key"`
	Actor           eventActor           `json:"actor"`
	SubjectType     string               `json:"subject_type"`
	SubjectID       string               `json:"subject_id"`
	VisibilityScope domain.LocationScope `json:"visibility_scope"`
	EvidenceRefs    []domain.EvidenceRef `json:"evidence_refs"`
	Payload         any                  `json:"payload"`
	TraceID         string               `json:"trace_id"`
}

type eventProducer struct {
	Service string  `json:"service"`
	Module  string  `json:"module"`
	Version *string `json:"version"`
}

type eventActor struct {
	ActorType string  `json:"actor_type"`
	ActorID   *string `json:"actor_id"`
	ActorRef  *string `json:"actor_ref"`
}

func decisionSummaryFromInsertRow(row identitydb.InsertIdentityDecisionRow) domain.DecisionRecordSummary {
	return domain.DecisionRecordSummary{
		DecisionID:     row.DecisionID,
		DecisionType:   row.DecisionType,
		DecisionResult: row.DecisionResult,
		DecisionState:  row.DecisionState,
		PolicyVersion:  row.PolicyVersion,
		CreatedAt:      row.CreatedAt.Time,
	}
}

func nullableUUID(value *string) pgtype.UUID {
	if value == nil || strings.TrimSpace(*value) == "" {
		return pgtype.UUID{}
	}
	out, err := uuidParam(*value)
	if err != nil {
		return pgtype.UUID{}
	}
	return out
}

func nullableText(value *string) pgtype.Text {
	if value == nil || strings.TrimSpace(*value) == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: strings.TrimSpace(*value), Valid: true}
}

func scopeType(scope domain.LocationScope) string {
	switch {
	case scope.CohortID != nil:
		return "cohort"
	case scope.ShedID != nil:
		return "shed"
	case scope.ParkID != nil:
		return "park"
	case scope.FarmID != nil:
		return "farm"
	default:
		return ""
	}
}

func scopeID(scope domain.LocationScope) *string {
	switch {
	case scope.CohortID != nil:
		return scope.CohortID
	case scope.ShedID != nil:
		return scope.ShedID
	case scope.ParkID != nil:
		return scope.ParkID
	case scope.FarmID != nil:
		return scope.FarmID
	default:
		return nil
	}
}
