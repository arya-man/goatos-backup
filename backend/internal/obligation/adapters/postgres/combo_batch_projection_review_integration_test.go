package postgres

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// Adversarial regression tests for the combo-batch aggregate projection
// (ListPlannedComboBatches / ListPlannedComboBatchesKeyset in repository.go).
//
// The projection-review marker on those queries claims:
//   - membership = planned combo:% batches with sop_task_id IS NULL and no
//     stock_reservation context, plus their obligation_instances target_ids via
//     the LATERAL array_agg(DISTINCT target_id).
//   - group_key = batch_id (one row per batch).
//   - join_cardinality = the LATERAL pre-aggregates the 1:N obligation_instances
//     so the outer grain stays one-row-per-batch with no JOIN fan-out.
//   - pagination = keyset over (scope_type,scope_id,session,planned_date,batch_id)
//     so groups are contiguous and never split across pages.
//   - scope = batch scope_type/scope_id (park or shed).
//
// Each test below proves one of those claims against a real Postgres instance.

const (
	comboSopVersionID = "b0000000-0000-4000-8000-000000000002" // seeded skeleton sop_version (migration 000075)
)

// seedComboGoat inserts an additional alive goat target in the CBE park.
func seedComboGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)
			 ON CONFLICT (goat_id) DO NOTHING`,
		goatID, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed combo goat %s: %v", goatID, err)
	}
}

// insertComboObligation inserts one scheduled obligation for goatID in the given scope.
func insertComboObligation(t *testing.T, ctx context.Context, repo *Repository, versionID, ruleID, goatID, scopeType, scopeID, key string, due time.Time) string {
	t.Helper()
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: scopeType, ScopeID: scopeID,
		DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
	})
	if err != nil || !applied || id == "" {
		t.Fatalf("insert combo obligation %s: applied=%v err=%v", key, applied, err)
	}
	return id
}

// mkPlannedComboBatch creates one planned combo batch (no attached obligations) and returns its id.
func mkPlannedComboBatch(t *testing.T, ctx context.Context, repo *Repository, versionID, scopeType, scopeID, session string, planned time.Time) string {
	t.Helper()
	id, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: scopeType, ScopeID: scopeID,
		Session: session, PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 0, PlannedQuantity: "0", QuantityUnit: "dose",
	})
	if err != nil {
		t.Fatalf("create planned combo batch (%s/%s/%s): %v", scopeType, scopeID, session, err)
	}
	return id
}

// mkComboSOPTask inserts a real sop_task (FK-valid against the seeded skeleton SOP) and returns its id.
func mkComboSOPTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, scopeType, scopeID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO sop_tasks (tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id, priority, context)
		 VALUES ($1, $2, $3, 'vaccination', 'combo drive', 'queued', $4, $5, 'normal', '{}'::jsonb)
		 RETURNING task_id::text`,
		tenantID, skeletonSOPID, comboSopVersionID, scopeType, scopeID).Scan(&id); err != nil {
		t.Fatalf("insert combo sop task: %v", err)
	}
	return id
}

func listComboBatchIDs(rows []domain.ComboDriveBatch) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.BatchID)
	}
	sort.Strings(ids)
	return ids
}

// TestListPlannedComboBatchesOneToManyTargetsAggregatedOnce proves the join_cardinality claim:
// a combo batch with N attached obligations produces exactly ONE row whose target_ids holds all N
// distinct animals, with no fan-out duplication from the 1:N LATERAL join.
func TestListPlannedComboBatchesOneToManyTargetsAggregatedOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool) // obligation for testGoatID
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	goatB := "10000000-0000-4000-8000-0000000000b1"
	goatC := "10000000-0000-4000-8000-0000000000c1"
	seedComboGoat(t, ctx, pool, goatB)
	seedComboGoat(t, ctx, pool, goatC)
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	repo := NewRepository(pool, 5*time.Second)
	obB := insertComboObligation(t, ctx, repo, versionID, ruleID, goatB, "park", cbePark, "combo-o2m-b", due)
	obC := insertComboObligation(t, ctx, repo, versionID, ruleID, goatC, "park", cbePark, "combo-o2m-c", due)

	planned := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Session: "combo:FMD+HS", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 3, PlannedQuantity: "3", QuantityUnit: "dose",
	}, []string{obA, obB, obC})
	if err != nil {
		t.Fatalf("create combo batch: %v", err)
	}
	if attached != 3 {
		t.Fatalf("attached = %d, want 3", attached)
	}

	rows, err := repo.ListPlannedComboBatches(ctx, tenantID, planned.AddDate(0, 0, 1), 100)
	if err != nil {
		t.Fatalf("list planned combo batches: %v", err)
	}
	// Exactly ONE row for this batch (no fan-out to 3 rows).
	matches := 0
	var found *domain.ComboDriveBatch
	for i := range rows {
		if rows[i].BatchID == batchID {
			matches++
			found = &rows[i]
		}
	}
	if matches != 1 {
		t.Fatalf("combo batch appeared %d times, want exactly 1 (1:N join must not fan out)", matches)
	}
	got := append([]string(nil), found.TargetIDs...)
	sort.Strings(got)
	want := []string{goatB, goatC, testGoatID}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("target_ids = %#v, want %#v (N distinct entries, deduplicated)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("target_ids = %#v, want %#v", got, want)
		}
	}
}

