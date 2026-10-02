package postgres

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/farmvaluation"
)

// fvSQL is the farm valuation as it RUNS: the statement with the shared pricing fragments
// (farmvaluation) spliced in. Shape assertions read this, not the format string, so they see the
// stage, species and rate SQL the database actually receives.
var fvSQL = farmValuationQuery("")

func TestFarmValuationSQLUsesCurrentInventoryShape(t *testing.T) {
	for _, want := range []string{
		"FROM public.goats g",
		"LEFT JOIN public.locations park",
		"LEFT JOIN public.locations farm",
		"g.lifecycle_status NOT IN ('dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive')",
		// The stage an animal is valued in is AUTHORED (2026-09-24): the CASE that named F2 and K0
		// is replaced by the farm's own rows, matched on the register entries they cover.
		"fv_stage_rules AS (",
		"jsonb_to_recordset(va.stages) AS st(stage text, display_order int, matches jsonb)",
		"jsonb_array_elements_text(st.matches) AS mt(match)",
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
		if !strings.Contains(fvSQL, want) {
			t.Fatalf("farm valuation SQL missing %q", want)
		}
	}
	if strings.Contains(fvSQL, "public.farms") {
		t.Fatal("farm valuation must use the locations register; public.farms does not exist in OCI/stg")
	}
	if strings.Contains(fvSQL, "purpose") {
		t.Fatal("farm valuation must use current herd stage, not historical procurement purpose")
	}
	if strings.Contains(fvSQL, "g.lifecycle_status = 'alive'") {
		t.Fatal("farm valuation must not hard-code alive-only; Counts/stg current inventory excludes terminal statuses")
	}
	if strings.Contains(fvSQL, "g.management_stage ILIKE '%%kid%%'") {
		t.Fatal("K0 valuation must be literal K0, not every unhandled kid-like stage such as ICU-Kid")
	}
	if strings.Contains(fvSQL, "animal_identifier_1 AS identifier") {
		t.Fatal("fattening weights must resolve through canonical goat_identifiers, not procurement-load snapshots")
	}
}

func TestFarmValuationSQLMultipleDimensionsWeightsEachAnimalOnce(t *testing.T) {
	for _, want := range []string{
		"SELECT DISTINCT ON (i.tenant_id, i.goat_id)",
		"ORDER BY i.tenant_id, i.goat_id, w.accepted_at DESC",
		"LEFT JOIN goat_weight gw ON gw.tenant_id = g.tenant_id AND gw.goat_id = g.goat_id",
	} {
		if !strings.Contains(fvSQL, want) {
			t.Fatalf("farm valuation SQL missing one-animal weight grain proof %q", want)
		}
	}
	if strings.Count(fvSQL, "JOIN latest_weight") != 1 {
		t.Fatalf("farm valuation should join latest weights once, got %d joins", strings.Count(fvSQL, "JOIN latest_weight"))
	}
}

