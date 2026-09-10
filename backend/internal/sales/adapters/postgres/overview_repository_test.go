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
		"g.management_stage IN ('F2', 'F2-Male', 'F2-Female')",
		"g.milk_cohort = 'K0' OR g.management_stage = 'K0'",
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
