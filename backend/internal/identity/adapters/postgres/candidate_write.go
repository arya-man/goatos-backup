package postgres

import (
	"context"
	"encoding/base64"
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
	candidateResultType         = "identity_match_candidate"
	candidateAggregate          = "identity_match_candidate"
	candidateRejectAuditAction  = "identity.match_candidate.rejected"
	candidateApproveAuditAction = "identity.match_candidate.approved"
)

type candidateCursor struct {
	CreatedAt   string `json:"created_at"`
	CandidateID string `json:"candidate_id"`
}

type candidateDecisionEvidence struct {
	EvidenceRefs   []domain.EvidenceRef `json:"evidence_refs"`
	Reason         string               `json:"reason"`
	DecisionRecord json.RawMessage      `json:"decision_record"`
}

func (r *Repository) ListCandidates(ctx context.Context, params ports.ListCandidatesParams) ([]domain.CandidateSummary, *string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(params.TenantID)
	if err != nil {
		return nil, nil, err
	}
	cursorCreatedAt := pgtype.Timestamptz{}
	cursorCandidateID := pgtype.UUID{}
	if params.Cursor != nil && strings.TrimSpace(*params.Cursor) != "" {
		cursorCreatedAt, cursorCandidateID, err = decodeCandidateCursor(*params.Cursor)
		if err != nil {
			return nil, nil, err
		}
	}
	rows, err := r.queries.ListIdentityCandidates(ctx, identitydb.ListIdentityCandidatesParams{
		TenantID:          tenantUUID,
		CursorCreatedAt:   cursorCreatedAt,
		CursorCandidateID: cursorCandidateID,
		LimitCount:        int32(params.Limit + 1),
	})
	if err != nil {
		return nil, nil, err
	}
	items := make([]domain.CandidateSummary, 0, min(len(rows), params.Limit))
	for _, row := range rows {
		candidate, err := candidateSummaryFromListRow(row)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, candidate)
	}
	var next *string
	if len(items) > params.Limit {
		cursor, err := encodeCandidateCursor(items[params.Limit-1])
		if err != nil {
			return nil, nil, err
		}
		next = &cursor
		items = items[:params.Limit]
	}
	return items, next, nil
}

