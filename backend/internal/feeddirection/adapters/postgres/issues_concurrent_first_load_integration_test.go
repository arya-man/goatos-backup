package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Two first reads of preview/worklist race to freeze the same sheet. The loser's header INSERT hits
// the live/idempotency unique index (23505). That error aborts the surrounding transaction, so the
// old code's "re-read and fall through" ran lockIssue on an aborted tx and failed with 25P02 --
// surfacing as a 500 (or a retry) on the worklist's first load. The loser must instead see the
// winner's sheet: a replay when the fingerprints match.
//
// This forces the interleaving deterministically: a separate transaction inserts the winning
// header and holds it uncommitted, PersistIssue's INSERT blocks on the unique index behind it, and
// only then is the winner committed -- so PersistIssue gets exactly the 23505 path.
func TestPersistIssueLosingTheInsertRaceReplaysTheWinner(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	cmd := issueCmd(sampleCells(), "fp-1", issuedAt)

	winner, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = winner.Rollback(ctx) }()
	if _, err := insertIssueHeader(ctx, winner, cmd); err != nil {
		t.Fatalf("winner insert: %v", err)
	}

	type out struct {
		res ports.IssueResult
		err error
	}
	done := make(chan out, 1)
	go func() {
		res, err := repo.PersistIssue(ctx, cmd)
		done <- out{res, err}
	}()

	// Wait until PersistIssue is blocked on the winner's uncommitted unique-index entry.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock' AND query ILIKE '%INSERT INTO feed_direction_issues%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("PersistIssue never blocked behind the winner's insert")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := winner.Commit(ctx); err != nil {
		t.Fatalf("winner commit: %v", err)
	}

	got := <-done
	if got.err != nil {
		t.Fatalf("losing first load must not fail: %v", got.err)
	}
	if got.res.Outcome != ports.IssueOutcomeReplayed {
		t.Fatalf("losing first load outcome = %q, want replayed (the winner's sheet)", got.res.Outcome)
	}
}

// Many simultaneous first loads of the same sheet: every caller succeeds, exactly one inserts.
func TestPersistIssueConcurrentFirstLoadsAllSucceed(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	for round := 0; round < 5; round++ {
		cmd := issueCmd(sampleCells(), "fp-1", issuedAt)
		cmd.FeedDay = time.Date(2026, 8, 1+round, 0, 0, 0, 0, biztime.DefaultLocation()).Format("2006-01-02")
		cmd.IdempotencyKey = "issue:" + fdiTenant + ":" + fdiPark + ":" + cmd.FeedDay + ":normal"
		const n = 8
		var wg sync.WaitGroup
		results := make([]ports.IssueResult, n)
		errs := make([]error, n)
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				results[i], errs[i] = repo.PersistIssue(ctx, cmd)
			}(i)
		}
		close(start)
		wg.Wait()
		inserted := 0
		for i := 0; i < n; i++ {
			if errs[i] != nil {
				t.Fatalf("round %d caller %d: %v", round, i, errs[i])
			}
			if results[i].Outcome == ports.IssueOutcomeInserted {
				inserted++
			}
		}
		if inserted != 1 {
			t.Fatalf("round %d: %d callers inserted, want exactly 1", round, inserted)
		}
		var headers int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_direction_issues WHERE tenant_id = $1::uuid AND feed_day = $2::date`, fdiTenant, cmd.FeedDay).Scan(&headers); err != nil {
			t.Fatal(err)
		}
		if headers != 1 {
			t.Fatalf("round %d: %d headers, want 1", round, headers)
		}
	}
}
