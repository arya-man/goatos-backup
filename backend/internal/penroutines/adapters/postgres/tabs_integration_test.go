package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
)

// phoneTaskDSL is a phone-task SOP version's form_dsl: one tab, the given parks (park -> person,
// pens), daily, a verifier, two videos per task.
func phoneTaskDSL(icon string, filters []string, parks ...map[string]any) map[string]any {
	parkList := make([]any, 0, len(parks))
	for _, p := range parks {
		parkList = append(parkList, p)
	}
	filterList := make([]any, 0, len(filters))
	for _, f := range filters {
		filterList = append(filterList, f)
	}
	return map[string]any{"phone_task": map[string]any{
		"tab":          map[string]any{"icon": icon, "filters": filterList},
		"instruction":  "Spray every pen; film before and after.",
		"scope_kind":   "selected_pens",
		"cadence_kind": "daily", "start_date": "2026-09-14",
		"review_kind": "verifier",
		"evidence":    map[string]any{"questions": []any{}, "photo": map[string]any{"min": 0, "max": 0}, "video": map[string]any{"min": 2, "max": 2}, "presence": "off"},
		"parks":       parkList,
	}}
}

func parkEntry(park, person string, pens ...domain.PenRef) map[string]any {
	list := make([]any, 0, len(pens))
	for _, p := range pens {
		list = append(list, map[string]any{"shed_id": p.ShedID, "partition_label": p.Partition})
	}
	return map[string]any{"park_id": park, "assignee_user_id": person, "pens": list}
}

