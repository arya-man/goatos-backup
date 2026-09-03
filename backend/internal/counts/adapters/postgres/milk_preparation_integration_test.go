package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func TestMilkPreparationUsesExactCohortGrainAndWholeScopeSummary(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	for i := 0; i < 2; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(60+i), goatDisplayID(60+i), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	}
	insertBreakdownGoat(t, ctx, pool, goatUUID(70), goatDisplayID(70), "female", "Beetal", "alive", "K2", strp(countsPark), strp(countsShedA), nil)
	for i := 0; i < 3; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(80+i), goatDisplayID(80+i), "male", "Beetal", "alive", "K3", strp(countsPark), strp(countsShedA), nil)
	}
	// This animal is in the same shed but outside the milk-preparation membership set.
	insertBreakdownGoat(t, ctx, pool, goatUUID(90), goatDisplayID(90), "male", "Beetal", "alive", "Adult", strp(countsPark), strp(countsShedA), nil)
	openK3Window(t, ctx, pool, "2026-07-30")

	asOf := time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC) // 2026-07-30 in India.
	got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{
		TenantID: countsTenant, ParkID: strp(countsPark), Limit: 1, AsOf: asOf,
	})
	if err != nil {
		t.Fatalf("GetMilkPreparation: %v", err)
	}
	if len(got.Items) != 1 || !got.HasMore {
		t.Fatalf("page items=%d has_more=%v, want one item and more", len(got.Items), got.HasMore)
	}
	if got.Summary.Scope != "filtered" || got.Summary.CohortCount != 3 || got.Summary.HeadCount != 6 || got.Summary.ShedCount != 1 {
		t.Fatalf("whole-scope summary=%+v", got.Summary)
	}
	if got.Summary.TotalRequiredML != 4000 || got.Summary.CitricAcidGrams != 22 {
		t.Fatalf("quantity summary=%+v, want 4000 ml and 22 g", got.Summary)
	}
	if len(got.FarmTasks) != 1 {
		t.Fatalf("farm tasks=%+v, want one whole-scope farm-day task", got.FarmTasks)
	}
	farmTask := got.FarmTasks[0]
	if farmTask.ParkID != countsPark || farmTask.CohortCount != 3 || farmTask.HeadCount != 6 {
		t.Fatalf("farm-day task grain=%+v", farmTask)
	}
	if farmTask.TotalRequiredML != 4000 || farmTask.CitricAcidGrams != 22 || farmTask.VerificationStatus != domain.MilkPreparationVerificationNotSubmitted {
		t.Fatalf("farm-day task direction/status=%+v", farmTask)
	}
	if got.PreparationDate != biztime.BusinessDate(asOf) || got.FeedingDate != "2026-07-31" {
		t.Fatalf("dates preparation=%s feeding=%s", got.PreparationDate, got.FeedingDate)
	}

	emptyPark := "00000000-0000-4000-8000-000000000099"
	empty, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{
		TenantID: countsTenant, ParkID: &emptyPark, Limit: 10, AsOf: asOf,
	})
	if err != nil {
		t.Fatalf("GetMilkPreparation empty park: %v", err)
	}
	if len(empty.Items) != 0 || len(empty.FarmTasks) != 0 || empty.HasMore || empty.Summary.HeadCount != 0 || empty.Summary.TotalRequiredML != 0 {
		t.Fatalf("empty park response=%+v", empty)
	}
}

