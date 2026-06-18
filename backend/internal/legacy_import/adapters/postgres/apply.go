package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/legacy_import"
	importdb "github.com/vgoats/goatos/backend/internal/legacy_import/adapters/postgres/sqlc"
)

const (
	rfidApplyCommandName    = "legacy_import.apply_rfid_row"
	rfidApplyResultType     = "goat"
	rfidApplyCreatedEvent   = "goat.created"
	rfidApplyIDAddedEvent   = "goat.identifier.added"
	rfidApplyMergedEvent    = "goat.merged"
	rfidApplyEventSchemaRef = "contracts/jsonschema/domain-event-envelope.schema.json"
	rfidApplySchemaVersion  = "1.0.0"
	rfidApplyTopic          = "identity.events"

	blankOldTagSuffixReason         = "blank_old_tag_suffix"
	safeBackfillSourceSystem        = "legacy_bigquery"
	safeBackfillSourceRecordPattern = "safe_old_tag_passport_backfill:%"
)

type pendingApplyRow struct {
	LegacyRowID          string
	RowNumber            int32
	SourceSystem         string
	SourceDataset        string
	SourceRecordID       pgtype.Text
	SourceRowKey         string
	SourceRowVersionHash string
	RawPayload           []byte
	NormalizedPayload    []byte
	ProcessingState      string
	ErrorReason          pgtype.Text
}

type applyPlan struct {
	RFID               string
	OldTag             string
	OldTagScopeKey     string
	Sex                string
	AgeBand            *string
	Breed              string
	BreedID            *string
	LifecycleStatus    string
	ReproductiveStatus *string
	GrowthCohortTag    *string
	ManagementStage    *string
	HealthStatus       *string
	Reason             string
	ExistingGoatID     string
	ExistingDisplayID  string
	ExistingOldTagID   string
	ExistingRFIDGoatID string
	ExistingRFIDID     string
}

type applyOutcome struct {
	applied bool
	review  string
	replay  bool
	goatID  string
	updated bool
}

func (r *Repository) ApplyRFIDRows(ctx context.Context, cmd legacy_import.ApplyCommand) (*legacy_import.ApplyResult, error) {
	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	runUUID, err := uuidParam(cmd.ImportRunID)
	if err != nil {
		return nil, err
	}
	if cmd.BatchSize <= 0 {
		cmd.BatchSize = legacy_import.DefaultBatchSize
	}

	run, err := r.queries.GetLegacyImportRunForApply(ctx, importdb.GetLegacyImportRunForApplyParams{
		TenantID:    tenantUUID,
		ImportRunID: runUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("legacy import run not found")
	}
	if err != nil {
		return nil, err
	}
	if run.Status != "completed" {
		return nil, fmt.Errorf("legacy import run must be completed")
	}
	if run.DryRun {
		return nil, fmt.Errorf("dry-run import run cannot be applied")
	}
	if cmd.PolicyVersion != "" && cmd.PolicyVersion != run.PolicyVersion {
		return nil, fmt.Errorf("apply policy_version does not match import run")
	}

	policy, err := r.LoadApprovedPolicy(ctx, run.PolicyVersion)
	if err != nil {
		return nil, err
	}
	if policy.SourceSystem != run.SourceSystem || policy.SourceDataset != run.SourceDataset {
		return nil, fmt.Errorf("legacy import policy source does not match import run")
	}
	meshaPartyID, err := r.resolveMeshaParty(ctx)
	if err != nil {
		return nil, err
	}
	meshaPartyUUID, err := uuidParam(meshaPartyID)
	if err != nil {
		return nil, err
	}
	actorUUID, err := nullableUUID(cmd.ActorID)
	if err != nil {
		return nil, err
	}

	result := &legacy_import.ApplyResult{
		ImportRunID:   cmd.ImportRunID,
		TenantID:      cmd.TenantID,
		PolicyVersion: run.PolicyVersion,
		SourceSystem:  run.SourceSystem,
		SourceDataset: run.SourceDataset,
		DryRun:        cmd.DryRun,
		ReviewReasons: map[string]int{},
	}

	var cursorRow pgtype.Int4
	var cursorID pgtype.UUID
	for {
		rows, err := r.listRFIDApplyRows(ctx, tenantUUID, runUUID, cursorRow, cursorID, int32(cmd.BatchSize), cmd.AllowRFIDOnlyBlankSuffix)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		for _, applyRow := range rows {
			result.PendingScanned++
			cursorRow = pgtype.Int4{Int32: applyRow.RowNumber, Valid: true}
			if err := cursorID.Scan(applyRow.LegacyRowID); err != nil {
				return nil, err
			}

			if cmd.DryRun {
				mode := classifyApplyCandidate(applyRow, cmd.AllowRFIDOnlyBlankSuffix)
				if mode == applyCandidateIneligible {
					reason := firstApplyReason(applyReasonSet(applyRow))
					if reason != "" {
						result.ReviewCount++
						result.ReviewReasons[reason]++
					} else {
						result.SkippedCount++
					}
					continue
				}
				plan, reason, err := r.evaluatePendingApplyRow(ctx, r.queries, tenantUUID, policy, applyRow, mode == applyCandidateRFIDOnlyBlankSuffix, false)
				if err != nil {
					return nil, err
				}
				if reason != "" {
					result.ReviewCount++
					result.ReviewReasons[reason]++
					continue
				}
				if plan.RFID == "" {
					result.ReviewCount++
					result.ReviewReasons["missing_rfid"]++
					continue
				}
				result.AppliedCount++
				continue
			}

			outcome, err := r.applyOnePendingRow(ctx, tenantUUID, runUUID, meshaPartyUUID, actorUUID, policy, cmd, applyRow.LegacyRowID)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, err
				}
				if !isIsolatableApplyRowError(err) {
					return nil, fmt.Errorf("apply row %s failed: %s", applyRow.LegacyRowID, applyErrorSummary(err))
				}
				if markErr := r.markApplyRowError(ctx, tenantUUID, runUUID, applyRow.LegacyRowID, err); markErr != nil {
					return nil, fmt.Errorf("apply row %s failed: %s; mark row error failed: %s", applyRow.LegacyRowID, applyErrorSummary(err), applyErrorSummary(markErr))
				}
				result.ErrorCount++
				continue
			}
			switch {
			case outcome.applied:
				result.AppliedCount++
				if outcome.updated {
					result.UpdatedGoatIDs = append(result.UpdatedGoatIDs, outcome.goatID)
				} else {
					result.CreatedGoatIDs = append(result.CreatedGoatIDs, outcome.goatID)
				}
			case outcome.replay:
				result.ReplayCount++
				if outcome.goatID != "" {
					result.CreatedGoatIDs = append(result.CreatedGoatIDs, outcome.goatID)
				}
			case outcome.review != "":
				result.ReviewCount++
				result.ReviewReasons[outcome.review]++
			default:
				result.SkippedCount++
			}
		}
	}
	sort.Strings(result.CreatedGoatIDs)
	sort.Strings(result.UpdatedGoatIDs)
	return result, nil
}

func (r *Repository) listRFIDApplyRows(ctx context.Context, tenantUUID, runUUID pgtype.UUID, cursorRow pgtype.Int4, cursorID pgtype.UUID, limit int32, includeBlankSuffixCandidates bool) ([]pendingApplyRow, error) {
	if includeBlankSuffixCandidates {
		rows, err := r.queries.ListRFIDApplyCandidateRowsForBlankSuffixPolicy(ctx, importdb.ListRFIDApplyCandidateRowsForBlankSuffixPolicyParams{
			TenantID:          tenantUUID,
			ImportRunID:       runUUID,
			CursorRowNumber:   cursorRow,
			CursorLegacyRowID: cursorID,
			LimitCount:        limit,
		})
		if err != nil {
			return nil, err
		}
		out := make([]pendingApplyRow, 0, len(rows))
		for _, row := range rows {
			out = append(out, pendingRowFromBlankSuffixPolicyList(row))
		}
		return out, nil
	}
	rows, err := r.queries.ListPendingLegacyImportRowsForApply(ctx, importdb.ListPendingLegacyImportRowsForApplyParams{
		TenantID:          tenantUUID,
		ImportRunID:       runUUID,
		CursorRowNumber:   cursorRow,
		CursorLegacyRowID: cursorID,
		LimitCount:        limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]pendingApplyRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, pendingRowFromList(row))
	}
	return out, nil
}

func (r *Repository) resolveMeshaParty(ctx context.Context) (string, error) {
	rows, err := r.queries.FindActiveMeshaOrgPartyIDs(ctx)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("active Mesha org party not found")
	}
	if len(rows) > 1 {
		return "", fmt.Errorf("active Mesha org party is ambiguous")
	}
	return rows[0], nil
}

