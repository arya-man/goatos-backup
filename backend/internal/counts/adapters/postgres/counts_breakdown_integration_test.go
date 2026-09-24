package postgres

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/herdstage"
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

func seedBreakdownGoatIdentifier(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, typ, value string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identifiers (
  tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
  scope_key, is_primary_for_goat, status, valid_from, normalizer_version
) VALUES (
  $1::uuid, $2::uuid, $3, $4, lower($4), $4, $3 = 'animal_identifier_1', 'active', now(), 'v1'
)`, countsTenant, goatID, typ, value); err != nil {
		t.Fatalf("seed goat identifier %s %s: %v", goatID, value, err)
	}
}

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

// The stage x sex cross-tab must agree with the plain stage series bar for bar, and each bar's
// sex segments must add up to that bar. Two series describing the same population that disagree
// about a head count is exactly the cross-surface defect the repo bans; here both live in one
// response, so the disagreement would be visible on one screen.
//
// Requested with Limit: 1 on purpose — both series are whole-result rollups and must ignore the
// page size.
func TestCountsBreakdownStageSexCrossTabMatchesTheStageSeries(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	seed := []struct{ breed, stage, sex, shed string }{
		{"Beetal", "K1", "female", countsShedA},
		{"Beetal", "K1", "female", countsShedB},
		{"Beetal", "K1", "male", countsShedA},
		{"Malai", "Mother", "female", countsShedB},
		{"Malai", "Mother", "female", countsShedA},
		{"Sojat", "Buck", "male", countsShedA},
		// No stage recorded. The unrecorded bucket is a real bar, not a dropped row — hiding it
		// would shrink a series the KPI above still counts in full.
		{"Sojat", "", "female", countsShedB},
	}
	for i, s := range seed {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			s.sex, s.breed, "alive", s.stage, strp(countsPark), strp(s.shed), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 1})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	stageBars := map[string]int64{}
	for _, point := range got.Charts.Stage {
		stageBars[point.Key] = point.Count
	}
	if len(got.Charts.StageSex) != len(stageBars) {
		t.Fatalf("stage_sex has %d bars, stage has %d — the two series describe different populations",
			len(got.Charts.StageSex), len(stageBars))
	}

	var crossTotal int64
	for _, point := range got.Charts.StageSex {
		want, ok := stageBars[point.Key]
		if !ok {
			t.Fatalf("stage_sex carries stage %q, which the stage series does not", point.Key)
		}
		if point.Count != want {
			t.Errorf("stage %q: stage_sex count=%d, stage series=%d", point.Key, point.Count, want)
		}
		if point.Female+point.Male+point.Other != point.Count {
			t.Errorf("stage %q: female+male+other=%d, want count=%d — a bar's segments must fill it",
				point.Key, point.Female+point.Male+point.Other, point.Count)
		}
		crossTotal += point.Count
	}
	if crossTotal != got.TotalCount {
		t.Errorf("stage_sex sums to %d, want total_count %d", crossTotal, got.TotalCount)
	}

	byStage := map[string]domain.CountsBreakdownStageSexPoint{}
	for _, point := range got.Charts.StageSex {
		byStage[point.Key] = point
	}
	// Mother is female by definition and Buck male by definition — the whole reason this is a
	// cross-tab rather than a stage total read beside a herd-wide sex ratio.
	if p := byStage["Mother"]; p.Female != 2 || p.Male != 0 {
		t.Errorf("Mother: female=%d male=%d, want 2/0", p.Female, p.Male)
	}
	if p := byStage["Buck"]; p.Male != 1 || p.Female != 0 {
		t.Errorf("Buck: male=%d female=%d, want 1/0", p.Male, p.Female)
	}
	if p := byStage["K1"]; p.Female != 2 || p.Male != 1 {
		t.Errorf("K1: female=%d male=%d, want 2/1", p.Female, p.Male)
	}
	if p, ok := byStage[""]; !ok || p.Count != 1 {
		t.Errorf("the unrecorded-stage bucket is missing or wrong: %+v", p)
	}

	// Biggest stage first, so the chart does not reshuffle between two identical reads.
	for i := 1; i < len(got.Charts.StageSex); i++ {
		if got.Charts.StageSex[i-1].Count < got.Charts.StageSex[i].Count {
			t.Fatalf("stage_sex is not ordered by head count: %+v", got.Charts.StageSex)
		}
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
func TestCountsBreakdownMultipleDimensionsPageBoundaryParkScopeStatusBucketsUnionWithinADimension(t *testing.T) {
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
func TestCountsBreakdownOneToManyWildcardShedPenFiltersPartitionAcrossSheds(t *testing.T) {
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

// The pen page is a ROLLUP of the grain page, never a second count. For the same filters the two
// must agree animal for animal: every pen's count is the sum of the grain rows nested under it,
// its stage/breed/sex chips re-roll those same rows, total_rows counts pens instead of
// combinations, and total_count/kids/adults are byte-identical to the grain page's.
func TestCountsBreakdownPenPageOneToManyGrainRollupReconcilesToTheGrainPage(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedPenChartFixture(t, ctx, pool) // Castro1(park1): pen "2" x3, "Part 1" x1; Castro(park2): pen "2" x2

	grain, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 100})
	if err != nil {
		t.Fatalf("grain page: %v", err)
	}
	pens, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, GroupByPen: true, Limit: 100})
	if err != nil {
		t.Fatalf("pen page: %v", err)
	}

	if len(pens.Items) != 0 {
		t.Errorf("pen page must not also carry the grain page: items=%d", len(pens.Items))
	}
	if len(grain.Pens) != 0 {
		t.Errorf("grain page must not also carry the pen page: pens=%d", len(grain.Pens))
	}
	if pens.TotalRows != 3 {
		t.Errorf("pen page total_rows=%d, want 3 pens", pens.TotalRows)
	}
	if pens.TotalCount != grain.TotalCount || pens.TotalKids != grain.TotalKids || pens.TotalAdults != grain.TotalAdults {
		t.Errorf("pen page totals (%d/%d/%d) differ from grain page totals (%d/%d/%d)",
			pens.TotalCount, pens.TotalKids, pens.TotalAdults, grain.TotalCount, grain.TotalKids, grain.TotalAdults)
	}

	// Rebuild the grain set from the nested rows and compare it to the grain page as a multiset.
	type grainKey struct{ shed, partition, stage, breed, sex string }
	fromGrain := map[grainKey]int64{}
	for _, row := range grain.Items {
		fromGrain[grainKey{ptrValue(row.ShedID), row.PartitionLabel, row.ManagementStage, row.Breed, row.Sex}] += row.Count
	}
	fromPens := map[grainKey]int64{}
	var penSum int64
	for _, pen := range pens.Pens {
		var rowsSum int64
		for _, row := range pen.Rows {
			if ptrValue(row.ShedID) != ptrValue(pen.ShedID) || row.PartitionLabel != pen.PartitionLabel || row.OperationalLocationDisplay != pen.OperationalLocationDisplay {
				t.Errorf("nested row of %q carries a different location: %+v", pen.OperationalLocationDisplay, row)
			}
			fromPens[grainKey{ptrValue(row.ShedID), row.PartitionLabel, row.ManagementStage, row.Breed, row.Sex}] += row.Count
			rowsSum += row.Count
		}
		if rowsSum != pen.Count {
			t.Errorf("pen %q count=%d but its rows sum to %d", pen.OperationalLocationDisplay, pen.Count, rowsSum)
		}
		if pen.KidCount+pen.AdultCount != pen.Count {
			t.Errorf("pen %q kids+adults=%d, want %d", pen.OperationalLocationDisplay, pen.KidCount+pen.AdultCount, pen.Count)
		}
		for name, series := range map[string][]domain.CountsBreakdownSeriesPoint{"stages": pen.Stages, "breeds": pen.Breeds, "sexes": pen.Sexes} {
			var s int64
			for i, point := range series {
				s += point.Count
				if i > 0 && series[i-1].Count < point.Count {
					t.Errorf("pen %q %s not largest-first: %+v", pen.OperationalLocationDisplay, name, series)
				}
			}
			if s != pen.Count {
				t.Errorf("pen %q %s sum to %d, want %d", pen.OperationalLocationDisplay, name, s, pen.Count)
			}
		}
		penSum += pen.Count
	}
	if penSum != pens.TotalCount {
		t.Errorf("pens on the page sum to %d, total_count=%d (page of 100 must hold every pen)", penSum, pens.TotalCount)
	}
	if !reflect.DeepEqual(fromGrain, fromPens) {
		t.Errorf("grain set differs\n grain page: %v\n from pens:  %v", fromGrain, fromPens)
	}

	// Pens page largest-first, and the partitioned display is the canonical one, never a sentinel.
	for i, pen := range pens.Pens {
		if i > 0 && pens.Pens[i-1].Count < pen.Count {
			t.Errorf("pens not ordered largest-first at %d: %+v", i, pens.Pens)
		}
		if pen.PartitionLabel == "whole" || pen.OperationalLocationDisplay == "" {
			t.Errorf("pen %d leaks a sentinel or blank display: %+v", i, pen)
		}
	}
	if pens.Pens[0].OperationalLocationDisplay != "Castro 1 - Part 2" && pens.Pens[0].OperationalLocationDisplay != "Castro 1 2" {
		// Whichever convention the fixture stores, the largest pen (3 animals) is Castro 1's pen "2".
		if pens.Pens[0].Count != 3 {
			t.Errorf("largest pen should hold 3 animals: %+v", pens.Pens[0])
		}
	}
}

// The pen page honours the same filters as the grain page: a filter that narrows a pen to a subset
// of its animals narrows the pen's line and its nested rows alike, and a pen with no matching
// animal disappears rather than showing a zero line.
func TestCountsBreakdownPenPagePaginationAndPageBoundaryHonourFilters(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedPenChartFixture(t, ctx, pool)

	// Filter to one pen: exactly one line, whose count equals the grain page's total for the same pen.
	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID:   countsTenant,
		GroupByPen: true,
		Pens:       []domain.CountsBreakdownPen{{ShedID: countsShedCastroOne, PartitionLabel: "Part 2"}},
		Limit:      50,
	})
	if err != nil {
		t.Fatalf("pen filter: %v", err)
	}
	if got.TotalRows != 1 || len(got.Pens) != 1 {
		t.Fatalf("one pen selected, got total_rows=%d pens=%d", got.TotalRows, len(got.Pens))
	}
	if got.Pens[0].Count != 3 || got.TotalCount != 3 {
		t.Errorf("Castro 1 pen 2 holds 3 animals: pen=%d total=%d", got.Pens[0].Count, got.TotalCount)
	}

	// Paging walks PENS: a page of 1 returns one pen but still reports every pen and every animal.
	page, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, GroupByPen: true, Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("pen page 2: %v", err)
	}
	if len(page.Pens) != 1 || page.TotalRows != 3 || page.TotalCount != 6 {
		t.Errorf("limit=1 offset=1: pens=%d total_rows=%d total_count=%d, want 1/3/6", len(page.Pens), page.TotalRows, page.TotalCount)
	}
}

// Status matrix on the pen page: only the requested lifecycle bucket is counted. A dead and a sold
// animal in an otherwise live pen must not appear on the pen's line, in its chips, or in its
// nested rows — and a pen whose every animal has exited must vanish rather than show a zero line.
func TestCountsBreakdownPenPageStatusMatrixCountsOnlyTheRequestedLifecycle(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedPenChartFixture(t, ctx, pool) // 6 alive: Castro1 pen "2" x3, "Part 1" x1; Castro Two pen "2" x2

	// Two exited animals in Castro 1 pen 2, and one dead animal alone in a third shed.
	for i, lifecycle := range []string{"dead", "sold"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(80+i), goatDisplayID(80+i), "male", "Sirohi", lifecycle, "F2",
			strp(countsPark), strp(countsShedCastroOne), nil)
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2', 'Castro 1')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET partition_label = EXCLUDED.partition_label`,
			countsTenant, goatUUID(80+i), countsShedCastroOne); err != nil {
			t.Fatalf("seed exited partition: %v", err)
		}
	}
	insertBreakdownGoat(t, ctx, pool, goatUUID(85), goatDisplayID(85), "female", "Sirohi", "dead", "F2",
		strp(countsPark), strp(countsShedB), nil)

	live, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, GroupByPen: true, Limit: 100})
	if err != nil {
		t.Fatalf("live pen page: %v", err)
	}
	if live.TotalRows != 3 || live.TotalCount != 6 {
		t.Errorf("live pen page total_rows=%d total_count=%d, want 3 pens / 6 animals", live.TotalRows, live.TotalCount)
	}
	for _, pen := range live.Pens {
		for _, b := range pen.Breeds {
			if b.Key == "Sirohi" {
				t.Errorf("exited Sirohi animals leaked into live pen %q chips: %+v", pen.OperationalLocationDisplay, pen.Breeds)
			}
		}
		for _, row := range pen.Rows {
			if row.Breed == "Sirohi" {
				t.Errorf("exited Sirohi animals leaked into live pen %q rows: %+v", pen.OperationalLocationDisplay, row)
			}
		}
	}

	dead, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, LifecycleStatus: strp("dead"), GroupByPen: true, Limit: 100})
	if err != nil {
		t.Fatalf("dead pen page: %v", err)
	}
	if dead.TotalRows != 2 || dead.TotalCount != 2 {
		t.Errorf("dead pen page total_rows=%d total_count=%d, want 2 pens / 2 animals", dead.TotalRows, dead.TotalCount)
	}
	for _, pen := range dead.Pens {
		if pen.Count != 1 || len(pen.Rows) != 1 || pen.Rows[0].Breed != "Sirohi" {
			t.Errorf("dead pen %q should hold exactly one Sirohi row: %+v", pen.OperationalLocationDisplay, pen)
		}
	}
}