// A kid housed in an ICU / quarantine shed still drinks milk, and it drinks the band it was on
// before it got sick. Its management_stage no longer carries that band -- the clinical tag
// overwrote it -- so the band is read from milk_cohort, recovered from stage history by 000166.
//
// The negative half is the maintainer's rule and is the more important assertion of the two: a
// clinically-housed kid whose band could NOT be recovered stays out of milk preparation entirely.
// It is never defaulted to a band, because a guessed band is a guessed milk volume for a sick
// animal. Those are resolved by hand, not by this query.
func TestMilkPreparationCountsClinicallyHousedKidsAtTheirRecoveredBand(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// Ordinary milk kid, answering from its own management_stage: 1 x K1 = 800 ml.
	insertBreakdownGoat(t, ctx, pool, goatUUID(60), goatDisplayID(60), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)

	// Two kids in the ICU shed. Both lost their band to the clinical tag; only the first one's band
	// was recoverable from history. The second must not be fed a guess.
	insertBreakdownGoat(t, ctx, pool, goatUUID(61), goatDisplayID(61), "female", "Beetal", "alive", "ICU-Kid", strp(countsPark), strp(countsShedB), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(62), goatDisplayID(62), "male", "Beetal", "alive", "ICU-Kid", strp(countsPark), strp(countsShedB), nil)
	setMilkCohort(t, ctx, pool, goatUUID(61), "K2")

	asOf := time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC) // 2026-07-30 in India.
	got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{
		TenantID: countsTenant, ParkID: strp(countsPark), Limit: 50, AsOf: asOf,
	})
	if err != nil {
		t.Fatalf("GetMilkPreparation: %v", err)
	}

	// Two heads, not three: the unrecoverable ICU kid is excluded, not counted at a default band.
	if got.Summary.HeadCount != 2 || got.Summary.ShedCount != 2 || got.Summary.CohortCount != 2 {
		t.Fatalf("summary=%+v, want 2 heads across 2 sheds in 2 cohorts (K1 + recovered K2)", got.Summary)
	}
	// 800 (K1) + 1200 (recovered K2). A K3 default for the excluded animal would read 2400.
	if got.Summary.TotalRequiredML != 2000 {
		t.Fatalf("total required ml=%d, want 2000 (800 K1 + 1200 recovered K2)", got.Summary.TotalRequiredML)
	}

	// The ICU shed appears as its own preparation row, at K2, priced as a K2 -- and holding ONE
	// head, so the excluded sibling standing in the same shed did not sneak in through the row.
	var icuRow *domain.MilkPreparationRow
	for i := range got.Items {
		if got.Items[i].ShedID == countsShedB {
			icuRow = &got.Items[i]
		}
	}
	if icuRow == nil {
		t.Fatalf("no milk preparation row for the ICU shed; items=%+v", got.Items)
	}
	if icuRow.ManagementStage != "K2" || icuRow.HeadCount != 1 || icuRow.DailyRequiredML != 1200 {
		t.Fatalf("ICU shed row=%+v, want stage K2, 1 head, 1200 ml", *icuRow)
	}
}

// K3 is a SEVEN DAY weaning window, not a standing cohort. An animal shifted into K3 draws milk
// for 7 days from its entry day and then stops, and an animal that was ALREADY in K3 when the rule
// landed carries no clock at all -- it has had its week, so it draws nothing.
//
// The boundaries are the point of this test. Day 7 is the last fed day and day 8 is dry, and both
// are asserted on the SAME animal by re-querying for a later preparation date, so an off-by-one in
// either direction fails: an exclusive upper bound loses day 7, and a `+ 7` feeds day 8.
func TestMilkPreparationK3IsASevenDayWindowFromEntry(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// Entered K3 on the preparation day itself: day 1 of 7.
	fresh := goatUUID(60)
	insertBreakdownGoat(t, ctx, pool, fresh, goatDisplayID(60), "female", "Beetal", "alive", "K3", strp(countsPark), strp(countsShedA), nil)
	setK3Start(t, ctx, pool, fresh, "2026-07-30")

	// Already in K3 before the rule landed: no clock, no milk, per "count as 0 days".
	stale := goatUUID(61)
	insertBreakdownGoat(t, ctx, pool, stale, goatDisplayID(61), "male", "Beetal", "alive", "K3", strp(countsPark), strp(countsShedA), nil)

	asOf := time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC) // 2026-07-30 in India.
	page := func(t *testing.T, at time.Time) domain.MilkPreparationSummary {
		t.Helper()
		got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{
			TenantID: countsTenant, ParkID: strp(countsPark), Limit: 50, AsOf: at,
		})
		if err != nil {
			t.Fatalf("GetMilkPreparation: %v", err)
		}
		return got.Summary
	}

	// Day 1: only the clocked animal. 400 ml, not 800 -- the clockless one is out.
	if s := page(t, asOf); s.HeadCount != 1 || s.TotalRequiredML != 400 {
		t.Fatalf("day 1 summary=%+v, want 1 head / 400 ml (the clockless K3 animal draws nothing)", s)
	}

	// Day 7 (2026-08-05) is the LAST fed day: entry day counts as day 1.
	if s := page(t, asOf.AddDate(0, 0, 6)); s.HeadCount != 1 || s.TotalRequiredML != 400 {
		t.Fatalf("day 7 summary=%+v, want the animal still fed on its seventh and final day", s)
	}

	// Day 8 (2026-08-06): dry.
	if s := page(t, asOf.AddDate(0, 0, 7)); s.HeadCount != 0 || s.TotalRequiredML != 0 {
		t.Fatalf("day 8 summary=%+v, want no milk -- the seven day window is over", s)
	}

	// A K1 animal in the same shed is unaffected by any of this: only K3 is windowed.
	insertBreakdownGoat(t, ctx, pool, goatUUID(62), goatDisplayID(62), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	if s := page(t, asOf.AddDate(0, 0, 7)); s.HeadCount != 1 || s.TotalRequiredML != 800 {
		t.Fatalf("day 8 with a K1 present=%+v, want the K1 fed at 800 ml and only the K3 dropped", s)
	}
}

