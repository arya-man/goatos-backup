package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

func TestConflictMergeWritePathWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	t.Run("merge success writes decision links events audit outbox and replays", func(t *testing.T) {
		survivorID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		loserID := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		loserIdentifierID := insertActiveIdentifier(t, pool, meshaTenant, loserID, "old_tag", "synthetic-merge-transfer-1", "park:CPT", true)
		defaultRetiredIdentifierID := insertActiveIdentifier(t, pool, meshaTenant, loserID, "visual_tag", "synthetic-visual-retire-1", "goat:"+loserID, false)
		conflictID := insertSyntheticConflict(t, pool, meshaTenant, "possible_duplicate_goat", []string{survivorID, loserID})
		cmd := mergeConflictCommand(t, meshaTenant, "idem-merge-0001", conflictID, survivorID, []string{loserID}, []domain.IdentifierAction{{
			IdentifierID:    strPtr(loserIdentifierID),
			IdentifierType:  strPtr("old_tag"),
			IdentifierValue: strPtr("synthetic-merge-transfer-1"),
			Action:          "transfer",
		}}, 1)

		result, err := repo.ResolveConflict(ctx, cmd)
		if err != nil {
			t.Fatalf("ResolveConflict: %v", err)
		}
		if result.Replayed || result.State != "resolved" || result.Decision.DecisionType != "merge_goats" || result.Decision.DecisionResult != "same_goat_merge" {
			t.Fatalf("unexpected merge result: %#v", result)
		}
		if result.Merge.SurvivorGoatID != survivorID || len(result.Merge.MergedGoatIDs) != 1 || result.Merge.MergedGoatIDs[0] != loserID || len(result.Events) != 1 {
			t.Fatalf("unexpected merge payload: %#v", result)
		}

		var conflictState string
		var conflictRowVersion int
		var conflictDecisionID string
		if err := pool.QueryRow(ctx, `SELECT state, row_version, decision_id::text FROM identity_conflicts WHERE conflict_id = $1`, conflictID).Scan(&conflictState, &conflictRowVersion, &conflictDecisionID); err != nil {
			t.Fatal(err)
		}
		if conflictState != "resolved" || conflictRowVersion != 2 || conflictDecisionID != result.Decision.DecisionID {
			t.Fatalf("conflict state/version/decision = %s/%d/%s", conflictState, conflictRowVersion, conflictDecisionID)
		}

		var loserState, loserRedirect string
		if err := pool.QueryRow(ctx, `SELECT identity_state, merged_into_goat_id::text FROM goats WHERE goat_id = $1`, loserID).Scan(&loserState, &loserRedirect); err != nil {
			t.Fatal(err)
		}
		if loserState != "merged" || loserRedirect != survivorID {
			t.Fatalf("loser state/redirect = %s/%s", loserState, loserRedirect)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_merge_links WHERE survivor_goat_id = $1 AND merged_goat_id = $2 AND decision_id = $3`, survivorID, loserID, result.Decision.DecisionID); got != 1 {
			t.Fatalf("merge link rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_decision_goats WHERE decision_id = $1 AND goat_id = $2 AND role = 'survivor'`, result.Decision.DecisionID, survivorID); got != 1 {
			t.Fatalf("survivor decision goat rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_decision_goats WHERE decision_id = $1 AND goat_id = $2 AND role = 'merged'`, result.Decision.DecisionID, loserID); got != 1 {
			t.Fatalf("merged decision goat rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_decision_identifiers WHERE decision_id = $1 AND identifier_id = $2 AND action = 'transfer'`, result.Decision.DecisionID, loserIdentifierID); got != 1 {
			t.Fatalf("decision identifier rows = %d", got)
		}
		var transferredGoatID string
		var transferredPrimary bool
		if err := pool.QueryRow(ctx, `SELECT goat_id::text, is_primary_for_goat FROM goat_identifiers WHERE identifier_id = $1`, loserIdentifierID).Scan(&transferredGoatID, &transferredPrimary); err != nil {
			t.Fatal(err)
		}
		if transferredGoatID != survivorID || transferredPrimary {
			t.Fatalf("transferred identifier goat/primary = %s/%v", transferredGoatID, transferredPrimary)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identifiers WHERE goat_id = $1 AND status = 'active'`, loserID); got != 0 {
			t.Fatalf("active loser identifiers after merge = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identifiers WHERE identifier_id = $1 AND status = 'retired'`, defaultRetiredIdentifierID); got != 1 {
			t.Fatalf("default-retired identifier rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_decision_identifiers WHERE decision_id = $1 AND identifier_id = $2 AND action = 'retire'`, result.Decision.DecisionID, defaultRetiredIdentifierID); got != 1 {
			t.Fatalf("default-retired decision identifier rows = %d", got)
		}

		eventID := result.Events[0].EventID
		var recordedAt string
		if err := pool.QueryRow(ctx, `SELECT recorded_at::text FROM goat_identity_events WHERE identity_event_id = $1 AND goat_id = $2 AND event_type = 'goat.identity.merge_approved'`, eventID, survivorID).Scan(&recordedAt); err != nil {
			t.Fatal(err)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_decision_events WHERE decision_id = $1 AND event_id = $2 AND event_recorded_at::text = $3`, result.Decision.DecisionID, eventID, recordedAt); got != 1 {
			t.Fatalf("decision event rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM outbox_messages WHERE idempotency_key = $1 AND event_id = $2 AND aggregate_type = 'goat' AND aggregate_id = $3`, cmd.StoredIdempotencyKey, eventID, survivorID); got != 1 {
			t.Fatalf("outbox rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM audit_log WHERE action = 'goat.identity.merge_approved' AND resource_type = 'identity_conflict' AND resource_id = $1`, conflictID); got != 1 {
			t.Fatalf("audit rows = %d", got)
		}
		decisionPayload := queryBytes(t, pool, `SELECT evidence->'decision_record' FROM identity_decisions WHERE decision_id = $1`, result.Decision.DecisionID)
		validateDecisionRecord(t, decisionPayload)
		outboxPayload := queryBytes(t, pool, `SELECT payload FROM outbox_messages WHERE event_id = $1`, eventID)
		validateDomainEventEnvelope(t, outboxPayload)
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, conflictID)

		replay, err := repo.ResolveConflict(ctx, cmd)
		if err != nil {
			t.Fatalf("merge replay: %v", err)
		}
		if !replay.Replayed || replay.FirstResultID == nil || *replay.FirstResultID != conflictID || replay.Decision.DecisionID != result.Decision.DecisionID || replay.Events[0].EventID != eventID {
			t.Fatalf("unexpected replay: %#v", replay)
		}
		changed := mergeConflictCommand(t, meshaTenant, "idem-merge-0001", conflictID, survivorID, []string{loserID}, nil, 1)
		changed.RequestHash = "synthetic-different-request-hash"
		if _, err := repo.ResolveConflict(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected changed-body conflict, got %v", err)
		}

		if _, err := pool.Exec(ctx, `UPDATE goats SET updated_at = now() WHERE goat_id = $1`, loserID); err == nil {
			t.Fatal("expected merged goat normal update to fail after merge transaction")
		}
	})

	t.Run("merge guards conflict type membership tenant state and rollback", func(t *testing.T) {
		survivorID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		loserID := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		outsideID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		conflictID := insertSyntheticConflict(t, pool, meshaTenant, "possible_duplicate_goat", []string{survivorID, loserID})
		if _, err := repo.ResolveConflict(ctx, mergeConflictCommand(t, meshaTenant, "idem-merge-stale-0001", conflictID, survivorID, []string{loserID}, nil, 2)); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected stale row_version conflict, got %v", err)
		}
		if _, err := repo.ResolveConflict(ctx, mergeConflictCommand(t, secondTenant, "idem-merge-wrongtenant-0001", conflictID, survivorID, []string{loserID}, nil, 1)); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected wrong tenant not found, got %v", err)
		}
		if _, err := repo.ResolveConflict(ctx, mergeConflictCommand(t, meshaTenant, "idem-merge-outside-0001", conflictID, survivorID, []string{outsideID}, nil, 1)); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected outside goat conflict, got %v", err)
		}

		statusConflictID := insertSyntheticConflict(t, pool, meshaTenant, "status_mismatch", []string{survivorID, loserID})
		if _, err := repo.ResolveConflict(ctx, mergeConflictCommand(t, meshaTenant, "idem-merge-type-0001", statusConflictID, survivorID, []string{loserID}, nil, 1)); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected non-merge conflict type rejection, got %v", err)
		}

		rollbackConflictID := insertSyntheticConflict(t, pool, meshaTenant, "possible_duplicate_goat", []string{survivorID, loserID})
		rollback := mergeConflictCommand(t, meshaTenant, "idem-merge-rollback-0001", rollbackConflictID, survivorID, []string{loserID}, nil, 1)
		rollback.TraceID = "trace-merge-forced-rollback"
		repo.afterAuditHook = func(context.Context) error { return errors.New("forced merge rollback") }
		_, err := repo.ResolveConflict(ctx, rollback)
		repo.afterAuditHook = nil
		if err == nil {
			t.Fatal("expected forced rollback error")
		}
		var state string
		var rowVersion int
		var decisionID string
		if err := pool.QueryRow(ctx, `SELECT state, row_version, COALESCE(decision_id::text, '') FROM identity_conflicts WHERE conflict_id = $1`, rollbackConflictID).Scan(&state, &rowVersion, &decisionID); err != nil {
			t.Fatal(err)
		}
		if state != "open" || rowVersion != 1 || decisionID != "" {
			t.Fatalf("conflict mutated despite rollback: %s/%d/%s", state, rowVersion, decisionID)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_merge_links WHERE merged_goat_id = $1`, loserID); got != 0 {
			t.Fatalf("merge links after rollback = %d", got)
		}
		assertNoRows(t, pool, "idempotency after merge rollback", `SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1`, rollback.StoredIdempotencyKey)
		assertNoRows(t, pool, "event after merge rollback", `SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1`, rollback.StoredIdempotencyKey)
		assertNoRows(t, pool, "audit after merge rollback", `SELECT count(*) FROM audit_log WHERE trace_id = $1`, rollback.TraceID)
		assertNoRows(t, pool, "outbox after merge rollback", `SELECT count(*) FROM outbox_messages WHERE trace_id = $1`, rollback.TraceID)
	})

	t.Run("multi goat merge and redirect flattening keep merge link history", func(t *testing.T) {
		survivorID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		loserA := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		loserB := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		oldRedirect := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		seedHistoricalMerge(t, pool, oldRedirect, loserA)
		conflictID := insertSyntheticConflict(t, pool, meshaTenant, "possible_duplicate_goat", []string{survivorID, loserA, loserB})
		result, err := repo.ResolveConflict(ctx, mergeConflictCommand(t, meshaTenant, "idem-merge-multi-0001", conflictID, survivorID, []string{loserA, loserB}, nil, 1))
		if err != nil {
			t.Fatalf("multi merge: %v", err)
		}
		if len(result.Merge.MergedGoatIDs) != 2 || len(result.Events) != 2 {
			t.Fatalf("expected two losers/events: %#v", result)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_merge_links WHERE decision_id = $1`, result.Decision.DecisionID); got != 2 {
			t.Fatalf("new merge links = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_merge_links WHERE merged_goat_id = $1 AND survivor_goat_id = $2`, oldRedirect, loserA); got != 1 {
			t.Fatalf("historical merge link rewritten/missing, count=%d", got)
		}
		var redirect string
		if err := pool.QueryRow(ctx, `SELECT merged_into_goat_id::text FROM goats WHERE goat_id = $1`, oldRedirect).Scan(&redirect); err != nil {
			t.Fatal(err)
		}
		if redirect != survivorID {
			t.Fatalf("old redirected goat was not flattened: %s", redirect)
		}
	})

	t.Run("already redirected affected goat merges live representative", func(t *testing.T) {
		survivorID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		liveRepresentativeID := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		staleMemberID := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		liveRepresentativeIdentifierID := insertActiveIdentifier(t, pool, meshaTenant, liveRepresentativeID, "visual_tag", "synthetic-live-representative-retire-1", "goat:"+liveRepresentativeID, false)
		seedHistoricalMerge(t, pool, staleMemberID, liveRepresentativeID)
		conflictID := insertSyntheticConflict(t, pool, meshaTenant, "possible_duplicate_goat", []string{survivorID, staleMemberID})

		result, err := repo.ResolveConflict(ctx, mergeConflictCommand(t, meshaTenant, "idem-merge-redirected-affected-0001", conflictID, survivorID, []string{staleMemberID}, nil, 1))
		if err != nil {
			t.Fatalf("redirected affected merge: %v", err)
		}
		if len(result.Merge.MergedGoatIDs) != 1 || result.Merge.MergedGoatIDs[0] != liveRepresentativeID || len(result.Events) != 1 {
			t.Fatalf("expected live representative to be merged: %#v", result)
		}
		if len(result.Merge.RedirectWarnings) == 0 || result.Merge.RedirectWarnings[0].OriginalGoatID != staleMemberID || result.Merge.RedirectWarnings[0].RedirectGoatID != liveRepresentativeID {
			t.Fatalf("expected stale member redirect warning: %#v", result.Merge.RedirectWarnings)
		}
		var liveState, liveRedirect string
		if err := pool.QueryRow(ctx, `SELECT identity_state, merged_into_goat_id::text FROM goats WHERE goat_id = $1`, liveRepresentativeID).Scan(&liveState, &liveRedirect); err != nil {
			t.Fatal(err)
		}
		if liveState != "merged" || liveRedirect != survivorID {
			t.Fatalf("live representative state/redirect = %s/%s", liveState, liveRedirect)
		}
		var staleRedirect string
		if err := pool.QueryRow(ctx, `SELECT merged_into_goat_id::text FROM goats WHERE goat_id = $1`, staleMemberID).Scan(&staleRedirect); err != nil {
			t.Fatal(err)
		}
		if staleRedirect != survivorID {
			t.Fatalf("stale member redirect was not flattened: %s", staleRedirect)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_merge_links WHERE merged_goat_id = $1 AND survivor_goat_id = $2`, staleMemberID, liveRepresentativeID); got != 1 {
			t.Fatalf("historical stale-member merge link rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_merge_links WHERE merged_goat_id = $1 AND survivor_goat_id = $2 AND decision_id = $3`, liveRepresentativeID, survivorID, result.Decision.DecisionID); got != 1 {
			t.Fatalf("new live-representative merge link rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identifiers WHERE identifier_id = $1 AND status = 'retired'`, liveRepresentativeIdentifierID); got != 1 {
			t.Fatalf("live representative identifier default-retired rows = %d", got)
		}
		decisionPayload := queryBytes(t, pool, `SELECT evidence->'decision_record' FROM identity_decisions WHERE decision_id = $1`, result.Decision.DecisionID)
		var decisionRecord struct {
			Evidence struct {
				After struct {
					SurvivorGoatID           string   `json:"survivor_goat_id"`
					MergedGoatIDs            []string `json:"merged_goat_ids"`
					RequestedAffectedGoatIDs []string `json:"requested_affected_goat_ids"`
				} `json:"after"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(decisionPayload, &decisionRecord); err != nil {
			t.Fatalf("decode decision record: %v", err)
		}
		if decisionRecord.Evidence.After.SurvivorGoatID != survivorID || len(decisionRecord.Evidence.After.MergedGoatIDs) != 1 || decisionRecord.Evidence.After.MergedGoatIDs[0] != liveRepresentativeID {
			t.Fatalf("decision record did not use resolved merge goats: %#v", decisionRecord.Evidence.After)
		}
		if len(decisionRecord.Evidence.After.RequestedAffectedGoatIDs) != 1 || decisionRecord.Evidence.After.RequestedAffectedGoatIDs[0] != staleMemberID {
			t.Fatalf("decision record lost requested affected goats: %#v", decisionRecord.Evidence.After)
		}
	})

	t.Run("identifier collision defaults to retiring loser identifier", func(t *testing.T) {
		survivorID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		loserID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		if _, err := pool.Exec(ctx, `DROP INDEX goat_identifiers_active_rfid_unique`); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS goat_identifiers_active_rfid_unique ON goat_identifiers(normalized_value) WHERE identifier_type = 'rfid' AND status = 'active'`)
		}()
		_ = insertActiveIdentifier(t, pool, meshaTenant, survivorID, "rfid", "RFID-SYNTHETIC-COLLIDE", "global:rfid", false)
		loserIdentifierID := insertActiveIdentifier(t, pool, meshaTenant, loserID, "rfid", "RFID-SYNTHETIC-COLLIDE", "global:rfid", false)
		conflictID := insertSyntheticConflict(t, pool, meshaTenant, "rfid_already_linked", []string{survivorID, loserID})
		result, err := repo.ResolveConflict(ctx, mergeConflictCommand(t, meshaTenant, "idem-merge-collision-0001", conflictID, survivorID, []string{loserID}, nil, 1))
		if err != nil {
			t.Fatalf("collision default-retire merge: %v", err)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identifiers WHERE identifier_id = $1 AND status = 'retired'`, loserIdentifierID); got != 1 {
			t.Fatalf("loser colliding identifier retired rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identifiers WHERE identifier_type = 'rfid' AND normalized_value = 'RFID-SYNTHETIC-COLLIDE' AND status = 'active'`); got != 1 {
			t.Fatalf("active RFID collision rows = %d", got)
		}
		if len(result.Merge.AffectedIdentifiers) != 1 || result.Merge.AffectedIdentifiers[0].Action != "retire" {
			t.Fatalf("unexpected collision actions: %#v", result.Merge.AffectedIdentifiers)
		}
	})
}

