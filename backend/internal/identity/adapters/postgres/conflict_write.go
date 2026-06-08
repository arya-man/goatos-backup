package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	conflictResultType        = "identity_conflict"
	mergeDecisionType         = "merge_goats"
	mergeDecisionResult       = "same_goat_merge"
	mergeEventType            = "goat.identity.merge_approved"
	mergeAggregateType        = "goat"
	mergeSubjectType          = "goat"
	mergeResourceType         = "identity_conflict"
	mergePolicyVersion        = "phase1-manual-correction-review-v1"
	maxMergeWriteRedirectHops = 16
)

func (r *Repository) ResolveConflict(ctx context.Context, cmd ports.ResolveConflictCommand) (*ports.ResolveConflictResult, error) {
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
	conflictUUID, err := uuidParam(cmd.ConflictID)
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
		return replayResolvedConflict(ctx, qtx, tenantUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	}
	if err != nil {
		return nil, err
	}

	conflict, err := qtx.GetConflictForResolve(ctx, identitydb.GetConflictForResolveParams{
		TenantID:   tenantUUID,
		ConflictID: conflictUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !mergeEligibleConflictTypes[conflict.ConflictType] {
		return nil, ports.ErrWriteConflict
	}

	members, err := qtx.ListConflictMemberGoats(ctx, identitydb.ListConflictMemberGoatsParams{
		TenantID:   tenantUUID,
		ConflictID: conflictUUID,
	})
	if err != nil {
		return nil, err
	}
	memberSet := map[string]struct{}{}
	for _, member := range members {
		memberSet[member.GoatID] = struct{}{}
	}
	if _, ok := memberSet[cmd.SurvivorGoatID]; !ok {
		return nil, ports.ErrWriteConflict
	}
	for _, goatID := range cmd.AffectedGoatIDs {
		if _, ok := memberSet[goatID]; !ok {
			return nil, ports.ErrWriteConflict
		}
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

	decisionRecord, err := mergeDecisionRecordPayload(cmd, decisionID, nil, nil, now)
	if err != nil {
		return nil, err
	}
	decisionEvidence, err := json.Marshal(mergeDecisionEvidence{
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

	resolved, err := qtx.ResolveIdentityConflict(ctx, identitydb.ResolveIdentityConflictParams{
		ResolvedAt: pgtype.Timestamptz{Time: now, Valid: true},
		ResolvedBy: actorUUID,
		DecisionID: decisionUUID,
		TenantID:   tenantUUID,
		ConflictID: conflictUUID,
		RowVersion: int32(cmd.RowVersion),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, resolveConflictGuardError(ctx, qtx, tenantUUID, conflictUUID)
	}
	if err != nil {
		return nil, err
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

	decisionRecord, err = mergeDecisionRecordPayload(cmd, decisionID, redirectWarnings, identifierActions, now)
	if err != nil {
		return nil, err
	}
	decisionEvidence, err = json.Marshal(mergeDecisionEvidence{
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
		eventPayload, err := mergeEventPayload(cmd, finalSurvivorID, liveLoserIDs, loserID, decision.DecisionID, mergeLinkIDs, identifierActions, redirectWarnings)
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
		envelope, err := mergeDomainEventEnvelope(cmd, eventRow.EventID, eventRow.RecordedAt.Time, visibility, finalSurvivorID, liveLoserIDs, loserID, decision.DecisionID, mergeLinkIDs, identifierActions, redirectWarnings)
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
		"conflict_id": cmd.ConflictID,
		"state":       resolved.State,
		"decision":    decision,
		"merge":       merge,
		"events":      events,
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
		Action:       mergeEventType,
		ResourceType: mergeResourceType,
		ResourceID:   conflictUUID,
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
		ResultType:     textParam(conflictResultType),
		ResultID:       conflictUUID,
		IdempotencyKey: cmd.StoredIdempotencyKey,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.ResolveConflictResult{
		ConflictID: cmd.ConflictID,
		State:      resolved.State,
		Decision:   decision,
		Merge:      merge,
		Events:     events,
	}, nil
}

type mergeDecisionEvidence struct {
	EvidenceRefs   []domain.EvidenceRef `json:"evidence_refs"`
	Reason         string               `json:"reason"`
	DecisionRecord json.RawMessage      `json:"decision_record"`
}

type mergeGoatRow struct {
	GoatID           string
	IdentityState    string
	MergedIntoGoatID string
	RowVersion       int32
	FarmID           string
	ParkID           string
	ShedID           string
	CohortID         string
}

func lockMergeGraph(ctx context.Context, qtx *identitydb.Queries, tenantUUID pgtype.UUID, startIDs []string) (map[string]mergeGoatRow, error) {
	known := map[string]mergeGoatRow{}
	frontier := uniqueStrings(startIDs)
	for hop := 0; len(frontier) > 0; hop++ {
		if hop >= maxMergeWriteRedirectHops {
			return nil, ports.ErrWriteConflict
		}
		ids, err := uuidListParam(frontier)
		if err != nil {
			return nil, err
		}
		rows, err := qtx.LockGoatsForMerge(ctx, identitydb.LockGoatsForMergeParams{
			TenantID: tenantUUID,
			GoatIds:  ids,
		})
		if err != nil {
			return nil, err
		}
		if len(rows) != len(frontier) {
			return nil, ports.ErrNotFound
		}
		nextSet := map[string]struct{}{}
		for _, row := range rows {
			known[row.GoatID] = mergeGoatRowFromLock(row)
			if row.MergedIntoGoatID != "" {
				if _, ok := known[row.MergedIntoGoatID]; !ok {
					nextSet[row.MergedIntoGoatID] = struct{}{}
				}
			}
		}
		frontier = sortedKeys(nextSet)
	}
	return known, nil
}

func lockRedirectingGoats(ctx context.Context, qtx *identitydb.Queries, tenantUUID pgtype.UUID, startIDs []string) (map[string]mergeGoatRow, error) {
	known := map[string]mergeGoatRow{}
	frontier := uniqueStrings(startIDs)
	for hop := 0; len(frontier) > 0; hop++ {
		if hop >= maxMergeWriteRedirectHops {
			return nil, ports.ErrWriteConflict
		}
		ids, err := uuidListParam(frontier)
		if err != nil {
			return nil, err
		}
		rows, err := qtx.ListGoatsRedirectingTo(ctx, identitydb.ListGoatsRedirectingToParams{
			TenantID: tenantUUID,
			GoatIds:  ids,
		})
		if err != nil {
			return nil, err
		}
		nextSet := map[string]struct{}{}
		for _, row := range rows {
			if _, ok := known[row.GoatID]; ok {
				continue
			}
			known[row.GoatID] = mergeGoatRowFromRedirect(row)
			nextSet[row.GoatID] = struct{}{}
		}
		frontier = sortedKeys(nextSet)
	}
	return known, nil
}

func resolveMergeRedirect(startID string, goats map[string]mergeGoatRow) (string, []domain.MergeRedirectWarning, error) {
	current := startID
	visited := map[string]struct{}{}
	warnings := []domain.MergeRedirectWarning{}
	for hop := 0; hop < maxMergeWriteRedirectHops; hop++ {
		row, ok := goats[current]
		if !ok {
			return "", nil, ports.ErrNotFound
		}
		if row.IdentityState != "merged" {
			return current, warnings, nil
		}
		if _, ok := visited[current]; ok {
			return "", nil, ports.ErrWriteConflict
		}
		visited[current] = struct{}{}
		if row.MergedIntoGoatID == "" {
			return "", nil, ports.ErrWriteConflict
		}
		warnings = append(warnings, domain.MergeRedirectWarning{
			OriginalGoatID: current,
			RedirectGoatID: row.MergedIntoGoatID,
			Code:           "merged_redirect_flattened",
			Message:        "Merge used the live survivor from an existing redirect.",
		})
		current = row.MergedIntoGoatID
	}
	return "", nil, ports.ErrWriteConflict
}

func applyMergeIdentifierActions(ctx context.Context, qtx *identitydb.Queries, tenantUUID, actorUUID pgtype.UUID, survivorID string, loserIDs []string, supplied []domain.IdentifierAction, now time.Time) ([]domain.IdentifierAction, error) {
	allIDs := append([]string{survivorID}, loserIDs...)
	ids, err := uuidListParam(allIDs)
	if err != nil {
		return nil, err
	}
	rows, err := qtx.ListActiveIdentifiersForGoatsForUpdate(ctx, identitydb.ListActiveIdentifiersForGoatsForUpdateParams{
		TenantID: tenantUUID,
		GoatIds:  ids,
	})
	if err != nil {
		return nil, err
	}
	actionByID := map[string]domain.IdentifierAction{}
	for _, action := range supplied {
		if action.IdentifierID != nil {
			actionByID[*action.IdentifierID] = action
		}
	}
	survivorKeys := map[string]struct{}{}
	involvedIDs := map[string]identitydb.ListActiveIdentifiersForGoatsForUpdateRow{}
	for _, row := range rows {
		involvedIDs[row.IdentifierID] = row
		if row.GoatID == survivorID {
			for _, key := range identifierUniqueKeys(row) {
				survivorKeys[key] = struct{}{}
			}
		}
	}
	for _, action := range supplied {
		if action.IdentifierID == nil {
			continue
		}
		if _, ok := involvedIDs[*action.IdentifierID]; !ok {
			return nil, ports.ErrWriteConflict
		}
	}
	applied := make([]domain.IdentifierAction, 0, len(supplied))
	for _, row := range rows {
		if row.GoatID == survivorID {
			continue
		}
		action, hasAction := actionByID[row.IdentifierID]
		collides := false
		for _, key := range identifierUniqueKeys(row) {
			if _, ok := survivorKeys[key]; ok {
				collides = true
				break
			}
		}
		if collides && (!hasAction || (action.Action != "retire" && action.Action != "reject")) {
			return nil, ports.ErrWriteConflict
		}
		if !hasAction {
			continue
		}
		switch action.Action {
		case "retire", "reject":
			updated, err := qtx.RetireIdentifierForMerge(ctx, identitydb.RetireIdentifierForMergeParams{
				ValidTo:      pgtype.Timestamptz{Time: now, Valid: true},
				ApprovedBy:   actorUUID,
				UpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
				TenantID:     tenantUUID,
				IdentifierID: mustUUID(row.IdentifierID),
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ports.ErrWriteConflict
			}
			if err != nil {
				return nil, err
			}
			applied = append(applied, actionFromMergeRetireRow(updated, action.Action))
		case "transfer":
			updated, err := qtx.TransferIdentifierForMerge(ctx, identitydb.TransferIdentifierForMergeParams{
				SurvivorGoatID: mustUUID(survivorID),
				ApprovedBy:     actorUUID,
				UpdatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
				TenantID:       tenantUUID,
				IdentifierID:   mustUUID(row.IdentifierID),
			})
			if isUniqueViolation(err) {
				return nil, ports.ErrWriteConflict
			}
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ports.ErrWriteConflict
			}
			if err != nil {
				return nil, err
			}
			applied = append(applied, actionFromMergeTransferRow(updated, "transfer"))
		case "preserve":
			applied = append(applied, actionFromActiveIdentifier(row, "preserve"))
		default:
			return nil, ports.ErrWriteConflict
		}
	}
	for _, action := range supplied {
		if action.IdentifierID == nil {
			applied = append(applied, action)
		}
	}
	return applied, nil
}

func replayResolvedConflict(ctx context.Context, qtx *identitydb.Queries, tenantUUID pgtype.UUID, key string, requestHash string) (*ports.ResolveConflictResult, error) {
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
	if idempotency.Status != "completed" || idempotency.ResultType != conflictResultType || strings.TrimSpace(idempotency.ResultID) == "" {
		return nil, ports.ErrIdempotencyPending
	}
	conflictUUID, err := uuidParam(idempotency.ResultID)
	if err != nil {
		return nil, err
	}
	conflict, err := qtx.GetConflictForResolve(ctx, identitydb.GetConflictForResolveParams{
		TenantID:   tenantUUID,
		ConflictID: conflictUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	decisionID := strings.TrimSpace(conflict.DecisionID)
	if decisionID == "" {
		return nil, ports.ErrIdempotencyPending
	}
	decisionUUID, err := uuidParam(decisionID)
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
	warnings := redirectWarningsFromDecisionEvidence(evidence)
	actions := make([]domain.IdentifierAction, 0, len(identifiers))
	for _, row := range identifiers {
		actions = append(actions, domain.IdentifierAction{
			IdentifierID:    nonEmptyStringPtr(row.IdentifierID),
			IdentifierType:  pgTextPtr(row.IdentifierType),
			IdentifierValue: pgTextPtr(row.IdentifierValue),
			Action:          row.Action,
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
	for _, row := range eventRows {
		events = append(events, domain.EventSummary{EventID: row.EventID, EventType: row.EventType})
	}
	firstResultID := idempotency.ResultID
	return &ports.ResolveConflictResult{
		ConflictID: idempotency.ResultID,
		State:      conflict.State,
		Decision:   decisionSummaryFromGetRow(decisionRow),
		Merge: domain.MergeResult{
			SurvivorGoatID:      survivorID,
			MergedGoatIDs:       mergedIDs,
			RedirectWarnings:    warnings,
			AffectedIdentifiers: actions,
		},
		Events:        events,
		Replayed:      true,
		FirstResultID: &firstResultID,
	}, nil
}

func resolveConflictGuardError(ctx context.Context, qtx *identitydb.Queries, tenantUUID, conflictUUID pgtype.UUID) error {
	row, err := qtx.GetConflictForResolve(ctx, identitydb.GetConflictForResolveParams{
		TenantID:   tenantUUID,
		ConflictID: conflictUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.State == "resolved" || row.State == "rejected" || row.State == "closed" {
		return ports.ErrWriteConflict
	}
	return ports.ErrWriteConflict
}

func mergeDecisionRecordPayload(cmd ports.ResolveConflictCommand, decisionID string, redirectWarnings []domain.MergeRedirectWarning, identifierActions []domain.IdentifierAction, createdAt time.Time) ([]byte, error) {
	affected := []map[string]string{{"goat_id": cmd.SurvivorGoatID, "role": "survivor"}}
	for _, goatID := range cmd.AffectedGoatIDs {
		if goatID != cmd.SurvivorGoatID {
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
				"survivor_goat_id":    cmd.SurvivorGoatID,
				"merged_goat_ids":     cmd.AffectedGoatIDs,
				"redirect_warnings":   redirectWarnings,
				"identifier_actions":  identifierActions,
				"conflict_id":         cmd.ConflictID,
				"decision_result":     mergeDecisionResult,
				"decision_type":       mergeDecisionType,
				"decision_state":      "approved",
				"merge_policy_source": mergePolicyVersion,
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

func mergeEventPayload(cmd ports.ResolveConflictCommand, survivorID string, mergedIDs []string, thisMergedID string, decisionID string, mergeLinkIDs []string, identifierActions []domain.IdentifierAction, redirectWarnings []domain.MergeRedirectWarning) ([]byte, error) {
	return json.Marshal(map[string]any{
		"conflict_id":                 cmd.ConflictID,
		"survivor_goat_id":            survivorID,
		"merged_goat_ids":             mergedIDs,
		"this_merged_goat_id":         thisMergedID,
		"decision_id":                 decisionID,
		"merge_link_ids":              mergeLinkIDs,
		"affected_identifier_actions": identifierActions,
		"redirect_warnings":           redirectWarnings,
	})
}

func mergeDomainEventEnvelope(cmd ports.ResolveConflictCommand, eventID string, recordedAt time.Time, visibility domain.LocationScope, survivorID string, mergedIDs []string, thisMergedID string, decisionID string, mergeLinkIDs []string, identifierActions []domain.IdentifierAction, redirectWarnings []domain.MergeRedirectWarning) ([]byte, error) {
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
			"conflict_id":                 cmd.ConflictID,
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

func redirectWarningsFromDecisionEvidence(raw []byte) []domain.MergeRedirectWarning {
	var evidence mergeDecisionEvidence
	if len(raw) == 0 || json.Unmarshal(raw, &evidence) != nil {
		return []domain.MergeRedirectWarning{}
	}
	var record struct {
		Evidence struct {
			After struct {
				RedirectWarnings []domain.MergeRedirectWarning `json:"redirect_warnings"`
			} `json:"after"`
		} `json:"evidence"`
	}
	if json.Unmarshal(evidence.DecisionRecord, &record) != nil {
		return []domain.MergeRedirectWarning{}
	}
	if record.Evidence.After.RedirectWarnings == nil {
		return []domain.MergeRedirectWarning{}
	}
	return record.Evidence.After.RedirectWarnings
}

func insertDecisionIdentifierAction(ctx context.Context, qtx *identitydb.Queries, tenantUUID, decisionUUID pgtype.UUID, action domain.IdentifierAction) error {
	return qtx.InsertIdentityDecisionIdentifier(ctx, identitydb.InsertIdentityDecisionIdentifierParams{
		DecisionID:      decisionUUID,
		TenantID:        tenantUUID,
		IdentifierID:    nullableUUID(action.IdentifierID),
		IdentifierType:  nullableText(action.IdentifierType),
		IdentifierValue: nullableText(action.IdentifierValue),
		Action:          action.Action,
	})
}

func identifierUniqueKeys(row identitydb.ListActiveIdentifiersForGoatsForUpdateRow) []string {
	switch row.IdentifierType {
	case "rfid":
		return []string{"rfid:" + row.NormalizedValue}
	case "old_tag", "sheet_row_id", "purchase_load_id", "temp_field_id", "external_system_id":
		return []string{row.IdentifierType + ":" + row.NormalizedValue + ":" + row.ScopeKey}
	default:
		return nil
	}
}

func actionFromActiveIdentifier(row identitydb.ListActiveIdentifiersForGoatsForUpdateRow, action string) domain.IdentifierAction {
	identifierID := row.IdentifierID
	identifierType := row.IdentifierType
	identifierValue := row.IdentifierValue
	return domain.IdentifierAction{IdentifierID: &identifierID, IdentifierType: &identifierType, IdentifierValue: &identifierValue, Action: action}
}

func actionFromMergeRetireRow(row identitydb.RetireIdentifierForMergeRow, action string) domain.IdentifierAction {
	identifierID := row.IdentifierID
	identifierType := row.IdentifierType
	identifierValue := row.IdentifierValue
	return domain.IdentifierAction{IdentifierID: &identifierID, IdentifierType: &identifierType, IdentifierValue: &identifierValue, Action: action}
}

func actionFromMergeTransferRow(row identitydb.TransferIdentifierForMergeRow, action string) domain.IdentifierAction {
	identifierID := row.IdentifierID
	identifierType := row.IdentifierType
	identifierValue := row.IdentifierValue
	return domain.IdentifierAction{IdentifierID: &identifierID, IdentifierType: &identifierType, IdentifierValue: &identifierValue, Action: action}
}

func mergeGoatRowFromLock(row identitydb.LockGoatsForMergeRow) mergeGoatRow {
	return mergeGoatRow{
		GoatID:           row.GoatID,
		IdentityState:    row.IdentityState,
		MergedIntoGoatID: row.MergedIntoGoatID,
		RowVersion:       row.RowVersion,
		FarmID:           row.FarmID,
		ParkID:           row.ParkID,
		ShedID:           row.ShedID,
		CohortID:         row.CohortID,
	}
}

func mergeGoatRowFromRedirect(row identitydb.ListGoatsRedirectingToRow) mergeGoatRow {
	return mergeGoatRow{
		GoatID:           row.GoatID,
		IdentityState:    row.IdentityState,
		MergedIntoGoatID: row.MergedIntoGoatID,
		RowVersion:       row.RowVersion,
		FarmID:           row.FarmID,
		ParkID:           row.ParkID,
		ShedID:           row.ShedID,
		CohortID:         row.CohortID,
	}
}

func locationScopeFromMergeRow(row mergeGoatRow) domain.LocationScope {
	return domain.LocationScope{
		FarmID:   nonEmptyStringPtr(row.FarmID),
		ParkID:   nonEmptyStringPtr(row.ParkID),
		ShedID:   nonEmptyStringPtr(row.ShedID),
		CohortID: nonEmptyStringPtr(row.CohortID),
	}
}

func uuidListParam(values []string) ([]pgtype.UUID, error) {
	out := make([]pgtype.UUID, 0, len(values))
	for _, value := range values {
		uuid, err := uuidParam(value)
		if err != nil {
			return nil, err
		}
		out = append(out, uuid)
	}
	return out, nil
}

func mustUUID(value string) pgtype.UUID {
	uuid, err := uuidParam(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return uuid
}

func uniqueStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			set[value] = struct{}{}
		}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

var mergeEligibleConflictTypes = map[string]bool{
	"possible_duplicate_goat":     true,
	"duplicate_active_identifier": true,
	"rfid_already_linked":         true,
	"old_tag_reused":              true,
}
