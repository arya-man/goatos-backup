package postgres

import (
	"strings"
	"testing"
)

func TestFarmValuationSQLUsesCurrentInventoryShape(t *testing.T) {
	for _, want := range []string{
		"FROM public.goats g",
		"LEFT JOIN public.locations park",
		"LEFT JOIN public.locations farm",
		"g.lifecycle_status NOT IN ('dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive')",
		// The stage an animal is valued in is AUTHORED (2026-09-24): the CASE that named F2 and K0
		// is replaced by the farm's own rows, matched on the register entries they cover.
		"stage_rules AS (",
		"jsonb_to_recordset(a.stages) AS st(stage text, display_order int, matches jsonb)",
		"jsonb_array_elements_text(st.matches) AS m(match)",
		"total_inventory AS",
		"count(*) FILTER (WHERE bucket <> 'unmapped')::int AS valued_animals",
		"count(*) FILTER (WHERE bucket = 'unmapped')::int AS excluded_animals",
		"jsonb_build_object('label', label, 'count', animal_count)",
		"SELECT DISTINCT ON (i.tenant_id, i.goat_id)",
		"FROM public.goat_identifiers",
		"status = 'active'",
		"identifier_type IN ('animal_identifier_1', 'animal_identifier_2')",
		"lower(btrim(scanned_identifier))",
		"verification_status = 'verified'",
		"450::float8",
		"600::float8",
	} {
		if !strings.Contains(farmValuationSQL, want) {
			t.Fatalf("farm valuation SQL missing %q", want)
		}
	}
	if strings.Contains(farmValuationSQL, "public.farms") {
		t.Fatal("farm valuation must use the locations register; public.farms does not exist in OCI/stg")
	}
	if strings.Contains(farmValuationSQL, "purpose") {
		t.Fatal("farm valuation must use current herd stage, not historical procurement purpose")
	}
	if strings.Contains(farmValuationSQL, "g.lifecycle_status = 'alive'") {
		t.Fatal("farm valuation must not hard-code alive-only; Counts/stg current inventory excludes terminal statuses")
	}
	if strings.Contains(farmValuationSQL, "g.management_stage ILIKE '%%kid%%'") {
		t.Fatal("K0 valuation must be literal K0, not every unhandled kid-like stage such as ICU-Kid")
	}
	if strings.Contains(farmValuationSQL, "animal_identifier_1 AS identifier") {
		t.Fatal("fattening weights must resolve through canonical goat_identifiers, not procurement-load snapshots")
	}
}

func TestFarmValuationSQLMultipleDimensionsWeightsEachAnimalOnce(t *testing.T) {
	for _, want := range []string{
		"SELECT DISTINCT ON (i.tenant_id, i.goat_id)",
		"ORDER BY i.tenant_id, i.goat_id, w.accepted_at DESC",
		"LEFT JOIN goat_weight gw ON gw.tenant_id = g.tenant_id AND gw.goat_id = g.goat_id",
	} {
		if !strings.Contains(farmValuationSQL, want) {
			t.Fatalf("farm valuation SQL missing one-animal weight grain proof %q", want)
		}
	}
	if strings.Count(farmValuationSQL, "JOIN latest_weight") != 1 {
		t.Fatalf("farm valuation should join latest weights once, got %d joins", strings.Count(farmValuationSQL, "JOIN latest_weight"))
	}
}

func TestFarmValuationSQLPageBoundaryTotalsAreWholeInventory(t *testing.T) {
	for _, blocked := range []string{" LIMIT ", " OFFSET ", "FETCH FIRST", "ROW_NUMBER()"} {
		if strings.Contains(strings.ToUpper(farmValuationSQL), blocked) {
			t.Fatalf("farm valuation must be a whole-inventory projection, found page boundary marker %q", blocked)
		}
	}
	for _, want := range []string{
		"total_inventory AS",
		"FROM classified",
		"CROSS JOIN total_inventory ti",
	} {
		if !strings.Contains(farmValuationSQL, want) {
			t.Fatalf("farm valuation SQL missing whole-result total proof %q", want)
		}
	}
}

func TestFarmValuationSQLParkScopeHierarchyUsesLocationCodes(t *testing.T) {
	scopedQuery := farmValuationQuery("CBE")
	for _, want := range []string{
		"LEFT JOIN public.locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id",
		"LEFT JOIN public.locations farm ON farm.tenant_id = g.tenant_id AND farm.location_id = g.farm_id",
		"upper(park.location_code) = upper($2)",
		"park.location_code IS NULL AND upper(farm.location_code) = upper($2)",
	} {
		if !strings.Contains(scopedQuery, want) {
			t.Fatalf("farm valuation SQL missing scope hierarchy proof %q", want)
		}
	}
	if strings.Contains(scopedQuery, "COALESCE(park") || strings.Contains(scopedQuery, "coalesce(park") {
		t.Fatal("farm valuation farm scope must not collapse hierarchy with a generic park/farm COALESCE")
	}
}