// TestListPlannedComboBatchesMultiPageKeysetCoversAll proves the pagination claim: with more
// candidate batches than one page can hold, the keyset loop returns ALL of them exactly once, and a
// scope/session group that straddles a page boundary is not split (its members are contiguous in
// keyset order, so the caller's group accumulation is never truncated mid-group).
func TestListPlannedComboBatchesMultiPageKeysetCoversAll(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)

	// Five combo batches in ONE scope+session group (park/cbePark/combo:FMD+HS), on ascending
	// planned dates. With page size 2 they span three pages, so the group straddles two page
	// boundaries: a naive single-page read would drop the tail. Each batch belongs to its OWN
	// protocol version -- mirroring the real production shape, where every combo-session batch
	// comes from a DIFFERENT swept protocol version/vaccine -- so aligning several of them onto the
	// same target date does not collide with obligation_batches_unfinalized_planned_unique_idx
	// (keyed in part on protocol_version_id).
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	want := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
			TenantID: tenantID, Code: "vaccination.combo.multipage.v" + strconv.Itoa(i), Name: "ComboMultiPage",
			Category: "vaccination", Status: "draft",
		})
		if err != nil {
			t.Fatalf("definition %d: %v", i, err)
		}
		versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
			TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
			EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("version %d: %v", i, err)
		}
		id := mkPlannedComboBatch(t, ctx, repo, versionID, "park", cbePark, "combo:FMD+HS", base.AddDate(0, 0, i))
		want = append(want, id)
	}
	sort.Strings(want)

	dueBefore := base.AddDate(0, 0, 30)

	// Single-page read with the same limit would truncate at 2.
	firstPage, err := repo.ListPlannedComboBatchesKeyset(ctx, tenantID, dueBefore, nil, 2)
	if err != nil {
		t.Fatalf("keyset first page: %v", err)
	}
	if len(firstPage) != 2 {
		t.Fatalf("first page returned %d, want 2 (proves a single page truncates)", len(firstPage))
	}

	// Keyset loop must recover every batch exactly once.
	seen := make(map[string]int)
	var after *domain.ComboBatchCursor
	pages := 0
	for {
		page, err := repo.ListPlannedComboBatchesKeyset(ctx, tenantID, dueBefore, after, 2)
		if err != nil {
			t.Fatalf("keyset page %d: %v", pages, err)
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			seen[r.BatchID]++
		}
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		after = &domain.ComboBatchCursor{
			ScopeType: last.ScopeType,
			ScopeID:   last.ScopeID,
			Session:   last.Session,
			BatchID:   last.BatchID,
		}
		pages++
		if pages > 100 {
			t.Fatalf("keyset loop did not terminate")
		}
	}

	if len(seen) != 5 {
		t.Fatalf("keyset covered %d distinct batches, want 5 (none dropped at page boundaries)", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("batch %s returned %d times, want exactly 1 (no cross-page duplication)", id, n)
		}
	}
	got := make([]string, 0, len(seen))
	for id := range seen {
		got = append(got, id)
	}
	sort.Strings(got)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keyset covered %#v, want %#v (boundary-straddling group aligned whole)", got, want)
		}
	}

	// R2-06 guard: a raw keyset read recovering every row is necessary but not sufficient -- the
	// caller (AlignComboDrives) must also ASSEMBLE a group that straddles a page boundary as one
	// whole before aligning it, not align page-local fragments each too small to see the rest of
	// their own group. Run the real sweeper's AlignComboDrives over this same 5-row/1-group fixture
	// with the page size still forced to 2, and assert the group converges to exactly ONE final
	// planned_date across all 5 batches -- proving the fix assembles the whole group across all
	// three pages instead of aligning (or silently skipping) page-sized fragments.
	sweep := oblapp.NewSweeperService(repo, nil, nil)
	sweep.SetPageSize(2)
	if _, err := sweep.AlignComboDrives(ctx, tenantID, 30, dueBefore, 0, oblapp.NewSweepSession()); err != nil {
		t.Fatalf("AlignComboDrives: %v", err)
	}

	rows, err := pool.Query(ctx, `
SELECT DISTINCT planned_date FROM obligation_batches WHERE tenant_id=$1 AND batch_id = ANY($2::uuid[])`,
		tenantID, want)
	if err != nil {
		t.Fatalf("query final planned dates: %v", err)
	}
	defer rows.Close()
	var finalDates []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan final planned date: %v", err)
		}
		finalDates = append(finalDates, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate final planned dates: %v", err)
	}
	if len(finalDates) != 1 {
		t.Fatalf("distinct planned_date values across the group after alignment = %d (%v), want exactly 1 -- a group spanning >1 page must still be aligned as a whole", len(finalDates), finalDates)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND batch_id = ANY($2::uuid[])`, tenantID, want); got != 5 {
		t.Fatalf("batch row count after alignment = %d, want still 5 (no row skipped or duplicated)", got)
	}
}

// TestListPlannedComboBatchesDateShiftFiltersByPlannedDate proves the membership date predicate:
// a batch planned AFTER dueBefore is excluded; one planned on/before is included.
func TestListPlannedComboBatchesDateShiftFiltersByPlannedDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool)
	versionID := mustVersionOf(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	onOrBefore := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	included := mkPlannedComboBatch(t, ctx, repo, versionID, "park", cbePark, "combo:FMD+HS", onOrBefore)
	excluded := mkPlannedComboBatch(t, ctx, repo, versionID, "park", cbePark, "combo:PPR+Blue Tongue", after)

	// dueBefore sits between the two planned dates.
	dueBefore := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	rows, err := repo.ListPlannedComboBatches(ctx, tenantID, dueBefore, 100)
	if err != nil {
		t.Fatalf("list planned combo batches: %v", err)
	}
	ids := listComboBatchIDs(rows)
	if len(ids) != 1 || ids[0] != included {
		t.Fatalf("date-shift filter returned %#v, want only the on/before batch %s (excluded=%s)", ids, included, excluded)
	}
}

func TestListPlannedComboBatchesKeysetUsesISTBusinessDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool)
	versionID := mustVersionOf(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	plannedTodayIST := time.Date(2026, 7, 16, 0, 0, 0, 0, biztime.DefaultLocation())
	included := mkPlannedComboBatch(t, ctx, repo, versionID, "park", cbePark, "combo:FMD+HS", plannedTodayIST)
	dueBeforePreDawnIST := time.Date(2026, 7, 16, 4, 0, 0, 0, biztime.DefaultLocation())

	rows, err := repo.ListPlannedComboBatchesKeyset(ctx, tenantID, dueBeforePreDawnIST, nil, 100)
	if err != nil {
		t.Fatalf("list planned combo batches keyset: %v", err)
	}
	if ids := listComboBatchIDs(rows); len(ids) != 1 || ids[0] != included {
		t.Fatalf("IST business-date filter returned %#v, want today's batch %s", ids, included)
	}
}

// TestListPlannedComboBatchesScopeHierarchyGroupsByScope proves the scope claim: batches in
// different scope_type/scope_id are each returned under their own scope, never merged across scopes.
func TestListPlannedComboBatchesScopeHierarchyGroupsByScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool)
	versionID := mustVersionOf(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// A shed scope under the same park, plus the park scope itself.
	seedParkConsolidationShed(t, ctx, pool, parkShedA, "SCOPE-SHED-A")
	planned := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	parkBatch := mkPlannedComboBatch(t, ctx, repo, versionID, "park", cbePark, "combo:FMD+HS", planned)
	shedBatch := mkPlannedComboBatch(t, ctx, repo, versionID, "shed", parkShedA, "combo:FMD+HS", planned)

	rows, err := repo.ListPlannedComboBatches(ctx, tenantID, planned.AddDate(0, 0, 1), 100)
	if err != nil {
		t.Fatalf("list planned combo batches: %v", err)
	}
	byScope := make(map[string]domain.ComboDriveBatch)
	for _, r := range rows {
		byScope[fmt.Sprintf("%s/%s", r.ScopeType, r.ScopeID)] = r
	}
	park, okPark := byScope["park/"+cbePark]
	shed, okShed := byScope["shed/"+parkShedA]
	if !okPark || !okShed {
		t.Fatalf("scope grouping missing rows: rows=%#v", rows)
	}
	if park.BatchID != parkBatch {
		t.Fatalf("park scope row = %s, want %s", park.BatchID, parkBatch)
	}
	if shed.BatchID != shedBatch {
		t.Fatalf("shed scope row = %s, want %s", shed.BatchID, shedBatch)
	}
	// The two scopes are distinct rows, not collapsed into one.
	if park.BatchID == shed.BatchID {
		t.Fatalf("park and shed scopes must not merge into one row")
	}
}

// TestListPlannedComboBatchesStatusBucketsExcludesNonCandidates proves the membership predicate:
// only status='planned' + session LIKE 'combo:%' + sop_task_id IS NULL + no stock_reservation
// context qualify. A canceled, non-combo, sop-task-linked, or stock-reserved batch is excluded.
func TestListPlannedComboBatchesStatusBucketsExcludesNonCandidates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool)
	versionID := mustVersionOf(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	// Distinct planned dates so the same-session unfinalized-planned rows do not collide on the
	// obligation_batches_unfinalized_planned_unique_idx partial unique index (which keys on
	// tenant/version/scope/session/planned_date for status='planned' AND sop_task_id IS NULL AND no
	// stock reservation). All dates are <= dueBefore below.
	day5 := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	day6 := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	day7 := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	day8 := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	day9 := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	// The one qualifying candidate.
	qualifying := mkPlannedComboBatch(t, ctx, repo, versionID, "park", cbePark, "combo:FMD+HS", day5)

	// Excluded #1: canceled status (not covered by the planned-only partial index).
	canceled, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Session: "combo:FMD+HS", PlannedDate: &day6, Status: "canceled",
		EstimatedTargets: 0, PlannedQuantity: "0", QuantityUnit: "dose",
	})
	if err != nil {
		t.Fatalf("create canceled batch: %v", err)
	}

	// Excluded #2: non-combo session.
	nonCombo := mkPlannedComboBatch(t, ctx, repo, versionID, "park", cbePark, "shed:primary", day7)

	// Excluded #3: already has a linked SOP task (not covered by the sop_task_id IS NULL partial index).
	taskID := mkComboSOPTask(t, ctx, pool, "park", cbePark)
	withTask, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Session: "combo:FMD+HS", PlannedDate: &day8, Status: "planned",
		EstimatedTargets: 0, PlannedQuantity: "0", QuantityUnit: "dose", SopTaskID: &taskID,
	})
	if err != nil {
		t.Fatalf("create sop-task-linked batch: %v", err)
	}

	// Excluded #4: stock reservation already recorded in context.
	withReservation := mkPlannedComboBatch(t, ctx, repo, versionID, "park", cbePark, "combo:FMD+HS", day9)
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_batches SET context = jsonb_build_object('stock_reservation', jsonb_build_object('status', 'reserved'))
		 WHERE tenant_id=$1 AND batch_id=$2`, tenantID, withReservation); err != nil {
		t.Fatalf("set stock_reservation context: %v", err)
	}

	rows, err := repo.ListPlannedComboBatches(ctx, tenantID, day9.AddDate(0, 0, 1), 100)
	if err != nil {
		t.Fatalf("list planned combo batches: %v", err)
	}
	ids := listComboBatchIDs(rows)
	if len(ids) != 1 || ids[0] != qualifying {
		t.Fatalf("status-bucket filter returned %#v, want only the qualifying batch %s (excluded canceled=%s non-combo=%s with-task=%s with-reservation=%s)",
			ids, qualifying, canceled, nonCombo, withTask, withReservation)
	}
}