// openK3Window puts every seeded K3 animal inside its seven-day weaning window (000168) by
// starting its clock on the given preparation date.
//
// Suites that are about something else -- cohort grain, partition rollup, pagination, status
// buckets -- seed K3 animals as ordinary members of the milk set. Since 000168 a K3 animal with no
// clock draws no milk, so without this they would silently test an empty K3 cohort and stop proving
// what they are named for. The window itself is proved by
// TestMilkPreparationK3IsASevenDayWindowFromEntry, which is the only place that leaves a clock off
// on purpose.
func openK3Window(t *testing.T, ctx context.Context, pool *pgxpool.Pool, preparationDate string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE goats SET k3_milk_started_on = $2::date
WHERE tenant_id = $1::uuid AND management_stage = 'K3'`, countsTenant, preparationDate); err != nil {
		t.Fatalf("open K3 window: %v", err)
	}
}

func setK3Start(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, startedOn string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`UPDATE goats SET k3_milk_started_on = $3::date WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		countsTenant, goatID, startedOn); err != nil {
		t.Fatalf("set k3_milk_started_on: %v", err)
	}
}

func setMilkCohort(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, cohort string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`UPDATE goats SET milk_cohort = $3 WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		countsTenant, goatID, cohort); err != nil {
		t.Fatalf("set milk_cohort: %v", err)
	}
}

func TestMilkPreparationVerificationStateIsOneTaskPerFarm(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	insertBreakdownGoat(t, ctx, pool, goatUUID(160), goatDisplayID(160), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(161), goatDisplayID(161), "female", "Beetal", "alive", "K2", strp(countsPark), strp(countsShedB), nil)

	if _, err := pool.Exec(ctx, `INSERT INTO milk_preparation_completions
(tenant_id, park_id, shed_id, preparation_date, feeding_date, submitted_by)
VALUES ($1::uuid,$2::uuid,NULL,'2026-07-30','2026-07-31','90000000-0000-4000-8000-000000000101')`,
		countsTenant, countsPark); err != nil {
		t.Fatalf("insert farm completion: %v", err)
	}

	got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{
		TenantID: countsTenant, ParkID: strp(countsPark), Limit: 10,
		AsOf: time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("GetMilkPreparation: %v", err)
	}
	if len(got.FarmTasks) != 1 {
		t.Fatalf("farm tasks=%+v", got.FarmTasks)
	}
	if got.FarmTasks[0].VerificationStatus != domain.MilkPreparationVerificationPending {
		t.Fatalf("farm status=%+v", got.FarmTasks[0])
	}
	if got.Summary.PendingVerificationFarmCount != 1 || got.Summary.NotSubmittedFarmCount != 0 || got.Summary.ParkCount != 1 {
		t.Fatalf("farm summary buckets=%+v", got.Summary)
	}
}

// insertMilkPrepPartition assigns a goat to a partition of its shed. The row grain the
// milk-preparation page now carries (physical shed x management stage x partition) must never
// change the pre-partition whole-scope truth, so every partition test in this file compares a
// partitioned scenario against its un-partitioned parent-shed baseline.
func insertMilkPrepPartition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, shedID, label string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $4)`,
		countsTenant, goatID, shedID, label); err != nil {
		t.Fatalf("seed goat_shed_partitions: %v", err)
	}
}

