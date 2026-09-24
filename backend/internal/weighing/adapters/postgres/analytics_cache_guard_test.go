package postgres

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/readcache"
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
			"readcache.Load(ctx, r.cache, analyticsReadKey(tenantID,",
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

func TestWeighingWritesPublishScopedEvictionInsideTheTransaction(t *testing.T) {
	src := readSource(t, "growth.go")
	start := strings.Index(src, "func (r *Repository) commitAndEvict(")
	if start < 0 {
		t.Fatal("growth.go missing commitAndEvict")
	}
	body := src[start:]
	notify := strings.Index(body, "readcache.NotifyTx(ctx, tx, tenantID, parks...)")
	commit := strings.Index(body, "tx.Commit(ctx)")
	evict := strings.Index(body, "r.cache.Evict(ctx, tenantID, parks...)")
	if notify < 0 || commit < 0 || evict < 0 || !(notify < commit && commit < evict) {
		t.Fatal("commitAndEvict must NOTIFY inside the transaction, commit, then evict locally (tenant+park scoped)")
	}
	// Every weighing write path that changes an analytics input publishes a scoped eviction; only
	// the kernel cadence claims (worker-only, no analytics input) keep the local-only helper.
	for _, file := range []string{"close.go", "repository.go", "verification_verdict.go", "weight_correction.go", "rework_digest.go"} {
		if strings.Contains(readSource(t, file), "commitAndInvalidateReadCache") {
			t.Fatalf("%s still uses the removed process-local invalidation", file)
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

func TestShedWeightsSupportReadsDoNotFanOutPoolSlots(t *testing.T) {
	src := readSource(t, "shed_weights.go")
	start := strings.Index(src, "func (r *Repository) GetShedWeights(")
	if start < 0 {
		t.Fatal("shed_weights.go missing GetShedWeights")
	}
	body := src[start:]
	if strings.Contains(body, "go func()") || strings.Contains(body, "sync.WaitGroup") {
		t.Fatal("GetShedWeights must keep support DB reads serial so one request cannot hold multiple pool slots during ADG bursts")
	}
	for _, required := range []string{
		"sexScope, scopeErr = r.resolveSexScope(",
		"originScope, originErr = r.resolveOriginScope(",
		"idMap, idErr = r.resolveAnimalIdentityMap(",
		"dates, err = r.weighingDates(",
		"byLoad, err = r.loadAnimalWindow(",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("GetShedWeights serial-read guard missing %q", required)
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

func TestGrowthLosingAnimalsPairsAcrossReportStartBoundary(t *testing.T) {
	src := readSource(t, "growth.go")
	start := strings.Index(src, "func (r *Repository) growthLosingAnimals(")
	if start < 0 {
		t.Fatal("missing growthLosingAnimals")
	}
	body := src[start:]
	for _, required := range []string{
		`growthPairsCTE`,
		`FROM qualifying`,
		`WHERE accepted_at >= $5::timestamptz`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("losing animals must pair from lookback and filter on the later report-period weigh; missing %q", required)
		}
	}
	if strings.Contains(body, `AND wo.accepted_at >= $5::timestamptz`) {
		t.Fatal("losing animals must not filter the pairing input to the selected period before finding previous weighs")
	}
}

func TestWeighingAnalyticsCacheExpiryAndInvalidationRejectStaleResults(t *testing.T) {
	clk := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	opts := readcache.DefaultOptions("test")
	opts.Now = func() time.Time { return clk }
	repo := NewRepository(nil, time.Second).WithReadCache(readcache.New(opts))
	load := func(v string) (string, error) {
		return readcache.Load(context.Background(), repo.cache, analyticsReadKey("t1", []string{"p1"}, "scope"), func(context.Context) (string, error) { return v, nil })
	}
	if v, _ := load("before"); v != "before" {
		t.Fatal("first read not served")
	}
	if v, _ := load("other"); v != "before" {
		t.Fatal("fresh result was not reusable")
	}
	// Expiry must force a fresh read; the length of a browser sweep is not
	// a freshness contract and must never determine the minimum cache TTL.
	clk = clk.Add(31 * time.Second)
	if v, _ := load("after-expiry"); v != "after-expiry" {
		t.Fatal("expired result was served")
	}
	repo.cache.Evict(context.Background(), "t1", "p1")
	if v, _ := load("after-write"); v != "after-write" {
		t.Fatal("evicted result was served")
	}
}

func TestWeightDemographicsSectionedReadsGateProducerCTEs(t *testing.T) {
	src := readSource(t, "weight_demographics.go")
	for _, required := range []string{
		`cacheKey := weighingAnalyticsCacheKey("weight_demographics:"+sectionKey`,
		// The template is rendered for the selected Time-wise bucket (week, or a rolling 30-day
		// block) BEFORE it is pruned, so the pruner still sees the whole query.
		`query := weightDemographicsPruneInactiveSectionSelects(bucketedSQL, sectionSet)`,
		`bound, bindErr := sqlbind.Bind(query,`,
		`if bindErr != nil {`,
		`return domain.WeightDemographics{}, fmt.Errorf("weighing: bind weight demographics query: %w", bindErr)`,
		`QueryRow(ctx, bound.SQL(), bound.Args()...)`,
		`needLatest := sectionSet["composition"] || sectionSet["dimensions"] || sectionSet["shed_type"] || sectionSet["weight_bands"] || sectionSet["weekly_gain"]`,
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

func TestWeightDemographicsWeeklyGainKeepsPenAndLoadProducers(t *testing.T) {
	src := readSource(t, "weight_demographics.go")
	for _, required := range []string{
		`gainPenWeekJSON`,
		`gainLoadWeekJSON`,
		`decodeWeightGainPenWeekBuckets`,
		`decodeWeightGainLoadWeekBuckets`,
		`jsonb_build_array(location_id::text, partition_label, week_start, n, g, shed_name, park_id::text, park_name)`,
		`JOIN latest l ON l.tag = aw.tag`,
		// A load is its animals (maintainer decision 2026-09-24): the per-load weeks come from
		// load_animals.go, on the same animals and legs as every other load chart.
		`r.loadAnimalBuckets(ctx, tenantID, parkIDs, periodStart, periodEnd, sex, origin, weighingCategory, timeScope)`,
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("weekly_gain must produce breed, pen, and load week grids; missing %q", required)
		}
	}
	if strings.Contains(src, `FROM weighing_shed_load_tags`) {
		t.Fatalf("the per-load weeks must not return to the fixed load-to-pen tag list")
	}
}

func TestWeightDemographicsWeeklyGainDoesNotUseBrokenCaseWrappedSubselects(t *testing.T) {
	src := readSource(t, "weight_demographics.go")
	for _, name := range []string{"gbw", "gpw", "glw"} {
		brokenCase := regexp.MustCompile(`\)\s+` + name + `\)\s*\)\s+ELSE\s+'?\[\]'?::jsonb\s+END`)
		if brokenCase.MatchString(src) {
			t.Fatalf("weight demographics SQL must not reintroduce the PR270 broken %s CASE-wrapped subselect", name)
		}
	}
	if strings.Contains(src, `CASE WHEN $19::boolean THEN (SELECT`) {
		t.Fatalf("weight demographics SQL must not reintroduce PR270's unsectioned $19 boolean weekly-grid switch")
	}
}

func TestShedPartitionShortcutOnlyHandlesSectionsItPopulates(t *testing.T) {
	src := readSource(t, "weight_demographics.go")
	for _, required := range []string{
		`shortcutSafe := !sectionSet["weekly_gain"] && !sectionSet["composition"] && !sectionSet["gain_thresholds"] && !sectionSet["shed_type"]`,
		`&& shortcutSafe`,
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("per-shed partition shortcut must not swallow requested unimplemented sections; missing %q", required)
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

func TestWeightDemographicsRealSectionQueriesPruneInactiveResults(t *testing.T) {
	src := readSource(t, "weight_demographics.go")
	queryMatch := regexp.MustCompile(`(?s)const q = \x60(.*?)\x60\n\n\tvar \(`).FindStringSubmatch(src)
	if len(queryMatch) != 2 {
		t.Fatal("could not extract the real weight demographics SQL")
	}

	selectorBySection := map[string]string{
		"composition":     "$19::bool",
		"dimensions":      "$20::bool",
		"origin":          "$21::bool",
		"shed_type":       "$22::bool",
		"weight_bands":    "$23::bool",
		"weekly_gain":     "$24::bool",
		"gain_thresholds": "$25::bool",
	}
	cases := []struct {
		name     string
		sections map[string]bool
	}{
		{name: "default_all", sections: weightDemographicsSectionSet("")},
		{name: "dimensions", sections: weightDemographicsSectionSet("dimensions")},
		{name: "origin", sections: weightDemographicsSectionSet("origin")},
		{name: "shed_type", sections: weightDemographicsSectionSet("shed_type")},
		{name: "weight_bands", sections: weightDemographicsSectionSet("weight_bands")},
		{name: "weekly_gain", sections: weightDemographicsSectionSet("weekly_gain")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query := weightDemographicsPruneInactiveSectionSelects(queryMatch[1], tc.sections)
			for section, selector := range selectorBySection {
				originalCount := strings.Count(queryMatch[1], selector)
				if tc.sections[section] {
					if strings.Count(query, selector) != originalCount {
						t.Fatalf("pruned %s query changed its active %s SELECT references", tc.name, section)
					}
					continue
				}
				if strings.Count(query, selector) >= originalCount {
					t.Fatalf("pruned %s query did not remove inactive %s SELECT references", tc.name, section)
				}
			}
		})
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

// A write on one API instance reaches a sibling only through the NOTIFY feed. Without the feed
// (listener down => cache not coherent) the sibling must still expire within 30 seconds.
func TestAnalyticsSiblingInstanceExpiresWithinThirtySeconds(t *testing.T) {
	clk := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	opts := readcache.DefaultOptions("sibling")
	opts.Now = func() time.Time { return clk }
	reader := NewRepository(nil, time.Second).WithReadCache(readcache.New(opts))
	key := analyticsReadKey("t1", nil, "same-tenant-same-report")
	get := func(v string) string {
		out, _ := readcache.Load(context.Background(), reader.cache, key, func(context.Context) (string, error) { return v, nil })
		return out
	}
	get("before-correction")
	clk = clk.Add(31 * time.Second)
	if get("after-correction") != "after-correction" {
		t.Fatal("sibling API instance still serves pre-correction analytics after 31 seconds")
	}
}

// Every weighing write publishes its eviction: the process-local-only helper is gone, including
// from the kernel cadence claims (carry-over closes buckets, roll-forward moves due dates).
func TestNoWeighingWriteUsesAProcessLocalOnlyInvalidation(t *testing.T) {
	for _, file := range []string{"growth.go", "kernel.go", "close.go", "repository.go", "verification_verdict.go", "weight_correction.go", "rework_digest.go"} {
		if strings.Contains(readSource(t, file), "commitAndInvalidateReadCache") {
			t.Fatalf("%s still commits with a process-local-only invalidation", file)
		}
	}
	if !strings.Contains(readSource(t, "kernel.go"), "r.commitClaimAndEvict(ctx, tx, claimed)") {
		t.Fatal("kernel claims must commit through commitClaimAndEvict")
	}
}