// Park scope on the pen page: a park filter keeps only that park's pens, and the pen page's total
// for that scope equals the grain page's total for the same scope. Shed names repeat across parks
// ("Castro 1" exists under both), so this pins that the scope is keyed on park_id, never on name.
func TestCountsBreakdownPenPageParkScopeMatchesTheGrainPage(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedPenChartFixture(t, ctx, pool) // park1: Castro 1 pens "2" x3 + "Part 1" x1; park2: Castro pen "2" x2

	for _, park := range []string{countsPark, countsParkTwo} {
		grain, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ParkIDs: []string{park}, Limit: 100})
		if err != nil {
			t.Fatalf("grain page park %s: %v", park, err)
		}
		pens, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ParkIDs: []string{park}, GroupByPen: true, Limit: 100})
		if err != nil {
			t.Fatalf("pen page park %s: %v", park, err)
		}
		if pens.TotalCount != grain.TotalCount || pens.TotalCount == 0 {
			t.Errorf("park %s: pen page total_count=%d, grain page total_count=%d", park, pens.TotalCount, grain.TotalCount)
		}
		for _, pen := range pens.Pens {
			if ptrValue(pen.ParkID) != park {
				t.Errorf("park %s scope leaked pen %q from park %q", park, pen.OperationalLocationDisplay, ptrValue(pen.ParkID))
			}
		}
	}
	if !(func() bool {
		one, _ := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ParkIDs: []string{countsPark}, GroupByPen: true, Limit: 100})
		two, _ := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ParkIDs: []string{countsParkTwo}, GroupByPen: true, Limit: 100})
		return one.TotalRows == 2 && two.TotalRows == 1
	})() {
		t.Errorf("expected 2 pens in park one and 1 pen in park two")
	}
}

