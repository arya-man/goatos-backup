package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// Adversarial coverage for the Counts Breakdown census aggregate. Every test here targets a way
// a JOIN + COUNT + GROUP BY can silently report the wrong number rather than fail loudly, which
// is the whole risk class the projection-review marker exists to guard.

const countsFarm = "00000000-0000-4000-8000-000000002001"

// insertBreakdownGoat seeds one goat with every census dimension set explicitly, including the
// nullable ones, so tests can assert NULL bucketing rather than only the happy path.
func insertBreakdownGoat(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	goatID, displayID, sex, breed, lifecycle, stage string,
	parkID, shedID *string,
	mergedInto *string,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status,
  age_band, custodian_party_id, park_id, shed_id, management_stage,
  merged_into_goat_id
) VALUES (
  $1::uuid, $2::uuid, $3, 'goat', $4, $5, $6, 'adult',
  '00000000-0000-4000-8000-000000001001'::uuid, $7::uuid, $8::uuid, $9,
  $10::uuid
)`,
		goatID, countsTenant, displayID, breed, sex, lifecycle,
		parkID, shedID, stage, mergedInto); err != nil {
		t.Fatalf("seed breakdown goat %s: %v", displayID, err)
	}
}

func seedBreakdownFarm(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'farm', 'CPT-F1', 'CPT Farm 1', 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant, countsFarm); err != nil {
		t.Fatalf("seed farm: %v", err)
	}
}

func strp(s string) *string { return &s }

// goatUUID builds a distinct, valid uuid per test row without needing a generator.
func goatUUID(n int) string {
	const prefix = "00000000-0000-4000-8000-0000000090"
	return prefix + string(rune('0'+(n/10)%10)) + string(rune('0'+n%10))
}

// goatDisplayID satisfies goats_display_id_format_check (^G-[0-9]{6,}$).
func goatDisplayID(n int) string { return fmt.Sprintf("G-9%05d", n) }

func newBreakdownRepo(t *testing.T, ctx context.Context) (*Repository, *pgxpool.Pool) {
	t.Helper()
	pool := setupCountsDB(t, ctx)
	seedBreakdownFarm(t, ctx, pool)
	return NewRepository(pool, 10*time.Second), pool
}

// The two locations joins (farm, shed) are on the locations primary key. If either were ever
// rewritten to join on a non-unique column, a single goat would be counted once per matching
// location row and every number on the page would inflate. Seed extra locations that a sloppy
// join could match, then assert the head count is exactly the number of goats.
func TestCountsBreakdownOneToManyLocationJoinDoesNotInflateCounts(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// Decoy locations sharing the same code/name shape as the real farm and shed.
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES
  ('00000000-0000-4000-8000-000000002099'::uuid, $1::uuid, 'farm', 'CPT-F1-DUP', 'CPT Farm 1', 'active'),
  ('00000000-0000-4000-8000-000000004099'::uuid, $1::uuid, 'shed', 'CPT-S1-DUP', 'CPT Shed 1', 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant); err != nil {
		t.Fatalf("seed decoy locations: %v", err)
	}

	for i := 0; i < 3; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if got.TotalCount != 3 {
		t.Fatalf("total_count=%d, want 3 — a fan-out on the locations join would inflate this", got.TotalCount)
	}
	if got.TotalRows != 1 {
		t.Fatalf("total_rows=%d, want 1 (all three goats share one grain)", got.TotalRows)
	}
	if len(got.Items) != 1 || got.Items[0].Count != 3 {
		t.Fatalf("items=%+v, want a single grain row with count 3", got.Items)
	}
	// The park row is owned by the migration baseline, so assert it RESOLVED rather than pinning
	// its code; the shed is created by this test, so its label is pinned exactly.
	if got.Items[0].ParkLabel == "" {
		t.Errorf("park_label is empty — the locations join did not resolve")
	}
	if got.Items[0].ShedLabel != "CPT Shed 1" {
		t.Errorf("shed_label=%q, want CPT Shed 1", got.Items[0].ShedLabel)
	}
}

// total_count and total_rows come from window functions over the full grouped set, so they must
// be byte-identical at every page size and offset. If they were ever recomputed from the
// returned page, the footer would silently report a page subtotal as business truth. The union
// of all pages must also equal the full set exactly once — that is the deterministic-ORDER BY
// regression test, since an unstable sort lets a row appear twice or never.
func TestCountsBreakdownPaginationTotalsAndPageBoundaryAreStable(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// Six distinct grains, several with EQUAL counts so an unstable sort would be exposed.
	seed := []struct {
		breed, stage, sex string
		shed              string
		copies            int
	}{
		{"Beetal", "K1", "female", countsShedA, 2},
		{"Beetal", "K2", "female", countsShedA, 2},
		{"Malai", "K1", "male", countsShedB, 2},
		{"Malai", "K2", "male", countsShedB, 2},
		{"Sojat", "F2", "female", countsShedA, 1},
		{"Sojat", "F2", "male", countsShedB, 1},
	}
	id := 0
	wantTotal := int64(0)
	for _, s := range seed {
		for c := 0; c < s.copies; c++ {
			insertBreakdownGoat(t, ctx, pool, goatUUID(id), goatDisplayID(id),
				s.sex, s.breed, "alive", s.stage, strp(countsPark), strp(s.shed), nil)
			id++
			wantTotal++
		}
	}

	full, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 100})
	if err != nil {
		t.Fatalf("full page: %v", err)
	}
	if full.TotalCount != wantTotal {
		t.Fatalf("total_count=%d, want %d", full.TotalCount, wantTotal)
	}
	if full.TotalRows != int64(len(seed)) {
		t.Fatalf("total_rows=%d, want %d", full.TotalRows, len(seed))
	}

	// Walk the same set two rows at a time; totals must not move and rows must not repeat.
	seen := map[string]int{}
	for offset := int32(0); offset < int32(len(seed)); offset += 2 {
		page, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant, Limit: 2, Offset: offset,
		})
		if err != nil {
			t.Fatalf("page offset=%d: %v", offset, err)
		}
		if page.TotalCount != wantTotal {
			t.Errorf("offset=%d total_count=%d, want %d — totals must be page-independent", offset, page.TotalCount, wantTotal)
		}
		if page.TotalRows != int64(len(seed)) {
			t.Errorf("offset=%d total_rows=%d, want %d", offset, page.TotalRows, len(seed))
		}
		for _, row := range page.Items {
			seen[row.Breed+"|"+row.ManagementStage+"|"+row.Sex+"|"+row.ShedLabel]++
		}
	}
	if len(seen) != len(seed) {
		t.Fatalf("paged through %d distinct grains, want %d — an unstable ORDER BY drops or repeats rows", len(seen), len(seed))
	}
	for key, count := range seen {
		if count != 1 {
			t.Errorf("grain %q appeared %d times across pages, want exactly 1", key, count)
		}
	}
}

// Park and shed are independent nullable columns, deliberately NOT collapsed into a parent via
// COALESCE. Each of the four presence combinations must land in its own grain and all must be
// included in the total — a hierarchy walk would merge them and undercount the distinct rows.
// A NULL park and a NULL shed each get their own explicit bucket rather than being dropped.
func TestCountsBreakdownScopeHierarchyKeepsParkAndShedIndependent(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	combos := []struct {
		park, shed *string
	}{
		{strp(countsPark), strp(countsShedA)},
		{strp(countsPark), nil},
		{nil, strp(countsShedA)},
		{nil, nil},
	}
	for i, combo := range combos {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", "K1", combo.park, combo.shed, nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if got.TotalRows != 4 {
		t.Fatalf("total_rows=%d, want 4 distinct park/shed presence combinations", got.TotalRows)
	}
	if got.TotalCount != 4 {
		t.Fatalf("total_count=%d, want 4", got.TotalCount)
	}

	// Scoping to the park must return exactly the two rows that carry it — never the NULL-park
	// rows (which would over-report) and never zero rows (which would under-report).
	scoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, ParkID: strp(countsPark), Limit: 50,
	})
	if err != nil {
		t.Fatalf("park-scoped: %v", err)
	}
	if scoped.TotalCount != 2 {
		t.Fatalf("park-scoped total_count=%d, want 2 (the two rows carrying that park)", scoped.TotalCount)
	}

	// Scoping to a shed is independent of park: one row has the park, one does not.
	shedScoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, ShedID: strp(countsShedA), Limit: 50,
	})
	if err != nil {
		t.Fatalf("shed-scoped: %v", err)
	}
	if shedScoped.TotalCount != 2 {
		t.Fatalf("shed-scoped total_count=%d, want 2", shedScoped.TotalCount)
	}
	if shedScoped.TotalRows != 2 {
		t.Fatalf("shed-scoped total_rows=%d, want 2 — the NULL-park row must keep its own bucket", shedScoped.TotalRows)
	}
}

// Pins the exact population rule against the LIVE lifecycle_status CHECK constraint rather than
// a remembered list, and makes one consequential trade-off explicit instead of leaving it
// implicit in a WHERE clause.
//
// goats_lifecycle_status_check allows:
//
//	alive, sick, under_treatment, quarantine, icu, dead, sold, culled, transferred, lost,
//	merged, inactive
//
// CAUTION — sick / under_treatment / quarantine / icu are LIVING animals that are still
// physically in the shed. The default here is lifecycle_status='alive', which EXCLUDES them, so
// this page agrees with Counts -> Herd Register (whose Active KPI also filters to exactly
// 'alive'). That consistency is the reason for the choice, but it means the default head count
// is the strictly-healthy herd, not everything standing in the pen.
//
// The clinical animals are not lost — they are reachable through the lifecycle_status filter,
// which this test proves. If the business wants the census to mean "physically present", the
// change is one line in GetCountsBreakdown's default, and Herd Register's Active KPI should move
// with it so the two Counts tabs keep agreeing.
func TestCountsBreakdownStatusMatrixCountsOnlyLiveAnimals(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// Every value the live CHECK constraint permits, except 'merged' (covered separately below).
	statuses := []string{
		"alive", "sick", "under_treatment", "quarantine", "icu",
		"dead", "sold", "culled", "transferred", "lost", "inactive",
	}
	id := 0
	for _, status := range statuses {
		insertBreakdownGoat(t, ctx, pool, goatUUID(id), goatDisplayID(id),
			"female", "Beetal", status, "K1", strp(countsPark), strp(countsShedA), nil)
		id++
	}

	// A merged goat is a redirect, not a second animal: it must be invisible even though it is alive.
	survivor := goatUUID(id)
	insertBreakdownGoat(t, ctx, pool, survivor, goatDisplayID(90),
		"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	id++
	insertBreakdownGoat(t, ctx, pool, goatUUID(id), goatDisplayID(91),
		"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), strp(survivor))

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	// One 'alive' from the sweep + the survivor. The four clinical states are excluded by the
	// default, and the merged redirect is excluded regardless of its status.
	if got.TotalCount != 2 {
		t.Fatalf("total_count=%d, want 2 (default is strictly 'alive'; merged redirect excluded)", got.TotalCount)
	}

	// Every other status must be individually reachable and disjoint — nothing is unreachable.
	var reachable int64
	for _, status := range statuses {
		scoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant, LifecycleStatus: strp(status), Limit: 50,
		})
		if err != nil {
			t.Fatalf("status %s: %v", status, err)
		}
		want := int64(1)
		if status == "alive" {
			want = 2 // the sweep row plus the merge survivor
		}
		if scoped.TotalCount != want {
			t.Errorf("status %s total_count=%d, want %d", status, scoped.TotalCount, want)
		}
		reachable += scoped.TotalCount
	}
	// Buckets must partition the non-merged population exactly: 11 statuses + 1 survivor.
	if reachable != int64(len(statuses))+1 {
		t.Errorf("status buckets sum to %d, want %d — buckets must be disjoint and complete", reachable, len(statuses)+1)
	}

	// The clinical states specifically: alive animals the default hides. Proving they are
	// queryable is what keeps the default a trade-off rather than data loss.
	for _, clinical := range []string{"sick", "under_treatment", "quarantine", "icu"} {
		scoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant, LifecycleStatus: strp(clinical), Limit: 50,
		})
		if err != nil {
			t.Fatalf("clinical %s: %v", clinical, err)
		}
		if scoped.TotalCount != 1 {
			t.Errorf("clinical state %s is not reachable through the filter (count=%d)", clinical, scoped.TotalCount)
		}
	}
}

// The page query and the chart query share one CTE const. If those two definitions ever drift,
// the footer total and the chart bars disagree and nothing fails loudly — this is the test that
// catches it.
func TestCountsBreakdownChartSeriesReconcileToTotalCount(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	seed := []struct{ breed, stage, sex, shed string }{
		{"Beetal", "K1", "female", countsShedA},
		{"Beetal", "K2", "male", countsShedA},
		{"Malai", "K1", "female", countsShedB},
		{"Malai", "F2", "male", countsShedB},
		{"Sojat", "F2", "female", countsShedA},
	}
	for i, s := range seed {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			s.sex, s.breed, "alive", s.stage, strp(countsPark), strp(s.shed), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 2})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	sum := func(points []domain.CountsBreakdownSeriesPoint) int64 {
		var total int64
		for _, p := range points {
			total += p.Count
		}
		return total
	}

	// Deliberately requested with Limit: 2 — the series must still cover the whole set.
	for name, series := range map[string][]domain.CountsBreakdownSeriesPoint{
		"breed": got.Charts.Breed,
		"stage": got.Charts.Stage,
		"sex":   got.Charts.Sex,
		"shed":  got.Charts.Shed,
	} {
		if sum(series) != got.TotalCount {
			t.Errorf("charts.%s sums to %d, want total_count %d — the shared CTE has drifted", name, sum(series), got.TotalCount)
		}
	}
	if len(got.Items) != 2 {
		t.Fatalf("items=%d, want the requested page size of 2", len(got.Items))
	}
}

// The Kids · Adults KPI is only trustworthy if the two buckets partition the total EXACTLY.
// herd_register_is_kid returns NULL when age_band is NULL, so a bare NOT would drop those animals
// from both buckets and the KPI would quietly under-report while the headline count stayed right.
// Seed every age_band shape, including NULL, and assert the partition.
func TestCountsBreakdownKidAdultBucketsPartitionTotalExactly(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// age_band is free text; 'kid' is the only value herd_register_is_kid treats as a kid outright,
	// and a K-prefixed management_stage promotes an otherwise-unknown band to kid.
	seed := []struct {
		ageBand string
		stage   string
	}{
		{"kid", "K1"},
		{"kid", ""},
		{"adult", "F2"},
		{"adult", ""},
		{"", "K2"}, // unknown band + K-stage => kid
		{"", ""},   // unknown band, no stage => NULL from the function, must fall to adult
	}
	for i, s := range seed {
		if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status,
  age_band, custodian_party_id, park_id, shed_id, management_stage
) VALUES (
  $1::uuid, $2::uuid, $3, 'goat', 'Beetal', 'female', 'alive',
  NULLIF($4, ''), '00000000-0000-4000-8000-000000001001'::uuid, $5::uuid, $6::uuid, NULLIF($7, '')
)`, goatUUID(i), countsTenant, goatDisplayID(i), s.ageBand, countsPark, countsShedA, s.stage); err != nil {
			t.Fatalf("seed age goat %d: %v", i, err)
		}
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if got.TotalCount != int64(len(seed)) {
		t.Fatalf("total_count=%d, want %d", got.TotalCount, len(seed))
	}
	if got.TotalKids+got.TotalAdults != got.TotalCount {
		t.Fatalf("kids(%d)+adults(%d)=%d, want total_count %d — an animal fell out of both buckets",
			got.TotalKids, got.TotalAdults, got.TotalKids+got.TotalAdults, got.TotalCount)
	}
	// kid, kid, and the unknown-band-with-K2 row.
	if got.TotalKids != 3 {
		t.Errorf("total_kids=%d, want 3", got.TotalKids)
	}
	if got.TotalAdults != 3 {
		t.Errorf("total_adults=%d, want 3 (including the NULL-age-band animal)", got.TotalAdults)
	}

	// The partition must survive paging: these are whole-result window totals, not page sums.
	paged, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 1})
	if err != nil {
		t.Fatalf("paged: %v", err)
	}
	if paged.TotalKids != got.TotalKids || paged.TotalAdults != got.TotalAdults {
		t.Errorf("paged kids/adults = %d/%d, want %d/%d — KPI totals must be page-independent",
			paged.TotalKids, paged.TotalAdults, got.TotalKids, got.TotalAdults)
	}
}

