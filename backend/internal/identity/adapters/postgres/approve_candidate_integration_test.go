package postgres

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	approveGoatA = "10000000-0000-4000-8000-000000000001"
	approveGoatB = "10000000-0000-4000-8000-000000000002"
)

func TestApproveCandidateWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	t.Run("attach explicit identifier approves candidate in one transaction", func(t *testing.T) {
		candidateID := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-10T10:00:00Z")
		goatVersion := goatRowVersion(t, pool, approveGoatA)
		cmd := approveAttachCommand(t, "idem-approve-attach-0001", candidateID, approveGoatA, goatVersion, "rfid", "RFID_APPROVE_0001", 1)

		result, err := repo.ApproveCandidate(ctx, cmd)
		if err != nil {
			t.Fatalf("ApproveCandidate attach: %v", err)
		}
		if result.Replayed || result.Candidate.State != "approved" || result.Candidate.RowVersion != 2 {
			t.Fatalf("unexpected attach result: %#v", result)
		}
		if result.Decision.DecisionType != "attach_identifier" || result.Decision.DecisionResult != "identifier_attached" {
			t.Fatalf("unexpected decision: %#v", result.Decision)
		}
		if len(result.Events) != 1 || result.Events[0].EventType != "goat.identifier.added" {
			t.Fatalf("unexpected events: %#v", result.Events)
		}
		if got := goatRowVersion(t, pool, approveGoatA); got != goatVersion+1 {
			t.Fatalf("goat row_version = %d, want %d", got, goatVersion+1)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identifiers WHERE goat_id = $1 AND normalized_value = 'RFID_APPROVE_0001' AND status = 'active'`, approveGoatA); got != 1 {
			t.Fatalf("attached identifier rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1 AND event_type = 'goat.identifier.added'`, cmd.StoredIdempotencyKey); got != 1 {
			t.Fatalf("attach event rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM outbox_messages WHERE idempotency_key = $1`, cmd.StoredIdempotencyKey); got != 1 {
			t.Fatalf("attach outbox rows = %d", got)
		}
		var candidateState, decisionID string
		if err := pool.QueryRow(ctx, `SELECT state, decision_id::text FROM identity_match_candidates WHERE candidate_id = $1`, candidateID).Scan(&candidateState, &decisionID); err != nil {
			t.Fatal(err)
		}
		if candidateState != "approved" || decisionID != result.Decision.DecisionID {
			t.Fatalf("candidate columns = %s/%s", candidateState, decisionID)
		}
		decisionPayload := queryBytes(t, pool, `SELECT evidence->'decision_record' FROM identity_decisions WHERE decision_id = $1`, result.Decision.DecisionID)
		validateDecisionRecord(t, decisionPayload)
		if got := countRows(t, pool, `SELECT count(*) FROM identity_decisions WHERE decision_id = $1 AND evidence->>'candidate_id' = $2`, result.Decision.DecisionID, candidateID); got != 1 {
			t.Fatalf("decision evidence missing candidate_id reference")
		}
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, candidateID)

		replay, err := repo.ApproveCandidate(ctx, cmd)
		if err != nil {
			t.Fatalf("ApproveCandidate replay: %v", err)
		}
		if !replay.Replayed || replay.Candidate.CandidateID != candidateID || replay.Decision.DecisionID != result.Decision.DecisionID {
			t.Fatalf("unexpected replay: %#v", replay)
		}

		changed := approveAttachCommand(t, "idem-approve-attach-0001", candidateID, approveGoatA, goatVersion, "rfid", "RFID_APPROVE_DIFFERENT", 1)
		if _, err := repo.ApproveCandidate(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected idempotency conflict, got %v", err)
		}
	})

	t.Run("attach with stale candidate row_version conflicts and rolls back", func(t *testing.T) {
		candidateID := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-10T11:00:00Z")
		goatVersion := goatRowVersion(t, pool, approveGoatA)
		cmd := approveAttachCommand(t, "idem-approve-stale-0001", candidateID, approveGoatA, goatVersion, "rfid", "RFID_APPROVE_STALE", 2)
		if _, err := repo.ApproveCandidate(ctx, cmd); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected stale write conflict, got %v", err)
		}
		assertNoRows(t, pool, "stale attach idempotency", `SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1`, cmd.StoredIdempotencyKey)
		if got := countRows(t, pool, `SELECT count(*) FROM identity_match_candidates WHERE candidate_id = $1 AND state = 'proposed' AND row_version = 1`, candidateID); got != 1 {
			t.Fatalf("stale candidate state rows = %d", got)
		}
	})

	t.Run("attach extraction without a legacy RFID is rejected", func(t *testing.T) {
		// Synthetic candidates have no linked legacy row, so extraction fails closed.
		candidateID := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-10T12:00:00Z")
		goatVersion := goatRowVersion(t, pool, approveGoatA)
		cmd := approveAttachCommand(t, "idem-approve-extract-0001", candidateID, approveGoatA, goatVersion, "", "", 1)
		cmd.ExtractFromLegacyRow = true
		cmd.IdentifierType = ""
		cmd.IdentifierValue = ""
		cmd.NormalizedValue = ""
		if _, err := repo.ApproveCandidate(ctx, cmd); !errors.Is(err, ports.ErrCannotExtractIdentifier) {
			t.Fatalf("expected cannot-extract error, got %v", err)
		}
	})

	t.Run("create_goat decision is blocked at the repository", func(t *testing.T) {
		candidateID := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-10T12:30:00Z")
		cmd := approveAttachCommand(t, "idem-approve-create-0001", candidateID, approveGoatA, goatRowVersion(t, pool, approveGoatA), "rfid", "RFID_APPROVE_CREATE", 1)
		cmd.DecisionType = "create_goat"
		if _, err := repo.ApproveCandidate(ctx, cmd); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected write conflict for create_goat, got %v", err)
		}
	})

	t.Run("merge with survivor outside the candidate goats is rejected", func(t *testing.T) {
		candidateID := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-10T13:00:00Z")
		cmd := approveMergeCommand(t, "idem-approve-merge-guard-0001", candidateID, "10000000-0000-4000-8000-000000000003", []string{approveGoatB}, 1)
		if _, err := repo.ApproveCandidate(ctx, cmd); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected write conflict for foreign survivor, got %v", err)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_match_candidates WHERE candidate_id = $1 AND state = 'proposed'`, candidateID); got != 1 {
			t.Fatalf("guard merge must not approve candidate; rows = %d", got)
		}
	})

	t.Run("merge approves candidate and merges the two candidate goats", func(t *testing.T) {
		candidateID := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-10T14:00:00Z")
		cmd := approveMergeCommand(t, "idem-approve-merge-0001", candidateID, approveGoatA, []string{approveGoatB}, 1)
		result, err := repo.ApproveCandidate(ctx, cmd)
		if err != nil {
			t.Fatalf("ApproveCandidate merge: %v", err)
		}
		if result.Candidate.State != "approved" || result.Decision.DecisionType != "merge_goats" || result.Merge == nil {
			t.Fatalf("unexpected merge result: %#v", result)
		}
		if result.Merge.SurvivorGoatID != approveGoatA || len(result.Merge.MergedGoatIDs) != 1 || result.Merge.MergedGoatIDs[0] != approveGoatB {
			t.Fatalf("unexpected merge survivor/losers: %#v", result.Merge)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goats WHERE goat_id = $1 AND identity_state = 'merged' AND merged_into_goat_id = $2`, approveGoatB, approveGoatA); got != 1 {
			t.Fatalf("merged goat rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_merge_links WHERE survivor_goat_id = $1 AND merged_goat_id = $2`, approveGoatA, approveGoatB); got != 1 {
			t.Fatalf("merge link rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1 AND event_type = 'goat.identity.merge_approved'`, cmd.StoredIdempotencyKey); got != 1 {
			t.Fatalf("merge event rows = %d", got)
		}
		decisionPayload := queryBytes(t, pool, `SELECT evidence->'decision_record' FROM identity_decisions WHERE decision_id = $1`, result.Decision.DecisionID)
		validateDecisionRecord(t, decisionPayload)
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, candidateID)
	})
}

