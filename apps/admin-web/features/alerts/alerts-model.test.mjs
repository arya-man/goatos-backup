import assert from "node:assert/strict";
import test from "node:test";
import { alertsEmptyState } from "./alerts-model.ts";

const clean = { visibleRows: 0, totalRows: 0, allFailed: false, incomplete: false, rulesRun: 2 };
for (const [name, overrides, expected] of [
  ["completed clean checks", {}, "state.empty"],
  ["a failed detector", { incomplete: true }, "state.empty.incomplete"],
  ["all detectors degraded", { incomplete: true, rulesRun: 0 }, "state.empty.incomplete"],
  ["missing feed sheets", { incomplete: true, rulesRun: 0 }, "state.empty.incomplete"],
  ["severity hides existing alerts", { totalRows: 3 }, "state.empty.filtered"],
  ["incomplete filtered result", { totalRows: 3, incomplete: true }, "state.empty.incomplete"],
  ["no enabled rules", { rulesRun: 0 }, "state.empty.no_rules"],
  ["all park requests failed", { allFailed: true }, null],
  ["visible alert rows", { visibleRows: 1, totalRows: 1 }, null],
]) {
  test(name, () => assert.equal(alertsEmptyState({ ...clean, ...overrides }), expected));
}
