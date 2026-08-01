import { test } from "node:test";
import assert from "node:assert";
import { driveClosedCoveragePct, driveCoveragePct, driveCoverage, driveStatusChips, driveVisibleProgress, drivePctFor } from "./drive-card-metrics.ts";

test("driveCoveragePct: rounds half-up", () => {
  // 46/77 = 59.74%, rounds to 60%
  assert.strictEqual(driveCoveragePct(46, 77), 60);
  // 1/2 = 50%, rounds to 50%
  assert.strictEqual(driveCoveragePct(1, 2), 50);
  // 2/3 = 66.67%, rounds to 67%
  assert.strictEqual(driveCoveragePct(2, 3), 67);
  // 0/10 = 0%
  assert.strictEqual(driveCoveragePct(0, 10), 0);
  // 10/10 = 100%
  assert.strictEqual(driveCoveragePct(10, 10), 100);
});

test("driveCoveragePct: handles zero total", () => {
  assert.strictEqual(driveCoveragePct(0, 0), 0);
  assert.strictEqual(driveCoveragePct(5, 0), 0);
});

test("driveClosedCoveragePct: caps submitted review drives below closed completion", () => {
  assert.strictEqual(driveClosedCoveragePct(5, 5, "verification_pending"), 99);
  assert.strictEqual(driveClosedCoveragePct(5, 5, "completed"), 100);
  assert.strictEqual(driveClosedCoveragePct(0, 5, "verification_pending"), 0);
});

test("driveCoverage: prefers animal counts", () => {
  const result = driveCoverage(10, 20, 30, 40);
  assert.strictEqual(result.completed, 10);
  assert.strictEqual(result.total, 20);
  assert.strictEqual(result.usesAnimals, true);
});

test("driveCoverage: falls back to dose counts when animals missing", () => {
  const result = driveCoverage(null, undefined, 30, 40);
  assert.strictEqual(result.completed, 30);
  assert.strictEqual(result.total, 40);
  assert.strictEqual(result.usesAnimals, false);
});

test("driveCoverage: falls back when only one animal count missing", () => {
  const result = driveCoverage(10, null, 30, 40);
  assert.strictEqual(result.completed, 30);
  assert.strictEqual(result.total, 40);
  assert.strictEqual(result.usesAnimals, false);
});

test("driveStatusChips: returns nonzero chips in order", () => {
  const result = driveStatusChips({
    completed_count: 5,
    due_count: 3,
    overdue_count: 0,
    deferred_count: 1,
  });
  assert.deepStrictEqual(result, [
    { key: "completed", count: 5 },
    { key: "due", count: 3 },
    { key: "deferred", count: 1 },
  ]);
});

test("driveStatusChips: skips zero counts", () => {
  const result = driveStatusChips({
    completed_count: 0,
    due_count: 0,
    overdue_count: 2,
    deferred_count: 0,
  });
  assert.deepStrictEqual(result, [{ key: "overdue", count: 2 }]);
});

test("driveStatusChips: returns empty array when all zero", () => {
  const result = driveStatusChips({
    completed_count: 0,
    due_count: 0,
    overdue_count: 0,
    deferred_count: 0,
  });
  assert.deepStrictEqual(result, []);
});

// Cross-surface parity. THE SAME fixture is asserted by the Android
// DriveCardMetricsTest ("renders the backend progress contract verbatim"): both surfaces must
// resolve to 46 / 77 animals and a 60% ring.
const parityFixture = {
  progress_basis: "animals",
  progress_completed: 46,
  progress_total: 77,
  progress_pct: 60,
  completed_animals: 46,
  total_animals: 77,
  submitted_animals: 60,
  completed_count: 120,
  total_count: 200,
  submitted_count: 150,
};

test("driveVisibleProgress: the two legacy per-client numerators disagreed on one drive", () => {
  // What each client used to compute for itself, on the SAME drive:
  const legacyWeb = parityFixture.completed_animals; // web ring numerator
  const legacyAndroid = Math.max(parityFixture.submitted_animals, parityFixture.completed_animals); // Android's max()
  assert.notStrictEqual(legacyWeb, legacyAndroid); // 46 vs 60 -> two different cards for one drive
  assert.notStrictEqual(driveCoveragePct(legacyWeb, 77), driveCoveragePct(legacyAndroid, 77)); // 60% vs 78%
});

test("driveVisibleProgress: renders the backend progress contract verbatim", () => {
  const coverage = driveVisibleProgress(parityFixture);
  assert.deepStrictEqual(coverage, { completed: 46, total: 77, usesAnimals: true });
  assert.strictEqual(drivePctFor(parityFixture, coverage), 60);
  // Submitted-but-unverified never inflates progress, on either surface.
  assert.notStrictEqual(coverage.completed, parityFixture.submitted_animals);
});

test("driveVisibleProgress: dose basis is carried by the contract, not guessed", () => {
  const coverage = driveVisibleProgress({
    progress_basis: "doses",
    progress_completed: 3,
    progress_total: 8,
    progress_pct: 38,
    completed_count: 3,
    total_count: 8,
  });
  assert.deepStrictEqual(coverage, { completed: 3, total: 8, usesAnimals: false });
  assert.strictEqual(drivePctFor({ progress_pct: 38 }, coverage), 38);
});

test("driveVisibleProgress: legacy fallback when an older backend omits the contract", () => {
  const coverage = driveVisibleProgress({
    completed_animals: 5,
    total_animals: 9,
    completed_count: 11,
    total_count: 20,
  });
  assert.deepStrictEqual(coverage, { completed: 5, total: 9, usesAnimals: true });
  assert.strictEqual(drivePctFor({}, coverage), 56);
});

// Cross-SURFACE parity within admin-web itself. The drive card and the drive DETAIL page render the
// same drive; the detail page used to derive its own numerator (driveCoverage) and its own
// percentage (driveClosedCoveragePct, which caps at 99 until event.status === "completed"), so a
// fully covered drive read 100% on the card and 99% on its detail page. This is a source-shape
// guard, because the detail page is a server component that cannot be unit-rendered here.
test("calendar-drive-detail renders the backend progress contract, not its own", async () => {
  const { readFileSync } = await import("node:fs");
  const src = readFileSync(new URL("./calendar-drive-detail.tsx", import.meta.url), "utf8");
  assert.ok(src.includes("driveVisibleProgress(summary)"), "detail must take its numerator from the backend contract");
  assert.ok(src.includes("drivePctFor(summary, coverage)"), "detail must take its ring percentage from the backend contract");
  assert.ok(!src.includes("driveClosedCoveragePct("), "detail must not re-derive a capped percentage (100% card vs 99% detail)");
  assert.ok(!src.includes("driveCoverage("), "detail must not re-derive its own coverage numerator");
});

test("the legacy detail percentage really did disagree with the card", () => {
  // Same fully covered drive, awaiting the event-level close: card said 100, detail said 99.
  assert.strictEqual(drivePctFor({ progress_pct: 100 }, { completed: 77, total: 77, usesAnimals: true }), 100);
  assert.strictEqual(driveClosedCoveragePct(77, 77, "verification_pending"), 99);
});
