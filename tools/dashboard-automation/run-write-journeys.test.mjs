// node --test tools/dashboard-automation/run-write-journeys.test.mjs
//
// What this proves:
//  - the catalogue's farm-manager sentences are farm-manager sentences (no SQL, tables, selectors)
//  - a journey that writes a table it never declared FAILS
//  - a failing journey still restores, and still proves the restore
//  - a restore that cannot be proved becomes a louder finding than the failure that caused it
//  - the runner refuses a production URL and an STG URL from the command line, exit 1
//  - a lane-1-only receipt renders byte-identically to what it rendered before this lane existed
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import writeJourneysKind, { toFindingsFromJourneys } from "./lib/finding-kinds/write-journeys.mjs";
import { modeFlags } from "./lib/run-modes.mjs";
import {
  dbAssertionHolds,
  declaredTablesFor,
  fillTokens,
  loadCatalogue,
  pageNameFor,
  runJourney,
  screenDriverMissing,
  summarise,
  validateCatalogue,
  attemptRefusal,
  exitCodeFor
} from "./run-write-journeys.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../..");
const runner = path.join(here, "run-write-journeys.mjs");
const catalogue = loadCatalogue();

test("the catalogue is valid and covers the roadmap's journeys and the three repeat rules", () => {
  assert.deepEqual(validateCatalogue(catalogue), []);
  const names = catalogue.journeys.map((j) => j.name);
  for (const expected of ["publish-vaccination-plan-version", "change-a-feed-rate", "publish-an-sop", "create-and-move-a-task", "record-a-sale", "approve-a-verification"]) {
    assert.ok(names.includes(expected), `missing journey ${expected}`);
  }
  assert.equal(catalogue.journeys.filter((j) => j.rule === true).length, 3);
});