// The cross-tab groups one key deeper than the stage series, over rows the locations joins can
// legitimately match more than once. If either join were rewritten onto a non-unique column, a
// single animal would be counted once per matching location row and every segment would inflate
// while the bar still looked internally consistent. Seed decoy locations, spread ONE (stage, sex)
// pair across several breeds and pens, and assert the segment is exactly the number of animals.
func TestCountsBreakdownStageSexOneToManyLocationJoinDoesNotInflateSegments(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES
  ('00000000-0000-4000-8000-000000002098'::uuid, $1::uuid, 'farm', 'CPT-F1-DUP2', 'CPT Farm 1', 'active'),
  ('00000000-0000-4000-8000-000000004098'::uuid, $1::uuid, 'shed', 'CPT-S1-DUP2', 'CPT Shed 1', 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant); err != nil {
		t.Fatalf("seed decoy locations: %v", err)
	}

	// Four animals, all K1 female, deliberately spread across two breeds and two pens: the pair is
	// one BAR SEGMENT however many grain rows it is made of.
	seed := []struct{ breed, shed string }{
		{"Beetal", countsShedA},
		{"Beetal", countsShedB},
		{"Malai", countsShedA},
		{"Malai", countsShedB},
	}
	for i, s := range seed {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", s.breed, "alive", "K1", strp(countsPark), strp(s.shed), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if len(got.Charts.StageSex) != 1 {
		t.Fatalf("stage_sex has %d bars, want 1 — the four animals share one stage", len(got.Charts.StageSex))
	}
	bar := got.Charts.StageSex[0]
	if bar.Female != 4 || bar.Male != 0 || bar.Other != 0 || bar.Count != 4 {
		t.Fatalf("K1 bar = %+v, want female 4 / male 0 / other 0 / count 4", bar)
	}
}

// Both series are WHOLE-RESULT rollups. Walking the detail table must move rows only: a chart that
// followed the page would report a different herd on page two than on page one, and the KPI above
// it would agree with neither.
func TestCountsBreakdownStageSexPageBoundaryLeavesTheSeriesWhole(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	seed := []struct{ breed, stage, sex, shed string }{
		{"Beetal", "K1", "female", countsShedA},
		{"Malai", "K1", "male", countsShedB},
		{"Sojat", "Mother", "female", countsShedA},
		{"Beetal", "Buck", "male", countsShedB},
		{"Malai", "K2", "female", countsShedA},
	}
	for i, s := range seed {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			s.sex, s.breed, "alive", s.stage, strp(countsPark), strp(s.shed), nil)
	}

	read := func(limit, offset int32) []domain.CountsBreakdownStageSexPoint {
		t.Helper()
		got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant, Limit: limit, Offset: offset,
		})
		if err != nil {
			t.Fatalf("GetCountsBreakdown(limit=%d offset=%d): %v", limit, offset, err)
		}
		return got.Charts.StageSex
	}

	whole := read(50, 0)
	for _, page := range []struct{ limit, offset int32 }{{2, 0}, {2, 2}, {2, 4}, {1, 3}} {
		got := read(page.limit, page.offset)
		if len(got) != len(whole) {
			t.Fatalf("limit=%d offset=%d: %d bars, want %d", page.limit, page.offset, len(got), len(whole))
		}
		for i := range whole {
			if got[i] != whole[i] {
				t.Errorf("limit=%d offset=%d: bar %d = %+v, want %+v", page.limit, page.offset, i, got[i], whole[i])
			}
		}
	}
}

// Park scope must narrow the cross-tab exactly as it narrows the head count, and the two halves of
// a split herd must add back up to the whole. A chart that ignored the scope would show one park's
// reader the other park's animals under their own park's total.
func TestCountsBreakdownStageSexParkScopeNarrowsWithTheHeadCount(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	const otherPark = "00000000-0000-4000-8000-000000003002"
	const otherShed = "00000000-0000-4000-8000-000000004097"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES
  ($2::uuid, $1::uuid, 'park', 'CBE-P1', 'CBE Park 1', 'active'),
  ($3::uuid, $1::uuid, 'shed', 'CBE-S1', 'Castro', 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant, otherPark, otherShed); err != nil {
		t.Fatalf("seed second park: %v", err)
	}

	seed := []struct{ stage, sex, park, shed string }{
		{"K1", "female", countsPark, countsShedA},
		{"K1", "male", countsPark, countsShedA},
		{"Mother", "female", countsPark, countsShedB},
		{"K1", "female", otherPark, otherShed},
		{"Buck", "male", otherPark, otherShed},
	}
	for i, s := range seed {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			s.sex, "Beetal", "alive", s.stage, strp(s.park), strp(s.shed), nil)
	}

	scoped := func(parks ...string) (map[string]domain.CountsBreakdownStageSexPoint, int64) {
		t.Helper()
		got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant, ParkIDs: parks, Limit: 50,
		})
		if err != nil {
			t.Fatalf("GetCountsBreakdown(parks=%v): %v", parks, err)
		}
		out := map[string]domain.CountsBreakdownStageSexPoint{}
		var sum int64
		for _, point := range got.Charts.StageSex {
			out[point.Key] = point
			sum += point.Count
		}
		if sum != got.TotalCount {
			t.Fatalf("parks=%v: stage_sex sums to %d, want total_count %d", parks, sum, got.TotalCount)
		}
		return out, got.TotalCount
	}

	here, hereTotal := scoped(countsPark)
	there, thereTotal := scoped(otherPark)
	_, wholeTotal := scoped()

	if hereTotal != 3 || thereTotal != 2 || wholeTotal != 5 {
		t.Fatalf("totals here=%d there=%d whole=%d, want 3/2/5", hereTotal, thereTotal, wholeTotal)
	}
	if p := here["K1"]; p.Female != 1 || p.Male != 1 {
		t.Errorf("this park's K1 = %+v, want female 1 / male 1", p)
	}
	if _, ok := here["Buck"]; ok {
		t.Error("this park's chart carries the other park's Buck bar")
	}
	if p := there["K1"]; p.Female != 1 || p.Male != 0 {
		t.Errorf("other park's K1 = %+v, want female 1 / male 0", p)
	}
	if _, ok := there["Mother"]; ok {
		t.Error("the other park's chart carries this park's Mother bar")
	}
}

// The label join must change WORDS and nothing else. It is a LEFT JOIN added to two branches that
// carry a lifecycle predicate, so this walks the whole status matrix: the stage facet and the
// stage bars must count exactly the animals they counted before the join existed, and a stage the
// lookup does not know must keep its own code rather than going blank.
func TestCountsBreakdownStageLabelsOneToManyAcrossTheStatusMatrixLeaveCountsAlone(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, status, sort_order)
VALUES
  ($1::uuid, 'F2-Male',   'Fattening male',   'kid',   'active', 1),
  ($1::uuid, 'F2-Female', 'Fattening female', 'kid',   'active', 2),
  ($1::uuid, 'K3',        'Weaned kids',      'kid',   'active', 3),
  ($1::uuid, 'Mother',    'Mother',           'adult', 'active', 4)
