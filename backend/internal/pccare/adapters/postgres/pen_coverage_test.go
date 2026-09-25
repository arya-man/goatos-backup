package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// The Care Coverage board against the REAL schema (maintainer request 2026-09-25): pens down the
// left, the five hands-on-the-animal categories across the top, a tick where the work is done.
// These are the adversarial cases the aggregate/projection rule demands of a read that joins
// tasks onto a pen catalog and groups them: one-to-many fan-out, page boundaries, park scope, and
// the whole status matrix. Each is a way the board could silently tick the wrong pen.

const (
	covShedGodel = "9c000000-0000-4000-8000-000000004301"
	covShedOther = "9c000000-0000-4000-8000-000000004302"
)

// seedCoverageGodel adds a partitioned shed "Godel 1" with pens "Part 1" and "Part 2" to pcPark.
func seedCoverageGodel(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($2::uuid, $1::uuid, 'shed', 'S-G', 'Godel 1', 'active', $3::uuid, 2) ON CONFLICT (location_id) DO NOTHING`,
		pcTenant, covShedGodel, pcPark); err != nil {
		t.Fatalf("seed godel shed: %v", err)
	}
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual'), ($1::uuid, $2::uuid, 'Part 2', '2', 'active', 'manual')
ON CONFLICT DO NOTHING`, pcTenant, covShedGodel); err != nil {
		t.Fatalf("seed godel partitions: %v", err)
	}
}