test("every journey's farm-manager sentence is plain English with no SQL, table name or selector", () => {
  for (const journey of catalogue.journeys) {
    const sentence = journey.humanFailure;
    assert.ok(sentence && sentence.length > 30, `${journey.name}: humanFailure is too short to mean anything`);
    assert.doesNotThrow(() => writeJourneysKind.assertPlainEnglish(sentence, journey.name));
    // It must also not be a restatement of its own SQL or of its own table list.
    for (const table of declaredTablesFor(journey)) {
      assert.ok(!sentence.toLowerCase().includes(table), `${journey.name}: humanFailure names the table ${table}`);
    }
    assert.ok(!/\bselect\b[\s\S]*\bfrom\b|\bjoin\b\s+\w+\s+\bon\b|\binsert into\b|\bdelete from\b/i.test(sentence), `${journey.name}: humanFailure reads like SQL`);
    assert.ok(!/[#.][a-z][\w-]*\s*[>[]|aria-label|\bcss\b/i.test(sentence), `${journey.name}: humanFailure names a selector`);
  }
});

test("a journey with no declared tables, or one naming a moving table, is refused", () => {
  const broken = { journeys: [{ ...catalogue.journeys[0], writesTables: [] }] };
  assert.ok(validateCatalogue(broken).some((p) => /declares no writesTables/.test(p)));
  const moving = { journeys: [{ ...catalogue.journeys[0], writesTables: ["herd_signal_analytics"] }] };
  assert.ok(validateCatalogue(moving).some((p) => /analytics and telemetry/.test(p)));
  const noCommit = { journeys: [{ ...catalogue.journeys[0], sourceCommits: [] }] };
  assert.ok(validateCatalogue(noCommit).some((p) => /source commit/.test(p)));
});

// ---------------------------------------------------------------------------------------------
// A fake engine that records the order in which the runner does things.
// ---------------------------------------------------------------------------------------------
function fakeEngine({ undeclared = [], restored = true, verified = true, mismatches = [] } = {}) {
  const calls = [];
  return {
    calls,
    snapshot(tables, name) { calls.push("snapshot"); return { id: "h1", journey: name, tables, before: tables.map((t) => ({ table: t, rowCount: 1, fingerprint: "f" })), writeBaseline: [] }; },
    undeclaredWrites() { calls.push("undeclaredWrites"); return undeclared; },
    restore() { calls.push("restore"); return { restored, strategy: restored ? "replica-role" : null, error: restored ? undefined : "boom" }; },
    verifyRestore() { calls.push("verifyRestore"); return { verified, tables: [], mismatches }; },
    cleanup() { calls.push("cleanup"); },
    fingerprint(t) { return { table: t, rowCount: 1, fingerprint: "f" }; }
  };
}

const journey = catalogue.journeys.find((j) => j.name === "create-and-move-a-task");
const okUi = () => ({ ok: true, status: "ran", screenshot: null });
const rows1 = () => [["row-id"]];

test("a green journey snapshots, runs, asserts, restores, proves the restore and cleans up - in that order", () => {
  const engine = fakeEngine();
  const record = runJourney(journey, { engine, driveUi: okUi, queryRows: rows1, token: "A1234", screenshotDir: "/tmp" });
  assert.equal(record.status, "pass");
  assert.equal(record.screen.status, "pass");
  assert.equal(record.database.status, "pass");
  assert.deepEqual(engine.calls, ["snapshot", "undeclaredWrites", "restore", "verifyRestore", "cleanup"]);
  assert.equal(record.restore.verified, true);
});

test("a journey that writes a table it never declared FAILS, and says so without naming the table", () => {
  const engine = fakeEngine({ undeclared: ["notification_delivery_attempts"] });
  const record = runJourney(journey, { engine, driveUi: okUi, queryRows: rows1, token: "A1", screenshotDir: "/tmp" });
  assert.equal(record.status, "fail");
  assert.deepEqual(record.undeclaredTables, ["notification_delivery_attempts"]);
  assert.match(record.humanFailure, /changed more of the farm's data than it said it would/);
  assert.doesNotThrow(() => writeJourneysKind.assertPlainEnglish(record.humanFailure));
});

test("a failing screen still restores and still proves the restore", () => {
  const engine = fakeEngine();
  const failingUi = () => ({ ok: false, status: "ran", failedStep: "publish it", screenshot: null });
  const record = runJourney(journey, { engine, driveUi: failingUi, queryRows: rows1, token: "A1", screenshotDir: "/tmp" });
  assert.equal(record.status, "fail");
  assert.equal(record.humanFailure, journey.humanFailure);
  assert.equal(record.failedStep, "publish it");
  assert.ok(engine.calls.includes("restore"), "the restore must still run");
  assert.ok(engine.calls.includes("verifyRestore"), "the restore must still be proved");
  assert.equal(record.database.status, "not_run", "the database is not asserted once the screen already failed");
});

test("a journey that throws mid-flight still restores and still proves the restore", () => {
  const engine = fakeEngine();
  const throwingUi = () => { throw new Error("the browser went away"); };
  const record = runJourney(journey, { engine, driveUi: throwingUi, queryRows: rows1, token: "A1", screenshotDir: "/tmp" });
  assert.equal(record.status, "fail");
  assert.ok(engine.calls.includes("restore"));
  assert.ok(engine.calls.includes("verifyRestore"));
});

test("a restore that cannot be proved is recorded, and the snapshot is NOT cleaned up", () => {
  const engine = fakeEngine({ verified: false, mismatches: [{ table: "leadership_tasks", rowCount: 16, actualRowCount: 15 }] });
  const record = runJourney(journey, { engine, driveUi: okUi, queryRows: rows1, token: "A1", screenshotDir: "/tmp" });
  assert.equal(record.restore.restored, true);
  assert.equal(record.restore.verified, false);
  assert.match(record.restore.humanRestore, /1 record did not come back/);
  assert.ok(!engine.calls.includes("cleanup"), "the evidence must survive an unproved restore");
});

test("a restore problem outranks every journey failure in the run's status and in Slack", () => {
  const passing = { status: "pass", restore: { attempted: true, restored: true, verified: true } };
  const failing = { status: "fail", restore: { attempted: true, restored: true, verified: true } };
  const unproved = { status: "pass", restore: { attempted: true, restored: true, verified: false } };
  assert.equal(summarise([passing, passing]).status, "pass");
  assert.equal(summarise([passing, failing]).status, "fail");
  assert.equal(summarise([failing, unproved]).status, "blocked");

  const findings = toFindingsFromJourneys([
    { page: "Tasks Board", story: "s", status: "fail", humanFailure: "Creating a task and moving it did not stick." },
    { page: "Feed Config", story: "s", status: "fail", humanFailure: "Changing a feed rate did not save.", restore: { attempted: true, restored: false } }
  ]);
  assert.equal(findings[0].kind, "restore", "the restore finding must come first");
  const blocks = writeJourneysKind.renderSection(findings);
  assert.match(JSON.stringify(blocks[0]), /Practice data was left changed/);
  assert.match(JSON.stringify(blocks[1]), /Doing this on the site did not work/);
});

test("nothing this lane renders into Slack contains SQL, a table name or a selector", () => {
  const findings = toFindingsFromJourneys(catalogue.journeys.map((j) => ({
    page: pageNameFor(j),
    url: j.url,
    story: j.story,
    status: "fail",
    humanFailure: j.humanFailure,
    failedStep: j.steps[j.steps.length - 1].do,
    restore: { attempted: true, restored: true, verified: false }
  })));
  const rendered = JSON.stringify([writeJourneysKind.renderSection(findings), writeJourneysKind.summaryText(findings), writeJourneysKind.headline(findings)]);
  for (const journeyEntry of catalogue.journeys) {
    for (const table of declaredTablesFor(journeyEntry)) {
      assert.ok(!rendered.includes(table), `Slack text leaked the table ${table}`);
    }
    assert.ok(!rendered.includes(journeyEntry.dbAssertion.sql.slice(0, 20)), "Slack text leaked SQL");
  }
  assert.ok(!/GOATOS_[A-Z_]+/.test(rendered), "Slack text leaked an env var name");
});

test("tokens make every journey's marker unique and the SQL substitution is complete", () => {
  // Not every journey can carry the token: the vaccination plan editor names versions itself
  // (V10, V11...) and offers no field to type one into, so that journey is bound by publish time
  // instead. Every journey that DOES declare a token must substitute it completely.
  const tokenBound = catalogue.journeys.filter((j) => JSON.stringify(j).includes("{{token}}"));
  assert.ok(tokenBound.length > 0, "at least one journey must be token-bound");
  for (const journey of tokenBound) {
    const filled = fillTokens(journey, "A12345678");
    assert.ok(!JSON.stringify(filled).includes("{{token}}"), `${journey.name} still has an unsubstituted token`);
    assert.ok(JSON.stringify(filled).includes("A12345678"), `${journey.name} did not take the token`);
  }
  const rate = fillTokens(catalogue.journeys.find((j) => j.name === "change-a-feed-rate"), "A12345678");
  assert.ok(!JSON.stringify(rate).includes("{{numericToken}}"));
});

test("the two shapes of database assertion mean what they say", () => {
  assert.equal(dbAssertionHolds({ expect: "at-least-one-row" }, [["x"]]), true);
  assert.equal(dbAssertionHolds({ expect: "at-least-one-row" }, []), false);
  assert.equal(dbAssertionHolds({ expect: "zero-count" }, [["0"]]), true);
  assert.equal(dbAssertionHolds({ expect: "zero-count" }, [["3"]]), false);
  assert.equal(dbAssertionHolds({ expect: "zero-count" }, []), true);
});

// ---------------------------------------------------------------------------------------------
// The guard, from the command line, as an operator would hit it.
// ---------------------------------------------------------------------------------------------
function runCli(env) {
  return spawnSync(process.execPath, [runner, "--only", "change-a-feed-rate"], {
    cwd: repo,
    encoding: "utf8",
    env: { PATH: process.env.PATH, HOME: process.env.HOME, ...env }
  });
}

test("the runner refuses a production URL from the command line", () => {
  const result = runCli({ GOATOS_WRITE_JOURNEY_DATABASE_URL: "postgres://app:pw@10.20.30.40:5432/goatos" });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /refuse 10\.20\.30\.40:5432\/goatos/);
  assert.match(result.stderr, /non-loopback/);
  assert.ok(!result.stderr.includes("pw@"), "a refusal must never echo the DSN");
});

test("the runner refuses the STG read-only proxy from the command line", () => {
  const result = runCli({
    GOATOS_WRITE_JOURNEY_DATABASE_URL: "postgres://writer@127.0.0.1:5455/goatos",
    GOATOS_STG_READONLY_DATABASE_URL: "postgres://reader@127.0.0.1:5455/goatos"
  });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /production or STG/);
});

test("the runner refuses a loopback database that is not declared disposable", () => {
  const result = runCli({ GOATOS_WRITE_JOURNEY_DATABASE_URL: "postgres://p@127.0.0.1:5432/goatos" });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /neither a disposable automation\/preview database nor the declared OCI clone/);
});

