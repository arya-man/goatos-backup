// node --test tools/dashboard-automation/lane-coverage.test.mjs
//
// Guards the promise lane-checks.json makes: every commit on origin/main since the pinned
// window is accounted for EXACTLY ONCE across lane 1, lanes 2-5 and the parked list, and
// nothing in lanes 2 or 3 can write.
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import {
  AUTO_PARK_REASON, COVERAGE_WINDOW, LANE1_LEDGER_FILES, LANE_LEDGER_FILES, LANE_NEEDS_ROUTING,
  describeLanePlan, failureSentenceFindings, isCoverageBookkeeping, laneCoverageSelfTest, laneFindings,
  newNeedsLaneRow, planLaneCoverage, readLaneChecks, readLaneLedgers, sqlReadOnlyFindings,
  summarizeLanePlan,
} from "./sync-coverage.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../..");

const plan = (over = {}) => planLaneCoverage({
  windowShas: [], lane1Rows: [], laneRows: [], laneChecks: { lanes: {} }, ...over,
});
const row = (sha, lane, over = {}) => ({ sha, lane, _file: LANE_LEDGER_FILES[lane], _lane: lane, ...over });

// ---------------------------------------------------------------- read-only contract

test("lane 2 SQL must be a single read-only SELECT with a LIMIT", () => {
  assert.deepEqual(sqlReadOnlyFindings("SELECT goat_id FROM public.goats LIMIT 50", "t"), []);
  for (const bad of [
    "DELETE FROM public.goats LIMIT 1",
    "UPDATE public.goats SET sex = 'male' LIMIT 1",
    "INSERT INTO public.goats VALUES (1) LIMIT 1",
    "DROP TABLE public.goats",
    "TRUNCATE public.goats",
    "GRANT SELECT ON public.goats TO x",
    "COPY public.goats TO '/tmp/x' LIMIT 1",
    "SELECT setval('s', 1) LIMIT 1",
  ]) {
    assert.ok(sqlReadOnlyFindings(bad, "t").length > 0, `must reject: ${bad}`);
  }
});

test("lane 2 SQL rejects a second statement and a missing LIMIT", () => {
  assert.ok(sqlReadOnlyFindings("SELECT 1 LIMIT 1; DROP TABLE public.goats", "t").some((f) => /single statement/.test(f)));
  assert.ok(sqlReadOnlyFindings("SELECT 1 FROM public.goats", "t").some((f) => /LIMIT/.test(f)));
  assert.ok(sqlReadOnlyFindings("", "t").length > 0, "an empty sql must be rejected, not silently passed");
});

test("a write word inside a string literal is not a write", () => {
  // 'sold', 'dead' and reason text are ordinary data values; flagging them would make the
  // guard a false-alarm generator and it would get switched off.
  assert.deepEqual(sqlReadOnlyFindings(
    "SELECT exit_reason FROM public.goats WHERE exit_reason = 'update pending' LIMIT 5", "t"), []);
});

test("failure sentences stay in farm words", () => {
  assert.deepEqual(failureSentenceFindings("Animals that were sold are still listed inside a pen.", "t"), []);
  for (const bad of [
    "SELECT * FROM public.goats returned rows",
    "lane2.herd-total-reconciles failed",
    "the .wchart svg selector is missing",
    "goat_id was null",
    "the endpoint answered HTTP 500",
    "P-partition-pen-label regressed",
  ]) {
    assert.ok(failureSentenceFindings(bad, "t").length > 0, `must reject: ${bad}`);
  }
  assert.ok(failureSentenceFindings("", "t").length > 0, "a check with no failure sentence must be rejected");
});

test("lane 3 must be GET only and lane 4 must name what it writes", () => {
  const p = plan({
    laneChecks: { lanes: {
      lane3: { checks: [{ id: "l3", method: "POST", failureSentence: "The Sales page is blank." }] },
      lane4: { checks: [{ id: "l4", failureSentence: "Recording a sale does not stick.", writesTables: [] }] },
    } },
  });
  assert.ok(p.contract.some((f) => /only GET is allowed/.test(f)));
  assert.ok(p.contract.some((f) => /name the tables it touches/.test(f)));
});

// ---------------------------------------------------------------- exactly-once accounting

test("a commit no lane names is reported uncovered and fails the guard", () => {
  const p = plan({ windowShas: ["aaaaaaaa1", "newnewnew"], lane1Rows: [{ sha: "aaaaaaaa1" }] });
  assert.deepEqual(p.uncovered, ["newnewnew"]);
  assert.ok(laneFindings(p).some((f) => /covered by NO lane/.test(f)));
  assert.match(summarizeLanePlan(p), /1 UNCOVERED/);
});

