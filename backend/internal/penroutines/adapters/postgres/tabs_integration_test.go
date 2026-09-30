package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Phone tabs on real SQL (docs/decisions/simple-task-phone-tabs.md):
//   - a tab is created with a key derived from its label, a second tab of the same name gets
//     the next free key, and placing a routine on the second tab moves it off the first;
//   - only the person who owes a routine on an active tab gets that tab on their bar; a retired
//     tab leaves every bar;
//   - the list opened from a tab narrows to that tab's routines, and its chip counts, pen
//     options, date window and pen filter all range over the same predicate;
//   - an exact replay of a tab write returns the original; a same-key different body is refused.
func TestPenRoutinePhoneTabsPlaceNarrowAndReachOnlyTheAssigneePostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedRoutineFixture(t, ctx, pool)

	const today = "2026-09-16"
	now := istInstant(today, 9)
	repo := NewRepository(pool, 15*time.Second).WithClock(func() time.Time { return now })
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: prTenant, ActorID: prCXO, IdempotencyKey: key, TraceID: "trace-" + key}
	}
	routine := func(key, name string, pens []domain.PenRef) domain.Definition {
		t.Helper()
		d, err := repo.CreateRoutine(ctx, write(key), domain.Definition{
			ParkID: prParkCBE, Name: name, ScopeKind: domain.ScopeSelectedPens, Pens: pens,
			CadenceKind: domain.CadenceDaily, StartDate: today, NotifyTime: "07:00", ReviewKind: domain.ReviewNone,
			Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeUserID: prHead,
		})
		if err != nil {
			t.Fatalf("create routine %s: %v", name, err)
		}
		return d
	}
	fumigation := routine("r-fum", "Fumigation", []domain.PenRef{{ShedID: prShedCastro, Partition: "1"}, {ShedID: prShedCastro, Partition: "2"}})
	trough := routine("r-trough", "Trough", []domain.PenRef{{ShedID: prShedCastro, Partition: "1"}})
	if res, err := repo.Materialize(ctx, prTenant, today, today, now); err != nil || res.Created != 3 {
		t.Fatalf("materialize = %+v / %v, want 3 tasks", res, err)
	}

	tab := domain.Tab{Label: "Fumigation", ModuleKey: "pc_care", IconKey: "fumigation", Filters: []string{domain.TabFilterStatus, domain.TabFilterPen}, RoutineIDs: []string{fumigation.RoutineID}}
	first, err := repo.CreateTab(ctx, write("tab-1"), tab)
	if err != nil {
		t.Fatalf("create tab: %v", err)
	}
	if first.Key != "fumigation" || len(first.Routines) != 1 || first.Routines[0].RoutineID != fumigation.RoutineID {
		t.Fatalf("tab = %+v", first)
	}
	// Exact replay returns the original; same key, different body, is refused.
	if replay, err := repo.CreateTab(ctx, write("tab-1"), tab); err != nil || replay.TabID != first.TabID {
		t.Fatalf("replay = %+v / %v", replay, err)
	}
	changed := tab
	changed.IconKey = "water"
	if _, err := repo.CreateTab(ctx, write("tab-1"), changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same key different body err = %v", err)
	}

	// Only the assignee's bar carries it.
	bar, err := repo.PhoneTabsFor(ctx, prTenant, prHead)
	if err != nil || len(bar) != 1 || bar[0].Key != "fumigation" || bar[0].ModuleKey != "pc_care" || bar[0].IconKey != "fumigation" {
		t.Fatalf("assignee bar = %+v / %v", bar, err)
	}
	if other, err := repo.PhoneTabsFor(ctx, prTenant, prCXO); err != nil || len(other) != 0 {
		t.Fatalf("a person owing nothing on the tab got %+v / %v", other, err)
	}

	// The list opened from the tab holds only that tab's routine, with matching counts and pens.
	page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "fumigation", Limit: 50, WithPenOptions: true})
	if err != nil {
		t.Fatalf("tab list: %v", err)
	}
	if len(page.Rows) != 2 || page.StateCounts[domain.WorkStateScheduled] != 2 {
		t.Fatalf("tab list rows=%d counts=%v, want 2 fumigation tasks", len(page.Rows), page.StateCounts)
	}
	for _, row := range page.Rows {
		if row.RoutineID != fumigation.RoutineID {
			t.Fatalf("tab list leaked routine %s", row.RoutineName)
		}
	}
	if len(page.PenOptions) != 2 || page.PenOptions[0].Label != "Castro 1" || page.PenOptions[1].Label != "Castro 2" || page.PenOptions[0].Count != 1 {
		t.Fatalf("pen options = %+v, want Castro 1 and Castro 2 once each", page.PenOptions)
	}
	onePen, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "fumigation", PenShedID: prShedCastro, PenPartition: "2", Limit: 50})
	if err != nil || len(onePen.Rows) != 1 || onePen.Rows[0].Partition != "2" || onePen.StateCounts[domain.WorkStateScheduled] != 1 {
		t.Fatalf("pen filter = %+v / %v", onePen, err)
	}
	outside, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "fumigation", DueFrom: "2026-09-17", Limit: 50})
	if err != nil || len(outside.Rows) != 0 || len(outside.StateCounts) != 0 {
		t.Fatalf("a window after today still listed %+v / %v", outside, err)
	}
	// The Routines tab still lists everything.
	all, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, Limit: 50})
	if err != nil || len(all.Rows) != 3 || all.PenOptions != nil {
		t.Fatalf("routines tab = %d rows, pen options %v / %v", len(all.Rows), all.PenOptions, err)
	}

	// A second tab named the same gets the next key; placing the routine there moves it.
	second, err := repo.CreateTab(ctx, write("tab-2"), domain.Tab{Label: "Fumigation", ModuleKey: "pen_routines", IconKey: "routine", RoutineIDs: []string{fumigation.RoutineID, trough.RoutineID}})
	if err != nil || second.Key != "fumigation_2" || len(second.Routines) != 2 {
		t.Fatalf("second tab = %+v / %v", second, err)
	}
	moved, err := repo.GetTabByKey(ctx, prTenant, "fumigation")
	if err != nil || len(moved.Routines) != 0 {
		t.Fatalf("the routine did not leave the first tab: %+v / %v", moved, err)
	}
	// An empty tab leaves the bar; retiring the other does too.
	if bar, _ := repo.PhoneTabsFor(ctx, prTenant, prHead); len(bar) != 1 || bar[0].Key != "fumigation_2" {
		t.Fatalf("bar after the move = %+v", bar)
	}
	if _, err := repo.SetTabStatus(ctx, write("tab-2-retire"), second.TabID, domain.TabStatusRetired, second.RowVersion); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if bar, _ := repo.PhoneTabsFor(ctx, prTenant, prHead); len(bar) != 0 {
		t.Fatalf("a retired tab stayed on the bar: %+v", bar)
	}

	// A stale edit is refused; an unknown routine refuses the whole write.
	if _, err := repo.UpdateTab(ctx, write("tab-1-stale"), domain.Tab{TabID: first.TabID, Label: "Fumigation", ModuleKey: "pc_care", IconKey: "fumigation", RowVersion: first.RowVersion + 5}); !errors.Is(err, ports.ErrTabVersionConflict) {
		t.Fatalf("stale update err = %v", err)
	}
	if _, err := repo.UpdateTab(ctx, write("tab-1-bad"), domain.Tab{TabID: first.TabID, Label: "Fumigation", ModuleKey: "pc_care", IconKey: "fumigation", RoutineIDs: []string{"00000000-0000-4000-8000-0000000fffff"}}); !errors.Is(err, domain.ErrInvalidTab) {
		t.Fatalf("unknown routine err = %v", err)
	}
}