test("the runner's own self-test passes", () => {
  const result = spawnSync(process.execPath, [runner, "--self-test"], { cwd: repo, encoding: "utf8" });
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /self-test passed/);
});

// ---------------------------------------------------------------------------------------------
// The Slack contract lane 1 already depends on.
// ---------------------------------------------------------------------------------------------
test("a lane-1-only receipt renders byte-identically to the pre-registry rendering", () => {
  const golden = readFileSync(path.join(here, "testdata/lane1-only-slack.golden.txt"), "utf8");
  const dir = mkdtempSync(path.join(tmpdir(), "lane1-slack-"));
  try {
    const shot = path.join(dir, "shot.png");
    writeFileSync(shot, Buffer.from("89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000a49444154789c63000100000500010d0a2db40000000049454e44ae426082", "hex"));
    mkdirSync(path.join(dir, "module-journeys"), { recursive: true });
    writeFileSync(path.join(dir, "module-journeys", "module-journeys-receipt.json"), JSON.stringify({
      status: "fail",
      modules: [
        { id: "feed", failures: [
          { route: "laptop:feed-analytics", url: "https://dashboard.mesha.sg/feed/analytics", error: 'feed-analytics laptop A-chart-label-clipped: label "Warmup Ration" clipped', screenshots: [shot] },
          { route: "mobile:feed-analytics", url: "https://dashboard.mesha.sg/feed/analytics", error: 'feed-analytics mobile A-chart-label-clipped: label "Warmup Ration" clipped', screenshots: [shot] }
        ] },
        { id: "perf", failures: [
          { route: "laptop:goat-passport", url: "https://dashboard.mesha.sg/goats/x", error: "goat-passport laptop page load 14812ms exceeded budget 8000ms | slowest requests: /api/goat-passport 9.2s", screenshots: [shot] },
          { route: "mobile:goat-passport", url: "https://dashboard.mesha.sg/goats/x", error: "goat-passport mobile page load 12004ms exceeded budget 8000ms", screenshots: [shot] }
        ] }
      ]
    }));
    const receiptPath = path.join(dir, "receipt.json");
    writeFileSync(receiptPath, JSON.stringify({
      runId: "golden-visual",
      mode: "production-smoke",
      status: "fail",
      repoSha: "4aeb8264b0000000",
      runtimePolicy: { browserSmoke: "ran_failed", dataTrust: "not_checked_read_only_smoke" },
      layers: [{ name: "production-module-journeys", status: "fail", message: "module journeys failed: feed, perf" }],
      blockers: [{ layer: "production-module-journeys", message: "module journeys failed: feed, perf" }],
      artifacts: []
    }));
    const result = spawnSync(process.execPath, [path.join(here, "notify-slack.mjs"), "--receipt", receiptPath], {
      cwd: repo,
      encoding: "utf8",
      env: { ...process.env, GOATOS_DASHBOARD_SLACK_DRY_RUN: "1", GOATOS_DASHBOARD_SLACK_STATE_FILE: path.join(dir, "state.json"), GOATOS_DASHBOARD_RUN_PART: "", GOATOS_DASHBOARD_RUN_PART_DIR: "" }
    });
    assert.equal(result.status, 0, result.stderr);
    const actual = result.stdout.split("\n").filter((line) => !line.startsWith("dashboard Slack notify: would")).join("\n");
    assert.equal(actual, golden, "a lane-1-only receipt must render exactly what it rendered before the finding-kind registry existed");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a write-journey receipt beside a lane-1 receipt adds this lane's finding through the same path", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "lane4-slack-"));
  try {
    mkdirSync(path.join(dir, "write-journeys"), { recursive: true });
    writeFileSync(path.join(dir, "write-journeys", "write-journeys-receipt.json"), JSON.stringify({
      status: "fail",
      journeys: [{
        name: "publish-vaccination-plan-version",
        page: "Vaccination Plan Edit",
        url: "https://dashboard.mesha.sg/vaccination/plan/edit",
        story: "A manager publishes a new vaccination plan version.",
        status: "fail",
        humanFailure: "Publishing a new vaccination plan version did not save. A manager who publishes a plan would be told it worked and find nothing there.",
        failedStep: "publish it",
        restore: { attempted: true, restored: true, verified: true }
      }]
    }));
    const receiptPath = path.join(dir, "receipt.json");
    writeFileSync(receiptPath, JSON.stringify({
      mode: "write-journeys", status: "fail", repoSha: "abc123456789",
      runtimePolicy: { browserSmoke: "ran_failed" },
      layers: [{ name: "write-journeys", status: "fail", message: "write journeys failed" }],
      blockers: []
    }));
    const result = spawnSync(process.execPath, [path.join(here, "notify-slack.mjs"), "--receipt", receiptPath], {
      cwd: repo, encoding: "utf8",
      env: { ...process.env, GOATOS_DASHBOARD_SLACK_DRY_RUN: "1", GOATOS_DASHBOARD_SLACK_STATE_FILE: path.join(dir, "state.json") }
    });
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /Doing this on the site did not work/);
    assert.match(result.stdout, /Publishing a new vaccination plan version did not save/);
    assert.ok(!result.stdout.includes("protocol_versions"), "Slack must not name a table");
    assert.ok(!/\bselect\b/i.test(result.stdout.replace(/[A-Za-z]*select[A-Za-z]*/g, (m) => m === "select" ? m : "")), "Slack must not contain SQL");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a missing screen driver is a runner blocker, never a journey failure", () => {
  // Reporting "publishing a plan version did not save" because an npm script is absent would be
  // a lie about the product, and a farm manager would act on it. The driver now ships in
  // apps/admin-web, so the branch is proved directly rather than by deleting it from the repo.
  assert.equal(screenDriverMissing({}), true, "no scripts at all means the driver is missing");
  assert.equal(screenDriverMissing({ "smoke:visual:live": "x" }), true, "another smoke script is not this driver");
  assert.equal(screenDriverMissing({ "smoke:write-journey:live": "node scripts/smoke-write-journey-live.mjs" }), false);
  // And the driver really is wired up, so a real run drives the screen instead of blocking.
  const scripts = JSON.parse(readFileSync(path.join(repo, "apps/admin-web/package.json"), "utf8")).scripts ?? {};
  assert.equal(screenDriverMissing(scripts), false, "apps/admin-web must ship the write-journey screen driver");
});

test("notify-slack's own self-test passes, including the broken-lane isolation case", () => {
  const result = spawnSync(process.execPath, [path.join(here, "notify-slack.mjs"), "--self-test"], { cwd: repo, encoding: "utf8" });
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /self-test passed/);
  // The isolation case must actually have run: the dropped-kind log is its fingerprint.
  assert.match(result.stderr, /finding kind fake-broken failed and was dropped/);
});

