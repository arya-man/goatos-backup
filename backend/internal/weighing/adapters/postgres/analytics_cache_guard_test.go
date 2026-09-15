package postgres

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestWeighingAnalyticsCacheOneToManyPageBoundaryParkScopeStatusMatrix(t *testing.T) {
	for _, file := range []string{"growth.go", "weight_demographics.go", "shed_weights.go"} {
		src := readSource(t, file)
		for _, required := range []string{
			"weighingAnalyticsCacheKey(",
			"tenantID",
			"periodStart",
			"periodEnd",
			"sex",
			"origin",
			"weighingCategory",
			"getReadCache(",
			"cacheEpoch := r.readCacheEpoch()",
			"setReadCacheIfEpoch(",
		} {
			if !strings.Contains(src, required) {
				t.Fatalf("%s cache guard missing %q", file, required)
			}
		}
	}
	src := readSource(t, "shed_weights.go")
	for _, required := range []string{"selectedParkID", "saleThresholdToleranceKg"} {
		if !strings.Contains(src, required) {
			t.Fatalf("shed_weights cache key must include %q", required)
		}
	}
}

func TestWeighingAnalyticsCacheInvalidationUsesEpoch(t *testing.T) {
	src := readSource(t, "growth.go")
	for _, required := range []string{
		"const weighingAnalyticsCacheTTL = 2 * time.Minute",
		"func (r *Repository) readCacheEpoch() uint64",
		"func (r *Repository) setReadCacheIfEpoch(",
		"if r.cacheEpoch != epoch",
		"r.cacheEpoch++",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("weighing cache epoch guard missing %q", required)
		}
	}
}

func TestWeighingAnalyticsReadsUseRepositoryTimeout(t *testing.T) {
	for _, tc := range []struct {
		file       string
		entrypoint string
	}{
		{file: "growth.go", entrypoint: "func (r *Repository) GetLeadershipGrowthADG("},
		{file: "shed_weights.go", entrypoint: "func (r *Repository) GetShedWeights("},
		{file: "weight_demographics.go", entrypoint: "func (r *Repository) GetWeightDemographics("},
	} {
		src := readSource(t, tc.file)
		start := strings.Index(src, tc.entrypoint)
		if start < 0 {
			t.Fatalf("%s missing analytics entrypoint %q", tc.file, tc.entrypoint)
		}
		next := strings.Index(src[start+len(tc.entrypoint):], "\nfunc (r *Repository) ")
		body := src[start:]
		if next >= 0 {
			body = src[start : start+len(tc.entrypoint)+next]
		}
		if !strings.Contains(body, "ctx, cancel := r.timeout(ctx)") || !strings.Contains(body, "defer cancel()") {
			t.Fatalf("%s %s must wrap heavy analytics DB work in the repository timeout", tc.file, tc.entrypoint)
		}
	}
}

func TestGrowthAnalyticsReadFanoutIsBounded(t *testing.T) {
	src := readSource(t, "growth.go")
	for _, required := range []string{
		"const weighingGrowthReadParallelism = 3",
		"analyticsSlots := make(chan struct{}, weighingGrowthReadParallelism)",
		"case analyticsSlots <- struct{}{}:",
		"runGrowthRead(&wg, errs, fn)",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("growth analytics fanout guard missing %q", required)
		}
	}
}

