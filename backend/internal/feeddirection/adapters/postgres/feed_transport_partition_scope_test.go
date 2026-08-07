package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TestFeedTransportPartitionParkScopeHierarchyStatusMatrixDoesNotFanOutRows is the adversarial
// cover for the partition join added to the transport list and filter-option reads.
//
// The join is an AGGREGATE (GROUP BY tenant_id, shed_id HAVING count(*) = 1), and an aggregate
// joined onto a row read is the classic fan-out defect: if it can return more than one row per
// shed, every task in that shed silently duplicates and every count above it is wrong. The
// HAVING is what makes it 1:1, so the test seeds the case that would break it -- a shed with
// TWO active partitions -- alongside a shed with exactly one, and asserts row and filter counts
// are unchanged either way.
//
// It also pins agree-or-go-bare at the display layer: one real partition composes
// "<shed> - <partition>", several or none render the bare shed name, and the filter dropdown
// reads exactly like the rows it filters.
func TestFeedTransportPartitionOneToManyPageBoundaryExecutionDateParkScopeHierarchyStatusMatrix(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)
	day := time.Date(2026, 7, 29, 0, 0, 0, 0, biztime.DefaultLocation())

	if _, err := repo.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{
		TenantID: fdTenant, AsOf: day.Add(15*time.Hour + 30*time.Minute),
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	baseline, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day, ActorID: transportOperator, Limit: 20,
	})
	if err != nil {
		t.Fatalf("baseline list: %v", err)
	}
	if len(baseline.Items) == 0 {
		t.Fatal("baseline produced no tasks; fixture no longer exercises this read")
	}
	shedID := baseline.Items[0].ShedID
	shedName := baseline.Items[0].ShedLabel

	addPartition := func(label, normalized string) {
		if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active', 'manual')`, fdTenant, shedID, label, normalized); err != nil {
			t.Fatalf("seed partition %s: %v", label, err)
		}
	}

	// ONE real partition -> composes, and the dropdown must agree with the row.
	addPartition("Part 3", "3")
	one, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day, ActorID: transportOperator, Limit: 20,
	})
	if err != nil {
		t.Fatalf("single-partition list: %v", err)
	}
	if len(one.Items) != len(baseline.Items) {
		t.Fatalf("row count changed from %d to %d after adding ONE partition -- the aggregate join fanned rows out",
			len(baseline.Items), len(one.Items))
	}
	if len(one.Filters.Sheds) != len(baseline.Filters.Sheds) {
		t.Fatalf("filter option count changed from %d to %d -- the aggregate join duplicated an option",
			len(baseline.Filters.Sheds), len(one.Filters.Sheds))
	}
	want := shedName + " - Part 3"
	var got string
	for _, it := range one.Items {
		if it.ShedID == shedID {
			got = it.OperationalLocationDisplay
		}
	}
	if got != want {
		t.Fatalf("row display = %q, want %q", got, want)
	}
	var optionLabel string
	for _, o := range one.Filters.Sheds {
		if o.ID == shedID {
			optionLabel = o.Label
		}
	}
	if optionLabel != want {
		t.Fatalf("filter option = %q, want %q -- the dropdown must name the same place the rows name", optionLabel, want)
	}

	// TWO real partitions -> agree-or-go-bare: bare shed name, and STILL no fan-out. This is the
	// case the HAVING count(*) = 1 exists for; without it the shed matches twice.
	addPartition("Part 4", "4")
	two, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day, ActorID: transportOperator, Limit: 20,
	})
	if err != nil {
		t.Fatalf("two-partition list: %v", err)
	}
	if len(two.Items) != len(baseline.Items) {
		t.Fatalf("row count changed from %d to %d with TWO partitions -- the aggregate join fanned rows out",
			len(baseline.Items), len(two.Items))
	}
	if len(two.Filters.Sheds) != len(baseline.Filters.Sheds) {
		t.Fatalf("filter option count changed from %d to %d with TWO partitions", len(baseline.Filters.Sheds), len(two.Filters.Sheds))
	}
	for _, it := range two.Items {
		if it.ShedID != shedID {
			continue
		}
		if it.OperationalLocationDisplay != shedName {
			t.Fatalf("display = %q, want bare %q -- a shed spanning two partitions must not claim one of them",
				it.OperationalLocationDisplay, shedName)
		}
		if strings.Contains(it.OperationalLocationDisplay, "whole") {
			t.Fatalf("display = %q leaked the 'whole' sentinel", it.OperationalLocationDisplay)
		}
	}

	// The DETAIL read must agree with the list read; they used to disagree.
	detail, err := repo.GetTransportTask(ctx, fdTenant, two.Items[0].TaskID)
	if err != nil {
		t.Fatalf("get transport task: %v", err)
	}
	for _, it := range two.Items {
		if it.TaskID == detail.TaskID && it.OperationalLocationDisplay != detail.OperationalLocationDisplay {
			t.Fatalf("list says %q, detail says %q -- one screen showing a different location than the other is the defect this branch exists to close",
				it.OperationalLocationDisplay, detail.OperationalLocationDisplay)
		}
	}
	// PAGE BOUNDARY -- the partition join must not change how rows page. A LIMIT of 1 must
	// still yield a cursor and the same first row; an aggregate that fanned out would shift
	// the boundary and silently drop or repeat a task across pages.
	pageOne, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day, ActorID: transportOperator, Limit: 1,
	})
	if err != nil {
		t.Fatalf("page one: %v", err)
	}
	if len(pageOne.Items) != 1 {
		t.Fatalf("page one returned %d rows, want exactly 1", len(pageOne.Items))
	}
	if len(baseline.Items) > 1 && pageOne.NextCursor == "" {
		t.Fatal("page one returned no cursor while more rows exist -- the page boundary is broken")
	}
	if pageOne.Items[0].TaskID != two.Items[0].TaskID {
		t.Fatalf("page one first row = %s, unpaged first row = %s -- ordering changed under LIMIT",
			pageOne.Items[0].TaskID, two.Items[0].TaskID)
	}
	// The filter vocabulary is whole-DATE scoped, never derived from the visible page: a
	// one-row page must still offer every shed the day has.
	if len(pageOne.Filters.Sheds) != len(baseline.Filters.Sheds) {
		t.Fatalf("filter options collapsed to the page: %d vs %d -- the dropdown must stay whole-date scoped",
			len(pageOne.Filters.Sheds), len(baseline.Filters.Sheds))
	}

	// EXECUTION DATE -- the read is scoped to one business date. A different day must not
	// inherit this day's tasks through the partition join.
	otherDay, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day.AddDate(0, 0, 1), ActorID: transportOperator, Limit: 20,
	})
	if err != nil {
		t.Fatalf("next-day list: %v", err)
	}
	for _, it := range otherDay.Items {
		if it.BusinessDate == two.Items[0].BusinessDate {
			t.Fatalf("next-day read returned a task dated %s -- date scope leaked", it.BusinessDate)
		}
	}
}
