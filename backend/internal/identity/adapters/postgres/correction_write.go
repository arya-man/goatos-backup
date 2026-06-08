package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	correctionResultType = "identity_correction_request"
	correctionEventType  = "identity.correction_request.created"
	correctionAggregate  = "correction_request"
	correctionTopic      = "identity.events"
	eventSchemaVersion   = "1.0.0"
	eventSchemaRef       = "contracts/jsonschema/domain-event-envelope.schema.json"
)

func (r *Repository) CreateCorrectionRequest(ctx context.Context, cmd ports.CreateCorrectionRequestCommand) (*ports.CreateCorrectionRequestResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	actorUUID, err := uuidParam(cmd.ActorID)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)

	_, err = qtx.InsertIdempotencyStarted(ctx, identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TenantID:       tenantUUID,
		Scope:          cmd.IdempotencyScope,
		RequestHash:    cmd.RequestHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return r.replayCorrectionRequest(ctx, qtx, tenantUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	}
	if err != nil {
		return nil, err
	}

	if cmd.GoatID != nil {
		goatUUID, err := uuidParam(*cmd.GoatID)
		if err != nil {
			return nil, err
		}
		ok, err := qtx.GoatBelongsToTenant(ctx, identitydb.GoatBelongsToTenantParams{
			TenantID: tenantUUID,
			GoatID:   goatUUID,
		})
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ports.ErrNotFound
		}
	}

	evidence, err := json.Marshal(correctionEvidence{EvidenceRefs: cmd.EvidenceRefs})
	if err != nil {
		return nil, err
	}

	row, err := qtx.CreateCorrectionRequest(ctx, identitydb.CreateCorrectionRequestParams{
		TenantID:        tenantUUID,
		RequestType:     cmd.RequestType,
		GoatID:          nullableUUID(cmd.GoatID),
		IdentifierType:  nullableText(cmd.IdentifierType),
		IdentifierValue: nullableText(cmd.IdentifierValue),
		FarmID:          nullableUUID(cmd.LocationScope.FarmID),
		ParkID:          nullableUUID(cmd.LocationScope.ParkID),
		ShedID:          nullableUUID(cmd.LocationScope.ShedID),
		CohortID:        nullableUUID(cmd.LocationScope.CohortID),
		Description:     cmd.Description,
		Evidence:        evidence,
		RequestedBy:     actorUUID,
	})
	if err != nil {
		return nil, err
	}
	correction, err := correctionRequestFromCreateRow(row)
	if err != nil {
		return nil, err
	}

	afterState, err := json.Marshal(correction)
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(map[string]any{
		"actor_id":               cmd.ActorID,
		"idempotency_key":        cmd.StoredIdempotencyKey,
		"client_idempotency_key": cmd.ClientIdempotencyKey,
		"idempotency_scope":      cmd.IdempotencyScope,
		"trace_id":               cmd.TraceID,
	})
	if err != nil {
		return nil, err
	}
	correctionUUID, err := uuidParam(correction.CorrectionRequestID)
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertAuditLog(ctx, identitydb.InsertAuditLogParams{
		TenantID:     tenantUUID,
		ActorID:      actorUUID,
		Action:       correctionEventType,
		ResourceType: correctionAggregate,
		ResourceID:   correctionUUID,
		ScopeType:    nullableText(nonEmptyStringPtr(scopeType(correction.LocationScope))),
		ScopeID:      nullableUUID(scopeID(correction.LocationScope)),
		AfterState:   afterState,
		Metadata:     metadata,
		TraceID:      nullableText(nonEmptyStringPtr(cmd.TraceID)),
	}); err != nil {
		return nil, err
	}

	if r.afterAuditHook != nil {
		if err := r.afterAuditHook(ctx); err != nil {
			return nil, err
		}
	}

	eventID, err := qtx.NewUUID(ctx)
	if err != nil {
		return nil, err
	}
	eventUUID, err := uuidParam(eventID)
	if err != nil {
		return nil, err
	}
	envelope, err := correctionCreatedEnvelope(cmd, correction, eventID)
	if err != nil {
		return nil, err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":               cmd.ActorID,
		"client_idempotency_key": cmd.ClientIdempotencyKey,
		"trace_id":               cmd.TraceID,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertOutboxMessage(ctx, identitydb.InsertOutboxMessageParams{
		TenantID:       tenantUUID,
		EventID:        eventUUID,
		EventType:      correctionEventType,
		SchemaVersion:  eventSchemaVersion,
		AggregateType:  correctionAggregate,
		AggregateID:    correctionUUID,
		Topic:          correctionTopic,
		Payload:        envelope,
		Headers:        headers,
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TraceID:        nullableText(nonEmptyStringPtr(cmd.TraceID)),
	}); err != nil {
		return nil, err
	}

	if err := qtx.CompleteIdempotencyKey(ctx, identitydb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(correctionResultType),
		ResultID:       correctionUUID,
		IdempotencyKey: cmd.StoredIdempotencyKey,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	return &ports.CreateCorrectionRequestResult{
		CorrectionRequest: correction,
		Replayed:          false,
	}, nil
}

func (r *Repository) replayCorrectionRequest(ctx context.Context, qtx *identitydb.Queries, tenantUUID pgtype.UUID, key string, requestHash string) (*ports.CreateCorrectionRequestResult, error) {
	idempotency, err := qtx.GetIdempotencyKey(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrIdempotencyPending
	}
	if err != nil {
		return nil, err
	}
	if idempotency.RequestHash != requestHash {
		return nil, ports.ErrIdempotencyConflict
	}
	if idempotency.Status != "completed" || idempotency.ResultType != correctionResultType || strings.TrimSpace(idempotency.ResultID) == "" {
		return nil, ports.ErrIdempotencyPending
	}
	resultUUID, err := uuidParam(idempotency.ResultID)
	if err != nil {
		return nil, err
	}
	row, err := qtx.GetCorrectionRequestByID(ctx, identitydb.GetCorrectionRequestByIDParams{
		TenantID:            tenantUUID,
		CorrectionRequestID: resultUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	correction, err := correctionRequestFromGetRow(row)
	if err != nil {
		return nil, err
	}
	firstResultID := idempotency.ResultID
	return &ports.CreateCorrectionRequestResult{
		CorrectionRequest: correction,
		Replayed:          true,
		FirstResultID:     &firstResultID,
	}, nil
}

type correctionEvidence struct {
	EvidenceRefs []domain.EvidenceRef `json:"evidence_refs"`
}

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

func correctionCreatedEnvelope(cmd ports.CreateCorrectionRequestCommand, correction domain.CorrectionRequest, eventID string) ([]byte, error) {
	subjectType := correctionAggregate
	subjectID := correction.CorrectionRequestID
	if correction.GoatID != nil {
		subjectType = "goat"
		subjectID = *correction.GoatID
	}
	visibility := correction.LocationScope
	envelope := eventEnvelope{
		EventID:        eventID,
		EventType:      correctionEventType,
		SchemaVersion:  eventSchemaVersion,
		SchemaRef:      eventSchemaRef,
		AggregateType:  correctionAggregate,
		AggregateID:    correction.CorrectionRequestID,
		OccurredAt:     correction.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		RecordedAt:     correction.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		Producer:       eventProducer{Service: "goatos-api", Module: "identity"},
		IdempotencyKey: cmd.StoredIdempotencyKey,
		Actor:          eventActor{ActorType: "human", ActorID: &cmd.ActorID},
		SubjectType:    subjectType,
		SubjectID:      subjectID,
		VisibilityScope: domain.LocationScope{
			FarmID:   visibility.FarmID,
			ParkID:   visibility.ParkID,
			ShedID:   visibility.ShedID,
			CohortID: visibility.CohortID,
		},
		EvidenceRefs: correction.EvidenceRefs,
		Payload: map[string]any{
			"correction_request_id": correction.CorrectionRequestID,
			"request_type":          correction.RequestType,
			"state":                 correction.State,
			"goat_id":               correction.GoatID,
			"identifier_type":       correction.IdentifierType,
			"identifier_value":      correction.IdentifierValue,
			"location_scope":        correction.LocationScope,
			"description":           correction.Description,
		},
		TraceID: cmd.TraceID,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}

	var withTenant map[string]any
	if err := json.Unmarshal(payload, &withTenant); err != nil {
		return nil, err
	}
	visibilityScope, ok := withTenant["visibility_scope"].(map[string]any)
	if !ok {
		visibilityScope = map[string]any{}
	}
	visibilityScope["tenant_id"] = cmd.TenantID
	withTenant["visibility_scope"] = visibilityScope
	return json.Marshal(withTenant)
}

func correctionRequestFromCreateRow(row identitydb.CreateCorrectionRequestRow) (domain.CorrectionRequest, error) {
	return correctionRequestFromFields(
		row.CorrectionRequestID,
		row.RequestType,
		row.State,
		row.GoatID,
		row.IdentifierType,
		row.IdentifierValue,
		row.FarmID,
		row.ParkID,
		row.ShedID,
		row.CohortID,
		row.Description,
		row.Evidence,
		row.CreatedAt,
		row.ResolvedAt,
	)
}

func correctionRequestFromGetRow(row identitydb.GetCorrectionRequestByIDRow) (domain.CorrectionRequest, error) {
	return correctionRequestFromFields(
		row.CorrectionRequestID,
		row.RequestType,
		row.State,
		row.GoatID,
		row.IdentifierType,
		row.IdentifierValue,
		row.FarmID,
		row.ParkID,
		row.ShedID,
		row.CohortID,
		row.Description,
		row.Evidence,
		row.CreatedAt,
		row.ResolvedAt,
	)
}

func correctionRequestFromFields(correctionID, requestType, state, goatID string, identifierType pgtype.Text, identifierValue pgtype.Text, farmID, parkID, shedID, cohortID, description string, evidenceBytes []byte, createdAt pgtype.Timestamptz, resolvedAt pgtype.Timestamptz) (domain.CorrectionRequest, error) {
	var evidence correctionEvidence
	if len(evidenceBytes) > 0 {
		if err := json.Unmarshal(evidenceBytes, &evidence); err != nil {
			return domain.CorrectionRequest{}, err
		}
	}
	created := createdAt.Time
	var resolved *time.Time
	if resolvedAt.Valid {
		resolved = &resolvedAt.Time
	}
	return domain.CorrectionRequest{
		CorrectionRequestID: correctionID,
		RequestType:         requestType,
		State:               state,
		GoatID:              nonEmptyStringPtr(goatID),
		IdentifierType:      pgTextPtr(identifierType),
		IdentifierValue:     pgTextPtr(identifierValue),
		LocationScope: domain.LocationScope{
			FarmID:   nonEmptyStringPtr(farmID),
			ParkID:   nonEmptyStringPtr(parkID),
			ShedID:   nonEmptyStringPtr(shedID),
			CohortID: nonEmptyStringPtr(cohortID),
		},
		Description:  description,
		EvidenceRefs: evidence.EvidenceRefs,
		CreatedAt:    created,
		ResolvedAt:   resolved,
	}, nil
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
