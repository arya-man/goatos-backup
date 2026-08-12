package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// A shed with several active pens still gets exactly ONE transport task, named by the bare shed.
// Transport is one loading trip for the shed, so a pen grain would ask an operator to film the same
// physical load once per pen. Migration 000143 did exactly that; 000152 is the forward repair.
func TestFeedTransportPartitionedShedStillGetsOneShedGrainTaskPageBoundaryExecutionDateParkScopeStatusMatrix(t *testing.T) {
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
	// Retries and overlapping scheduler workers must not add a second task for the same shed.
	if _, err := repo.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{
		TenantID: fdTenant, AsOf: day.Add(16 * time.Hour),
	}); err != nil {
		t.Fatalf("re-materialize: %v", err)
	}

	all, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day, ActorID: transportOperator, Limit: 20,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all.Items) != 2 {
		t.Fatalf("tasks=%d want 2: one for Shed A (two pens, still one trip) and one for Shed B; %+v", len(all.Items), all.Items)
	}
	shedATasks := make([]ports.FeedTransportTask, 0, 2)
	for _, it := range all.Items {
		if it.ShedID == shedID {
			shedATasks = append(shedATasks, it)
		}
	}
	if len(shedATasks) != 1 {
		t.Fatalf("Shed A has %d tasks, want exactly 1 -- a partitioned shed must not fan out into one task per pen: %+v", len(shedATasks), shedATasks)
	}
	if shedATasks[0].PartitionLabel != "" {
		t.Fatalf("partition_label=%q want empty -- transport rows carry no pen", shedATasks[0].PartitionLabel)
	}
	if shedATasks[0].OperationalLocationDisplay != shedName {
		t.Fatalf("display=%q want the bare shed name %q", shedATasks[0].OperationalLocationDisplay, shedName)
	}

	// The shed FILTER is shed-grain too: one option per shed, keyed by the bare shed UUID, so the
	// dropdown cannot list the same shed once per pen.
	shedOptions := 0
	for _, o := range all.Filters.Sheds {
		if o.ID == shedID {
			shedOptions++
			if o.Label != shedName {
				t.Fatalf("filter label=%q want %q", o.Label, shedName)
			}
			if o.PartitionLabel != "" {
				t.Fatalf("filter option carries partition %q -- transport options are shed-grain", o.PartitionLabel)
			}
		}
	}
	if shedOptions != 1 {
		t.Fatalf("Shed A appears %d times in the shed filter, want 1", shedOptions)
	}

	// Filtering by that option returns the shed's single task, not a subset of it.
	shedOnly, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day, ActorID: transportOperator, ShedID: shedID, Limit: 20,
	})
	if err != nil {
		t.Fatalf("shed filter: %v", err)
	}
	if len(shedOnly.Items) != 1 || shedOnly.Items[0].ShedID != shedID {
		t.Fatalf("shed filter items=%+v want the one Shed A task", shedOnly.Items)
	}

	// The DETAIL read must agree with the list read; they used to disagree.
	detail, err := repo.GetTransportTask(ctx, fdTenant, all.Items[0].TaskID)
	if err != nil {
		t.Fatalf("get transport task: %v", err)
	}
	for _, it := range all.Items {
		if it.TaskID == detail.TaskID && it.OperationalLocationDisplay != detail.OperationalLocationDisplay {
			t.Fatalf("list says %q, detail says %q -- one screen showing a different location than the other is the defect this branch exists to close",
				it.OperationalLocationDisplay, detail.OperationalLocationDisplay)
		}
	}

	// PAGE BOUNDARY -- a LIMIT of 1 must still yield a cursor and the same first row.
	pageOne, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day, ActorID: transportOperator, Limit: 1,
	})
	if err != nil {
		t.Fatalf("page one: %v", err)
	}
	if len(pageOne.Items) != 1 {
		t.Fatalf("page one returned %d rows, want exactly 1", len(pageOne.Items))
	}
	if pageOne.NextCursor == "" {
		t.Fatal("page one returned no cursor while more rows exist -- the page boundary is broken")
	}
	if pageOne.Items[0].TaskID != all.Items[0].TaskID {
		t.Fatalf("page one first row = %s, unpaged first row = %s -- ordering changed under LIMIT",
			pageOne.Items[0].TaskID, all.Items[0].TaskID)
	}
	// The filter vocabulary is whole-DATE scoped, never derived from the visible page: a
	// one-row page must still offer every shed the day has.
	if len(pageOne.Filters.Sheds) != len(all.Filters.Sheds) {
		t.Fatalf("filter options collapsed to the page: %d vs %d -- the dropdown must stay whole-date scoped",
			len(pageOne.Filters.Sheds), len(all.Filters.Sheds))
	}

	// EXECUTION DATE -- the read is scoped to one business date.
	otherDay, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: fdTenant, Day: day.AddDate(0, 0, 1), ActorID: transportOperator, Limit: 20,
	})
	if err != nil {
		t.Fatalf("next-day list: %v", err)
	}
	for _, it := range otherDay.Items {
		if it.BusinessDate == all.Items[0].BusinessDate {
			t.Fatalf("next-day read returned a task dated %s -- date scope leaked", it.BusinessDate)
		}
	}
}