test("adding the write-journeys mode did not change what the two existing modes do", () => {
  // The only reason run.mjs's mode logic lives in a pure module: so this can be proved, not read.
  assert.deepEqual(modeFlags("production-smoke", false), {
    isProductionSmoke: true, isWriteJourneys: false, parityIsNeverAGate: true,
    runCertificationExtras: false, dataTrustWhenUnchecked: "not_checked_read_only_smoke"
  });
  assert.deepEqual(modeFlags("production-smoke", true), {
    isProductionSmoke: true, isWriteJourneys: false, parityIsNeverAGate: true,
    runCertificationExtras: true, dataTrustWhenUnchecked: "not_checked_read_only_smoke"
  });
  assert.deepEqual(modeFlags("post-main-certification", false), {
    isProductionSmoke: false, isWriteJourneys: false, parityIsNeverAGate: false,
    runCertificationExtras: true, dataTrustWhenUnchecked: null
  });
});

test("parity never gates the write-path lane, and it never runs the certification extras", () => {
  const flags = modeFlags("write-journeys", true);
  assert.equal(flags.parityIsNeverAGate, true, "a stale parity receipt must not fail a green journey run");
  assert.equal(flags.runCertificationExtras, false, "lighthouse, API latency and the Go lifecycle suites are not this lane's job");
  assert.equal(flags.dataTrustWhenUnchecked, "not_checked_write_clone");
});

