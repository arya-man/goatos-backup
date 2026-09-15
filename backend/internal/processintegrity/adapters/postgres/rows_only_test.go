package postgres

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCanonicalMembershipCannotBeReplayedPerShed(t *testing.T) {
	// CPT's 125-shed plan replayed the obligation query 125 times before this
	// optimization fence (229,500 index probes / 11.48s for 11 board rows).
	for name, sql := range map[string]string{"rows": processIntegrityCanonicalRowsSQL, "counts": processIntegrityCanonicalCountsSQL, "adherence": processIntegrityCanonicalAdherenceSummarySQL} {
		if !strings.Contains(sql, "raw AS MATERIALIZED (") {
			t.Fatalf("%s lost the bounded obligation decoration planning fence", name)
		}
		if !strings.Contains(sql, "located AS MATERIALIZED (") {
			t.Fatalf("%s must evaluate canonical membership once before dimension joins", name)
		}
	}
}

func TestListRowsOnlyPreservesCanonicalPaginationWithoutPoisoningSummary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	q := domain.Query{TenantID: piTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Limit: 1}
	// The Work Board may count a complete canonical first page. Prove parity
	// against the independent live SQL aggregate, using the same vaccination scope.
	whole := q
	whole.Limit = 100
	category := "vaccination"
	whole.Category = &category
	whole.IncludeCompleted = true
	complete, err := repo.ListRowsOnly(ctx, whole)
	if err != nil {
		t.Fatal(err)
	}
	if complete.NextCursor != nil {
		t.Fatal("fixture must be a complete canonical page")
	}
	counts, err := repo.CountByWorkStateLive(ctx, whole)
	if err != nil {
		t.Fatal(err)
	}
	fromRows := map[domain.WorkState]int64{}
	for _, row := range complete.Rows {
		fromRows[row.WorkState]++
	}
	fromSQL := map[domain.WorkState]int64{}
	for _, count := range counts {
		fromSQL[count.WorkState] += count.Count
	}
	if len(complete.Rows) == 0 || !reflect.DeepEqual(fromRows, fromSQL) {
		t.Fatalf("complete canonical state parity rows=%v SQL=%v", fromRows, fromSQL)
	}
	total := 0
	for page := 0; page < 20; page++ {
		only, err := repo.ListRowsOnly(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		full, err := repo.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(only.Rows, full.Rows) || !reflect.DeepEqual(only.NextCursor, full.NextCursor) {
			t.Fatalf("page %d rows/cursor changed", page)
		}
		if len(only.CountsByWorkState) != 0 || only.TotalCount != 0 {
			t.Fatal("rows-only result incorrectly promises aggregate metadata")
		}
		if len(full.CountsByWorkState) == 0 || full.TotalCount == 0 {
			t.Fatal("rows-only read poisoned full summary cache")
		}
		total += len(only.Rows)
		if only.NextCursor == nil {
			if total < 2 {
				t.Fatalf("expected multiple row pages, got %d", total)
			}
			return
		}
		cursor, err := domain.DecodeCursor(*only.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		q.Cursor = &cursor
	}
	t.Fatal("cursor walk did not terminate")
}

func TestCanonicalBatchRepresentativeIsStableWhenTimestampsTie(t *testing.T) {
	for _, prefix := range []string{"located.task_id::text", "located.task_row_version"} {
		want := "ARRAY_AGG(" + prefix + " ORDER BY located.execution_due_at DESC NULLS LAST, located.due_at DESC NULLS LAST, located.task_id DESC NULLS LAST)"
		if !strings.Contains(processIntegrityCanonicalRowsSQL, want) {
			t.Fatalf("unstable task representative for %s", prefix)
		}
	}

	// IDs, states and proof refs must choose the same submission when batch imports
	// give many animals equal timestamps. Otherwise changing a valid query plan
	// changes which proof the same board row opens.
	for _, prefix := range []string{"located.submission_id::text", "located.submission_state", "located.proof_refs"} {
		want := "ARRAY_AGG(" + prefix + " ORDER BY located.submitted_at DESC NULLS LAST, located.submission_id DESC NULLS LAST)"
		if !strings.Contains(processIntegrityCanonicalRowsSQL, want) {
			t.Fatalf("unstable submission representative for %s", prefix)
		}
	}
	for _, prefix := range []string{"located.completion_id::text", "located.completion_status", "located.rejection_reason"} {
		want := "ARRAY_AGG(" + prefix + " ORDER BY located.completion_updated_at DESC NULLS LAST, located.completion_id DESC NULLS LAST)"
		if !strings.Contains(processIntegrityCanonicalRowsSQL, want) {
			t.Fatalf("unstable completion representative for %s", prefix)
		}
	}
}

func TestLiveCountsIgnoreCachedStateAfterCanonicalMutation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	category := "vaccination"
	q := domain.Query{TenantID: piTenant, Category: &category, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Limit: 10}
	before, err := repo.CountByWorkState(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("fixture must populate count cache")
	}
	execPI(t, ctx, pool, "cancel fixture work", `UPDATE obligation_instances SET status='canceled' WHERE tenant_id=$1`, piTenant)
	stale, err := repo.CountByWorkState(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stale, before) {
		t.Fatal("test did not retain a stale cache entry")
	}
	live, err := repo.CountByWorkStateLive(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Fatalf("live board aggregate retained canceled work: %v", live)
	}
}
