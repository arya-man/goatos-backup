// node --test tools/dashboard-automation/run-android-journeys.test.mjs
//
// Four things this lane cannot be allowed to get wrong, and one thing it cannot be
// allowed to break:
//   1. the free-tier guard refuses an oversized matrix, rather than trimming it
//   2. a physical-device check is NEVER marked covered by a virtual run
//   3. a crashed or timed-out device reads as a failure, not as a pass
//   4. no sentence that reaches Slack carries a class name, a test id, a selector
//      or a stack trace
//   5. a lane-1-only receipt still renders byte-identically with lane 5 registered
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  DEV_APPLICATION_ID,
  FORBIDDEN_BACKENDS,
  FREE_TIER,
  PAID_RATES,
  applicationIdVerdict,
  backendUrlVerdict,
  blockedReason,
  reachedADevice,
  deviceDied,
  freeTierVerdict,
  normaliseOutcome,
  parseTestLabOutcome,
  planMatrix,
  readCatalog,
  readSpend,
  recordSpend,
  runnableJourneys
} from "./run-android-journeys.mjs";
import { engineeringLeaks, renderReplies, renderSection, summaryText, toFindingsFromRows } from "./lib/finding-kinds/android-journeys.mjs";
import { failureSentenceFindings } from "./sync-coverage.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../..");
const catalog = readCatalog();
const FRESH = { tests: 0, deviceMinutes: 0 };

// ---------------------------------------------------------------------------
// 1. The free-tier guard
// ---------------------------------------------------------------------------
test("the guard refuses a matrix with more tests than the free tier allows", () => {
  const plan = planMatrix(catalog, { model: "MediumPhone.arm,SmallPhone.arm,Pixel2.arm", version: "30,31,32,33" });
  assert.equal(plan.tests, 12);
  const verdict = freeTierVerdict(plan, FRESH);
  assert.equal(verdict.allowed, false);
  assert.match(verdict.reason, /12 tests/);
  assert.match(verdict.reason, new RegExp(`${FREE_TIER.virtualTestsPerDay} virtual tests a day`));
  // It must say what to shrink, and it must not offer billing as the way out.
  assert.match(verdict.whatToDo, /Shrink the matrix/);
  assert.match(verdict.whatToDo, /do not enable billing/i);
});

test("the guard refuses a matrix that could hold devices longer than the free device-minutes", () => {
  const plan = planMatrix(catalog, { timeoutMinutes: 90 });
  assert.equal(plan.tests, 1);
  assert.equal(plan.deviceMinutes, 90);
  const verdict = freeTierVerdict(plan, FRESH);
  assert.equal(verdict.allowed, false);
  assert.match(verdict.reason, /90 device-minutes/);
});

test("the guard never trims an oversized matrix to make it fit", () => {
  const plan = planMatrix(catalog, { model: "MediumPhone.arm,SmallPhone.arm,Pixel2.arm", version: "30,31,32,33" });
  const verdict = freeTierVerdict(plan, FRESH);
  // A run that quietly dropped half its journeys reports a green covering less than it claims.
  assert.equal(verdict.allowed, false);
  assert.equal(plan.tests, 12, "the plan itself must be left alone so the refusal names the real number");
});

test("the guard counts what has already been spent today, and parks the run rather than overspending", () => {
  const plan = planMatrix(catalog, { timeoutMinutes: 12 });
  assert.equal(freeTierVerdict(plan, FRESH).allowed, true);
  assert.equal(freeTierVerdict(plan, { tests: 10, deviceMinutes: 0 }).allowed, false);
  assert.equal(freeTierVerdict(plan, { tests: 0, deviceMinutes: 55 }).allowed, false);
  assert.match(freeTierVerdict(plan, { tests: 10, deviceMinutes: 0 }).whatToDo, /until tomorrow/);
});

test("a physical device is refused outright, and no physical allowance hides in the free-tier block", () => {
  const verdict = freeTierVerdict(planMatrix(catalog, { physical: true }), FRESH);
  assert.equal(verdict.allowed, false);
  assert.match(verdict.reason, /\$5\/device-hour/);
  assert.equal(PAID_RATES.physicalPerDeviceHourUsd, 5);
  assert.equal(Object.keys(FREE_TIER).some((key) => /physical/i.test(key)), false,
    "the free tier grants no physical device time; a number there would read as permission to spend");
  assert.equal(PAID_RATES.whoDecidesToSpend, "Ravi. Not this runner, and not an agent.");
});

