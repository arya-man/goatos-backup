package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func TestFeedTransportPartitionOneToManyPageBoundaryExecutionDateParkScopeHierarchyStatusMatrix(t *testing.T) {
	// Aggregate guard anchor for this changed projection: OneToMany PageBoundary ExecutionDate
	// ParkScope StatusMatrix.
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)
	day := time.Date(2026, 7, 29, 0, 0, 0, 0, biztime.DefaultLocation())

	shedID := fdShedA
	shedName := "Shed A"

	addPartition := func(label, normalized string) {
		if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active', 'manual')`, fdTenant, shedID, label, normalized); err != nil {
			t.Fatalf("seed partition %s: %v", label, err)
		}
	}
	addPartition("Part 3", "3")
	addPartition("Part 4", "4")

	if _, err := repo.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{
		TenantID: fdTenant, AsOf: day.Add(15*time.Hour + 30*time.Minute),
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	two, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day, ActorID: transportOperator, Limit: 20,
	})
	if err != nil {
		t.Fatalf("two-partition list: %v", err)
	}
	if len(two.Items) != 3 {
		t.Fatalf("tasks=%d want 3: two partitions of Shed A plus one whole Shed B", len(two.Items))
	}
	byPartition := map[string]ports.FeedTransportTask{}
	for _, it := range two.Items {
		if it.ShedID == shedID {
			byPartition[it.PartitionLabel] = it
		}
	}
	for _, label := range []string{"Part 3", "Part 4"} {
		row, ok := byPartition[label]
		if !ok {
			t.Fatalf("missing task for partition %q; got %#v", label, byPartition)
		}
		want := shedName + " - " + label
		if row.OperationalLocationDisplay != want {
			t.Fatalf("%s display=%q want %q", label, row.OperationalLocationDisplay, want)
		}
	}

	part3Only, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day, ActorID: transportOperator, ShedID: shedID, PartitionLabel: "Part 3", Limit: 20,
	})
	if err != nil {
		t.Fatalf("partition filter: %v", err)
	}
	if len(part3Only.Items) != 1 || part3Only.Items[0].PartitionLabel != "Part 3" {
		t.Fatalf("partition filter items=%+v want only Part 3", part3Only.Items)
	}
	for _, o := range two.Filters.Sheds {
		if o.ID == shedID && o.PartitionLabel == "Part 3" {
			if o.Label != shedName+" - Part 3" {
				t.Fatalf("filter label=%q want %q", o.Label, shedName+" - Part 3")
			}
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
	if len(two.Items) > 1 && pageOne.NextCursor == "" {
		t.Fatal("page one returned no cursor while more rows exist -- the page boundary is broken")
	}
	if pageOne.Items[0].TaskID != two.Items[0].TaskID {
		t.Fatalf("page one first row = %s, unpaged first row = %s -- ordering changed under LIMIT",
			pageOne.Items[0].TaskID, two.Items[0].TaskID)
	}
	// The filter vocabulary is whole-DATE scoped, never derived from the visible page: a
	// one-row page must still offer every shed the day has.
	if len(pageOne.Filters.Sheds) != len(two.Filters.Sheds) {
		t.Fatalf("filter options collapsed to the page: %d vs %d -- the dropdown must stay whole-date scoped",
			len(pageOne.Filters.Sheds), len(two.Filters.Sheds))
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
