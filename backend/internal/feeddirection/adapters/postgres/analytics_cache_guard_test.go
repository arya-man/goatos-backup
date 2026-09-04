package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestFeedAnalyticsCacheOneToManyPageBoundaryParkScopeStatusMatrix(t *testing.T) {
	src := mustReadAnalyticsSource(t)
	for _, required := range []string{
		`feedAnalyticsCacheKey("directed", tenantID, q)`,
		`feedAnalyticsCacheKey("execution", tenantID, q)`,
		`feedAnalyticsCacheKey("shed_feed", tenantID, q)`,
		"q.ParkIDs",
		"q.DateFrom",
		"q.DateTo",
		"q.Sections",
		"q.CompletionStatus",
		"q.PackingVarianceLimit",
		"q.PackingVarianceOffset",
		"q.CompletionLimit",
		"q.CompletionOffset",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("feed analytics cache guard missing %q", required)
		}
	}
	for _, required := range []string{
		`feedAnalyticsCacheKey("stock", tenantID, q)`,
		"feedAnalyticsCacheTTL = 30 * time.Second",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("feed stock burst cache guard missing %q", required)
		}
	}
}

func TestFeedAnalyticsTrendQueriesStayNarrow(t *testing.T) {
	src := mustReadAnalyticsSource(t)
	directed := sourceBlock(t, src, "const directedAnalyticsSQL = `")
	if strings.Contains(directed, "GROUP BY feed_day, feed_item_label, feed_item_key") {
		t.Fatalf("directed item trend must not group the inner rollup by display label")
	}
	if !strings.Contains(directed, "MIN(feed_item_label)") {
		t.Fatalf("directed item trend must pick one label after grouping by stable item key")
	}

	consumption := sourceBlock(t, src, "const executionConsumptionSQL = `")
	for _, forbidden := range []string{
		"JOIN locations lp",
		"JOIN locations ls",
		"feed_config_norm(",
		"breed_label",
		"age_group",
	} {
		if strings.Contains(consumption, forbidden) {
			t.Fatalf("execution consumption trend must stay day-total narrow; found %q", forbidden)
		}
	}
}

func mustReadAnalyticsSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("analytics.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sourceBlock(t *testing.T, src, start string) string {
	t.Helper()
	startIndex := strings.Index(src, start)
	if startIndex < 0 {
		t.Fatalf("source block %q not found", start)
	}
	nextIndex := strings.Index(src[startIndex+len(start):], "\nfunc ")
	if nextIndex < 0 {
		return src[startIndex:]
	}
	return src[startIndex : startIndex+len(start)+nextIndex]
}
