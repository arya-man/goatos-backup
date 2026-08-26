package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// TestReportCountsLoadsNotRounds is the report's load-bearing assertion, and it is written
// against a real Postgres because the defect it guards is a SQL-grain defect that no
// in-memory fake can reproduce.
//
// One delivery is tested, comes back Invalid, is retested and cleared. That is ONE load
// and THREE task rows. A report keyed on tasks reads three deliveries and two outstanding
// problems; the farm received one bag of feed and it is fine. The DISTINCT ON collapse is
// what makes the page tell the truth, so this drives the whole read through the real query
// and asserts the load count, the chip counts and the row's state.
func TestReportCountsLoadsNotRounds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 10*time.Second)
	base := time.Now()

	// Load A: tested, void strip, retested, accepted negative -> ONE cleared load.
	if err := repo.CreateTaskFromPurchase(ctx, toxinCreateParams(toxinTestPurchase)); err != nil {
		t.Fatalf("create: %v", err)
	}
	page, err := repo.ListTasks(ctx, ports.ListTasksParams{TenantID: toxinTestTenant, Limit: 5})
	if err != nil || len(page.Rows) == 0 {
		t.Fatalf("list after create: %v rows=%d", err, len(page.Rows))
	}
	round1 := page.Rows[0].Task.TaskID

	runToxinSteps(t, ctx, repo, round1, "r1", base)
	if _, err := repo.SubmitReading(ctx, ports.SubmitParams{
		TenantID: toxinTestTenant, TaskID: round1, Outcome: domain.OutcomeInvalid,
		StripPhotoRef: "r1-strip", IdempotencyKey: "r1-submit", Now: base.Add(80 * time.Minute),
	}); err != nil {
		t.Fatalf("submit invalid: %v", err)
	}

	// The Invalid strip minted round 2 in the same transaction. Find it and clear it.
	page, err = repo.ListTasks(ctx, ports.ListTasksParams{
		TenantID: toxinTestTenant, Statuses: []string{domain.StatusInProgress}, Limit: 5,
	})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("expected exactly one live round after the void strip, got %d (%v)", len(page.Rows), err)
	}
	round2 := page.Rows[0].Task.TaskID
	if round2 == round1 {
		t.Fatal("the void strip did not mint a fresh round")
	}
	runToxinSteps(t, ctx, repo, round2, "r2", base)
	if _, err := repo.SubmitReading(ctx, ports.SubmitParams{
		TenantID: toxinTestTenant, TaskID: round2, Outcome: domain.OutcomeNegative,
		StripPhotoRef: "r2-strip", IdempotencyKey: "r2-submit", Now: base.Add(80 * time.Minute),
	}); err != nil {
		t.Fatalf("submit negative: %v", err)
	}
	if _, err := repo.RecordVerdict(ctx, ports.VerdictParams{
		TenantID: toxinTestTenant, TaskID: round2, Decision: domain.VerdictAccept,
		ActorID: toxinTestTenant, IdempotencyKey: "r2-verdict", RowVersion: 0,
	}); err != nil {
		t.Logf("accept (version-fenced, retrying unfenced): %v", err)
		row, gerr := repo.GetTask(ctx, toxinTestTenant, round2)
		if gerr != nil {
			t.Fatalf("get for version: %v", gerr)
		}
		if _, err := repo.RecordVerdict(ctx, ports.VerdictParams{
			TenantID: toxinTestTenant, TaskID: round2, Decision: domain.VerdictAccept,
			ActorID: toxinTestTenant, IdempotencyKey: "r2-verdict-2",
			RowVersion: row.Task.RowVersion,
		}); err != nil {
			t.Fatalf("accept: %v", err)
		}
	}

	// Load B: recorded, never tested -> ONE waiting load.
	second := toxinCreateParams("10000000-0000-4000-8000-00000000000b")
	second.BatchNo = 2
	if err := repo.CreateTaskFromPurchase(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}

	report, err := repo.LoadReport(ctx, ports.ReportParams{TenantID: toxinTestTenant, Limit: 20, WindowDays: 3650})
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	// THE ASSERTION: two deliveries, not the three task rows behind them.
	if len(report.Loads) != 2 {
		t.Fatalf("report listed %d rows; two loads were delivered (three rounds exist)", len(report.Loads))
	}
	if got := report.FilterCounts[domain.ReportFilterAll]; got != 2 {
		t.Fatalf("All chip = %d, want 2 loads", got)
	}
	if got := report.FilterCounts[domain.ReportFilterCleared]; got != 1 {
		t.Fatalf("Cleared chip = %d, want 1", got)
	}
	if got := report.FilterCounts[domain.ReportFilterWaiting]; got != 1 {
		t.Fatalf("Waiting chip = %d, want 1", got)
	}
	if got := report.FilterCounts[domain.ReportFilterFlagged]; got != 0 {
		t.Fatalf("Flagged chip = %d; the void strip was superseded by a clean retest", got)
	}
	if report.Summary.LoadsReceived != 2 {
		t.Fatalf("summary received = %d, want 2", report.Summary.LoadsReceived)
	}
	// The cleared load must read from its LATEST round: negative, not the void first strip.
	for _, l := range report.Loads {
		if l.FeedPurchaseID != toxinTestPurchase {
			continue
		}
		if l.RoundNo != 2 || l.Outcome != domain.OutcomeNegative {
			t.Fatalf("cleared load shows round %d outcome %q; the report must read the latest round", l.RoundNo, l.Outcome)
		}
	}
}

// TestReportBucketSQLMatchesTheDomainPartition drives every status/outcome pair through
// BOTH the SQL CASE and domain.ReportBucketFor and fails on the first disagreement. The
// two are separate implementations of one rule, and a SQL chip that disagrees with the
// domain badges a count its own page cannot list.
func TestReportBucketSQLMatchesTheDomainPartition(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	statuses := []string{domain.StatusInProgress, domain.StatusPendingReview, domain.StatusAccepted, domain.StatusCancelled}
	outcomes := []string{"", domain.OutcomeNegative, domain.OutcomePositive, domain.OutcomeInvalid}
	query := "SELECT " + reportBucketSQL("latest") + " FROM (SELECT $1::text AS status, $2::text AS outcome) latest"

	for _, status := range statuses {
		for _, outcome := range outcomes {
			var got string
			if err := pool.QueryRow(ctx, query, status, outcome).Scan(&got); err != nil {
				t.Fatalf("bucket sql (%s/%s): %v", status, outcome, err)
			}
			if want := domain.ReportBucketFor(status, outcome); got != want {
				t.Fatalf("status %q outcome %q: SQL says %q, domain says %q", status, outcome, got, want)
			}
		}
	}
}
