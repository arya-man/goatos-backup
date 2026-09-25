package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// EMPTY PENS (2026-09-25). A pen's authored stage tag decides where a newborn is placed, and the
// only editor for that tag lives on a Counts Breakdown pen line. Pen lines used to be derived from
// animals alone, so a pen holding none -- a new pen, a new park's kid pen -- could never be tagged
// and a birth there could never be placed. These tests pin that every active catalog pen now has
// a line, that the animal totals do not move, and that an animal filter still hides them.
//
// Fixture (one park, CPT): the three default seeded sheds CPT Shed 1/2/3 are undivided and empty.
// On top of them:
//
//	Occupied  -- undivided, 2 live animals, tagged K1 on its shed profile
//	Bare      -- undivided, EMPTY
//	Mandela 1 -- divided: "Part 1" holds 1 animal, "Part 3" is EMPTY and tagged K0 on its pen row
//	Legacy alias row "Mandela 1 - Part 3" -- an old duplicate location, must NOT become a pen
const (
	emptyPensOccupiedShed = "00000000-0000-4000-8000-000000004031"
	emptyPensBareShed     = "00000000-0000-4000-8000-000000004032"
	emptyPensMandelaShed  = "00000000-0000-4000-8000-000000004033"
	emptyPensAliasShed    = "00000000-0000-4000-8000-000000004034"
)

func seedEmptyPensFixture(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	pool := repo.pool
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($2::uuid, $1::uuid, 'shed', 'CPT-OCC', 'Occupied', $6::uuid, 'active'),
  ($3::uuid, $1::uuid, 'shed', 'CPT-BARE', 'Bare', $6::uuid, 'active'),
  ($4::uuid, $1::uuid, 'shed', 'CPT-MANDELA-1', 'Mandela 1', $6::uuid, 'active'),
  ($5::uuid, $1::uuid, 'shed', 'CPT-MANDELA-1-P3', 'Mandela 1 - Part 3', $6::uuid, 'active')`,
		countsTenant, emptyPensOccupiedShed, emptyPensBareShed, emptyPensMandelaShed, emptyPensAliasShed, countsPark); err != nil {
		t.Fatalf("seed sheds: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, status, sort_order)
VALUES ($1::uuid, 'K0', 'Newborn kids', 'kid', 'active', 1),
       ($1::uuid, 'K1', 'Kids', 'kid', 'active', 2)
ON CONFLICT (tenant_id, stage_code) DO NOTHING`, countsTenant); err != nil {
		t.Fatalf("seed stages: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source, animal_stage_id)
VALUES
  ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual', NULL),
  ($1::uuid, $2::uuid, 'Part 3', '3', 'active', 'manual',
   (SELECT animal_stage_id FROM animal_stage_lookup WHERE tenant_id = $1::uuid AND stage_code = 'K0'))`,
		countsTenant, emptyPensMandelaShed); err != nil {
		t.Fatalf("seed partitions: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, row_version)
SELECT $2::uuid, $1::uuid, animal_stage_id, 1 FROM animal_stage_lookup WHERE tenant_id = $1::uuid AND stage_code = 'K1'`,
		countsTenant, emptyPensOccupiedShed); err != nil {
		t.Fatalf("seed shed profile: %v", err)
	}
	insertBreakdownGoat(t, ctx, pool, goatUUID(1), goatDisplayID(1), "female", "Beetal", "alive", "K1", strp(countsPark), strp(emptyPensOccupiedShed), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(2), goatDisplayID(2), "male", "Beetal", "alive", "K1", strp(countsPark), strp(emptyPensOccupiedShed), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(3), goatDisplayID(3), "female", "Sirohi", "alive", "K2", strp(countsPark), strp(emptyPensMandelaShed), nil)
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Part 1', 'seed')`, countsTenant, goatUUID(3), emptyPensMandelaShed); err != nil {
		t.Fatalf("seed goat partition: %v", err)
	}
}

func penByDisplay(pens []domain.CountsBreakdownPenRow) map[string]domain.CountsBreakdownPenRow {
	out := map[string]domain.CountsBreakdownPenRow{}
	for _, p := range pens {
		out[p.OperationalLocationDisplay] = p
	}
	return out
}

