package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const (
	adminGoatResultType     = "goat"
	adminGoatPolicyVersion  = "admin-goat-create-v1"
	adminGoatDecisionType   = "create_goat"
	adminGoatDecisionResult = "goat_created"
	adminGoatEventType      = "goat.created"
	adminGoatAggregate      = "goat"
	adminGoatSubject        = "goat"
	adminGoatTopic          = "identity.events"
)

func (r *Repository) ValidateAdminGoatCreate(ctx context.Context, cmd ports.ValidateAdminGoatCreateCommand) (ports.AdminGoatCreateValidation, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var out ports.AdminGoatCreateValidation
	if _, replay, err := r.completedAdminGoatCreateReplayTarget(ctx, cmd.StoredIdempotencyKey, cmd.RequestHash); err != nil {
		return out, err
	} else if replay {
		return out, nil
	}

	custodian, err := r.resolveMeshaCustodian(ctx, cmd.TenantID)
	if err != nil {
		return out, err
	}
	out.CustodianPartyID = custodian

	if cmd.FarmID != nil || cmd.FarmCode != nil {
		farmID, err := r.resolveLocation(ctx, cmd.TenantID, "farm", cmd.FarmID, cmd.FarmCode)
		if err != nil {
			out.Conflicts = append(out.Conflicts, domain.FieldError{Field: "farm_id", Code: "not_found", Message: "farm_id or farm_code does not resolve to an active farm"})
		} else {
			out.FarmID = &farmID
		}
	}
	parkID, err := r.resolveLocation(ctx, cmd.TenantID, "park", cmd.ParkID, cmd.ParkCode)
	if err != nil {
		out.Conflicts = append(out.Conflicts, domain.FieldError{Field: "park_id", Code: "not_found", Message: "park_id or park_code does not resolve to an active park"})
	} else {
		out.ParkID = parkID
	}
	shedID, err := r.resolveLocation(ctx, cmd.TenantID, "shed", cmd.ShedID, cmd.ShedCode)
	if err != nil {
		out.Conflicts = append(out.Conflicts, domain.FieldError{Field: "shed_id", Code: "not_found", Message: "shed_id or shed_code does not resolve to an active shed"})
	} else {
		out.ShedID = shedID
	}
	if out.ParkID != "" && out.ShedID != "" {
		if err := r.ensureShedUnderPark(ctx, cmd.TenantID, out.ShedID, out.ParkID); err != nil {
			out.Conflicts = append(out.Conflicts, domain.FieldError{Field: "shed_id", Code: "wrong_parent", Message: "shed does not belong to the selected park"})
		}
	}
	if cmd.ManagementStage != nil {
		exists, err := r.activeManagementStageExists(ctx, cmd.TenantID, *cmd.ManagementStage)
		if err != nil {
			return out, err
		}
		if !exists {
			out.Conflicts = append(out.Conflicts, domain.FieldError{Field: "management_stage", Code: "not_found", Message: "management_stage does not resolve to an active animal stage"})
		}
	}
	if len(out.Conflicts) > 0 {
		return out, nil
	}
	for _, identifier := range cmd.Identifiers {
		conflictGoatID, err := r.identifierLifetimeConflict(ctx, cmd.TenantID, identifier.NormalizedValue)
		if err != nil {
			return out, err
		}
		if conflictGoatID != "" {
			out.Conflicts = append(out.Conflicts, domain.FieldError{
				Field:   identifier.IdentifierType,
				Code:    "identifier_already_owned",
				Message: fmt.Sprintf("%s already belongs to animal %s", identifier.IdentifierType, conflictGoatID),
			})
		}
	}
	return out, nil
}