ON CONFLICT (tenant_id, stage_code) DO NOTHING`, countsTenant); err != nil {
		t.Fatalf("seed stage lookup: %v", err)
	}

	// Every lifecycle the page can be asked for, plus a stage the lookup has never heard of.
	seed := []struct{ stage, sex, lifecycle string }{
		{"F2-Male", "male", "alive"},
		{"F2-Male", "male", "alive"},
		{"F2-Female", "female", "alive"},
		{"K3", "female", "alive"},
		{"Mother", "female", "alive"},
		{"F2-Trial", "male", "alive"}, // no lookup row
		{"F2-Male", "male", "dead"},
		{"F2-Female", "female", "sold"},
		{"K3", "male", "culled"},
	}
	for i, s := range seed {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			s.sex, "Beetal", s.lifecycle, s.stage, strp(countsPark), strp(countsShedA), nil)
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}

	// Only the six live animals are counted, exactly as before the join.
	if got.TotalCount != 6 {
		t.Fatalf("total_count=%d, want 6 — the label join must not change membership", got.TotalCount)
	}

	wantLabel := map[string]string{
		"F2-Male":   "Fattening male",
		"F2-Female": "Fattening female",
		"K3":        "K3",       // has a lookup name ("Weaned kids") and deliberately keeps its code
		"Mother":    "Mother",   // name equals code
		"F2-Trial":  "F2-Trial", // no lookup row at all
	}
	wantCount := map[string]int64{"F2-Male": 2, "F2-Female": 1, "K3": 1, "Mother": 1, "F2-Trial": 1}

	// The CHART still draws the cohort's sexes apart -- the fold is the FILTER's, and a reader who
	// wants to know how fattening splits by sex can still see it.
	seen := map[string]bool{}
	for _, point := range got.Charts.Stage {
		want, known := wantLabel[point.Key]
		if !known {
			continue
		}
		seen[point.Key] = true
		if point.Label != want {
			t.Errorf("charts.stage[%q].Label = %q, want %q", point.Key, point.Label, want)
		}
		if point.Count != wantCount[point.Key] {
			t.Errorf("charts.stage[%q].Count = %d, want %d", point.Key, point.Count, wantCount[point.Key])
		}
	}
	for key := range wantLabel {
		if !seen[key] {
			t.Errorf("charts.stage is missing stage %q", key)
		}
	}

	// The FILTER folds the fattening pair into one option. F2-Trial is the control: it is NOT a
	// fattening code (the membership is matched whole, never on an "F2" prefix), so it keeps its
	// own option. No lookup row names the cohort here, so the folded option falls back to its CODE
	// rather than borrowing "Fattening male" from a member.
	facetLabel := map[string]string{}
	for _, point := range got.Facets.Stages {
		facetLabel[point.Key] = point.Label
	}
	for _, key := range []string{"F2-Male", "F2-Female"} {
		if _, present := facetLabel[key]; present {
			t.Errorf("facets.stages still offers %q; the fattening pair must fold into one option", key)
		}
	}
	for key, want := range map[string]string{
		herdstage.FatteningKey: herdstage.FatteningKey,
		"K3":                   "K3",
		"Mother":               "Mother",
		"F2-Trial":             "F2-Trial",
	} {
		got, present := facetLabel[key]
		if !present {
			t.Errorf("facets.stages is missing stage %q", key)
			continue
		}
		if got != want {
			t.Errorf("facets.stages[%q].Label = %q, want %q", key, got, want)
		}
	}

	// The cross-tab reads the same words, so the bar and the filter it drives cannot disagree.
	for _, point := range got.Charts.StageSex {
		if want, known := wantLabel[point.Key]; known && point.Label != want {
			t.Errorf("charts.stage_sex[%q].Label = %q, want %q", point.Key, point.Label, want)
		}
	}
}

func TestCountsBreakdownStageLabelsPageBoundaryPaginationLeavesTheSeriesWhole(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, status, sort_order)
VALUES
  ($1::uuid, 'F2-Male',   'Fattening male',   'kid',   'active', 1),
  ($1::uuid, 'F2-Female', 'Fattening female', 'kid',   'active', 2)
ON CONFLICT (tenant_id, stage_code) DO NOTHING`, countsTenant); err != nil {
		t.Fatalf("seed stage lookup: %v", err)
	}
	for i, stage := range []string{"F2-Male", "F2-Female", "K3"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(300+i), goatDisplayID(300+i),
			"female", "Beetal", "alive", stage, strp(countsPark), strp(countsShedA), nil)
	}

	read := func(limit, offset int32) map[string]string {
		t.Helper()
		got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant,
			Limit:    limit,
			Offset:   offset,
		})
		if err != nil {
			t.Fatalf("GetCountsBreakdown(limit=%d offset=%d): %v", limit, offset, err)
		}
		out := make(map[string]string, len(got.Charts.Stage))
		for _, point := range got.Charts.Stage {
			out[point.Key] = point.Label
		}
		return out
	}

	whole := read(50, 0)
	for _, page := range []struct{ limit, offset int32 }{{1, 0}, {1, 1}, {1, 2}} {
		got := read(page.limit, page.offset)
		if len(got) != len(whole) {
			t.Fatalf("limit=%d offset=%d: %d stage labels, want %d", page.limit, page.offset, len(got), len(whole))
		}
		for key, want := range whole {
			if got[key] != want {
				t.Errorf("limit=%d offset=%d: stage %q label=%q, want %q", page.limit, page.offset, key, got[key], want)
			}
		}
	}
}

