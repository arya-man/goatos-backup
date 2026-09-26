import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const source = readFileSync(new URL("./command-board-view.tsx", import.meta.url), "utf8");

test("command-board KPI tiles filter the status matrix instead of rendering dead cards", () => {
  assert.match(source, /const activateStatusKpi = \(key: StatusKey\) =>/);
  assert.match(source, /setStatuses\(new Set\(\[key\]\)\)/);
  assert.match(source, /scrollIntoView\(\{ block: "start", behavior: "smooth" \}\)/);
  assert.match(source, /const statusKpiProps = \(key: StatusKey, count: number\) =>/);
  // The tiles are kit KpiCards now: passing `onClick` is what makes one clickable, and KpiCard
  // itself binds the Enter/Space handler and the button role, so the keyboard path is no longer
  // spelled out here. A zero-count tile is handed no onClick and therefore stays inert.
  assert.match(source, /count > 0 \? \{ onClick: \(\) => activateStatusKpi\(key\) \} : \{\}/);
  assert.match(source, /<KpiCard/);
  assert.match(source, /id="cbm-shed-dose-matrix"/);

  for (const [key, metric] of [
    ["overdue", "view.kpis.missedNotGiven"],
    ["verified", "view.kpis.dosesVerified"],
    ["awaiting", "view.kpis.awaitingVerification"],
    ["overdue", "view.kpis.overdueNotGiven"],
    ["scheduled", "view.kpis.scheduledAhead"],
  ]) {
    assert.match(source, new RegExp(`statusKpiProps\\("${key}", ${metric.replaceAll(".", "\\.")}\\)`));
  }
});
