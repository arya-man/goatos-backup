package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Adversarial coverage for the Herd Analytics aggregate. Every test here targets a way this
// read can report a WRONG NUMBER rather than fail loudly, which is the whole risk class the
// projection-review markers on the two queries exist to guard.

func newHerdAnalyticsRepo(t *testing.T, ctx context.Context) (*Repository, *pgxpool.Pool) {
	t.Helper()
	pool := setupCountsDB(t, ctx)
	return NewRepository(pool, 10*time.Second), pool
}

// monthsBack is the first day of the month n back, so a window always opens on a
// month boundary and the spine covers whole buckets.
func monthsBack(n int) string {
	now := time.Now().In(biztime.DefaultLocation())
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, biztime.DefaultLocation()).AddDate(0, -n, 0).Format("2006-01-02")
}

func thisMonthDay(day int) string {
	now := time.Now().In(biztime.DefaultLocation())
	return time.Date(now.Year(), now.Month(), day, 0, 0, 0, 0, biztime.DefaultLocation()).Format("2006-01-02")
}

// insertExitedGoat seeds one animal that has LEFT the herd, with the lifecycle/exit-reason pair
// under test. exitReason may be empty, which stores NULL and exercises the lifecycle fallback.
func insertExitedGoat(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	goatID, displayID, lifecycle, exitReason, exitedOn string,
) {
	t.Helper()
	var reason *string
	if exitReason != "" {
		reason = &exitReason
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status, age_band,
  custodian_party_id, park_id, shed_id, management_stage, exit_reason, exited_at
) VALUES (
  $1::uuid, $2::uuid, $3, 'goat', 'Beetal', 'female', $4, 'adult',
  '00000000-0000-4000-8000-000000001001'::uuid, $5::uuid, $6::uuid, 'K1', $7,
  ($8::date + time '10:00') AT TIME ZONE 'Asia/Kolkata'
)`, goatID, countsTenant, displayID, lifecycle, countsPark, countsShedA, reason, exitedOn); err != nil {
		t.Fatalf("seed exited goat %s: %v", displayID, err)
	}
}

// THE STATUS MATRIX. Every lifecycle_status x exit_reason pair the schema permits must land in
// EXACTLY ONE bucket -- deaths, sold or other_exits -- and every one must land in some bucket.
//
// This is the test the whole page's honesty rests on. net_change subtracts all three, so an exit
// double-counted across two buckets understates the herd and an exit that falls out of all three
// overstates it, and NEITHER fails loudly: the page still renders, with a wrong number on it.
// The mutual exclusion is not obvious from reading the SQL either, because it comes from the
// interaction of two clauses -- exit_reason wins when present, the lifecycle_status fallback
// applies only when exit_reason IS NULL -- so it is exactly the kind of rule that survives one
// refactor and dies on the next.
func TestHerdAnalyticsMultipleDimensionsEveryStatusBucketIsDisjointAndComplete(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)

	// The full matrix the goats CHECK constraints allow for an animal that has exited:
	// each of the five exit_reason values, plus each lifecycle status with a NULL reason
	// so the fallback branch is exercised on its own.
	matrix := []struct {
		lifecycle, exitReason string
		bucket                string
	}{
		{"dead", "died", "deaths"},
		{"dead", "", "deaths"},
		{"sold", "sold", "sold"},
		{"sold", "", "sold"},
		{"culled", "culled", "other"},
		{"culled", "", "other"},
		{"transferred", "transferred", "other"},
		{"transferred", "", "other"},
		{"lost", "lost", "other"},
		{"lost", "", "other"},
		// A reason that disagrees with the status must follow the REASON, not the status,
		// and must still be counted exactly once rather than in both.
		{"dead", "sold", "sold"},
	}
	want := map[string]int64{}
	for i, row := range matrix {
		insertExitedGoat(t, ctx, pool,
			fmt.Sprintf("00000000-0000-4000-8000-0000000091%02d", i),
			fmt.Sprintf("G-9100%02d", i),
			row.lifecycle, row.exitReason, thisMonthDay(2))
		want[row.bucket]++
	}

	got, err := repo.GetHerdAnalytics(ctx, domain.HerdAnalyticsQuery{TenantID: countsTenant, FromDate: monthsBack(2), ToDate: thisMonthDay(28)})
	if err != nil {
		t.Fatalf("GetHerdAnalytics: %v", err)
	}

	if got.Totals.Deaths != want["deaths"] {
		t.Fatalf("deaths=%d, want %d", got.Totals.Deaths, want["deaths"])
	}
	if got.Totals.Sold != want["sold"] {
		t.Fatalf("sold=%d, want %d", got.Totals.Sold, want["sold"])
	}
	if got.Totals.OtherExits != want["other"] {
		t.Fatalf("other_exits=%d, want %d", got.Totals.OtherExits, want["other"])
	}
	// Completeness AND disjointness in one assertion: the three buckets must sum to exactly the
	// number of exited animals seeded. A row counted twice overshoots; a row in no bucket
	// undershoots.
	if total := got.Totals.Deaths + got.Totals.Sold + got.Totals.OtherExits; total != int64(len(matrix)) {
		t.Fatalf("exit buckets sum to %d, want %d — an exit is either double-counted or dropped", total, len(matrix))
	}
	if got.Totals.NetChange != -int64(len(matrix)) {
		t.Fatalf("net_change=%d, want %d", got.Totals.NetChange, -len(matrix))
	}
}

// Kids + adults must PARTITION the live herd exactly. An animal with no recorded age band counts
// as an adult rather than falling out of both buckets -- the KPI tile shows the two side by side
// under one total, so a dropped animal reads as arithmetic that does not add up.
func TestHerdAnalyticsKidAndAdultBucketsPartitionTheLiveHerd(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)

	if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status, age_band,
  custodian_party_id, park_id, shed_id, management_stage
) VALUES
  ('00000000-0000-4000-8000-000000009201'::uuid, $1::uuid, 'G-920001', 'goat', 'Beetal', 'female', 'alive', 'kid',   '00000000-0000-4000-8000-000000001001'::uuid, $2::uuid, $3::uuid, 'K1'),
  ('00000000-0000-4000-8000-000000009202'::uuid, $1::uuid, 'G-920002', 'goat', 'Beetal', 'male',   'alive', 'adult', '00000000-0000-4000-8000-000000001001'::uuid, $2::uuid, $3::uuid, 'Mother'),
  ('00000000-0000-4000-8000-000000009203'::uuid, $1::uuid, 'G-920003', 'goat', 'Malai',  'male',   'alive', NULL,    '00000000-0000-4000-8000-000000001001'::uuid, $2::uuid, $3::uuid, NULL)`,
		countsTenant, countsPark, countsShedA); err != nil {
		t.Fatalf("seed live goats: %v", err)
	}

	got, err := repo.GetHerdAnalytics(ctx, domain.HerdAnalyticsQuery{TenantID: countsTenant, FromDate: monthsBack(2), ToDate: thisMonthDay(28)})
	if err != nil {
		t.Fatalf("GetHerdAnalytics: %v", err)
	}
	if got.Totals.LiveAnimals != 3 {
		t.Fatalf("live=%d, want 3", got.Totals.LiveAnimals)
	}
	if got.Totals.Kids+got.Totals.Adults != got.Totals.LiveAnimals {
		t.Fatalf("kids(%d)+adults(%d) != live(%d)", got.Totals.Kids, got.Totals.Adults, got.Totals.LiveAnimals)
	}

	// The composition series range over the same animal key set, so each must also sum to the
	// live total. A series that silently dropped its NULL bucket would pass a bar-count check
	// and fail this one.
	for name, series := range map[string][]domain.HerdAnalyticsSeriesPoint{
		"breed": got.Breed, "stage": got.Stage, "sex": got.Sex, "age_band": got.AgeBand, "park": got.Park,
	} {
		var sum int64
		for _, point := range series {
			sum += point.Count
		}
		if sum != got.Totals.LiveAnimals {
			t.Fatalf("%s series sums to %d, want %d", name, sum, got.Totals.LiveAnimals)
		}
	}
}

// A quiet month must be reported as a REAL ZERO, not omitted. A gap in a flow chart reads as
// "no data recorded", which is a different claim from "nothing happened", and the month spine is
// the only thing standing between the two.
func TestHerdAnalyticsPageBoundaryReportsAQuietMonthAsZeroRatherThanDroppingIt(t *testing.T) {
	ctx := context.Background()
	repo, _ := newHerdAnalyticsRepo(t, ctx)

	got, err := repo.GetHerdAnalytics(ctx, domain.HerdAnalyticsQuery{TenantID: countsTenant, FromDate: monthsBack(5), ToDate: thisMonthDay(28)})
	if err != nil {
		t.Fatalf("GetHerdAnalytics: %v", err)
	}
	if len(got.Months) != 6 {
		t.Fatalf("months=%d, want 6 — the window must report every month, activity or not", len(got.Months))
	}
	for _, month := range got.Months {
		if month.Month == "" || month.Label == "" {
			t.Fatalf("month row missing its key or label: %+v", month)
		}
	}
	if got.Months[len(got.Months)-1].Month != time.Now().In(biztime.DefaultLocation()).Format("2006-01") {
		t.Fatalf("last month=%q, want the month in progress", got.Months[len(got.Months)-1].Month)
	}
}