func (r *Repository) RejectCandidate(ctx context.Context, cmd ports.RejectCandidateCommand) (*ports.RejectCandidateResult, error) {
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
	candidateUUID, err := uuidParam(cmd.CandidateID)
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
		return replayRejectedCandidate(ctx, qtx, tenantUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	decisionID, err := qtx.NewUUID(ctx)
	if err != nil {
		return nil, err
	}
	decisionUUID, err := uuidParam(decisionID)
	if err != nil {
		return nil, err
	}
	decisionRecord, err := candidateRejectDecisionRecordPayload(cmd, decisionID, now)
	if err != nil {
		return nil, err
	}
	decisionEvidence, err := json.Marshal(candidateDecisionEvidence{
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
		DecisionType:   rejectMatchDecisionType,
		DecisionResult: "candidate_rejected",
		DecisionState:  "rejected",
		DecidedBy:      actorUUID,
		PolicyVersion:  correctionResolvePolicyVersion,
		ReviewerID:     actorUUID,
		Evidence:       decisionEvidence,
		CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
		ApprovedAt:     pgtype.Timestamptz{},
		DecidedAt:      pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	decision := decisionSummaryFromInsertRow(decisionRow)

	updated, err := qtx.RejectIdentityMatchCandidate(ctx, identitydb.RejectIdentityMatchCandidateParams{
		ReviewedBy:  actorUUID,
		ReviewedAt:  pgtype.Timestamptz{Time: now, Valid: true},
		DecisionID:  decisionUUID,
		TenantID:    tenantUUID,
		CandidateID: candidateUUID,
		RowVersion:  int32(cmd.RowVersion),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, rejectCandidateGuardError(ctx, qtx, tenantUUID, candidateUUID)
	}
	if err != nil {
		return nil, err
	}
	candidate, err := candidateSummaryFromRejectRow(updated)
	if err != nil {
		return nil, err
	}

	afterState, err := json.Marshal(map[string]any{
		"candidate": candidate,
		"decision":  decision,
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
		Action:       candidateRejectAuditAction,
		ResourceType: candidateAggregate,
		ResourceID:   candidateUUID,
		ScopeType:    pgtype.Text{},
		ScopeID:      pgtype.UUID{},
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
	if err := qtx.CompleteIdempotencyKey(ctx, identitydb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(candidateResultType),
		ResultID:       candidateUUID,
		IdempotencyKey: cmd.StoredIdempotencyKey,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	return &ports.RejectCandidateResult{
		Candidate: candidate,
		Decision:  decision,
	}, nil
}

func replayRejectedCandidate(ctx context.Context, qtx *identitydb.Queries, tenantUUID pgtype.UUID, key string, requestHash string) (*ports.RejectCandidateResult, error) {
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
	if idempotency.Status != "completed" || idempotency.ResultType != candidateResultType || strings.TrimSpace(idempotency.ResultID) == "" {
		return nil, ports.ErrIdempotencyPending
	}
	candidateUUID, err := uuidParam(idempotency.ResultID)
	if err != nil {
		return nil, err
	}
	row, err := qtx.GetCandidateForReview(ctx, identitydb.GetCandidateForReviewParams{
		TenantID:    tenantUUID,
		CandidateID: candidateUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(row.DecisionID) == "" {
		return nil, ports.ErrIdempotencyPending
	}
	candidate, err := candidateSummaryFromReviewRow(row)
	if err != nil {
		return nil, err
	}
	decisionUUID, err := uuidParam(row.DecisionID)
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
	return &ports.RejectCandidateResult{
		Candidate:     candidate,
		Decision:      decisionSummaryFromGetRow(decisionRow),
		Replayed:      true,
		FirstResultID: &firstResultID,
	}, nil
}

func rejectCandidateGuardError(ctx context.Context, qtx *identitydb.Queries, tenantUUID, candidateUUID pgtype.UUID) error {
	row, err := qtx.GetCandidateForReview(ctx, identitydb.GetCandidateForReviewParams{
		TenantID:    tenantUUID,
		CandidateID: candidateUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.State == "approved" || row.State == "rejected" || row.State == "expired" {
		return ports.ErrWriteConflict
	}
	return ports.ErrWriteConflict
}

func candidateRejectDecisionRecordPayload(cmd ports.RejectCandidateCommand, decisionID string, createdAt time.Time) ([]byte, error) {
	payload := map[string]any{
		"decision_id":     decisionID,
		"decision_type":   rejectMatchDecisionType,
		"decision_result": "candidate_rejected",
		"decision_state":  "rejected",
		"decided_by_type": "human",
		"decided_by":      cmd.ActorID,
		"reviewer_id":     cmd.ActorID,
		"policy_version":  correctionResolvePolicyVersion,
		"reason":          cmd.Reason,
		"evidence": map[string]any{
			"evidence_refs": cmd.EvidenceRefs,
			"after": map[string]any{
				"candidate_id":    cmd.CandidateID,
				"target_state":    "rejected",
				"decision_type":   rejectMatchDecisionType,
				"decision_result": "candidate_rejected",
				"decision_state":  "rejected",
			},
		},
		"idempotency_key": cmd.StoredIdempotencyKey,
		"trace_id":        cmd.TraceID,
		"created_at":      createdAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"approved_at":     nil,
		"decided_at":      createdAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
	}
	return json.Marshal(payload)
}

func candidateSummaryFromListRow(row identitydb.ListIdentityCandidatesRow) (domain.CandidateSummary, error) {
	return candidateSummaryFromFields(row.CandidateID, row.ProposedGoatID, row.CandidateGoatID, row.MatchScore, row.MatchReasons, row.State, row.CreatedBy, row.RowVersion, row.CreatedAt)
}

func candidateSummaryFromReviewRow(row identitydb.GetCandidateForReviewRow) (domain.CandidateSummary, error) {
	return candidateSummaryFromFields(row.CandidateID, row.ProposedGoatID, row.CandidateGoatID, row.MatchScore, row.MatchReasons, row.State, row.CreatedBy, row.RowVersion, row.CreatedAt)
}

func candidateSummaryFromRejectRow(row identitydb.RejectIdentityMatchCandidateRow) (domain.CandidateSummary, error) {
	return candidateSummaryFromFields(row.CandidateID, row.ProposedGoatID, row.CandidateGoatID, row.MatchScore, row.MatchReasons, row.State, row.CreatedBy, row.RowVersion, row.CreatedAt)
}

func candidateSummaryFromFields(candidateID, proposedGoatID, candidateGoatID string, matchScore float64, matchReasonsBytes []byte, state, createdBy string, rowVersion int32, createdAt pgtype.Timestamptz) (domain.CandidateSummary, error) {
	reasons := []string{}
	if len(matchReasonsBytes) > 0 {
		if err := json.Unmarshal(matchReasonsBytes, &reasons); err != nil {
			return domain.CandidateSummary{}, err
		}
	}
	return domain.CandidateSummary{
		CandidateID:     candidateID,
		ProposedGoatID:  nonEmptyStringPtr(proposedGoatID),
		CandidateGoatID: nonEmptyStringPtr(candidateGoatID),
		MatchScore:      matchScore,
		MatchReasons:    reasons,
		State:           state,
		CreatedBy:       createdBy,
		RowVersion:      int(rowVersion),
		CreatedAt:       createdAt.Time,
	}, nil
}

func (r *Repository) ApproveCandidate(ctx context.Context, cmd ports.ApproveCandidateCommand) (*ports.ApproveCandidateResult, error) {
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
	candidateUUID, err := uuidParam(cmd.CandidateID)
	if err != nil {
		return nil, err
	}

	switch cmd.DecisionType {
	case attachIdentifierDecisionType:
		return r.approveAttachIdentifier(ctx, cmd, tenantUUID, actorUUID, candidateUUID)
	case mergeDecisionType:
		return r.approveMergeGoats(ctx, cmd, tenantUUID, actorUUID, candidateUUID)
	default:
		return nil, ports.ErrWriteConflict
	}
}

func (r *Repository) approveAttachIdentifier(ctx context.Context, cmd ports.ApproveCandidateCommand, tenantUUID, actorUUID, candidateUUID pgtype.UUID) (*ports.ApproveCandidateResult, error) {
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
		return replayApprovedCandidate(ctx, qtx, tenantUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	// Resolve the identifier to attach. Either the reviewer supplies an explicit
	// identifier_action (type/value/scope) or we deterministically extract it from
	// the candidate's linked legacy row. Extraction only supports RFID (global
	// scope): an old-tag park scope cannot be derived safely here, so an old_tag
	// attach must be explicit with an operator-supplied scope_key. We never parse
	// display text.
	identifierType := strings.TrimSpace(cmd.IdentifierType)
	identifierValue := strings.TrimSpace(cmd.IdentifierValue)
	scopeKey := strings.TrimSpace(cmd.ScopeKey)
	if cmd.ExtractFromLegacyRow {
		legacyRowIDStr, err := qtx.GetCandidateLegacyRowID(ctx, identitydb.GetCandidateLegacyRowIDParams{
			TenantID:    tenantUUID,
			CandidateID: candidateUUID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ports.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(legacyRowIDStr) == "" {
			return nil, ports.ErrCannotExtractIdentifier
		}
		legacyRowUUID, err := uuidParam(legacyRowIDStr)
		if err != nil {
			return nil, err
		}
		legacyRow, err := qtx.GetLegacyImportRowForCandidate(ctx, identitydb.GetLegacyImportRowForCandidateParams{
			TenantID:    tenantUUID,
			LegacyRowID: legacyRowUUID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ports.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		rfid := strings.TrimSpace(legacyRow.Rfid)
		if rfid == "" {
			return nil, ports.ErrCannotExtractIdentifier
		}
		identifierType = "rfid"
		identifierValue = rfid
		scopeKey = ""
	}
	if identifierType == "" || identifierValue == "" {
		return nil, ports.ErrWriteConflict
	}
	// RFID is globally scoped by policy; default an unset scope to global.
	// Non-global identifier types (old_tag, etc.) must carry an explicit scope_key.
	if identifierType == "rfid" && scopeKey == "" {
		scopeKey = "global"
	}
	normalizedValue := identifierValue
	if identifierType == "rfid" {
		normalizedValue = strings.ToUpper(identifierValue)
	}

	goatUUID, err := uuidParam(cmd.GoatID)
	if err != nil {
		return nil, err
	}
	if err := guardGoatForIdentifierMutation(ctx, qtx, tenantUUID, goatUUID, cmd.GoatRowVersion, now); err != nil {
		return nil, err
	}

	policy, err := qtx.GetIdentifierPolicy(ctx, identitydb.GetIdentifierPolicyParams{
		PolicyVersion:  identifierPolicyVersion,
		IdentifierType: identifierType,
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
		IdentifierType:    identifierType,
		IdentifierValue:   identifierValue,
		NormalizedValue:   normalizedValue,
		ScopeKey:          scopeKey,
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

	// Single transaction: write the attach decision/identifier/event using the
	// same payload builders as the standalone identifier-attach path, mark the
	// candidate approved against the same decision, and complete the idempotency
	// key with the CANDIDATE result so replay rebuilds from candidate state.
	finish := identifierMutationFinish{
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
		NormalizedValue: normalizedValue,
		DecisionType:    attachIdentifierDecisionType,
		DecisionResult:  attachIdentifierDecisionResult,
		Action:          attachIdentifierAction,
		EventType:       attachIdentifierEventType,
		OccurredAt:      now,
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
	decisionEvidence, err := json.Marshal(map[string]any{
		"evidence_refs":   cmd.EvidenceRefs,
		"reason":          cmd.Reason,
		"candidate_id":    cmd.CandidateID,
		"decision_record": json.RawMessage(decisionRecord),
	})
	if err != nil {
		return nil, err
	}
	decisionRow, err := qtx.InsertIdentityDecision(ctx, identitydb.InsertIdentityDecisionParams{
		DecisionID:     decisionUUID,
		TenantID:       tenantUUID,
		DecisionType:   attachIdentifierDecisionType,
		DecisionResult: attachIdentifierDecisionResult,
		DecisionState:  "approved",
		DecidedBy:      actorUUID,
		PolicyVersion:  identifierPolicyVersion,
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

	identifierUUID, err := uuidParam(identifier.IdentifierID)
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertIdentityDecisionIdentifier(ctx, identitydb.InsertIdentityDecisionIdentifierParams{
		DecisionID:      decisionUUID,
		TenantID:        tenantUUID,
		IdentifierID:    identifierUUID,
		IdentifierType:  textParam(identifier.IdentifierType),
		IdentifierValue: textParam(identifier.IdentifierValue),
		Action:          attachIdentifierAction,
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
		TenantID:        tenantUUID,
		GoatID:          goatUUID,
		EventType:       attachIdentifierEventType,
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

	updated, err := qtx.ApproveIdentityMatchCandidate(ctx, identitydb.ApproveIdentityMatchCandidateParams{
		ReviewedBy:  actorUUID,
		ReviewedAt:  pgtype.Timestamptz{Time: now, Valid: true},
		DecisionID:  decisionUUID,
		TenantID:    tenantUUID,
		CandidateID: candidateUUID,
		RowVersion:  int32(cmd.RowVersion),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, approveCandidateGuardError(ctx, qtx, tenantUUID, candidateUUID)
	}
	if err != nil {
		return nil, err
	}
	candidate, err := candidateSummaryFromApproveRow(updated)
	if err != nil {
		return nil, err
	}

	events := []domain.EventSummary{{EventID: eventRow.EventID, EventType: attachIdentifierEventType}}
	response, err := adminGoatMutationResult(ctx, qtx, tenantUUID, cmd.TenantID, goatUUID, decision, events, false, nil)
	if err != nil {
		return nil, err
	}

	afterState, err := json.Marshal(map[string]any{
		"candidate_id": cmd.CandidateID,
		"state":        updated.State,
		"decision":     decision,
		"identifier":   identifier,
		"events":       events,
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
		"identifier_id":          identifier.IdentifierID,
		"goat_id":                cmd.GoatID,
	})
	if err != nil {
		return nil, err
	}
	visibility := locationScopeFromSummary(response.Goat)
	if err := qtx.InsertAuditLog(ctx, identitydb.InsertAuditLogParams{
		TenantID:     tenantUUID,
		ActorID:      actorUUID,
		Action:       candidateApproveAuditAction,
		ResourceType: candidateAggregate,
		ResourceID:   candidateUUID,
		ScopeType:    nullableText(nonEmptyStringPtr(scopeType(visibility))),
		ScopeID:      nullableUUID(scopeID(visibility)),
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

	envelope, err := identifierDomainEventEnvelope(finish, response.Goat, decision, eventRow.EventID)
	if err != nil {
		return nil, err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":               cmd.ActorID,
		"client_idempotency_key": cmd.ClientIdempotencyKey,
		"trace_id":               cmd.TraceID,
		"decision_id":            decision.DecisionID,
		"identifier_id":          identifier.IdentifierID,
		"candidate_id":           cmd.CandidateID,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertOutboxMessage(ctx, identitydb.InsertOutboxMessageParams{
		TenantID:       tenantUUID,
		EventID:        eventUUID,
		EventType:      attachIdentifierEventType,
		SchemaVersion:  eventSchemaVersion,
		AggregateType:  identifierAggregate,
		AggregateID:    goatUUID,
		Topic:          identifierTopic,
		Payload:        envelope,
		Headers:        headers,
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TraceID:        nullableText(nonEmptyStringPtr(cmd.TraceID)),
	}); err != nil {
		return nil, err
	}

	if err := qtx.CompleteIdempotencyKey(ctx, identitydb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(candidateResultType),
		ResultID:       candidateUUID,
		IdempotencyKey: cmd.StoredIdempotencyKey,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	return &ports.ApproveCandidateResult{
		Candidate: candidate,
		Decision:  decision,
		Events:    events,
	}, nil
}

func (r *Repository) approveMergeGoats(ctx context.Context, cmd ports.ApproveCandidateCommand, tenantUUID, actorUUID, candidateUUID pgtype.UUID) (*ports.ApproveCandidateResult, error) {
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
		return replayApprovedCandidate(ctx, qtx, tenantUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	decisionID, err := qtx.NewUUID(ctx)
	if err != nil {
		return nil, err
	}
	decisionUUID, err := uuidParam(decisionID)
	if err != nil {
		return nil, err
	}

	decisionRecord, err := candidateMergeDecisionRecordPayload(cmd, decisionID, cmd.SurvivorGoatID, requestedMergedGoatIDs(cmd.SurvivorGoatID, cmd.AffectedGoatIDs), nil, nil, now)
	if err != nil {
		return nil, err
	}
	decisionEvidence, err := json.Marshal(candidateDecisionEvidence{
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
		DecisionType:   mergeDecisionType,
		DecisionResult: mergeDecisionResult,
		DecisionState:  "approved",
		DecidedBy:      actorUUID,
		PolicyVersion:  mergePolicyVersion,
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

	updated, err := qtx.ApproveIdentityMatchCandidate(ctx, identitydb.ApproveIdentityMatchCandidateParams{
		ReviewedBy:  actorUUID,
		ReviewedAt:  pgtype.Timestamptz{Time: now, Valid: true},
		DecisionID:  decisionUUID,
		TenantID:    tenantUUID,
		CandidateID: candidateUUID,
		RowVersion:  int32(cmd.RowVersion),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, approveCandidateGuardError(ctx, qtx, tenantUUID, candidateUUID)
	}
	if err != nil {
		return nil, err
	}
	candidate, err := candidateSummaryFromApproveRow(updated)
	if err != nil {
		return nil, err
	}

	// Bind the merge to the candidate's own goats: the survivor and every affected
	// goat must be one of the candidate's proposed/candidate goats. This prevents a
	// candidate approve from merging unrelated goats.
	candidateGoatSet := map[string]struct{}{}
	if id := strings.TrimSpace(updated.ProposedGoatID); id != "" {
		candidateGoatSet[id] = struct{}{}
	}
	if id := strings.TrimSpace(updated.CandidateGoatID); id != "" {
		candidateGoatSet[id] = struct{}{}
	}
	if _, ok := candidateGoatSet[cmd.SurvivorGoatID]; !ok {
		return nil, ports.ErrWriteConflict
	}
	for _, affectedID := range cmd.AffectedGoatIDs {
		if _, ok := candidateGoatSet[affectedID]; !ok {
			return nil, ports.ErrWriteConflict
		}
	}

	if _, err := tx.Exec(ctx, "SET LOCAL goatos.allow_merged_goat_update = 'on'"); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL goatos.allow_merged_goat_child_write = 'on'"); err != nil {
		return nil, err
	}

	initialIDs := append([]string{cmd.SurvivorGoatID}, cmd.AffectedGoatIDs...)
	goats, err := lockMergeGraph(ctx, qtx, tenantUUID, initialIDs)
	if err != nil {
		return nil, err
	}
	finalSurvivorID, survivorWarnings, err := resolveMergeRedirect(cmd.SurvivorGoatID, goats)
	if err != nil {
		return nil, err
	}
	finalSurvivor := goats[finalSurvivorID]
	if finalSurvivor.IdentityState == "merged" {
		return nil, ports.ErrWriteConflict
	}

	redirectWarnings := append([]domain.MergeRedirectWarning{}, survivorWarnings...)
	liveLoserSet := map[string]struct{}{}
	repointSet := map[string]struct{}{}
	for _, affectedID := range cmd.AffectedGoatIDs {
		if affectedID == finalSurvivorID {
			continue
		}
		resolvedID, warnings, err := resolveMergeRedirect(affectedID, goats)
		if err != nil {
			return nil, err
		}
		redirectWarnings = append(redirectWarnings, warnings...)
		if resolvedID != affectedID {
			repointSet[affectedID] = struct{}{}
		}
		if resolvedID != finalSurvivorID {
			liveLoserSet[resolvedID] = struct{}{}
		}
	}
	delete(liveLoserSet, finalSurvivorID)
	if len(liveLoserSet) == 0 {
		return nil, ports.ErrWriteConflict
	}

	liveLoserIDs := sortedKeys(liveLoserSet)
	redirecters, err := lockRedirectingGoats(ctx, qtx, tenantUUID, liveLoserIDs)
	if err != nil {
		return nil, err
	}
	for goatID := range redirecters {
		if goatID != finalSurvivorID {
			repointSet[goatID] = struct{}{}
		}
	}

	identifierActions, err := applyMergeIdentifierActions(ctx, qtx, tenantUUID, actorUUID, finalSurvivorID, liveLoserIDs, cmd.IdentifierActions, now)
	if err != nil {
		return nil, err
	}
	if hasTransferredIdentifier(identifierActions) {
		if err := touchGoatsForIdentityMutation(ctx, qtx, tenantUUID, []string{finalSurvivorID}, now); err != nil {
			return nil, err
		}
	}

	if err := qtx.InsertIdentityDecisionGoat(ctx, identitydb.InsertIdentityDecisionGoatParams{
		DecisionID: decisionUUID,
		TenantID:   tenantUUID,
		GoatID:     mustUUID(finalSurvivorID),
		Role:       "survivor",
	}); err != nil {
		return nil, err
	}
	for _, loserID := range liveLoserIDs {
		if err := qtx.InsertIdentityDecisionGoat(ctx, identitydb.InsertIdentityDecisionGoatParams{
			DecisionID: decisionUUID,
			TenantID:   tenantUUID,
			GoatID:     mustUUID(loserID),
			Role:       "merged",
		}); err != nil {
			return nil, err
		}
	}
	for _, action := range identifierActions {
		if err := insertDecisionIdentifierAction(ctx, qtx, tenantUUID, decisionUUID, action); err != nil {
			return nil, err
		}
	}

	for _, loserID := range liveLoserIDs {
		if err := qtx.MarkGoatMerged(ctx, identitydb.MarkGoatMergedParams{
			SurvivorGoatID: mustUUID(finalSurvivorID),
			UpdatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
			TenantID:       tenantUUID,
			MergedGoatID:   mustUUID(loserID),
		}); err != nil {
			return nil, err
		}
	}
	for _, goatID := range sortedKeys(repointSet) {
		if goatID == finalSurvivorID {
			continue
		}
		if err := qtx.RepointMergedGoatRedirect(ctx, identitydb.RepointMergedGoatRedirectParams{
			SurvivorGoatID: mustUUID(finalSurvivorID),
			UpdatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
			TenantID:       tenantUUID,
			GoatID:         mustUUID(goatID),
		}); err != nil {
			return nil, err
		}
	}

	mergeLinkIDs := make([]string, 0, len(liveLoserIDs))
	for _, loserID := range liveLoserIDs {
		linkID, err := qtx.InsertGoatMergeLink(ctx, identitydb.InsertGoatMergeLinkParams{
			TenantID:       tenantUUID,
			SurvivorGoatID: mustUUID(finalSurvivorID),
			MergedGoatID:   mustUUID(loserID),
			DecisionID:     decisionUUID,
			Reason:         cmd.Reason,
			CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
			CreatedBy:      actorUUID,
		})
		if err != nil {
			return nil, err
		}
		mergeLinkIDs = append(mergeLinkIDs, linkID)
	}

	decisionRecord, err = candidateMergeDecisionRecordPayload(cmd, decisionID, finalSurvivorID, liveLoserIDs, redirectWarnings, identifierActions, now)
	if err != nil {
		return nil, err
	}
	decisionEvidence, err = json.Marshal(candidateDecisionEvidence{
		EvidenceRefs:   cmd.EvidenceRefs,
		Reason:         cmd.Reason,
		DecisionRecord: decisionRecord,
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE identity_decisions SET evidence = $1 WHERE tenant_id = $2 AND decision_id = $3`, decisionEvidence, tenantUUID, decisionUUID); err != nil {
		return nil, err
	}

	events := make([]domain.EventSummary, 0, len(liveLoserIDs))
	visibility := locationScopeFromMergeRow(finalSurvivor)
	for _, loserID := range liveLoserIDs {
		eventID, err := qtx.NewUUID(ctx)
		if err != nil {
			return nil, err
		}
		eventUUID, err := uuidParam(eventID)
		if err != nil {
			return nil, err
		}
		eventPayload, err := candidateMergeEventPayload(cmd, finalSurvivorID, liveLoserIDs, loserID, decision.DecisionID, mergeLinkIDs, identifierActions, redirectWarnings)
		if err != nil {
			return nil, err
		}
		eventRow, err := qtx.InsertGoatIdentityEvent(ctx, identitydb.InsertGoatIdentityEventParams{
			IdentityEventID: eventUUID,
			TenantID:        tenantUUID,
			GoatID:          mustUUID(finalSurvivorID),
			EventType:       mergeEventType,
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
		envelope, err := candidateMergeDomainEventEnvelope(cmd, eventRow.EventID, eventRow.RecordedAt.Time, visibility, finalSurvivorID, liveLoserIDs, loserID, decision.DecisionID, mergeLinkIDs, identifierActions, redirectWarnings)
		if err != nil {
			return nil, err
		}
		headers, err := json.Marshal(map[string]any{
			"actor_id":               cmd.ActorID,
			"client_idempotency_key": cmd.ClientIdempotencyKey,
			"trace_id":               cmd.TraceID,
			"decision_id":            decision.DecisionID,
			"merged_goat_id":         loserID,
		})
		if err != nil {
			return nil, err
		}
		if err := qtx.InsertOutboxMessage(ctx, identitydb.InsertOutboxMessageParams{
			TenantID:       tenantUUID,
			EventID:        eventUUID,
			EventType:      mergeEventType,
			SchemaVersion:  eventSchemaVersion,
			AggregateType:  mergeAggregateType,
			AggregateID:    mustUUID(finalSurvivorID),
			Topic:          identifierTopic,
			Payload:        envelope,
			Headers:        headers,
			IdempotencyKey: cmd.StoredIdempotencyKey,
			TraceID:        nullableText(nonEmptyStringPtr(cmd.TraceID)),
		}); err != nil {
			return nil, err
		}
		events = append(events, domain.EventSummary{EventID: eventRow.EventID, EventType: mergeEventType})
	}

	merge := domain.MergeResult{
		SurvivorGoatID:      finalSurvivorID,
		MergedGoatIDs:       liveLoserIDs,
		RedirectWarnings:    redirectWarnings,
		AffectedIdentifiers: identifierActions,
	}
	afterState, err := json.Marshal(map[string]any{
		"candidate_id": cmd.CandidateID,
		"state":        updated.State,
		"decision":     decision,
		"merge":        merge,
		"events":       events,
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
		"survivor_goat_id":       finalSurvivorID,
		"merged_goat_ids":        liveLoserIDs,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertAuditLog(ctx, identitydb.InsertAuditLogParams{
		TenantID:     tenantUUID,
		ActorID:      actorUUID,
		Action:       candidateApproveAuditAction,
		ResourceType: candidateAggregate,
		ResourceID:   candidateUUID,
		ScopeType:    nullableText(nonEmptyStringPtr(scopeType(visibility))),
		ScopeID:      nullableUUID(scopeID(visibility)),
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
	if err := qtx.CompleteIdempotencyKey(ctx, identitydb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(candidateResultType),
		ResultID:       candidateUUID,
		IdempotencyKey: cmd.StoredIdempotencyKey,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	return &ports.ApproveCandidateResult{
		Candidate: candidate,
		Decision:  decision,
		Merge:     &merge,
		Events:    events,
	}, nil
}

func replayApprovedCandidate(ctx context.Context, qtx *identitydb.Queries, tenantUUID pgtype.UUID, key string, requestHash string) (*ports.ApproveCandidateResult, error) {
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
	if idempotency.Status != "completed" || strings.TrimSpace(idempotency.ResultID) == "" {
		return nil, ports.ErrIdempotencyPending
	}

	firstResultID := idempotency.ResultID

	// Both approve outcomes (attach_identifier and merge_goats) complete with the
	// candidate result type and are rebuilt from canonical candidate + decision
	// state below. For attach there are no merge links, so the reconstructed
	// merge stays nil and events carry the goat.identifier.added event.
	if idempotency.ResultType != candidateResultType {
		return nil, ports.ErrIdempotencyPending
	}
	candidateUUID, err := uuidParam(idempotency.ResultID)
	if err != nil {
		return nil, err
	}
	row, err := qtx.GetCandidateForReview(ctx, identitydb.GetCandidateForReviewParams{
		TenantID:    tenantUUID,
		CandidateID: candidateUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(row.DecisionID) == "" {
		return nil, ports.ErrIdempotencyPending
	}
	candidate, err := candidateSummaryFromReviewRow(row)
	if err != nil {
		return nil, err
	}
	decisionUUID, err := uuidParam(row.DecisionID)
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
	links, err := qtx.ListMergeLinksByDecision(ctx, identitydb.ListMergeLinksByDecisionParams{
		TenantID:   tenantUUID,
		DecisionID: decisionUUID,
	})
	if err != nil {
		return nil, err
	}
	identifiers, err := qtx.ListDecisionIdentifiers(ctx, identitydb.ListDecisionIdentifiersParams{
		TenantID:   tenantUUID,
		DecisionID: decisionUUID,
	})
	if err != nil {
		return nil, err
	}
	eventRows, err := qtx.ListDecisionEvents(ctx, identitydb.ListDecisionEventsParams{
		TenantID:   tenantUUID,
		DecisionID: decisionUUID,
	})
	if err != nil {
		return nil, err
	}
	evidence, err := qtx.GetDecisionEvidenceByID(ctx, identitydb.GetDecisionEvidenceByIDParams{
		TenantID:   tenantUUID,
		DecisionID: decisionUUID,
	})
	if err != nil {
		return nil, err
	}
	actions := make([]domain.IdentifierAction, 0, len(identifiers))
	for _, id := range identifiers {
		actions = append(actions, domain.IdentifierAction{
			IdentifierID:    nonEmptyStringPtr(id.IdentifierID),
			IdentifierType:  pgTextPtr(id.IdentifierType),
			IdentifierValue: pgTextPtr(id.IdentifierValue),
			Action:          id.Action,
		})
	}
	mergedIDs := make([]string, 0, len(links))
	survivorID := ""
	for _, link := range links {
		if survivorID == "" {
			survivorID = link.SurvivorGoatID
		}
		mergedIDs = append(mergedIDs, link.MergedGoatID)
	}
	events := make([]domain.EventSummary, 0, len(eventRows))
	for _, ev := range eventRows {
		events = append(events, domain.EventSummary{EventID: ev.EventID, EventType: ev.EventType})
	}
	var merge *domain.MergeResult
	if decisionRow.DecisionType == mergeDecisionType {
		warnings := redirectWarningsFromDecisionEvidence(evidence)
		merge = &domain.MergeResult{
			SurvivorGoatID:      survivorID,
			MergedGoatIDs:       mergedIDs,
			RedirectWarnings:    warnings,
			AffectedIdentifiers: actions,
		}
	}
	return &ports.ApproveCandidateResult{
		Candidate:     candidate,
		Decision:      decisionSummaryFromGetRow(decisionRow),
		Merge:         merge,
		Events:        events,
		Replayed:      true,
		FirstResultID: &firstResultID,
	}, nil
}

func approveCandidateGuardError(ctx context.Context, qtx *identitydb.Queries, tenantUUID, candidateUUID pgtype.UUID) error {
	row, err := qtx.GetCandidateForReview(ctx, identitydb.GetCandidateForReviewParams{
		TenantID:    tenantUUID,
		CandidateID: candidateUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.State == "approved" || row.State == "rejected" || row.State == "expired" {
		return ports.ErrWriteConflict
	}
	return ports.ErrWriteConflict
}

func candidateSummaryFromApproveRow(row identitydb.ApproveIdentityMatchCandidateRow) (domain.CandidateSummary, error) {
	return candidateSummaryFromFields(row.CandidateID, row.ProposedGoatID, row.CandidateGoatID, row.MatchScore, row.MatchReasons, row.State, row.CreatedBy, row.RowVersion, row.CreatedAt)
}

func candidateMergeDecisionRecordPayload(cmd ports.ApproveCandidateCommand, decisionID string, survivorID string, mergedGoatIDs []string, redirectWarnings []domain.MergeRedirectWarning, identifierActions []domain.IdentifierAction, createdAt time.Time) ([]byte, error) {
	affected := []map[string]string{{"goat_id": survivorID, "role": "survivor"}}
	for _, goatID := range mergedGoatIDs {
		if goatID != survivorID {
			affected = append(affected, map[string]string{"goat_id": goatID, "role": "merged"})
		}
	}
	payload := map[string]any{
		"decision_id":     decisionID,
		"decision_type":   mergeDecisionType,
		"decision_result": mergeDecisionResult,
		"decision_state":  "approved",
		"decided_by_type": "human",
		"decided_by":      cmd.ActorID,
		"reviewer_id":     cmd.ActorID,
		"policy_version":  mergePolicyVersion,
		"reason":          cmd.Reason,
		"evidence": map[string]any{
			"evidence_refs": cmd.EvidenceRefs,
			"after": map[string]any{
				"survivor_goat_id":            survivorID,
				"merged_goat_ids":             mergedGoatIDs,
				"requested_survivor_goat_id":  cmd.SurvivorGoatID,
				"requested_affected_goat_ids": cmd.AffectedGoatIDs,
				"redirect_warnings":           redirectWarnings,
				"identifier_actions":          identifierActions,
				"candidate_id":                cmd.CandidateID,
				"decision_result":             mergeDecisionResult,
				"decision_type":               mergeDecisionType,
				"decision_state":              "approved",
				"merge_policy_source":         mergePolicyVersion,
			},
		},
		"affected_goats":     affected,
		"identifier_actions": identifierActions,
		"idempotency_key":    cmd.StoredIdempotencyKey,
		"trace_id":           cmd.TraceID,
		"created_at":         createdAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"approved_at":        createdAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"decided_at":         createdAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
	}
	return json.Marshal(payload)
}

func candidateMergeEventPayload(cmd ports.ApproveCandidateCommand, survivorID string, mergedIDs []string, thisMergedID string, decisionID string, mergeLinkIDs []string, identifierActions []domain.IdentifierAction, redirectWarnings []domain.MergeRedirectWarning) ([]byte, error) {
	return json.Marshal(map[string]any{
		"candidate_id":                cmd.CandidateID,
		"survivor_goat_id":            survivorID,
		"merged_goat_ids":             mergedIDs,
		"this_merged_goat_id":         thisMergedID,
		"decision_id":                 decisionID,
		"merge_link_ids":              mergeLinkIDs,
		"affected_identifier_actions": identifierActions,
		"redirect_warnings":           redirectWarnings,
	})
}

func candidateMergeDomainEventEnvelope(cmd ports.ApproveCandidateCommand, eventID string, recordedAt time.Time, visibility domain.LocationScope, survivorID string, mergedIDs []string, thisMergedID string, decisionID string, mergeLinkIDs []string, identifierActions []domain.IdentifierAction, redirectWarnings []domain.MergeRedirectWarning) ([]byte, error) {
	envelope := eventEnvelope{
		EventID:         eventID,
		EventType:       mergeEventType,
		SchemaVersion:   eventSchemaVersion,
		SchemaRef:       eventSchemaRef,
		AggregateType:   mergeAggregateType,
		AggregateID:     survivorID,
		OccurredAt:      recordedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		RecordedAt:      recordedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		Producer:        eventProducer{Service: "goatos-api", Module: "identity"},
		IdempotencyKey:  cmd.StoredIdempotencyKey,
		Actor:           eventActor{ActorType: "human", ActorID: &cmd.ActorID},
		SubjectType:     mergeSubjectType,
		SubjectID:       thisMergedID,
		VisibilityScope: visibility,
		EvidenceRefs:    cmd.EvidenceRefs,
		Payload: map[string]any{
			"candidate_id":                cmd.CandidateID,
			"survivor_goat_id":            survivorID,
			"merged_goat_ids":             mergedIDs,
			"this_merged_goat_id":         thisMergedID,
			"decision_id":                 decisionID,
			"merge_link_ids":              mergeLinkIDs,
			"affected_identifier_actions": identifierActions,
			"redirect_warnings":           redirectWarnings,
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

func encodeCandidateCursor(candidate domain.CandidateSummary) (string, error) {
	payload, err := json.Marshal(candidateCursor{
		CreatedAt:   candidate.CreatedAt.UTC().Format(time.RFC3339Nano),
		CandidateID: candidate.CandidateID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCandidateCursor(raw string) (pgtype.Timestamptz, pgtype.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	var cursor candidateCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	candidateUUID, err := uuidParam(cursor.CandidateID)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ports.ErrInvalidCursor
	}
	return pgtype.Timestamptz{Time: createdAt, Valid: true}, candidateUUID, nil
}