func TestCountsBreakdownEmptyPensOneToManyCatalogJoinListsEachPenOnceWithItsAuthoredTag(t *testing.T) {
	ctx := context.Background()
	repo, _ := newBreakdownRepo(t, ctx)
	seedEmptyPensFixture(t, ctx, repo)

	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, GroupByPen: true, Limit: 50})
	if err != nil {
		t.Fatalf("GetCountsBreakdown: %v", err)
	}
	pens := penByDisplay(got.Pens)
	// Occupied, Mandela 1 - Part 1 (animals) + Bare, Mandela 1 - Part 3, CPT Shed 1/2/3 (empty).
	want := []string{"Occupied", "Mandela 1 - Part 1", "Bare", "Mandela 1 - Part 3", "CPT Shed 1", "CPT Shed 2", "CPT Shed 3"}
	if len(got.Pens) != len(want) || got.TotalRows != int64(len(want)) {
		t.Fatalf("pens=%d total_rows=%d, want %d: %v", len(got.Pens), got.TotalRows, len(want), got.Pens)
	}
	for _, name := range want {
		if _, ok := pens[name]; !ok {
			t.Errorf("pen %q missing from the pen table; got %v", name, pens)
		}
	}
	for name := range pens {
		if name == "Mandela 1 - Part 3 - Part 3" || name == "Mandela 1" || name == "Bare whole" {
			t.Errorf("pen rendered as %q -- partition identity doubled, missing or leaked", name)
		}
	}
	// The legacy alias location never becomes a pen line of its own.
	for _, p := range got.Pens {
		if p.ShedID != nil && *p.ShedID == emptyPensAliasShed {
			t.Errorf("legacy alias location listed as a pen: %+v", p)
		}
	}

	// Totals stay animal-grain: three animals, whatever the empty pens.
	if got.TotalCount != 3 || got.TotalKids+got.TotalAdults != 3 {
		t.Errorf("total_count=%d kids+adults=%d, want 3 (empty pens add nothing)", got.TotalCount, got.TotalKids+got.TotalAdults)
	}

	empty := pens["Mandela 1 - Part 3"]
	if empty.Count != 0 || empty.KidCount != 0 || empty.AdultCount != 0 {
		t.Errorf("empty pen counts=%d/%d/%d, want 0", empty.Count, empty.KidCount, empty.AdultCount)
	}
	if empty.Rows == nil || len(empty.Rows) != 0 || empty.Stages == nil || len(empty.Stages) != 0 {
		t.Errorf("empty pen rows=%v stages=%v, want non-nil empty slices", empty.Rows, empty.Stages)
	}
	if empty.PartitionLabel != "Part 3" {
		t.Errorf("empty pen partition_label=%q, want the human label 'Part 3'", empty.PartitionLabel)
	}
	if empty.AuthoredStage != "K0" {
		t.Errorf("empty pen authored_stage=%q, want K0 from its own pen row", empty.AuthoredStage)
	}
	bare := pens["Bare"]
	if bare.PartitionLabel != "" || bare.AuthoredStage != "" || bare.Count != 0 {
		t.Errorf("empty undivided shed = %+v, want no partition, no tag, 0 count", bare)
	}
	if occ := pens["Occupied"]; occ.AuthoredStage != "K1" || occ.Count != 2 {
		t.Errorf("occupied undivided shed authored_stage=%q count=%d, want K1 (shed profile) and 2", occ.AuthoredStage, occ.Count)
	}
	if p1 := pens["Mandela 1 - Part 1"]; p1.Count != 1 || p1.AuthoredStage != "" {
		t.Errorf("occupied pen = %+v, want 1 animal and no authored tag", p1)
	}

	// Pagination: the pager walks every pen, the animal totals are unchanged on every page.
	var walked int
	for off := int32(0); off < int32(len(want)); off += 3 {
		page, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, GroupByPen: true, Limit: 3, Offset: off})
		if err != nil {
			t.Fatalf("page at %d: %v", off, err)
		}
		if page.TotalRows != int64(len(want)) || page.TotalCount != 3 {
			t.Errorf("offset %d: total_rows=%d total_count=%d, want %d/3", off, page.TotalRows, page.TotalCount, len(want))
		}
		walked += len(page.Pens)
	}
	if walked != len(want) {
		t.Errorf("paging walked %d pens, want %d", walked, len(want))
	}

	// The combination table and the charts remain animal-grain: no zero grain rows, no zero bars.
	grain, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, Limit: 50})
	if err != nil {
		t.Fatalf("grain page: %v", err)
	}
	if grain.TotalCount != 3 {
		t.Errorf("grain total_count=%d, want 3", grain.TotalCount)
	}
	for _, r := range grain.Items {
		if r.Count == 0 {
			t.Errorf("combination table carries a zero row: %+v", r)
		}
	}
	var shedBars int64
	for _, bar := range grain.Charts.Shed {
		if bar.Count == 0 {
			t.Errorf("pen chart carries a zero bar: %+v", bar)
		}
		shedBars += bar.Count
	}
	if shedBars != grain.TotalCount {
		t.Errorf("pen chart sums to %d, want total_count %d", shedBars, grain.TotalCount)
	}

	// The filter dropdown offers the empty undivided shed.
	var offered bool
	for _, f := range got.Facets.Sheds {
		if f.ShedID == emptyPensBareShed && f.PartitionLabel == "" && f.Count == 0 && f.ParkID == countsPark && f.Label == "Bare" {
			offered = true
		}
		if f.ShedID == emptyPensAliasShed {
			t.Errorf("legacy alias location offered as a filter option: %+v", f)
		}
	}
	if !offered {
		t.Errorf("empty undivided shed 'Bare' is not a filter option; facets=%+v", got.Facets.Sheds)
	}
}