// newTabFixture is one CBE routine for the CBE head over Castro 1 and 2, placed on a "Wash" tab,
// with tasks materialized on each of the given business dates.
func newTabFixture(t *testing.T, ctx context.Context, dates ...string) (*Repository, func(string) ports.WriteParams) {
	t.Helper()
	pool := pgtest.StartPostgres(t, ctx)
	seedRoutineFixture(t, ctx, pool)
	now := istInstant(dates[0], 9)
	repo := NewRepository(pool, 15*time.Second).WithClock(func() time.Time { return now })
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: prTenant, ActorID: prCXO, IdempotencyKey: key, TraceID: "trace-" + key}
	}
	d, err := repo.CreateRoutine(ctx, write("r-wash"), domain.Definition{
		ParkID: prParkCBE, Name: "Wash", ScopeKind: domain.ScopeSelectedPens,
		Pens:        []domain.PenRef{{ShedID: prShedCastro, Partition: "1"}, {ShedID: prShedCastro, Partition: "2"}},
		CadenceKind: domain.CadenceDaily, StartDate: dates[0], NotifyTime: "07:00", ReviewKind: domain.ReviewNone,
		Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeUserID: prHead,
	})
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}
	for _, date := range dates {
		if _, err := repo.Materialize(ctx, prTenant, date, date, istInstant(date, 9)); err != nil {
			t.Fatalf("materialize %s: %v", date, err)
		}
	}
	if _, err := repo.CreateTab(ctx, write("tab-wash"), domain.Tab{Label: "Wash", ModuleKey: "pc_care", IconKey: "water", Filters: []string{domain.TabFilterStatus, domain.TabFilterPen}, RoutineIDs: []string{d.RoutineID}}); err != nil {
		t.Fatalf("create tab: %v", err)
	}
	return repo, write
}