// coverageTask plans one task, then moves it to the given work_state/status and submit day.
func coverageTask(t *testing.T, ctx context.Context, repo *Repository, parkID, shedID, partition, category string, plannedDay int, workState, status string, submittedDay int) {
	t.Helper()
	key := fmt.Sprintf("cov-%s-%s-%s-%d", shedID[len(shedID)-4:], partition, category, plannedDay)
	task, err := repo.CreateTask(ctx, ports.CreateTaskParams{
		TenantID: pcTenant, Category: category, ParkID: parkID, ShedID: shedID, PartitionLabel: partition,
		PlannedBusinessDate: pcBusinessDay(2026, 9, plannedDay),
		AssigneeUserIDs:     assigneesFor(parkID),
		IdempotencyKey:      key, CreatedBy: pcVerifier, ActorID: pcVerifier,
	})
	if err != nil {
		t.Fatalf("CreateTask %s: %v", key, err)
	}
	var submitted any
	if submittedDay > 0 {
		submitted = time.Date(2026, 9, submittedDay, 10, 0, 0, 0, time.FixedZone("IST", 5*3600+1800))
	}
	if _, err := repo.pool.Exec(ctx, `
UPDATE pc_care_tasks SET work_state = $3, status = $4, submitted_at = $5
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, pcTenant, task.TaskID, workState, status, submitted); err != nil {
		t.Fatalf("move task %s: %v", key, err)
	}
}

func assigneesFor(parkID string) []string {
	if parkID == pcOtherPark {
		return []string{pcOtherParkOperator}
	}
	return []string{pcOperator1, pcOperator2}
}

func coverage(t *testing.T, ctx context.Context, repo *Repository, q ports.PenCareCoverageQuery) ports.PenCareCoveragePage {
	t.Helper()
	q.TenantID = pcTenant
	page, err := repo.PenCareCoverage(ctx, q)
	if err != nil {
		t.Fatalf("PenCareCoverage: %v", err)
	}
	return page
}

// cellOf returns the done date of one category on the row whose display matches.
func cellOf(t *testing.T, page ports.PenCareCoveragePage, shedID, partition, category string) (string, bool) {
	t.Helper()
	for _, row := range page.Rows {
		if row.ShedID != shedID || row.PartitionLabel != partition {
			continue
		}
		if len(row.Cells) != len(domain.PlannerCategories) {
			t.Fatalf("row %s/%s has %d cells, want %d", shedID, partition, len(row.Cells), len(domain.PlannerCategories))
		}
		for _, cell := range row.Cells {
			if cell.Category == category {
				return cell.LastDoneBusinessDate, true
			}
		}
	}
	return "", false
}

// ONE-TO-MANY: a pen collects MANY tasks per category over time, each with several assignees.
// The board must show ONE cell per category per pen carrying the LATEST done day, never a row per
// task; and a partitioned shed's pens (MultipleDimensions: shed x partition) must never borrow
// each other's ticks.
func TestPenCoverageOneToManyTasksAndMultipleDimensionsPartitions(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	seedCoverageGodel(t, ctx, repo)

	coverageTask(t, ctx, repo, pcPark, pcShedA, "", domain.CategoryDeworming, 2, "completed", "completed", 3)
	coverageTask(t, ctx, repo, pcPark, pcShedA, "", domain.CategoryDeworming, 10, "completed", "completed", 11)
	coverageTask(t, ctx, repo, pcPark, pcShedA, "", domain.CategoryDeworming, 6, "completed", "completed", 7)
	coverageTask(t, ctx, repo, pcPark, covShedGodel, "Part 1", domain.CategoryHoofTrimming, 4, "completed", "completed", 5)

	page := coverage(t, ctx, repo, ports.PenCareCoverageQuery{TenantWide: true, Limit: 50})
	if page.Total != 3 || len(page.Rows) != 3 {
		t.Fatalf("pens = total %d rows %d, want 3 (Castro + Godel 1 Part 1/Part 2) with no per-task fan-out", page.Total, len(page.Rows))
	}
	if got, _ := cellOf(t, page, pcShedA, "", domain.CategoryDeworming); got != "2026-09-11" {
		t.Fatalf("Castro deworming last done = %q, want the latest of three, 2026-09-11", got)
	}
	if got, _ := cellOf(t, page, covShedGodel, "Part 1", domain.CategoryHoofTrimming); got != "2026-09-05" {
		t.Fatalf("Godel 1 Part 1 hoof = %q, want 2026-09-05", got)
	}
	if got, ok := cellOf(t, page, covShedGodel, "Part 2", domain.CategoryHoofTrimming); !ok || got != "" {
		t.Fatalf("Godel 1 Part 2 hoof = %q (found %v), want no tick: its sibling pen's work is not its own", got, ok)
	}
	if got, _ := cellOf(t, page, pcShedA, "", domain.CategoryHoofTrimming); got != "" {
		t.Fatalf("Castro hoof = %q, want no tick: another shed's work must not leak across", got)
	}
}

// PAGINATION: a keyset walk at a tiny page size must visit every pen exactly once across page
// boundaries, and total must stay the whole-scope count on every page.
func TestPenCoveragePaginationPageBoundaryWalksEveryPenOnce(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	seedCoverageGodel(t, ctx, repo)
	seedRoundCardsPark(t, ctx, repo, covShedOther, "S-O", "Gandhi 2")
	coverageTask(t, ctx, repo, pcPark, covShedOther, "", domain.CategoryTicksRemoval, 3, "completed", "completed", 3)

	seen := map[string]int{}
	cursor := ""
	pages := 0
	for {
		page := coverage(t, ctx, repo, ports.PenCareCoverageQuery{TenantWide: true, Limit: 1, Cursor: cursor})
		pages++
		if page.Total != 4 {
			t.Fatalf("page %d total = %d, want the whole-scope 4 on every page", pages, page.Total)
		}
		if len(page.Rows) != 1 {
			t.Fatalf("page %d rows = %d, want 1", pages, len(page.Rows))
		}
		seen[page.Rows[0].ShedID+"|"+page.Rows[0].PartitionLabel]++
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if pages != 4 || len(seen) != 4 {
		t.Fatalf("walked %d pages over %d distinct pens (%v), want 4 and 4", pages, len(seen), seen)
	}
	for pen, n := range seen {
		if n != 1 {
			t.Fatalf("pen %s listed %d times across pages", pen, n)
		}
	}
	if _, err := repo.PenCareCoverage(ctx, ports.PenCareCoverageQuery{TenantID: pcTenant, TenantWide: true, Cursor: "not-a-cursor!"}); err == nil {
		t.Fatal("a malformed cursor must be refused, not read as the first page")
	}
}

// PARK SCOPE: a park-scoped caller sees only their park's pens, and naming one park narrows a
// tenant-wide caller. A pen in the other park with done work must never appear.
func TestPenCoverageParkScopeClampsToAuthorizedParks(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($2::uuid, $1::uuid, 'shed', 'S-X', 'Castro', 'active', $3::uuid, 1) ON CONFLICT (location_id) DO NOTHING`,
		pcTenant, covShedOther, pcOtherPark); err != nil {
		t.Fatalf("seed other-park shed: %v", err)
	}
	coverageTask(t, ctx, repo, pcOtherPark, covShedOther, "", domain.CategoryDeworming, 3, "completed", "completed", 3)

	scoped := coverage(t, ctx, repo, ports.PenCareCoverageQuery{AuthorizedParkIDs: []string{pcPark}, Limit: 50})
	if scoped.Total != 1 || len(scoped.Rows) != 1 || scoped.Rows[0].ShedID != pcShedA {
		t.Fatalf("park-scoped rows = %+v, want only CPT's Castro", scoped.Rows)
	}
	if got, _ := cellOf(t, scoped, pcShedA, "", domain.CategoryDeworming); got != "" {
		t.Fatalf("CPT Castro deworming = %q, want no tick: the same-named CBE Castro's work must not leak", got)
	}
	none := coverage(t, ctx, repo, ports.PenCareCoverageQuery{Limit: 50})
	if none.Total != 0 || len(none.Rows) != 0 {
		t.Fatalf("no parks + not tenant-wide = %d rows, want 0", len(none.Rows))
	}
	narrowed := coverage(t, ctx, repo, ports.PenCareCoverageQuery{TenantWide: true, ParkID: pcOtherPark, Limit: 50})
	if narrowed.Total != 1 || narrowed.Rows[0].ShedID != covShedOther || narrowed.Rows[0].ParkName != "CBE" {
		t.Fatalf("park_id narrow = %+v, want only CBE's Castro", narrowed.Rows)
	}
	if got, _ := cellOf(t, narrowed, covShedOther, "", domain.CategoryDeworming); got != "2026-09-03" {
		t.Fatalf("CBE Castro deworming = %q, want 2026-09-03", got)
	}
}

