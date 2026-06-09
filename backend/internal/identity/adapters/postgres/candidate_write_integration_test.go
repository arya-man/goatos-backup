package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

func TestCandidateListAndRejectWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	t.Run("list returns actionable queue with row versions and cursor", func(t *testing.T) {
		proposed := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-09T10:00:00Z")
		needsReview := insertSyntheticCandidate(t, pool, meshaTenant, "needs_review", "import_policy", "2026-06-09T09:00:00Z")
		_ = insertSyntheticCandidate(t, pool, meshaTenant, "rejected", "system_rule", "2026-06-09T11:00:00Z")
		_ = insertSyntheticCandidate(t, pool, meshaTenant, "approved", "system_rule", "2026-06-09T12:00:00Z")
		_ = insertSyntheticCandidate(t, pool, meshaTenant, "expired", "system_rule", "2026-06-09T13:00:00Z")
		_ = insertSyntheticCandidate(t, pool, secondTenant, "proposed", "system_rule", "2026-06-09T14:00:00Z")

		first, cursor, err := repo.ListCandidates(ctx, ports.ListCandidatesParams{TenantID: meshaTenant, Limit: 1})
		if err != nil {
			t.Fatalf("ListCandidates first page: %v", err)
		}
		if len(first) != 1 || first[0].CandidateID != proposed || first[0].State != "proposed" || first[0].RowVersion != 1 || cursor == nil {
			t.Fatalf("unexpected first page: items=%#v cursor=%v", first, cursor)
		}
		second, next, err := repo.ListCandidates(ctx, ports.ListCandidatesParams{TenantID: meshaTenant, Limit: 10, Cursor: cursor})
		if err != nil {
			t.Fatalf("ListCandidates second page: %v", err)
		}
		if len(second) != 1 || second[0].CandidateID != needsReview || second[0].State != "needs_review" || second[0].CreatedBy != "import_policy" || next != nil {
			t.Fatalf("unexpected second page: items=%#v next=%v", second, next)
		}
		if second[0].ProposedGoatID == nil || second[0].CandidateGoatID == nil || len(second[0].MatchReasons) != 2 {
			t.Fatalf("candidate summary missing fields: %#v", second[0])
		}
		if _, _, err := repo.ListCandidates(ctx, ports.ListCandidatesParams{TenantID: meshaTenant, Limit: 10, Cursor: strPtr("not-a-valid-cursor")}); !errors.Is(err, ports.ErrInvalidCursor) {
			t.Fatalf("expected invalid cursor error, got %v", err)
		}
	})

	t.Run("reject proposed candidate writes decision audit only and replays", func(t *testing.T) {
		candidateID := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-09T15:00:00Z")
		goatA := "10000000-0000-4000-8000-000000000001"
		goatB := "10000000-0000-4000-8000-000000000002"
		goatAVersion := goatRowVersion(t, pool, goatA)
		goatBVersion := goatRowVersion(t, pool, goatB)
		cmd := rejectCandidateCommand(t, meshaTenant, "idem-candidate-reject-0001", candidateID, 1, "synthetic candidate rejection")

		result, err := repo.RejectCandidate(ctx, cmd)
		if err != nil {
			t.Fatalf("RejectCandidate: %v", err)
		}
		if result.Replayed || result.Candidate.CandidateID != candidateID || result.Candidate.State != "rejected" || result.Candidate.RowVersion != 2 {
			t.Fatalf("unexpected reject result: %#v", result)
		}
		if result.Decision.DecisionType != "reject_match" || result.Decision.DecisionResult != "candidate_rejected" || result.Decision.DecisionState != "rejected" {
			t.Fatalf("unexpected decision: %#v", result.Decision)
		}
		var reviewedBy, decisionID string
		var reviewedAt *time.Time
		if err := pool.QueryRow(ctx, `SELECT reviewed_by::text, reviewed_at, decision_id::text FROM identity_match_candidates WHERE candidate_id = $1`, candidateID).Scan(&reviewedBy, &reviewedAt, &decisionID); err != nil {
			t.Fatal(err)
		}
		if reviewedBy != correctionActor || reviewedAt == nil || decisionID != result.Decision.DecisionID {
			t.Fatalf("review columns = %s/%v/%s", reviewedBy, reviewedAt, decisionID)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM audit_log WHERE action = 'identity.match_candidate.rejected' AND resource_id = $1`, candidateID); got != 1 {
			t.Fatalf("candidate audit rows = %d", got)
		}
		if got := goatRowVersion(t, pool, goatA); got != goatAVersion {
			t.Fatalf("candidate reject bumped proposed goat row_version = %d, want %d", got, goatAVersion)
		}
		if got := goatRowVersion(t, pool, goatB); got != goatBVersion {
			t.Fatalf("candidate reject bumped candidate goat row_version = %d, want %d", got, goatBVersion)
		}
		assertNoRows(t, pool, "candidate reject goat events", `SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1`, cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "candidate reject outbox", `SELECT count(*) FROM outbox_messages WHERE idempotency_key = $1`, cmd.StoredIdempotencyKey)
		decisionPayload := queryBytes(t, pool, `SELECT evidence->'decision_record' FROM identity_decisions WHERE decision_id = $1`, result.Decision.DecisionID)
		validateDecisionRecord(t, decisionPayload)
		var decisionRecord map[string]any
		if err := json.Unmarshal(decisionPayload, &decisionRecord); err != nil {
			t.Fatal(err)
		}
		evidence := decisionRecord["evidence"].(map[string]any)
		if len(evidence["evidence_refs"].([]any)) != 1 || decisionRecord["policy_version"] != "phase1-manual-correction-review-v1" {
			t.Fatalf("decision evidence/policy mismatch: %#v", decisionRecord)
		}
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, candidateID)

		replay, err := repo.RejectCandidate(ctx, cmd)
		if err != nil {
			t.Fatalf("RejectCandidate replay: %v", err)
		}
		if !replay.Replayed || replay.Candidate.CandidateID != candidateID || replay.Decision.DecisionID != result.Decision.DecisionID || replay.Candidate.RowVersion != 2 {
			t.Fatalf("unexpected replay: %#v", replay)
		}

		changed := rejectCandidateCommand(t, meshaTenant, "idem-candidate-reject-0001", candidateID, 1, "synthetic changed candidate rejection")
		if _, err := repo.RejectCandidate(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected idempotency conflict, got %v", err)
		}
	})

	t.Run("reject needs_review candidate succeeds", func(t *testing.T) {
		candidateID := insertSyntheticCandidate(t, pool, meshaTenant, "needs_review", "ai_proposal", "2026-06-09T16:00:00Z")
		cmd := rejectCandidateCommand(t, meshaTenant, "idem-candidate-reject-needs-review-0001", candidateID, 1, "synthetic needs-review rejection")
		result, err := repo.RejectCandidate(ctx, cmd)
		if err != nil {
			t.Fatalf("RejectCandidate needs_review: %v", err)
		}
		if result.Candidate.State != "rejected" || result.Candidate.RowVersion != 2 {
			t.Fatalf("unexpected needs_review result: %#v", result)
		}
	})

	t.Run("guard failures and rollback leave no partial writes", func(t *testing.T) {
		staleID := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-09T17:00:00Z")
		stale := rejectCandidateCommand(t, meshaTenant, "idem-candidate-stale-0001", staleID, 2, "synthetic stale reject")
		if _, err := repo.RejectCandidate(ctx, stale); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected stale write conflict, got %v", err)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1`, stale.StoredIdempotencyKey); got != 0 {
			t.Fatalf("stale idempotency rows = %d", got)
		}

		terminalID := insertSyntheticCandidate(t, pool, meshaTenant, "approved", "system_rule", "2026-06-09T18:00:00Z")
		terminal := rejectCandidateCommand(t, meshaTenant, "idem-candidate-terminal-0001", terminalID, 1, "synthetic terminal reject")
		if _, err := repo.RejectCandidate(ctx, terminal); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected terminal write conflict, got %v", err)
		}

		wrongTenant := rejectCandidateCommand(t, meshaTenant, "idem-candidate-wrong-tenant-0001", insertSyntheticCandidate(t, pool, secondTenant, "proposed", "system_rule", "2026-06-09T19:00:00Z"), 1, "synthetic wrong tenant reject")
		if _, err := repo.RejectCandidate(ctx, wrongTenant); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected wrong tenant not found, got %v", err)
		}

		rollbackID := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-09T20:00:00Z")
		rollback := rejectCandidateCommand(t, meshaTenant, "idem-candidate-rollback-0001", rollbackID, 1, "synthetic rollback candidate")
		rollback.TraceID = "trace-candidate-forced-rollback"
		repo.afterAuditHook = func(context.Context) error { return errors.New("forced rollback") }
		_, err := repo.RejectCandidate(ctx, rollback)
		repo.afterAuditHook = nil
		if err == nil {
			t.Fatal("expected forced rollback error")
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_match_candidates WHERE candidate_id = $1 AND state = 'proposed' AND row_version = 1 AND decision_id IS NULL`, rollbackID); got != 1 {
			t.Fatalf("rollback candidate state rows = %d", got)
		}
		assertNoRows(t, pool, "idempotency after candidate rollback", `SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1`, rollback.StoredIdempotencyKey)
		assertNoRows(t, pool, "decision after candidate rollback", `SELECT count(*) FROM identity_decisions WHERE evidence->'decision_record'->>'trace_id' = $1`, rollback.TraceID)
		assertNoRows(t, pool, "audit after candidate rollback", `SELECT count(*) FROM audit_log WHERE trace_id = $1`, rollback.TraceID)
	})

	t.Run("concurrent same key rejects once and replays loser", func(t *testing.T) {
		candidateID := insertSyntheticCandidate(t, pool, meshaTenant, "proposed", "system_rule", "2026-06-09T21:00:00Z")
		cmd := rejectCandidateCommand(t, meshaTenant, "idem-candidate-concurrent-0001", candidateID, 1, "synthetic concurrent reject")
		var hookCalls int32
		repo.afterAuditHook = func(context.Context) error {
			if atomic.AddInt32(&hookCalls, 1) == 1 {
				time.Sleep(300 * time.Millisecond)
			}
			return nil
		}
		defer func() { repo.afterAuditHook = nil }()

		start := make(chan struct{})
		results := make(chan *ports.RejectCandidateResult, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				result, err := repo.RejectCandidate(ctx, cmd)
				if err != nil {
					errs <- err
					return
				}
				results <- result
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			t.Fatalf("concurrent candidate reject error: %v", err)
		}
		var fresh, replay int
		for result := range results {
			if result.Replayed {
				replay++
			} else {
				fresh++
			}
		}
		if fresh != 1 || replay != 1 {
			t.Fatalf("fresh=%d replay=%d", fresh, replay)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_decisions WHERE evidence->'decision_record'->>'idempotency_key' = $1`, cmd.StoredIdempotencyKey); got != 1 {
			t.Fatalf("concurrent candidate decisions = %d", got)
		}
	})
}