func TestFarmValuationSQLEveryStatusBucketsExcludeOnlyTerminalInventory(t *testing.T) {
	for _, want := range []string{
		"g.lifecycle_status NOT IN ('dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive')",
		"g.merged_into_goat_id IS NULL",
		"count(*) FILTER (WHERE bucket <> 'unmapped')::int AS valued_animals",
		"count(*) FILTER (WHERE bucket = 'unmapped')::int AS excluded_animals",
		"not_valued AS",
	} {
		if !strings.Contains(farmValuationSQL, want) {
			t.Fatalf("farm valuation SQL missing status bucket proof %q", want)
		}
	}
	if strings.Contains(farmValuationSQL, "g.lifecycle_status = 'alive'") {
		t.Fatal("farm valuation status matrix must not hard-code alive and silently drop non-terminal live inventory")
	}
}

func TestFarmValuationNotValuedBreakdownAggregateProjectionMultipleDimensionsPageBoundaryParkScopeEveryStatus(t *testing.T) {
	for _, want := range []string{
		"not_valued AS",
		"jsonb_agg(jsonb_build_object('label', label, 'count', animal_count) ORDER BY animal_count DESC, label)",
		"g.management_stage",
		"g.milk_cohort",
		"coalesce(nullif(btrim(management_stage), ''), nullif(btrim(milk_cohort), ''), 'Unmapped') AS label",
		"WHERE bucket = 'unmapped'",
		"CROSS JOIN not_valued nv",
		"nv.breakdown",
	} {
		if !strings.Contains(farmValuationSQL, want) {
			t.Fatalf("farm valuation not-valued breakdown missing aggregate projection proof %q", want)
		}
	}
	if strings.Contains(farmValuationSQL, "LIMIT") || strings.Contains(farmValuationSQL, "OFFSET") {
		t.Fatal("farm valuation not-valued breakdown must stay whole-inventory, not page-local")
	}
}

func TestFarmValuationClassifiedProjectsNotValuedBreakdownInputs(t *testing.T) {
	classifiedStart := strings.Index(farmValuationSQL, "classified AS (")
	notValuedStart := strings.Index(farmValuationSQL, "not_valued AS (")
	if classifiedStart < 0 || notValuedStart < 0 || notValuedStart <= classifiedStart {
		t.Fatal("farm valuation SQL must keep classified before not_valued")
	}
	classified := farmValuationSQL[classifiedStart:notValuedStart]
	for _, want := range []string{
		"g.management_stage",
		"g.milk_cohort",
	} {
		if !strings.Contains(classified, want) {
			t.Fatalf("classified CTE must project %q for the not_valued breakdown", want)
		}
	}
}

// TestFarmValuationClinicalStagesAreValuedThroughTheirCohort pins the 2026-09-10 rule that an
// animal in ICU is still inventory -- and, since 2026-09-24, that the rule is DATA. The three
// branches that used to be CASE arms are now entries in the seeded stage rows, so what this test
// pins is that they are still there and still filed the same way.
func TestFarmValuationClinicalStagesAreValuedThroughTheirCohort(t *testing.T) {
	for _, want := range []string{
		// A plain ICU tag is an adult tag; an ICU kid falls to K2, the maintainer's stated default
		// for a kid whose milk band was lost when it moved to ICU.
		"('adult', 2, 'ICU')",
		"('K2', 5, 'ICUKID')",
		// And a mother is still valued as an adult -- through the adult row, which is now a row the
		// farm can move rather than an arm nobody could reach.
		"('adult', 2, 'MOTHER')",
	} {
		if !strings.Contains(farmValuationSQL, want) {
			t.Fatalf("farm valuation must value clinically housed animals; missing %q", want)
		}
	}
	// AN UNNAMED STAGE IS STILL NOT VALUED, AND STILL SHOWN. This is the property that makes an
	// authored stage list safe: a stage nobody has placed -- Warmup, 58 live kids the day this
	// changed -- stands in the not-valued breakdown asking to be priced, instead of falling into
	// whichever bucket a fallback would have chosen for it. Do not add a fallback.
	if !strings.Contains(farmValuationSQL, "CASE WHEN coalesce(sm.stage, sc.stage) IS NULL THEN 'unmapped'") {
		t.Fatal("an animal in no authored stage must stay unmapped and reach the not-valued breakdown")
	}
	if strings.Contains(farmValuationSQL, "g.age_band = 'adult'") {
		t.Fatal("the age_band catch-all is retired: it is what made an unpriced stage invisible")
	}
}