func (r *Repository) activeManagementStageExists(ctx context.Context, tenantID, stageCode string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM animal_stage_lookup
  WHERE tenant_id = $1::uuid
    AND stage_code = $2
    AND status = 'active'
)`, tenantID, stageCode).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *Repository) completedAdminGoatCreateReplayTarget(ctx context.Context, key, requestHash string) (string, bool, error) {
	key = strings.TrimSpace(key)
	requestHash = strings.TrimSpace(requestHash)
	if key == "" || requestHash == "" {
		return "", false, nil
	}
	idempotency, err := r.queries.GetIdempotencyKey(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if idempotency.RequestHash != requestHash {
		return "", false, ports.ErrIdempotencyConflict
	}
	if idempotency.Status != "completed" || idempotency.ResultType != adminGoatResultType || strings.TrimSpace(idempotency.ResultID) == "" {
		return "", false, ports.ErrIdempotencyPending
	}
	return idempotency.ResultID, true, nil
}

// CreateAdminGoat owns its transaction: it begins, performs the whole create, and commits.
func (r *Repository) CreateAdminGoat(ctx context.Context, cmd ports.CreateAdminGoatCommand) (*ports.AdminGoatMutationResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

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
	result, err := r.createAdminGoatInTx(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}

// CreateAdminGoatInTx performs the identical create inside a transaction the CALLER owns and
// commits. It exists so another module can make a goat creation atomic with its own state change --
// specifically, so approving a pending Counts birth request flips the request to 'approved' and
// creates the kid in ONE transaction. Without this seam the two writes would be separate
// transactions and a request could read 'approved' while its goat never committed, which is exactly
// what the atomic transition rule in AGENTS.md forbids.
//
// This is a TRANSPORT seam only. Every rule -- idempotency claim/replay, identifier uniqueness, the
// decision record, the audit row, and the goat.created outbox message that generates the kid's
// vaccination obligations -- is the same code the pool-owned CreateAdminGoat runs. Note that the
// caller is responsible for the surrounding timeout: the 3s r.withTimeout applied by
// CreateAdminGoat is deliberately NOT applied here, because the caller's transaction sets the
// budget for all the work in it.
func (r *Repository) CreateAdminGoatInTx(ctx context.Context, tx pgx.Tx, cmd ports.CreateAdminGoatCommand) (*ports.AdminGoatMutationResult, error) {
	return r.createAdminGoatInTx(ctx, tx, cmd)
}

func (r *Repository) createAdminGoatInTx(ctx context.Context, tx pgx.Tx, cmd ports.CreateAdminGoatCommand) (*ports.AdminGoatMutationResult, error) {
	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	actorUUID, err := uuidParam(cmd.ActorID)
	if err != nil {
		return nil, err
	}
	farmUUID := nullableUUID(cmd.FarmID)

	qtx := r.queries.WithTx(tx)

	_, err = qtx.InsertIdempotencyStarted(ctx, identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TenantID:       tenantUUID,
		Scope:          cmd.IdempotencyScope,
		RequestHash:    cmd.RequestHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return r.replayAdminGoatCreate(ctx, tx, qtx, tenantUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	goatID, err := qtx.NewUUID(ctx)
	if err != nil {
		return nil, err
	}
	goatUUID, err := uuidParam(goatID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
	INSERT INTO goats (
	  goat_id, tenant_id, species, breed, sex, approx_dob, lifecycle_status,
	  management_stage, health_status, custodian_party_id,
	  current_location_id, farm_id, park_id, shed_id,
	  created_by, dob, dob_estimated, origin_type, entry_date
	) VALUES (
	  $1::uuid, $2::uuid, $3::text, nullif($4::text, ''), $5::text, $6::date, 'alive',
	  nullif($7::text, ''), nullif($8::text, ''), $9::uuid,
	  $10::uuid, $11::uuid, $12::uuid, $10::uuid,
	  $13::uuid, $6::date, $14::boolean, $15::text, $16::date
	)`,
		goatID,
		cmd.TenantID,
		cmd.Species,
		stringValue(cmd.Breed),
		cmd.Sex,
		dateValue(cmd.DOB),
		stringValue(cmd.ManagementStage),
		stringValue(cmd.HealthStatus),
		cmd.CustodianPartyID,
		cmd.ShedID,
		uuidArg(farmUUID),
		cmd.ParkID,
		cmd.ActorID,
		cmd.DOBEstimated,
		cmd.OriginType,
		cmd.EntryDate,
	); err != nil {
		return nil, err
	}

	identifiers := make([]domain.GoatIdentifier, 0, len(cmd.Identifiers))
	for _, identifier := range cmd.Identifiers {
		policy, err := qtx.GetIdentifierPolicy(ctx, identitydb.GetIdentifierPolicyParams{
			PolicyVersion:  identifierPolicyVersion,
			IdentifierType: identifier.IdentifierType,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ports.ErrWriteConflict
		}
		if err != nil {
			return nil, err
		}
		if identifier.IsPrimary && !policy.PrimaryAllowed {
			return nil, ports.ErrWriteConflict
		}
		inserted, err := insertAdminGoatIdentifier(ctx, tx, tenantUUID, goatUUID, actorUUID, identifier, policy.NormalizerVersion, cmd.SourceRecordID, now)
		if isUniqueViolation(err) {
			return nil, ports.ErrWriteConflict
		}
		if err != nil {
			return nil, err
		}
		identifiers = append(identifiers, inserted)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO goat_location_history (
  tenant_id, goat_id, to_location_id, reason, occurred_at, actor_id, source_record_id
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'admin_goat_create', $4::timestamptz, $5::uuid, nullif($6::text, '')
)`,
		cmd.TenantID, goatID, cmd.ShedID, now, cmd.ActorID, stringValue(cmd.SourceRecordID)); err != nil {
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
	decisionRecord, err := adminGoatDecisionRecordPayload(cmd, goatID, decisionID, now)
	if err != nil {
		return nil, err
	}
	decisionEvidence, err := json.Marshal(map[string]any{
		"evidence_refs":   cmd.EvidenceRefs,
		"decision_record": json.RawMessage(decisionRecord),
	})
	if err != nil {
		return nil, err
	}
	decisionRow, err := qtx.InsertIdentityDecision(ctx, identitydb.InsertIdentityDecisionParams{
		DecisionID:     decisionUUID,
		TenantID:       tenantUUID,
		DecisionType:   adminGoatDecisionType,
		DecisionResult: adminGoatDecisionResult,
		DecisionState:  "approved",
		DecidedBy:      actorUUID,
		PolicyVersion:  adminGoatPolicyVersion,
		ReviewerID:     actorUUID,
		Evidence:       decisionEvidence,
		CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
		ApprovedAt:     pgtype.Timestamptz{Time: now, Valid: true},
		DecidedAt:      pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	decision := decisionSummaryFromInsertRow(decisionRow)
	if _, err := tx.Exec(ctx, `
INSERT INTO identity_decision_goats (decision_id, tenant_id, goat_id, role)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'created')`,
		decisionID, cmd.TenantID, goatID); err != nil {
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
	eventPayload, err := adminGoatEventPayload(cmd, goatID, decision.DecisionID, "queued")
	if err != nil {
		return nil, err
	}
	eventRow, err := qtx.InsertGoatIdentityEvent(ctx, identitydb.InsertGoatIdentityEventParams{
		IdentityEventID: eventUUID,
		TenantID:        tenantUUID,
		GoatID:          goatUUID,
		EventType:       adminGoatEventType,
		OccurredAt:      pgtype.Timestamptz{Time: now, Valid: true},
		RecordedAt:      pgtype.Timestamptz{Time: now, Valid: true},
		ActorID:         actorUUID,
		Payload:         eventPayload,
		DecisionID:      decisionUUID,
		IdempotencyKey:  cmd.StoredIdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertIdentityDecisionEvent(ctx, identitydb.InsertIdentityDecisionEventParams{
		DecisionID:      decisionUUID,
		TenantID:        tenantUUID,
		EventID:         eventUUID,
		EventRecordedAt: eventRow.RecordedAt,
	}); err != nil {
		return nil, err
	}

	response, err := adminGoatMutationResult(ctx, qtx, tenantUUID, cmd.TenantID, goatUUID, decision, []domain.EventSummary{{EventID: eventRow.EventID, EventType: adminGoatEventType}}, false, nil)
	if err != nil {
		return nil, err
	}
	response.GenerationStatus = "queued"
	if len(response.Identifiers) == 0 {
		response.Identifiers = identifiers
	}
	afterState, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	scope := locationScopeFromSummary(response.Goat)
	scopeIDValue := scopeID(scope)
	metadata := map[string]any{
		"domain":                 "counts",
		"module":                 "herd_register",
		"category":               "goat_identity",
		"result":                 "queued",
		"status":                 "queued",
		"actor_id":               cmd.ActorID,
		"idempotency_key":        cmd.StoredIdempotencyKey,
		"operation_id":           cmd.StoredIdempotencyKey,
		"client_idempotency_key": cmd.ClientIdempotencyKey,
		"idempotency_scope":      cmd.IdempotencyScope,
		"trace_id":               cmd.TraceID,
		"decision_id":            decision.DecisionID,
		"identity_event_id":      eventRow.EventID,
		"outbox_event_id":        eventRow.EventID,
		"generation_status":      "queued",
		"source_record_id":       stringValue(cmd.SourceRecordID),
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     cmd.TenantID,
		ActorID:      cmd.ActorID,
		ActorType:    "human",
		Action:       adminGoatEventType,
		ResourceType: adminGoatSubject,
		ResourceID:   goatID,
		ScopeType:    scopeType(scope),
		ScopeID:      stringValue(scopeIDValue),
		DecisionID:   decision.DecisionID,
		AfterState:   json.RawMessage(afterState),
		Metadata:     metadata,
		TraceID:      cmd.TraceID,
	}); err != nil {
		return nil, err
	}
	if r.afterAuditHook != nil {
		if err := r.afterAuditHook(ctx); err != nil {
			return nil, err
		}
	}

	envelope, err := adminGoatDomainEventEnvelope(cmd, response.Goat, decision, eventRow.EventID, now, "queued")
	if err != nil {
		return nil, err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":               cmd.ActorID,
		"client_idempotency_key": cmd.ClientIdempotencyKey,
		"trace_id":               cmd.TraceID,
		"decision_id":            decision.DecisionID,
		"generation_status":      "queued",
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertOutboxMessage(ctx, identitydb.InsertOutboxMessageParams{
		TenantID:       tenantUUID,
		EventID:        eventUUID,
		EventType:      adminGoatEventType,
		SchemaVersion:  eventSchemaVersion,
		AggregateType:  adminGoatAggregate,
		AggregateID:    goatUUID,
		Topic:          adminGoatTopic,
		Payload:        envelope,
		Headers:        headers,
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TraceID:        nullableText(nonEmptyStringPtr(cmd.TraceID)),
	}); err != nil {
		return nil, err
	}
	if err := qtx.CompleteIdempotencyKey(ctx, identitydb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(adminGoatResultType),
		ResultID:       goatUUID,
		IdempotencyKey: cmd.StoredIdempotencyKey,
	}); err != nil {
		return nil, err
	}
	return response, nil
}

func (r *Repository) replayAdminGoatCreate(ctx context.Context, tx pgx.Tx, qtx *identitydb.Queries, tenantUUID pgtype.UUID, key string, requestHash string) (*ports.AdminGoatMutationResult, error) {
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
	if idempotency.Status != "completed" || idempotency.ResultType != adminGoatResultType || strings.TrimSpace(idempotency.ResultID) == "" {
		return nil, ports.ErrIdempotencyPending
	}
	goatUUID, err := uuidParam(idempotency.ResultID)
	if err != nil {
		return nil, err
	}
	var row struct {
		DecisionID       string
		DecisionType     string
		DecisionResult   string
		DecisionState    string
		PolicyVersion    string
		CreatedAt        time.Time
		EventID          string
		EventType        string
		GenerationStatus string
	}
	err = tx.QueryRow(ctx, `
SELECT
  d.decision_id::text,
  d.decision_type,
  d.decision_result,
  d.decision_state,
  d.policy_version,
  d.created_at,
  gie.identity_event_id::text,
  gie.event_type,
  COALESCE(gie.payload->>'generation_status', 'queued')
FROM identity_decision_goats idg
JOIN identity_decisions d
  ON d.tenant_id = idg.tenant_id
 AND d.decision_id = idg.decision_id
JOIN identity_decision_events ide
  ON ide.tenant_id = idg.tenant_id
 AND ide.decision_id = idg.decision_id
JOIN goat_identity_events gie
  ON gie.tenant_id = idg.tenant_id
 AND gie.identity_event_id = ide.event_id
 AND gie.recorded_at = ide.event_recorded_at
WHERE idg.tenant_id = $1
  AND idg.goat_id = $2
  AND idg.role = 'created'
  AND d.decision_type = 'create_goat'
  AND d.evidence->'decision_record'->>'idempotency_key' = $3
ORDER BY d.created_at DESC
LIMIT 1`, tenantUUID, goatUUID, key).Scan(
		&row.DecisionID,
		&row.DecisionType,
		&row.DecisionResult,
		&row.DecisionState,
		&row.PolicyVersion,
		&row.CreatedAt,
		&row.EventID,
		&row.EventType,
		&row.GenerationStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrIdempotencyPending
	}
	if err != nil {
		return nil, err
	}
	firstResultID := idempotency.ResultID
	result, err := adminGoatMutationResult(
		ctx,
		qtx,
		tenantUUID,
		uuidText(tenantUUID),
		goatUUID,
		domain.DecisionRecordSummary{
			DecisionID:     row.DecisionID,
			DecisionType:   row.DecisionType,
			DecisionResult: row.DecisionResult,
			DecisionState:  row.DecisionState,
			PolicyVersion:  row.PolicyVersion,
			CreatedAt:      row.CreatedAt,
		},
		[]domain.EventSummary{{EventID: row.EventID, EventType: row.EventType}},
		true,
		&firstResultID,
	)
	if err != nil {
		return nil, err
	}
	result.GenerationStatus = row.GenerationStatus
	return result, nil
}

func insertAdminGoatIdentifier(ctx context.Context, tx pgx.Tx, tenantUUID, goatUUID, actorUUID pgtype.UUID, identifier ports.AdminGoatCreateIdentifier, normalizerVersion string, sourceRecordID *string, now time.Time) (domain.GoatIdentifier, error) {
	scopeKey := identifier.ScopeKey
	if strings.TrimSpace(scopeKey) == "" {
		scopeKey = "global"
	}
	var row struct {
		IdentifierID     string
		IdentifierType   string
		IdentifierValue  string
		ScopeKey         string
		Status           string
		IsPrimaryForGoat bool
		ValidFrom        time.Time
		ValidTo          *time.Time
		SourceSystem     *string
		SourceRecordID   *string
		Confidence       *float64
	}
	confidence := 1.0
	err := tx.QueryRow(ctx, `
INSERT INTO goat_identifiers (
  tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key,
  is_primary_for_goat, status, valid_from, source_system, source_record_id,
  normalizer_version, confidence, approved_by
) VALUES (
  $1, $2, $3, $4, $5, $6,
  $7, 'active', $8::timestamptz, 'admin_herd_register', nullif($9::text, ''),
  $10, $11::numeric, $12
)
RETURNING identifier_id::text, identifier_type, identifier_value, scope_key, status,
          is_primary_for_goat, valid_from, valid_to, source_system, source_record_id,
          confidence::float8`,
		tenantUUID,
		goatUUID,
		identifier.IdentifierType,
		identifier.IdentifierValue,
		identifier.NormalizedValue,
		scopeKey,
		identifier.IsPrimary,
		now,
		stringValue(sourceRecordID),
		normalizerVersion,
		confidence,
		actorUUID,
	).Scan(
		&row.IdentifierID,
		&row.IdentifierType,
		&row.IdentifierValue,
		&row.ScopeKey,
		&row.Status,
		&row.IsPrimaryForGoat,
		&row.ValidFrom,
		&row.ValidTo,
		&row.SourceSystem,
		&row.SourceRecordID,
		&row.Confidence,
	)
	if err != nil {
		return domain.GoatIdentifier{}, err
	}
	return domain.GoatIdentifier{
		IdentifierID:     row.IdentifierID,
		IdentifierType:   row.IdentifierType,
		IdentifierValue:  row.IdentifierValue,
		ScopeKey:         row.ScopeKey,
		Status:           row.Status,
		IsPrimaryForGoat: row.IsPrimaryForGoat,
		ValidFrom:        row.ValidFrom,
		ValidTo:          row.ValidTo,
		SourceSystem:     row.SourceSystem,
		SourceRecordID:   row.SourceRecordID,
		Confidence:       row.Confidence,
	}, nil
}

func adminGoatDecisionRecordPayload(cmd ports.CreateAdminGoatCommand, goatID, decisionID string, at time.Time) ([]byte, error) {
	sourceRecordIDs := []string{}
	if sourceRecordID := stringValue(cmd.SourceRecordID); sourceRecordID != "" {
		sourceRecordIDs = append(sourceRecordIDs, sourceRecordID)
	}
	identifierActions := make([]map[string]any, 0, len(cmd.Identifiers))
	for _, identifier := range cmd.Identifiers {
		identifierActions = append(identifierActions, map[string]any{
			"identifier_type":  identifier.IdentifierType,
			"identifier_value": identifier.IdentifierValue,
			"action":           "attach",
		})
	}
	return json.Marshal(map[string]any{
		"decision_id":        decisionID,
		"decision_type":      adminGoatDecisionType,
		"decision_result":    adminGoatDecisionResult,
		"decision_state":     "approved",
		"decided_by_type":    "human",
		"decided_by":         cmd.ActorID,
		"reviewer_id":        cmd.ActorID,
		"policy_version":     adminGoatPolicyVersion,
		"reason":             "Admin created canonical goat from herd register evidence.",
		"source_record_ids":  sourceRecordIDs,
		"affected_goats":     []map[string]any{{"goat_id": goatID, "role": "affected"}},
		"identifier_actions": identifierActions,
		"evidence": map[string]any{
			"evidence_refs": cmd.EvidenceRefs,
		},
		"idempotency_key": cmd.StoredIdempotencyKey,
		"trace_id":        cmd.TraceID,
		"created_at":      at.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"approved_at":     at.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"decided_at":      at.UTC().Format("2006-01-02T15:04:05.000000Z"),
	})
}

func adminGoatEventPayload(cmd ports.CreateAdminGoatCommand, goatID, decisionID, generationStatus string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"goat_id":           goatID,
		"decision_id":       decisionID,
		"identifiers":       cmd.Identifiers,
		"species":           cmd.Species,
		"origin_type":       cmd.OriginType,
		"entry_date":        biztime.BusinessDate(cmd.EntryDate),
		"farm_id":           stringValue(cmd.FarmID),
		"park_id":           cmd.ParkID,
		"shed_id":           cmd.ShedID,
		"weight_kg":         cmd.WeightKg,
		"source_record_id":  stringValue(cmd.SourceRecordID),
		"generation_status": generationStatus,
	})
}

func adminGoatDomainEventEnvelope(cmd ports.CreateAdminGoatCommand, goat domain.GoatSummary, decision domain.DecisionRecordSummary, eventID string, at time.Time, generationStatus string) ([]byte, error) {
	envelope := eventEnvelope{
		EventID:         eventID,
		EventType:       adminGoatEventType,
		SchemaVersion:   eventSchemaVersion,
		SchemaRef:       eventSchemaRef,
		AggregateType:   adminGoatAggregate,
		AggregateID:     goat.GoatID,
		OccurredAt:      at.UTC().Format("2006-01-02T15:04:05.000000Z"),
		RecordedAt:      at.UTC().Format("2006-01-02T15:04:05.000000Z"),
		Producer:        eventProducer{Service: "goatos-api", Module: "identity"},
		IdempotencyKey:  cmd.StoredIdempotencyKey,
		Actor:           eventActor{ActorType: "human", ActorID: &cmd.ActorID},
		SubjectType:     adminGoatSubject,
		SubjectID:       goat.GoatID,
		VisibilityScope: locationScopeFromSummary(goat),
		EvidenceRefs:    cmd.EvidenceRefs,
		Payload: map[string]any{
			"goat_id":           goat.GoatID,
			"decision_id":       decision.DecisionID,
			"identifiers":       cmd.Identifiers,
			"species":           cmd.Species,
			"origin_type":       cmd.OriginType,
			"entry_date":        biztime.BusinessDate(cmd.EntryDate),
			"farm_id":           stringValue(cmd.FarmID),
			"park_id":           cmd.ParkID,
			"shed_id":           cmd.ShedID,
			"weight_kg":         cmd.WeightKg,
			"source_record_id":  stringValue(cmd.SourceRecordID),
			"generation_status": generationStatus,
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

func (r *Repository) resolveMeshaCustodian(ctx context.Context, tenantID string) (string, error) {
	var partyID string
	err := r.pool.QueryRow(ctx, `
SELECT p.party_id::text
FROM parties p
JOIN orgs o ON o.party_id = p.party_id
WHERE p.party_type = 'org'
  AND p.status = 'active'
  AND o.org_type = 'mesha'
  AND o.status = 'active'
ORDER BY p.created_at ASC
LIMIT 1`).Scan(&partyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrWriteConflict
	}
	_ = tenantID
	return partyID, err
}

func (r *Repository) resolveLocation(ctx context.Context, tenantID, locationType string, id *string, code *string) (string, error) {
	var locationID string
	idValue := stringValue(id)
	codeValue := stringValue(code)
	err := r.pool.QueryRow(ctx, `
SELECT l.location_id::text
FROM locations l
WHERE l.tenant_id = $1::uuid
  AND l.location_type = $2
  AND l.status = 'active'
  AND (
    (nullif($3::text, '') IS NOT NULL AND l.location_id = nullif($3::text, '')::uuid)
    OR (nullif($4::text, '') IS NOT NULL AND (l.location_code = $4 OR EXISTS (
      SELECT 1
      FROM location_aliases la
      WHERE la.tenant_id = l.tenant_id
        AND la.canonical_location_id = l.location_id
        AND la.alias_code = $4
    )))
  )
ORDER BY l.created_at ASC
LIMIT 1`, tenantID, locationType, idValue, codeValue).Scan(&locationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrNotFound
	}
	return locationID, err
}

// assertGoatsInPark fails closed unless EVERY movable animal in the command is currently in the
// destination park (P0-3). This is the ground-truth cross-park guard: it reads the same rows the
// relocation is about to lock and move, so it holds even when the command's optional FromParkID is
// nil (a multi-animal / not-derivable submit stores a NULL source park). A move whose animals are
// already in the destination park is a legal within-park shed move; any animal in a different park
// makes it a forbidden cross-park move.
func (r *Repository) assertGoatsInPark(ctx context.Context, tx pgx.Tx, cmd ports.RelocateGoatsCommand) error {
	var offending int
	err := tx.QueryRow(ctx, `
SELECT count(*)
FROM goats
WHERE tenant_id = $1::uuid
  AND goat_id = ANY($2::uuid[])
  AND merged_into_goat_id IS NULL
  AND exited_at IS NULL
  AND park_id IS DISTINCT FROM $3::uuid`,
		cmd.TenantID, cmd.GoatIDs, cmd.ToParkID).Scan(&offending)
	if err != nil {
		return fmt.Errorf("identity: relocate goats: verify same-park: %w", err)
	}
	if offending > 0 {
		return fmt.Errorf(
			"identity: relocate goats: cross-park movement forbidden — %d animal(s) are not in destination park %s",
			offending, cmd.ToParkID)
	}
	return nil
}

// assertGoatsAtExpectedSource enforces the approved per-goat source placement (CR-01). When the
// relocation command carries an expected source shed and/or park (FromShedID/FromParkID, captured
// on the shifting event at approval time), every named animal's CURRENT placement must still match
// it. It must be called AFTER insertRelocationIdentityEvents has taken the FOR UPDATE lock on the
// same rows, so the read here is stable for the rest of the transaction and a concurrent relocation
// cannot slip a move in between this check and the apply.
//
// Ground-truth, not the command hint: a completion re-derives placement from goats, so a stale
// approved source (A) that no longer matches the animal's current shed (C, after a newer A->C move)
// fails closed here instead of being overwritten. An absent expectation (both nil) is a no-op --
// the existing same-park guard still applies.
func (r *Repository) assertGoatsAtExpectedSource(ctx context.Context, tx pgx.Tx, cmd ports.RelocateGoatsCommand) error {
	if cmd.FromShedID == nil && cmd.FromParkID == nil {
		return nil
	}
	var offending int
	err := tx.QueryRow(ctx, `
SELECT count(*)
FROM goats
WHERE tenant_id = $1::uuid
  AND goat_id = ANY($2::uuid[])
  AND merged_into_goat_id IS NULL
  AND exited_at IS NULL
  AND (
        (nullif($3::text, '') IS NOT NULL AND shed_id IS DISTINCT FROM nullif($3::text, '')::uuid)
     OR (nullif($4::text, '') IS NOT NULL AND park_id IS DISTINCT FROM nullif($4::text, '')::uuid)
      )`,
		cmd.TenantID, cmd.GoatIDs, stringValue(cmd.FromShedID), stringValue(cmd.FromParkID)).Scan(&offending)
	if err != nil {
		return fmt.Errorf("identity: relocate goats: verify expected source placement: %w", err)
	}
	if offending > 0 {
		return fmt.Errorf(
			"%w: relocate goats: %d animal(s) are no longer at the approved source placement (expected shed %s, park %s) — a newer relocation moved them, so this stale movement must be reconciled rather than applied",
			ports.ErrWriteConflict, offending, stringValue(cmd.FromShedID), stringValue(cmd.FromParkID))
	}
	return nil
}

func (r *Repository) ensureShedUnderPark(ctx context.Context, tenantID, shedID, parkID string) error {
	var ok bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM locations shed
  WHERE shed.tenant_id = $1::uuid
    AND shed.location_id = $2::uuid
    AND shed.location_type = 'shed'
    AND shed.status = 'active'
    AND shed.parent_location_id = $3::uuid
)`, tenantID, shedID, parkID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ports.ErrWriteConflict
	}
	return nil
}

