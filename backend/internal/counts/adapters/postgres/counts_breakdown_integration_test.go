package postgres

import (
	"context"
	"fmt"
	"reflect"
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
		TenantID: countsTenant, ParkIDs: []string{countsPark}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("park-scoped: %v", err)
	}
	if scoped.TotalCount != 2 {
		t.Fatalf("park-scoped total_count=%d, want 2 (the two rows carrying that park)", scoped.TotalCount)
	}

	// Scoping to a shed is independent of park: one row has the park, one does not.
	shedScoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, Pens: []domain.CountsBreakdownPen{{ShedID: countsShedA}}, Limit: 50,
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
		TenantID: countsTenant, ManagementStages: []string{"K1"}, Limit: 50,
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

// Lifecycle facet labels must render human-readable text, not raw DB tokens. The series_key stays
// as the raw status (for filtering) while series_label carries the display label.
func TestCountsBreakdownLifecycleFacetRendersHumanLabels(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// Seed every lifecycle status and assert each has a human-readable label.
	statuses := []struct {
		status, expectedLabel string
	}{
		{"alive", "Live"},
		{"sick", "Sick"},
		{"under_treatment", "Under Treatment"},
		{"quarantine", "Quarantine"},
		{"icu", "ICU"},
		{"dead", "Dead"},
		{"sold", "Sold"},
		{"culled", "Culled"},
		{"transferred", "Transferred"},
		{"lost", "Lost"},
		{"inactive", "Inactive"},
	}
	for i, s := range statuses {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", s.status, "K1", strp(countsPark), strp(countsShedA), nil)
	}

	// Get the facets with no filter applied — should include all statuses and all labels.
	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	if len(got.Facets.Lifecycle) != len(statuses) {
		t.Fatalf("lifecycle facets=%d, want %d", len(got.Facets.Lifecycle), len(statuses))
	}

	for _, s := range statuses {
		found := false
		for _, facet := range got.Facets.Lifecycle {
			if facet.Key == s.status {
				found = true
				if facet.Label != s.expectedLabel {
					t.Errorf("status %s: label=%q, want %q", s.status, facet.Label, s.expectedLabel)
				}
				break
			}
		}
		if !found {
			t.Errorf("status %s not found in facets: %+v", s.status, got.Facets.Lifecycle)
		}
	}
}

// The lifecycle facet is the vocabulary behind the census lifecycle filter (Live/Sold/Culled/
// Dead/Transferred). It must report every distinct lifecycle_status present in the WHOLE tenant
// herd, independent of the currently-selected lifecycle filter — otherwise selecting "Sold" would
// collapse the filter sheet to a single option and the operator could never switch back to Live.
func TestCountsBreakdownLifecycleFacetStatusMatrixIsWholeHerdVocabularyNotNarrowedByActiveFilter(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	for i, lifecycle := range []string{"alive", "alive", "sold", "dead", "culled"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", lifecycle, "F2", strp(countsPark), strp(countsShedA), nil)
	}

	// Default query (no explicit lifecycle) narrows the grain page to the live herd, per
	// GetCountsBreakdown's alive default — but the facet must still list all five statuses.
	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if got.TotalCount != 2 {
		t.Fatalf("total_count=%d, want 2 (default grain narrows to the live herd)", got.TotalCount)
	}
	lifecycleCounts := map[string]int64{}
	for _, p := range got.Facets.Lifecycle {
		lifecycleCounts[p.Key] = p.Count
	}
	if len(lifecycleCounts) != 4 {
		t.Fatalf("facets.lifecycle=%d distinct statuses, want 4 (alive/sold/dead/culled): %+v", len(lifecycleCounts), got.Facets.Lifecycle)
	}
	if lifecycleCounts["alive"] != 2 || lifecycleCounts["sold"] != 1 || lifecycleCounts["dead"] != 1 || lifecycleCounts["culled"] != 1 {
		t.Fatalf("facets.lifecycle counts=%+v, want alive=2 sold=1 dead=1 culled=1", lifecycleCounts)
	}
	// Assert human labels are present for all lifecycle statuses.
	expectedLabels := map[string]string{
		"alive":  "Live",
		"sold":   "Sold",
		"dead":   "Dead",
		"culled": "Culled",
	}
	for _, point := range got.Facets.Lifecycle {
		expectedLabel, exists := expectedLabels[point.Key]
		if !exists {
			continue
		}
		if point.Label != expectedLabel {
			t.Errorf("lifecycle status %s: label=%q, want %q", point.Key, point.Label, expectedLabel)
		}
	}

	// Explicitly selecting a non-live lifecycle must still return the FULL vocabulary, not just
	// the selected one — the same invariant TestCountsBreakdownFacetsIgnoreActiveDimensionFilters
	// proves for the stage dimension.
	scoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, LifecycleStatus: strp("sold"), Limit: 50,
	})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(sold): %v", err)
	}
	if scoped.TotalCount != 1 {
		t.Fatalf("total_count=%d, want 1 (the lifecycle filter must narrow the grain rows)", scoped.TotalCount)
	}
	if len(scoped.Facets.Lifecycle) != 4 {
		t.Fatalf("facets.lifecycle=%d, want 4 — facets must not be narrowed by the active lifecycle filter", len(scoped.Facets.Lifecycle))
	}
}