test("a sha in two lane ledgers breaks the exactly-once count", () => {
  const p = plan({
    windowShas: ["bbbbbbbb2"],
    laneRows: [row("bbbbbbbb2", "lane2", { checkId: "c" }), row("bbbbbbbb2", "lane3", { checkId: "c" })],
    laneChecks: { lanes: { lane2: { checks: [{ id: "c", failureSentence: "The pen list is wrong.", sql: "SELECT 1 FROM public.goats LIMIT 1" }] } } },
  });
  assert.equal(p.duplicated.length, 1);
  assert.ok(laneFindings(p).some((f) => /more than one lane ledger/.test(f)));
});

test("accounted-for is the union of the two ledgers, never their sum", () => {
  // Every Android commit sits in BOTH lane 1's classification ledger and lane5-android.jsonl,
  // because lane 1 classified it and then explicitly did not cover the app.
  const p = plan({
    windowShas: ["android001", "webonly002"],
    lane1Rows: [{ sha: "android001" }, { sha: "webonly002" }],
    laneRows: [row("android001", "lane5-android", { checkId: "c" })],
    laneChecks: { lanes: { "lane5-android": { checks: [{ id: "c", failureSentence: "The app will not record work." }] } } },
  });
  assert.equal(p.bothLedgers, 1);
  assert.equal(p.accountedFor, 2, "the shared commit must be counted once, not twice");
  assert.equal(p.accountedFor + p.uncovered.length, p.windowSize, "the arithmetic must close");
});

test("a checkId with no check behind it is reported", () => {
  const p = plan({
    windowShas: ["ccccccccc"],
    laneRows: [row("ccccccccc", "lane4", { checkId: "lane4.does-not-exist" })],
    laneChecks: { lanes: { lane4: { checks: [{ id: "lane4.real", failureSentence: "A sale does not stick.", writesTables: ["sales_deals"] }] } } },
  });
  assert.equal(p.dangling.length, 1);
  assert.ok(laneFindings(p).some((f) => /lane-checks.json does not define/.test(f)));
});

test("a row filed in the wrong lane file is reported", () => {
  const p = plan({
    windowShas: ["ddddddddd"],
    laneRows: [{ sha: "ddddddddd", lane: "lane3", checkId: "c", _file: "lane2.jsonl", _lane: "lane2" }],
    laneChecks: { lanes: { lane2: { checks: [{ id: "c", failureSentence: "The pen list is wrong.", sql: "SELECT 1 FROM public.goats LIMIT 1" }] } } },
  });
  assert.equal(p.misfiled.length, 1);
  assert.ok(laneFindings(p).some((f) => /but its row says lane/.test(f)));
});

test("parked work must say why", () => {
  const p = plan({
    windowShas: ["eeeeeeeee", "fffffffff"],
    laneRows: [
      row("eeeeeeeee", "not-automatable", { reason: "Documentation only." }),
      row("fffffffff", "not-automatable", { reason: "  " }),
    ],
  });
  assert.equal(p.parkedWithoutReason.length, 1);
  assert.ok(laneFindings(p).some((f) => /parked with no reason/.test(f)));
});

test("a ledger row naming a commit outside the window is reported", () => {
  const p = plan({
    windowShas: ["aaaaaaaa1"],
    lane1Rows: [{ sha: "aaaaaaaa1" }],
    laneRows: [row("rewritten", "not-automatable", { reason: "Docs." })],
  });
  assert.deepEqual(p.outsideWindow, ["rewritten"]);
  assert.ok(laneFindings(p).some((f) => /history was rewritten/.test(f)));
});

test("a fully accounted-for window produces no findings", () => {
  const p = plan({
    windowShas: ["aaaaaaaa1", "bbbbbbbb2"],
    lane1Rows: [{ sha: "aaaaaaaa1" }],
    laneRows: [row("bbbbbbbb2", "not-automatable", { reason: "Repo tooling only." })],
  });
  assert.deepEqual(laneFindings(p), []);
  assert.match(summarizeLanePlan(p), /none uncovered/);
});

// ---------------------------------------------------------------- parking new work