func TestCountsBreakdownStageLabelsParkScopeHierarchyKeepsRawCodesScoped(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	const otherPark = "00000000-0000-4000-8000-000000003012"
	const otherShed = "00000000-0000-4000-8000-000000004112"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES
  ($2::uuid, $1::uuid, 'park', 'CBE-P12', 'CBE Park 12', 'active'),
  ($3::uuid, $1::uuid, 'shed', 'CBE-S12', 'Castro', 'active')
ON CONFLICT (location_id) DO NOTHING;
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, status, sort_order)
VALUES ($1::uuid, 'F2-Male', 'Fattening male', 'kid', 'active', 1)
ON CONFLICT (tenant_id, stage_code) DO NOTHING`, countsTenant, otherPark, otherShed); err != nil {
		t.Fatalf("seed scoped locations and stage lookup: %v", err)
	}

	insertBreakdownGoat(t, ctx, pool, goatUUID(350), goatDisplayID(350), "male", "Beetal", "alive", "F2-Male", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(351), goatDisplayID(351), "female", "Beetal", "alive", "K3", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(352), goatDisplayID(352), "male", "Beetal", "alive", "F2-Male", strp(otherPark), strp(otherShed), nil)

	scoped := func(park string) (map[string]domain.CountsBreakdownSeriesPoint, int64) {
		t.Helper()
		got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant,
			ParkIDs:  []string{park},
			Limit:    50,
		})
		if err != nil {
			t.Fatalf("GetCountsBreakdown(park=%s): %v", park, err)
		}
		out := map[string]domain.CountsBreakdownSeriesPoint{}
		for _, point := range got.Charts.Stage {
			out[point.Key] = point
		}
		return out, got.TotalCount
	}

	here, hereTotal := scoped(countsPark)
	there, thereTotal := scoped(otherPark)
	if hereTotal != 2 || thereTotal != 1 {
		t.Fatalf("park totals here=%d there=%d, want 2/1", hereTotal, thereTotal)
	}
	if point := here["F2-Male"]; point.Label != "Fattening male" || point.Count != 1 {
		t.Errorf("this park F2-Male = %+v, want Fattening male count 1", point)
	}
	if point := here["K3"]; point.Label != "K3" || point.Count != 1 {
		t.Errorf("this park K3 = %+v, want raw-code label count 1", point)
	}
	if point := there["F2-Male"]; point.Label != "Fattening male" || point.Count != 1 {
		t.Errorf("other park F2-Male = %+v, want Fattening male count 1", point)
	}
	if _, ok := there["K3"]; ok {
		t.Error("other park stage chart leaked this park's K3 bar")
	}
}

// PURCHASED LOADS (maintainer request 2026-09-18). Three things a wrong join would get wrong here,
// each asserted: (1) an animal accepted on TWO loads is counted on exactly ONE, and it is the same
// one Sales -> Purchase and Born names; (2) "bought" is the load's OWN figure — declared size when
// stated, else tracked + prior outcomes, exactly the Sales page's rule — and is NOT narrowed by the
// page filters, while on-farm/stages/sexes ARE; (3) on_farm equals the sum of the stage split and
// the sum of the sex split for every load, so the client can render both without re-summing.
func TestCountsBreakdownLoadsReadCurrentTagAndSexPerLoadAndMatchTheSalesPurchasedRule(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	var vendor string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_type, display_name, status) VALUES ('org', 'Sardar Traders', 'active')
RETURNING party_id::text`).Scan(&vendor); err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	load := func(key, purchaseDate string, expected int) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, expected_count, idempotency_key, context)
VALUES ($1::uuid, $2::uuid, $3::date, 'accepted_intake', $4, $5, jsonb_build_object('load_ref', $6::text))
RETURNING load_id::text`, countsTenant, vendor, purchaseDate, expected, key, key).Scan(&id); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
		return id
	}
	// Load A: undeclared size, so bought = tracked + prior. Load B: declares 10, tracked 1.
	loadA := load("A", "2026-08-01", 0)
	loadB := load("B", "2026-08-09", 10)
	accept := func(loadID, goatID, acceptedAt string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', $4::timestamptz)`,
			countsTenant, loadID, goatID, acceptedAt); err != nil {
			t.Fatalf("seed load goat: %v", err)
		}
	}

	// Load A's animals: two fattening males and a kid female alive, one dead, one accepted on BOTH
	// loads (later acceptance on B wins, by the same total order the Sales read uses).
	insertBreakdownGoat(t, ctx, pool, goatUUID(1), goatDisplayID(1), "male", "Beetal", "alive", "Fattening", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(2), goatDisplayID(2), "male", "Beetal", "alive", "Fattening", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(3), goatDisplayID(3), "female", "Beetal", "alive", "Kid", strp(countsPark), strp(countsShedB), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(4), goatDisplayID(4), "male", "Beetal", "dead", "Fattening", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(5), goatDisplayID(5), "female", "Sojat", "alive", "Fattening", strp(countsPark), strp(countsShedB), nil)
	seedBreakdownGoatIdentifier(t, ctx, pool, goatUUID(1), "animal_identifier_1", "RFID-A-001")
	seedBreakdownGoatIdentifier(t, ctx, pool, goatUUID(1), "animal_identifier_2", "TAG-A-001")
	seedBreakdownGoatIdentifier(t, ctx, pool, goatUUID(2), "animal_identifier_1", "RFID-A-002")
	seedBreakdownGoatIdentifier(t, ctx, pool, goatUUID(3), "animal_identifier_1", "RFID-A-003")
	seedBreakdownGoatIdentifier(t, ctx, pool, goatUUID(4), "animal_identifier_1", "RFID-A-004")
	seedBreakdownGoatIdentifier(t, ctx, pool, goatUUID(5), "animal_identifier_1", "RFID-B-005")
	for _, id := range []int{1, 2, 3, 4} {
		accept(loadA, goatUUID(id), "2026-08-01T10:00:00Z")
	}
	accept(loadA, goatUUID(5), "2026-08-01T10:00:00Z")
	accept(loadB, goatUUID(5), "2026-08-09T10:00:00Z")
	// A pre-register outcome on load A: two animals sold before Goat OS existed.
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_prior_outcomes (tenant_id, load_id, outcome, animal_count)
VALUES ($1::uuid, $2::uuid, 'sold', 2)`, countsTenant, loadA); err != nil {
		t.Fatalf("seed prior outcome: %v", err)
	}
	// A farm-born animal on no load must not appear anywhere in the loads read.
	insertBreakdownGoat(t, ctx, pool, goatUUID(6), goatDisplayID(6), "female", "Beetal", "alive", "Kid", strp(countsPark), strp(countsShedB), nil)

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if len(got.Loads) != 2 {
		t.Fatalf("loads=%d, want 2 (newest first): %+v", len(got.Loads), got.Loads)
	}
	b, a := got.Loads[0], got.Loads[1]
	if b.LoadID != loadB || a.LoadID != loadA {
		t.Fatalf("load order = [%s %s], want newest purchase first [%s %s]", b.LoadID, a.LoadID, loadB, loadA)
	}
	if b.LoadRef != "B" || b.VendorName != "Sardar Traders" || b.PurchaseDate != "2026-08-09" {
		t.Errorf("load B identity = %+v", b)
	}

	// Rule (2): bought. A has no declared size: 4 tracked (goat 5 moved to B) + 2 prior = 6.
	// B declares 10, so 10 -- not the 1 animal it tracks.
	if a.Purchased != 6 || b.Purchased != 10 {
		t.Errorf("purchased A=%d B=%d, want 6 and 10 (the Sales page's declared-else-attributed rule)", a.Purchased, b.Purchased)
	}
	// Rule (1): goat 5 is on B only, so A's live set is goats 1-3 and B's is goat 5.
	if a.OnFarm != 3 || b.OnFarm != 1 {
		t.Errorf("on_farm A=%d B=%d, want 3 and 1", a.OnFarm, b.OnFarm)
	}
	wantStagesA := []domain.CountsBreakdownSeriesPoint{{Key: "Fattening", Label: "Fattening", Count: 2}, {Key: "Kid", Label: "Kid", Count: 1}}
	wantSexesA := []domain.CountsBreakdownSeriesPoint{{Key: "male", Label: "male", Count: 2}, {Key: "female", Label: "female", Count: 1}}
	if !reflect.DeepEqual(a.Stages, wantStagesA) || !reflect.DeepEqual(a.Sexes, wantSexesA) {
		t.Errorf("load A split: stages=%+v sexes=%+v", a.Stages, a.Sexes)
	}
	wantTagsA := []domain.CountsBreakdownLoadTag{
		{Type: "animal_identifier_1", Value: "RFID-A-001", Count: 1},
		{Type: "animal_identifier_1", Value: "RFID-A-002", Count: 1},
		{Type: "animal_identifier_1", Value: "RFID-A-003", Count: 1},
		{Type: "animal_identifier_2", Value: "TAG-A-001", Count: 1},
	}
	wantTagsB := []domain.CountsBreakdownLoadTag{{Type: "animal_identifier_1", Value: "RFID-B-005", Count: 1}}
	if !reflect.DeepEqual(a.CurrentTags, wantTagsA) || !reflect.DeepEqual(b.CurrentTags, wantTagsB) {
		t.Errorf("current tags: load A=%+v load B=%+v", a.CurrentTags, b.CurrentTags)
	}
	// Rule (3), on every load.
	for _, row := range got.Loads {
		var stages, sexes int64
		for _, p := range row.Stages {
			stages += p.Count
		}
		for _, p := range row.Sexes {
			sexes += p.Count
		}
		if stages != row.OnFarm || sexes != row.OnFarm {
			t.Errorf("load %s: on_farm=%d but stages sum %d and sexes sum %d", row.LoadRef, row.OnFarm, stages, sexes)
		}
	}

	// Rule (2), the other half: filtering the page to one stage narrows on-farm but never bought.
	filtered, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ManagementStages: []string{"Fattening"}})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(Fattening): %v", err)
	}
	if len(filtered.Loads) != 2 {
		t.Fatalf("filtered loads=%d, want both loads still listed", len(filtered.Loads))
	}
	fa := filtered.Loads[1]
	if fa.Purchased != 6 || fa.OnFarm != 2 || len(fa.Stages) != 1 || fa.Stages[0].Key != "Fattening" {
		t.Errorf("filtered load A = %+v, want bought 6 unchanged, on_farm 2, one Fattening bucket", fa)
	}
	wantFatteningTagsA := []domain.CountsBreakdownLoadTag{
		{Type: "animal_identifier_1", Value: "RFID-A-001", Count: 1},
		{Type: "animal_identifier_1", Value: "RFID-A-002", Count: 1},
		{Type: "animal_identifier_2", Value: "TAG-A-001", Count: 1},
	}
	if !reflect.DeepEqual(fa.CurrentTags, wantFatteningTagsA) {
		t.Errorf("filtered load A tags = %+v, want only current Fattening animal tags %+v", fa.CurrentTags, wantFatteningTagsA)
	}

	// A load with none of its animals surviving the filter is still listed, with an empty split
	// rather than a missing row -- "bought 10, none under these filters" is the honest answer.
	kids, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ManagementStages: []string{"Kid"}})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(Kid): %v", err)
	}
	kb := kids.Loads[0]
	if kb.LoadID != loadB || kb.OnFarm != 0 || len(kb.Stages) != 0 || len(kb.Sexes) != 0 || kb.Purchased != 10 {
		t.Errorf("load B under Kid filter = %+v, want listed with on_farm 0 and empty splits", kb)
	}
	if len(kb.CurrentTags) != 0 {
		t.Errorf("load B under Kid filter current_tags = %+v, want empty", kb.CurrentTags)
	}

	read := func(t *testing.T, q domain.CountsBreakdownQuery) []domain.CountsBreakdownLoadRow {
		t.Helper()
		q.TenantID = countsTenant
		res, err := repo.GetCountsBreakdown(ctx, q)
		if err != nil {
			t.Fatalf("GetCountsBreakdown(%+v): %v", q, err)
		}
		return res.Loads
	}

	// OneToMany: goat 5 has TWO accepted rows and lands on ONE load -- the tracked totals across
	// both loads add up to the five distinct accepted animals, never six.
	t.Run("OneToManyAcceptanceRowsCountEachAnimalOnce", func(t *testing.T) {
		var tracked int64
		// Load A: 4 tracked (goats 1-4) + 2 prior = 6 bought; load B declares 10 but tracks 1.
		// on_farm over the unfiltered live set is 3 + 1 = 4 live animals, none counted twice.
		for _, row := range read(t, domain.CountsBreakdownQuery{}) {
			tracked += row.OnFarm
		}
		if tracked != 4 {
			t.Errorf("live animals across loads = %d, want 4 (goat 5 on one load only)", tracked)
		}
	})

	// PaginationIndependence: the pen table's limit/offset never touch the loads read.
	t.Run("PaginationOfThePenTableLeavesLoadsWhole", func(t *testing.T) {
		paged := read(t, domain.CountsBreakdownQuery{GroupByPen: true, Limit: 1, Offset: 1})
		if !reflect.DeepEqual(paged, got.Loads) {
			t.Errorf("loads under Limit 1 / Offset 1 = %+v, want the same whole-result rows %+v", paged, got.Loads)
		}
	})

	// ParkScopeHierarchy: the park decides WHICH loads are listed -- a load belongs to the park its
	// animals are in -- and never what a listed load bought, which is the load's own fact. Every
	// animal here sits in countsPark, so its own park keeps both loads whole, and a park holding
	// none of them lists no load at all rather than both at "on farm 0", which read as sold out
	// (TestCountsBreakdownLoadsFollowTheSelectedPark covers loads split across two parks).
	t.Run("ParkScopeHierarchyPicksTheLoadsButNeverNarrowsBought", func(t *testing.T) {
		same := read(t, domain.CountsBreakdownQuery{ParkIDs: []string{countsPark}})
		if !reflect.DeepEqual(same, got.Loads) {
			t.Errorf("own-park scope changed the loads: %+v", same)
		}
		other := read(t, domain.CountsBreakdownQuery{ParkIDs: []string{"00000000-0000-4000-8000-000000003999"}})
		if len(other) != 0 {
			t.Errorf("other-park scope = %+v, want no load listed: none of their animals are in that park", other)
		}
	})

	// StatusMatrix: the live default excludes the dead animal; asking for the dead bucket reports
	// exactly it, on its own load, with the tag it died carrying. Load B holds no dead animal, so
	// the dead bucket does not list it at all (a load is listed only where it has animals).
	t.Run("StatusMatrixLifecycleBucketsAreDisjoint", func(t *testing.T) {
		dead := read(t, domain.CountsBreakdownQuery{LifecycleStatus: strp("dead")})
		if len(dead) != 1 {
			t.Fatalf("dead bucket loads = %+v, want only load A, the one with a dead animal", dead)
		}
		da := dead[0]
		if da.LoadID != loadA || da.OnFarm != 1 || da.Purchased != 6 ||
			!reflect.DeepEqual(da.Stages, []domain.CountsBreakdownSeriesPoint{{Key: "Fattening", Label: "Fattening", Count: 1}}) ||
			!reflect.DeepEqual(da.Sexes, []domain.CountsBreakdownSeriesPoint{{Key: "male", Label: "male", Count: 1}}) ||
			!reflect.DeepEqual(da.CurrentTags, []domain.CountsBreakdownLoadTag{{Type: "animal_identifier_1", Value: "RFID-A-004", Count: 1}}) {
			t.Errorf("dead bucket load A = %+v, want the one dead Fattening male and bought 6", da)
		}
		if a.OnFarm+da.OnFarm != 4 {
			t.Errorf("alive %d + dead %d on load A, want the 4 tracked animals partitioned exactly", a.OnFarm, da.OnFarm)
		}
	})
}

// The three stored fattening values reach the stage dropdown as ONE option carrying the whole
// cohort's head count, and ticking it returns every one of those animals. This is a RECORDED fold
// of three exact, known values (maintainer decision 2026-09-22) and is the opposite of the
// near-duplicate case above: the sexed pair is answered better by this page's gender filter, while
// `ICU-Kid`/`ICU-Kids`/`Icu- Kid` are a data-quality problem that must stay visible.
func TestFatteningStagesReachTheFilterAsOneOption(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	for i, stage := range []string{"F2", "F2-Male", "F2-Female", "K2"} {
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", stage, strp(countsPark), strp(countsShedA), nil)
	}
	// OneToMany across the stage lookup: a row per member, joined through the folded code. The
	// authored names the farm really carries, and the folded option must be labelled from the
	// COHORT's row, not from whichever member the read happened to see first -- labelling the whole
	// cohort "Fattening female" is the exact way this fold goes wrong.
	for _, row := range [][2]string{{"F2", "Fattening"}, {"F2-Male", "Fattening male"}, {"F2-Female", "Fattening female"}} {
		if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, status, sort_order)
VALUES ($1::uuid, $2, $3, 'kid', 'active', 1)
ON CONFLICT (tenant_id, stage_code) DO NOTHING`, countsTenant, row[0], row[1]); err != nil {
			t.Fatalf("seed stage lookup %s: %v", row[0], err)
		}
	}

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	stages := map[string]domain.CountsBreakdownSeriesPoint{}
	for _, point := range got.Facets.Stages {
		stages[point.Key] = point
	}
	if len(stages) != 2 {
		t.Fatalf("facets.stages=%+v, want 2 options (the folded fattening one, and K2)", got.Facets.Stages)
	}
	folded := stages[herdstage.FatteningKey]
	if folded.Count != 3 {
		t.Fatalf("facets.stages[%s]=%d, want 3 — the folded option carries the whole cohort",
			herdstage.FatteningKey, folded.Count)
	}
	if folded.Label != "Fattening" {
		t.Fatalf("folded option label = %q, want the cohort's own authored name %q", folded.Label, "Fattening")
	}

	// Ticking that one option must reach all three animals, not only the one on the bare tag.
	filtered, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, ManagementStages: []string{herdstage.FatteningKey}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if filtered.TotalCount != 3 {
		t.Fatalf("total_count=%d, want 3 — the fattening option must match every stored value it stands for", filtered.TotalCount)
	}

	// The fold is FILTER-OPTIONS-ONLY: the grain rows still print the value each animal carries,
	// so nothing downstream of this page starts reading a stage the herd register never recorded.
	seen := map[string]bool{}
	for _, row := range filtered.Items {
		seen[row.ManagementStage] = true
	}
	for _, stage := range []string{"F2", "F2-Male", "F2-Female"} {
		if !seen[stage] {
			t.Fatalf("grain rows = %+v, want the stored stage %q still on its own row", filtered.Items, stage)
		}
	}
}