// STATUS MATRIX: submitted work ticks — approved, or still waiting for the verifier (maintainer
// decision 2026-09-26). Open, sent back for rework, and canceled work each leave the cell empty.
func TestPenCoverageStatusMatrixSubmittedWorkTicks(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)

	cases := []struct {
		category, workState, status string
		wantTick                    bool
	}{
		{domain.CategoryDeworming, "scheduled", "open", false},
		{domain.CategoryAntiProtozoan, "scheduled", "pending_verification", true},
		{domain.CategoryTicksRemoval, "delayed", "rework", false},
		{domain.CategoryHairTrimming, "canceled", "completed", false},
		{domain.CategoryHoofTrimming, "completed", "completed", true},
	}
	for i, c := range cases {
		coverageTask(t, ctx, repo, pcPark, pcShedA, "", c.category, 3+i, c.workState, c.status, 3+i)
	}
	page := coverage(t, ctx, repo, ports.PenCareCoverageQuery{TenantWide: true, Limit: 50})
	for i, c := range cases {
		got, _ := cellOf(t, page, pcShedA, "", c.category)
		if c.wantTick && got != fmt.Sprintf("2026-09-%02d", 3+i) {
			t.Fatalf("%s (%s/%s) = %q, want a tick dated its submit day", c.category, c.workState, c.status, got)
		}
		if !c.wantTick && got != "" {
			t.Fatalf("%s (%s/%s) = %q, want no tick", c.category, c.workState, c.status, got)
		}
	}
	// The columns are exactly the five hands-on-the-animal jobs, in order: the kernel-owned
	// categories (feed & water removal, vaccine inventory) are never board columns.
	cells := page.Rows[0].Cells
	if len(cells) != len(domain.PlannerCategories) {
		t.Fatalf("columns = %d, want %d", len(cells), len(domain.PlannerCategories))
	}
	for i, category := range domain.PlannerCategories {
		if cells[i].Category != category {
			t.Fatalf("column %d = %s, want %s", i, cells[i].Category, category)
		}
	}
}