test("--write parks a new commit as needs-lane, covered for the count and covered by no check", () => {
  const parked = newNeedsLaneRow({ sha: "newnewnew", date: "2026-09-24", subject: "feat: something" });
  assert.equal(parked.lane, LANE_NEEDS_ROUTING);
  assert.equal(parked.checkId, null, "parked work must not claim a check");
  assert.ok(parked.reason.length > 40, "parked work must say what a human has to do next");
  const p = plan({ windowShas: ["newnewnew"], laneRows: [{ ...parked, _file: "needs-lane.jsonl", _lane: LANE_NEEDS_ROUTING }] });
  assert.deepEqual(p.uncovered, [], "once parked it is accounted for");
  assert.equal(p.needsRouting, 1, "and it is still visibly awaiting routing");
  assert.deepEqual(p.dangling, [], "a needs-lane row must not be read as a dangling checkId");
  assert.match(summarizeLanePlan(p), /1 awaiting routing/);
});

test("a commit that edits only this automation's own ledger is auto-parked, not left red", () => {
  // Otherwise the guard can never go green: writing the ledger makes a commit, and that commit
  // would itself be uncovered, for ever.
  const book = { sha: "bookkeep1", date: "2026-09-24", subject: "chore: sync coverage",
    files: ["tools/dashboard-automation/commit-classification/needs-lane.jsonl", "tools/dashboard-automation/coverage-state.json"] };
  const real = { sha: "realwork1", date: "2026-09-24", subject: "feat(feed): a new rate",
    files: ["backend/internal/feedconfig/service.go"] };
  const p = plan({ windowCommits: [book, real] });
  assert.deepEqual(p.autoParked, ["bookkeep1"]);
  assert.deepEqual(p.uncovered, ["realwork1"], "real work must still hold the guard red");
  assert.equal(p.accountedFor + p.uncovered.length, p.windowSize);
  assert.ok(isCoverageBookkeeping(book));
  assert.ok(!isCoverageBookkeeping(real));
  assert.ok(!isCoverageBookkeeping({ sha: "x", files: [] }), "a commit with no files is not bookkeeping");
  assert.ok(!isCoverageBookkeeping({ sha: "x", files: [...book.files, "apps/admin-web/app/page.tsx"] }),
    "a commit that also touches a real surface is not bookkeeping");
  assert.match(summarizeLanePlan(p), /1 auto-parked as this automation's own bookkeeping/);
  assert.ok(AUTO_PARK_REASON.length > 40, "the auto-park reason must say why, in farm words");
  assert.deepEqual(failureSentenceFindings(AUTO_PARK_REASON, "auto-park"), []);
});

// ---------------------------------------------------------------- weak ties

test("path-only ties stay visible to builders and to the judge", () => {
  const p = plan({
    windowShas: ["1111111aa", "2222222bb", "3333333cc"],
    laneRows: [
      row("1111111aa", "lane3", { checkId: "c", matchStrength: "subject+path" }),
      row("2222222bb", "lane3", { checkId: "c", matchStrength: "subject" }),
      row("3333333cc", "lane3", { checkId: "c", matchStrength: "path" }),
    ],
    laneChecks: { lanes: { lane3: { checks: [{ id: "c", method: "GET", failureSentence: "The Sales page is blank." }] } } },
  });
  assert.deepEqual(p.weakTies.lane3, { "subject+path": 1, subject: 1, path: 1 });
  assert.match(summarizeLanePlan(p), /1 tied to their check by file path alone/);
  assert.match(describeLanePlan(p), /path-only \(weakest tie\)/);
  assert.deepEqual(laneFindings(p), [], "a weak tie is reported, never a failure");
});

// ---------------------------------------------------------------- regression guards

test("lane 1's ledger read never swallows the lane 2-5 ledgers", () => {
  // This was a real bug: readLedger() globbed every .jsonl in the directory, so once lane2..5
  // landed, lane 1 believed it already covered an Android commit.
  for (const file of Object.values(LANE_LEDGER_FILES)) {
    assert.ok(!LANE1_LEDGER_FILES.includes(file), `${file} must not be read as part of lane 1's ledger`);
  }
});

test("the window is pinned to a timestamp, not a bare date", () => {
  // A bare `--since=2026-08-01` is an approxidate: git fills in the current time of day, so the
  // commit set changes depending on what o'clock the guard runs.
  assert.match(COVERAGE_WINDOW, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}$/);
});

test("the self-test that the guard hook runs actually passes", () => {
  assert.equal(laneCoverageSelfTest(), true);
});

// ---------------------------------------------------------------- the shipped artefacts

test("the shipped lane-checks.json honours the read-only contract", (t) => {
  const checks = readLaneChecks(join(repoRoot, "tools/dashboard-automation/lane-checks.json"));
  if (!checks) return t.skip("lane-checks.json not present");
  const p = planLaneCoverage({ windowShas: [], lane1Rows: [], laneRows: [], laneChecks: checks });
  assert.deepEqual(p.contract, [], `shipped checks must be read-only and farm-readable:\n${p.contract.join("\n")}`);
  assert.ok(p.checkCount > 100, "the shipped catalogue should not silently shrink to nothing");
});