// Near-duplicate raw stage labels must stay distinct. Collapsing them would hide the source
// data-quality problem this screen is meant to surface.
func TestCountsBreakdownKeepsRawStageLabelsDistinct(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	for i, stage := range []string{"ICU-Kid", "ICU-Kids", "Icu- Kid"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", stage, strp(countsPark), strp(countsShedA), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if got.TotalRows != 3 {
		t.Fatalf("total_rows=%d, want 3 — near-duplicate stage labels must NOT be merged", got.TotalRows)
	}
	if len(got.Facets.Stages) != 3 {
		t.Fatalf("facets.stages=%d, want 3", len(got.Facets.Stages))
	}
}

// Facets describe the whole selectable vocabulary, not the current selection. If they were
// filtered by the active filter, choosing a stage would collapse the stage dropdown to that one
// value and the operator could never change it back.
func TestCountsBreakdownFacetsIgnoreActiveDimensionFilters(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	for i, stage := range []string{"K1", "K2", "F2"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", stage, strp(countsPark), strp(countsShedA), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, ManagementStage: strp("K1"), Limit: 50,
	})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if got.TotalCount != 1 {
		t.Fatalf("total_count=%d, want 1 (the filter must narrow the grain rows)", got.TotalCount)
	}
	if len(got.Facets.Stages) != 3 {
		t.Fatalf("facets.stages=%d, want 3 — facets must not be narrowed by the active stage filter", len(got.Facets.Stages))
	}
}