// TestMilkPreparationPartitionRowsSumExactlyToParentShedTotal is the sum-to-parent proof required
// before the shed x stage grain could be widened to shed x stage x partition: seed the exact same
// K1/K2/K3 goats twice -- once entirely un-partitioned, once split across two partitions of a
// second shed -- and assert both the row set and every whole-scope number (summary, farm_tasks,
// milk_direction) agree between the two, even though the partitioned shed now produces multiple
// page rows.
func TestMilkPreparationPartitionRowsSumExactlyToParentShedTotal(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// Baseline: 5 K1 goats in shed A, entirely un-partitioned.
	for i := 0; i < 5; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(200+i), goatDisplayID(200+i), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	}
	asOf := time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC)
	baseline, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{TenantID: countsTenant, ParkID: strp(countsPark), Limit: 50, AsOf: asOf})
	if err != nil {
		t.Fatalf("GetMilkPreparation baseline: %v", err)
	}
	if len(baseline.Items) != 1 || baseline.Items[0].HeadCount != 5 || baseline.Items[0].PartitionLabel != "" {
		t.Fatalf("baseline items=%+v, want one un-partitioned 5-head row", baseline.Items)
	}
	if baseline.Items[0].OperationalLocationDisplay != baseline.Items[0].ShedLabel {
		t.Fatalf("baseline operational_location_display=%q, want bare shed label %q", baseline.Items[0].OperationalLocationDisplay, baseline.Items[0].ShedLabel)
	}

	// Same 5 goats worth of K1 headcount, in shed B, split 3/2 across two partitions.
	for i := 0; i < 3; i++ {
		id := goatUUID(210 + i)
		insertBreakdownGoat(t, ctx, pool, id, goatDisplayID(210+i), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedB), nil)
		insertMilkPrepPartition(t, ctx, pool, id, countsShedB, "1")
	}
	for i := 0; i < 2; i++ {
		id := goatUUID(220 + i)
		insertBreakdownGoat(t, ctx, pool, id, goatDisplayID(220+i), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedB), nil)
		insertMilkPrepPartition(t, ctx, pool, id, countsShedB, "Part 2")
	}

	got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{TenantID: countsTenant, ParkID: strp(countsPark), Limit: 50, AsOf: asOf})
	if err != nil {
		t.Fatalf("GetMilkPreparation: %v", err)
	}

	// Row grain: shed B produces TWO rows (one per partition), each carrying its own
	// operational_location_display -- this is the whole point of the grain change.
	shedBRows := make([]domain.MilkPreparationRow, 0, 2)
	for _, row := range got.Items {
		if row.ShedID == countsShedB {
			shedBRows = append(shedBRows, row)
		}
	}
	if len(shedBRows) != 2 {
		t.Fatalf("shed B rows=%+v, want 2 partition rows", shedBRows)
	}
	var shedBHeadSum int
	labels := map[string]bool{}
	for _, row := range shedBRows {
		shedBHeadSum += row.HeadCount
		labels[row.OperationalLocationDisplay] = true
		if row.PartitionLabel == "" {
			t.Fatalf("shed B row=%+v, want a real partition label", row)
		}
	}
	// SUM-TO-PARENT PROOF: the two partition rows' head counts sum to exactly the 5 heads the
	// pre-partition (shed-only) grain would have reported for the same goats.
	if shedBHeadSum != 5 {
		t.Fatalf("shed B partition rows sum to %d heads, want 5 (sum-to-parent violated)", shedBHeadSum)
	}
	if len(labels) != 2 {
		t.Fatalf("shed B operational_location_display values=%v, want 2 visibly distinct labels", labels)
	}

	// WHOLE-SCOPE PROOF: summary, farm_tasks, and milk_direction are unaffected by the partition
	// split -- they still read the pre-partition (shed, stage) grain via grouped_by_shed.
	if got.Summary.ShedCount != 2 || got.Summary.HeadCount != baseline.Summary.HeadCount+5 {
		t.Fatalf("summary=%+v, want unaffected whole-scope shed_count/head_count", got.Summary)
	}
	// cohort_count is (shed, stage) grain in the summary, so it is 2 (shed A K1 + shed B K1), not 3.
	if got.Summary.CohortCount != 2 {
		t.Fatalf("summary.cohort_count=%d, want 2 (partition split must not inflate the shed-grain cohort count)", got.Summary.CohortCount)
	}
	if len(got.FarmTasks) != 1 || got.FarmTasks[0].HeadCount != 10 {
		t.Fatalf("farm tasks=%+v, want one farm task with 10 total heads", got.FarmTasks)
	}
}