test("the shipped lane ledgers are valid JSONL with no duplicate sha", (t) => {
  const dir = join(repoRoot, "tools/dashboard-automation/commit-classification");
  if (!existsSync(join(dir, "lane2.jsonl"))) return t.skip("lane ledgers not present");
  const { rows } = readLaneLedgers(dir);
  const seen = new Set();
  for (const r of rows) {
    assert.ok(r.sha, `${r._file}:${r._line} has no sha`);
    assert.ok(!seen.has(r.sha), `${r.sha} appears twice (${r._file})`);
    seen.add(r.sha);
  }
  assert.equal(seen.size, rows.length);
});

test("every shipped lane row points at a check or carries a reason", (t) => {
  const dir = join(repoRoot, "tools/dashboard-automation/commit-classification");
  const checks = readLaneChecks(join(repoRoot, "tools/dashboard-automation/lane-checks.json"));
  if (!existsSync(join(dir, "lane2.jsonl")) || !checks) return t.skip("lane artefacts not present");
  const { rows } = readLaneLedgers(dir);
  const p = planLaneCoverage({ windowShas: rows.map((r) => r.sha), lane1Rows: [], laneRows: rows, laneChecks: checks });
  assert.deepEqual(p.dangling, [], "no row may point at a check that does not exist");
  assert.deepEqual(p.parkedWithoutReason, [], "no parked row may be silent about why");
  assert.deepEqual(p.misfiled, [], "no row may sit in the wrong lane file");
});

test("the report's arithmetic matches the ledgers on disk", (t) => {
  const report = join(repoRoot, "tools/dashboard-automation/LANE-COVERAGE-REPORT.md");
  const dir = join(repoRoot, "tools/dashboard-automation/commit-classification");
  if (!existsSync(report) || !existsSync(join(dir, "lane2.jsonl"))) return t.skip("artefacts not present");
  const text = readFileSync(report, "utf8");
  const { byLane } = readLaneLedgers(dir);
  // The report prints `a + b + c + d + e + f = T`; the lane terms must match the files on disk.
  const sums = text.match(/`(\d+(?: \+ \d+)+) = (\d+)`/);
  assert.ok(sums, "the report must show its reconciliation arithmetic");
  const terms = sums[1].split(" + ").map(Number);
  assert.equal(terms.reduce((a, b) => a + b, 0), Number(sums[2]), "the printed arithmetic must actually add up");
  for (const [lane, expected] of [["lane2", null], ["lane3", null], ["lane4", null], ["lane5-android", null]]) {
    void expected;
    assert.ok(terms.includes(byLane[lane].length),
      `the report's arithmetic must contain ${lane}'s real row count (${byLane[lane].length})`);
  }
});

// ---------------------------------------------------------------------------------------------
// A check routed to no commit covers nothing.
//
// 37 of the 194 shipped checks are in that state. They are good invariants and they stay, but
// counting them as coverage overstated the catalogue by 24%. These tests keep the two numbers
// apart so the headline cannot quietly re-inflate.
// ---------------------------------------------------------------------------------------------

test("checks that cover no commit are counted separately from checks that do", () => {
  const checks = readLaneChecks(join(repoRoot, "tools/dashboard-automation/lane-checks.json"));
  const p = planLaneCoverage({ windowShas: [], lane1Rows: [], laneRows: [], laneChecks: checks });
  assert.equal(p.commitCoveringCheckCount + p.checksCoveringNoCommit.length, p.checkCount);
  assert.ok(p.commitCoveringCheckCount < p.checkCount, "the split must actually be reported");
  // Every unrouted check has to say what it guards, so it is never mistaken for an oversight.
  for (const lane of Object.values(checks.lanes)) {
    for (const check of lane.checks ?? []) {
      const routed = Number(check.sourceCommitCount ?? 0) > 0 || (check.sourceShas ?? []).length > 0;
      if (routed) continue;
      assert.ok(String(check.derivedFrom ?? "").length > 30, `${check.id} covers no commit and does not say what it guards`);
    }
  }
});

test("the report's check table shows the commit-covering count, not just the catalogue size", () => {
  const report = readFileSync(join(repoRoot, "tools/dashboard-automation/LANE-COVERAGE-REPORT.md"), "utf8");
  assert.match(report, /\| Lane \| Distinct checks \| Checks covering a commit \| Commits routed \|/);
  assert.match(report, /routed to no commit at all/);
});
