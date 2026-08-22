package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// Regression for the 2026-08-22 STG incident: every first-page monitor read of
// GET /app/pc-care/tasks answered 500 `invalid input syntax for type uuid: "" (22P02)`,
// so a task the CEO had just planned never appeared in the monitor list.
//
// Root cause: the keyset page predicate compared
// `(park.name, ...) > ($9::text, ..., $13::uuid)` behind a `$9::text = ”` guard. The guard
// short-circuits at EXECUTION, but the planner folds the row-comparison arm at PLAN time,
// so casting the empty-string cursor component straight to uuid raised 22P02 before a single
// row was read. Every optional uuid parameter in that predicate must stay nullif-guarded.
//
// Gated by pgtest.SkipIfNoDocker + GOATOS_RUN_POSTGRES_TESTS.
func TestListTasksFirstPageServesWithEmptyOptionalFilters(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	created := createPCTask(t, ctx, repo, "deworming", "idem-list-empty-cursor")

	// The exact production first-page read: no cursor, no park filter, no assignee filter —
	// the CEO monitor's opening request. This is the call that 500'd on STG.
	page, err := repo.ListTasks(ctx, ports.ListTasksQuery{
		TenantID:        pcTenant,
		DueBusinessDate: "2026-08-21",
		TenantWide:      true,
		Category:        "deworming",
		Limit:           20,
	})
	if err != nil {
		t.Fatalf("first-page ListTasks with empty optional filters: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].TaskID != created.TaskID {
		t.Fatalf("first page = %d items, want the created task %s", len(page.Items), created.TaskID)
	}

	// The optional park + assignee filters POPULATED must still narrow correctly (the nullif
	// guard must not turn a real filter into a no-op).
	page, err = repo.ListTasks(ctx, ports.ListTasksQuery{
		TenantID:        pcTenant,
		DueBusinessDate: "2026-08-21",
		TenantWide:      true,
		ParkID:          pcPark,
		AssigneeUserID:  pcOperator1,
		Limit:           20,
	})
	if err != nil {
		t.Fatalf("filtered ListTasks: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("filtered page = %d items, want 1", len(page.Items))
	}

	// A non-matching assignee filter excludes the task (proves the EXISTS arm still bites).
	page, err = repo.ListTasks(ctx, ports.ListTasksQuery{
		TenantID:        pcTenant,
		DueBusinessDate: "2026-08-21",
		TenantWide:      true,
		AssigneeUserID:  pcOutsider,
		Limit:           20,
	})
	if err != nil {
		t.Fatalf("outsider-filtered ListTasks: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("outsider filter returned %d items, want 0", len(page.Items))
	}
}

// A real keyset walk still pages: with limit 1 the first page carries a next cursor and the
// second page resumes strictly after it — the populated-cursor arm of the same predicate.
func TestListTasksKeysetCursorResumesAfterFirstPage(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	first := createPCTask(t, ctx, repo, "deworming", "idem-keyset-a")
	second := createPCTask(t, ctx, repo, "ticks_removal", "idem-keyset-b")

	seen := map[string]bool{}
	cursor := ""
	for range [3]int{} {
		page, err := repo.ListTasks(ctx, ports.ListTasksQuery{
			TenantID:        pcTenant,
			DueBusinessDate: "2026-08-21",
			TenantWide:      true,
			Cursor:          cursor,
			Limit:           1,
		})
		if err != nil {
			t.Fatalf("ListTasks(cursor=%q): %v", cursor, err)
		}
		for _, item := range page.Items {
			if seen[item.TaskID] {
				t.Fatalf("task %s served twice across pages", item.TaskID)
			}
			seen[item.TaskID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if !seen[first.TaskID] || !seen[second.TaskID] {
		t.Fatalf("keyset walk missed a task: saw %v", seen)
	}
}
