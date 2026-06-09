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
	candidateResultType        = "identity_match_candidate"
	candidateAggregate         = "identity_match_candidate"
	candidateRejectAuditAction = "identity.match_candidate.rejected"
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