// TestFarmValuationNormalizesTheClinicalStageOnce pins the normalizer itself. The imported herd
// carries both 'ICU- kid' and 'ICU-Kid', so raw equality would value one spelling and drop the
// other -- the defect migration 000166 already had to repair once for milk cohorts.
func TestFarmValuationNormalizesTheClinicalStageOnce(t *testing.T) {
	const normalizer = "upper(regexp_replace(btrim(coalesce(g.management_stage, '')), '[^A-Za-z0-9]+', '', 'g')) AS stage_norm"
	if !strings.Contains(farmValuationSQL, normalizer) {
		t.Fatal("clinical stage must use the 000166 normalizer so 'ICU- kid' and 'ICU-Kid' are one tag")
	}
	// THREE normalizations, and each is a different SIDE of one comparison rather than a copy of
	// another: the animal's management stage, the animal's milk cohort, and the authored match the
	// two are compared against. A fourth would be a copy, and copies drift apart -- which is the
	// defect 000166 had to repair for milk cohorts.
	if n := strings.Count(farmValuationSQL, "regexp_replace"); n != 3 {
		t.Fatalf("stage normalization must have one definition per side, found %d -- copies drift apart", n)
	}
	if !strings.Contains(farmValuationSQL, "'[^A-Za-z0-9]+', '', 'g')) AS match_norm") {
		t.Fatal("the authored side must be normalized the SAME way, or a stage the farm picks files nothing")
	}
	if !strings.Contains(farmValuationSQL, "CROSS JOIN LATERAL (") {
		t.Fatal("the animal's own normalizer must reach the bucket through a lateral, not inline copies")
	}
	// Literal tags only. An ILIKE over clinical-looking text would sweep in stages nobody named,
	// which is the same trap the K0 branch is already guarded against above.
	if strings.Contains(farmValuationSQL, "ILIKE") {
		t.Fatal("clinical valuation must name its tags literally, never ILIKE over kid-like or clinical-looking text")
	}
}

// TestFarmValuationClinicalStagesMultipleDimensionsPageBoundaryParkScopeEveryStatus is the
// adversarial cover for the clinical branch as an AGGREGATE, not just as a CASE arm. Moving
// animals between buckets changes every count, weight and rupee figure on the Farm Value cards, so
// the four ways a rollup usually breaks are each asserted.
//
// projection-review: membership=public.goats at (tenant_id, goat_id) grain, one row per live
// animal, unchanged by this branch; group_key=the CASE bucket, still exactly one bucket per animal
// because CASE stops at its first true arm, so the clinical arms can only claim animals that would
// otherwise have been 'unmapped' (mother excepted, which is claimed ahead of the sex branches by
// design); join_cardinality=the new CROSS JOIN LATERAL reads NO table -- it is a scalar expression
// over the row's own management_stage -- so it multiplies the goat row by exactly 1 and the
// valued + excluded counts still sum to live_animals; pagination=none, whole-inventory aggregate;
// scope=tenant plus the optional farm predicate and the same park/farm location joins, all of
// which sit after the lateral and are untouched by it.
func TestFarmValuationClinicalStagesMultipleDimensionsPageBoundaryParkScopeEveryStatus(t *testing.T) {
	lateralStart := strings.Index(farmValuationSQL, "CROSS JOIN LATERAL (")
	if lateralStart < 0 {
		t.Fatal("clinical stage normalization must come through a lateral")
	}
	lateralEnd := strings.Index(farmValuationSQL[lateralStart:], ") s")
	if lateralEnd < 0 {
		t.Fatal("lateral must be aliased so the CASE can name it")
	}
	// Slice the BODY, past the "CROSS JOIN LATERAL (" header itself -- the header carries the
	// word JOIN and would otherwise trip the read-no-table check below on every run.
	bodyStart := lateralStart + len("CROSS JOIN LATERAL (")
	lateral := farmValuationSQL[bodyStart : lateralStart+lateralEnd]

	// CARDINALITY. The lateral reads no table, so it returns exactly one row per goat and cannot
	// fan the herd out. A lateral that grew a FROM would double-count every animal it matched
	// twice -- silently inflating both the head counts and the rupee total.
	if strings.Contains(strings.ToUpper(lateral), "FROM") || strings.Contains(strings.ToUpper(lateral), "JOIN") {
		t.Fatalf("the stage lateral must stay a scalar over the goat's own row, never a table read: %s", lateral)
	}

	// PAGE BOUNDARY. The cards are whole-inventory; a window would make the clinical animals
	// appear or vanish with the page rather than with the herd.
	if strings.Contains(farmValuationSQL, "LIMIT") || strings.Contains(farmValuationSQL, "OFFSET") {
		t.Fatal("farm valuation must stay a whole-inventory aggregate")
	}

	// PARK/FARM SCOPE. The lateral is joined before the scope predicates, so those must still be
	// there and still apply to the newly valued animals.
	for _, want := range []string{
		"LEFT JOIN public.locations park ON park.tenant_id = g.tenant_id",
		"LEFT JOIN public.locations farm ON farm.tenant_id = g.tenant_id",
		"WHERE g.tenant_id = $1",
	} {
		idx := strings.Index(farmValuationSQL, want)
		if idx < 0 || idx < lateralStart {
			t.Fatalf("scope predicate %q must survive, and stay after the stage lateral", want)
		}
	}

	// EVERY STATUS. A clinical tag must not resurrect a terminal animal: the CASE lives inside
	// classified, whose WHERE already excludes sold/dead/culled, and the clinical arms add no
	// status branch of their own.
	if !strings.Contains(farmValuationSQL, "g.lifecycle_status NOT IN ('dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive')") {
		t.Fatal("clinical valuation must inherit the terminal-status exclusion, never value a dead or sold animal")
	}
	if strings.Contains(farmValuationSQL, "stage_norm = 'ICU' AND g.lifecycle_status") {
		t.Fatal("the clinical arms must not carry a status rule of their own; classified already owns that")
	}
}