func TestGrowthADGSectionsOneToManyPageBoundaryParkScopeStatusMatrix(t *testing.T) {
	all := growthADGSectionSet("")
	for _, section := range []string{
		"headline",
		"sale_readiness",
		"rejected",
		"eligibility",
		"trend",
		"weekly_gain",
		"shed_leaderboard",
		"distribution",
		"lump_sum",
		"parks",
		"losing_animals",
		"by_park",
	} {
		if !all[section] {
			t.Fatalf("default growth ADG read must keep legacy full-payload section %q", section)
		}
	}

	narrow := growthADGSectionSet("rejected,shed_leaderboard,parks,losing_animals")
	for _, section := range []string{"rejected", "shed_leaderboard", "parks", "losing_animals"} {
		if !narrow[section] {
			t.Fatalf("narrowed growth ADG read dropped requested section %q", section)
		}
	}
	for _, section := range []string{"sale_readiness", "eligibility", "trend", "weekly_gain", "distribution", "lump_sum", "by_park"} {
		if narrow[section] {
			t.Fatalf("narrowed growth ADG read must not run unrequested section %q", section)
		}
	}
	if growthADGSectionKey(narrow) != "rejected,shed_leaderboard,parks,losing_animals" {
		t.Fatalf("section cache key must be stable, got %q", growthADGSectionKey(narrow))
	}
	timeOnly := growthADGSectionSet("weekly_gain")
	if timeOnly["headline"] {
		t.Fatal("time-only growth ADG read must not compute the headline")
	}
	if growthADGSectionKey(timeOnly) != "weekly_gain" {
		t.Fatalf("time-only section cache key must be stable, got %q", growthADGSectionKey(timeOnly))
	}
}

func TestGrowthADGHeadlineSectionDoesNotLoadHiddenLosingList(t *testing.T) {
	src := readSource(t, "growth.go")
	for _, required := range []string{
		`if sectionSet["headline"] || sectionSet["rejected"]`,
		`if sectionSet["losing_animals"]`,
		"headline.RejectedObservationCount = rejected",
		"headline.LosingAnimalCount = len(losing)",
		"func growthADGLosingAnimals(",
		`if !sectionSet["losing_animals"]`,
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("growth ADG must load the losing list only for the explicit losing_animals section; missing %q", required)
		}
	}
	if strings.Contains(src, `if sectionSet["headline"] || sectionSet["losing_animals"]`) {
		t.Fatal("headline-only growth ADG reads must not pay for the hidden losing-animals list")
	}
}

func TestWeighingAnalyticsCacheCoversBrowserSmokeSweep(t *testing.T) {
	if weighingAnalyticsCacheTTL < 2*time.Minute {
		t.Fatalf("weighing analytics cache TTL %s is too short for the local browser route sweep", weighingAnalyticsCacheTTL)
	}
}

func TestWeightDemographicsSectionedReadsGateProducerCTEs(t *testing.T) {
	src := readSource(t, "weight_demographics.go")
	for _, required := range []string{
		`cacheKey := weighingAnalyticsCacheKey("weight_demographics:"+sectionKey`,
		`query := weightDemographicsPruneInactiveSectionSelects(q, sectionSet)`,
		`QueryRow(ctx, query`,
		`needLatest := sectionSet["composition"] || sectionSet["dimensions"] || sectionSet["shed_type"] || sectionSet["weight_bands"]`,
		`needLump := sectionSet["composition"] || sectionSet["dimensions"] || sectionSet["weight_bands"]`,
		`needGain := sectionSet["dimensions"] || sectionSet["origin"] || sectionSet["shed_type"] || sectionSet["weight_bands"] || sectionSet["gain_thresholds"]`,
		`needWeeklyGain := sectionSet["weekly_gain"]`,
		`AND $26::bool`,
		`AND ($28::bool OR $30::bool)`,
		`FROM paired WHERE $28::bool`,
		`WHERE $27::bool`,
		`WHERE $29::bool`,
		`WHERE $30::bool`,
		`WHERE $22::bool`,
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("weight demographics section fast path missing %q", required)
		}
	}
}

