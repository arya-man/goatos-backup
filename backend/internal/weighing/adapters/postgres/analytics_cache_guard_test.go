package postgres

import (
	"os"
	"strings"
	"testing"
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
		"const weighingAnalyticsCacheTTL = 30 * time.Second",
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

func readSource(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