// TestMilkPreparationNonPartitionedShedRendersBareLabelAsOneRow proves the display-vs-storage
// contract for the common case: a shed with zero goat_shed_partitions rows is one row, and its
// operational_location_display is the bare shed name -- never "<shed> whole".
func TestMilkPreparationNonPartitionedShedRendersBareLabelAsOneRow(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	for i := 0; i < 4; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(230+i), goatDisplayID(230+i), "female", "Beetal", "alive", "K2", strp(countsPark), strp(countsShedA), nil)
	}
	got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{
		TenantID: countsTenant, ParkID: strp(countsPark), Limit: 50,
		AsOf: time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("GetMilkPreparation: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items=%+v, want exactly one non-partitioned row", got.Items)
	}
	row := got.Items[0]
	if row.PartitionLabel != "" {
		t.Fatalf("row.PartitionLabel=%q, want empty for a non-partitioned shed", row.PartitionLabel)
	}
	if row.OperationalLocationDisplay != row.ShedLabel {
		t.Fatalf("row.OperationalLocationDisplay=%q, want bare shed label %q, never a %q suffix", row.OperationalLocationDisplay, row.ShedLabel, "whole")
	}
	if strings.Contains(strings.ToLower(row.OperationalLocationDisplay), "whole") {
		t.Fatalf("row.OperationalLocationDisplay=%q leaked the matching sentinel", row.OperationalLocationDisplay)
	}
}

// TestMilkPreparationPaginationIsStableAcrossPartitionedPageBoundary proves the partition
// dimension is part of the pagination ordering key: with several partitions on one shed, walking
// the page with a small limit must visit every row exactly once with no duplicate and no gap
// across the page boundary.
func TestMilkPreparationPaginationIsStableAcrossPartitionedPageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	partitions := []string{"1", "2", "3", "4"}
	for i, label := range partitions {
		id := goatUUID(240 + i)
		insertBreakdownGoat(t, ctx, pool, id, goatDisplayID(240+i), "male", "Beetal", "alive", "K3", strp(countsPark), strp(countsShedA), nil)
		insertMilkPrepPartition(t, ctx, pool, id, countsShedA, label)
	}
	openK3Window(t, ctx, pool, "2026-07-30")
	asOf := time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC)

	seen := map[string]bool{}
	var all []domain.MilkPreparationRow
	offset := int32(0)
	for {
		page, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{
			TenantID: countsTenant, ParkID: strp(countsPark), Limit: 2, Offset: offset, AsOf: asOf,
		})
		if err != nil {
			t.Fatalf("GetMilkPreparation offset=%d: %v", offset, err)
		}
		if len(page.Items) == 0 {
			break
		}
		for _, row := range page.Items {
			key := row.ShedID + "|" + row.PartitionLabel + "|" + row.ManagementStage
			if seen[key] {
				t.Fatalf("row %s seen twice across the page boundary (unstable pagination)", key)
			}
			seen[key] = true
			all = append(all, row)
		}
		if !page.HasMore {
			break
		}
		offset += int32(len(page.Items))
		if offset > 20 {
			t.Fatalf("pagination did not terminate: offset=%d rows=%d", offset, len(all))
		}
	}
	if len(all) != len(partitions) {
		t.Fatalf("walked %d rows across pages, want exactly %d (one per partition, no dup/gap)", len(all), len(partitions))
	}
	var headSum int
	for _, row := range all {
		headSum += row.HeadCount
	}
	if headSum != len(partitions) {
		t.Fatalf("paginated rows sum to %d heads, want %d", headSum, len(partitions))
	}
}

