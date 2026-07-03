package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	identifierResultType    = "goat_identifier"
	identifierPolicyVersion = "phase1-identifier-v1"
	identifierAggregate     = "goat"
	identifierSubject       = "identifier"
	identifierTopic         = "identity.events"

	attachIdentifierDecisionType   = "attach_identifier"
	attachIdentifierDecisionResult = "identifier_attached"
	attachIdentifierAction         = "attach"
	attachIdentifierEventType      = "goat.identifier.added"

	retireIdentifierDecisionType   = "retire_identifier"
	retireIdentifierDecisionResult = "identifier_retired"
	retireIdentifierAction         = "retire"
	retireIdentifierEventType      = "goat.identifier.retired"
)

func (r *Repository) AddGoatIdentifier(ctx context.Context, cmd ports.AddGoatIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
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
	goatUUID, err := uuidParam(cmd.GoatID)
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
		return r.replayIdentifierMutation(ctx, qtx, tenantUUID, cmd.StoredIdempotencyKey, cmd.RequestHash, attachIdentifierAction)
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if err := guardGoatForIdentifierMutation(ctx, qtx, tenantUUID, goatUUID, cmd.RowVersion, now); err != nil {
		return nil, err
	}

	policy, err := qtx.GetIdentifierPolicy(ctx, identitydb.GetIdentifierPolicyParams{
		PolicyVersion:  identifierPolicyVersion,
		IdentifierType: cmd.IdentifierType,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrWriteConflict
	}
	if err != nil {
		return nil, err
	}
	if cmd.IsPrimaryForGoat && !policy.PrimaryAllowed {
		return nil, ports.ErrWriteConflict
	}

	inserted, err := qtx.InsertGoatIdentifier(ctx, identitydb.InsertGoatIdentifierParams{
		TenantID:          tenantUUID,
		GoatID:            goatUUID,
		IdentifierType:    cmd.IdentifierType,
		IdentifierValue:   cmd.IdentifierValue,
		NormalizedValue:   cmd.NormalizedValue,
		ScopeKey:          cmd.ScopeKey,
		IsPrimaryForGoat:  cmd.IsPrimaryForGoat,
		ValidFrom:         pgtype.Timestamptz{Time: now, Valid: true},
		NormalizerVersion: policy.NormalizerVersion,
		ApprovedBy:        actorUUID,
	})
	if isUniqueViolation(err) {
		return nil, ports.ErrWriteConflict
	}
	if err != nil {
		return nil, err
	}
	identifier := identifierFromInsertRow(inserted)
	return r.finishIdentifierMutation(ctx, qtx, tx, &committed, identifierMutationFinish{
		TenantUUID: tenantUUID,
		ActorUUID:  actorUUID,
		GoatUUID:   goatUUID,
		Command: identifierCommandEnvelope{
			TenantID:             cmd.TenantID,
			ActorID:              cmd.ActorID,
			ClientIdempotencyKey: cmd.ClientIdempotencyKey,
			StoredIdempotencyKey: cmd.StoredIdempotencyKey,
			IdempotencyScope:     cmd.IdempotencyScope,
			TraceID:              cmd.TraceID,
			GoatID:               cmd.GoatID,
			Reason:               cmd.Reason,
			EvidenceRefs:         cmd.EvidenceRefs,
		},
		Identifier:      identifier,
		NormalizedValue: cmd.NormalizedValue,
		DecisionType:    attachIdentifierDecisionType,
		DecisionResult:  attachIdentifierDecisionResult,
		Action:          attachIdentifierAction,
		EventType:       attachIdentifierEventType,
		OccurredAt:      now,
	})
}

func (r *Repository) RetireGoatIdentifier(ctx context.Context, cmd ports.RetireGoatIdentifierCommand) (*ports.AdminGoatMutationResult, error) {
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
	goatUUID, err := uuidParam(cmd.GoatID)
	if err != nil {
		return nil, err
	}
	identifierUUID, err := uuidParam(cmd.IdentifierID)
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
		return r.replayIdentifierMutation(ctx, qtx, tenantUUID, cmd.StoredIdempotencyKey, cmd.RequestHash, retireIdentifierAction)
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if err := guardGoatForIdentifierMutation(ctx, qtx, tenantUUID, goatUUID, cmd.RowVersion, now); err != nil {
		return nil, err
	}

	retired, err := qtx.RetireGoatIdentifier(ctx, identitydb.RetireGoatIdentifierParams{
		ValidTo:      pgtype.Timestamptz{Time: now, Valid: true},
		ApprovedBy:   actorUUID,
		UpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		TenantID:     tenantUUID,
		GoatID:       goatUUID,
		IdentifierID: identifierUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if conflictErr := retireIdentifierConflict(ctx, qtx, tenantUUID, goatUUID, identifierUUID); conflictErr != nil {
			return nil, conflictErr
		}
		return nil, ports.ErrWriteConflict
	}
	if err != nil {
		return nil, err
	}
	identifier := identifierFromRetireRow(retired)
	return r.finishIdentifierMutation(ctx, qtx, tx, &committed, identifierMutationFinish{
		TenantUUID: tenantUUID,
		ActorUUID:  actorUUID,
		GoatUUID:   goatUUID,
		Command: identifierCommandEnvelope{
			TenantID:             cmd.TenantID,
			ActorID:              cmd.ActorID,
			ClientIdempotencyKey: cmd.ClientIdempotencyKey,
			StoredIdempotencyKey: cmd.StoredIdempotencyKey,
			IdempotencyScope:     cmd.IdempotencyScope,
			TraceID:              cmd.TraceID,
			GoatID:               cmd.GoatID,
			Reason:               cmd.Reason,
			EvidenceRefs:         cmd.EvidenceRefs,
		},
		Identifier:      identifier,
		NormalizedValue: retired.NormalizedValue,
		DecisionType:    retireIdentifierDecisionType,
		DecisionResult:  retireIdentifierDecisionResult,
		Action:          retireIdentifierAction,
		EventType:       retireIdentifierEventType,
		OccurredAt:      now,
	})
}

type identifierCommandEnvelope struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	TraceID              string
	GoatID               string
	Reason               string
	EvidenceRefs         []domain.EvidenceRef
}

type identifierMutationFinish struct {
	TenantUUID      pgtype.UUID
	ActorUUID       pgtype.UUID
	GoatUUID        pgtype.UUID
	Command         identifierCommandEnvelope
	Identifier      domain.GoatIdentifier
	NormalizedValue string
	DecisionType    string
	DecisionResult  string
	Action          string
	EventType       string
	OccurredAt      time.Time
}

type identifierDecisionEvidence struct {
	EvidenceRefs   []domain.EvidenceRef `json:"evidence_refs"`
	Reason         string               `json:"reason"`
	DecisionRecord json.RawMessage      `json:"decision_record"`
}

func (r *Repository) finishIdentifierMutation(ctx context.Context, qtx *identitydb.Queries, tx pgx.Tx, committed *bool, finish identifierMutationFinish) (*ports.AdminGoatMutationResult, error) {
	identifierUUID, err := uuidParam(finish.Identifier.IdentifierID)
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
	decisionRecord, err := identifierDecisionRecordPayload(finish, decisionID)
	if err != nil {
		return nil, err
	}
	decisionEvidence, err := json.Marshal(identifierDecisionEvidence{
		EvidenceRefs:   finish.Command.EvidenceRefs,
		Reason:         finish.Command.Reason,
		DecisionRecord: decisionRecord,
	})
	if err != nil {
		return nil, err
	}
	decisionRow, err := qtx.InsertIdentityDecision(ctx, identitydb.InsertIdentityDecisionParams{
		DecisionID:     decisionUUID,
		TenantID:       finish.TenantUUID,
		DecisionType:   finish.DecisionType,
		DecisionResult: finish.DecisionResult,
		DecisionState:  "approved",
		DecidedBy:      finish.ActorUUID,
		PolicyVersion:  identifierPolicyVersion,
		ReviewerID:     finish.ActorUUID,
		Evidence:       decisionEvidence,
		CreatedAt:      pgtype.Timestamptz{Time: finish.OccurredAt, Valid: true},
		ApprovedAt:     pgtype.Timestamptz{Time: finish.OccurredAt, Valid: true},
		DecidedAt:      pgtype.Timestamptz{Time: finish.OccurredAt, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	decision := decisionSummaryFromInsertRow(decisionRow)

	if err := qtx.InsertIdentityDecisionIdentifier(ctx, identitydb.InsertIdentityDecisionIdentifierParams{
		DecisionID:      decisionUUID,
		TenantID:        finish.TenantUUID,
		IdentifierID:    identifierUUID,
		IdentifierType:  textParam(finish.Identifier.IdentifierType),
		IdentifierValue: textParam(finish.Identifier.IdentifierValue),
		Action:          finish.Action,
	}); err != nil {
		return nil, err
	}

	eventID, err := qtx.NewUUID(ctx)
	if err != nil {
		return nil, err
	}
	eventUUID, err := uuidParam(eventID)
	if err != nil {
		return nil, err
	}
	eventPayload, err := identifierEventPayload(finish, decision.DecisionID)
	if err != nil {
		return nil, err
	}
	eventRow, err := qtx.InsertGoatIdentityEvent(ctx, identitydb.InsertGoatIdentityEventParams{
		IdentityEventID: eventUUID,
		TenantID:        finish.TenantUUID,
		GoatID:          finish.GoatUUID,
		EventType:       finish.EventType,
		OccurredAt:      pgtype.Timestamptz{Time: finish.OccurredAt, Valid: true},
		RecordedAt:      pgtype.Timestamptz{Time: finish.OccurredAt, Valid: true},
		ActorID:         finish.ActorUUID,
		Payload:         eventPayload,
		DecisionID:      decisionUUID,
		IdempotencyKey:  finish.Command.StoredIdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertIdentityDecisionEvent(ctx, identitydb.InsertIdentityDecisionEventParams{
		DecisionID:      decisionUUID,
		TenantID:        finish.TenantUUID,
		EventID:         eventUUID,
		EventRecordedAt: eventRow.RecordedAt,
	}); err != nil {
		return nil, err
	}

	response, err := adminGoatMutationResult(ctx, qtx, finish.TenantUUID, finish.Command.TenantID, finish.GoatUUID, decision, []domain.EventSummary{{EventID: eventRow.EventID, EventType: finish.EventType}}, false, nil)
	if err != nil {
		return nil, err
	}
	afterState, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(map[string]any{
		"actor_id":               finish.Command.ActorID,
		"idempotency_key":        finish.Command.StoredIdempotencyKey,
		"client_idempotency_key": finish.Command.ClientIdempotencyKey,
		"idempotency_scope":      finish.Command.IdempotencyScope,
		"trace_id":               finish.Command.TraceID,
		"decision_id":            decision.DecisionID,
		"identifier_id":          finish.Identifier.IdentifierID,
		"action":                 finish.Action,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertAuditLog(ctx, identitydb.InsertAuditLogParams{
		TenantID:     finish.TenantUUID,
		ActorID:      finish.ActorUUID,
		Action:       finish.EventType,
		ResourceType: identifierSubject,
		ResourceID:   identifierUUID,
		ScopeType:    nullableText(nonEmptyStringPtr(scopeType(locationScopeFromSummary(response.Goat)))),
		ScopeID:      nullableUUID(scopeID(locationScopeFromSummary(response.Goat))),
		AfterState:   afterState,
		Metadata:     metadata,
		TraceID:      nullableText(nonEmptyStringPtr(finish.Command.TraceID)),
	}); err != nil {
		return nil, err
	}

	if r.afterAuditHook != nil {
		if err := r.afterAuditHook(ctx); err != nil {
			return nil, err
		}
	}

	envelope, err := identifierDomainEventEnvelope(finish, response.Goat, decision, eventRow.EventID)
	if err != nil {
		return nil, err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":               finish.Command.ActorID,
		"client_idempotency_key": finish.Command.ClientIdempotencyKey,
		"trace_id":               finish.Command.TraceID,
		"decision_id":            decision.DecisionID,
		"identifier_id":          finish.Identifier.IdentifierID,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertOutboxMessage(ctx, identitydb.InsertOutboxMessageParams{
		TenantID:       finish.TenantUUID,
		EventID:        eventUUID,
		EventType:      finish.EventType,
		SchemaVersion:  eventSchemaVersion,
		AggregateType:  identifierAggregate,
		AggregateID:    finish.GoatUUID,
		Topic:          identifierTopic,
		Payload:        envelope,
		Headers:        headers,
		IdempotencyKey: finish.Command.StoredIdempotencyKey,
		TraceID:        nullableText(nonEmptyStringPtr(finish.Command.TraceID)),
	}); err != nil {
		return nil, err
	}

	if err := qtx.CompleteIdempotencyKey(ctx, identitydb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(identifierResultType),
		ResultID:       identifierUUID,
		IdempotencyKey: finish.Command.StoredIdempotencyKey,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	*committed = true
	return response, nil
}

func (r *Repository) replayIdentifierMutation(ctx context.Context, qtx *identitydb.Queries, tenantUUID pgtype.UUID, key string, requestHash string, action string) (*ports.AdminGoatMutationResult, error) {
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
	if idempotency.Status != "completed" || idempotency.ResultType != identifierResultType || strings.TrimSpace(idempotency.ResultID) == "" {
		return nil, ports.ErrIdempotencyPending
	}
	identifierUUID, err := uuidParam(idempotency.ResultID)
	if err != nil {
		return nil, err
	}
	identifier, err := qtx.GetIdentifierByID(ctx, identitydb.GetIdentifierByIDParams{
		TenantID:     tenantUUID,
		IdentifierID: identifierUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	replayRow, err := qtx.GetIdentifierDecisionEventForReplay(ctx, identitydb.GetIdentifierDecisionEventForReplayParams{
		TenantID:       tenantUUID,
		IdentifierID:   identifierUUID,
		Action:         action,
		IdempotencyKey: key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrIdempotencyPending
	}
	if err != nil {
		return nil, err
	}
	goatUUID, err := uuidParam(identifier.GoatID)
	if err != nil {
		return nil, err
	}
	firstResultID := idempotency.ResultID
	return adminGoatMutationResult(
		ctx,
		qtx,
		tenantUUID,
		uuidText(tenantUUID),
		goatUUID,
		decisionSummaryFromIdentifierReplay(replayRow),
		[]domain.EventSummary{{EventID: replayRow.EventID, EventType: replayRow.EventType}},
		true,
		&firstResultID,
	)
}

func guardGoatForIdentifierMutation(ctx context.Context, qtx *identitydb.Queries, tenantUUID, goatUUID pgtype.UUID, rowVersion int, at time.Time) error {
	_, err := qtx.GuardGoatForIdentifierMutation(ctx, identitydb.GuardGoatForIdentifierMutationParams{
		UpdatedAt:  pgtype.Timestamptz{Time: at, Valid: true},
		GoatID:     goatUUID,
		TenantID:   tenantUUID,
		RowVersion: int32(rowVersion),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return goatMutationConflict(ctx, qtx, tenantUUID, goatUUID)
	}
	return err
}

func goatMutationConflict(ctx context.Context, qtx *identitydb.Queries, tenantUUID, goatUUID pgtype.UUID) error {
	row, err := qtx.GetGoatMutationState(ctx, identitydb.GetGoatMutationStateParams{
		TenantID: tenantUUID,
		GoatID:   goatUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.MergedIntoGoatID != "" {
		return ports.ErrWriteConflict
	}
	return ports.ErrWriteConflict
}

func retireIdentifierConflict(ctx context.Context, qtx *identitydb.Queries, tenantUUID, goatUUID, identifierUUID pgtype.UUID) error {
	row, err := qtx.GetIdentifierByID(ctx, identitydb.GetIdentifierByIDParams{
		TenantID:     tenantUUID,
		IdentifierID: identifierUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.GoatID != uuidText(goatUUID) {
		return ports.ErrNotFound
	}
	if row.Status != "active" {
		return ports.ErrWriteConflict
	}
	return ports.ErrWriteConflict
}

func adminGoatMutationResult(ctx context.Context, qtx *identitydb.Queries, tenantUUID pgtype.UUID, tenantID string, goatUUID pgtype.UUID, decision domain.DecisionRecordSummary, events []domain.EventSummary, replayed bool, firstResultID *string) (*ports.AdminGoatMutationResult, error) {
	goatRow, err := qtx.GetGoatByID(ctx, identitydb.GetGoatByIDParams{
		TenantID: tenantUUID,
		GoatID:   goatUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	goat, _, _, _ := goatSummaryFromSQLC(sqlcGoatRowFromID(goatRow))
	identifiers, err := qtx.ListIdentifiersForGoat(ctx, identitydb.ListIdentifiersForGoatParams{
		TenantID: tenantUUID,
		GoatID:   goatUUID,
	})
	if err != nil {
		return nil, err
	}
	items := make([]domain.GoatIdentifier, 0, len(identifiers))
	for _, row := range identifiers {
		items = append(items, identifierFromSQLC(row))
	}
	_ = tenantID
	return &ports.AdminGoatMutationResult{
		Goat:          goat,
		Identifiers:   items,
		Decision:      decision,
		Events:        events,
		Replayed:      replayed,
		FirstResultID: firstResultID,
	}, nil
}

func identifierDecisionRecordPayload(finish identifierMutationFinish, decisionID string) ([]byte, error) {
	payload := map[string]any{
		"decision_id":     decisionID,
		"decision_type":   finish.DecisionType,
		"decision_result": finish.DecisionResult,
		"decision_state":  "approved",
		"decided_by_type": "human",
		"decided_by":      finish.Command.ActorID,
		"reviewer_id":     finish.Command.ActorID,
		"policy_version":  identifierPolicyVersion,
		"reason":          finish.Command.Reason,
		"evidence": map[string]any{
			"evidence_refs": finish.Command.EvidenceRefs,
		},
		"identifier_actions": []map[string]any{{
			"identifier_id":    finish.Identifier.IdentifierID,
			"identifier_type":  finish.Identifier.IdentifierType,
			"identifier_value": finish.Identifier.IdentifierValue,
			"action":           finish.Action,
		}},
		"idempotency_key": finish.Command.StoredIdempotencyKey,
		"trace_id":        finish.Command.TraceID,
		"created_at":      finish.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"approved_at":     finish.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"decided_at":      finish.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
	}
	return json.Marshal(payload)
}

func identifierEventPayload(finish identifierMutationFinish, decisionID string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"goat_id":          finish.Command.GoatID,
		"identifier_id":    finish.Identifier.IdentifierID,
		"identifier_type":  finish.Identifier.IdentifierType,
		"identifier_value": finish.Identifier.IdentifierValue,
		"normalized_value": finish.NormalizedValue,
		"scope_key":        finish.Identifier.ScopeKey,
		"decision_id":      decisionID,
		"action":           finish.Action,
	})
}

func identifierDomainEventEnvelope(finish identifierMutationFinish, goat domain.GoatSummary, decision domain.DecisionRecordSummary, eventID string) ([]byte, error) {
	envelope := eventEnvelope{
		EventID:         eventID,
		EventType:       finish.EventType,
		SchemaVersion:   eventSchemaVersion,
		SchemaRef:       eventSchemaRef,
		AggregateType:   identifierAggregate,
		AggregateID:     finish.Command.GoatID,
		OccurredAt:      finish.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		RecordedAt:      finish.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		Producer:        eventProducer{Service: "goatos-api", Module: "identity"},
		IdempotencyKey:  finish.Command.StoredIdempotencyKey,
		Actor:           eventActor{ActorType: "human", ActorID: &finish.Command.ActorID},
		SubjectType:     identifierSubject,
		SubjectID:       finish.Identifier.IdentifierID,
		VisibilityScope: locationScopeFromSummary(goat),
		EvidenceRefs:    finish.Command.EvidenceRefs,
		Payload: map[string]any{
			"goat_id":          finish.Command.GoatID,
			"identifier_id":    finish.Identifier.IdentifierID,
			"identifier_type":  finish.Identifier.IdentifierType,
			"identifier_value": finish.Identifier.IdentifierValue,
			"normalized_value": finish.NormalizedValue,
			"scope_key":        finish.Identifier.ScopeKey,
			"decision_id":      decision.DecisionID,
			"action":           finish.Action,
		},
		TraceID: finish.Command.TraceID,
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
	visibilityScope["tenant_id"] = finish.Command.TenantID
	withTenant["visibility_scope"] = visibilityScope
	return json.Marshal(withTenant)
}

func identifierFromInsertRow(row identitydb.InsertGoatIdentifierRow) domain.GoatIdentifier {
	confidence := row.Confidence
	var confidencePtr *float64
	if !isNaN(confidence) {
		confidencePtr = &confidence
	}
	return domain.GoatIdentifier{
		IdentifierID:     row.IdentifierID,
		IdentifierType:   row.IdentifierType,
		IdentifierValue:  row.IdentifierValue,
		ScopeKey:         row.ScopeKey,
		Status:           row.Status,
		IsPrimaryForGoat: row.IsPrimaryForGoat,
		ValidFrom:        pgTime(row.ValidFrom),
		ValidTo:          pgTimePtr(row.ValidTo),
		SourceSystem:     pgTextPtr(row.SourceSystem),
		SourceRecordID:   pgTextPtr(row.SourceRecordID),
		Confidence:       confidencePtr,
	}
}

func identifierFromRetireRow(row identitydb.RetireGoatIdentifierRow) domain.GoatIdentifier {
	confidence := row.Confidence
	var confidencePtr *float64
	if !isNaN(confidence) {
		confidencePtr = &confidence
	}
	return domain.GoatIdentifier{
		IdentifierID:     row.IdentifierID,
		IdentifierType:   row.IdentifierType,
		IdentifierValue:  row.IdentifierValue,
		ScopeKey:         row.ScopeKey,
		Status:           row.Status,
		IsPrimaryForGoat: row.IsPrimaryForGoat,
		ValidFrom:        pgTime(row.ValidFrom),
		ValidTo:          pgTimePtr(row.ValidTo),
		SourceSystem:     pgTextPtr(row.SourceSystem),
		SourceRecordID:   pgTextPtr(row.SourceRecordID),
		Confidence:       confidencePtr,
	}
}

func decisionSummaryFromIdentifierReplay(row identitydb.GetIdentifierDecisionEventForReplayRow) domain.DecisionRecordSummary {
	return domain.DecisionRecordSummary{
		DecisionID:     row.DecisionID,
		DecisionType:   row.DecisionType,
		DecisionResult: row.DecisionResult,
		DecisionState:  row.DecisionState,
		PolicyVersion:  row.PolicyVersion,
		CreatedAt:      pgTime(row.CreatedAt),
	}
}

func locationScopeFromSummary(goat domain.GoatSummary) domain.LocationScope {
	return domain.LocationScope{
		FarmID:   goat.LocationPath.FarmID,
		ParkID:   goat.LocationPath.ParkID,
		ShedID:   goat.LocationPath.ShedID,
		CohortID: goat.LocationPath.CohortID,
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func uuidText(value pgtype.UUID) string {
	return value.String()
}

func isNaN(value float64) bool {
	return value != value
}