func (r *Repository) markApplyRowError(ctx context.Context, tenantUUID, runUUID pgtype.UUID, legacyRowID string, cause error) error {
	rowUUID, err := uuidParam(legacyRowID)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)
	if err := qtx.MarkLegacyImportRowError(ctx, importdb.MarkLegacyImportRowErrorParams{
		TenantID:    tenantUUID,
		LegacyRowID: rowUUID,
		ErrorReason: textParam(applyUnexpectedErrorReason(cause)),
	}); err != nil {
		return err
	}
	if err := qtx.RefreshLegacyImportRunErrorCount(ctx, importdb.RefreshLegacyImportRunErrorCountParams{
		TenantID:    tenantUUID,
		ImportRunID: runUUID,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *Repository) applyOnePendingRow(ctx context.Context, tenantUUID, runUUID, meshaPartyUUID, actorUUID pgtype.UUID, policy legacy_import.Policy, cmd legacy_import.ApplyCommand, legacyRowID string) (applyOutcome, error) {
	rowUUID, err := uuidParam(legacyRowID)
	if err != nil {
		return applyOutcome{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return applyOutcome{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)

	locked, err := qtx.LockLegacyImportRowForApply(ctx, importdb.LockLegacyImportRowForApplyParams{
		TenantID:    tenantUUID,
		LegacyRowID: rowUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return applyOutcome{}, nil
	}
	if err != nil {
		return applyOutcome{}, err
	}
	row := pendingRowFromLock(locked)
	mode := classifyApplyCandidate(row, cmd.AllowRFIDOnlyBlankSuffix)
	if mode == applyCandidateIneligible {
		return applyOutcome{}, nil
	}

	idempotencyKey := stableApplyIdempotencyKey(cmd.TenantID, row.SourceSystem, row.SourceDataset, row.SourceRowKey, row.SourceRowVersionHash)
	requestHash := stableApplyRequestHash(row)
	existingIDM, err := qtx.GetIdempotencyKey(ctx, idempotencyKey)
	if err == nil {
		if existingIDM.RequestHash != requestHash {
			return applyOutcome{}, fmt.Errorf("idempotency key reused with different source payload")
		}
		if existingIDM.Status != "completed" || existingIDM.ResultType != rfidApplyResultType || strings.TrimSpace(existingIDM.ResultID) == "" {
			return applyOutcome{}, fmt.Errorf("idempotency key is not completed")
		}
		goatUUID := mustUUID(existingIDM.ResultID)
		if err := qtx.MarkLegacyImportRowCreatedGoat(ctx, importdb.MarkLegacyImportRowCreatedGoatParams{
			TenantID:      tenantUUID,
			LegacyRowID:   rowUUID,
			MatchedGoatID: goatUUID,
		}); err != nil {
			return applyOutcome{}, err
		}
		if err := qtx.RefreshLegacyImportRunCreatedGoatCount(ctx, importdb.RefreshLegacyImportRunCreatedGoatCountParams{
			TenantID:    tenantUUID,
			ImportRunID: runUUID,
		}); err != nil {
			return applyOutcome{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return applyOutcome{}, err
		}
		committed = true
		return applyOutcome{replay: true, goatID: existingIDM.ResultID}, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return applyOutcome{}, err
	}

	plan, reviewReason, err := r.evaluatePendingApplyRow(ctx, qtx, tenantUUID, policy, row, mode == applyCandidateRFIDOnlyBlankSuffix, true)
	if err != nil {
		return applyOutcome{}, err
	}
	if reviewReason != "" {
		if err := qtx.MarkLegacyImportRowNeedsReview(ctx, importdb.MarkLegacyImportRowNeedsReviewParams{
			TenantID:          tenantUUID,
			LegacyRowID:       rowUUID,
			NormalizedPayload: appendReviewReason(row.NormalizedPayload, reviewReason),
			ErrorReason:       textParam(reviewReason),
		}); err != nil {
			return applyOutcome{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return applyOutcome{}, err
		}
		committed = true
		return applyOutcome{review: reviewReason}, nil
	}

	_, err = qtx.InsertIdempotencyStarted(ctx, importdb.InsertIdempotencyStartedParams{
		IdempotencyKey: idempotencyKey,
		TenantID:       tenantUUID,
		Scope:          rfidApplyCommandName,
		RequestHash:    requestHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		outcome, err := replayAppliedRow(ctx, qtx, tenantUUID, runUUID, rowUUID, idempotencyKey, requestHash)
		if err != nil {
			return applyOutcome{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return applyOutcome{}, err
		}
		committed = true
		return outcome, nil
	}
	if err != nil {
		return applyOutcome{}, err
	}

	now := time.Now().UTC()
	traceID := "rfid-apply:" + row.LegacyRowID
	decisionID, err := qtx.NewUUID(ctx)
	if err != nil {
		return applyOutcome{}, err
	}
	decisionUUID := mustUUID(decisionID)

	breedUUID, err := nullableUUID(plan.BreedID)
	if err != nil {
		return applyOutcome{}, err
	}
	if plan.ExistingGoatID != "" && plan.ExistingRFIDGoatID != "" {
		outcome, err := r.mergeBackfillOldTagIntoExistingRFIDGoat(ctx, qtx, importApplyContext{
			TenantUUID:     tenantUUID,
			RunUUID:        runUUID,
			MeshaPartyUUID: meshaPartyUUID,
			ActorUUID:      actorUUID,
			Policy:         policy,
			Command:        cmd,
			Row:            row,
			RowUUID:        rowUUID,
			Plan:           plan,
			BreedUUID:      breedUUID,
			DecisionID:     decisionID,
			DecisionUUID:   decisionUUID,
			IdempotencyKey: idempotencyKey,
			Now:            now,
			TraceID:        traceID,
		})
		if err != nil {
			return applyOutcome{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return applyOutcome{}, err
		}
		committed = true
		return outcome, nil
	}
	if plan.ExistingGoatID != "" {
		outcome, err := r.attachRFIDToExistingBackfillGoat(ctx, qtx, importApplyContext{
			TenantUUID:     tenantUUID,
			RunUUID:        runUUID,
			MeshaPartyUUID: meshaPartyUUID,
			ActorUUID:      actorUUID,
			Policy:         policy,
			Command:        cmd,
			Row:            row,
			RowUUID:        rowUUID,
			Plan:           plan,
			BreedUUID:      breedUUID,
			DecisionID:     decisionID,
			DecisionUUID:   decisionUUID,
			IdempotencyKey: idempotencyKey,
			Now:            now,
			TraceID:        traceID,
		})
		if err != nil {
			return applyOutcome{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return applyOutcome{}, err
		}
		committed = true
		return outcome, nil
	}
	goatRow, err := qtx.InsertGoatFromRFIDApply(ctx, importdb.InsertGoatFromRFIDApplyParams{
		TenantID:           tenantUUID,
		Breed:              nullableText(planStringPtr(plan.Breed)),
		BreedID:            breedUUID,
		Sex:                textParam(plan.Sex),
		AgeBand:            nullableText(plan.AgeBand),
		LifecycleStatus:    plan.LifecycleStatus,
		ReproductiveStatus: nullableText(plan.ReproductiveStatus),
		GrowthCohortTag:    nullableText(plan.GrowthCohortTag),
		ManagementStage:    nullableText(plan.ManagementStage),
		HealthStatus:       nullableText(plan.HealthStatus),
		CustodianPartyID:   meshaPartyUUID,
		CreatedAt:          pgtype.Timestamptz{Time: now, Valid: true},
		CreatedBy:          actorUUID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	goatUUID := mustUUID(goatRow.GoatID)

	rfidPolicy, err := qtx.GetIdentifierPolicyForApply(ctx, importdb.GetIdentifierPolicyForApplyParams{
		PolicyVersion:  policy.IdentifierPolicyVersion,
		IdentifierType: "rfid",
	})
	if err != nil {
		return applyOutcome{}, err
	}
	rfidIdentifierID, err := qtx.InsertGoatIdentifierFromRFIDApply(ctx, importdb.InsertGoatIdentifierFromRFIDApplyParams{
		TenantID:          tenantUUID,
		GoatID:            goatUUID,
		IdentifierType:    "rfid",
		IdentifierValue:   plan.RFID,
		NormalizedValue:   plan.RFID,
		ScopeKey:          "global",
		IsPrimaryForGoat:  true,
		ValidFrom:         pgtype.Timestamptz{Time: now, Valid: true},
		SourceSystem:      textParam(row.SourceSystem),
		SourceRecordID:    textParam(sourceRecordID(row)),
		NormalizerVersion: rfidPolicy.NormalizerVersion,
		ApprovedBy:        actorUUID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	identifierIDs := []string{rfidIdentifierID}
	identifierActions := []map[string]any{{
		"identifier_id":    rfidIdentifierID,
		"identifier_type":  "rfid",
		"identifier_value": plan.RFID,
		"action":           "attach",
	}}

	oldTagPolicy, err := qtx.GetIdentifierPolicyForApply(ctx, importdb.GetIdentifierPolicyForApplyParams{
		PolicyVersion:  policy.IdentifierPolicyVersion,
		IdentifierType: "old_tag",
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if plan.OldTag != "" && plan.OldTagScopeKey != "" {
		oldTagIdentifierID, err := qtx.InsertGoatIdentifierFromRFIDApply(ctx, importdb.InsertGoatIdentifierFromRFIDApplyParams{
			TenantID:          tenantUUID,
			GoatID:            goatUUID,
			IdentifierType:    "old_tag",
			IdentifierValue:   plan.OldTag,
			NormalizedValue:   plan.OldTag,
			ScopeKey:          plan.OldTagScopeKey,
			IsPrimaryForGoat:  oldTagPolicy.PrimaryAllowed,
			ValidFrom:         pgtype.Timestamptz{Time: now, Valid: true},
			SourceSystem:      textParam(row.SourceSystem),
			SourceRecordID:    textParam(sourceRecordID(row)),
			NormalizerVersion: oldTagPolicy.NormalizerVersion,
			ApprovedBy:        actorUUID,
		})
		if err != nil {
			return applyOutcome{}, err
		}
		identifierIDs = append(identifierIDs, oldTagIdentifierID)
		identifierActions = append(identifierActions, map[string]any{
			"identifier_id":    oldTagIdentifierID,
			"identifier_type":  "old_tag",
			"identifier_value": plan.OldTag,
			"action":           "attach",
		})
	}

	decisionRecord, err := importDecisionRecordPayload(importDecisionRecordInput{
		DecisionID:        decisionID,
		TenantID:          cmd.TenantID,
		ActorID:           cmd.ActorID,
		PolicyVersion:     policy.PolicyVersion,
		Reason:            plan.Reason,
		ImportRunID:       cmd.ImportRunID,
		LegacyRowID:       row.LegacyRowID,
		SourceSystem:      row.SourceSystem,
		SourceRowKey:      row.SourceRowKey,
		GoatID:            goatRow.GoatID,
		IdentifierActions: identifierActions,
		IdempotencyKey:    idempotencyKey,
		TraceID:           traceID,
		CreatedAt:         now,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	decisionEvidence, err := json.Marshal(map[string]any{
		"evidence_refs":   importEvidenceRefs(cmd.ImportRunID, row.LegacyRowID, row.SourceSystem, row.SourceRowKey),
		"reason":          plan.Reason,
		"decision_record": json.RawMessage(decisionRecord),
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if _, err := qtx.InsertImportIdentityDecision(ctx, importdb.InsertImportIdentityDecisionParams{
		DecisionID:    decisionUUID,
		TenantID:      tenantUUID,
		DecidedBy:     actorUUID,
		PolicyVersion: policy.PolicyVersion,
		ReviewerID:    actorUUID,
		Evidence:      decisionEvidence,
		CreatedAt:     pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
		return applyOutcome{}, err
	}

	if err := qtx.InsertGoatOwnershipFromRFIDApply(ctx, importdb.InsertGoatOwnershipFromRFIDApplyParams{
		TenantID:     tenantUUID,
		GoatID:       goatUUID,
		OwnerPartyID: meshaPartyUUID,
		ValidFrom:    pgtype.Timestamptz{Time: now, Valid: true},
		DecisionID:   decisionUUID,
		CreatedBy:    actorUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertGoatCustodyHistoryFromRFIDApply(ctx, importdb.InsertGoatCustodyHistoryFromRFIDApplyParams{
		TenantID:         tenantUUID,
		GoatID:           goatUUID,
		CustodianPartyID: meshaPartyUUID,
		ValidFrom:        pgtype.Timestamptz{Time: now, Valid: true},
		DecisionID:       decisionUUID,
		CreatedBy:        actorUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertIdentityDecisionGoatForApply(ctx, importdb.InsertIdentityDecisionGoatForApplyParams{
		DecisionID: decisionUUID,
		TenantID:   tenantUUID,
		GoatID:     goatUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	for _, identifierID := range identifierIDs {
		identifierUUID := mustUUID(identifierID)
		var identifierType, identifierValue string
		if identifierID == rfidIdentifierID {
			identifierType, identifierValue = "rfid", plan.RFID
		} else {
			identifierType, identifierValue = "old_tag", plan.OldTag
		}
		if err := qtx.InsertIdentityDecisionIdentifierForApply(ctx, importdb.InsertIdentityDecisionIdentifierForApplyParams{
			DecisionID:      decisionUUID,
			TenantID:        tenantUUID,
			IdentifierID:    identifierUUID,
			IdentifierType:  textParam(identifierType),
			IdentifierValue: textParam(identifierValue),
		}); err != nil {
			return applyOutcome{}, err
		}
	}

	eventID, err := qtx.NewUUID(ctx)
	if err != nil {
		return applyOutcome{}, err
	}
	eventUUID := mustUUID(eventID)
	eventPayload, err := json.Marshal(map[string]any{
		"goat_id":            goatRow.GoatID,
		"display_id":         goatRow.DisplayID,
		"decision_id":        decisionID,
		"import_run_id":      cmd.ImportRunID,
		"legacy_row_id":      row.LegacyRowID,
		"source_row_key":     row.SourceRowKey,
		"identifier_ids":     identifierIDs,
		"identifier_actions": identifierActions,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	eventRow, err := qtx.InsertGoatIdentityEventFromRFIDApply(ctx, importdb.InsertGoatIdentityEventFromRFIDApplyParams{
		IdentityEventID: eventUUID,
		TenantID:        tenantUUID,
		GoatID:          goatUUID,
		OccurredAt:      pgtype.Timestamptz{Time: now, Valid: true},
		ActorID:         actorUUID,
		SourceSystem:    textParam(row.SourceSystem),
		SourceRecordID:  textParam(sourceRecordID(row)),
		Payload:         eventPayload,
		DecisionID:      decisionUUID,
		IdempotencyKey:  idempotencyKey,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertIdentityDecisionEventForApply(ctx, importdb.InsertIdentityDecisionEventForApplyParams{
		DecisionID:      decisionUUID,
		TenantID:        tenantUUID,
		EventID:         eventUUID,
		EventRecordedAt: eventRow.RecordedAt,
	}); err != nil {
		return applyOutcome{}, err
	}

	afterState, err := json.Marshal(map[string]any{
		"goat_id":        goatRow.GoatID,
		"display_id":     goatRow.DisplayID,
		"legacy_row_id":  row.LegacyRowID,
		"import_run_id":  cmd.ImportRunID,
		"identifier_ids": identifierIDs,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	metadata, err := json.Marshal(map[string]any{
		"command":         rfidApplyCommandName,
		"idempotency_key": idempotencyKey,
		"trace_id":        traceID,
		"source_system":   row.SourceSystem,
		"source_dataset":  row.SourceDataset,
		"source_row_key":  row.SourceRowKey,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertAuditLogForApply(ctx, importdb.InsertAuditLogForApplyParams{
		TenantID:   tenantUUID,
		ActorID:    actorUUID,
		ResourceID: goatUUID,
		DecisionID: decisionUUID,
		AfterState: afterState,
		Metadata:   metadata,
		TraceID:    textParam(traceID),
	}); err != nil {
		return applyOutcome{}, err
	}
	if r.afterAuditHook != nil {
		if err := r.afterAuditHook(ctx); err != nil {
			return applyOutcome{}, err
		}
	}

	envelope, err := importDomainEventEnvelope(importEventEnvelopeInput{
		EventID:        eventRow.EventID,
		GoatID:         goatRow.GoatID,
		TenantID:       cmd.TenantID,
		ActorID:        cmd.ActorID,
		IdempotencyKey: idempotencyKey,
		TraceID:        traceID,
		OccurredAt:     now,
		RecordedAt:     eventRow.RecordedAt.Time,
		EvidenceRefs:   importEvidenceRefs(cmd.ImportRunID, row.LegacyRowID, row.SourceSystem, row.SourceRowKey),
		Payload:        json.RawMessage(eventPayload),
	})
	if err != nil {
		return applyOutcome{}, err
	}
	headers, err := json.Marshal(map[string]any{
		"command":       rfidApplyCommandName,
		"trace_id":      traceID,
		"decision_id":   decisionID,
		"legacy_row_id": row.LegacyRowID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertOutboxMessageForApply(ctx, importdb.InsertOutboxMessageForApplyParams{
		TenantID:       tenantUUID,
		EventID:        eventUUID,
		AggregateID:    goatUUID,
		Payload:        envelope,
		Headers:        headers,
		IdempotencyKey: idempotencyKey,
		TraceID:        textParam(traceID),
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.MarkLegacyImportRowCreatedGoat(ctx, importdb.MarkLegacyImportRowCreatedGoatParams{
		TenantID:      tenantUUID,
		LegacyRowID:   rowUUID,
		MatchedGoatID: goatUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.IncrementLegacyImportRunCreatedGoatCount(ctx, importdb.IncrementLegacyImportRunCreatedGoatCountParams{
		TenantID:    tenantUUID,
		ImportRunID: runUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.CompleteIdempotencyKey(ctx, importdb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(rfidApplyResultType),
		ResultID:       goatUUID,
		IdempotencyKey: idempotencyKey,
	}); err != nil {
		return applyOutcome{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return applyOutcome{}, err
	}
	committed = true
	return applyOutcome{applied: true, goatID: goatRow.GoatID}, nil
}

type importApplyContext struct {
	TenantUUID     pgtype.UUID
	RunUUID        pgtype.UUID
	MeshaPartyUUID pgtype.UUID
	ActorUUID      pgtype.UUID
	Policy         legacy_import.Policy
	Command        legacy_import.ApplyCommand
	Row            pendingApplyRow
	RowUUID        pgtype.UUID
	Plan           applyPlan
	BreedUUID      pgtype.UUID
	DecisionID     string
	DecisionUUID   pgtype.UUID
	IdempotencyKey string
	Now            time.Time
	TraceID        string
}

func (r *Repository) mergeBackfillOldTagIntoExistingRFIDGoat(ctx context.Context, qtx *importdb.Queries, in importApplyContext) (applyOutcome, error) {
	survivorUUID := mustUUID(in.Plan.ExistingRFIDGoatID)
	mergedUUID := mustUUID(in.Plan.ExistingGoatID)
	oldTagIdentifierUUID := mustUUID(in.Plan.ExistingOldTagID)

	goatRow, err := qtx.UpdateGoatFromRFIDApply(ctx, importdb.UpdateGoatFromRFIDApplyParams{
		TenantID:           in.TenantUUID,
		GoatID:             survivorUUID,
		Breed:              nullableText(planStringPtr(in.Plan.Breed)),
		BreedID:            in.BreedUUID,
		Sex:                textParam(in.Plan.Sex),
		AgeBand:            nullableText(in.Plan.AgeBand),
		LifecycleStatus:    in.Plan.LifecycleStatus,
		ReproductiveStatus: nullableText(in.Plan.ReproductiveStatus),
		GrowthCohortTag:    nullableText(in.Plan.GrowthCohortTag),
		ManagementStage:    nullableText(in.Plan.ManagementStage),
		HealthStatus:       nullableText(in.Plan.HealthStatus),
		CustodianPartyID:   in.MeshaPartyUUID,
		UpdatedAt:          pgtype.Timestamptz{Time: in.Now, Valid: true},
	})
	if err != nil {
		return applyOutcome{}, err
	}

	if err := qtx.TransferGoatIdentifierToGoatForApply(ctx, importdb.TransferGoatIdentifierToGoatForApplyParams{
		TargetGoatID: survivorUUID,
		UpdatedAt:    pgtype.Timestamptz{Time: in.Now, Valid: true},
		TenantID:     in.TenantUUID,
		IdentifierID: oldTagIdentifierUUID,
		SourceGoatID: mergedUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.MarkGoatMergedForApply(ctx, importdb.MarkGoatMergedForApplyParams{
		SurvivorGoatID: survivorUUID,
		UpdatedAt:      pgtype.Timestamptz{Time: in.Now, Valid: true},
		TenantID:       in.TenantUUID,
		MergedGoatID:   mergedUUID,
	}); err != nil {
		return applyOutcome{}, err
	}

	identifierActions := []map[string]any{{
		"identifier_id":    in.Plan.ExistingOldTagID,
		"identifier_type":  "old_tag",
		"identifier_value": in.Plan.OldTag,
		"scope_key":        in.Plan.OldTagScopeKey,
		"action":           "transfer_to_rfid_passport",
		"from_goat_id":     in.Plan.ExistingGoatID,
		"to_goat_id":       in.Plan.ExistingRFIDGoatID,
	}}
	decisionRecord, err := json.Marshal(map[string]any{
		"decision_id":     in.DecisionID,
		"decision_type":   "merge_goats",
		"decision_result": "safe_backfill_merged_into_rfid_passport",
		"decision_state":  "approved",
		"decided_by_type": "import_policy",
		"decided_by":      in.Command.ActorID,
		"reviewer_id":     in.Command.ActorID,
		"policy_version":  in.Policy.PolicyVersion,
		"reason":          in.Plan.Reason,
		"source_record_ids": []string{
			in.Row.LegacyRowID,
			in.Row.SourceRowKey,
		},
		"affected_goats": []map[string]any{
			{"goat_id": in.Plan.ExistingRFIDGoatID, "role": "survivor"},
			{"goat_id": in.Plan.ExistingGoatID, "role": "merged"},
		},
		"identifier_actions": identifierActions,
		"evidence": map[string]any{
			"evidence_refs": importEvidenceRefs(in.Command.ImportRunID, in.Row.LegacyRowID, in.Row.SourceSystem, in.Row.SourceRowKey),
		},
		"idempotency_key": in.IdempotencyKey,
		"trace_id":        in.TraceID,
		"created_at":      formatEventTime(in.Now),
		"approved_at":     formatEventTime(in.Now),
		"decided_at":      formatEventTime(in.Now),
	})
	if err != nil {
		return applyOutcome{}, err
	}
	decisionEvidence, err := json.Marshal(map[string]any{
		"evidence_refs":                  importEvidenceRefs(in.Command.ImportRunID, in.Row.LegacyRowID, in.Row.SourceSystem, in.Row.SourceRowKey),
		"reason":                         in.Plan.Reason,
		"decision_record":                json.RawMessage(decisionRecord),
		"survivor_goat_id":               in.Plan.ExistingRFIDGoatID,
		"merged_goat_id":                 in.Plan.ExistingGoatID,
		"existing_rfid_identifier_id":    in.Plan.ExistingRFIDID,
		"existing_old_tag_identifier_id": in.Plan.ExistingOldTagID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if _, err := qtx.InsertImportMergeDecision(ctx, importdb.InsertImportMergeDecisionParams{
		DecisionID:    in.DecisionUUID,
		TenantID:      in.TenantUUID,
		DecidedBy:     in.ActorUUID,
		PolicyVersion: in.Policy.PolicyVersion,
		ReviewerID:    in.ActorUUID,
		Evidence:      decisionEvidence,
		CreatedAt:     pgtype.Timestamptz{Time: in.Now, Valid: true},
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertIdentityDecisionGoatWithRoleForApply(ctx, importdb.InsertIdentityDecisionGoatWithRoleForApplyParams{
		DecisionID: in.DecisionUUID,
		TenantID:   in.TenantUUID,
		GoatID:     survivorUUID,
		Role:       "survivor",
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertIdentityDecisionGoatWithRoleForApply(ctx, importdb.InsertIdentityDecisionGoatWithRoleForApplyParams{
		DecisionID: in.DecisionUUID,
		TenantID:   in.TenantUUID,
		GoatID:     mergedUUID,
		Role:       "merged",
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertIdentityDecisionIdentifierForApply(ctx, importdb.InsertIdentityDecisionIdentifierForApplyParams{
		DecisionID:      in.DecisionUUID,
		TenantID:        in.TenantUUID,
		IdentifierID:    oldTagIdentifierUUID,
		IdentifierType:  textParam("old_tag"),
		IdentifierValue: textParam(in.Plan.OldTag),
	}); err != nil {
		return applyOutcome{}, err
	}
	mergeLinkID, err := qtx.InsertGoatMergeLinkForApply(ctx, importdb.InsertGoatMergeLinkForApplyParams{
		TenantID:       in.TenantUUID,
		SurvivorGoatID: survivorUUID,
		MergedGoatID:   mergedUUID,
		DecisionID:     in.DecisionUUID,
		Reason:         in.Plan.Reason,
		CreatedAt:      pgtype.Timestamptz{Time: in.Now, Valid: true},
		CreatedBy:      in.ActorUUID,
	})
	if err != nil {
		return applyOutcome{}, err
	}

	eventID, err := qtx.NewUUID(ctx)
	if err != nil {
		return applyOutcome{}, err
	}
	eventUUID := mustUUID(eventID)
	eventPayload, err := json.Marshal(map[string]any{
		"survivor_goat_id":               in.Plan.ExistingRFIDGoatID,
		"merged_goat_id":                 in.Plan.ExistingGoatID,
		"decision_id":                    in.DecisionID,
		"merge_link_id":                  mergeLinkID,
		"import_run_id":                  in.Command.ImportRunID,
		"legacy_row_id":                  in.Row.LegacyRowID,
		"source_row_key":                 in.Row.SourceRowKey,
		"existing_rfid_identifier_id":    in.Plan.ExistingRFIDID,
		"existing_old_tag_identifier_id": in.Plan.ExistingOldTagID,
		"identifier_actions":             identifierActions,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	eventRow, err := qtx.InsertGoatMergedEventFromRFIDApply(ctx, importdb.InsertGoatMergedEventFromRFIDApplyParams{
		IdentityEventID: eventUUID,
		TenantID:        in.TenantUUID,
		GoatID:          survivorUUID,
		OccurredAt:      pgtype.Timestamptz{Time: in.Now, Valid: true},
		ActorID:         in.ActorUUID,
		SourceSystem:    textParam(in.Row.SourceSystem),
		SourceRecordID:  textParam(sourceRecordID(in.Row)),
		Payload:         eventPayload,
		DecisionID:      in.DecisionUUID,
		IdempotencyKey:  in.IdempotencyKey,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertIdentityDecisionEventForApply(ctx, importdb.InsertIdentityDecisionEventForApplyParams{
		DecisionID:      in.DecisionUUID,
		TenantID:        in.TenantUUID,
		EventID:         eventUUID,
		EventRecordedAt: eventRow.RecordedAt,
	}); err != nil {
		return applyOutcome{}, err
	}

	afterState, err := json.Marshal(map[string]any{
		"survivor_goat_id":               in.Plan.ExistingRFIDGoatID,
		"survivor_display_id":            goatRow.DisplayID,
		"merged_goat_id":                 in.Plan.ExistingGoatID,
		"merged_display_id":              in.Plan.ExistingDisplayID,
		"legacy_row_id":                  in.Row.LegacyRowID,
		"import_run_id":                  in.Command.ImportRunID,
		"existing_rfid_identifier_id":    in.Plan.ExistingRFIDID,
		"existing_old_tag_identifier_id": in.Plan.ExistingOldTagID,
		"row_version":                    goatRow.RowVersion,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	metadata, err := json.Marshal(map[string]any{
		"command":         rfidApplyCommandName,
		"idempotency_key": in.IdempotencyKey,
		"trace_id":        in.TraceID,
		"source_system":   in.Row.SourceSystem,
		"source_dataset":  in.Row.SourceDataset,
		"source_row_key":  in.Row.SourceRowKey,
		"link_mode":       "safe_old_tag_backfill_merge_into_rfid",
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertAuditLogGoatMergedForApply(ctx, importdb.InsertAuditLogGoatMergedForApplyParams{
		TenantID:   in.TenantUUID,
		ActorID:    in.ActorUUID,
		ResourceID: survivorUUID,
		DecisionID: in.DecisionUUID,
		AfterState: afterState,
		Metadata:   metadata,
		TraceID:    textParam(in.TraceID),
	}); err != nil {
		return applyOutcome{}, err
	}
	if r.afterAuditHook != nil {
		if err := r.afterAuditHook(ctx); err != nil {
			return applyOutcome{}, err
		}
	}

	envelope, err := importDomainEventEnvelope(importEventEnvelopeInput{
		EventID:        eventRow.EventID,
		EventType:      rfidApplyMergedEvent,
		GoatID:         in.Plan.ExistingRFIDGoatID,
		TenantID:       in.Command.TenantID,
		ActorID:        in.Command.ActorID,
		IdempotencyKey: in.IdempotencyKey,
		TraceID:        in.TraceID,
		OccurredAt:     in.Now,
		RecordedAt:     eventRow.RecordedAt.Time,
		EvidenceRefs:   importEvidenceRefs(in.Command.ImportRunID, in.Row.LegacyRowID, in.Row.SourceSystem, in.Row.SourceRowKey),
		Payload:        json.RawMessage(eventPayload),
		SubjectType:    "goat",
		SubjectID:      in.Plan.ExistingGoatID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	headers, err := json.Marshal(map[string]any{
		"command":       rfidApplyCommandName,
		"trace_id":      in.TraceID,
		"decision_id":   in.DecisionID,
		"legacy_row_id": in.Row.LegacyRowID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertOutboxGoatMergedMessageForApply(ctx, importdb.InsertOutboxGoatMergedMessageForApplyParams{
		TenantID:       in.TenantUUID,
		EventID:        eventUUID,
		AggregateID:    survivorUUID,
		Payload:        envelope,
		Headers:        headers,
		IdempotencyKey: in.IdempotencyKey,
		TraceID:        textParam(in.TraceID),
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.MarkLegacyImportRowAutoLinked(ctx, importdb.MarkLegacyImportRowAutoLinkedParams{
		TenantID:      in.TenantUUID,
		LegacyRowID:   in.RowUUID,
		MatchedGoatID: survivorUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.IncrementLegacyImportRunUpdatedGoatCount(ctx, importdb.IncrementLegacyImportRunUpdatedGoatCountParams{
		TenantID:    in.TenantUUID,
		ImportRunID: in.RunUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.CompleteIdempotencyKey(ctx, importdb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(rfidApplyResultType),
		ResultID:       survivorUUID,
		IdempotencyKey: in.IdempotencyKey,
	}); err != nil {
		return applyOutcome{}, err
	}
	return applyOutcome{applied: true, goatID: goatRow.GoatID, updated: true}, nil
}

func (r *Repository) attachRFIDToExistingBackfillGoat(ctx context.Context, qtx *importdb.Queries, in importApplyContext) (applyOutcome, error) {
	goatUUID := mustUUID(in.Plan.ExistingGoatID)
	goatRow, err := qtx.UpdateGoatFromRFIDApply(ctx, importdb.UpdateGoatFromRFIDApplyParams{
		TenantID:           in.TenantUUID,
		GoatID:             goatUUID,
		Breed:              nullableText(planStringPtr(in.Plan.Breed)),
		BreedID:            in.BreedUUID,
		Sex:                textParam(in.Plan.Sex),
		AgeBand:            nullableText(in.Plan.AgeBand),
		LifecycleStatus:    in.Plan.LifecycleStatus,
		ReproductiveStatus: nullableText(in.Plan.ReproductiveStatus),
		GrowthCohortTag:    nullableText(in.Plan.GrowthCohortTag),
		ManagementStage:    nullableText(in.Plan.ManagementStage),
		HealthStatus:       nullableText(in.Plan.HealthStatus),
		CustodianPartyID:   in.MeshaPartyUUID,
		UpdatedAt:          pgtype.Timestamptz{Time: in.Now, Valid: true},
	})
	if err != nil {
		return applyOutcome{}, err
	}

	rfidPolicy, err := qtx.GetIdentifierPolicyForApply(ctx, importdb.GetIdentifierPolicyForApplyParams{
		PolicyVersion:  in.Policy.IdentifierPolicyVersion,
		IdentifierType: "rfid",
	})
	if err != nil {
		return applyOutcome{}, err
	}
	rfidIdentifierID, err := qtx.InsertGoatIdentifierFromRFIDApply(ctx, importdb.InsertGoatIdentifierFromRFIDApplyParams{
		TenantID:          in.TenantUUID,
		GoatID:            goatUUID,
		IdentifierType:    "rfid",
		IdentifierValue:   in.Plan.RFID,
		NormalizedValue:   in.Plan.RFID,
		ScopeKey:          "global",
		IsPrimaryForGoat:  true,
		ValidFrom:         pgtype.Timestamptz{Time: in.Now, Valid: true},
		SourceSystem:      textParam(in.Row.SourceSystem),
		SourceRecordID:    textParam(sourceRecordID(in.Row)),
		NormalizerVersion: rfidPolicy.NormalizerVersion,
		ApprovedBy:        in.ActorUUID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	identifierUUID := mustUUID(rfidIdentifierID)
	identifierActions := []map[string]any{{
		"identifier_id":    rfidIdentifierID,
		"identifier_type":  "rfid",
		"identifier_value": in.Plan.RFID,
		"action":           "attach",
	}}
	decisionRecord, err := importDecisionRecordPayload(importDecisionRecordInput{
		DecisionID:        in.DecisionID,
		DecisionType:      "attach_identifier",
		DecisionResult:    "rfid_attached_to_backfill_passport",
		TenantID:          in.Command.TenantID,
		ActorID:           in.Command.ActorID,
		PolicyVersion:     in.Policy.PolicyVersion,
		Reason:            in.Plan.Reason,
		ImportRunID:       in.Command.ImportRunID,
		LegacyRowID:       in.Row.LegacyRowID,
		SourceSystem:      in.Row.SourceSystem,
		SourceRowKey:      in.Row.SourceRowKey,
		GoatID:            goatRow.GoatID,
		IdentifierActions: identifierActions,
		IdempotencyKey:    in.IdempotencyKey,
		TraceID:           in.TraceID,
		CreatedAt:         in.Now,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	decisionEvidence, err := json.Marshal(map[string]any{
		"evidence_refs":                  importEvidenceRefs(in.Command.ImportRunID, in.Row.LegacyRowID, in.Row.SourceSystem, in.Row.SourceRowKey),
		"reason":                         in.Plan.Reason,
		"decision_record":                json.RawMessage(decisionRecord),
		"existing_old_tag_identifier_id": in.Plan.ExistingOldTagID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if _, err := qtx.InsertImportAttachIdentifierDecision(ctx, importdb.InsertImportAttachIdentifierDecisionParams{
		DecisionID:    in.DecisionUUID,
		TenantID:      in.TenantUUID,
		DecidedBy:     in.ActorUUID,
		PolicyVersion: in.Policy.PolicyVersion,
		ReviewerID:    in.ActorUUID,
		Evidence:      decisionEvidence,
		CreatedAt:     pgtype.Timestamptz{Time: in.Now, Valid: true},
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertIdentityDecisionGoatForApply(ctx, importdb.InsertIdentityDecisionGoatForApplyParams{
		DecisionID: in.DecisionUUID,
		TenantID:   in.TenantUUID,
		GoatID:     goatUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertIdentityDecisionIdentifierForApply(ctx, importdb.InsertIdentityDecisionIdentifierForApplyParams{
		DecisionID:      in.DecisionUUID,
		TenantID:        in.TenantUUID,
		IdentifierID:    identifierUUID,
		IdentifierType:  textParam("rfid"),
		IdentifierValue: textParam(in.Plan.RFID),
	}); err != nil {
		return applyOutcome{}, err
	}

	eventID, err := qtx.NewUUID(ctx)
	if err != nil {
		return applyOutcome{}, err
	}
	eventUUID := mustUUID(eventID)
	eventPayload, err := json.Marshal(map[string]any{
		"goat_id":                        goatRow.GoatID,
		"display_id":                     goatRow.DisplayID,
		"decision_id":                    in.DecisionID,
		"import_run_id":                  in.Command.ImportRunID,
		"legacy_row_id":                  in.Row.LegacyRowID,
		"source_row_key":                 in.Row.SourceRowKey,
		"identifier_id":                  rfidIdentifierID,
		"identifier_type":                "rfid",
		"identifier_value":               in.Plan.RFID,
		"identifier_actions":             identifierActions,
		"existing_old_tag_identifier_id": in.Plan.ExistingOldTagID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	eventRow, err := qtx.InsertGoatIdentifierAddedEventFromRFIDApply(ctx, importdb.InsertGoatIdentifierAddedEventFromRFIDApplyParams{
		IdentityEventID: eventUUID,
		TenantID:        in.TenantUUID,
		GoatID:          goatUUID,
		OccurredAt:      pgtype.Timestamptz{Time: in.Now, Valid: true},
		ActorID:         in.ActorUUID,
		SourceSystem:    textParam(in.Row.SourceSystem),
		SourceRecordID:  textParam(sourceRecordID(in.Row)),
		Payload:         eventPayload,
		DecisionID:      in.DecisionUUID,
		IdempotencyKey:  in.IdempotencyKey,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertIdentityDecisionEventForApply(ctx, importdb.InsertIdentityDecisionEventForApplyParams{
		DecisionID:      in.DecisionUUID,
		TenantID:        in.TenantUUID,
		EventID:         eventUUID,
		EventRecordedAt: eventRow.RecordedAt,
	}); err != nil {
		return applyOutcome{}, err
	}

	afterState, err := json.Marshal(map[string]any{
		"goat_id":                        goatRow.GoatID,
		"display_id":                     goatRow.DisplayID,
		"legacy_row_id":                  in.Row.LegacyRowID,
		"import_run_id":                  in.Command.ImportRunID,
		"identifier_ids":                 []string{rfidIdentifierID},
		"existing_old_tag_identifier_id": in.Plan.ExistingOldTagID,
		"row_version":                    goatRow.RowVersion,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	metadata, err := json.Marshal(map[string]any{
		"command":         rfidApplyCommandName,
		"idempotency_key": in.IdempotencyKey,
		"trace_id":        in.TraceID,
		"source_system":   in.Row.SourceSystem,
		"source_dataset":  in.Row.SourceDataset,
		"source_row_key":  in.Row.SourceRowKey,
		"link_mode":       "safe_old_tag_backfill_to_rfid",
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertAuditLogIdentifierAddedForApply(ctx, importdb.InsertAuditLogIdentifierAddedForApplyParams{
		TenantID:   in.TenantUUID,
		ActorID:    in.ActorUUID,
		ResourceID: goatUUID,
		DecisionID: in.DecisionUUID,
		AfterState: afterState,
		Metadata:   metadata,
		TraceID:    textParam(in.TraceID),
	}); err != nil {
		return applyOutcome{}, err
	}
	if r.afterAuditHook != nil {
		if err := r.afterAuditHook(ctx); err != nil {
			return applyOutcome{}, err
		}
	}

	envelope, err := importDomainEventEnvelope(importEventEnvelopeInput{
		EventID:        eventRow.EventID,
		EventType:      rfidApplyIDAddedEvent,
		GoatID:         goatRow.GoatID,
		TenantID:       in.Command.TenantID,
		ActorID:        in.Command.ActorID,
		IdempotencyKey: in.IdempotencyKey,
		TraceID:        in.TraceID,
		OccurredAt:     in.Now,
		RecordedAt:     eventRow.RecordedAt.Time,
		EvidenceRefs:   importEvidenceRefs(in.Command.ImportRunID, in.Row.LegacyRowID, in.Row.SourceSystem, in.Row.SourceRowKey),
		Payload:        json.RawMessage(eventPayload),
		SubjectType:    "identifier",
		SubjectID:      rfidIdentifierID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	headers, err := json.Marshal(map[string]any{
		"command":       rfidApplyCommandName,
		"trace_id":      in.TraceID,
		"decision_id":   in.DecisionID,
		"legacy_row_id": in.Row.LegacyRowID,
	})
	if err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.InsertOutboxIdentifierAddedMessageForApply(ctx, importdb.InsertOutboxIdentifierAddedMessageForApplyParams{
		TenantID:       in.TenantUUID,
		EventID:        eventUUID,
		AggregateID:    goatUUID,
		Payload:        envelope,
		Headers:        headers,
		IdempotencyKey: in.IdempotencyKey,
		TraceID:        textParam(in.TraceID),
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.MarkLegacyImportRowAutoLinked(ctx, importdb.MarkLegacyImportRowAutoLinkedParams{
		TenantID:      in.TenantUUID,
		LegacyRowID:   in.RowUUID,
		MatchedGoatID: goatUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.IncrementLegacyImportRunUpdatedGoatCount(ctx, importdb.IncrementLegacyImportRunUpdatedGoatCountParams{
		TenantID:    in.TenantUUID,
		ImportRunID: in.RunUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.CompleteIdempotencyKey(ctx, importdb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(rfidApplyResultType),
		ResultID:       goatUUID,
		IdempotencyKey: in.IdempotencyKey,
	}); err != nil {
		return applyOutcome{}, err
	}
	return applyOutcome{applied: true, goatID: goatRow.GoatID, updated: true}, nil
}

func (r *Repository) evaluatePendingApplyRow(ctx context.Context, q *importdb.Queries, tenantUUID pgtype.UUID, policy legacy_import.Policy, row pendingApplyRow, allowRFIDOnlyBlankSuffix bool, persistSourceBreed bool) (applyPlan, string, error) {
	if row.ProcessingState != legacy_import.StatePending && !(allowRFIDOnlyBlankSuffix && row.ProcessingState == legacy_import.StateNeedsReview) {
		return applyPlan{}, "", nil
	}
	if changed, err := q.HasLegacyImportRowWithDifferentHash(ctx, importdb.HasLegacyImportRowWithDifferentHashParams{
		TenantID:             tenantUUID,
		SourceSystem:         row.SourceSystem,
		SourceDataset:        row.SourceDataset,
		SourceRowKey:         row.SourceRowKey,
		SourceRowVersionHash: row.SourceRowVersionHash,
	}); err != nil {
		return applyPlan{}, "", err
	} else if changed {
		return applyPlan{}, "source_row_changed", nil
	}

	var payload map[string]any
	if err := json.Unmarshal(row.NormalizedPayload, &payload); err != nil {
		return applyPlan{}, "", fmt.Errorf("decode normalized payload: %w", err)
	}
	if reasons := stringSlice(payload["processing_reasons"]); len(reasons) > 0 && !(allowRFIDOnlyBlankSuffix && stringSliceOnlyReason(reasons, blankOldTagSuffixReason)) {
		return applyPlan{}, "normalized_payload_has_review_reasons", nil
	}
	rfid := strings.TrimSpace(stringValue(payload["rfid"]))
	if rfid == "" {
		return applyPlan{}, "missing_rfid", nil
	}
	sex := strings.TrimSpace(stringValue(payload["sex"]))
	if sex != "male" && sex != "female" {
		return applyPlan{}, "sex_requires_review", nil
	}
	plan := applyPlan{
		RFID:            rfid,
		Sex:             sex,
		AgeBand:         optionalString(payload["age"]),
		LifecycleStatus: "alive",
		Reason:          "Imported from approved Phase 1 RFID DB staging row.",
	}
	if allowRFIDOnlyBlankSuffix {
		plan.Reason = "Imported from approved Phase 1 RFID DB staging row with unresolved old-tag suffix; RFID-only policy applied."
	}

	tag := strings.TrimSpace(stringValue(payload["tag"]))
	if tag != "" {
		status, err := q.GetLegacyStatusMappingForApply(ctx, importdb.GetLegacyStatusMappingForApplyParams{
			SourceSystem:       policy.SourceSystem,
			NormalizedRawLabel: normalizedStatusLabel(tag),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return applyPlan{}, "unknown_status_mapping", nil
		}
		if err != nil {
			return applyPlan{}, "", err
		}
		if status.ReviewRequired {
			return applyPlan{}, "status_mapping_requires_review", nil
		}
		plan.LifecycleStatus = pgTextValue(status.LifecycleStatus, "alive")
		plan.ReproductiveStatus = pgTextStringPtr(status.ReproductiveStatus)
		plan.GrowthCohortTag = pgTextStringPtr(status.GrowthCohortTag)
		plan.ManagementStage = pgTextStringPtr(status.ManagementStage)
		plan.HealthStatus = pgTextStringPtr(status.HealthStatus)
	}

	breed := strings.TrimSpace(stringValue(payload["breed"]))
	if breed == "" {
		return applyPlan{}, "species_or_breed_requires_review", nil
	}
	breedRow, err := q.ResolveBreedAliasForApply(ctx, importdb.ResolveBreedAliasForApplyParams{
		NormalizedAlias: normalizedBreedAlias(breed),
		SourceSystem:    textParam(policy.SourceSystem),
	})
	breedID := breedRow.BreedID
	if errors.Is(err, pgx.ErrNoRows) {
		if !persistSourceBreed {
			plan.Breed = breed
			return plan, "", nil
		}
		ensuredBreedRow, ensureErr := q.EnsureSourceBreedAliasForApply(ctx, importdb.EnsureSourceBreedAliasForApplyParams{
			CanonicalName:   breed,
			NormalizedAlias: normalizedBreedAlias(breed),
			SourceSystem:    textParam(policy.SourceSystem),
		})
		if ensureErr != nil {
			err = fmt.Errorf("ensure source breed alias %q: %w", breed, ensureErr)
		} else {
			err = nil
		}
		breedID = ensuredBreedRow.BreedID
	}
	if err != nil {
		return applyPlan{}, "", err
	}
	plan.Breed = breed
	plan.BreedID = &breedID

	oldTag := strings.TrimSpace(stringValue(payload["normalized_old_tag"]))
	parkCode := strings.TrimSpace(stringValue(payload["normalized_park_code"]))
	if oldTag != "" && parkCode != "" {
		scopeKey := "park:" + parkCode
		exists, err := q.ActiveScopedIdentifierExistsForApply(ctx, importdb.ActiveScopedIdentifierExistsForApplyParams{
			TenantID:        tenantUUID,
			IdentifierType:  "old_tag",
			NormalizedValue: oldTag,
			ScopeKey:        scopeKey,
		})
		if err != nil {
			return applyPlan{}, "", err
		}
		if exists {
			backfillGoat, err := q.GetSafeBackfillOldTagGoatForApply(ctx, importdb.GetSafeBackfillOldTagGoatForApplyParams{
				TenantID:           tenantUUID,
				NormalizedValue:    oldTag,
				ScopeKey:           scopeKey,
				SourceSystem:       textParam(safeBackfillSourceSystem),
				SourceRecordPrefix: textParam(safeBackfillSourceRecordPattern),
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return applyPlan{}, "old_tag_same_scope_conflict", nil
			}
			if err != nil {
				return applyPlan{}, "", err
			}
			plan.ExistingGoatID = backfillGoat.GoatID
			plan.ExistingDisplayID = backfillGoat.DisplayID
			plan.ExistingOldTagID = backfillGoat.IdentifierID
			plan.Reason = "Attached RFID source row to safe old-tag backfill passport."
		}
		plan.OldTag = oldTag
		plan.OldTagScopeKey = scopeKey
	}
	rfidGoat, err := q.GetActiveRFIDGoatForApply(ctx, importdb.GetActiveRFIDGoatForApplyParams{
		TenantID:        tenantUUID,
		NormalizedValue: rfid,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return plan, "", nil
	}
	if err != nil {
		return applyPlan{}, "", err
	}
	if plan.ExistingGoatID == "" {
		return applyPlan{}, "rfid_already_linked", nil
	}
	if rfidGoat.GoatID == plan.ExistingGoatID {
		return applyPlan{}, "rfid_already_linked", nil
	}
	plan.ExistingRFIDGoatID = rfidGoat.GoatID
	plan.ExistingRFIDID = rfidGoat.IdentifierID
	plan.Reason = "Merged safe old-tag backfill passport into existing RFID passport."
	return plan, "", nil
}

func replayAppliedRow(ctx context.Context, qtx *importdb.Queries, tenantUUID, runUUID, rowUUID pgtype.UUID, idempotencyKey, requestHash string) (applyOutcome, error) {
	row, err := qtx.GetIdempotencyKey(ctx, idempotencyKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return applyOutcome{}, fmt.Errorf("idempotency key missing during replay")
	}
	if err != nil {
		return applyOutcome{}, err
	}
	if row.RequestHash != requestHash {
		return applyOutcome{}, fmt.Errorf("idempotency key reused with different source payload")
	}
	if row.Status != "completed" || row.ResultType != rfidApplyResultType || strings.TrimSpace(row.ResultID) == "" {
		return applyOutcome{}, fmt.Errorf("idempotency key is not completed")
	}
	goatUUID := mustUUID(row.ResultID)
	if err := qtx.MarkLegacyImportRowCreatedGoat(ctx, importdb.MarkLegacyImportRowCreatedGoatParams{
		TenantID:      tenantUUID,
		LegacyRowID:   rowUUID,
		MatchedGoatID: goatUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	if err := qtx.RefreshLegacyImportRunCreatedGoatCount(ctx, importdb.RefreshLegacyImportRunCreatedGoatCountParams{
		TenantID:    tenantUUID,
		ImportRunID: runUUID,
	}); err != nil {
		return applyOutcome{}, err
	}
	return applyOutcome{replay: true, goatID: row.ResultID}, nil
}

func pendingRowFromList(row importdb.ListPendingLegacyImportRowsForApplyRow) pendingApplyRow {
	return pendingApplyRow{
		LegacyRowID:          row.LegacyRowID,
		RowNumber:            row.RowNumber,
		SourceSystem:         row.SourceSystem,
		SourceDataset:        row.SourceDataset,
		SourceRecordID:       row.SourceRecordID,
		SourceRowKey:         row.SourceRowKey,
		SourceRowVersionHash: row.SourceRowVersionHash,
		RawPayload:           row.RawPayload,
		NormalizedPayload:    row.NormalizedPayload,
		ProcessingState:      row.ProcessingState,
		ErrorReason:          row.ErrorReason,
	}
}

func pendingRowFromBlankSuffixPolicyList(row importdb.ListRFIDApplyCandidateRowsForBlankSuffixPolicyRow) pendingApplyRow {
	return pendingApplyRow{
		LegacyRowID:          row.LegacyRowID,
		RowNumber:            row.RowNumber,
		SourceSystem:         row.SourceSystem,
		SourceDataset:        row.SourceDataset,
		SourceRecordID:       row.SourceRecordID,
		SourceRowKey:         row.SourceRowKey,
		SourceRowVersionHash: row.SourceRowVersionHash,
		RawPayload:           row.RawPayload,
		NormalizedPayload:    row.NormalizedPayload,
		ProcessingState:      row.ProcessingState,
		ErrorReason:          row.ErrorReason,
	}
}

func pendingRowFromLock(row importdb.LockLegacyImportRowForApplyRow) pendingApplyRow {
	return pendingApplyRow{
		LegacyRowID:          row.LegacyRowID,
		RowNumber:            row.RowNumber,
		SourceSystem:         row.SourceSystem,
		SourceDataset:        row.SourceDataset,
		SourceRecordID:       row.SourceRecordID,
		SourceRowKey:         row.SourceRowKey,
		SourceRowVersionHash: row.SourceRowVersionHash,
		RawPayload:           row.RawPayload,
		NormalizedPayload:    row.NormalizedPayload,
		ProcessingState:      row.ProcessingState,
		ErrorReason:          row.ErrorReason,
	}
}

type applyCandidateMode int

const (
	applyCandidateIneligible applyCandidateMode = iota
	applyCandidatePending
	applyCandidateRFIDOnlyBlankSuffix
)

func classifyApplyCandidate(row pendingApplyRow, allowRFIDOnlyBlankSuffix bool) applyCandidateMode {
	switch row.ProcessingState {
	case legacy_import.StatePending:
		return applyCandidatePending
	case legacy_import.StateNeedsReview:
		if allowRFIDOnlyBlankSuffix && reasonSetOnly(applyReasonSet(row), blankOldTagSuffixReason) {
			return applyCandidateRFIDOnlyBlankSuffix
		}
		return applyCandidateIneligible
	default:
		return applyCandidateIneligible
	}
}

func applyReasonSet(row pendingApplyRow) map[string]struct{} {
	reasons := map[string]struct{}{}
	if row.ErrorReason.Valid {
		addApplyReason(reasons, row.ErrorReason.String)
	}
	var payload map[string]any
	if err := json.Unmarshal(row.NormalizedPayload, &payload); err == nil {
		for _, reason := range stringSlice(payload["processing_reasons"]) {
			addApplyReason(reasons, reason)
		}
	}
	return reasons
}

func addApplyReason(reasons map[string]struct{}, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	for _, part := range strings.FieldsFunc(reason, func(r rune) bool { return r == ',' || r == ';' }) {
		part = strings.TrimSpace(part)
		if part != "" {
			reasons[part] = struct{}{}
		}
	}
}

func reasonSetOnly(reasons map[string]struct{}, expected string) bool {
	if len(reasons) != 1 {
		return false
	}
	_, ok := reasons[expected]
	return ok
}

func firstApplyReason(reasons map[string]struct{}) string {
	if len(reasons) == 0 {
		return ""
	}
	values := make([]string, 0, len(reasons))
	for reason := range reasons {
		values = append(values, reason)
	}
	sort.Strings(values)
	return values[0]
}

func stringSliceOnlyReason(reasons []string, expected string) bool {
	set := map[string]struct{}{}
	for _, reason := range reasons {
		addApplyReason(set, reason)
	}
	return reasonSetOnly(set, expected)
}

func stableApplyIdempotencyKey(tenantID, sourceSystem, sourceDataset, sourceRowKey, sourceRowVersionHash string) string {
	input := strings.Join([]string{tenantID, rfidApplyCommandName, sourceSystem, sourceDataset, sourceRowKey, sourceRowVersionHash}, "\x1f")
	sum := sha256.Sum256([]byte(input))
	return rfidApplyCommandName + ":" + hex.EncodeToString(sum[:])
}

func stableApplyRequestHash(row pendingApplyRow) string {
	input := strings.Join([]string{row.SourceSystem, row.SourceDataset, row.SourceRowKey, row.SourceRowVersionHash, string(row.NormalizedPayload)}, "\x1f")
	sum := sha256.Sum256([]byte(input))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func applyUnexpectedErrorReason(err error) string {
	return applyErrorSummary(err)
}

func applyErrorSummary(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return "apply_row_error"
	}
	parts := []string{"apply_row_error"}
	if code := safeApplyErrorToken(pgErr.Code); code != "" {
		parts = append(parts, "sqlstate="+code)
	}
	if constraint := safeApplyErrorToken(pgErr.ConstraintName); constraint != "" {
		parts = append(parts, "constraint="+constraint)
	}
	if table := safeApplyErrorToken(pgErr.TableName); table != "" {
		parts = append(parts, "table="+table)
	}
	if column := safeApplyErrorToken(pgErr.ColumnName); column != "" {
		parts = append(parts, "column="+column)
	}
	return strings.Join(parts, " ")
}

func isIsolatableApplyRowError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return strings.HasPrefix(pgErr.Code, "22") || strings.HasPrefix(pgErr.Code, "23")
}

var safeApplyErrorTokenRe = regexp.MustCompile(`[^a-zA-Z0-9_.:-]+`)

func safeApplyErrorToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = safeApplyErrorTokenRe.ReplaceAllString(value, "_")
	const maxTokenLength = 120
	if len(value) > maxTokenLength {
		value = value[:maxTokenLength]
	}
	return value
}

func normalizedStatusLabel(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

var breedAliasCleanRe = regexp.MustCompile(`[^a-z0-9]+`)

func normalizedBreedAlias(value string) string {
	cleaned := breedAliasCleanRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "_")
	return strings.Trim(cleaned, "_")
}

func appendReviewReason(payload []byte, reason string) []byte {
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return payload
	}
	reasons := stringSlice(doc["processing_reasons"])
	reasons = append(reasons, reason)
	doc["processing_reasons"] = reasons
	updated, err := json.Marshal(doc)
	if err != nil {
		return payload
	}
	return updated
}

func stringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := item.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func optionalString(value any) *string {
	text := strings.TrimSpace(stringValue(value))
	if text == "" {
		return nil
	}
	return &text
}

func planStringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func pgTextStringPtr(value pgtype.Text) *string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	return &value.String
}

func pgTextValue(value pgtype.Text, fallback string) string {
	if !value.Valid || value.String == "" {
		return fallback
	}
	return value.String
}

func sourceRecordID(row pendingApplyRow) string {
	if row.SourceRecordID.Valid && row.SourceRecordID.String != "" {
		return row.SourceRecordID.String
	}
	return row.SourceRowKey
}

func importEvidenceRefs(importRunID, legacyRowID, sourceSystem, sourceRowKey string) []map[string]any {
	return []map[string]any{
		{
			"evidence_type": "import_run",
			"evidence_id":   importRunID,
			"source_system": sourceSystem,
			"description":   "Approved legacy RFID import run.",
		},
		{
			"evidence_type": "source_record",
			"evidence_id":   legacyRowID,
			"source_system": sourceSystem,
			"description":   "Synthetic-safe legacy RFID source row reference: " + sourceRowKey,
		},
	}
}

type importDecisionRecordInput struct {
	DecisionID        string
	DecisionType      string
	DecisionResult    string
	TenantID          string
	ActorID           *string
	PolicyVersion     string
	Reason            string
	ImportRunID       string
	LegacyRowID       string
	SourceSystem      string
	SourceRowKey      string
	GoatID            string
	IdentifierActions []map[string]any
	IdempotencyKey    string
	TraceID           string
	CreatedAt         time.Time
}

func importDecisionRecordPayload(input importDecisionRecordInput) ([]byte, error) {
	decisionType := strings.TrimSpace(input.DecisionType)
	if decisionType == "" {
		decisionType = "create_goat"
	}
	decisionResult := strings.TrimSpace(input.DecisionResult)
	if decisionResult == "" {
		decisionResult = "imported_from_rfid_source"
	}
	payload := map[string]any{
		"decision_id":     input.DecisionID,
		"decision_type":   decisionType,
		"decision_result": decisionResult,
		"decision_state":  "approved",
		"decided_by_type": "import_policy",
		"decided_by":      input.ActorID,
		"reviewer_id":     input.ActorID,
		"policy_version":  input.PolicyVersion,
		"reason":          input.Reason,
		"source_record_ids": []string{
			input.LegacyRowID,
			input.SourceRowKey,
		},
		"affected_goats":     []map[string]any{{"goat_id": input.GoatID, "role": "affected"}},
		"identifier_actions": input.IdentifierActions,
		"evidence": map[string]any{
			"evidence_refs": importEvidenceRefs(input.ImportRunID, input.LegacyRowID, input.SourceSystem, input.SourceRowKey),
		},
		"idempotency_key": input.IdempotencyKey,
		"trace_id":        input.TraceID,
		"created_at":      formatEventTime(input.CreatedAt),
		"approved_at":     formatEventTime(input.CreatedAt),
		"decided_at":      formatEventTime(input.CreatedAt),
	}
	return json.Marshal(payload)
}

type importEventEnvelopeInput struct {
	EventID        string
	EventType      string
	GoatID         string
	TenantID       string
	ActorID        *string
	IdempotencyKey string
	TraceID        string
	OccurredAt     time.Time
	RecordedAt     time.Time
	EvidenceRefs   []map[string]any
	Payload        json.RawMessage
	SubjectType    string
	SubjectID      string
}

func importDomainEventEnvelope(input importEventEnvelopeInput) ([]byte, error) {
	eventType := strings.TrimSpace(input.EventType)
	if eventType == "" {
		eventType = rfidApplyCreatedEvent
	}
	subjectType := strings.TrimSpace(input.SubjectType)
	if subjectType == "" {
		subjectType = "goat"
	}
	subjectID := strings.TrimSpace(input.SubjectID)
	if subjectID == "" {
		subjectID = input.GoatID
	}
	envelope := map[string]any{
		"event_id":        input.EventID,
		"event_type":      eventType,
		"schema_version":  rfidApplySchemaVersion,
		"schema_ref":      rfidApplyEventSchemaRef,
		"aggregate_type":  "goat",
		"aggregate_id":    input.GoatID,
		"occurred_at":     formatEventTime(input.OccurredAt),
		"recorded_at":     formatEventTime(input.RecordedAt),
		"producer":        map[string]any{"service": "goatos-import", "module": "legacy_import"},
		"idempotency_key": input.IdempotencyKey,
		"actor":           map[string]any{"actor_type": "import_job", "actor_id": input.ActorID},
		"subject_type":    subjectType,
		"subject_id":      subjectID,
		"visibility_scope": map[string]any{
			"tenant_id": input.TenantID,
		},
		"evidence_refs": input.EvidenceRefs,
		"payload":       json.RawMessage(input.Payload),
		"trace_id":      input.TraceID,
	}
	return json.Marshal(envelope)
}

func formatEventTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000Z")
}

func mustUUID(value string) pgtype.UUID {
	uuid, err := uuidParam(value)
	if err != nil {
		panic(err)
	}
	return uuid
}

func textParam(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: strings.TrimSpace(value) != ""}
}