// The four tests below close the aggregate/projection review lens for milk prep's NEW grain.
// Partition was just added as a dimension here, so each one asks the same question from a
// different angle: can the added dimension make a whole-scope number drift?

// TestMilkPreparationPartitionCardinalityOneToMany: goat_shed_partitions is 1:{0,1} per animal
// (PK (tenant_id, goat_id)), so fanning one shed across many pens must SPLIT its head count, never
// MULTIPLY it. A many-side join here would inflate every farm's required milk volume.
func TestMilkPreparationPartitionCardinalityOneToMany(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// One shed, one cohort, eight pens: the classic one-to-many shape.
	labels := []string{"1", "2", "3", "4", "5", "6", "7", "8"}
	for i, label := range labels {
		insertBreakdownGoat(t, ctx, pool, goatUUID(600+i), goatDisplayID(600+i), "male", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
		insertMilkPrepPartition(t, ctx, pool, goatUUID(600+i), countsShedA, label)
	}
	asOf := time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC)
	got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{TenantID: countsTenant, ParkID: strp(countsPark), Limit: 50, AsOf: asOf})
	if err != nil {
		t.Fatalf("GetMilkPreparation: %v", err)
	}
	var summed int
	for _, item := range got.Items {
		summed += item.HeadCount
	}
	if summed != len(labels) {
		t.Fatalf("partition rows sum to %d head, want %d -- the partition join must not multiply animals", summed, len(labels))
	}
	if got.Summary.HeadCount != len(labels) {
		t.Fatalf("whole-scope summary head_count=%d, want %d -- the summary must stay a rollup of the same animals", got.Summary.HeadCount, len(labels))
	}
}

// TestMilkPreparationPartitionParkScopeHierarchy: shed NAMES repeat across parks (two "Castro").
// The grain must key on shed_id + park, so one park's pen can never absorb another's.
func TestMilkPreparationPartitionParkScopeHierarchy(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	insertBreakdownGoat(t, ctx, pool, goatUUID(620), goatDisplayID(620), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	insertMilkPrepPartition(t, ctx, pool, goatUUID(620), countsShedA, "1")
	insertBreakdownGoat(t, ctx, pool, goatUUID(621), goatDisplayID(621), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedB), nil)
	insertMilkPrepPartition(t, ctx, pool, goatUUID(621), countsShedB, "1")

	asOf := time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC)
	got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{TenantID: countsTenant, ParkID: strp(countsPark), Limit: 50, AsOf: asOf})
	if err != nil {
		t.Fatalf("GetMilkPreparation: %v", err)
	}
	// Same partition LABEL ("1") in two different sheds must stay two rows.
	if len(got.Items) != 2 {
		t.Fatalf("got %d rows, want 2 -- partition '1' in two sheds must not collapse into one", len(got.Items))
	}
	for _, item := range got.Items {
		if item.HeadCount != 1 {
			t.Fatalf("row %+v head_count=%d, want 1", item, item.HeadCount)
		}
	}
}