// The lifecycle branch joins NOTHING (unlike the shed/park branches, which LEFT JOIN locations),
// so a duplicate-label location cannot fan it out — but the herd-membership contract must still
// hold: sum(lifecycle facet counts) == the whole tenant herd, exactly once per animal, regardless
// of how many locations/sheds/parks exist around them.
func TestCountsBreakdownLifecycleFacetOneToManyLocationChurnDoesNotInflateCounts(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// Extra locations sharing a NAME (but distinct codes, since location_code is unique) that a
	// sloppy label join elsewhere could fan out on.
	for i := 0; i < 3; i++ {
		if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'shed', $3, 'Duplicate Shed', 'active')
ON CONFLICT (location_id) DO NOTHING`, goatUUID(90+i), countsTenant, fmt.Sprintf("DUP-%d", i)); err != nil {
			t.Fatalf("seed duplicate-label shed %d: %v", i, err)
		}
	}
	for i, lifecycle := range []string{"alive", "sold", "dead"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", lifecycle, "F2", strp(countsPark), strp(countsShedA), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	var sum int64
	for _, p := range got.Facets.Lifecycle {
		sum += p.Count
	}
	if sum != 3 {
		t.Fatalf("sum(facets.lifecycle counts)=%d, want 3 — the whole herd, exactly once per animal", sum)
	}
	// Assert human labels are correct even with decoy locations.
	for _, p := range got.Facets.Lifecycle {
		switch p.Key {
		case "alive":
			if p.Label != "Live" {
				t.Errorf("alive: label=%q, want Live", p.Label)
			}
		case "sold":
			if p.Label != "Sold" {
				t.Errorf("sold: label=%q, want Sold", p.Label)
			}
		case "dead":
			if p.Label != "Dead" {
				t.Errorf("dead: label=%q, want Dead", p.Label)
			}
		}
	}
}

// Facets are a whole-result rollup, independent of the detail page's limit/offset — proven
// generically for shed/park by the sibling tests above; this proves the SAME independence holds
// for the new lifecycle branch specifically.
func TestCountsBreakdownLifecycleFacetPaginationIsIndependentOfPageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	for i, lifecycle := range []string{"alive", "alive", "sold", "dead", "culled"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", lifecycle, "F2", strp(countsPark), strp(countsShedA), nil)
	}

	full, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(full): %v", err)
	}
	page, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(page): %v", err)
	}
	if len(page.Facets.Lifecycle) != len(full.Facets.Lifecycle) {
		t.Fatalf(
			"a 1-row page's lifecycle facet has %d entries, want the same %d as the full result — facets must not shrink with the detail page",
			len(page.Facets.Lifecycle), len(full.Facets.Lifecycle),
		)
	}
	// Assert human labels are the same across all page sizes.
	for _, p := range page.Facets.Lifecycle {
		var fullLabel string
		for _, fp := range full.Facets.Lifecycle {
			if fp.Key == p.Key {
				fullLabel = fp.Label
				break
			}
		}
		if p.Label != fullLabel {
			t.Errorf("lifecycle %s: paged label=%q, full label=%q — labels must be consistent", p.Key, p.Label, fullLabel)
		}
	}
}

// The lifecycle facet reports the WHOLE tenant herd's vocabulary, not just the scope currently
// selected by park/shed/breed — an operator who has drilled into one park must still be able to
// switch lifecycle status without first clearing the park.
func TestCountsBreakdownLifecycleFacetScopeHierarchyIgnoresParkAndShedScope(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	insertBreakdownGoat(t, ctx, pool, goatUUID(0), goatDisplayID(0),
		"female", "Beetal", "alive", "F2", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(1), goatDisplayID(1),
		"female", "Beetal", "sold", "F2", nil, nil, nil)

	scoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, ParkIDs: []string{countsPark}, Pens: []domain.CountsBreakdownPen{{ShedID: countsShedA}}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(park+shed scoped): %v", err)
	}
	lifecycleKeys := map[string]bool{}
	for _, p := range scoped.Facets.Lifecycle {
		lifecycleKeys[p.Key] = true
	}
	if !lifecycleKeys["sold"] {
		t.Fatalf(
			"facets.lifecycle=%+v is missing 'sold' — the lifecycle vocabulary must not be narrowed by an active park/shed scope",
			scoped.Facets.Lifecycle,
		)
	}
	// Assert human labels are correct even when scope is narrowed by park/shed.
	for _, p := range scoped.Facets.Lifecycle {
		switch p.Key {
		case "alive":
			if p.Label != "Live" {
				t.Errorf("alive: label=%q, want Live", p.Label)
			}
		case "sold":
			if p.Label != "Sold" {
				t.Errorf("sold: label=%q, want Sold", p.Label)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Shed facet — the Park -> Shed cascade's vocabulary
// ---------------------------------------------------------------------------

// A second park plus two sheds that share a NAME across parks, mirroring real seeded data where
// 66 of 154 shed names exist under both Coimbatore and Channapatna.
const (
	countsParkTwo       = "00000000-0000-4000-8000-000000003002"
	countsShedCastroOne = "00000000-0000-4000-8000-000000004011"
	countsShedCastroTwo = "00000000-0000-4000-8000-000000004012"
)

// seedSameNamedShedsInTwoParks creates "Castro 1" TWICE — once under each park — as two distinct
// location rows. Any facet that keys or dedupes sheds by name collapses these two physically
// separate sheds into one option and reports a merged head count.
func seedSameNamedShedsInTwoParks(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CBE', 'CBE', 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant, countsParkTwo); err != nil {
		t.Fatalf("seed second park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($3::uuid, $1::uuid, 'shed', 'CPT-CASTRO-1', 'Castro 1', $2::uuid, 'active'),
  ($5::uuid, $1::uuid, 'shed', 'CBE-CASTRO-1', 'Castro 1', $4::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`,
		countsTenant, countsPark, countsShedCastroOne, countsParkTwo, countsShedCastroTwo); err != nil {
		t.Fatalf("seed same-named sheds: %v", err)
	}
}

// shedFacetByKey indexes the shed facet by (key, park_id) — the composite the branch groups on.
func shedFacetByKey(facets []domain.CountsBreakdownShedFacet) map[[2]string]domain.CountsBreakdownShedFacet {
	out := make(map[[2]string]domain.CountsBreakdownShedFacet, len(facets))
	for _, f := range facets {
		out[[2]string{f.Key, f.ParkID}] = f
	}
	return out
}