// The fattening and kid cards show the bucket by recorded sex (maintainer request 2026-09-11).
// The three counts are taken in the SAME grouped read as animal_count, over the SAME classified
// rows, so they cannot range over a different herd than the figure they divide.
func TestFarmValuationSQLSexCountsShareOneBucketRollupMultipleDimensions(t *testing.T) {
	for _, want := range []string{
		"lower(btrim(coalesce(g.sex, ''))) AS sex",
		"count(*) FILTER (WHERE sex = 'male')::int AS male_count",
		"count(*) FILTER (WHERE sex = 'female')::int AS female_count",
		"count(*) FILTER (WHERE sex NOT IN ('male', 'female'))::int AS sex_missing_count",
		"coalesce(c.male_count, 0) AS male_count",
		"coalesce(c.sex_missing_count, 0) AS sex_missing_count",
	} {
		if !strings.Contains(farmValuationSQL, want) {
			t.Fatalf("farm valuation SQL missing %q", want)
		}
	}
	// ONE rollup for the counts: a second GROUP BY bucket INSIDE this CTE would let the split and
	// the count drift apart, and a join inside it (say, back to goat_identifiers) would count a
	// double-tagged animal twice. The measured-weight CTE groups by the same key elsewhere and is
	// LEFT JOINed 1:0..1, which is why the whole-file count is no longer the assertion.
	counts := farmValuationSQL[strings.Index(farmValuationSQL, "counts AS ("):strings.Index(farmValuationSQL, "total_inventory AS (")]
	if strings.Count(counts, "GROUP BY bucket") != 1 {
		t.Fatalf("the sex counts must ride one bucket rollup, got %d GROUP BY bucket in counts", strings.Count(counts, "GROUP BY bucket"))
	}
	if strings.Contains(counts, "JOIN") {
		t.Fatalf("the bucket rollup must not join; a fan-out here would inflate the split: %s", counts)
	}
}

// The split is exhaustive over EVERY status the sex column can hold: male, female, and everything
// else (blank, NULL, an unknown token) counted as missing -- never dropped, never folded into a
// side. So the three add up to the bucket's animals by construction.
func TestFarmValuationSQLSexSplitStatusBucketsAreDisjointAndExhaustive(t *testing.T) {
	if !strings.Contains(farmValuationSQL, "coalesce(g.sex, '')") {
		t.Fatal("a NULL sex must normalise to the empty string so it is counted missing, not skipped")
	}
	if !strings.Contains(farmValuationSQL, "FILTER (WHERE sex NOT IN ('male', 'female'))") {
		t.Fatal("missing must be the complement of male and female, not a third literal that leaves gaps")
	}
	if strings.Contains(farmValuationSQL, "sex = 'unknown'") || strings.Contains(farmValuationSQL, "sex = ''") {
		t.Fatal("missing must not be a literal match; an unexpected token would then vanish from all three")
	}
}

// The farm predicate is applied in `classified`, BEFORE the rollup, so the split obeys the same
// CBE/CPT park scope as the figure it divides, and there is no page: the card is whole inventory.
func TestFarmValuationSQLSexSplitObeysParkScopeAndHasNoPageBoundary(t *testing.T) {
	classified := farmValuationSQL[strings.Index(farmValuationSQL, "classified AS ("):strings.Index(farmValuationSQL, "not_valued AS (")]
	if !strings.Contains(classified, "%s") {
		t.Fatal("the farm predicate placeholder must sit inside classified, ahead of every count")
	}
	if strings.Contains(farmValuationSQL, "LIMIT") || strings.Contains(farmValuationSQL, "OFFSET") {
		t.Fatal("the valuation is a whole-inventory read; a page boundary would make the split partial")
	}
}
