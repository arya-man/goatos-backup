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
	correctionResultType           = "identity_correction_request"
	correctionCreatedEventType     = "identity.correction_request.created"
	correctionUpdatedEventType     = "identity.correction_request.updated"
	correctionAggregate            = "correction_request"
	correctionTopic                = "identity.events"
	correctionResolveDecisionType  = "resolve_correction_request"
	correctionResolvePolicyVersion = "phase1-manual-correction-review-v1"
	eventSchemaVersion             = "1.0.0"
	eventSchemaRef                 = "contracts/jsonschema/domain-event-envelope.schema.json"
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
		Action:       correctionCreatedEventType,
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
		EventType:      correctionCreatedEventType,
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

func (r *Repository) ResolveCorrectionRequest(ctx context.Context, cmd ports.ResolveCorrectionRequestCommand) (*ports.ResolveCorrectionRequestResult, error) {
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
	correctionUUID, err := uuidParam(cmd.CorrectionRequestID)
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
		return r.replayResolvedCorrectionRequest(ctx, qtx, tenantUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	}
	if err != nil {
		return nil, err
	}

	decisionID, err := qtx.NewUUID(ctx)
	if err != nil {
		return nil, err
	}
	decisionUUID, err := uuidParam(decisionID)
	if err != nil {
		return nil, err
	}
	decisionState, decisionResult := correctionDecisionMapping(cmd.TargetState)
	now := time.Now().UTC()
	approvedAt := pgtype.Timestamptz{}
	if decisionState == "approved" {
		approvedAt = pgtype.Timestamptz{Time: now, Valid: true}
	}
	decisionRecord, err := correctionDecisionRecordPayload(cmd, decisionID, decisionState, decisionResult, now)
	if err != nil {
		return nil, err
	}
	decisionEvidence, err := json.Marshal(correctionDecisionEvidence{
		EvidenceRefs:   cmd.EvidenceRefs,
		Reason:         cmd.Reason,
		DecisionRecord: decisionRecord,
	})
	if err != nil {
		return nil, err
	}
	decisionRow, err := qtx.InsertIdentityDecision(ctx, identitydb.InsertIdentityDecisionParams{
		DecisionID:     decisionUUID,
		TenantID:       tenantUUID,
		DecisionType:   correctionResolveDecisionType,
		DecisionResult: decisionResult,
		DecisionState:  decisionState,
		DecidedBy:      actorUUID,
		PolicyVersion:  correctionResolvePolicyVersion,
		ReviewerID:     actorUUID,
		Evidence:       decisionEvidence,
		CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
		ApprovedAt:     approvedAt,
		DecidedAt:      pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	decision := decisionSummaryFromInsertRow(decisionRow)

	terminal := correctionTargetTerminal(cmd.TargetState)
	resolvedAt := pgtype.Timestamptz{}
	if terminal {
		resolvedAt = pgtype.Timestamptz{Time: now, Valid: true}
	}
	updateRow, err := qtx.ResolveCorrectionRequest(ctx, identitydb.ResolveCorrectionRequestParams{
		State:               cmd.TargetState,
		ReviewerID:          actorUUID,
		DecisionID:          decisionUUID,
		Terminal:            terminal,
		ResolvedAt:          resolvedAt,
		TenantID:            tenantUUID,
		CorrectionRequestID: correctionUUID,
		RowVersion:          int32(cmd.RowVersion),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if conflictErr := resolveCorrectionConflict(ctx, qtx, tenantUUID, correctionUUID); conflictErr != nil {
			return nil, conflictErr
		}
		return nil, ports.ErrWriteConflict
	}
	if err != nil {
		return nil, err
	}
	correction, err := correctionRequestFromResolveRow(updateRow)
	if err != nil {
		return nil, err
	}

	afterState, err := json.Marshal(map[string]any{
		"correction_request": correction,
		"decision":           decision,
	})
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(map[string]any{
		"actor_id":               cmd.ActorID,
		"idempotency_key":        cmd.StoredIdempotencyKey,
		"client_idempotency_key": cmd.ClientIdempotencyKey,
		"idempotency_scope":      cmd.IdempotencyScope,
		"trace_id":               cmd.TraceID,
		"decision_id":            decision.DecisionID,
		"reason":                 cmd.Reason,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertAuditLog(ctx, identitydb.InsertAuditLogParams{
		TenantID:     tenantUUID,
		ActorID:      actorUUID,
		Action:       correctionUpdatedEventType,
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
	envelope, err := correctionUpdatedEnvelope(cmd, correction, decision, eventID)
	if err != nil {
		return nil, err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":               cmd.ActorID,
		"client_idempotency_key": cmd.ClientIdempotencyKey,
		"trace_id":               cmd.TraceID,
		"decision_id":            decision.DecisionID,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertOutboxMessage(ctx, identitydb.InsertOutboxMessageParams{
		TenantID:       tenantUUID,
		EventID:        eventUUID,
		EventType:      correctionUpdatedEventType,
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

	return &ports.ResolveCorrectionRequestResult{
		CorrectionRequest: correction,
		Decision:          decision,
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

func (r *Repository) replayResolvedCorrectionRequest(ctx context.Context, qtx *identitydb.Queries, tenantUUID pgtype.UUID, key string, requestHash string) (*ports.ResolveCorrectionRequestResult, error) {
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
	row, err := qtx.GetCorrectionRequestForResolve(ctx, identitydb.GetCorrectionRequestForResolveParams{
		TenantID:            tenantUUID,
		CorrectionRequestID: resultUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	correction, decisionID, err := correctionRequestFromResolveGetRow(row)
	if err != nil {
		return nil, err
	}
	if decisionID == nil {
		return nil, ports.ErrIdempotencyPending
	}
	decisionUUID, err := uuidParam(*decisionID)
	if err != nil {
		return nil, err
	}
	decisionRow, err := qtx.GetDecisionSummaryByID(ctx, identitydb.GetDecisionSummaryByIDParams{
		TenantID:   tenantUUID,
		DecisionID: decisionUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrIdempotencyPending
	}
	if err != nil {
		return nil, err
	}
	firstResultID := idempotency.ResultID
	return &ports.ResolveCorrectionRequestResult{
		CorrectionRequest: correction,
		Decision:          decisionSummaryFromGetRow(decisionRow),
		Replayed:          true,
		FirstResultID:     &firstResultID,
	}, nil
}

type correctionEvidence struct {
	EvidenceRefs []domain.EvidenceRef `json:"evidence_refs"`
}

type correctionDecisionEvidence struct {
	EvidenceRefs   []domain.EvidenceRef `json:"evidence_refs"`
	Reason         string               `json:"reason"`
	DecisionRecord json.RawMessage      `json:"decision_record"`
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
		EventType:      correctionCreatedEventType,
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

func correctionUpdatedEnvelope(cmd ports.ResolveCorrectionRequestCommand, correction domain.CorrectionRequest, decision domain.DecisionRecordSummary, eventID string) ([]byte, error) {
	recordedAt := correction.CreatedAt
	if correction.ResolvedAt != nil {
		recordedAt = *correction.ResolvedAt
	} else {
		recordedAt = decision.CreatedAt
	}
	envelope := eventEnvelope{
		EventID:        eventID,
		EventType:      correctionUpdatedEventType,
		SchemaVersion:  eventSchemaVersion,
		SchemaRef:      eventSchemaRef,
		AggregateType:  correctionAggregate,
		AggregateID:    correction.CorrectionRequestID,
		OccurredAt:     decision.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		RecordedAt:     recordedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		Producer:       eventProducer{Service: "goatos-api", Module: "identity"},
		IdempotencyKey: cmd.StoredIdempotencyKey,
		Actor:          eventActor{ActorType: "human", ActorID: &cmd.ActorID},
		SubjectType:    correctionAggregate,
		SubjectID:      correction.CorrectionRequestID,
		VisibilityScope: domain.LocationScope{
			FarmID:   correction.LocationScope.FarmID,
			ParkID:   correction.LocationScope.ParkID,
			ShedID:   correction.LocationScope.ShedID,
			CohortID: correction.LocationScope.CohortID,
		},
		EvidenceRefs: cmd.EvidenceRefs,
		Payload: map[string]any{
			"correction_request_id": correction.CorrectionRequestID,
			"state":                 correction.State,
			"row_version":           correction.RowVersion,
			"resolved_at":           correction.ResolvedAt,
			"decision_id":           decision.DecisionID,
			"decision_type":         decision.DecisionType,
			"decision_result":       decision.DecisionResult,
			"decision_state":        decision.DecisionState,
			"reason":                cmd.Reason,
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
		nil,
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
		nil,
		row.CreatedAt,
		row.ResolvedAt,
	)
}

func correctionRequestFromResolveRow(row identitydb.ResolveCorrectionRequestRow) (domain.CorrectionRequest, error) {
	rowVersion := int(row.RowVersion)
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
		&rowVersion,
		row.CreatedAt,
		row.ResolvedAt,
	)
}

func correctionRequestFromResolveGetRow(row identitydb.GetCorrectionRequestForResolveRow) (domain.CorrectionRequest, *string, error) {
	rowVersion := int(row.RowVersion)
	correction, err := correctionRequestFromFields(
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
		&rowVersion,
		row.CreatedAt,
		row.ResolvedAt,
	)
	if err != nil {
		return domain.CorrectionRequest{}, nil, err
	}
	return correction, nonEmptyStringPtr(row.DecisionID), nil
}

func correctionRequestFromFields(correctionID, requestType, state, goatID string, identifierType pgtype.Text, identifierValue pgtype.Text, farmID, parkID, shedID, cohortID, description string, evidenceBytes []byte, rowVersion *int, createdAt pgtype.Timestamptz, resolvedAt pgtype.Timestamptz) (domain.CorrectionRequest, error) {
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
		RowVersion:   rowVersion,
		CreatedAt:    created,
		ResolvedAt:   resolved,
	}, nil
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

func decisionSummaryFromGetRow(row identitydb.GetDecisionSummaryByIDRow) domain.DecisionRecordSummary {
	return domain.DecisionRecordSummary{
		DecisionID:     row.DecisionID,
		DecisionType:   row.DecisionType,
		DecisionResult: row.DecisionResult,
		DecisionState:  row.DecisionState,
		PolicyVersion:  row.PolicyVersion,
		CreatedAt:      row.CreatedAt.Time,
	}
}

func correctionDecisionMapping(targetState string) (decisionState string, decisionResult string) {
	switch targetState {
	case "approved":
		return "approved", "approved"
	case "rejected":
		return "rejected", "rejected"
	case "needs_field_check":
		return "needs_review", "needs_field_check"
	case "closed":
		return "approved", "closed"
	default:
		return "needs_review", targetState
	}
}

func correctionTargetTerminal(targetState string) bool {
	return targetState == "approved" || targetState == "rejected" || targetState == "closed"
}

func correctionDecisionRecordPayload(cmd ports.ResolveCorrectionRequestCommand, decisionID, decisionState, decisionResult string, createdAt time.Time) ([]byte, error) {
	payload := map[string]any{
		"decision_id":     decisionID,
		"decision_type":   correctionResolveDecisionType,
		"decision_result": decisionResult,
		"decision_state":  decisionState,
		"decided_by_type": "human",
		"decided_by":      cmd.ActorID,
		"reviewer_id":     cmd.ActorID,
		"policy_version":  correctionResolvePolicyVersion,
		"reason":          cmd.Reason,
		"evidence": map[string]any{
			"evidence_refs": cmd.EvidenceRefs,
		},
		"idempotency_key": cmd.StoredIdempotencyKey,
		"trace_id":        cmd.TraceID,
		"created_at":      createdAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"decided_at":      createdAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
	}
	if decisionState == "approved" {
		payload["approved_at"] = createdAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	} else {
		payload["approved_at"] = nil
	}
	return json.Marshal(payload)
}

func resolveCorrectionConflict(ctx context.Context, qtx *identitydb.Queries, tenantUUID, correctionUUID pgtype.UUID) error {
	row, err := qtx.GetCorrectionRequestForResolve(ctx, identitydb.GetCorrectionRequestForResolveParams{
		TenantID:            tenantUUID,
		CorrectionRequestID: correctionUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.State == "approved" || row.State == "rejected" || row.State == "closed" {
		return ports.ErrWriteConflict
	}
	return ports.ErrWriteConflict
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
