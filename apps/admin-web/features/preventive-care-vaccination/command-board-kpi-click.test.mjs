import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const source = readFileSync(new URL("./command-board-view.tsx", import.meta.url), "utf8");

test("command-board KPI tiles filter the status matrix instead of rendering dead cards", () => {
  assert.match(source, /const activateStatusKpi = \(key: StatusKey\) =>/);
  assert.match(source, /setStatuses\(new Set\(\[key\]\)\)/);
  assert.match(source, /scrollIntoView\(\{ block: "start", behavior: "smooth" \}\)/);
  assert.match(source, /const statusKpiClick = \(key: StatusKey, count: number\) =>/);
  // The tiles are the shared KpiWidget adapter (template CourseWidgetSummary) with the backend
  // explanation as the caption (REVIEW-6: Missed vs Overdue stays readable); a tile with an action
  // is wrapped in a button-role Box with the Enter/Space handler; a zero-count tile stays inert.
  assert.match(source, /count > 0 \? \(\) => activateStatusKpi\(key\) : undefined/);
  assert.match(source, /<KpiWidget title=\{title\} total=\{total\} caption=\{hint\}/);
  assert.doesNotMatch(source, /aria-description=\{hint\}/);
  assert.match(source, /role="button"[\s\S]*?event\.key === "Enter" \|\| event\.key === " "/);
  assert.doesNotMatch(source, /KpiCard|KpiGrid/);
  assert.match(source, /id="cbm-shed-dose-matrix"/);

  for (const [key, metric] of [
    ["overdue", "view.kpis.missedNotGiven"],
    ["verified", "view.kpis.dosesVerified"],
    ["awaiting", "view.kpis.awaitingVerification"],
    ["overdue", "view.kpis.overdueNotGiven"],
    ["scheduled", "view.kpis.scheduledAhead"],
  ]) {
    assert.match(source, new RegExp(`statusKpiClick\\("${key}", ${metric.replaceAll(".", "\\.")}\\)`));
  }
});