func TestCountsBreakdownEmptyPensStatusMatrixAndAnimalFiltersHideThem(t *testing.T) {
	ctx := context.Background()
	repo, _ := newBreakdownRepo(t, ctx)
	seedEmptyPensFixture(t, ctx, repo)

	for name, q := range map[string]domain.CountsBreakdownQuery{
		"stage": {ManagementStages: []string{"K1"}},
		"breed": {Breeds: []string{"Beetal"}},
		"sex":   {Sexes: []string{"female"}},
	} {
		q.TenantID, q.GroupByPen, q.Limit = countsTenant, true, 50
		got, err := repo.GetCountsBreakdown(ctx, q)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, p := range got.Pens {
			if p.Count == 0 {
				t.Errorf("%s filter lists an empty pen %q", name, p.OperationalLocationDisplay)
			}
		}
		if got.TotalRows != int64(len(got.Pens)) {
			t.Errorf("%s: total_rows=%d but %d pens listed", name, got.TotalRows, len(got.Pens))
		}
	}

	// A farm/pen filter is NOT an animal filter: selecting the empty pen lists exactly it.
	got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
		TenantID: countsTenant, GroupByPen: true, Limit: 50,
		Pens: []domain.CountsBreakdownPen{{ShedID: emptyPensMandelaShed, PartitionLabel: "3"}},
	})
	if err != nil {
		t.Fatalf("pen filter: %v", err)
	}
	if len(got.Pens) != 1 || got.Pens[0].OperationalLocationDisplay != "Mandela 1 - Part 3" || got.TotalCount != 0 {
		t.Errorf("pen filter on the empty pen = %+v total=%d, want exactly Mandela 1 - Part 3 with 0 animals", got.Pens, got.TotalCount)
	}

	// A dead-animal view is an animal filter too: no empty pens.
	dead := "dead"
	got, err = repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{TenantID: countsTenant, GroupByPen: true, Limit: 50, LifecycleStatus: &dead})
	if err != nil {
		t.Fatalf("dead: %v", err)
	}
	if len(got.Pens) != 0 || got.TotalRows != 0 {
		t.Errorf("dead lifecycle lists %d pens (total_rows %d), want none", len(got.Pens), got.TotalRows)
	}
}

// Park scope: an empty pen belongs to its shed's own park. Filtering to another park must not
// list it, filtering to its park must, and the totals stay the animal count of that scope.
func TestCountsBreakdownEmptyPensParkScopeFollowsTheFarmFilter(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	seedEmptyPensFixture(t, ctx, repo)
	seedSameNamedShedsInTwoParks(t, ctx, pool) // adds an empty "Castro 1" under each park

	for _, tc := range []struct {
		park      string
		wantEmpty []string
		absent    []string
		animals   int64
	}{
		{countsPark, []string{"Bare", "Mandela 1 - Part 3", "Castro 1"}, nil, 3},
		{countsParkTwo, []string{"Castro 1"}, []string{"Bare", "Mandela 1 - Part 3", "Occupied"}, 0},
	} {
		got, err := repo.GetCountsBreakdown(ctx, domain.CountsBreakdownQuery{
			TenantID: countsTenant, GroupByPen: true, ParkIDs: []string{tc.park}, Limit: 50,
		})
		if err != nil {
			t.Fatalf("park %s: %v", tc.park, err)
		}
		pens := penByDisplay(got.Pens)
		for _, name := range tc.wantEmpty {
			if p, ok := pens[name]; !ok || p.Count != 0 {
				t.Errorf("park %s: empty pen %q missing or counted: %+v", tc.park, name, p)
			}
		}
		for _, name := range tc.absent {
			if _, ok := pens[name]; ok {
				t.Errorf("park %s: pen %q from the other park leaked in", tc.park, name)
			}
		}
		for _, p := range got.Pens {
			if ptrValue(p.ParkID) != tc.park {
				t.Errorf("park %s: pen %q carries park %q", tc.park, p.OperationalLocationDisplay, ptrValue(p.ParkID))
			}
		}
		if got.TotalCount != tc.animals || got.TotalRows != int64(len(got.Pens)) {
			t.Errorf("park %s: total_count=%d total_rows=%d pens=%d, want %d animals and one row per listed pen",
				tc.park, got.TotalCount, got.TotalRows, len(got.Pens), tc.animals)
		}
	}
}