// TestPenRoutineTabPenOptionsOneToManyCountEachTaskOnce: a pen holding tasks on two dates is ONE
// pen option counting both, never two rows, and never inflated by the per-task assignee join.
func TestPenRoutineTabPenOptionsOneToManyCountEachTaskOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	repo, _ := newTabFixture(t, ctx, "2026-09-16", "2026-09-17")
	page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "wash", Limit: 50, WithPenOptions: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Rows) != 4 || len(page.PenOptions) != 2 || page.PenOptions[0].Count != 2 || page.PenOptions[1].Count != 2 {
		t.Fatalf("rows=%d options=%+v, want 4 rows and 2 pens x 2 tasks", len(page.Rows), page.PenOptions)
	}
}

// TestPenRoutineTabListPageBoundary: a tab list paged one row at a time walks every task exactly
// once, and the chip counts on every page are the whole-list numbers, never page-local.
func TestPenRoutineTabListPageBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	repo, _ := newTabFixture(t, ctx, "2026-09-16", "2026-09-17")
	seen := map[string]bool{}
	cursor := ""
	for pageNo := 0; pageNo < 10; pageNo++ {
		page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "wash", Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", pageNo, err)
		}
		if page.StateCounts[domain.WorkStateScheduled]+page.StateCounts[domain.WorkStateDelayed] != 4 {
			t.Fatalf("page %d counts = %v, want the whole list (4)", pageNo, page.StateCounts)
		}
		for _, row := range page.Rows {
			if seen[row.TaskID] {
				t.Fatalf("task %s listed twice", row.TaskID)
			}
			seen[row.TaskID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 4 {
		t.Fatalf("walked %d tasks, want 4", len(seen))
	}
}

// TestPenRoutineTabParkScope: one tab holding a CBE routine for the CBE head and a CPT routine for
// the CPT head reaches BOTH bars, but each head's tab list holds only their own park's work.
func TestPenRoutineTabParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	const today = "2026-09-16"
	repo, write := newTabFixture(t, ctx, today)
	cpt, err := repo.CreateRoutine(ctx, write("r-wash-cpt"), domain.Definition{
		ParkID: prParkCPT, Name: "Wash", ScopeKind: domain.ScopeSelectedPens, Pens: []domain.PenRef{{ShedID: prShedCPT}},
		CadenceKind: domain.CadenceDaily, StartDate: today, NotifyTime: "07:00", ReviewKind: domain.ReviewNone,
		Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeUserID: prCPTHead,
	})
	if err != nil {
		t.Fatalf("create CPT routine: %v", err)
	}
	if _, err := repo.Materialize(ctx, prTenant, today, today, istInstant(today, 9)); err != nil {
		t.Fatal(err)
	}
	tab, err := repo.GetTabByKey(ctx, prTenant, "wash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateTab(ctx, write("tab-wash-both"), domain.Tab{TabID: tab.TabID, Label: "Wash", ModuleKey: "pc_care", IconKey: "water", RoutineIDs: append(tab.RoutineIDs, cpt.RoutineID), RowVersion: tab.RowVersion}); err != nil {
		t.Fatalf("place CPT routine: %v", err)
	}
	for user, want := range map[string]int{prHead: 2, prCPTHead: 1} {
		if bar, err := repo.PhoneTabsFor(ctx, prTenant, user); err != nil || len(bar) != 1 {
			t.Fatalf("%s bar = %+v / %v", user, bar, err)
		}
		page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: user, TabKey: "wash", Limit: 50})
		if err != nil || len(page.Rows) != want {
			t.Fatalf("%s tab list = %d rows / %v, want %d", user, len(page.Rows), err, want)
		}
		for _, row := range page.Rows {
			wantPark := prParkCBE
			if user == prCPTHead {
				wantPark = prParkCPT
			}
			if row.ParkID != wantPark {
				t.Fatalf("%s saw the other park's task %+v", user, row)
			}
		}
	}
}