// The core grain proof. Every animal carries exactly one shed_id and one park_id on its own row,
// so grouping on those columns must PARTITION the herd: each shed's facet count equals that
// shed's real census, and the counts sum to the whole-herd total exactly once per animal. A
// fan-out on the locations join would inflate both.
func TestCountsBreakdownShedFacetMatchesPerShedCensusAndPartitionsTheHerd(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// 4 in shed A, 3 in shed B, spread across breeds/stages/sexes so the shed rollup has to
	// aggregate across several grain rows rather than read one.
	seed := []struct {
		shed              string
		breed, stage, sex string
	}{
		{countsShedA, "Beetal", "K1", "female"},
		{countsShedA, "Beetal", "K2", "male"},
		{countsShedA, "Malai", "K1", "female"},
		{countsShedA, "Malai", "F2", "male"},
		{countsShedB, "Beetal", "K1", "female"},
		{countsShedB, "Sojat", "K2", "male"},
		{countsShedB, "Sojat", "K2", "female"},
	}
	for i, s := range seed {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			s.sex, s.breed, "alive", s.stage, strp(countsPark), strp(s.shed), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	if len(got.Facets.Sheds) != 2 {
		t.Fatalf("facets.sheds=%d, want 2 — one entry per shed holding animals, got %+v",
			len(got.Facets.Sheds), got.Facets.Sheds)
	}
	byKey := shedFacetByKey(got.Facets.Sheds)
	a, ok := byKey[[2]string{countsShedA, countsPark}]
	if !ok {
		t.Fatalf("shed A missing from facets: %+v", got.Facets.Sheds)
	}
	if a.Count != 4 {
		t.Errorf("shed A count=%d, want 4 — this is the shed's real census", a.Count)
	}
	if a.Label != "CPT Shed 1" {
		t.Errorf("shed A label=%q, want CPT Shed 1", a.Label)
	}
	b, ok := byKey[[2]string{countsShedB, countsPark}]
	if !ok {
		t.Fatalf("shed B missing from facets: %+v", got.Facets.Sheds)
	}
	if b.Count != 3 {
		t.Errorf("shed B count=%d, want 3", b.Count)
	}

	// Partition proof: the shed facet must sum to the whole herd, never more (fan-out) and never
	// less (a silent cap or a dropped bucket).
	var sum int64
	for _, f := range got.Facets.Sheds {
		sum += f.Count
	}
	if sum != got.TotalCount || sum != 7 {
		t.Errorf("sum(shed facet counts)=%d, want %d (total_count) and 7 — the shed dimension must partition the herd exactly once per animal",
			sum, got.TotalCount)
	}
}

// THE reason park_id is on the entry. Two sheds legitimately share the name "Castro 1" in
// different parks. They must surface as two entries with distinct keys AND distinct park ids, so
// a Park -> Shed cascade can tell them apart. If the facet keyed or labelled by name, an operator
// filtering Coimbatore would see one merged "Castro 1" carrying Channapatna's animals too.
func TestCountsBreakdownShedFacetSeparatesSameNamedShedsInDifferentParks(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)

	// 5 animals in the CPT "Castro 1", 2 in the CBE "Castro 1".
	for i := 0; i < 5; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedCastroOne), nil)
	}
	for i := 5; i < 7; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"male", "Malai", "alive", "K2", strp(countsParkTwo), strp(countsShedCastroTwo), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	var castro []domain.CountsBreakdownShedFacet
	for _, f := range got.Facets.Sheds {
		if f.Label == "Castro 1" {
			castro = append(castro, f)
		}
	}
	if len(castro) != 2 {
		t.Fatalf("found %d facet entries labelled 'Castro 1', want 2 SEPARATE entries: %+v",
			len(castro), got.Facets.Sheds)
	}
	if castro[0].Key == castro[1].Key {
		t.Errorf("both 'Castro 1' entries share key %s — shed identity must be the shed UUID", castro[0].Key)
	}
	if castro[0].ParkID == castro[1].ParkID {
		t.Errorf("both 'Castro 1' entries report park_id %s — the cascade cannot tell them apart", castro[0].ParkID)
	}

	byKey := shedFacetByKey(got.Facets.Sheds)
	cpt, ok := byKey[[2]string{countsShedCastroOne, countsPark}]
	if !ok {
		t.Fatalf("CPT Castro 1 missing: %+v", got.Facets.Sheds)
	}
	if cpt.Count != 5 {
		t.Errorf("CPT Castro 1 count=%d, want 5 — not merged with the CBE shed of the same name", cpt.Count)
	}
	cbe, ok := byKey[[2]string{countsShedCastroTwo, countsParkTwo}]
	if !ok {
		t.Fatalf("CBE Castro 1 missing: %+v", got.Facets.Sheds)
	}
	if cbe.Count != 2 {
		t.Errorf("CBE Castro 1 count=%d, want 2", cbe.Count)
	}
}

// The Shed occupancy chart is ONE BAR PER PEN, each named with its park (maintainer decision
// 2026-08-12, superseding the parent-shed roll-up this series used to carry).
//
// Two properties, and the second is what the old grain was hiding. Pens must not be collapsed into
// their shed; and because 66 of 154 real shed names exist in BOTH parks, every bar must say which
// park it belongs to — the chart previously printed "Gandhi", "Godel 1", "Godel 2" and "Mandela 2"
// twice each with nothing to tell the pairs apart, which is OL-1 rendered as a bar chart.
func TestCountsBreakdownShedChartIsOneBarPerPenNamedWithItsPark(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedPenChartFixture(t, ctx, pool)

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	labels := map[string]int64{}
	for _, point := range got.Charts.Shed {
		labels[point.Label] = point.Count
	}
	// The park CODES come from the response's own park facet, never hardcoded here: the shared
	// fixture inserts with ON CONFLICT DO NOTHING, so which of the two ids carries which code
	// depends on what the suite seeded first. Asserting a literal made this test fail for a reason
	// that had nothing to do with the behaviour under test.
	parkCode := map[string]string{}
	for _, park := range got.Facets.Parks {
		parkCode[park.Key] = park.Label
	}
	one, two := parkCode[countsPark], parkCode[countsParkTwo]
	if one == "" || two == "" || one == two {
		t.Fatalf("park facet did not resolve two distinct park codes: %+v", got.Facets.Parks)
	}
	want := map[string]int64{
		// Park first, then the pen composed by oploc, which is the one place this convention
		// lives: a BARE numeral joins with a space ("Castro 1 2"), a worded label joins with
		// " - " ("Castro 1 - Part 1"). See oploc.OperationalLocation.Display and its golden
		// fixture -- admin-web and Android compose the same way, so a second convention here
		// would put two different names for one pen in front of the same operator.
		one + " · Castro 1 2":        3,
		one + " · Castro 1 - Part 1": 1,
		two + " · Castro 1 2":        2,
	}
	for label, count := range want {
		if labels[label] != count {
			t.Errorf("chart bar %q = %d, want %d — series: %+v", label, labels[label], count, got.Charts.Shed)
		}
	}
	// The roll-up must be GONE: a bare shed bar means partitions were collapsed again.
	for _, bare := range []string{"Castro 1", one + " · Castro 1", two + " · Castro 1"} {
		if _, found := labels[bare]; found {
			t.Errorf("found a whole-shed bar %q — the chart must be one bar per PEN: %+v", bare, got.Charts.Shed)
		}
	}
	// The scrubbed matching key must never reach a screen: "Part 1" normalizes to "1", so a bar
	// reading "Castro 1 - 1" here would mean the key was rendered instead of the label.
	if _, found := labels[one+" · Castro 1 - 1"]; found {
		t.Errorf("rendered the normalized partition KEY instead of its label: %+v", got.Charts.Shed)
	}
	// Same-named sheds in different parks stay separate bars, and the pens still partition the herd.
	var sum int64
	for _, point := range got.Charts.Shed {
		sum += point.Count
	}
	if sum != got.TotalCount {
		t.Errorf("pen bars sum to %d, want total_count %d — the pen grain must still partition the herd", sum, got.TotalCount)
	}
}

