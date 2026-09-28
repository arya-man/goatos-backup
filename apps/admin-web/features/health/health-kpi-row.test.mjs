import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: health-kpi-no-orphan (TR2-P2-15). /health/analytics laid five KPI tiles on a md-4 grid
// (3 + an orphan-looking 2) under a "Park scope is set in the top bar." caption.
const page = readFileSync(new URL("./health-analytics.tsx", import.meta.url), "utf8");
const layout = readFileSync(new URL("./health-analytics-layout.tsx", import.meta.url), "utf8");
const loading = readFileSync(new URL("../../app/(admin)/health/analytics/loading.tsx", import.meta.url), "utf8");

test("guard: health-kpi-no-orphan - 3 tiles at md 4 then the 2 death tiles at md 6, mirrored by the skeleton", () => {
  assert.match(layout, /HEALTH_KPI_ROW1 = \{ count: 3, size: \{ xs: 12, sm: 6, md: 4 \} \}/);
  assert.match(layout, /HEALTH_KPI_ROW2_MD = 6/);
  assert.equal((page.match(/md=\{HEALTH_KPI_ROW2_MD\}/g) ?? []).length, 2);
  assert.match(loading, /<HealthKpiSkeleton \/>/);
  assert.match(page, /fallback=\{<HealthKpiSkeleton \/>\}/);
});

test("guard: health-kpi-no-orphan - no read-only park scope caption", () => {
  assert.doesNotMatch(page, /filter\.scope_readonly/);
  assert.doesNotMatch(loading, /ControlRowSkeleton caption=/);
});