// PEN FILTER + FILTER VOCABULARY (park scope): choosing a pen narrows the board and its total to
// that pen alone, while neither dropdown shrinks — the park list still offers every scoped park
// and the pen list still offers every pen of the selected park. With no park selected, same-named
// pens in two parks stay distinguishable because each label names its park.
func TestPenCoveragePenFilterAndOptionsKeepParkScope(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	seedCoverageGodel(t, ctx, repo)
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($2::uuid, $1::uuid, 'shed', 'S-X', 'Castro', 'active', $3::uuid, 1) ON CONFLICT (location_id) DO NOTHING`,
		pcTenant, covShedOther, pcOtherPark); err != nil {
		t.Fatalf("seed other-park shed: %v", err)
	}
	coverageTask(t, ctx, repo, pcPark, covShedGodel, "Part 2", domain.CategoryDeworming, 3, "completed", "completed", 3)

	all := coverage(t, ctx, repo, ports.PenCareCoverageQuery{TenantWide: true, Limit: 50})
	if all.Total != 4 || len(all.ParkOptions) != 2 || len(all.PenOptions) != 4 {
		t.Fatalf("unfiltered total=%d parks=%d pens=%d, want 4/2/4", all.Total, len(all.ParkOptions), len(all.PenOptions))
	}
	castroLabels := map[string]bool{}
	for _, o := range all.PenOptions {
		if o.Value == pcShedA+"|whole" || o.Value == covShedOther+"|whole" {
			castroLabels[o.Label] = true
		}
	}
	if len(castroLabels) != 2 || !castroLabels["Castro · CPT"] || !castroLabels["Castro · CBE"] {
		t.Fatalf("same-named pens across parks = %v, want \"Castro · CPT\" and \"Castro · CBE\"", castroLabels)
	}

	pen := coverage(t, ctx, repo, ports.PenCareCoverageQuery{TenantWide: true, ParkID: pcPark, Pens: []ports.PenCareCoveragePen{{ShedID: covShedGodel, PartitionKey: "part 2"}}, Limit: 50})
	if pen.Total != 1 || len(pen.Rows) != 1 || pen.Rows[0].PartitionLabel != "Part 2" {
		t.Fatalf("pen filter rows = %+v total %d, want only Godel 1 Part 2", pen.Rows, pen.Total)
	}
	if got, _ := cellOf(t, pen, covShedGodel, "Part 2", domain.CategoryDeworming); got != "2026-09-03" {
		t.Fatalf("filtered pen deworming = %q, want 2026-09-03", got)
	}
	if len(pen.ParkOptions) != 2 {
		t.Fatalf("park options under a park filter = %d, want both parks still offered", len(pen.ParkOptions))
	}
	if len(pen.PenOptions) != 3 {
		t.Fatalf("pen options under a pen filter = %d, want CPT's 3 pens (the filter must not empty its own list)", len(pen.PenOptions))
	}
	for _, o := range pen.PenOptions {
		if o.Value == covShedOther+"|whole" {
			t.Fatal("pen options leaked the other park's pen while a park is selected")
		}
		if o.Label == "Godel 1 - Part 2 · CPT" {
			t.Fatalf("pen label %q names the park although one park is selected", o.Label)
		}
	}

	// MULTI-SELECT: two ticked pens in two parks (OR within the filter), in board order.
	two := coverage(t, ctx, repo, ports.PenCareCoverageQuery{TenantWide: true, Limit: 50, Pens: []ports.PenCareCoveragePen{
		{ShedID: covShedGodel, PartitionKey: "Part 1"}, {ShedID: covShedOther, PartitionKey: "whole"},
	}})
	if two.Total != 2 || len(two.Rows) != 2 || two.Rows[0].ShedID != covShedOther || two.Rows[1].PartitionLabel != "Part 1" {
		t.Fatalf("two ticked pens = %+v total %d, want CBE Castro then CPT Godel 1 Part 1", two.Rows, two.Total)
	}

	scoped := coverage(t, ctx, repo, ports.PenCareCoverageQuery{AuthorizedParkIDs: []string{pcPark}, Pens: []ports.PenCareCoveragePen{{ShedID: covShedOther, PartitionKey: "whole"}}, Limit: 50})
	if scoped.Total != 0 || len(scoped.ParkOptions) != 1 {
		t.Fatalf("pen filter aimed outside the caller's parks = total %d, park options %d; want 0 rows and only CPT offered", scoped.Total, len(scoped.ParkOptions))
	}
}

// PEN ORDER: pens read in the farm's order, numbers compared as numbers — Part 2 before Part 10,
// Yashoda 9 before Yashoda 10 — on the board, across keyset page boundaries (Pagination), and in
// the Pen filter's list. Plain text order put every "10" before "2".
func TestPenCoveragePaginationOrdersPenNumbersNaturally(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	yashoda9, yashoda10 := "9c000000-0000-4000-8000-000000004309", "9c000000-0000-4000-8000-000000004310"
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($2::uuid, $1::uuid, 'shed', 'S-M', 'Mandela 1', 'active', $5::uuid, 3),
       ($3::uuid, $1::uuid, 'shed', 'S-Y10', 'Yashoda 10', 'active', $5::uuid, 4),
       ($4::uuid, $1::uuid, 'shed', 'S-Y9', 'Yashoda 9', 'active', $5::uuid, 5)
ON CONFLICT (location_id) DO NOTHING`, pcTenant, covShedGodel, yashoda10, yashoda9, pcPark); err != nil {
		t.Fatalf("seed sheds: %v", err)
	}
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 10', '10', 'active', 'manual'), ($1::uuid, $2::uuid, 'Part 2', '2', 'active', 'manual'),
       ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')
