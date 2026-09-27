// Guard: list-no-kpi-row (TR1-#18). /approvals and /alerts are the template order list, which has no
// KPI row; their old tiles only re-counted rows already on screen (approvals) or repeated the
// severity tab Labels (alerts). The one figure the alerts tabs cannot carry (checks run) closes the
// toolbar.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const approvals = readFileSync(new URL("./approvals-page.tsx", import.meta.url), "utf8");
const alerts = readFileSync(new URL("../alerts/alerts-page.tsx", import.meta.url), "utf8");

test("list-no-kpi-row: approvals and alerts carry no KPI tiles", () => {
  for (const src of [approvals, alerts]) {
    assert.doesNotMatch(src, /CourseWidgetSummary|KpiWidget|EcommerceWidgetSummary|KpiRowSkeleton/);
  }
  assert.match(alerts, /data-testid="alerts-rules-run"/);
});