// A load names the PENS its filtered live animals sit in (maintainer request 2026-09-22), so the
// chart can carry them in a bracket beside the load number. Four properties, each a way the
// bracket could lie:
//
//  1. Pens are the HERD REGISTER's answer at PEN grain, biggest first — never rolled up to the
//     shed, which would merge Castro 1's two pens into one entry naming neither.
//  2. Same-named sheds in two parks stay two entries, carrying their own park code. This is the
//     OL-1 name-merge defect: both parks really do have a `Castro 1`.
//  3. The pens sum to on_farm, so the bracket and the bars on the SAME chart agree, and a page
//     filtered to one stage narrows both while bought — the load's own fact — does not move.
//  4. The HUMAN partition label is rendered, never the normalized matching key: `Part 1` must
//     read "Castro 1 - Part 1", never "Castro 1 - 1".
func TestCountsBreakdownLoadsNameThePensTheirAnimalsSitIn(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)

	var vendor string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_type, display_name, status) VALUES ('org', 'Krishnamorrthy', 'active')
RETURNING party_id::text`).Scan(&vendor); err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	var loadID string
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, expected_count, idempotency_key, context)
VALUES ($1::uuid, $2::uuid, '2026-08-01'::date, 'accepted_intake', 0, 'pens-load', jsonb_build_object('load_ref', '131'))
RETURNING load_id::text`, countsTenant, vendor).Scan(&loadID); err != nil {
		t.Fatalf("seed load: %v", err)
	}

	// Three pens across two parks, with the SAME shed name on both sides. The K1 kid in `Part 1`
	// is the stage-filter probe; everything else is Fattening.
	place := func(i int, shed, park, label, stage string) {
		t.Helper()
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"female", "Beetal", "alive", stage, strp(park), strp(shed), nil)
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'seed')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET partition_label = EXCLUDED.partition_label`,
			countsTenant, goatUUID(i), shed, label); err != nil {
			t.Fatalf("seed goat_shed_partitions: %v", err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', '2026-08-01T10:00:00Z'::timestamptz)`,
			countsTenant, loadID, goatUUID(i)); err != nil {
			t.Fatalf("seed load goat: %v", err)
		}
	}
	place(0, countsShedCastroOne, countsPark, "2", "Fattening")
	place(1, countsShedCastroOne, countsPark, "2", "Fattening")
	place(2, countsShedCastroOne, countsPark, "Part 1", "K1")
	place(3, countsShedCastroTwo, countsParkTwo, "2", "Fattening")

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	if len(got.Loads) != 1 {
		t.Fatalf("loads=%d, want 1: %+v", len(got.Loads), got.Loads)
	}
	row := got.Loads[0]

	// Park codes come from the response's own facet — the shared fixture inserts with
	// ON CONFLICT DO NOTHING, so which id carries which code depends on what the suite seeded
	// first, and a literal here would fail for a reason unrelated to the behaviour under test.
	parkCode := map[string]string{}
	for _, park := range got.Facets.Parks {
		parkCode[park.Key] = park.Label
	}
	one, two := parkCode[countsPark], parkCode[countsParkTwo]
	if one == "" || two == "" || one == two {
		t.Fatalf("park facet did not resolve two distinct park codes: %+v", got.Facets.Parks)
	}

	// (1) + (2) + (4): pen grain, biggest first, park-qualified, HUMAN partition label. Ties (the
	// two single-animal pens) fall back to park, then shed, then partition — the SQL's own ORDER
	// BY — so the expectation is deterministic.
	type pen struct {
		park, display string
		animals       int64
	}
	gotPens := make([]pen, 0, len(row.Pens))
	for _, p := range row.Pens {
		gotPens = append(gotPens, pen{p.ParkName, p.OperationalLocationDisplay, p.Animals})
	}
	wantPens := []pen{
		{one, "Castro 1 2", 2},
		{one, "Castro 1 - Part 1", 1},
		{two, "Castro 1 2", 1},
	}
	if !reflect.DeepEqual(gotPens, wantPens) {
		t.Errorf("pens = %+v, want %+v", gotPens, wantPens)
	}
	for _, p := range row.Pens {
		if p.OperationalLocationDisplay == "Castro 1 - 1" {
			t.Errorf("rendered the normalized partition KEY instead of its label: %+v", row.Pens)
		}
		if p.OperationalLocationDisplay == "Castro 1" {
			t.Errorf("rolled a pen up to its shed — the bracket must name the PEN: %+v", row.Pens)
		}
	}

	// (3): the pens partition on_farm exactly, so the bracket and the bars agree.
	var animals int64
	for _, p := range row.Pens {
		animals += p.Animals
	}
	if animals != row.OnFarm || row.OnFarm != 4 {
		t.Errorf("pens sum to %d and on_farm=%d, want both 4", animals, row.OnFarm)
	}

	// (3), the other half: a page filtered to one stage narrows the pens with the counts, while
	// bought is the load's own fact and must NOT move.
	filtered, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ManagementStages: []string{"K1"}, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(K1): %v", err)
	}
	fr := filtered.Loads[0]
	if len(fr.Pens) != 1 || fr.Pens[0].OperationalLocationDisplay != "Castro 1 - Part 1" || fr.Pens[0].Animals != 1 {
		t.Errorf("filtered pens = %+v, want only the kid's own pen", fr.Pens)
	}
	if fr.Purchased != row.Purchased {
		t.Errorf("filtering moved bought from %d to %d — bought is the load's own fact", row.Purchased, fr.Purchased)
	}

	// A load whose animals all fall out of the filter names NO pen rather than the pens they used
	// to be in, and the field is an empty slice rather than null.
	none, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ManagementStages: []string{"Milking"}, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(Milking): %v", err)
	}
	if nr := none.Loads[0]; nr.Pens == nil || len(nr.Pens) != 0 || nr.OnFarm != 0 {
		t.Errorf("load under a filter nothing survives = %+v, want on_farm 0 and an empty (non-nil) pens", nr)
	}

	// The four ways a JOIN + COUNT + GROUP BY silently reports the wrong number, asked of the pens
	// aggregate specifically. Each one is a shape that would leave the bracket disagreeing with
	// the bars beside it on the SAME chart.
	t.Run("OneToManyLocationJoinDoesNotFanOutPenCounts", func(t *testing.T) {
		// A shed whose park carries more than one location row of the same name, and an animal
		// whose partition row is rewritten, must still be counted ONCE. Both locations joins are
		// on the (tenant_id, location_id) primary key, so neither can widen the key set -- this
		// asserts that rather than trusting it.
		if _, err := pool.Exec(ctx, `
UPDATE goat_shed_partitions SET partition_label = '2'
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, countsTenant, goatUUID(0)); err != nil {
			t.Fatalf("rewrite partition: %v", err)
		}
		got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
		if err != nil {
			t.Fatalf("GetCountsBreakdown: %v", err)
		}
		row := got.Loads[0]
		var animals int64
		for _, p := range row.Pens {
			animals += p.Animals
		}
		if animals != row.OnFarm {
			t.Errorf("pens sum to %d but on_farm is %d — a join fanned the pen counts out: %+v", animals, row.OnFarm, row.Pens)
		}
	})

	t.Run("PaginationDoesNotMoveThePens", func(t *testing.T) {
		// The loads read is WHOLE-RESULT: it is never paged with the pen table under it, so
		// walking to page two must not change a load's pens or their counts. A pens aggregate
		// computed over the PAGE rather than the whole result is the classic version of this bug.
		first, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 1, Offset: 0})
		if err != nil {
			t.Fatalf("page 1: %v", err)
		}
		second, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 1, Offset: 1})
		if err != nil {
			t.Fatalf("page 2: %v", err)
		}
		if !reflect.DeepEqual(first.Loads[0].Pens, second.Loads[0].Pens) {
			t.Errorf("pens moved between pages: %+v vs %+v", first.Loads[0].Pens, second.Loads[0].Pens)
		}
	})

	t.Run("ParkScopeHierarchyNarrowsThePensWithTheCounts", func(t *testing.T) {
		// Scoping to ONE park must drop the other park's pens entirely -- and the pens must still
		// sum to the narrowed on_farm, not to the unscoped one.
		scoped, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ParkIDs: []string{countsParkTwo}, Limit: 50})
		if err != nil {
			t.Fatalf("GetCountsBreakdown(park two): %v", err)
		}
		row := scoped.Loads[0]
		var animals int64
		for _, p := range row.Pens {
			animals += p.Animals
			if p.ParkName == one {
				t.Errorf("park-scoped read still names a pen in the other park: %+v", row.Pens)
			}
		}
		if animals != row.OnFarm {
			t.Errorf("park-scoped pens sum to %d, want the narrowed on_farm %d", animals, row.OnFarm)
		}
	})

	t.Run("StatusMatrixExcludesEveryNonLiveAnimalFromThePens", func(t *testing.T) {
		// A dead or sold animal is in NO pen bucket. The page defaults to the live herd, so an
		// animal that leaves must leave the bracket too -- otherwise the bracket keeps pointing a
		// reader at a pen holding an animal that is not there.
		if _, err := pool.Exec(ctx, `
UPDATE goats SET lifecycle_status = 'dead' WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
			countsTenant, goatUUID(3)); err != nil {
			t.Fatalf("kill goat: %v", err)
		}
		got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
		if err != nil {
			t.Fatalf("GetCountsBreakdown: %v", err)
		}
		row := got.Loads[0]
		var animals int64
		for _, p := range row.Pens {
			animals += p.Animals
			if p.ParkName == two {
				t.Errorf("the dead animal's park is still named in the pens: %+v", row.Pens)
			}
		}
		if animals != row.OnFarm {
			t.Errorf("pens sum to %d, want on_farm %d after the death", animals, row.OnFarm)
		}
	})
}

