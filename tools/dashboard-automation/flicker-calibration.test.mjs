import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { CALIBRATION_FILE, flickerReportable, loadCalibration, loadCalibrationObject, partitionFindings, uncalibratedPages } from "./flicker-calibration.mjs";

const rows = (...r) => r;

test("only the screens the check was calibrated on may report flicker", () => {
  const c = loadCalibration();
  for (const route of ["tasks", "tasks-list", "herd-register", "vaccination-plan"]) {
    assert.equal(flickerReportable(route, c).report, true, `${route} was calibrated`);
  }
  for (const route of ["weighing-analytics", "feed-config", "counts-breakdown", "people-notifications"]) {
    const verdict = flickerReportable(route, c);
    assert.equal(verdict.report, false, `${route} was never calibrated`);
    assert.ok(verdict.why.includes("spinning refresh mark"),
      "and the reason names the mandated animation that makes an uncalibrated verdict untrustworthy");
  }
});

test("a finding on an uncalibrated page never reaches a person, and is not thrown away", () => {
  const c = loadCalibration();
  const swept = rows(
    { route: "tasks", viewport: "mobile", findings: [{ label: "Filters" }] },
    { route: "weighing-analytics", viewport: "mobile", findings: [{ label: "Refresh" }] },
    { route: "feed-config", viewport: "laptop", findings: [{ label: "Sync" }] },
  );
  const { reportable, notCalibrated } = partitionFindings(swept, c);
  assert.equal(reportable.length, 1, "only the calibrated page reports");
  assert.equal(reportable[0].route, "tasks");
  assert.equal(notCalibrated.length, 2, "the rest are kept, not discarded");
  for (const row of notCalibrated) {
    assert.ok(row.findings.length, "the finding is preserved for the supervised calibration run");
    assert.ok(row.why.length > 40, "with a sentence saying why it is not being shown");
  }
});

test("a filmed but uncalibrated page is not a clean page either", () => {
  const c = loadCalibration();
  const swept = rows(
    { route: "tasks", viewport: "mobile", findings: [] },
    { route: "weighing-analytics", viewport: "mobile", findings: [] },
    { route: "counts-breakdown", viewport: "laptop", findings: [], parked: "needed a signed-in session" },
  );
  const pending = uncalibratedPages(swept, c);
  assert.equal(pending.length, 1, "a parked page is already accounted for; only the filmed-but-untrusted one is added");
  assert.equal(pending[0].route, "weighing-analytics");
  assert.ok(!pending.some((p) => p.route === "tasks"), "a calibrated page that found nothing IS a clean page");
});

test("membership is evidence, not an edit", () => {
  // THE POINT: adding a bare route name must change nothing, or the list can be
  // widened by anyone who simply wants a page to go green.
  const withoutEvidence = { calibrated: new Map(), ignored: [] };
  const parsed = loadCalibration();
  assert.ok(parsed.calibrated.size > 0);
  // Simulate an entry added with no positive control.
  const { calibrated, ignored } = loadCalibrationObject({
    routes: [
      { route: "already-fine", positiveControl: null },
      { route: "hand-waved", positiveControl: { what: "" } },
      { route: "real", positiveControl: { what: "a filmed flicker", result: "the check fired" } },
    ],
  });
  assert.ok(!calibrated.has("already-fine"), "a route with no evidence is not calibrated");
  assert.ok(!calibrated.has("hand-waved"), "nor one whose evidence is blank");
  assert.ok(calibrated.has("real"), "a route with a recorded positive control is");
  assert.equal(ignored.length, 2, "and the ones ignored are reported, not silently dropped");
  for (const row of ignored) assert.ok(row.why.includes("changes nothing"));
  assert.equal(withoutEvidence.calibrated.size, 0);
});

test("an unreadable calibration file means nothing is calibrated, never everything", () => {
  const c = loadCalibration("/nonexistent/flicker-calibration.json");
  assert.equal(c.calibrated.size, 0, "failing open here would restore the exact state this prevents");
  assert.ok(c.error, "and it says it could not be read");
  assert.equal(flickerReportable("tasks", c).report, false);
});

test("the shipped file records that the spinner control is still missing", () => {
  // Honest about what calibration these four actually have: a positive control
  // against a real flicker, and NO negative control against the mandated
  // rotating refresh icon. Nobody has that yet.
  const parsed = JSON.parse(readFileSync(CALIBRATION_FILE, "utf8"));
  assert.ok(parsed.routes.every((r) => r.positiveControl), "every listed route has a positive control");
  assert.ok(parsed.routes.every((r) => r.spinnerControl === null),
    "and none claims a spinner control, because the supervised run that produces one has not happened");
});