// TestPenRoutineTabStatusMatrix: every work state lands in exactly one chip bucket; To do lists
// scheduled + delayed, Done lists completed, and cancelled work is counted in no list.
func TestPenRoutineTabStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	repo, _ := newTabFixture(t, ctx, "2026-09-14", "2026-09-15", "2026-09-16")
	// Six tasks (2 pens x 3 days). Force one into each terminal / late state.
	states := []string{domain.WorkStateCompleted, domain.WorkStateCanceled, domain.WorkStateDelayed}
	for i, state := range states {
		// A completed task carries the matching status and who submitted it, as the table requires.
		if _, err := repo.pool.Exec(ctx, `UPDATE pen_routine_tasks SET work_state = $2,
  status = CASE WHEN $2 = 'completed' THEN 'completed' ELSE status END,
  submitted_at = CASE WHEN $2 = 'completed' THEN now() ELSE submitted_at END,
  submitted_by = CASE WHEN $2 = 'completed' THEN $3::uuid ELSE submitted_by END
WHERE task_id = (SELECT task_id FROM pen_routine_tasks WHERE tenant_id = $1::uuid AND work_state = 'scheduled' ORDER BY due_business_date, task_id LIMIT 1)`, prTenant, state, prHead); err != nil {
			t.Fatalf("state %d: %v", i, err)
		}
	}
	todo, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "wash", States: domain.StatesForFilter(domain.FilterToDo), Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	done, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "wash", States: domain.StatesForFilter(domain.FilterDone), Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	c := todo.StateCounts
	if c[domain.WorkStateScheduled] != 3 || c[domain.WorkStateDelayed] != 1 || c[domain.WorkStateCompleted] != 1 || c[domain.WorkStateCanceled] != 1 {
		t.Fatalf("state buckets = %v, want scheduled 3, delayed 1, completed 1, canceled 1", c)
	}
	if len(todo.Rows) != 4 || len(done.Rows) != 1 || done.Rows[0].WorkState != domain.WorkStateCompleted {
		t.Fatalf("todo=%d done=%d, want 4 and 1", len(todo.Rows), len(done.Rows))
	}
	if domain.FilterCount(domain.FilterToDo, c) != 4 || domain.FilterCount(domain.FilterDone, c) != 1 {
		t.Fatalf("chip counts todo=%d done=%d", domain.FilterCount(domain.FilterToDo, c), domain.FilterCount(domain.FilterDone, c))
	}
}