func approveBaseCommand(t *testing.T, key, candidateID string, rowVersion int, rawBody string) ports.ApproveCandidateCommand {
	t.Helper()
	route := "/admin/identity/candidates/" + candidateID + "/approve"
	hash, err := app.CanonicalRequestHashWithSubject(meshaTenant, "approveIdentityCandidate", route, candidateID, []byte(rawBody))
	if err != nil {
		t.Fatal(err)
	}
	return ports.ApproveCandidateCommand{
		TenantID:             meshaTenant,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: meshaTenant + ":approveIdentityCandidate:" + candidateID + ":" + key,
		IdempotencyScope:     "approveIdentityCandidate",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		CandidateID:          candidateID,
		Reason:               "synthetic candidate approval",
		EvidenceRefs: []domain.EvidenceRef{{
			EvidenceType: "source_record",
			EvidenceID:   "synthetic-approve-row-1",
			SourceSystem: strPtr("synthetic_import"),
		}},
		RowVersion: rowVersion,
	}
}

func approveAttachCommand(t *testing.T, key, candidateID, goatID string, goatRowVersion int, identifierType, identifierValue string, rowVersion int) ports.ApproveCandidateCommand {
	t.Helper()
	rawBody := `{"decision_type":"attach_identifier","target_goat_id":"` + goatID + `","identifier_type":"` + identifierType + `","identifier_value":"` + identifierValue + `","row_version":` + itoa(rowVersion) + `}`
	cmd := approveBaseCommand(t, key, candidateID, rowVersion, rawBody)
	cmd.DecisionType = "attach_identifier"
	cmd.GoatID = goatID
	cmd.GoatRowVersion = goatRowVersion
	cmd.IdentifierType = identifierType
	cmd.IdentifierValue = identifierValue
	cmd.NormalizedValue = identifierValue
	return cmd
}

func approveMergeCommand(t *testing.T, key, candidateID, survivorGoatID string, affected []string, rowVersion int) ports.ApproveCandidateCommand {
	t.Helper()
	rawBody := `{"decision_type":"merge_goats","survivor_goat_id":"` + survivorGoatID + `","row_version":` + itoa(rowVersion) + `}`
	cmd := approveBaseCommand(t, key, candidateID, rowVersion, rawBody)
	cmd.DecisionType = "merge_goats"
	cmd.SurvivorGoatID = survivorGoatID
	cmd.AffectedGoatIDs = affected
	return cmd
}

func itoa(v int) string { return strconv.Itoa(v) }