// TestMilkPreparationPartitionEveryStatusBuckets: the per-cohort split (K1/K2/K3) is a strict
// partition of the head count. Adding the pen dimension must not let a cohort absorb or drop an
// animal -- the milk volumes are computed per cohort, so drift here mis-orders real milk.
func TestMilkPreparationPartitionEveryStatusBuckets(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	stages := []string{"K1", "K2", "K3"}
	for i, stage := range stages {
		insertBreakdownGoat(t, ctx, pool, goatUUID(640+i), goatDisplayID(640+i), "male", "Beetal", "alive", stage, strp(countsPark), strp(countsShedA), nil)
		insertMilkPrepPartition(t, ctx, pool, goatUUID(640+i), countsShedA, "2")
	}
	openK3Window(t, ctx, pool, "2026-07-30")
	asOf := time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC)
	got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{TenantID: countsTenant, ParkID: strp(countsPark), Limit: 50, AsOf: asOf})
	if err != nil {
		t.Fatalf("GetMilkPreparation: %v", err)
	}
	seen := map[string]int{}
	for _, item := range got.Items {
		seen[item.ManagementStage] += item.HeadCount
	}
	for _, stage := range stages {
		if seen[stage] != 1 {
			t.Fatalf("stage %s head_count=%d, want 1 (buckets: %+v)", stage, seen[stage], seen)
		}
	}
	if got.Summary.HeadCount != len(stages) {
		t.Fatalf("summary head_count=%d, want %d", got.Summary.HeadCount, len(stages))
	}
}

// TestMilkPreparationPartitionExecutionDateIndependence: partition is a LOCATION dimension. The
// as-of business date selects WHICH day's verification state is read; it must not change how
// animals group by pen, and a different as-of must not silently drop or merge partition rows.
func TestMilkPreparationPartitionExecutionDateIndependence(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	for i, label := range []string{"1", "2"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(660+i), goatDisplayID(660+i), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
		insertMilkPrepPartition(t, ctx, pool, goatUUID(660+i), countsShedA, label)
	}
	first := time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC)
	second := time.Date(2026, 8, 3, 20, 30, 0, 0, time.UTC)

	for _, asOf := range []time.Time{first, second} {
		got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{TenantID: countsTenant, ParkID: strp(countsPark), Limit: 50, AsOf: asOf})
		if err != nil {
			t.Fatalf("GetMilkPreparation(%s): %v", asOf.Format("2006-01-02"), err)
		}
		if len(got.Items) != 2 || got.Summary.HeadCount != 2 {
			t.Fatalf("as_of=%s items=%d summary=%d, want 2 partition rows totalling 2 regardless of date",
				asOf.Format("2006-01-02"), len(got.Items), got.Summary.HeadCount)
		}
	}
}

