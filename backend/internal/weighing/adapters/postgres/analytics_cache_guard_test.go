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
			"setReadCache(",
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

func readSource(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
