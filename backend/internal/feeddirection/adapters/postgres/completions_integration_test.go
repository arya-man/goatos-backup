package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// E2E proof for the feed.direction.completed producer (domain-event-registry.json).
//
// These prove the REAL production write path -- CompleteSession opening a transaction and, in that
// one transaction, writing the canonical feed_direction_session_completions row, the audit_log row,
// and the outbox_messages event -- against the real schema, plus the two idempotency axes and the
// serving read the generator overlays. A green pass here is the closure evidence the ledger rule
// requires: the event is produced on the same path a mobile "Done" tap drives, not a seeded readback.

const fdActor = "fd000000-0000-4000-8000-000000009001"

func completeParams() ports.CompleteSessionParams {
	return ports.CompleteSessionParams{
		TenantID:       fdTenant,
		ParkID:         fdPark,
		ShedID:         fdShedA,
		SessionNo:      1,
		TargetDate:     businessDay(2026, 7, 22),
		Workflow:       domain.WorkflowNormal,
		CompletedBy:    fdActor,
		IdempotencyKey: "feed-complete-key-0001",
		ActorID:        fdActor,
		ActorType:      "operator",
		TraceID:        "trace-feed-complete-1",
	}
}

func TestCompleteSessionWritesCanonicalAuditAndOutboxInOneTx(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	res, err := repo.CompleteSession(ctx, completeParams())
	if err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}
	if !res.Applied {
		t.Fatalf("first completion Applied = false, want true")
	}
	if res.CompletionID == "" {
		t.Fatal("first completion returned an empty completion id")
	}
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed", res.Status)
	}

	// Canonical row.
	var canonicalStatus, workflow string
	var sessionNo int32
	if err := pool.QueryRow(ctx, `
SELECT status, workflow, session_no
FROM feed_direction_session_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, fdTenant, res.CompletionID).
		Scan(&canonicalStatus, &workflow, &sessionNo); err != nil {
		t.Fatalf("read canonical completion: %v", err)
	}
	if canonicalStatus != "completed" || workflow != domain.WorkflowNormal || sessionNo != 1 {
		t.Fatalf("canonical row = (%s,%s,%d), want (completed,normal,1)", canonicalStatus, workflow, sessionNo)
	}

	// Audit row, same transaction.
	var auditCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM audit_log
WHERE tenant_id = $1::uuid AND action = 'feed.direction.completed' AND resource_id = $2::uuid`,
		fdTenant, res.CompletionID).Scan(&auditCount); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit rows = %d, want 1", auditCount)
	}

	// Outbox event, same transaction.
	var outboxCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'feed.direction.completed' AND aggregate_id = $2::uuid`,
		fdTenant, res.CompletionID).Scan(&outboxCount); err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox rows = %d, want 1", outboxCount)
	}

	// Serving read overlays this shed-session.
	completed, err := repo.ListCompletedSessions(ctx, fdTenant, fdPark, businessDay(2026, 7, 22))
	if err != nil {
		t.Fatalf("ListCompletedSessions: %v", err)
	}
	if len(completed) != 1 || completed[0].ShedID != fdShedA || completed[0].SessionNo != 1 || completed[0].Workflow != domain.WorkflowNormal {
		t.Fatalf("completed sessions = %+v, want one (Shed A, session 1, normal)", completed)
	}
}

func TestCompleteSessionExactReplayRunsNoSideEffects(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	first, err := repo.CompleteSession(ctx, completeParams())
	if err != nil {
		t.Fatalf("first CompleteSession: %v", err)
	}

	replay, err := repo.CompleteSession(ctx, completeParams())
	if err != nil {
		t.Fatalf("replay CompleteSession: %v", err)
	}
	if replay.Applied {
		t.Fatal("exact replay Applied = true, want false")
	}
	if replay.CompletionID != first.CompletionID {
		t.Fatalf("replay completion id = %s, want original %s", replay.CompletionID, first.CompletionID)
	}

	// No duplicate side effects.
	assertRowCount(t, ctx, pool, "feed_direction_session_completions", first.CompletionID, 1)
	assertOutboxCount(t, ctx, pool, first.CompletionID, 1)
}

func TestCompleteSessionSecondKeySameShedSessionIsNoop(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	first, err := repo.CompleteSession(ctx, completeParams())
	if err != nil {
		t.Fatalf("first CompleteSession: %v", err)
	}

	// A DIFFERENT client key for the SAME shed-session: the natural key conflicts, so no new
	// completion, no new event -- the existing completion is returned, not-applied.
	second := completeParams()
	second.IdempotencyKey = "feed-complete-key-0002"
	res, err := repo.CompleteSession(ctx, second)
	if err != nil {
		t.Fatalf("second CompleteSession: %v", err)
	}
	if res.Applied {
		t.Fatal("second completion of the same shed-session Applied = true, want false")
	}
	if res.CompletionID != first.CompletionID {
		t.Fatalf("second completion id = %s, want existing %s", res.CompletionID, first.CompletionID)
	}
	assertOutboxCount(t, ctx, pool, first.CompletionID, 1)
}

func TestCompleteSessionSameKeyDifferentPayloadConflicts(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	if _, err := repo.CompleteSession(ctx, completeParams()); err != nil {
		t.Fatalf("first CompleteSession: %v", err)
	}

	// Same idempotency key, different effect (session 2): must be rejected, not silently applied.
	conflicting := completeParams()
	conflicting.SessionNo = 2
	_, err := repo.CompleteSession(ctx, conflicting)
	if !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key different-payload err = %v, want ErrIdempotencyConflict", err)
	}
}

func TestCompleteSessionRejectsShedOutsidePark(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	// Shed A belongs to fdPark, not fdOtherPark. Completing it against the wrong park must fail closed.
	wrongPark := completeParams()
	wrongPark.ParkID = fdOtherPark
	_, err := repo.CompleteSession(ctx, wrongPark)
	if !errors.Is(err, ports.ErrShedNotInPark) {
		t.Fatalf("shed-not-in-park err = %v, want ErrShedNotInPark", err)
	}
}

func assertRowCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, completionID string, want int) {
	t.Helper()
	var got int
	sql := "SELECT count(*) FROM " + table + " WHERE tenant_id = $1::uuid AND completion_id = $2::uuid"
	if err := pool.QueryRow(ctx, sql, fdTenant, completionID).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s rows = %d, want %d", table, got, want)
	}
}

func assertOutboxCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, completionID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'feed.direction.completed' AND aggregate_id = $2::uuid`,
		fdTenant, completionID).Scan(&got); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if got != want {
		t.Fatalf("outbox rows = %d, want %d", got, want)
	}
}