// The Purchased loads card lists only loads with animals on the farm under the page's filters
// (maintainer, 2026-09-24), and the park is one of those filters. Before, a CPT page listed every
// CBE load at "on farm 0", which reads as sold out, and every sold-out load sat on the card too.
func TestCountsBreakdownLoadsFollowTheSelectedPark(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)

	var vendor string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_type, display_name, status) VALUES ('org', 'Park vendor', 'active')
RETURNING party_id::text`).Scan(&vendor); err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	seedLoad := func(key, ref string, expected int) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, expected_count, idempotency_key, context)
VALUES ($1::uuid, $2::uuid, '2026-08-01'::date, 'accepted_intake', $3, $4, jsonb_build_object('load_ref', $5::text))
RETURNING load_id::text`, countsTenant, vendor, expected, key, ref).Scan(&id); err != nil {
			t.Fatalf("seed load %s: %v", ref, err)
		}
		return id
	}
	member := func(load string, i int, park, shed, status string) {
		t.Helper()
		insertBreakdownGoat(t, ctx, pool, goatUUID(i), goatDisplayID(i),
			"male", "Beetal", status, "Fattening", strp(park), strp(shed), nil)
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', '2026-08-01T10:00:00Z'::timestamptz)`,
			countsTenant, load, goatUUID(i)); err != nil {
			t.Fatalf("seed load goat: %v", err)
		}
	}

	parkOneLoad := seedLoad("park-one-load", "201", 5)
	member(parkOneLoad, 0, countsPark, countsShedCastroOne, "alive")
	member(parkOneLoad, 1, countsPark, countsShedCastroOne, "alive")
	// Park two's load is SOLD OUT: nothing of it is on the farm, so no card lists it.
	parkTwoLoad := seedLoad("park-two-load", "202", 3)
	member(parkTwoLoad, 2, countsParkTwo, countsShedCastroTwo, "sold")
	// Known only from pre-GoatOS outcomes: no animal of it is on the farm, so it is not listed.
	priorOnly := seedLoad("prior-only-load", "203", 0)
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_prior_outcomes (tenant_id, load_id, outcome, animal_count)
VALUES ($1::uuid, $2::uuid, 'sold', 4)`, countsTenant, priorOnly); err != nil {
		t.Fatalf("seed prior outcome: %v", err)
	}

	refs := func(parks []string) map[string]int64 {
		t.Helper()
		got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ParkIDs: parks, Limit: 50})
		if err != nil {
			t.Fatalf("GetCountsBreakdown(%v): %v", parks, err)
		}
		out := map[string]int64{}
		for _, l := range got.Loads {
			out[l.LoadRef] = l.Purchased
		}
		return out
	}

	if got := refs(nil); !reflect.DeepEqual(got, map[string]int64{"201": 5}) {
		t.Errorf("all parks: loads = %v, want only load 201, the one with animals on the farm", got)
	}
	// Bought is the load's own total and does not shrink with the park.
	if got := refs([]string{countsPark}); !reflect.DeepEqual(got, map[string]int64{"201": 5}) {
		t.Errorf("park one: loads = %v, want only load 201 at bought 5", got)
	}
	if got := refs([]string{countsParkTwo}); len(got) != 0 {
		t.Errorf("park two: loads = %v, want none: its only load has sold out", got)
	}

	loadRefs := func(q domain.CountsBreakdownQuery) []string {
		t.Helper()
		q.TenantID = countsTenant
		got, err := repo.GetCountsBreakdown(ctx, q)
		if err != nil {
			t.Fatalf("GetCountsBreakdown(%+v): %v", q, err)
		}
		var out []string
		for _, l := range got.Loads {
			out = append(out, l.LoadRef)
		}
		return out
	}

	// StatusMatrix: a load is listed when it has an animal in the lifecycle bucket being read, and
	// only then -- the live default leaves the sold-out 202 off, the sold bucket lists it, and the
	// dead bucket (no dead animal) lists nothing.
	t.Run("StatusMatrixListsALoadOnlyForTheBucketItHasAnimalsIn", func(t *testing.T) {
		for status, want := range map[string][]string{"alive": nil, "sold": {"202"}, "dead": nil} {
			status := status
			if got := loadRefs(domain.CountsBreakdownQuery{ParkIDs: []string{countsParkTwo}, LifecycleStatus: &status, Limit: 50}); !reflect.DeepEqual(got, want) {
				t.Errorf("park two under %s = %v, want %v", status, got, want)
			}
		}
	})

	// Pagination: the pen table's limit/offset never touch which loads a park lists.
	t.Run("PaginationOfThePenTableLeavesTheParkLoadsWhole", func(t *testing.T) {
		whole := loadRefs(domain.CountsBreakdownQuery{ParkIDs: []string{countsPark}, Limit: 50})
		paged := loadRefs(domain.CountsBreakdownQuery{ParkIDs: []string{countsPark}, GroupByPen: true, Limit: 1, Offset: 1})
		if !reflect.DeepEqual(paged, whole) || !reflect.DeepEqual(whole, []string{"201"}) {
			t.Errorf("park one loads paged = %v, whole = %v, want [201] both", paged, whole)
		}
	})

	// OneToMany: an animal accepted onto TWO loads lands on exactly one (the latest acceptance, the
	// same DISTINCT ON the Sales load read uses). This live park-two animal puts 202 back on park
	// two's card, and its older acceptance onto 201 cannot drag 201 there too.
	t.Run("OneToManyAcceptanceRowsPlaceALoadOnce", func(t *testing.T) {
		insertBreakdownGoat(t, ctx, pool, goatUUID(7), goatDisplayID(7),
			"male", "Beetal", "alive", "Fattening", strp(countsParkTwo), strp(countsShedCastroTwo), nil)
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'accepted_herd_intake', 'accepted_herd_intake', '2026-07-01T10:00:00Z'::timestamptz),
       ($1::uuid, $3::uuid, $4::uuid, 'accepted_herd_intake', 'accepted_herd_intake', '2026-08-02T10:00:00Z'::timestamptz)`,
			countsTenant, parkOneLoad, parkTwoLoad, goatUUID(7)); err != nil {
			t.Fatalf("seed double acceptance: %v", err)
		}
		if got := loadRefs(domain.CountsBreakdownQuery{ParkIDs: []string{countsParkTwo}, Limit: 50}); !reflect.DeepEqual(got, []string{"202"}) {
			t.Errorf("park two = %v, want [202]: the animal's older acceptance onto 201 must not place 201 in park two", got)
		}
	})
}