// syncSOP runs one publish of a phone-task SOP in its own transaction, as the SOP publish hook does.
func syncSOP(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, ev PhoneTaskSOPEvent) error {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SyncPhoneTaskSOP(ctx, tx, ev); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// TestPhoneTaskSOPPublishWritesTabAndRoutinesThroughTheSOPPublishPostgres drives the REAL SOP
// repository (docs/decisions/simple-task-phone-tabs.md): a person who does not work at the park
// refuses the whole publish (nothing written); v1 of a pc_care.* SOP -> one tab in the Preventive
// Care bar and one routine per park, raising work for exactly the people named; a routine from the
// SOP is not edited on /routines; v2 drops a park and changes the icon -> that park's routine
// retires, the other gets a new version, the tab follows; retiring the SOP retires its tab.
func TestPhoneTaskSOPPublishWritesTabAndRoutinesThroughTheSOPPublishPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedRoutineFixture(t, ctx, pool)
	const today = "2026-09-16"
	now := istInstant(today, 9)
	repo := NewRepository(pool, 15*time.Second).WithClock(func() time.Time { return now })
	sops := soppg.NewRepository(pool, 15*time.Second).WithVersionStatusHook(func(ctx context.Context, tx pgx.Tx, e soppg.VersionStatusEvent) error {
		return repo.SyncPhoneTaskSOP(ctx, tx, PhoneTaskSOPEvent{TenantID: e.TenantID, ActorID: e.ActorID, SOPCode: e.SOPCode, SOPName: e.SOPName, Status: e.Status, StillPublished: e.StillPublished, FormDSL: e.FormDSL})
	})
	def, err := sops.CreateSOP(ctx, sopports.CreateSOPCommand{TenantID: prTenant, ActorID: prCXO, Body: sopdomain.CreateSOPRequest{Code: "pc_care.pen_wash", Name: "Pen wash", Kind: "module"}})
	if err != nil {
		t.Fatalf("create sop: %v", err)
	}
	version := func(label string, dsl map[string]any) sopdomain.SOPVersion {
		t.Helper()
		v, err := sops.CreateVersion(ctx, sopports.CreateVersionCommand{TenantID: prTenant, ActorID: prCXO, SOPID: def.SOPID, Body: sopdomain.CreateSOPVersionRequest{VersionLabel: label, FormDSL: dsl, ProofPolicy: map[string]any{}}, Report: sopdomain.ValidationReport{Valid: true}})
		if err != nil {
			t.Fatalf("version %s: %v", label, err)
		}
		return v
	}
	publish := func(v sopdomain.SOPVersion) (sopdomain.SOPVersion, error) {
		return sops.PublishVersion(ctx, sopports.VersionCommand{TenantID: prTenant, ActorID: prCXO, SOPID: def.SOPID, SOPVersionID: v.SOPVersionID, RowVersion: v.RowVersion})
	}
	castro := []domain.PenRef{{ShedID: prShedCastro, Partition: "1"}, {ShedID: prShedCastro, Partition: "2"}}

	bad := version("v0", phoneTaskDSL("fumigation", []string{"status"}, parkEntry(prParkCBE, prCPTHead, castro...)))
	if _, err := publish(bad); !errors.Is(err, domain.ErrNotAssignable) {
		t.Fatalf("publish naming the other park's head err = %v, want ErrNotAssignable", err)
	}
	var routines, tabs int
	var badStatus string
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM pen_routine_definitions WHERE tenant_id = $1::uuid), (SELECT count(*) FROM pen_routine_tabs WHERE tenant_id = $1::uuid), (SELECT status FROM sop_versions WHERE sop_version_id = $2::uuid)`, prTenant, bad.SOPVersionID).Scan(&routines, &tabs, &badStatus)
	if routines != 0 || tabs != 0 || badStatus != "draft" {
		t.Fatalf("a refused publish wrote %d routines / %d tabs and left the version %q", routines, tabs, badStatus)
	}

	v1 := version("v1", phoneTaskDSL("fumigation", []string{"status", "pen"}, parkEntry(prParkCBE, prHead, castro...), parkEntry(prParkCPT, prCPTHead, domain.PenRef{ShedID: prShedCPT})))
	if _, err := publish(v1); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	if bar, err := repo.PhoneTabsFor(ctx, prTenant, prHead); err != nil || len(bar) != 1 || bar[0].Key != "pc_care_pen_wash" || bar[0].ModuleKey != "pc_care" || bar[0].Label != "Pen wash" || bar[0].IconKey != "fumigation" {
		t.Fatalf("CBE head bar = %+v / %v", bar, err)
	}
	if bar, _ := repo.PhoneTabsFor(ctx, prTenant, prCPTHead); len(bar) != 1 {
		t.Fatalf("CPT head bar = %+v", bar)
	}
	if bar, _ := repo.PhoneTabsFor(ctx, prTenant, prSecond); len(bar) != 0 {
		t.Fatalf("a park head not named got the tab: %+v", bar)
	}
	if res, err := repo.Materialize(ctx, prTenant, today, today, now); err != nil || res.Created != 3 {
		t.Fatalf("materialize = %+v / %v, want 2 CBE pens + 1 CPT pen", res, err)
	}
	page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "pc_care_pen_wash", Limit: 50, WithPenOptions: true})
	if err != nil || len(page.Rows) != 2 || len(page.PenOptions) != 2 {
		t.Fatalf("CBE tab list = %d rows, %d pens / %v", len(page.Rows), len(page.PenOptions), err)
	}

	cbe := page.Rows[0].RoutineID
	if _, err := repo.SetRoutineStatus(ctx, ports.WriteParams{TenantID: prTenant, ActorID: prCXO, IdempotencyKey: "manual-pause"}, cbe, domain.StatusPaused, 0); !errors.Is(err, domain.ErrManagedBySOP) {
		t.Fatalf("manual pause of an SOP routine err = %v, want ErrManagedBySOP", err)
	}

	v2 := version("v2", phoneTaskDSL("water", []string{"status"}, parkEntry(prParkCBE, prHead, castro...)))
	if _, err := publish(v2); err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	var cbeVersion int
	var cptStatus string
	_ = pool.QueryRow(ctx, `SELECT (SELECT current_version FROM pen_routine_definitions WHERE routine_id = $1::uuid), (SELECT status FROM pen_routine_definitions WHERE tenant_id = $2::uuid AND sop_code = 'pc_care.pen_wash' AND park_id = $3::uuid)`, cbe, prTenant, prParkCPT).Scan(&cbeVersion, &cptStatus)
	if cbeVersion != 2 || cptStatus != domain.StatusRetired {
		t.Fatalf("after v2: CBE version %d (want 2), CPT status %q (want retired)", cbeVersion, cptStatus)
	}
	if bar, _ := repo.PhoneTabsFor(ctx, prTenant, prHead); len(bar) != 1 || bar[0].IconKey != "water" {
		t.Fatalf("tab did not follow v2: %+v", bar)
	}
	if bar, _ := repo.PhoneTabsFor(ctx, prTenant, prCPTHead); len(bar) != 0 {
		t.Fatalf("the dropped park kept the tab: %+v", bar)
	}

	published, err := sops.GetVersion(ctx, prTenant, def.SOPID, v2.SOPVersionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sops.RetireVersion(ctx, sopports.VersionCommand{TenantID: prTenant, ActorID: prCXO, SOPID: def.SOPID, SOPVersionID: published.SOPVersionID, RowVersion: published.RowVersion}); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if bar, _ := repo.PhoneTabsFor(ctx, prTenant, prHead); len(bar) != 0 {
		t.Fatalf("a retired SOP kept its tab: %+v", bar)
	}
}

// newTabFixture publishes one phone-task SOP ("pc_care.wash", tab key pc_care_wash) for the CBE
// head over Castro 1 and 2, with tasks materialized on each of the given business dates.
func newTabFixture(t *testing.T, ctx context.Context, dates ...string) (*Repository, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.StartPostgres(t, ctx)
	seedRoutineFixture(t, ctx, pool)
	now := istInstant(dates[0], 9)
	repo := NewRepository(pool, 15*time.Second).WithClock(func() time.Time { return now })
	dsl := phoneTaskDSL("water", []string{"status", "pen"}, parkEntry(prParkCBE, prHead, domain.PenRef{ShedID: prShedCastro, Partition: "1"}, domain.PenRef{ShedID: prShedCastro, Partition: "2"}))
	dsl["phone_task"].(map[string]any)["start_date"] = dates[0]
	if err := syncSOP(t, ctx, pool, repo, PhoneTaskSOPEvent{TenantID: prTenant, ActorID: prCXO, SOPCode: "pc_care.wash", SOPName: "Wash", Status: "published", StillPublished: true, FormDSL: dsl}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	for _, date := range dates {
		if _, err := repo.Materialize(ctx, prTenant, date, date, istInstant(date, 9)); err != nil {
			t.Fatalf("materialize %s: %v", date, err)
		}
	}
	return repo, pool
}

// TestPenRoutineTabPenOptionsOneToManyCountEachTaskOnce: a pen holding tasks on two dates is ONE
// pen option counting both, never two rows, and never inflated by the per-task assignee join.
func TestPenRoutineTabPenOptionsOneToManyCountEachTaskOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	repo, _ := newTabFixture(t, ctx, "2026-09-16", "2026-09-17")
	page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "pc_care_wash", Limit: 50, WithPenOptions: true})
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
		page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "pc_care_wash", Limit: 1, Cursor: cursor})
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
	repo, pool := newTabFixture(t, ctx, today)
	// Republish the SOP naming both parks.
	dsl := phoneTaskDSL("water", []string{"status"},
		parkEntry(prParkCBE, prHead, domain.PenRef{ShedID: prShedCastro, Partition: "1"}, domain.PenRef{ShedID: prShedCastro, Partition: "2"}),
		parkEntry(prParkCPT, prCPTHead, domain.PenRef{ShedID: prShedCPT}))
	dsl["phone_task"].(map[string]any)["start_date"] = today
	if err := syncSOP(t, ctx, pool, repo, PhoneTaskSOPEvent{TenantID: prTenant, ActorID: prCXO, SOPCode: "pc_care.wash", SOPName: "Wash", Status: "published", StillPublished: true, FormDSL: dsl}); err != nil {
		t.Fatalf("republish: %v", err)
	}
	if _, err := repo.Materialize(ctx, prTenant, today, today, istInstant(today, 9)); err != nil {
		t.Fatal(err)
	}
	for user, want := range map[string]int{prHead: 2, prCPTHead: 1} {
		if bar, err := repo.PhoneTabsFor(ctx, prTenant, user); err != nil || len(bar) != 1 {
			t.Fatalf("%s bar = %+v / %v", user, bar, err)
		}
		page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: user, TabKey: "pc_care_wash", Limit: 50})
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
	repo, pool := newTabFixture(t, ctx, "2026-09-14", "2026-09-15", "2026-09-16")
	// Six tasks (2 pens x 3 days). Force one into each terminal / late state.
	states := []string{domain.WorkStateCompleted, domain.WorkStateCanceled, domain.WorkStateDelayed}
	for i, state := range states {
		// A completed task carries the matching status and who submitted it, as the table requires.
		if _, err := pool.Exec(ctx, `UPDATE pen_routine_tasks SET work_state = $2,
  status = CASE WHEN $2 = 'completed' THEN 'completed' ELSE status END,
  submitted_at = CASE WHEN $2 = 'completed' THEN now() ELSE submitted_at END,
  submitted_by = CASE WHEN $2 = 'completed' THEN $3::uuid ELSE submitted_by END
WHERE task_id = (SELECT task_id FROM pen_routine_tasks WHERE tenant_id = $1::uuid AND work_state = 'scheduled' ORDER BY due_business_date, task_id LIMIT 1)`, prTenant, state, prHead); err != nil {
			t.Fatalf("state %d: %v", i, err)
		}
	}
	todo, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "pc_care_wash", States: domain.StatesForFilter(domain.FilterToDo), Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	done, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, TabKey: "pc_care_wash", States: domain.StatesForFilter(domain.FilterDone), Limit: 50})
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