func TestFarmValuationSQLPageBoundaryTotalsAreWholeInventory(t *testing.T) {
	for _, blocked := range []string{" LIMIT ", " OFFSET ", "FETCH FIRST", "ROW_NUMBER()"} {
		if strings.Contains(strings.ToUpper(fvSQL), blocked) {
			t.Fatalf("farm valuation must be a whole-inventory projection, found page boundary marker %q", blocked)
		}
	}
	for _, want := range []string{
		"total_inventory AS",
		"FROM classified",
		"CROSS JOIN total_inventory ti",
	} {
		if !strings.Contains(fvSQL, want) {
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
		if !strings.Contains(fvSQL, want) {
			t.Fatalf("farm valuation SQL missing status bucket proof %q", want)
		}
	}
	if strings.Contains(fvSQL, "g.lifecycle_status = 'alive'") {
		t.Fatal("farm valuation status matrix must not hard-code alive and silently drop non-terminal live inventory")
	}
}

func TestFarmValuationNotValuedBreakdownAggregateProjectionMultipleDimensionsPageBoundaryParkScopeEveryStatus(t *testing.T) {
	for _, want := range []string{
		"not_valued AS",
		"jsonb_agg(jsonb_build_object('label', label, 'count', animal_count) ORDER BY animal_count DESC, label)",
		"g.management_stage",
		"g.milk_cohort",
		"ELSE coalesce(nullif(btrim(management_stage), ''), nullif(btrim(milk_cohort), ''), 'Unmapped') END AS label",
		// An animal of neither species is named as such, not under a stage it IS priced in.
		"THEN 'No species recorded'",
		"WHERE bucket = 'unmapped'",
		"CROSS JOIN not_valued nv",
		"nv.breakdown",
	} {
		if !strings.Contains(fvSQL, want) {
			t.Fatalf("farm valuation not-valued breakdown missing aggregate projection proof %q", want)
		}
	}
	if strings.Contains(fvSQL, "LIMIT") || strings.Contains(fvSQL, "OFFSET") {
		t.Fatal("farm valuation not-valued breakdown must stay whole-inventory, not page-local")
	}
}

func TestFarmValuationClassifiedProjectsNotValuedBreakdownInputs(t *testing.T) {
	classifiedStart := strings.Index(fvSQL, "classified AS (")
	notValuedStart := strings.Index(fvSQL, "not_valued AS (")
	if classifiedStart < 0 || notValuedStart < 0 || notValuedStart <= classifiedStart {
		t.Fatal("farm valuation SQL must keep classified before not_valued")
	}
	classified := fvSQL[classifiedStart:notValuedStart]
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
		if !strings.Contains(fvSQL, want) {
			t.Fatalf("farm valuation must value clinically housed animals; missing %q", want)
		}
	}
	// AN UNNAMED STAGE IS STILL NOT VALUED, AND STILL SHOWN. This is the property that makes an
	// authored stage list safe: a stage nobody has placed -- Warmup, 58 live kids the day this
	// changed -- stands in the not-valued breakdown asking to be priced, instead of falling into
	// whichever bucket a fallback would have chosen for it. Do not add a fallback.
	if !strings.Contains(fvSQL, "CASE WHEN COALESCE(fv_sc.stage, fv_sm.stage) IS NULL OR") || !strings.Contains(fvSQL, "THEN 'unmapped'") {
		t.Fatal("an animal in no authored stage must stay unmapped and reach the not-valued breakdown")
	}
	// THE MILK COHORT WINS over the management stage, which is the order the retired CASE read
	// them in: its K1/K2/K3/K0 arms were tested BEFORE its clinical arms, so a kid moved to ICU
	// while the register still knew its milk band stayed valued as that band. Reading the stage
	// first flips exactly that animal from a 3-15 kg kid row to a 40-60 kg adult one, on a herd
	// where nothing carries both today and so nothing would show it.
	cohort := strings.Index(fvSQL, "LEFT JOIN fv_stage_by_match fv_sc")
	stage := strings.Index(fvSQL, "LEFT JOIN fv_stage_by_match fv_sm")
	if cohort < 0 || stage < 0 || cohort > stage {
		t.Fatal("the milk cohort must be matched BEFORE the management stage, as the retired CASE did")
	}
	if strings.Contains(fvSQL, "COALESCE(fv_sm.stage, fv_sc.stage)") {
		t.Fatal("coalesce order decides the precedence: the cohort must come first")
	}
	if strings.Contains(fvSQL, "g.age_band = 'adult'") {
		t.Fatal("the age_band catch-all is retired: it is what made an unpriced stage invisible")
	}
}