func mergeConflictCommand(t *testing.T, tenantID, key, conflictID, survivorID string, affectedIDs []string, actions []domain.IdentifierAction, rowVersion int) ports.ResolveConflictCommand {
	t.Helper()
	evidenceRefs := []domain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   "synthetic-conflict-row-1",
		SourceSystem: strPtr("synthetic_import"),
		Description:  strPtr("Synthetic merge review note."),
	}}
	body := map[string]any{
		"decision_type":      "merge_goats",
		"decision_result":    "same_goat_merge",
		"survivor_goat_id":   survivorID,
		"affected_goat_ids":  affectedIDs,
		"identifier_actions": actions,
		"evidence_refs":      evidenceRefs,
		"reason":             "synthetic same goat merge after manual review",
		"row_version":        rowVersion,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	route := "/admin/identity/conflicts/" + conflictID + "/resolve"
	hash, err := app.CanonicalRequestHashWithSubject(tenantID, "resolveIdentityConflict", route, conflictID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.ResolveConflictCommand{
		TenantID:             tenantID,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: tenantID + ":resolveIdentityConflict:" + conflictID + ":" + key,
		IdempotencyScope:     "resolveIdentityConflict",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		ConflictID:           conflictID,
		DecisionType:         "merge_goats",
		DecisionResult:       "same_goat_merge",
		SurvivorGoatID:       survivorID,
		AffectedGoatIDs:      affectedIDs,
		IdentifierActions:    actions,
		EvidenceRefs:         evidenceRefs,
		Reason:               "synthetic same goat merge after manual review",
		RowVersion:           rowVersion,
	}
}

func insertSyntheticConflict(t *testing.T, pool *pgxpool.Pool, tenantID, conflictType string, goatIDs []string) string {
	t.Helper()
	var conflictID string
	arrayExpr := uuidArraySQL(goatIDs)
	sqlText := `
INSERT INTO identity_conflicts (
  tenant_id,
  conflict_type,
  severity,
  state,
  goat_ids,
  source_record_ids,
  evidence
) VALUES (
  $1,
  $2,
  'medium',
  'open',
  ` + arrayExpr + `,
  ARRAY['synthetic-conflict-source-1'],
  '{"scope_key":"synthetic"}'::jsonb
)
RETURNING conflict_id::text`
	if err := pool.QueryRow(context.Background(), sqlText, tenantID, conflictType).Scan(&conflictID); err != nil {
		t.Fatal(err)
	}
	for _, goatID := range goatIDs {
		if _, err := pool.Exec(context.Background(), `INSERT INTO identity_conflict_goats (conflict_id, tenant_id, goat_id, role) VALUES ($1, $2, $3, 'affected')`, conflictID, tenantID, goatID); err != nil {
			t.Fatal(err)
		}
	}
	return conflictID
}

func insertActiveIdentifier(t *testing.T, pool *pgxpool.Pool, tenantID, goatID, identifierType, value, scopeKey string, primary bool) string {
	t.Helper()
	var identifierID string
	normalized := strings.TrimSpace(value)
	if identifierType == "rfid" {
		normalized = strings.ToUpper(normalized)
	}
	if err := pool.QueryRow(context.Background(), `
INSERT INTO goat_identifiers (
  tenant_id,
  goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  is_primary_for_goat,
  status,
  valid_from,
  normalizer_version
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', now(), 'test_v1')
RETURNING identifier_id::text`, tenantID, goatID, identifierType, value, normalized, scopeKey, primary).Scan(&identifierID); err != nil {
		t.Fatal(err)
	}
	return identifierID
}

func seedHistoricalMerge(t *testing.T, pool *pgxpool.Pool, mergedGoatID, survivorGoatID string) {
	t.Helper()
	var decisionID string
	if err := pool.QueryRow(context.Background(), `
INSERT INTO identity_decisions (
  tenant_id,
  decision_type,
  decision_result,
  decision_state,
  decided_by_type,
  decided_by,
  policy_version,
  reviewer_id,
  evidence,
  approved_at,
  decided_at
) VALUES (
  $1,
  'merge_goats',
  'same_goat_merge',
  'approved',
  'human',
  $2,
  'phase1-manual-correction-review-v1',
  $2,
  '{"synthetic":true}'::jsonb,
  now(),
  now()
)
RETURNING decision_id::text`, meshaTenant, correctionActor).Scan(&decisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE goats SET identity_state = 'merged', merged_into_goat_id = $1 WHERE goat_id = $2`, survivorGoatID, mergedGoatID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO goat_merge_links (tenant_id, survivor_goat_id, merged_goat_id, decision_id, reason, created_by) VALUES ($1, $2, $3, $4, 'synthetic historical merge', $5)`, meshaTenant, survivorGoatID, mergedGoatID, decisionID, correctionActor); err != nil {
		t.Fatal(err)
	}
}

func uuidArraySQL(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "'"+value+"'::uuid")
	}
	return "ARRAY[" + strings.Join(quoted, ", ") + "]"
}