test("the real script takes the headline path and exits 0 when lane 1 found nothing", () => {
  // The judge's crash was on exactly this path: lane 1 clean, so hasOwnIssues is false and a
  // finding kind's headline() is actually called. Here it is exercised end to end, for real.
  const dir = mkdtempSync(path.join(tmpdir(), "lane4-headline-"));
  try {
    mkdirSync(path.join(dir, "write-journeys"), { recursive: true });
    writeFileSync(path.join(dir, "write-journeys", "write-journeys-receipt.json"), JSON.stringify({
      status: "blocked",
      journeys: [{
        name: "change-a-feed-rate", page: "Feed Config", url: "https://dashboard.mesha.sg/feed/config",
        story: "A manager changes the feed rate on a ration and the sheet picks it up.",
        status: "fail", humanFailure: "Changing a feed rate did not save. The sheet the team feeds from would keep the old amount.",
        restore: { attempted: true, restored: false, verified: false }
      }]
    }));
    const receiptPath = path.join(dir, "receipt.json");
    writeFileSync(receiptPath, JSON.stringify({
      mode: "write-journeys", status: "fail", repoSha: "abc123456789",
      runtimePolicy: { browserSmoke: "ran_failed" },
      layers: [{ name: "write-journeys", status: "fail", message: "write journeys failed" }], blockers: []
    }));
    const result = spawnSync(process.execPath, [path.join(here, "notify-slack.mjs"), "--receipt", receiptPath], {
      cwd: repo, encoding: "utf8",
      env: { ...process.env, GOATOS_DASHBOARD_SLACK_DRY_RUN: "1", GOATOS_DASHBOARD_SLACK_STATE_FILE: path.join(dir, "state.json") }
    });
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /Practice data was left changed/, "the restore finding must own the headline");
    assert.ok(!result.stdout.includes("Something could not be checked"), "nothing was dropped on a healthy run");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// ---------------------------------------------------------------------------------------------
// A check that did not run must never render a verdict it did not earn.
//
// The defect these pin: with `--simulate-write`, five of the six write journeys have no stand-in
// for the write their screen makes. No write was applied, so the row assertion found nothing, and
// the runner reported the journey's product sentence -- "Publishing a new vaccination plan version
// did not save" -- about a publish that never happened. Five fabricated product defects, posted to
// Slack as if a manager had lost their work.
// ---------------------------------------------------------------------------------------------

/** The mode could not do the thing: no browser, no stand-in. Nothing was written. */
const notAttemptedUi = () => ({
  ok: true,
  status: "not-attempted",
  applied: false,
  reason: "this run had no stand-in for the write this journey makes on the screen, so the write was never made and nothing about it was checked",
  screenshot: null
});

test("a journey whose write this run could not make reports not-attempted, never a failure", () => {
  const engine = fakeEngine();
  let asked = 0;
  const record = runJourney(journey, {
    engine,
    driveUi: notAttemptedUi,
    queryRows: () => { asked += 1; return []; },
    token: "A1",
    screenshotDir: "/tmp"
  });
  assert.equal(record.status, "not-attempted", "a journey that could not be performed is not a failure");
  assert.equal(record.humanFailure, null, "a journey that never ran has no product failure sentence to tell");
  assert.equal(record.failedStep, null);
  assert.equal(record.database.status, "not-attempted");
  assert.equal(record.screen.status, "not-attempted");
  assert.match(record.notAttemptedReason, /no stand-in/, "the receipt must record WHY it was not attempted");
  assert.equal(asked, 0, "the database must not be asked a question this run could not have earned an answer to");
  // The safety contract is unchanged: the snapshot is still taken back, and still proved.
  assert.ok(engine.calls.includes("restore") && engine.calls.includes("verifyRestore"));
});

test("the live vaccination publish failure is still a failure: a real screen run earns its verdict", () => {
  // The screen driver really did drive the browser, and the publish really did fail on screen
  // (the button sticks on "Working…" forever). `applied: true` is what separates this from the
  // case above, and this sentence is a TRUE product defect that must keep reaching Slack.
  const publish = catalogue.journeys.find((j) => j.name === "publish-vaccination-plan-version");
  const engine = fakeEngine();
  const realScreenFailure = () => ({ ok: false, status: "ran", applied: true, failedStep: "publish it", screenshot: null });
  const record = runJourney(publish, { engine, driveUi: realScreenFailure, queryRows: () => [], token: "A1", screenshotDir: "/tmp" });
  assert.equal(record.status, "fail", "a screen that really was driven and really failed is a failure");
  assert.equal(record.humanFailure, publish.humanFailure);
  assert.match(record.humanFailure, /Publishing a new vaccination plan version did not save/);
  assert.equal(record.failedStep, "publish it");
  assert.equal(record.notAttemptedReason, undefined, "a real failure is never swept into the not-attempted bucket");
});

test("a rule journey that never attempts its forbidden operation is not-attempted, not a pass", () => {
  // The false green this replaced: `feed-item-in-use-cannot-be-deleted` asserts "the item a
  // ration names is still there". With no browser and no attempted delete that is just "the
  // fixture data exists", and reporting it as "the business rule holds" is a verdict the run
  // never earned. A false green gets trusted, which makes it worse than a false red.
  const engine = fakeEngine();
  let asked = 0;
  const rule = catalogue.journeys.find((j) => j.name === "feed-item-in-use-cannot-be-deleted");
  const record = runJourney(rule, {
    engine,
    driveUi: notAttemptedUi,
    queryRows: () => { asked += 1; return [["row-id"]]; },
    token: "A1",
    screenshotDir: "/tmp"
  });
  assert.equal(record.status, "not-attempted", "nobody tried the delete, so nothing about the rule is known");
  assert.equal(record.humanFailure, null);
  assert.equal(asked, 0, "the rule's assertion must not be asked when the rule was never put to the test");
  assert.match(record.notAttemptedReason, /never made|not checked|no stand-in/);
});

test("every journey needs its own action attempted - rule journeys included, no exceptions", () => {
  // There is no longer a category of journey that earns a verdict without doing anything.
  const engine = fakeEngine();
  for (const name of ["feed-item-in-use-cannot-be-deleted", "published-plan-version-stays-locked", "partition-label-survives-a-move"]) {
    const rule = catalogue.journeys.find((j) => j.name === name);
    const record = runJourney(rule, { engine, driveUi: notAttemptedUi, queryRows: () => [["1"]], token: "A1", screenshotDir: "/tmp" });
    assert.equal(record.status, "not-attempted", `${name} must not report a verdict it did not earn`);
  }
});

// ---------------------------------------------------------------------------------------------
// Where the database itself enforces the rule, the rule CAN be genuinely proved without a screen.
// ---------------------------------------------------------------------------------------------

const lockedRule = catalogue.journeys.find((j) => j.name === "published-plan-version-stays-locked");
const refusalError = (detail) => { const e = new Error("database statement failed (exit 3)"); e.serverDetail = detail; throw e; };

test("a rule whose forbidden operation is refused for the right reason earns its pass", () => {
  const ui = attemptRefusal(lockedRule, (sql) => {
    if (/select 1::text/.test(sql)) return [["1"]];
    return refusalError("ERROR: protocol version 7 is published, not draft; published config is immutable");
  });
  assert.equal(ui.applied, true, "the operation really was attempted");
  assert.equal(ui.ok, true, "and it really was refused");
  assert.equal(ui.status, "refused");
});

test("a rule whose forbidden operation GOES THROUGH is a real, earned failure", () => {
  const ui = attemptRefusal(lockedRule, (sql) => (/select 1::text/.test(sql) ? [["1"]] : []));
  assert.equal(ui.applied, true);
  assert.equal(ui.ok, false, "the guard did not hold, and that is a finding");
  const engine = fakeEngine();
  const record = runJourney(lockedRule, { engine, driveUi: () => ui, queryRows: () => [["0"]], token: "A1", screenshotDir: "/tmp" });
  assert.equal(record.status, "fail");
  assert.equal(record.humanFailure, lockedRule.humanFailure);
  assert.equal(record.screen.status, "parked", "no screen was driven, so the receipt must not claim one failed");
  assert.ok(engine.calls.includes("restore") && engine.calls.includes("verifyRestore"), "an attempted write is still put back and proved");
});

test("a refusal for the WRONG reason is not-attempted, never a pass", () => {
  // A missing column or an unrelated constraint also throws. Counting any error as "refused"
  // would be the same false green in a new place.
  const ui = attemptRefusal(lockedRule, (sql) => {
    if (/select 1::text/.test(sql)) return [["1"]];
    return refusalError('ERROR: column "created_at" does not exist');
  });
  assert.equal(ui.applied, false, "an unrelated error means the rule was never put to the test");
  assert.equal(ui.status, "not-attempted");
  assert.match(ui.reason, /never put to the test/);
});

test("a rule with nothing to attempt it against is not-attempted, never a pass", () => {
  const ui = attemptRefusal(lockedRule, () => []);
  assert.equal(ui.applied, false, "no published version to try it on means the guard was never tested");
  assert.equal(ui.status, "not-attempted");
  assert.match(ui.reason, /nothing to try this rule against/);
});

test("a refusal attempt must say how a real refusal is recognised, and what to try it against", () => {
  const base = catalogue.journeys[0];
  const noMatcher = { journeys: [{ ...base, simulatedRefusal: { sql: "update x set y = y", precondition: { sql: "select 1" } } }] };
  assert.ok(validateCatalogue(noMatcher).some((p) => /any error at all would be read as the rule holding/.test(p)));
  const noPrecondition = { journeys: [{ ...base, simulatedRefusal: { sql: "update x set y = y", refusedWhenErrorMatches: "nope" } }] };
  assert.ok(validateCatalogue(noPrecondition).some((p) => /never be put to the test/.test(p)));
});

test("a run where nothing was attempted is its own status, distinct from a run that failed", () => {
  const gap = { status: "not-attempted", restore: { attempted: true, restored: true, verified: true } };
  const pass = { status: "pass", restore: { attempted: true, restored: true, verified: true } };
  const fail = { status: "fail", restore: { attempted: true, restored: true, verified: true } };

  const nothingRan = summarise([gap, gap, gap]);
  assert.equal(nothingRan.status, "not-run", "nothing ran is not a pass");
  assert.equal(nothingRan.failed, 0, "nothing ran is not a failure either");
  assert.equal(nothingRan.notAttempted, 3);
  assert.equal(nothingRan.attempted, 0);

  const mixed = summarise([pass, gap, gap]);
  assert.equal(mixed.status, "pass", "a gap alongside a real pass does not invent a failure");
  assert.equal(mixed.notAttempted, 2);

  const withFailure = summarise([pass, gap, fail]);
  assert.equal(withFailure.status, "fail", "a real failure still outranks the gaps");
  assert.equal(withFailure.failed, 1, "the gaps are never counted as failures");
});

test("the exit code tells 'nothing ran' apart from 'something failed'", () => {
  const of = (journeys) => exitCodeFor(summarise(journeys));
  const gap = { status: "not-attempted", restore: { attempted: true, restored: true, verified: true } };
  const pass = { status: "pass", restore: { attempted: true, restored: true, verified: true } };
  const fail = { status: "fail", restore: { attempted: true, restored: true, verified: true } };
  const unproved = { status: "pass", restore: { attempted: true, restored: true, verified: false } };

  // Lane 5's code for "nothing about this was checked". The layer above must never read this
  // as a product failure, and must never read it as a clean run either.
  assert.equal(of([gap, gap, gap]), 3, "nothing ran must exit 3");
  assert.equal(of([pass, pass]), 0, "a real pass exits 0");
  assert.equal(of([pass, gap]), 0, "a gap beside a real pass is not a failure");
  assert.equal(of([pass, gap, fail]), 1, "a real failure exits 1, never 3");
  assert.equal(of([unproved]), 1, "a clone left changed still exits 1");
  assert.notEqual(of([gap]), of([fail]), "'nothing ran' and 'something failed' must not share an exit code");
});

test("the runner still refuses to run at all without a target database", () => {
  const result = spawnSync(process.execPath, [runner, "--simulate-write"], {
    cwd: repo,
    encoding: "utf8",
    env: { ...process.env, GOATOS_WRITE_JOURNEY_DATABASE_URL: "" }
  });
  assert.equal(result.status, 1, "no database URL is a runner error, not a product finding and not a gap");
  assert.ok(!result.stdout.includes("did not save"), "and it invents no product sentence on the way out");
});

// ---------------------------------------------------------------------------------------------
// Slack: a gap is reported as a gap. It is never silent, and it is never an accusation.
// ---------------------------------------------------------------------------------------------

test("a not-attempted journey produces no failure finding; a real failure still produces one", () => {
  const gapOnly = toFindingsFromJourneys([{
    name: "publish-vaccination-plan-version",
    page: "Vaccination Plan Edit",
    story: "A manager publishes a new vaccination plan version.",
    status: "not-attempted",
    // The receipt still carries the catalogue sentence; Slack must not reach for it.
    humanFailure: "Publishing a new vaccination plan version did not save. A manager who publishes a plan would be told it worked and find nothing there.",
    restore: { attempted: true, restored: true, verified: true }
  }]);
  assert.equal(gapOnly.filter((f) => f.kind === "journey").length, 0, "a journey that never ran is not a journey failure");
  assert.equal(gapOnly.filter((f) => f.kind === "not-checked").length, 1);
  const rendered = JSON.stringify([writeJourneysKind.renderSection(gapOnly), writeJourneysKind.summaryText(gapOnly)]);
  assert.ok(!rendered.includes("did not save"), "the fabricated product sentence must never reach Slack");
  assert.ok(!rendered.includes("did not work"), "a gap must not borrow the wording of a real failure");

  const realFailure = toFindingsFromJourneys([{
    name: "publish-vaccination-plan-version",
    page: "Vaccination Plan Edit",
    story: "A manager publishes a new vaccination plan version.",
    status: "fail",
    humanFailure: "Publishing a new vaccination plan version did not save. A manager who publishes a plan would be told it worked and find nothing there.",
    failedStep: "publish it",
    restore: { attempted: true, restored: true, verified: true }
  }]);
  assert.equal(realFailure.filter((f) => f.kind === "journey").length, 1, "a real failure must still be a finding");
  assert.match(JSON.stringify(writeJourneysKind.renderSection(realFailure)), /Publishing a new vaccination plan version did not save/);
});

test("a run that checked nothing says so in Slack; silence would read as fine", () => {
  const gaps = toFindingsFromJourneys([
    { name: "a", page: "Vaccination Plan Edit", status: "not-attempted", restore: {} },
    { name: "b", page: "Tasks", status: "not-attempted", restore: {} }
  ]);
  const blocks = writeJourneysKind.renderSection(gaps);
  assert.ok(blocks.length > 0, "a run that checked nothing must not go silent");
  const text = JSON.stringify(blocks);
  assert.match(text, /Not checked: things people do on the site/);
  assert.match(text, /not a fault in the site/, "it must say plainly that this is a gap, not an accusation");
  // Lane 5's rule, kept here: a run that checked nothing never owns the alert's headline.
  assert.equal(writeJourneysKind.headline(gaps), null, "a gap must not take the headline");
  assert.notEqual(writeJourneysKind.summaryText(gaps), "", "the fallback text must carry the gap too");
  assert.equal(writeJourneysKind.renderReplies(gaps).length, 0, "a gap has no screenshot and gets no threaded reply");
  assert.doesNotThrow(() => writeJourneysKind.assertPlainEnglish(text, "gap blocks"));
});

test("a real failure and a gap in the same run stay separate things in Slack", () => {
  const mixed = toFindingsFromJourneys([
    {
      name: "publish-vaccination-plan-version",
      page: "Vaccination Plan Edit",
      status: "fail",
      humanFailure: "Publishing a new vaccination plan version did not save. A manager who publishes a plan would be told it worked and find nothing there.",
      failedStep: "publish it",
      restore: { attempted: true, restored: true, verified: true }
    },
    { name: "record-a-sale", page: "Sales Loads", status: "not-attempted", restore: {} }
  ]);
  const text = JSON.stringify(writeJourneysKind.renderSection(mixed));
  assert.match(text, /Doing this on the site did not work/);
  assert.match(text, /Not checked: things people do on the site/);
  assert.match(writeJourneysKind.headline(mixed), /did not work/, "a real failure owns the headline, not the gap");
  assert.match(writeJourneysKind.summaryText(mixed), /1 thing a person does on the site did not work/);
  assert.match(writeJourneysKind.summaryText(mixed), /1 thing a person does on the site was not checked today/);
});

test("an all-not-attempted receipt reaches Slack as a gap, through the real notifier", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "lane4-gap-slack-"));
  try {
    mkdirSync(path.join(dir, "write-journeys"), { recursive: true });
    writeFileSync(path.join(dir, "write-journeys", "write-journeys-receipt.json"), JSON.stringify({
      status: "not-run",
      summary: { total: 6, attempted: 0, failed: 0, notAttempted: 6, restoreProblems: 0, status: "not-run" },
      journeys: [
        { name: "publish-vaccination-plan-version", page: "Vaccination Plan Edit", url: "https://dashboard.mesha.sg/vaccination/plan/edit", status: "not-attempted", notAttemptedReason: "no stand-in for the write", humanFailure: null, restore: { attempted: true, restored: true, verified: true } },
        { name: "record-a-sale", page: "Sales Loads", url: "https://dashboard.mesha.sg/sales", status: "not-attempted", notAttemptedReason: "no stand-in for the write", humanFailure: null, restore: { attempted: true, restored: true, verified: true } }
      ]
    }));
    const receiptPath = path.join(dir, "receipt.json");
    writeFileSync(receiptPath, JSON.stringify({
      mode: "write-journeys", status: "fail", repoSha: "abc123456789",
      runtimePolicy: { browserSmoke: "ran_failed" },
      layers: [{ name: "write-journeys", status: "fail", message: "write journeys did not run" }],
      blockers: []
    }));
    const result = spawnSync(process.execPath, [path.join(here, "notify-slack.mjs"), "--receipt", receiptPath], {
      cwd: repo, encoding: "utf8",
      env: { ...process.env, GOATOS_DASHBOARD_SLACK_DRY_RUN: "1", GOATOS_DASHBOARD_SLACK_STATE_FILE: path.join(dir, "state.json") }
    });
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /Not checked: things people do on the site/);
    assert.match(result.stdout, /not a fault in the site/);
    assert.ok(!result.stdout.includes("did not save"), "no fabricated product sentence may reach Slack");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