// ResolveGoatForReproductiveBulkUpdate resolves whether a bulk-import row's
// identifiers point to a single existing goat that can take a reproductive
// update. It reuses the same lifetime-ownership lookup used for create conflict
// detection: every provided identifier must resolve to the SAME goat (any that
// resolves elsewhere makes the match ambiguous and is rejected). Match
// confidence is the fraction of provided identifiers that matched that goat.
func (r *Repository) ResolveGoatForReproductiveBulkUpdate(ctx context.Context, cmd ports.ResolveReproductiveMatchCommand) (ports.ReproductiveMatchResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var result ports.ReproductiveMatchResult
	if len(cmd.Identifiers) == 0 {
		return result, nil
	}
	matchedGoat := ""
	matchedCount := 0
	for _, identifier := range cmd.Identifiers {
		goatID, err := r.identifierLifetimeConflict(ctx, cmd.TenantID, identifier.NormalizedValue)
		if err != nil {
			return ports.ReproductiveMatchResult{}, err
		}
		if goatID == "" {
			continue
		}
		if matchedGoat == "" {
			matchedGoat = goatID
		} else if matchedGoat != goatID {
			// Identifiers point at different goats: ambiguous, not a clean match.
			return ports.ReproductiveMatchResult{}, nil
		}
		matchedCount++
	}
	if matchedGoat == "" {
		return result, nil
	}

	var lifecycle, reproductive string
	var rowVersion int
	var merged bool
	err := r.pool.QueryRow(ctx, `
SELECT lifecycle_status, COALESCE(reproductive_status, ''), row_version, merged_into_goat_id IS NOT NULL
FROM goats
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, cmd.TenantID, matchedGoat).Scan(&lifecycle, &reproductive, &rowVersion, &merged)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return ports.ReproductiveMatchResult{}, err
	}
	result.GoatID = matchedGoat
	result.CurrentReproductiveStatus = reproductive
	result.RowVersion = rowVersion
	result.MatchConfidence = float64(matchedCount) / float64(len(cmd.Identifiers))
	if merged || exitedLifecycleStatus(lifecycle) {
		result.Blocked = true
		if merged {
			result.BlockReason = "goat identity has been merged into a survivor"
		} else {
			result.BlockReason = "goat has already exited the active herd"
		}
		return result, nil
	}
	result.Matched = true
	return result, nil
}

func (r *Repository) identifierLifetimeConflict(ctx context.Context, tenantID, normalizedValue string) (string, error) {
	var goatID string
	err := r.pool.QueryRow(ctx, `
SELECT goat_id::text
FROM goat_identifiers
WHERE tenant_id = $1::uuid
  AND normalized_value = $2
LIMIT 1`, tenantID, normalizedValue).Scan(&goatID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return goatID, err
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func dateValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func uuidArg(value pgtype.UUID) any {
	if !value.Valid {
		return nil
	}
	return value.String()
}