func TestWeightDemographicsPrunesInactiveSectionSelectReferences(t *testing.T) {
	query := `SELECT
  CASE WHEN $21::bool THEN (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, origin, n, g) ORDER BY breed, origin), '[]'::jsonb)
     FROM (
       SELECT breed, origin, sum(n)::bigint n, (sum(gsum) / NULLIF(sum(n), 0))::float8 g FROM (
         SELECT breed, origin, count(*)::bigint n, sum(g)::float8 gsum
         FROM (
           SELECT rg.breed, rg.g,
                  CASE WHEN rg.tag = ANY($10::text[]) THEN 'farm_born'
                       WHEN rg.tag = ANY($11::text[]) THEN 'purchased' END AS origin
           FROM resolved_gain rg WHERE rg.breed IS NOT NULL
         ) scanned
         WHERE origin IS NOT NULL
         GROUP BY breed, origin
         UNION ALL
         SELECT sc.breed, pen.origin, sum(ls.animals)::bigint, sum(ls.animals * ls.g_per_day)::float8
         FROM lump_span ls
         JOIN shed_cohort sc
           ON sc.location_id = ls.location_id AND sc.partition_label = ls.partition_label
         JOIN LATERAL (
           SELECT CASE
             WHEN EXISTS (SELECT 1 FROM unnest($12::uuid[], $13::text[]) AS fb(loc, part)
                          WHERE fb.loc = ls.location_id AND fb.part = ls.partition_label) THEN 'farm_born'
             WHEN EXISTS (SELECT 1 FROM unnest($14::uuid[], $15::text[]) AS pu(loc, part)
                          WHERE pu.loc = ls.location_id AND pu.part = ls.partition_label) THEN 'purchased'
           END AS origin
         ) pen ON pen.origin IS NOT NULL
         -- Same claim rule as every other whole-shed arm: the pen counts for a reader only when
         -- its cohort is entirely one breed, and entirely the selected sex.
         WHERE sc.breeds = 1 AND ($5::text = '' OR (sc.sexes = 1 AND lower(btrim(sc.sex)) = $5::text))
         GROUP BY sc.breed, pen.origin
       ) parts GROUP BY breed, origin
     ) gbo) ELSE '[]'::jsonb END,
  CASE WHEN $24::bool THEN (SELECT COALESCE(jsonb_agg(jsonb_build_array(breed, week_start, n, g) ORDER BY breed, week_start), '[]'::jsonb)
     FROM (
       SELECT breed, week_start, sum(n)::bigint AS n, (sum(gsum) / NULLIF(sum(n), 0))::float8 AS g
       FROM (
         SELECT gt.breed, aw.week_start, count(*)::bigint AS n, sum(aw.g)::float8 AS gsum
         FROM animal_gain_week aw
         LEFT JOIN ident i ON i.tag = aw.tag
         LEFT JOIN goats gt ON gt.goat_id = i.goat_id AND gt.tenant_id = $1::uuid
         WHERE gt.breed IS NOT NULL AND ($5::text = '' OR lower(btrim(gt.sex)) = $5::text)
         GROUP BY gt.breed, aw.week_start
         UNION ALL
         SELECT sc.breed, pw.week_start, sum(pw.animals)::bigint, sum(pw.animals * pw.g_per_day)::float8
         FROM pen_week pw
         JOIN shed_cohort sc
           ON sc.location_id = pw.location_id AND sc.partition_label = pw.partition_label
         WHERE sc.breeds = 1
           AND ($5::text = '' OR (sc.sexes = 1 AND lower(btrim(sc.sex)) = $5::text))
         GROUP BY sc.breed, pw.week_start
       ) parts GROUP BY breed, week_start
     ) gbw) ELSE '[]'::jsonb END`
	pruned := weightDemographicsPruneInactiveSectionSelects(query, map[string]bool{"weekly_gain": true})
	if strings.Contains(pruned, "resolved_gain") || strings.Contains(pruned, "farm_born") || strings.Contains(pruned, "$21::bool") {
		t.Fatalf("weekly_gain section must not reference inactive origin arm after pruning:\n%s", pruned)
	}
	if !strings.Contains(pruned, "animal_gain_week") || !strings.Contains(pruned, "$24::bool") {
		t.Fatalf("weekly_gain section must keep the weekly arm:\n%s", pruned)
	}
}

func readSource(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