// seedPenChartFixture puts two pens in one park's Castro 1 (3 + 1) and one pen in the other park's
// same-named shed (2), so every property below is asserted against pens that a name-keyed or
// partition-collapsing query would merge.
func seedPenChartFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedSameNamedShedsInTwoParks(t, ctx, pool)
	place := func(i int, shed, park, label string) {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", "K1", strp(park), strp(shed), nil)
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'seed')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET partition_label = EXCLUDED.partition_label`,
			countsTenant, goatUUID(i), shed, label); err != nil {
			t.Fatalf("seed goat_shed_partitions: %v", err)
		}
	}
	place(0, countsShedCastroOne, countsPark, "2")
	place(1, countsShedCastroOne, countsPark, "2")
	place(2, countsShedCastroOne, countsPark, "2")
	place(3, countsShedCastroOne, countsPark, "Part 1")
	place(4, countsShedCastroTwo, countsParkTwo, "2")
	place(5, countsShedCastroTwo, countsParkTwo, "2")
}

func penChartByKey(points []domain.CountsBreakdownSeriesPoint) map[string]int64 {
	out := make(map[string]int64, len(points))
	for _, point := range points {
		out[point.Key] = point.Count
	}
	return out
}

// COVERAGE. The pen series must PARTITION the herd: its bars sum to the same total_count the KPI
// above the chart reports. A top-N cap breaks that silently and looks fine — at the shed grain 12
// of 18 sheds was nearly the whole estate, so nobody noticed; at the pen grain 12 of 130 pens
// showed 560 of 1,670 animals under a headline reading 1,670 (maintainer report, 2026-08-12).
//
// Seeded ABOVE the old cap on purpose: with 12 or fewer pens the capped query and the uncapped one
// return the same rows, so a smaller fixture cannot tell the two apart and would pass either way.
func TestCountsBreakdownShedChartCoversEveryPen(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)

	const pens = 20
	for i := 0; i < pens; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedCastroOne), nil)
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'seed')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET partition_label = EXCLUDED.partition_label`,
			countsTenant, goatUUID(i), countsShedCastroOne, fmt.Sprintf("%d", i+1)); err != nil {
			t.Fatalf("seed goat_shed_partitions: %v", err)
		}
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 5})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if len(got.Charts.Shed) != pens {
		t.Fatalf("pen series has %d bars, want %d — a top-N cap is hiding pens", len(got.Charts.Shed), pens)
	}
	var sum int64
	for _, point := range got.Charts.Shed {
		sum += point.Count
	}
	if sum != got.TotalCount {
		t.Fatalf("pen bars sum to %d, want total_count %d — the chart must reconcile with the KPI above it", sum, got.TotalCount)
	}
	// ...and the page size must not move it: the series is a whole-result rollup, never the page.
	wide, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(limit=50): %v", err)
	}
	if !reflect.DeepEqual(penChartByKey(got.Charts.Shed), penChartByKey(wide.Charts.Shed)) {
		t.Errorf("pen series changed with page size: %+v vs %+v", got.Charts.Shed, wide.Charts.Shed)
	}
}

// CARDINALITY. The pen series joins locations TWICE — once for the shed name, once for the park
// code. Both are label-only lookups on the (tenant_id, location_id) primary key; if either were
// ever rewritten onto a non-unique column (name and location_code both repeat across parks in real
// data), every pen count would multiply by the number of matching rows while still looking
// plausible. Seed decoys that a sloppy join would match, then assert the counts are exact.
func TestCountsBreakdownShedChartOneToManyLocationJoinDoesNotFanOutPenCounts(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedPenChartFixture(t, ctx, pool)

	// Decoys share the NAME, which is the column a sloppy join would reach for (location_code is
	// unique per tenant, so it cannot be duplicated and is not the risk).
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES
  ('00000000-0000-4000-8000-000000004097'::uuid, $1::uuid, 'shed', 'DECOY-CASTRO-A', 'Castro 1', 'active'),
  ('00000000-0000-4000-8000-000000004098'::uuid, $1::uuid, 'shed', 'DECOY-CASTRO-B', 'Castro 1', 'active'),
  ('00000000-0000-4000-8000-000000003097'::uuid, $1::uuid, 'park', 'DECOY-PARK-A', 'CBE', 'active'),
  ('00000000-0000-4000-8000-000000003098'::uuid, $1::uuid, 'park', 'DECOY-PARK-B', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant); err != nil {
		t.Fatalf("seed decoy locations: %v", err)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	byKey := penChartByKey(got.Charts.Shed)
	for key, want := range map[string]int64{
		countsShedCastroOne + "#2": 3,
		countsShedCastroOne + "#1": 1, // "Part 1" normalizes to "1" in the KEY; the label keeps the word.
		countsShedCastroTwo + "#2": 2,
	} {
		if byKey[key] != want {
			t.Errorf("pen %s = %d, want %d — a label join fanned out the count: %+v", key, byKey[key], want, got.Charts.Shed)
		}
	}
	if len(got.Charts.Shed) != 3 {
		t.Errorf("pen series has %d bars, want 3 — decoy locations must not create bars: %+v", len(got.Charts.Shed), got.Charts.Shed)
	}
}

// PAGINATION. The pen series is a WHOLE-RESULT rollup capped at 12 for display; it must never be
// computed from the returned page. A series that moved with limit/offset would be the banned
// capped read-time rollup, and the chart would silently describe one page of grain rows as the
// whole estate.
func TestCountsBreakdownShedChartPaginationDoesNotMoveThePenSeries(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedPenChartFixture(t, ctx, pool)

	first, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 1})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(limit=1): %v", err)
	}
	// A page PAST the first, so the series cannot be right by accident of starting at row zero.
	second, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 1, Offset: 2})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(limit=1, offset=2): %v", err)
	}
	whole, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(limit=50): %v", err)
	}
	if !reflect.DeepEqual(penChartByKey(first.Charts.Shed), penChartByKey(whole.Charts.Shed)) {
		t.Errorf("pen series changed with page size: limit=1 %+v vs limit=50 %+v", first.Charts.Shed, whole.Charts.Shed)
	}
	if !reflect.DeepEqual(penChartByKey(second.Charts.Shed), penChartByKey(whole.Charts.Shed)) {
		t.Errorf("pen series changed with offset: offset=2 %+v vs whole %+v", second.Charts.Shed, whole.Charts.Shed)
	}
}

// SCOPE. Selecting a park must leave only that park's pens, and each remaining bar must carry the
// SAME count it had unfiltered — the chart and the filter have to describe one estate. Because both
// parks hold a shed called "Castro 1", a park-blind or name-keyed grouping would either keep the
// other park's pens or merge the two sheds' counts, and both failures look like a plausible chart.
func TestCountsBreakdownShedChartParkScopeMatchesTheFilter(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedPenChartFixture(t, ctx, pool)

	all, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	scoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, ParkIDs: []string{countsPark}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(park): %v", err)
	}

	unfiltered, filtered := penChartByKey(all.Charts.Shed), penChartByKey(scoped.Charts.Shed)
	for key, want := range map[string]int64{countsShedCastroOne + "#2": 3, countsShedCastroOne + "#1": 1} {
		if filtered[key] != want {
			t.Errorf("park-scoped pen %s = %d, want %d: %+v", key, filtered[key], want, scoped.Charts.Shed)
		}
		if unfiltered[key] != filtered[key] {
			t.Errorf("pen %s = %d unfiltered but %d park-scoped — the chart and the filter disagree", key, unfiltered[key], filtered[key])
		}
	}
	if _, leaked := filtered[countsShedCastroTwo+"#2"]; leaked {
		t.Errorf("the other park's pen survived the park filter: %+v", scoped.Charts.Shed)
	}
	var sum int64
	for _, count := range filtered {
		sum += count
	}
	if sum != scoped.TotalCount {
		t.Errorf("park-scoped pen bars sum to %d, want total_count %d", sum, scoped.TotalCount)
	}
}

