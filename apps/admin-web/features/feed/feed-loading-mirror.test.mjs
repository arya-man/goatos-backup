import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: feed-direction-loading-mirror / feed-analytics-loading-mirror (SK1). /feed/direction and
// /feed/packing draw their two day tiles for every served day (a not-yet-issued day too), so the
// loading twin's tile row never lands on a page without one; loading.tsx and the page fallbacks read
// feed-layout. /feed/analytics loading.tsx and the Overview panel fallback are the same
// feed-analytics-skeletons components, and the page reads its tabs / ranges / KPI size from there.
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("feed direction and packing mirror their pages", () => {
  for (const [page, loading] of [["./feed-direction.tsx", "direction"], ["./feed-packing.tsx", "packing"]]) {
    const src = read(page);
    const skel = read(`../../app/(admin)/feed/${loading}/loading.tsx`);
    assert.doesNotMatch(src, /summary && !lifecycleEmpty \? \(\s*<Grid container/, `${page}: tiles must not hide for an unissued day`);
    assert.equal((src.match(/<Grid size=\{FEED_KPI_SIZE\}>/g) ?? []).length, 2, page);
    assert.match(src, /fallback=\{<KpiRowSkeleton count=\{2\} hint size=\{FEED_KPI_SIZE\} \/>\}/);
    assert.match(skel, /<KpiRowSkeleton count=\{2\} hint size=\{FEED_KPI_SIZE\} \/>/);
    assert.match(skel, /<FilterCardSkeleton fold fields=\{FEED_(DIRECTION|PACKING)_FILTER_FIELDS\} \/>/);
  }
});

test("feed analytics loading is the page's own skeletons", () => {
  const skel = read("../../app/(admin)/feed/analytics/loading.tsx");
  const page = read("./feed-analytics.tsx");
  assert.match(skel, /<FeedAnalyticsStripSkeleton \/>/);
  assert.match(skel, /<FeedAnalyticsOverviewSkeleton \/>/);
  assert.match(page, /overview: <FeedAnalyticsOverviewSkeleton \/>/);
  assert.match(page, /<Grid key=\{kpi\.key\} size=\{FEED_ANALYTICS_KPI_SIZE\}>/);
  assert.match(page, /FEED_ANALYTICS_RANGES as RANGES, FEED_ANALYTICS_TABS as TABS/);
});