func insertSyntheticCandidate(t *testing.T, pool *pgxpool.Pool, tenantID, state, createdBy, createdAt string) string {
	t.Helper()
	proposedGoat := "10000000-0000-4000-8000-000000000001"
	candidateGoat := "10000000-0000-4000-8000-000000000002"
	if tenantID == secondTenant {
		proposedGoat = "10000000-0000-4000-8000-000000000101"
		candidateGoat = "10000000-0000-4000-8000-000000000101"
	}
	var candidateID string
	if err := pool.QueryRow(context.Background(), `
INSERT INTO identity_match_candidates (
  tenant_id,
  proposed_goat_id,
  candidate_goat_id,
  match_score,
  match_reasons,
  state,
  created_by,
  created_at
) VALUES (
  $1,
  $2,
  $3,
  0.93,
  '["synthetic tag overlap","synthetic location match"]'::jsonb,
  $4,
  $5,
  $6::timestamptz
)
RETURNING candidate_id::text`, tenantID, proposedGoat, candidateGoat, state, createdBy, createdAt).Scan(&candidateID); err != nil {
		t.Fatal(err)
	}
	return candidateID
}

func rejectCandidateCommand(t *testing.T, tenantID, key, candidateID string, rowVersion int, reason string) ports.RejectCandidateCommand {
	t.Helper()
	evidenceRefs := []domain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   "synthetic-candidate-row-1",
		SourceSystem: strPtr("synthetic_import"),
		Description:  strPtr("Synthetic candidate review note."),
	}}
	body := map[string]any{
		"reason":        reason,
		"evidence_refs": evidenceRefs,
		"row_version":   rowVersion,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	route := "/admin/identity/candidates/" + candidateID + "/reject"
	hash, err := app.CanonicalRequestHashWithSubject(tenantID, "rejectIdentityCandidate", route, candidateID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.RejectCandidateCommand{
		TenantID:             tenantID,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: tenantID + ":rejectIdentityCandidate:" + candidateID + ":" + key,
		IdempotencyScope:     "rejectIdentityCandidate",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		CandidateID:          candidateID,
		Reason:               reason,
		EvidenceRefs:         evidenceRefs,
		RowVersion:           rowVersion,
	}
}