// The cascade contract, asserted from the client's side: selecting a park and keeping only the
// shed entries whose park_id matches must yield exactly that park's sheds — and the count on
// each entry must equal what the equivalent park_id + shed_id request actually returns. If those
// two numbers could disagree, the dropdown would advertise a head count the filter never
// delivers.
func TestCountsBreakdownShedFacetParkScopeCascadeAgreesWithTheFilter(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)

	for i := 0; i < 5; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedCastroOne), nil)
	}
	insertBreakdownGoat(t, ctx, pool, goatUUID(5), goatDisplayID(5),
		"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	for i := 6; i < 8; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"male", "Malai", "alive", "K2", strp(countsParkTwo), strp(countsShedCastroTwo), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	// Cascade for the second park: exactly one shed, the CBE Castro 1.
	var cbeSheds []domain.CountsBreakdownShedFacet
	for _, f := range got.Facets.Sheds {
		if f.ParkID == countsParkTwo {
			cbeSheds = append(cbeSheds, f)
		}
	}
	if len(cbeSheds) != 1 || cbeSheds[0].Key != countsShedCastroTwo {
		t.Fatalf("cascade for park %s produced %+v, want only the CBE Castro 1", countsParkTwo, cbeSheds)
	}

	// Cascade for the first park: its two sheds, and NOT the identically named CBE shed.
	cptKeys := map[string]bool{}
	for _, f := range got.Facets.Sheds {
		if f.ParkID == countsPark {
			cptKeys[f.Key] = true
		}
	}
	if len(cptKeys) != 2 || !cptKeys[countsShedCastroOne] || !cptKeys[countsShedA] {
		t.Fatalf("cascade for park %s produced keys %v, want exactly the two CPT sheds", countsPark, cptKeys)
	}
	if cptKeys[countsShedCastroTwo] {
		t.Errorf("the CBE shed leaked into the CPT cascade — park_id is not scoping the list")
	}

	// Each advertised count must equal what the real filter returns.
	for _, f := range got.Facets.Sheds {
		filtered, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant, ParkIDs: []string{f.ParkID}, Pens: []domain.CountsBreakdownPen{{ShedID: f.Key}}, Limit: 50,
		})
		if err != nil {
			t.Fatalf("filtered breakdown for shed %s: %v", f.Key, err)
		}
		if filtered.TotalCount != f.Count {
			t.Errorf("shed %s (park %s): facet advertises %d but ?park_id=&shed_id= returns %d",
				f.Key, f.ParkID, f.Count, filtered.TotalCount)
		}
	}
}

// The shed facet LEFT JOINs locations for its label. That join is on the locations primary key,
// so it is a strict 1:{0,1} lookup — but a rewrite to join on name or code would match decoys and
// count each animal once per matching row. Seed decoy sheds sharing the real shed's name and code
// shape and assert the counts do not move.
func TestCountsBreakdownShedFacetOneToManyLabelJoinDoesNotFanOutCounts(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ('00000000-0000-4000-8000-000000004091'::uuid, $1::uuid, 'shed', 'CPT-S1-DUP-A', 'CPT Shed 1', $2::uuid, 'active'),
  ('00000000-0000-4000-8000-000000004092'::uuid, $1::uuid, 'shed', 'CPT-S1-DUP-B', 'CPT Shed 1', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant, countsPark); err != nil {
		t.Fatalf("seed decoy sheds: %v", err)
	}

	for i := 0; i < 3; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if len(got.Facets.Sheds) != 1 {
		t.Fatalf("facets.sheds=%d, want 1 — the two decoy sheds hold no animals and must not appear: %+v",
			len(got.Facets.Sheds), got.Facets.Sheds)
	}
	if got.Facets.Sheds[0].Count != 3 {
		t.Errorf("shed facet count=%d, want 3 — a fan-out on the label join would report 9", got.Facets.Sheds[0].Count)
	}
	if got.Facets.Sheds[0].Key != countsShedA {
		t.Errorf("shed facet key=%q, want the shed UUID %s", got.Facets.Sheds[0].Key, countsShedA)
	}
}

// ---------------------------------------------------------------------------
// Lifecycle label mapping — four new focused tests for guard compliance
// ---------------------------------------------------------------------------

// The lifecycle facet labels must render human-readable text (Live/Sold/Dead/Culled) not raw DB
// tokens (alive/sold/dead/culled). This StatusMatrix test seeds every lifecycle status,
// asserts each series_label is the human label while series_key stays raw for filtering.
func TestCountsBreakdownLifecycleLabelStatusMatrix(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	statuses := []struct {
		raw, label string
	}{
		{"alive", "Live"},
		{"sick", "Sick"},
		{"under_treatment", "Under Treatment"},
		{"quarantine", "Quarantine"},
		{"icu", "ICU"},
		{"dead", "Dead"},
		{"sold", "Sold"},
		{"culled", "Culled"},
		{"transferred", "Transferred"},
		{"lost", "Lost"},
		{"inactive", "Inactive"},
	}
	for i, s := range statuses {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", s.raw, "K1", strp(countsPark), strp(countsShedA), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	for _, s := range statuses {
		found := false
		for _, facet := range got.Facets.Lifecycle {
			if facet.Key == s.raw {
				found = true
				if facet.Label != s.label {
					t.Errorf("status %s: key=%q, label=%q, want label=%q (key must stay raw, label must be human)",
						s.raw, facet.Key, facet.Label, s.label)
				}
				break
			}
		}
		if !found {
			t.Errorf("status %s not found in facets", s.raw)
		}
	}
}