ON CONFLICT DO NOTHING`, pcTenant, covShedGodel); err != nil {
		t.Fatalf("seed partitions: %v", err)
	}
	want := []string{"Castro", "Mandela 1|Part 1", "Mandela 1|Part 2", "Mandela 1|Part 10", "Yashoda 9", "Yashoda 10"}
	label := func(row ports.PenCareCoverageRow) string {
		if row.PartitionLabel == "" {
			return row.ShedName
		}
		return row.ShedName + "|" + row.PartitionLabel
	}

	var walked []string
	cursor := ""
	for i := 0; i < 10; i++ {
		page := coverage(t, ctx, repo, ports.PenCareCoverageQuery{TenantWide: true, Limit: 2, Cursor: cursor})
		for _, row := range page.Rows {
			walked = append(walked, label(row))
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if fmt.Sprint(walked) != fmt.Sprint(want) {
		t.Fatalf("board order across pages = %v, want %v", walked, want)
	}

	page := coverage(t, ctx, repo, ports.PenCareCoverageQuery{TenantWide: true, ParkID: pcPark, Limit: 50})
	var options []string
	for _, o := range page.PenOptions {
		options = append(options, o.Label)
	}
	wantOptions := []string{"Castro", "Mandela 1 - Part 1", "Mandela 1 - Part 2", "Mandela 1 - Part 10", "Yashoda 9", "Yashoda 10"}
	if fmt.Sprint(options) != fmt.Sprint(wantOptions) {
		t.Fatalf("pen filter order = %v, want %v", options, wantOptions)
	}
}