// TestFarmValuationNormalizesTheClinicalStageOnce pins the normalizer itself. The imported herd
// carries both 'ICU- kid' and 'ICU-Kid', so raw equality would value one spelling and drop the
// other -- the defect migration 000166 already had to repair once for milk cohorts.
func TestFarmValuationNormalizesTheClinicalStageOnce(t *testing.T) {
	// The animal's management stage and milk cohort are normalized by ONE Go helper
	// (farmvaluation.NormSQL), the authored side by the same pattern inside PricingCTEs. The
	// statement itself writes no normalizer of its own: a hand copy is how 000166 drifted.
	for _, want := range []string{farmvaluation.NormSQL("g.management_stage"), farmvaluation.NormSQL("g.milk_cohort")} {
		if !strings.Contains(fvSQL, want) {
			t.Fatalf("clinical stage must use the 000166 normalizer so 'ICU- kid' and 'ICU-Kid' are one tag: missing %q", want)
		}
	}
	if strings.Contains(farmValuationSQL, "regexp_replace") {
		t.Fatal("the farm valuation statement must not normalize stages itself; use farmvaluation.NormSQL")
	}
	if !strings.Contains(fvSQL, "'[^A-Za-z0-9]+', '', 'g')) AS match_norm") {
		t.Fatal("the authored side must be normalized the SAME way, or a stage the farm picks files nothing")
	}
	if strings.Contains(fvSQL, "ILIKE") {
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
	// CARDINALITY. The stage joins read fv_stage_by_match, one row per register entry (DISTINCT
	// ON), and the rates are one row per bucket (DISTINCT ON): neither can fan the herd out.
	for _, want := range []string{
		"SELECT DISTINCT ON (match_norm) match_norm, stage",
		"SELECT DISTINCT ON (bucket) bucket, label, fixed_weight_kg, price_per_kg, display_order",
	} {
		if !strings.Contains(fvSQL, want) {
			t.Fatalf("a stage or rate join could fan the herd out: missing %q", want)
		}
	}
	// PAGE BOUNDARY. The cards are whole-inventory.
	if strings.Contains(fvSQL, "LIMIT") || strings.Contains(fvSQL, "OFFSET") {
		t.Fatal("farm valuation must stay a whole-inventory aggregate")
	}
	// PARK/FARM SCOPE. The scope joins and the tenant predicate sit in classified, after the stage
	// joins, so they apply to every valued animal.
	joins := strings.Index(fvSQL, "LEFT JOIN fv_stage_by_match fv_sc")
	for _, want := range []string{
		"LEFT JOIN public.locations park ON park.tenant_id = g.tenant_id",
		"LEFT JOIN public.locations farm ON farm.tenant_id = g.tenant_id",
		"WHERE g.tenant_id = $1",
	} {
		idx := strings.Index(fvSQL, want)
		if idx < 0 || idx < joins {
			t.Fatalf("scope predicate %q must survive, and stay after the stage joins", want)
		}
	}
	// EVERY STATUS. A clinical tag must not resurrect a terminal animal.
	if !strings.Contains(fvSQL, "g.lifecycle_status NOT IN ('dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive')") {
		t.Fatal("clinical valuation must inherit the terminal-status exclusion, never value a dead or sold animal")
	}
}

// SPECIES (maintainer decision 2026-10-02): the bucket an animal is priced in names its species,
// the same way Load wise names it -- both read the shared farmvaluation fragments.
func TestFarmValuationPricesBySpeciesThroughTheSharedRule(t *testing.T) {
	if !strings.Contains(fvSQL, farmvaluation.BucketKeySQL(farmvaluation.NormSQL("g.management_stage"), "g.species", "g.sex")) {
		t.Fatal("the bucket must be the shared stage_species_gender key")
	}
	if !strings.Contains(fvSQL, "fattening_sheep_female") || !strings.Contains(fvSQL, "fattening_goat_male") {
		t.Fatal("the seeded fallback must carry a row per species")
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
		if !strings.Contains(fvSQL, want) {
			t.Fatalf("farm valuation SQL missing %q", want)
		}
	}
	// ONE rollup for the counts: a second GROUP BY bucket INSIDE this CTE would let the split and
	// the count drift apart, and a join inside it (say, back to goat_identifiers) would count a
	// double-tagged animal twice. The measured-weight CTE groups by the same key elsewhere and is
	// LEFT JOINed 1:0..1, which is why the whole-file count is no longer the assertion.
	counts := fvSQL[strings.Index(fvSQL, "counts AS ("):strings.Index(fvSQL, "total_inventory AS (")]
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
	if !strings.Contains(fvSQL, "coalesce(g.sex, '')") {
		t.Fatal("a NULL sex must normalise to the empty string so it is counted missing, not skipped")
	}
	if !strings.Contains(fvSQL, "FILTER (WHERE sex NOT IN ('male', 'female'))") {
		t.Fatal("missing must be the complement of male and female, not a third literal that leaves gaps")
	}
	if strings.Contains(fvSQL, "sex = 'unknown'") || strings.Contains(fvSQL, "sex = ''") {
		t.Fatal("missing must not be a literal match; an unexpected token would then vanish from all three")
	}
}

// The farm predicate is applied in `classified`, BEFORE the rollup, so the split obeys the same
// CBE/CPT park scope as the figure it divides, and there is no page: the card is whole inventory.
func TestFarmValuationSQLSexSplitObeysParkScopeAndHasNoPageBoundary(t *testing.T) {
	classified := farmValuationSQL[strings.Index(farmValuationSQL, "classified AS ("):strings.Index(farmValuationSQL, "not_valued AS (")]
	if !strings.Contains(classified, "%[1]s") {
		t.Fatal("the farm predicate placeholder must sit inside classified, ahead of every count")
	}
	if strings.Contains(farmValuationSQL, "LIMIT") || strings.Contains(farmValuationSQL, "OFFSET") {
		t.Fatal("the valuation is a whole-inventory read; a page boundary would make the split partial")
	}
}
