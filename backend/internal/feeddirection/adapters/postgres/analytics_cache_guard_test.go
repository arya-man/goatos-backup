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
	stock := sourceBlock(t, src, "func (r *Repository) StockAnalytics(")
	for _, required := range []string{
		"const feedPurchaseStockKgSQL = `stock_kg`",
		"const stockRevisionSQL = `",
		"COALESCE(MAX(updated_at)::text, '')",
		"feedStockRevision(ctx, tenantID, parkIDs)",
		`feedAnalyticsCacheKey("stock:"+revision, tenantID, q)`,
		"FROM feed_direction_issues i",
		"COALESCE(MAX(i.updated_at)::text, '')",
		"q.StockSections",
		"delivery_status = 'reached'",
		"p.depletes_from <= di.feed_day",
		"p.total_cost / NULLIF(p.stock_kg, 0)",
		"array_agg(batch_no ORDER BY depletes_from DESC, purchase_date DESC, batch_no DESC)",
		"round(ll.stock_kg, 1)::text AS last_quantity_kg",
		"ORDER BY p.depletes_from DESC, p.purchase_date DESC, p.batch_no DESC",
		"ORDER BY farm_label, feed_item_key, depletes_from DESC, purchase_date DESC, batch_no DESC",
		"feedAnalyticsCacheTTL = 30 * time.Second",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("feed stock correctness guard missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`feedAnalyticsCacheKey("stock", tenantID, q)`,
	} {
		if strings.Contains(stock, forbidden) {
			t.Fatalf("stock analytics must not use an unversioned read cache; purchase delivery updates must be visible immediately")
		}
	}
	if strings.Contains(stock, `feedAnalyticsCacheKey("stock", tenantID, q)`) {
		t.Fatalf("stock analytics must not use the short read cache; purchase delivery updates must be visible immediately")
	}
	for _, forbidden := range []string{
		"stockRevisionCacheTTL",
		"stockRevisionCacheKey",
		"getStockRevisionCache",
		"setStockRevisionCache",
	} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("stock revision must be read fresh before using the stock payload cache; found %q", forbidden)
		}
	}
	stockRevision := sourceBlock(t, src, "const stockRevisionSQL = `")
	for _, forbidden := range []string{"md5(", "string_agg("} {
		if strings.Contains(stockRevision, forbidden) {
			t.Fatalf("stock revision must stay aggregate-only; row hashing caused OCI tail latency via %q", forbidden)
		}
	}
	if strings.Contains(stockRevision, "feed_direction_issue_rows") {
		t.Fatalf("stock revision must not scan issue rows; all row mutations stamp feed_direction_issues.updated_at")
	}
	experiment := sourceBlock(t, src, "func (r *Repository) ExperimentAnalytics(")
	for _, required := range []string{
		`feedAnalyticsCacheKey("experiment", tenantID, q)`,
		"r.setReadCache(cacheKey, out)",
	} {
		if !strings.Contains(experiment, required) {
			t.Fatalf("experiment analytics must use the same bounded feed analytics cache; missing %q", required)
		}
	}
}

func TestFeedAnalyticsTrendQueriesStayNarrow(t *testing.T) {
	src := mustReadAnalyticsSource(t)
	directed := sourceBlock(t, src, "const directedAnalyticsCombinedSQL = `")
	if strings.Contains(directed, "GROUP BY feed_day, feed_item_label, feed_item_key") {
		t.Fatalf("directed item trend must not group the inner rollup by display label")
	}
	if !strings.Contains(directed, "MIN(feed_item_label)") {
		t.Fatalf("directed item trend must pick one label after grouping by stable item key")
	}
	if !strings.Contains(directed, "SELECT * FROM day_rows") || !strings.Contains(directed, "SELECT * FROM item_rows") {
		t.Fatalf("directed analytics must keep day and item trends in one combined read")
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