// With a park selected, the Stage and Breed dropdowns offer only what that park holds. They used to
// list the whole tenant's vocabulary, so a CBE-only stage or breed stayed on a CPT page and picking
// it returned an empty table. The Farm dropdown itself is never narrowed by the park (a facet must
// not filter by its own dimension), so the reader can always switch park.
func TestCountsBreakdownStageAndBreedOptionsFollowTheSelectedPark(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)

	insertBreakdownGoat(t, ctx, pool, goatUUID(0), goatDisplayID(0), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedCastroOne), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(1), goatDisplayID(1), "female", "Malai", "alive", "Mother", strp(countsParkTwo), strp(countsShedCastroTwo), nil)

	keys := func(points []domain.CountsBreakdownSeriesPoint) map[string]bool {
		out := map[string]bool{}
		for _, p := range points {
			out[p.Key] = true
		}
		return out
	}
	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ParkIDs: []string{countsPark}, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	stages, breeds, parks := keys(got.Facets.Stages), keys(got.Facets.Breeds), keys(got.Facets.Parks)
	if !stages["K1"] || stages["Mother"] {
		t.Errorf("stage options for park one = %v, want K1 and not the other park's Mother", stages)
	}
	if !breeds["Beetal"] || breeds["Malai"] {
		t.Errorf("breed options for park one = %v, want Beetal and not the other park's Malai", breeds)
	}
	if !parks[countsPark] || !parks[countsParkTwo] {
		t.Errorf("farm options = %v, want both parks: the park facet never filters by the park", parks)
	}

	all, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown(all): %v", err)
	}
	if s, b := keys(all.Facets.Stages), keys(all.Facets.Breeds); !s["K1"] || !s["Mother"] || !b["Beetal"] || !b["Malai"] {
		t.Errorf("all parks: stages %v breeds %v, want both parks' values", s, b)
	}

	// MultipleDimensions: the park narrows the options' COUNTS too, and a second animal of the
	// same stage in the other park never inflates this park's option.
	t.Run("MultipleDimensionsCountOnlyTheSelectedPark", func(t *testing.T) {
		insertBreakdownGoat(t, ctx, pool, goatUUID(2), goatDisplayID(2), "male", "Beetal", "alive", "K1", strp(countsParkTwo), strp(countsShedCastroTwo), nil)
		got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, ParkIDs: []string{countsPark}, Limit: 50})
		if err != nil {
			t.Fatalf("GetCountsBreakdown: %v", err)
		}
		for _, p := range append(append([]domain.CountsBreakdownSeriesPoint{}, got.Facets.Stages...), got.Facets.Breeds...) {
			if (p.Key == "K1" || p.Key == "Beetal") && p.Count != 1 {
				t.Errorf("option %s counts %d in park one, want 1", p.Key, p.Count)
			}
		}
	})
}
