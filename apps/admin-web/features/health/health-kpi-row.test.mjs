import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: health-kpi-no-orphan (TR2-P2-15). /health/analytics laid five KPI tiles on a md-4 grid
// (3 + an orphan-looking 2) under a "Park scope is set in the top bar." caption.
const page = readFileSync(new URL("./health-analytics.tsx", import.meta.url), "utf8");
const layout = readFileSync(new URL("./health-analytics-layout.tsx", import.meta.url), "utf8");
const loading = readFileSync(new URL("../../app/(admin)/health/analytics/loading.tsx", import.meta.url), "utf8");

test("guard: health-kpi-no-orphan - 3 tiles at md 4 then the 2 death tiles at md 6, mirrored by the skeleton", () => {
  assert.match(layout, /HEALTH_KPI_MD = \[4, 4, 4, 6, 6\] as const/);
  // One tile per HEALTH_KPI_MD entry, each sized by healthKpiSize(tile) (TR3-P0-2).
  assert.match(page, /<Grid size=\{tile == null \? \{ xs: 12, sm: 6, md \} : healthKpiSize\(tile\)\}>/);
  assert.deepEqual([...page.matchAll(/tile=\{(\d)\}/g)].map((m) => Number(m[1])), [0, 1, 2, 3, 4]);
  assert.match(loading, /<HealthKpiSkeleton \/>/);
  assert.match(page, /fallback=\{<HealthKpiSkeleton \/>\}/);
});

test("guard: health-kpi-no-orphan - the skeleton is ONE grid like the page, not one block per row (TR3-P0-2)", () => {
  assert.match(layout, /HEALTH_KPI_MD\.map\(\(_, i\) => \(\s*<Grid key=\{i\} size=\{healthKpiSize\(i\)\}/);
  assert.equal((layout.match(/<Grid container/g) ?? []).length, 1);
  assert.doesNotMatch(layout, /KpiRowSkeleton/);
});

test("guard: health-kpi-no-orphan - no read-only park scope caption", () => {
  assert.doesNotMatch(page, /filter\.scope_readonly/);
  assert.doesNotMatch(loading, /ControlRowSkeleton caption=/);
});

// guard: health-kpi-trend-twin (FIXJ2-HEALTH). The Deaths tile renders the booking card with its
// month trend row (+x%) when the window has a prior month, which made the loaded KPI grid 34px
// taller than its skeleton (tabs IoU 0.17 at 1440 dark). The twin declares that tile's trend row.
test("guard: health-kpi-trend-twin - the Deaths tile's trend row is drawn in the skeleton", () => {
  assert.match(layout, /HEALTH_KPI_TRENDS = \[false, false, false, true, false\] as const/);
  assert.match(layout, /HEALTH_KPI_TRENDS\[i\] \? \{ booking: true, trend: true \}/);
  // Tile 3 is the one fed a monthly death series.
  assert.match(page, /tile=\{3\}[\s\S]{0,200}monthly=\{deathRowsByMonth/);
});