// TestMilkPreparationUHTConsumptionFollowsTheAcceptedAttempt drives the production submit ->
// rework -> re-submit -> approve path and pins what the feed stock actually depletes, which since
// migration 000216 is the preparation ITSELF, read through
// feed_effective_external_consumption -- not a ledger row some consumer copies over afterwards.
//
// Four properties, each a real failure mode:
//   - SUBMIT is the trigger: a pending_verification preparation already depletes (holding stock
//     behind review made every card lag the verification queue).
//   - The CURRENT attempt's answer wins: a bounced attempt's litres must never resurface.
//   - The APPROVE changes nothing. The retired 2026-08-22 recorder seam wrote a second
//     feed_external_consumption row here, keyed at preparation_date while this view books the same
//     milk at feeding_date, so the litres were deducted twice.
//   - The milk is booked on the FEEDING date, one row, never two.
func TestMilkPreparationUHTConsumptionFollowsTheAcceptedAttempt(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	submit := func(idem string, litres float64) domain.MilkPreparationSubmissionResult {
		t.Helper()
		res, err := repo.SubmitMilkPreparation(ctx, domain.MilkPreparationSubmission{
			TenantID: countsTenant, ParkID: countsPark,
			PreparationDate: time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC),
			FeedingDate:     time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC),
			GoatMilkUsed:    false,
			Answers: domain.MilkPreparationAnswers{
				MorningMilkCollectedLitres: 1, EveningMilkCollectedLitres: 1,
				UHTMilkQuantityLitres: litres, CitricAcidGrams: 120,
			},
			Proofs: domain.MilkPreparationProofs{
				UHTMilkQuantityProofRef: "proof-uht-" + idem, CitricAcidMixingProofRef: "proof-citric-" + idem,
			},
			SubmittedBy: "90000000-0000-4000-8000-000000000101", SubmittedAt: time.Now().In(biztime.DefaultLocation()),
			IdempotencyKey: "milk-prep:" + idem, TraceID: "trace-" + idem,
		})
		if err != nil {
			t.Fatalf("submit %s: %v", idem, err)
		}
		return res
	}

	// depleted returns every (day, kg) row the feed stock read would see for this park's UHT.
	depleted := func(stage string) []string {
		t.Helper()
		rows, err := pool.Query(ctx, `
SELECT feed_day::text || '=' || quantity_kg::text
FROM feed_effective_external_consumption
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
ORDER BY feed_day`, countsTenant, countsPark)
		if err != nil {
			t.Fatalf("%s: read effective consumption: %v", stage, err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Fatalf("%s: scan: %v", stage, err)
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("%s: rows: %v", stage, err)
		}
		return out
	}
	want := func(stage string, expected ...string) {
		t.Helper()
		got := depleted(stage)
		if len(got) != len(expected) {
			t.Fatalf("%s: consumption=%v, want %v", stage, got, expected)
		}
		for i := range expected {
			if got[i] != expected[i] {
				t.Fatalf("%s: consumption=%v, want %v", stage, got, expected)
			}
		}
	}

	first := submit("a1", 28)
	// Submitted, not yet reviewed: the milk is already gone from the store, on the FEEDING date.
	want("pending", "2026-08-23=28.000")

	// Verifier bounces the first attempt. The proof gets re-shot; the milk was still drunk, so the
	// preparation keeps depleting its current answer.
	if applied, err := repo.BounceMilkPreparationForRework(ctx, domain.MilkPreparationVerdictCommand{
		TenantID: countsTenant, CompletionID: first.CompletionID,
		VerifiedBy: "90000000-0000-4000-8000-000000000101", Reason: "blurry",
		OccurredAt: time.Now().In(biztime.DefaultLocation()), TraceID: "verdict-1",
	}); err != nil || !applied {
		t.Fatalf("rework: applied=%v err=%v", applied, err)
	}
	want("rework", "2026-08-23=28.000")

	// The re-shoot carries CORRECTED litres; the bounced attempt's 28 must not resurface.
	second := submit("a2", 30)
	if second.CompletionID != first.CompletionID || second.AttemptNo != 2 {
		t.Fatalf("second attempt must reuse the farm-day completion: %+v", second)
	}
	want("second attempt", "2026-08-23=30.000")

	if _, err := repo.ApplyVerifiedMilkPreparation(ctx, domain.MilkPreparationVerdictCommand{
		TenantID: countsTenant, CompletionID: first.CompletionID,
		VerifiedBy: "90000000-0000-4000-8000-000000000101", OccurredAt: time.Now().In(biztime.DefaultLocation()), TraceID: "verdict-2",
	}); err != nil {
		t.Fatalf("approve second attempt: %v", err)
	}
	// THE APPROVE MUST CHANGE NOTHING. One row, same day, same litres -- and no ledger row born
	// out of the verdict, which is the 2026-08-22 seam this test exists to keep retired.
	want("approved", "2026-08-23=30.000")
	var ledgerRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_external_consumption WHERE tenant_id = $1::uuid`, countsTenant).
		Scan(&ledgerRows); err != nil {
		t.Fatalf("count ledger rows: %v", err)
	}
	if ledgerRows != 0 {
		t.Fatalf("approving a preparation wrote %d feed_external_consumption row(s); the workflow owns the fact "+
			"and a second writer can only double-deduct it", ledgerRows)
	}

	// A stale rework verdict on the now-completed row is an at-least-once no-op.
	if applied, err := repo.BounceMilkPreparationForRework(ctx, domain.MilkPreparationVerdictCommand{
		TenantID: countsTenant, CompletionID: first.CompletionID,
		VerifiedBy: "90000000-0000-4000-8000-000000000101", Reason: "late duplicate",
		OccurredAt: time.Now().In(biztime.DefaultLocation()), TraceID: "verdict-3",
	}); err != nil || applied {
		t.Fatalf("stale rework must be a no-op: applied=%v err=%v", applied, err)
	}
	want("stale rework", "2026-08-23=30.000")
}