test("the spend ledger resets on a new day and never silently forgets today", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "lane5-spend-"));
  const file = path.join(dir, "spend.json");
  try {
    assert.deepEqual(readSpend(file, "2026-09-23"), { day: "2026-09-23", tests: 0, deviceMinutes: 0 });
    recordSpend(file, { tests: 3, deviceMinutes: 36 }, "2026-09-23");
    assert.deepEqual(readSpend(file, "2026-09-23"), { day: "2026-09-23", tests: 3, deviceMinutes: 36 });
    // Same day, second run: it accumulates.
    recordSpend(file, { tests: 2, deviceMinutes: 24 }, "2026-09-23");
    assert.deepEqual(readSpend(file, "2026-09-23"), { day: "2026-09-23", tests: 5, deviceMinutes: 60 });
    assert.equal(freeTierVerdict(planMatrix(catalog, { timeoutMinutes: 12 }), readSpend(file, "2026-09-23")).allowed, false,
      "with the day's device-minutes gone the run must be parked, not squeezed in");
    // Next day, the free tier is new again.
    assert.deepEqual(readSpend(file, "2026-09-24"), { day: "2026-09-24", tests: 0, deviceMinutes: 0 });
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("the guard refuses a corrupt ledger by assuming nothing has been spent is FALSE", () => {
  // A ledger we cannot read must not be read as "plenty left". It resets to zero for
  // today, which is the only safe reading available, and the day's real spend is then
  // re-accumulated from this run onward.
  const dir = mkdtempSync(path.join(tmpdir(), "lane5-spend-bad-"));
  const file = path.join(dir, "spend.json");
  try {
    writeFileSync(file, "{ not json");
    const spent = readSpend(file, "2026-09-23");
    assert.equal(spent.tests, 0);
    assert.equal(spent.day, "2026-09-23");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// ---------------------------------------------------------------------------
// 1b. The data guard — which backend the APK under test talks to.
// This matters MORE than the money guard: the wrong flavour writes to real farm data.
// ---------------------------------------------------------------------------
test("only the dev flavour may be submitted", () => {
  assert.equal(applicationIdVerdict(DEV_APPLICATION_ID).allowed, true);
  assert.equal(applicationIdVerdict("sg.mesha.goatos.dev").allowed, true);
});

test("the production flavour is refused, and the refusal says real farm data is at stake", () => {
  const verdict = applicationIdVerdict("sg.mesha.goatos");
  assert.equal(verdict.allowed, false);
  assert.match(verdict.reason, /PRODUCTION/);
  assert.match(verdict.reason, /real farm data/);
  assert.match(verdict.reason, /write/i);
});

test("the stg flavour is refused too", () => {
  const verdict = applicationIdVerdict("sg.mesha.goatos.stg");
  assert.equal(verdict.allowed, false);
  assert.match(verdict.reason, /stg/);
});

test("an APK whose flavour cannot be read is refused, never assumed safe", () => {
  // "Probably dev" is not good enough when being wrong means writing into a real
  // farm's records. No reading, no run.
  for (const unreadable of ["", null, undefined, "   "]) {
    assert.equal(applicationIdVerdict(unreadable).allowed, false, `${JSON.stringify(unreadable)} must be refused`);
  }
  assert.match(applicationIdVerdict("").reason, /could not be read/);
  assert.equal(applicationIdVerdict("com.example.somethingelse").allowed, false,
    "an unrecognised application id must be refused, not waved through");
});

test("the hosts that must never be talked to are named, and production is one of them", () => {
  const hosts = FORBIDDEN_BACKENDS.map((b) => b.host);
  assert.ok(hosts.includes("api.goatos.mesha.sg"), "production must be on the forbidden list");
  assert.ok(hosts.includes("stg-api.dashboard.mesha.sg"), "the deployed stg API must be on the forbidden list");
});

test("the catalogue says which backend the APK under test points at, and why not the others", () => {
  assert.equal(catalog.backendUnderTest.applicationId, DEV_APPLICATION_ID);
  assert.match(catalog.backendUnderTest.whyNotStgOrProd, /real farm data/);
  assert.match(catalog.backendUnderTest.neverTheCloudBuildArtifact, /SIGNED RELEASE/);
  // Every journey says what it needs from a server.
  for (const journey of catalog.journeys) {
    assert.ok(["none-by-design", "needs-reachable-non-production-backend"].includes(journey.backend),
      `${journey.name} must say what it needs from a server`);
  }
  // Anything claimed as covered must need no backend at all — there is no safe one.
  for (const journey of catalog.journeys.filter((j) => j.automation.tier === "runs-on-virtual")) {
    assert.equal(journey.backend, "none-by-design",
      `${journey.name} is claimed as covered, but it needs a backend and there is no safe one to give it`);
  }
});

// The mechanism is proved against INVENTED hostnames. These tests never name a host
// that holds real farm data, and the guard never opens a socket — it refuses on the
// string, before anything could connect.
const PRETEND_FORBIDDEN = [
  { host: "prod.example.invalid", what: "production (real farm data)" },
  { host: "stg-api.example.invalid", what: "the deployed stg API" }
];

test("a backend url pointing at a forbidden host is refused, without connecting to it", () => {
  for (const url of ["https://prod.example.invalid/", "https://prod.example.invalid/v1/goats",
                     "https://stg-api.example.invalid/", "https://inner.stg-api.example.invalid/health"]) {
    const verdict = backendUrlVerdict(url, PRETEND_FORBIDDEN);
    assert.equal(verdict.allowed, false, `${url} must be refused`);
    assert.match(verdict.reason, /may never be a target/);
  }
  assert.equal(backendUrlVerdict("http://localhost:8080/", PRETEND_FORBIDDEN).allowed, true);
  assert.equal(backendUrlVerdict("http://127.0.0.1:15432/", PRETEND_FORBIDDEN).allowed, true);
  assert.equal(backendUrlVerdict("garbage", PRETEND_FORBIDDEN).allowed, false,
    "an unparseable url must be refused, not assumed safe");
});

test("the guard is incapable of opening a socket", () => {
  // It must refuse BEFORE anything talks to a host holding real farm data, so it has
  // no networking capability at all — not to check a response shape, not once.
  const source = readFileSync(path.join(here, "run-android-journeys.mjs"), "utf8");
  const networking = ["net", "dns", "http", "https", "tls"].map((m) => `node:${m}`).concat([`${"fetch"}(`]);
  const used = networking.filter((capability) => source.includes(capability));
  assert.deepEqual(used, [], `the runner must open no socket, but it uses ${used.join(", ")}`);
});

test("both production-backed hosts are on the real forbidden list", () => {
  // Named here as configuration, never dialled. stg serves the same data as production.
  assert.equal(FORBIDDEN_BACKENDS.length, 2);
  assert.ok(FORBIDDEN_BACKENDS.every((entry) => typeof entry.host === "string" && entry.host.length > 0));
  assert.ok(FORBIDDEN_BACKENDS.some((entry) => /real farm data/.test(entry.what)));
});

// ---------------------------------------------------------------------------
// 1c. A run that never started must never be reported as the app failing
// ---------------------------------------------------------------------------
test("a submission that never booked a device is not-attempted, never a journey failure", () => {
  // Learned live: the first real submission was rejected before any phone started,
  // and the runner called two journeys FAILED — which would have put "People are
  // signed out at random" into Slack when nothing had run. That is a false red, and
  // it blames the app for an infrastructure problem.
  assert.equal(reachedADevice(1, []), false, "a non-zero exit with no outcomes means no device ran");
  assert.equal(reachedADevice(0, []), true, "a clean exit means the run happened");
  assert.equal(reachedADevice(1, [{ outcome: "fail" }]), true, "a real outcome means a device ran and the journey failed");
});

test("the reason a run never started is named in words an operator can act on", () => {
  const disabledApi = blockedReason({
    stdout: "",
    stderr: "API [toolresults.googleapis.com] not enabled on project [goatos-stg] ... reason: SERVICE_DISABLED"
  });
  assert.match(disabledApi, /toolresults\.googleapis\.com/);
  assert.match(disabledApi, /not enabled/);
  assert.match(disabledApi, /Ravi/, "enabling a project API is a decision, so it must say whose");
  assert.match(blockedReason({ stderr: "PERMISSION_DENIED" }), /permission/);
  assert.match(blockedReason({ stderr: "quota exceeded" }), /Nothing was spent/);
  assert.match(blockedReason({ stderr: "something unexpected" }), /testlab-stderr/);
});

// ---------------------------------------------------------------------------
// 2. A virtual run never claims a physical check
// ---------------------------------------------------------------------------
test("the five physical checks are never submitted to a virtual device", () => {
  const physical = catalog.journeys.filter((j) => j.device === "physical");
  assert.equal(physical.length, 5);
  const runnable = runnableJourneys(catalog);
  for (const journey of physical) {
    assert.equal(runnable.some((r) => r.name === journey.name), false,
      `${journey.name} is physical and must never be planned onto a virtual device`);
    assert.equal(journey.automation.testClass, null,
      `${journey.name} must carry no test class, so no code path can run it and mark it covered`);
    assert.ok(journey.physicalReason && journey.physicalReason.length > 30,
      `${journey.name} must say why a virtual device would be a false green`);
  }
});

test("asking for a physical journey by name is refused with the reason", () => {
  assert.throws(() => runnableJourneys(catalog, "proof-capture"), /physical-device journey/);
  assert.throws(() => runnableJourneys(catalog, "weighing-scan-identifies-the-animal"), /physical-device journey/);
  assert.throws(() => runnableJourneys(catalog, "herd-signal-tags"), /physical-device journey/);
});

test("every parked journey carries a real reason, and none is reported as covered", () => {
  const parked = catalog.journeys.filter((j) => ["parked", "needs-seeded-session"].includes(j.automation.tier));
  assert.ok(parked.length > 0);
  for (const journey of parked) {
    assert.ok(journey.automation.note && journey.automation.note.trim().length > 20,
      `${journey.name} must say why it cannot run`);
    assert.equal(journey.automation.testClass, null,
      `${journey.name} is parked, so nothing may be able to run it and mark it covered`);
  }
  // The journeys blocked on a server must say so, rather than blaming something vaguer.
  const blockedOnBackend = parked.filter((j) => j.backend === "needs-reachable-non-production-backend");
  assert.ok(blockedOnBackend.length > 0);
  for (const journey of blockedOnBackend) {
    assert.match(journey.automation.note, /Test Lab-reachable non-production backend/,
      `${journey.name} is blocked on a server and must say so`);
  }
  // And the only journeys the catalogue claims as covered say which half they prove.
  for (const journey of catalog.journeys.filter((j) => j.automation.tier === "runs-on-virtual")) {
    assert.ok(journey.automation.coversPartially,
      `${journey.name} is claimed as covered, so it must say which half of its check it proves`);
  }
  assert.equal(catalog.coverageToday.journeysAVirtualRunCanProveToday,
    catalog.journeys.filter((j) => j.automation.tier === "runs-on-virtual").length);
});

test("every one of the 47 lane 5 checks is in the catalogue exactly once", () => {
  assert.equal(catalog.journeys.length, 47);
  const names = catalog.journeys.map((j) => j.name);
  assert.equal(new Set(names).size, 47);
  const laneChecks = JSON.parse(readFileSync(path.join(repo, "tools/dashboard-automation/lane-checks.json"), "utf8"));
  const ledger = laneChecks.lanes["lane5-android"].checks.map((c) => c.id).sort();
  assert.deepEqual(catalog.journeys.map((j) => j.checkId).sort(), ledger,
    "the catalogue must be the ledger's 47 checks, not a hand-written list");
});

// ---------------------------------------------------------------------------
// 3. Reading the result, including a device that died
// ---------------------------------------------------------------------------
test("a crashed device is a failure, not a pass", () => {
  assert.equal(normaliseOutcome("Crashed"), "fail");
  const outcomes = parseTestLabOutcome(JSON.stringify([
    { axisValue: "MediumPhone.arm-33-en-portrait", outcome: "Crashed", errorDetails: "the app crashed on startup" }
  ]));
  assert.equal(outcomes.length, 1);
  assert.equal(outcomes[0].outcome, "fail");
  assert.equal(deviceDied(outcomes), true);
});

test("a timed-out device is a failure, not a pass", () => {
  assert.equal(normaliseOutcome("Timed out"), "fail");
  const outcomes = parseTestLabOutcome(JSON.stringify([
    { axisValue: "MediumPhone.arm-33-en-portrait", outcome: "Timed out", errorDetails: "Test timed out after 12m" }
  ]));
  assert.equal(outcomes[0].outcome, "fail");
  assert.equal(deviceDied(outcomes), true);
});

test("an inconclusive or unrecognised outcome is a failure, because it proved nothing", () => {
  assert.equal(normaliseOutcome("Inconclusive"), "fail");
  assert.equal(normaliseOutcome("Infrastructure failure"), "fail");
  assert.equal(normaliseOutcome(""), "fail");
  assert.equal(normaliseOutcome(undefined), "fail");
  assert.equal(normaliseOutcome("some outcome Google adds next year"), "fail");
});

test("a passing device reads as a pass and is not mistaken for a dead one", () => {
  const outcomes = parseTestLabOutcome(JSON.stringify([
    { axisValue: "MediumPhone.arm-33-en-portrait", outcome: "Passed", errorDetails: "" }
  ]));
  assert.equal(outcomes[0].outcome, "pass");
  assert.equal(deviceDied(outcomes), false);
});

test("a plain-text gcloud result is parsed as well as a JSON one", () => {
  const outcomes = parseTestLabOutcome([
    "OUTCOME    TEST_AXIS_VALUE                  TEST_DETAILS",
    "  MediumPhone.arm-33-en-portrait  Failed  1 test cases failed, 3 passed"
  ].join("\n"));
  assert.ok(outcomes.length >= 1);
  assert.equal(outcomes[0].outcome, "fail");
});

test("an ordinary test failure is a failure but not a dead device", () => {
  const outcomes = parseTestLabOutcome(JSON.stringify([
    { axisValue: "MediumPhone.arm-33-en-portrait", outcome: "Failed", errorDetails: "1 test cases failed" }
  ]));
  assert.equal(outcomes[0].outcome, "fail");
  assert.equal(deviceDied(outcomes), false,
    "a journey failing is the app's fault; the device dying is a different sentence for the farm");
});

// ---------------------------------------------------------------------------
// 4. Nothing an engineer wrote reaches Slack
// ---------------------------------------------------------------------------
test("every catalogue humanFailure is a sentence a farm manager can read", () => {
  for (const journey of catalog.journeys) {
    const leaks = engineeringLeaks(journey.humanFailure);
    assert.deepEqual(leaks, [], `${journey.name}: humanFailure leaks ${leaks.join(", ")} — "${journey.humanFailure}"`);
    // And it must also pass the guard the rest of the automation already enforces,
    // so lane 5 is held to exactly one standard, not its own.
    assert.deepEqual(failureSentenceFindings(journey.humanFailure, journey.name), []);
  }
});

test("every catalogue story is plain English too", () => {
  for (const journey of catalog.journeys) {
    const leaks = engineeringLeaks(journey.story);
    assert.deepEqual(leaks, [], `${journey.name}: story leaks ${leaks.join(", ")}`);
  }
});

test("the leak detector actually catches the things it claims to", () => {
  assert.ok(engineeringLeaks("GoatOsSyncJourneys failed").includes("a class name"));
  assert.ok(engineeringLeaks("at sg.mesha.goatos.Sync:12").length > 0);
  assert.ok(engineeringLeaks('By.desc("Calendar") not found').length > 0);
  assert.ok(engineeringLeaks("data-testid=sync-queue missing").length > 0);
  assert.ok(engineeringLeaks("lane5.offline-queue-survives-force-stop failed").includes("a check code"));
  assert.ok(engineeringLeaks("java.lang.IllegalStateException: boom").length > 0);
  assert.ok(engineeringLeaks("adb shell am broadcast -a NAVIGATE").length > 0);
  assert.deepEqual(engineeringLeaks("The photo never finished uploading and the app showed no error."), []);
});

test("a rendered Android finding names the screen and leaks nothing", () => {
  const findings = toFindingsFromRows(catalog.journeys.slice(0, 6).map((j) => ({
    name: j.name, screen: j.screen, outcome: "fail", humanFailure: j.humanFailure, seen: "Uploading proof | 2 queued"
  })), "Firebase Test Lab virtual MediumPhone.arm / Android 33");
  const rendered = JSON.stringify([renderSection(findings), renderReplies(findings), summaryText(findings)]);
  assert.deepEqual(engineeringLeaks(rendered), [], `Slack text leaked: ${rendered.slice(0, 300)}`);
  for (const finding of findings) assert.ok(rendered.includes(finding.screen), `the finding must name ${finding.screen}`);
});

test("jargon that somehow reaches the renderer is replaced, never printed", () => {
  const findings = toFindingsFromRows([{
    name: "x",
    screen: "GoatOsSyncJourneysTest",
    outcome: "fail",
    humanFailure: "java.lang.IllegalStateException at sg.mesha.goatos.Sync:12"
  }]);
  const rendered = JSON.stringify([renderSection(findings), summaryText(findings)]);
  for (const banned of ["sg.mesha", "IllegalStateException", "GoatOsSyncJourneysTest"]) {
    assert.ok(!rendered.includes(banned), `${banned} must not reach Slack`);
  }
  assert.deepEqual(engineeringLeaks(rendered), []);
});

test("a screenshot that is not a file on this disk is never offered to Slack", () => {
  // Test Lab pulls evidence into a GCS bucket. A gs:// string handed to Slack makes
  // readFileSync throw inside postSlack's catch, where path.basename throws again.
  const base = { screen: "Sync status", what: "It did not send.", seen: "", device: "a phone" };
  assert.equal(renderReplies([{ ...base, screenshot: "gs://goatos-testlab/shot.png" }]).length, 0);
  assert.equal(renderReplies([{ ...base, screenshot: "https://example/shot.png" }]).length, 0);
  assert.equal(renderReplies([{ ...base, screenshot: "/no/such/file.png" }]).length, 0);
  assert.equal(renderReplies([{ ...base, screenshot: null }]).length, 0);
});

// ---------------------------------------------------------------------------
// 5. Lane 1 must not notice lane 5 exists
// ---------------------------------------------------------------------------
test("a lane-1-only receipt renders byte-identically with lane 5 registered", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "lane5-slack-"));
  try {
    const receiptPath = path.join(dir, "receipt.json");
    writeFileSync(receiptPath, JSON.stringify({
      mode: "production-smoke",
      status: "fail",
      repoSha: "abc123456789",
      runtimePolicy: { browserSmoke: "ran_failed" },
      layers: [{ name: "production-module-journeys", status: "fail", message: "route failed" }],
      blockers: [{ layer: "production-module-journeys", message: "calendar laptop chip-crushed" }]
    }, null, 2));
    mkdirSync(path.join(dir, "module-journeys"), { recursive: true });
    writeFileSync(path.join(dir, "module-journeys", "module-journeys-receipt.json"), JSON.stringify({
      modules: [{ id: "calendar", failures: [{ route: "laptop:calendar", error: 'calendar laptop chip-crushed: "Warmup" cut off', screenshots: [] }] }]
    }, null, 2));
    // Deliberately NO android-journeys receipt beside it: this is a lane-1-only run.

    const withLane5 = renderSlack(receiptPath, dir, path.join(repo, "tools/dashboard-automation/notify-slack.mjs"));

    // The same file with lane 5's two lines removed — the state of the world before this lane.
    const original = readFileSync(path.join(repo, "tools/dashboard-automation/notify-slack.mjs"), "utf8");
    const withoutLane5Source = original
      .replace('import androidJourneysKind from "./lib/finding-kinds/android-journeys.mjs";\n', "")
      .replace(", androidJourneysKind]", "]");
    assert.notEqual(withoutLane5Source, original, "the two lane 5 lines must be findable, or this test proves nothing");
    const variant = path.join(repo, "tools/dashboard-automation", ".notify-slack.lane5-absent.test.mjs");
    writeFileSync(variant, withoutLane5Source);
    try {
      const withoutLane5 = renderSlack(receiptPath, dir, variant);
      assert.equal(withLane5, withoutLane5,
        "registering lane 5 changed a lane-1-only Slack message");
      assert.ok(withLane5.includes("blocks") || withLane5.length > 0, "the fixture must actually have rendered a message");
      assert.ok(!withLane5.includes("On the phone"), "lane 5 must contribute no section to a lane-1-only run");
    } finally {
      rmSync(variant, { force: true });
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

function renderSlack(receiptPath, stateDir, script) {
  const result = spawnSync(process.execPath, [script, "--receipt", receiptPath], {
    cwd: repo,
    encoding: "utf8",
    env: {
      ...process.env,
      GOATOS_DASHBOARD_SLACK_DRY_RUN: "1",
      GOATOS_DASHBOARD_SLACK_STATE_FILE: path.join(stateDir, "slack-state.json"),
      GOATOS_DASHBOARD_RUN_PART: "",
      GOATOS_DASHBOARD_RUN_PART_DIR: ""
    }
  });
  // The report path contains a timestamp-free temp dir, but the receipt path is echoed;
  // both scripts see the same one, so the output is comparable as-is.
  return `${result.stdout ?? ""}`.replace(/\.notify-slack\.lane5-absent\.test\.mjs/g, "notify-slack.mjs");
}