// Lifecycle facet labels must not be inflated by location joins. The lifecycle branch joins
// NOTHING (unlike shed/park branches which LEFT JOIN locations), but this OneToMany test seeds
// extra locations and asserts labels stay correct and counts match the animal census.
func TestCountsBreakdownLifecycleLabelOneToMany(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	// Decoy locations that could fan out if a sloppy join existed.
	for i := 0; i < 3; i++ {
		if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'shed', $3, 'Decoy Shed', 'active')
ON CONFLICT (location_id) DO NOTHING`, goatUUID(90+i), countsTenant, fmt.Sprintf("DECOY-%d", i)); err != nil {
			t.Fatalf("seed decoy: %v", err)
		}
	}

	for i, lifecycle := range []string{"alive", "sold", "dead"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", lifecycle, "F2", strp(countsPark), strp(countsShedA), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	// Count must be exactly 3, not inflated by decoys.
	var sum int64
	for _, p := range got.Facets.Lifecycle {
		sum += p.Count
	}
	if sum != 3 {
		t.Errorf("lifecycle facet count sum=%d, want 3 — decoy locations must not fan out", sum)
	}

	// Labels must be correct.
	expectedLabels := map[string]string{"alive": "Live", "sold": "Sold", "dead": "Dead"}
	for _, p := range got.Facets.Lifecycle {
		if expected, ok := expectedLabels[p.Key]; ok && p.Label != expected {
			t.Errorf("key=%q: label=%q, want=%q", p.Key, p.Label, expected)
		}
	}
}

// Lifecycle facet labels must survive pagination boundaries. This Pagination test compares
// the full page vs a 1-row page and asserts the lifecycle facet labels stay identical.
func TestCountsBreakdownLifecycleLabelPagination(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	for i, lifecycle := range []string{"alive", "alive", "sold", "dead", "culled"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", lifecycle, "F2", strp(countsPark), strp(countsShedA), nil)
	}

	full, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 100})
	if err != nil {
		t.Fatalf("full page: %v", err)
	}

	paged, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("paged: %v", err)
	}

	// Facet contents must be identical regardless of page boundary.
	if len(paged.Facets.Lifecycle) != len(full.Facets.Lifecycle) {
		t.Fatalf("paged lifecycle facet len=%d, full len=%d — must be independent of page",
			len(paged.Facets.Lifecycle), len(full.Facets.Lifecycle))
	}

	for _, p := range paged.Facets.Lifecycle {
		var fullEntry *domain.CountsBreakdownSeriesPoint
		for i := range full.Facets.Lifecycle {
			if full.Facets.Lifecycle[i].Key == p.Key {
				fullEntry = &full.Facets.Lifecycle[i]
				break
			}
		}
		if fullEntry == nil {
			t.Fatalf("paged lifecycle %s not in full result", p.Key)
		}
		if p.Label != fullEntry.Label {
			t.Errorf("paged label=%q, full label=%q — lifecycle labels must match", p.Label, fullEntry.Label)
		}
		if p.Count != fullEntry.Count {
			t.Errorf("paged count=%d, full count=%d — lifecycle counts must match", p.Count, fullEntry.Count)
		}
	}
}

// Lifecycle facet labels must survive scope narrowing and must never be narrowed by park/shed
// filters. This ScopeHierarchy test narrows the grain scope to park+shed but asserts the
// lifecycle facet still reports the WHOLE vocabulary with correct human labels.
func TestCountsBreakdownLifecycleLabelScopeHierarchy(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	insertBreakdownGoat(t, ctx, pool, goatUUID(0), goatDisplayID(0),
		"female", "Beetal", "alive", "F2", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(1), goatDisplayID(1),
		"female", "Beetal", "sold", "F2", nil, nil, nil)

	// Unscoped query — whole vocabulary.
	unscoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("unscoped: %v", err)
	}

	// Scoped to park+shed — grain is narrowed but facet is not.
	scoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, ParkIDs: []string{countsPark}, Pens: []domain.CountsBreakdownPen{{ShedID: countsShedA}}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("scoped: %v", err)
	}

	if len(scoped.Facets.Lifecycle) != len(unscoped.Facets.Lifecycle) {
		t.Errorf("scoped lifecycle facet len=%d, unscoped len=%d — facet must not be narrowed by park/shed scope",
			len(scoped.Facets.Lifecycle), len(unscoped.Facets.Lifecycle))
	}

	// Labels must be correct in both.
	for _, facet := range scoped.Facets.Lifecycle {
		switch facet.Key {
		case "alive":
			if facet.Label != "Live" {
				t.Errorf("scoped alive: label=%q, want Live", facet.Label)
			}
		case "sold":
			if facet.Label != "Sold" {
				t.Errorf("scoped sold: label=%q, want Sold", facet.Label)
			}
		}
	}
}

// The shed facet's membership must track the requested lifecycle bucket across the WHOLE status
// matrix, not just the 'alive' default. Sweep every value the lifecycle CHECK constraint permits,
// each in its own shed, then assert three things per status: the shed vocabulary contains exactly
// the shed holding that status, its count is right, and no other status leaks in. A merged animal
// is also seeded — it is a redirect, not a second animal, so it must stay invisible in the shed
// vocabulary even though it is alive and sits in a real shed.
func TestCountsBreakdownShedFacetStatusMatrixTracksTheRequestedLifecycleBucket(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)

	// Every value the live CHECK constraint permits, except 'merged' (covered below). Statuses
	// are spread over four sheds so a status bucket that leaked would surface as an extra shed
	// entry rather than only as a wrong number.
	sheds := []struct{ shed, park string }{
		{countsShedA, countsPark},
		{countsShedB, countsPark},
		{countsShedCastroOne, countsPark},
		{countsShedCastroTwo, countsParkTwo},
	}
	statuses := []string{
		"alive", "sick", "under_treatment", "quarantine", "icu",
		"dead", "sold", "culled", "transferred", "lost", "inactive",
	}
	shedForStatus := map[string]struct{ shed, park string }{}
	id := 0
	for i, status := range statuses {
		placement := sheds[i%len(sheds)]
		shedForStatus[status] = placement
		insertBreakdownGoat(t, ctx, pool, goatUUID(id), goatDisplayID(id),
			"female", "Beetal", status, "K1", strp(placement.park), strp(placement.shed), nil)
		id++
	}

	// A merged goat in its OWN shed: if merge exclusion were missing, that shed would appear in
	// the vocabulary as a phantom option the operator could select and find empty.
	survivor := goatUUID(id)
	insertBreakdownGoat(t, ctx, pool, survivor, goatDisplayID(80),
		"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	id++
	insertBreakdownGoat(t, ctx, pool, goatUUID(id), goatDisplayID(81),
		"female", "Beetal", "alive", "K1", strp(countsParkTwo), strp(countsShedCastroTwo), strp(survivor))

	// Default bucket is strictly 'alive': one swept animal plus the merge survivor, both in shed A.
	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if len(got.Facets.Sheds) != 1 {
		t.Fatalf("default facets.sheds=%d, want 1 — only shed A holds live unmerged animals: %+v",
			len(got.Facets.Sheds), got.Facets.Sheds)
	}
	if got.Facets.Sheds[0].Key != countsShedA || got.Facets.Sheds[0].Count != 2 {
		t.Fatalf("default shed facet=%+v, want shed A with count 2 (swept 'alive' + survivor); the merged twin must not add a shed",
			got.Facets.Sheds[0])
	}

	// Every status must be individually reachable, and must select ONLY its own shed.
	for _, status := range statuses {
		scoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant, LifecycleStatus: strp(status), Limit: 50,
		})
		if err != nil {
			t.Fatalf("status %s: %v", status, err)
		}
		want := int64(1)
		if status == "alive" {
			want = 2 // the swept row plus the merge survivor, both in shed A
		}
		if len(scoped.Facets.Sheds) != 1 {
			t.Errorf("status %s: facets.sheds=%d, want 1 — another status bucket leaked in: %+v",
				status, len(scoped.Facets.Sheds), scoped.Facets.Sheds)
			continue
		}
		placement := shedForStatus[status]
		entry := scoped.Facets.Sheds[0]
		if entry.Key != placement.shed || entry.ParkID != placement.park {
			t.Errorf("status %s: shed facet=%+v, want shed %s in park %s",
				status, entry, placement.shed, placement.park)
		}
		if entry.Count != want {
			t.Errorf("status %s: shed facet count=%d, want %d", status, entry.Count, want)
		}
		if entry.Count != scoped.TotalCount {
			t.Errorf("status %s: shed facet count=%d but total_count=%d — the facet must partition the bucket",
				status, entry.Count, scoped.TotalCount)
		}
	}
}

// Facets are a whole-result rollup, so the shed vocabulary must be byte-identical at every page
// size. If it were ever derived from the returned page, the Shed dropdown would gain and lose
// options as the operator paged the table underneath it.
func TestCountsBreakdownShedFacetPaginationAndPageBoundaryDoNotMoveTheVocabulary(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)

	sheds := []struct {
		shed, park string
	}{
		{countsShedA, countsPark},
		{countsShedB, countsPark},
		{countsShedCastroOne, countsPark},
		{countsShedCastroTwo, countsParkTwo},
	}
	n := 0
	for _, s := range sheds {
		for i := 0; i < 3; i++ {
			insertBreakdownGoat(t, ctx, pool, goatUUID(n), goatDisplayID(n),
				"female", "Beetal", "alive", fmt.Sprintf("K%d", i+1), strp(s.park), strp(s.shed), nil)
			n++
		}
	}

	full, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 100})
	if err != nil {
		t.Fatalf("full page: %v", err)
	}
	if len(full.Facets.Sheds) != 4 {
		t.Fatalf("facets.sheds=%d, want 4: %+v", len(full.Facets.Sheds), full.Facets.Sheds)
	}

	for _, page := range []struct {
		limit, offset int32
	}{{1, 0}, {1, 5}, {2, 3}, {100, 0}} {
		got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant, Limit: page.limit, Offset: page.offset,
		})
		if err != nil {
			t.Fatalf("limit=%d offset=%d: %v", page.limit, page.offset, err)
		}
		if len(got.Facets.Sheds) != len(full.Facets.Sheds) {
			t.Fatalf("limit=%d offset=%d: facets.sheds=%d, want %d — the shed vocabulary must not follow the page",
				page.limit, page.offset, len(got.Facets.Sheds), len(full.Facets.Sheds))
		}
		for i := range got.Facets.Sheds {
			if got.Facets.Sheds[i] != full.Facets.Sheds[i] {
				t.Errorf("limit=%d offset=%d: shed facet[%d]=%+v, want %+v",
					page.limit, page.offset, i, got.Facets.Sheds[i], full.Facets.Sheds[i])
			}
		}
	}
}

// The shed branch must share its siblings' scope semantics exactly: facets describe the whole
// selectable vocabulary, NOT the current selection. If an active park or shed filter narrowed
// the shed list, picking a shed would collapse the dropdown to that one shed and the operator
// could never switch parks. Asserted alongside the parks branch so the two cannot drift.
func TestCountsBreakdownShedFacetIsWholeResultRollupLikeParksBranch(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)

	insertBreakdownGoat(t, ctx, pool, goatUUID(0), goatDisplayID(0),
		"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(1), goatDisplayID(1),
		"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedCastroOne), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(2), goatDisplayID(2),
		"male", "Malai", "alive", "K2", strp(countsParkTwo), strp(countsShedCastroTwo), nil)

	unfiltered, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("unfiltered: %v", err)
	}

	filtered, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, ParkIDs: []string{countsParkTwo}, Pens: []domain.CountsBreakdownPen{{ShedID: countsShedCastroTwo}}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if filtered.TotalCount != 1 {
		t.Fatalf("total_count=%d, want 1 — the park/shed filters must narrow the GRAIN rows", filtered.TotalCount)
	}

	if len(filtered.Facets.Sheds) != len(unfiltered.Facets.Sheds) || len(filtered.Facets.Sheds) != 3 {
		t.Fatalf("filtered facets.sheds=%d, unfiltered=%d, want 3 both — the shed vocabulary must survive an active filter",
			len(filtered.Facets.Sheds), len(unfiltered.Facets.Sheds))
	}
	for i := range filtered.Facets.Sheds {
		if filtered.Facets.Sheds[i] != unfiltered.Facets.Sheds[i] {
			t.Errorf("shed facet[%d] under filter=%+v, unfiltered=%+v", i, filtered.Facets.Sheds[i], unfiltered.Facets.Sheds[i])
		}
	}
	// Same rule, sibling branch — the two must behave identically.
	if len(filtered.Facets.Parks) != len(unfiltered.Facets.Parks) || len(filtered.Facets.Parks) != 2 {
		t.Errorf("filtered facets.parks=%d, unfiltered=%d, want 2 both",
			len(filtered.Facets.Parks), len(unfiltered.Facets.Parks))
	}
}

// An animal with no shed is a real operational state (unplaced stock). It belongs in its own
// explicit ” bucket, exactly like the parks branch's null bucket, rather than being dropped —
// dropping it would make the shed facet stop summing to the herd total.
func TestCountsBreakdownShedFacetKeepsUnplacedAnimalsInTheirOwnBucket(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	insertBreakdownGoat(t, ctx, pool, goatUUID(0), goatDisplayID(0),
		"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(1), goatDisplayID(1),
		"female", "Beetal", "alive", "K1", strp(countsPark), nil, nil)

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	byKey := shedFacetByKey(got.Facets.Sheds)
	unplaced, ok := byKey[[2]string{"", countsPark}]
	if !ok {
		t.Fatalf("no '' bucket for the shedless animal: %+v", got.Facets.Sheds)
	}
	if unplaced.Count != 1 {
		t.Errorf("unplaced bucket count=%d, want 1", unplaced.Count)
	}
	if unplaced.ParkID != countsPark {
		t.Errorf("unplaced bucket park_id=%q, want %s — the park is still known", unplaced.ParkID, countsPark)
	}

	var sum int64
	for _, f := range got.Facets.Sheds {
		sum += f.Count
	}
	if sum != got.TotalCount {
		t.Errorf("sum(shed facet counts)=%d, want total_count=%d — the '' bucket must keep the partition complete",
			sum, got.TotalCount)
	}
}

// MULTI-VALUE FILTERS (multiselect, 2026-09-03). Each dimension is a SET: two selected stages
// must count animals in EITHER stage (OR within a dimension), while dimensions still AND
// together. An empty set means "no filter", never "match nothing".
func TestCountsBreakdownMultiValueFiltersUnionWithinADimension(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	insertBreakdownGoat(t, ctx, pool, goatUUID(0), goatDisplayID(0),
		"female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(1), goatDisplayID(1),
		"male", "Osmanabadi", "alive", "K2", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(2), goatDisplayID(2),
		"female", "Sirohi", "alive", "Buck", strp(countsPark), strp(countsShedB), nil)

	twoStages, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, ManagementStages: []string{"K1", "K2"}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("two stages: %v", err)
	}
	if twoStages.TotalCount != 2 {
		t.Errorf("two-stage filter total_count=%d, want 2 (K1 OR K2)", twoStages.TotalCount)
	}

	twoBreeds, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, Breeds: []string{"Beetal", "Sirohi"}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("two breeds: %v", err)
	}
	if twoBreeds.TotalCount != 2 {
		t.Errorf("two-breed filter total_count=%d, want 2 (Beetal OR Sirohi)", twoBreeds.TotalCount)
	}

	// Dimensions still AND: (K1 OR K2) AND female matches only the K1 female.
	crossed, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, ManagementStages: []string{"K1", "K2"}, Sexes: []string{"female"}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("crossed: %v", err)
	}
	if crossed.TotalCount != 1 {
		t.Errorf("crossed filter total_count=%d, want 1 ((K1|K2) AND female)", crossed.TotalCount)
	}

	// Both sexes selected reads the same as no sex filter at all.
	bothSexes, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, Sexes: []string{"female", "male"}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("both sexes: %v", err)
	}
	if bothSexes.TotalCount != 3 {
		t.Errorf("both-sex filter total_count=%d, want 3", bothSexes.TotalCount)
	}
}

// The pen filter is a set of (shed, partition) pairs: selecting Castro 1 pen 2 in one park plus
// the OTHER park's whole same-named Castro must count exactly those animals — a name-keyed or
// pair-collapsed predicate would leak the unselected "Part 1" pen in.
func TestCountsBreakdownMultiPenFilterSelectsExactPens(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedPenChartFixture(t, ctx, pool) // Castro1(park1): pen "2" x3, "Part 1" x1; Castro(park2): pen "2" x2

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant,
		Pens: []domain.CountsBreakdownPen{
			{ShedID: countsShedCastroOne, PartitionLabel: "2"},
			{ShedID: countsShedCastroTwo}, // whole shed, both its pens
		},
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if got.TotalCount != 5 {
		t.Errorf("pen-set filter total_count=%d, want 5 (Castro1 pen 2 x3 + whole Castro Two x2)", got.TotalCount)
	}

	// 'Part 2' and '2' are the same pen on the normalized key (oploc.SamePartition).
	normalized, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant,
		Pens:     []domain.CountsBreakdownPen{{ShedID: countsShedCastroOne, PartitionLabel: "Part 2"}},
		Limit:    50,
	})
	if err != nil {
		t.Fatalf("normalized pen: %v", err)
	}
	if normalized.TotalCount != 3 {
		t.Errorf("normalized pen filter total_count=%d, want 3", normalized.TotalCount)
	}
}

// A WILDCARD-SHED pen (empty ShedID + a partition) narrows the whole query to that partition
// across every shed — the CEO assistant's partition-named-without-its-shed scope, which the
// retired scalar PartitionLabel filter used to honor at query level. TotalCount must move with
// it, because the review finding this pins was exactly "unfiltered totals over filtered rows".
// An entry empty on both halves states no pen and must be dropped, never bound as match-all.
func TestCountsBreakdownWildcardShedPenFiltersPartitionAcrossSheds(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedPenChartFixture(t, ctx, pool) // Castro1(park1): pen "2" x3, "Part 1" x1; Castro(park2): pen "2" x2

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant,
		Pens:     []domain.CountsBreakdownPen{{PartitionLabel: "Part 2"}},
		Limit:    50,
	})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if got.TotalCount != 5 {
		t.Errorf("wildcard-shed pen total_count=%d, want 5 (pen 2 in both Castros)", got.TotalCount)
	}

	empty, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant,
		Pens:     []domain.CountsBreakdownPen{{}},
		Limit:    50,
	})
	if err != nil {
		t.Fatalf("empty pen entry: %v", err)
	}
	if empty.TotalCount != 6 {
		t.Errorf("an entry empty on both halves must be dropped (no filter): total_count=%d, want 6", empty.TotalCount)
	}
}

// An ALL-EMPTY subdivided shed must still carry a PARENT facet row (count 0, bare shed label,
// no partition). The parent-aggregate branch derives from goats, so before this branch existed a
// shed whose every pen was empty surfaced ONLY composed pen labels ("Yashoda - Part 1") — and the
// web shed dropdown, which heads a subdivided shed's optgroup with the parent row's label, fell
// back to the first pen row and rendered the pen label as the shed heading (review finding).
// An empty UNDIVIDED shed must stay absent, exactly as before.
func TestCountsBreakdownAllEmptyShedStillCarriesItsParentFacetRow(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	const emptyShed = "00000000-0000-4000-8000-000000004021"
	const emptyUndividedShed = "00000000-0000-4000-8000-000000004022"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($2::uuid, $1::uuid, 'shed', 'CPT-YASHODA', 'Yashoda', $4::uuid, 'active'),
  ($3::uuid, $1::uuid, 'shed', 'CPT-BARE', 'Bare', $4::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant, emptyShed, emptyUndividedShed, countsPark); err != nil {
		t.Fatalf("seed empty sheds: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES
  ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual'),
  ($1::uuid, $2::uuid, 'Part 2', '2', 'active', 'manual')
ON CONFLICT DO NOTHING`, countsTenant, emptyShed); err != nil {
		t.Fatalf("seed catalog partitions: %v", err)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	var parent *domain.CountsBreakdownShedFacet
	pens := 0
	for i, f := range got.Facets.Sheds {
		if f.ShedID == emptyUndividedShed {
			t.Fatalf("an empty UNDIVIDED shed must stay absent from the facet, got %+v", f)
		}
		if f.ShedID != emptyShed {
			continue
		}
		if f.PartitionLabel == "" {
			parent = &got.Facets.Sheds[i]
		} else {
			pens++
		}
	}
	if parent == nil {
		t.Fatalf("all-empty subdivided shed has no parent facet row; the dropdown group heading falls back to a pen label. facets=%+v", got.Facets.Sheds)
	}
	if parent.Label != "Yashoda" || parent.Count != 0 || parent.Key != emptyShed {
		t.Fatalf("parent row=%+v, want bare label 'Yashoda', count 0, bare shed key", *parent)
	}
	if parent.ParkID != countsPark {
		t.Fatalf("parent row park=%q, want %q — it must group under the same park key as its pen rows", parent.ParkID, countsPark)
	}
	if pens != 2 {
		t.Fatalf("expected the 2 empty catalog pens beside the parent row, got %d", pens)
	}
}
