#!/usr/bin/env node
// Lane 5 — build the Android app, submit the farm journeys to Firebase Test Lab
// VIRTUAL devices, wait for them, and pull back the screenshots.
//
// The budget is the whole point of this file. Firebase Test Lab's free tier is 10
// virtual tests and 60 device-minutes a day on goatos-stg. Past that Google bills
// $1/device-hour for a virtual device and $5/device-hour for a physical one. This
// runner REFUSES to submit a matrix that could cross either line, and says exactly
// why and exactly what is left. It never trims a matrix silently to make it fit,
// because a run that quietly dropped half its journeys reports a green that covers
// less than it claims. Deciding to spend money is Ravi's call, not this runner's.
//
// Usage:
//   node tools/dashboard-automation/run-android-journeys.mjs --out <file>
//   node tools/dashboard-automation/run-android-journeys.mjs --self-test
//   node tools/dashboard-automation/run-android-journeys.mjs --only offline-queue-survives-force-stop
//   node tools/dashboard-automation/run-android-journeys.mjs --dry-run      (plan + guard, submit nothing)
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { containsUnredactedSecret, redactText } from "./lib/redact.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
export const catalogPath = path.join(repo, "tools/dashboard-automation/android-journeys.json");

// ---------------------------------------------------------------------------
// The budget. These are Google's free-tier numbers for Firebase Test Lab, not ours.
// ---------------------------------------------------------------------------
export const FREE_TIER = Object.freeze({
  project: "goatos-stg",
  virtualTestsPerDay: 10,
  virtualDeviceMinutesPerDay: 60
  // There is deliberately no physical allowance here. The free tier grants none,
  // and a number sitting in a block labelled "free tier" is one commit away from
  // being read as permission to book a $5/device-hour device.
});

// Listed so nobody has to go and look it up. NOT an allowance: nothing in this
// runner may spend any of it. See `freeTierVerdict`, which refuses physical outright.
export const PAID_RATES = Object.freeze({
  virtualPerDeviceHourUsd: 1,
  physicalPerDeviceHourUsd: 5,
  whoDecidesToSpend: "Ravi. Not this runner, and not an agent."
});

// Which backend an APK talks to is decided by its flavour, and these are declared up
// here, above the --self-test entry point, so the self-test can reach them.
export const DEV_APPLICATION_ID = "sg.mesha.goatos.dev";
export const FORBIDDEN_BACKENDS = Object.freeze([
  { host: "api.goatos.mesha.sg", what: "production (real farm data)" },
  { host: "stg-api.dashboard.mesha.sg", what: "the deployed stg API" }
]);

// Worst case per shard, which is what the budget must be planned against: Test Lab
// bills the device for as long as it is held, so the timeout is the spend.
export const DEFAULT_TIMEOUT_MINUTES = 12;

// One virtual model, one API level. Every extra model or version multiplies the
// matrix, and a 2x2 matrix is four tests out of ten for the day.
export const DEFAULT_MATRIX = Object.freeze({
  model: "MediumPhone.arm",
  version: "33",
  locale: "en",
  orientation: "portrait"
});

const args = parseArgs(process.argv.slice(2));
if (args.selfTest) {
  selfTest();
  process.exit(0);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  // A refusal is a normal outcome of this runner, not a crash. It must read as one
  // sentence, not as a stack trace: the person reading it is being told why a run did
  // not happen, and exit 2 is how the layer above knows it was refused, not broken.
  try {
    await main();
  } catch (error) {
    console.error(`\nREFUSED: ${redactText(error?.message ?? String(error))}`);
    process.exit(2);
  }
}

async function main() {
  const catalog = readCatalog();
  const plan = planMatrix(catalog, args);
  const spent = readSpend(spendPath());
  const verdict = freeTierVerdict(plan, spent);

  console.log(describePlan(plan, spent, verdict));

  if (!verdict.allowed) {
    console.error(`\nREFUSED: ${verdict.reason}`);
    console.error(verdict.whatToDo);
    if (args.out) writeOut(args.out, { status: "refused", plan: redactPlan(plan), spent, verdict, journeys: [] });
    process.exit(2);
  }

  if (args.dryRun) {
    console.log("\n--dry-run: the matrix is inside the free tier and was NOT submitted.");
    if (args.out) writeOut(args.out, { status: "dry-run", plan: redactPlan(plan), spent, verdict, journeys: [] });
    return;
  }

  const apks = resolveApks(args);
  const outDir = path.resolve(args.outDir ?? path.join(repo, ".codex-goatos-render/android-journeys", stamp()));
  mkdirSync(outDir, { recursive: true });

  const submitted = submitToTestLab({ plan, apks, outDir, catalog });
  const outcomesForSpend = parseTestLabOutcome(submitted.stdout || submitted.stderr);
  const booked = reachedADevice(submitted.exitCode, outcomesForSpend);
  // Only device time that was actually booked comes out of the day's allowance. A
  // rejected submission costs nothing, and charging the ledger for it would park a
  // legitimate run later for a device that never existed.
  if (booked) recordSpend(spendPath(), plan);
  else console.log("  free tier       nothing spent — the submission never booked a device");

  const journeys = collectResults({ catalog, plan, outDir, submitted });
  const receipt = {
    status: !booked ? "not-run" : journeys.some((j) => j.outcome === "fail") ? "fail" : "pass",
    blockedReason: booked ? null : blockedReason(submitted),
    lane: "lane5-android",
    ranAt: new Date().toISOString(),
    device: `Firebase Test Lab virtual ${plan.matrix.model} / Android ${plan.matrix.version}`,
    plan: redactPlan(plan),
    spent: readSpend(spendPath()),
    resultsDir: outDir,
    journeys,
    // The promise this lane makes: a virtual run never claims a physical check.
    physicalOnlyNotCovered: catalog.journeys
      .filter((j) => j.device === "physical")
      .map((j) => ({ name: j.name, reason: j.physicalReason, humanFailure: j.humanFailure }))
  };
  if (args.out) writeOut(args.out, receipt);
  if (!booked) {
    console.log(`android journeys not-run; nothing about the app was checked. ${receipt.blockedReason}`);
    process.exit(3);
  }
  console.log(`android journeys ${receipt.status}; ${journeys.filter((j) => j.outcome === "pass").length} passed, ` +
    `${journeys.filter((j) => j.outcome === "fail").length} failed, ` +
    `${journeys.filter((j) => j.outcome === "skipped" || j.outcome === "not-attempted").length} not attempted`);
  if (receipt.status !== "pass") process.exit(1);
}

// ---------------------------------------------------------------------------
// The catalogue
// ---------------------------------------------------------------------------
export function readCatalog(file = catalogPath) {
  return JSON.parse(readFileSync(file, "utf8"));
}

/**
 * Only journeys with a test behind them are ever submitted. Physical-only journeys
 * are excluded here and reported separately, so no path through this runner can mark
 * one of them covered by a virtual device.
 */
export function runnableJourneys(catalog, only = null) {
  const runnable = catalog.journeys.filter((j) =>
    j.device === "virtual" && ["runs-on-virtual", "needs-seeded-session"].includes(j.automation?.tier));
  if (!only) return runnable;
  const picked = runnable.filter((j) => j.name === only);
  if (!picked.length) {
    const physical = catalog.journeys.find((j) => j.name === only && j.device === "physical");
    if (physical) {
      throw new Error(`"${only}" is a physical-device journey and cannot be run on a virtual device: ${physical.physicalReason}`);
    }
    throw new Error(`no runnable journey named "${only}"`);
  }
  return picked;
}

// ---------------------------------------------------------------------------
// The plan and the guard
// ---------------------------------------------------------------------------
export function planMatrix(catalog, options = {}) {
  const only = options.only ?? null;
  const journeys = runnableJourneys(catalog, only);
  const matrix = {
    model: options.model ?? DEFAULT_MATRIX.model,
    version: options.version ?? DEFAULT_MATRIX.version,
    locale: options.locale ?? DEFAULT_MATRIX.locale,
    orientation: options.orientation ?? DEFAULT_MATRIX.orientation
  };
  // Test Lab counts one "test" per device in the matrix, not per test method: the
  // whole suite runs on one device unless --num-uniform-shards splits it.
  const shards = Math.max(1, Number(options.shards ?? 1));
  const models = String(matrix.model).split(",").filter(Boolean);
  const versions = String(matrix.version).split(",").filter(Boolean);
  const locales = String(matrix.locale).split(",").filter(Boolean);
  const orientations = String(matrix.orientation).split(",").filter(Boolean);
  const devices = models.length * versions.length * locales.length * orientations.length;
  const tests = devices * shards;
  const timeoutMinutes = Number(options.timeoutMinutes ?? DEFAULT_TIMEOUT_MINUTES);
  return {
    matrix,
    shards,
    devices,
    tests,
    timeoutMinutes,
    deviceMinutes: tests * timeoutMinutes,
    journeys: journeys.map((j) => j.name),
    journeyCount: journeys.length,
    physical: Boolean(options.physical)
  };
}

export function readSpend(file, today = todayKey()) {
  if (!file || !existsSync(file)) return { day: today, tests: 0, deviceMinutes: 0 };
  let parsed;
  try {
    parsed = JSON.parse(readFileSync(file, "utf8"));
  } catch {
    return { day: today, tests: 0, deviceMinutes: 0 };
  }
  // A new day resets the free tier, so yesterday's ledger is not today's spend.
  if (parsed.day !== today) return { day: today, tests: 0, deviceMinutes: 0 };
  return { day: today, tests: Number(parsed.tests ?? 0), deviceMinutes: Number(parsed.deviceMinutes ?? 0) };
}

export function recordSpend(file, plan, today = todayKey()) {
  const spent = readSpend(file, today);
  const next = {
    day: today,
    tests: spent.tests + plan.tests,
    deviceMinutes: spent.deviceMinutes + plan.deviceMinutes
  };
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, `${JSON.stringify(next, null, 2)}\n`);
  return next;
}

/**
 * The one function that decides whether a run may happen. It is deliberately dull:
 * it never trims, it never rounds down, and it never has an override flag that an
 * agent could set. Crossing the line is a human decision.
 */
export function freeTierVerdict(plan, spent, tier = FREE_TIER) {
  const testsLeft = tier.virtualTestsPerDay - spent.tests;
  const minutesLeft = tier.virtualDeviceMinutesPerDay - spent.deviceMinutes;
  const room = { testsLeft, minutesLeft };

  if (plan.physical) {
    return {
      allowed: false,
      ...room,
      reason: `a physical device costs $${PAID_RATES.physicalPerDeviceHourUsd}/device-hour, the free tier grants no physical device time at all, and this runner is free-tier only`,
      whatToDo: "The physical-device set is a pre-release check run by hand on a real farm phone. " +
        "It is listed in docs/engineering/lane5-android.md. Nothing here may book a physical device."
    };
  }
  if (plan.journeyCount === 0) {
    return { allowed: false, ...room, reason: "the matrix would run no journeys at all", whatToDo: "Nothing to submit; no device time would be spent, but a run that proves nothing is not worth a test out of the ten." };
  }
  if (plan.tests > tier.virtualTestsPerDay) {
    return {
      allowed: false,
      ...room,
      reason: `the matrix is ${plan.tests} tests and the free tier is ${tier.virtualTestsPerDay} virtual tests a day`,
      whatToDo: `Shrink the matrix to ${tier.virtualTestsPerDay} tests or fewer (fewer models, versions, locales, orientations or shards). ` +
        "Do not raise the quota and do not enable billing — that is Ravi's call."
    };
  }
  if (plan.deviceMinutes > tier.virtualDeviceMinutesPerDay) {
    return {
      allowed: false,
      ...room,
      reason: `the matrix could hold devices for ${plan.deviceMinutes} device-minutes and the free tier is ${tier.virtualDeviceMinutesPerDay} a day`,
      whatToDo: `Lower --timeout-minutes (now ${plan.timeoutMinutes}) or the number of tests (now ${plan.tests}) until ` +
        `tests × timeout is ${tier.virtualDeviceMinutesPerDay} or less. Do not enable billing.`
    };
  }
  if (plan.tests > testsLeft) {
    return {
      allowed: false,
      ...room,
      reason: `${spent.tests} of the day's ${tier.virtualTestsPerDay} free tests are already spent and this matrix needs ${plan.tests}`,
      whatToDo: "Park the run until tomorrow. The free tier resets daily; nothing here may spend money to finish sooner."
    };
  }
  if (plan.deviceMinutes > minutesLeft) {
    return {
      allowed: false,
      ...room,
      reason: `${spent.deviceMinutes} of the day's ${tier.virtualDeviceMinutesPerDay} free device-minutes are already spent and this matrix could need ${plan.deviceMinutes}`,
      whatToDo: "Park the run until tomorrow, or lower --timeout-minutes so the worst case fits in what is left."
    };
  }
  return {
    allowed: true,
    ...room,
    reason: `${plan.tests} test(s) × up to ${plan.timeoutMinutes} min = ${plan.deviceMinutes} device-minutes, inside the ${testsLeft} tests and ${minutesLeft} device-minutes left today`,
    whatToDo: ""
  };
}

export function describePlan(plan, spent, verdict) {
  return [
    `Firebase Test Lab — ${FREE_TIER.project}, VIRTUAL devices only`,
    `  matrix          ${plan.matrix.model} / Android ${plan.matrix.version} / ${plan.matrix.locale} / ${plan.matrix.orientation}`,
    `  journeys        ${plan.journeyCount}`,
    `  tests           ${plan.tests} (${plan.devices} device(s) × ${plan.shards} shard(s))`,
    `  worst-case time ${plan.deviceMinutes} device-minutes (${plan.timeoutMinutes} min timeout each)`,
    `  spent today     ${spent.tests}/${FREE_TIER.virtualTestsPerDay} tests, ${spent.deviceMinutes}/${FREE_TIER.virtualDeviceMinutesPerDay} device-minutes`,
    `  free tier       ${verdict.allowed ? "OK" : "EXCEEDED"} — ${verdict.reason}`
  ].join("\n");
}

// ---------------------------------------------------------------------------
// Submitting
// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// The data guard. This matters MORE than the money guard.
// ---------------------------------------------------------------------------
// The app is one codebase with three flavours, and the flavour decides which backend
// the APK talks to: dev → a local backend, stg → the deployed stg API, prod →
// api.goatos.mesha.sg, which is STG-BACKED PRODUCTION carrying real farm data.
//
// This lane's highest-value journeys — proof upload, offline queue, retry, conflict,
// resume after force-stop — all WRITE. A stg- or prod-flavoured APK on a Test Lab
// device would put automation writes into real farm data, which breaks the rule the
// whole design rests on: production is read-only and writes go to the OCI clone.
//
// So: only the dev flavour may ever be submitted, and if that cannot be PROVEN the
// run is refused. "Probably dev" is not good enough when the cost of being wrong is
// a write into a real farm's records.
/**
 * Ravi's rule, and it settles the naming trap: the stg API serves the same STG-backed
 * data as production. "stg prod is still prod only" — the difference is a name, not the
 * data behind it. Neither is ever a valid target for this lane.
 *
 * THIS NEVER OPENS A SOCKET. It parses the url and compares hostnames as strings. It
 * does not resolve DNS and it does not connect, because the whole point is to refuse
 * BEFORE anything talks to a host holding real farm data. `forbiddenList` is injectable
 * so the tests can prove the mechanism against invented hostnames instead of naming
 * real ones.
 */
export function backendUrlVerdict(url, forbiddenList = FORBIDDEN_BACKENDS) {
  const text = String(url ?? "").trim();
  if (!text) return { allowed: true, reason: "no backend url was named, so the one compiled into the APK stands" };
  let host;
  try {
    host = new URL(text).hostname;
  } catch {
    return { allowed: false, reason: `"${text}" is not a url this runner can check, so which backend it points at cannot be proven` };
  }
  const forbidden = forbiddenList.find((entry) => host === entry.host || host.endsWith(`.${entry.host}`));
  if (forbidden) {
    return { allowed: false, reason: `${host} is ${forbidden.what}. These journeys write, and stg serves the same data as production — the difference is a name, not the data. It may never be a target.` };
  }
  return { allowed: true, reason: `${host} is not a production-backed host` };
}

export function applicationIdVerdict(applicationId) {
  const id = String(applicationId ?? "").trim();
  if (!id) {
    return { allowed: false, reason: "the APK's application id could not be read, so which backend it talks to cannot be proven" };
  }
  if (id === DEV_APPLICATION_ID) return { allowed: true, reason: `${id} — the dev flavour, pointed at a local backend` };
  if (id === "sg.mesha.goatos") {
    return { allowed: false, reason: `${id} is the PRODUCTION flavour, which talks to real farm data. These journeys write; they may never run against it.` };
  }
  if (id === "sg.mesha.goatos.stg") {
    return { allowed: false, reason: `${id} is the stg flavour, which talks to the deployed stg API. These journeys write; they may never run against it.` };
  }
  return { allowed: false, reason: `${id} is not the dev flavour, so which backend it talks to cannot be proven` };
}

/** Reads the application id out of a built APK. Refuses rather than guesses. */
export function readApplicationId(apkPath, aapt2 = findAapt2()) {
  if (!aapt2) return null;
  const result = spawnSync(aapt2, ["dump", "badging", apkPath], { encoding: "utf8" });
  if (result.status !== 0) return null;
  return result.stdout.match(/package:\s*name='([^']+)'/)?.[1] ?? null;
}

function findAapt2() {
  const sdk = process.env.ANDROID_HOME || process.env.ANDROID_SDK_ROOT ||
    path.join(process.env.HOME ?? "", "Library/Android/sdk");
  const buildTools = path.join(sdk, "build-tools");
  if (!existsSync(buildTools)) return null;
  const versions = readdirSync(buildTools).sort().reverse();
  for (const version of versions) {
    const candidate = path.join(buildTools, version, "aapt2");
    if (existsSync(candidate)) return candidate;
  }
  return null;
}

/** A second, independent read: does the binary itself carry a non-dev backend host? */
export function apkMentionsForbiddenBackend(apkPath) {
  if (!existsSync(apkPath)) return [];
  const bytes = readFileSync(apkPath);
  const text = bytes.toString("latin1");
  return FORBIDDEN_BACKENDS.filter(({ host }) => text.includes(host));
}

function resolveApks(options) {
  const app = options.appApk ?? process.env.GOATOS_ANDROID_APP_APK;
  const test = options.testApk ?? process.env.GOATOS_ANDROID_TEST_APK;
  if (!app || !test) {
    throw new Error("both --app-apk and --test-apk are required (or GOATOS_ANDROID_APP_APK / GOATOS_ANDROID_TEST_APK). " +
      "Build them with :app:assembleDevDebug and :journeys:assembleDevDebug.");
  }
  for (const [label, file] of [["app", app], ["test", test]]) {
    if (!existsSync(file)) throw new Error(`${label} APK not found: ${file}`);
  }
  // Three independent reads, all unconditional. Any one of them refusing stops the run.
  const urlVerdict = backendUrlVerdict(options.apiBaseUrl ?? process.env.GOATOS_ANDROID_API_BASE_URL);
  if (!urlVerdict.allowed) {
    throw new Error(`this run may not be submitted: ${urlVerdict.reason}`);
  }
  const mentioned = apkMentionsForbiddenBackend(app);
  if (mentioned.length) {
    throw new Error(`this APK may not be submitted: it carries ${mentioned.map((m) => `${m.host} (${m.what})`).join(" and ")}. ` +
      "These journeys write, and stg serves the same data as production — the difference is a name, not the data.");
  }
  const applicationId = readApplicationId(app);
  const verdict = applicationIdVerdict(applicationId);
  if (!verdict.allowed) {
    throw new Error(`this APK may not be submitted: ${verdict.reason}. ` +
      "Build the dev flavour: ./gradlew :app:assembleDevDebug :journeys:assembleDevDebug. " +
      "Never a stg or prod flavour, and never the signed release artifact Cloud Build ships to App Distribution.");
  }
  console.log(`  APK under test   ${verdict.reason}`);
  // The test APK's own id decides where its evidence lands under scoped storage.
  const testApplicationId = readApplicationId(test);
  if (!testApplicationId) {
    throw new Error("the test APK's application id could not be read, so the runner cannot know where to collect the screenshots from");
  }
  return { app, test, applicationId, testApplicationId };
}

function submitToTestLab({ plan, apks, outDir, catalog }) {
  const resultsBucketDir = `lane5-${stamp()}`;
  const gcloudArgs = [
    "firebase", "test", "android", "run",
    "--type", "instrumentation",
    "--project", FREE_TIER.project,
    "--app", apks.app,
    "--test", apks.test,
    "--device", `model=${plan.matrix.model},version=${plan.matrix.version},locale=${plan.matrix.locale},orientation=${plan.matrix.orientation}`,
    "--timeout", `${plan.timeoutMinutes}m`,
    // Where the tests write their evidence. NOT a bare /sdcard path: under scoped
    // storage the test app may only write inside its own external files directory,
    // so that is where the screenshots are and that is what gets pulled.
    "--directories-to-pull", `/sdcard/Android/data/${apks.testApplicationId ?? "sg.mesha.goatos.journeys"}/files`,
    "--results-dir", resultsBucketDir,
    "--format", "json"
  ];
  // Only the journeys that were planned, so the receipt and the matrix can never disagree.
  const classes = [...new Set(plan.journeys.map((name) => testClassFor(name, catalog)).filter(Boolean))];
  if (classes.length) gcloudArgs.push("--test-targets", classes.map((c) => `class ${c}`).join(","));

  console.log(`\nsubmitting: gcloud ${gcloudArgs.filter((a) => !a.endsWith(".apk")).join(" ")}`);
  const result = spawnSync("gcloud", gcloudArgs, { cwd: repo, encoding: "utf8", env: process.env });
  const stdout = redactText(result.stdout ?? "");
  const stderr = redactText(result.stderr ?? "");
  writeFileSync(path.join(outDir, "testlab-stdout.json"), stdout);
  writeFileSync(path.join(outDir, "testlab-stderr.txt"), stderr);
  if (stderr.trim()) console.error(stderr.trim());
  return { exitCode: result.status, stdout, stderr, resultsBucketDir };
}

// No module-level mutable state here on purpose. `main()` runs at import time, which
// is ABOVE every `let`/`const` further down this file: function declarations hoist,
// bindings do not, so a memo variable declared below would be in its temporal dead
// zone by the time the runner reached it. The catalogue is passed in instead.
export function testClassFor(journeyName, catalog) {
  const journey = catalog.journeys.find((j) => j.name === journeyName);
  const cls = journey?.automation?.testClass;
  return cls ? `sg.mesha.goatos.journeys.${cls}` : null;
}

// ---------------------------------------------------------------------------
// Reading the results
// ---------------------------------------------------------------------------
/**
 * Turns a Test Lab run into one row per journey. A device that crashed or timed out
 * is NOT a pass and NOT a silent gap: it is reported as a failure of every journey
 * that was on it, because from the farm's side "the app died" is the finding.
 */
export function parseTestLabOutcome(raw) {
  const text = String(raw ?? "");
  let parsed = null;
  try {
    parsed = JSON.parse(text);
  } catch { /* gcloud prints human text on some failures; fall through to the regexes */ }
  const rows = Array.isArray(parsed) ? parsed : parsed ? [parsed] : [];
  if (rows.length) {
    return rows.map((row) => ({
      outcome: normaliseOutcome(row.outcome ?? row.status),
      detail: String(row.errorDetails ?? row.details ?? "").trim(),
      axis: String(row.axisValue ?? row.axis_value ?? "").trim()
    }));
  }
  const found = [...text.matchAll(/^\s*(\S+)\s+(Passed|Failed|Inconclusive|Skipped|Crashed|Timed out|Infrastructure failure)\b(.*)$/gim)]
    .map((m) => ({ axis: m[1], outcome: normaliseOutcome(m[2]), detail: m[3].trim() }));
  return found;
}

export function normaliseOutcome(value) {
  const text = String(value ?? "").trim().toLowerCase();
  if (text === "passed" || text === "pass" || text === "success") return "pass";
  if (text === "skipped" || text === "skip") return "skipped";
  // A crashed, timed-out or infrastructure-failed device proved nothing, and
  // pretending otherwise is the exact failure mode this lane exists to prevent.
  if (["failed", "fail", "failure", "crashed", "timed out", "timedout", "inconclusive", "infrastructure failure", "error"].includes(text)) {
    return "fail";
  }
  return "fail";
}

/**
 * Did the submission ever reach a device at all?
 *
 * Learned the hard way on the first live attempt: the matrix was rejected before any
 * device was booked, and the runner reported two journeys as FAILED — which would have
 * put "People are signed out at random" into Slack when the truth was that nothing ran.
 * That is the mirror image of a false green and just as dishonest: it blames the app
 * for an infrastructure problem. A run that never started is NOT ATTEMPTED.
 */
export function reachedADevice(exitCode, outcomes) {
  if (outcomes.length > 0) return true;
  return exitCode === 0;
}

/** Did the whole device die, rather than a journey failing? The farm reads these differently. */
export function deviceDied(outcomes) {
  return outcomes.length > 0 && outcomes.every((o) => o.outcome === "fail") &&
    outcomes.some((o) => /crash|timed out|timeout|infrastructure/i.test(o.detail ?? ""));
}

function collectResults({ catalog, plan, outDir, submitted }) {
  const outcomes = parseTestLabOutcome(submitted.stdout || submitted.stderr);
  if (!reachedADevice(submitted.exitCode, outcomes)) {
    // Nothing ran, so nothing is known about the app. Say exactly that.
    return plan.journeys.map((name) => ({
      name,
      screen: catalog.journeys.find((j) => j.name === name)?.screen ?? null,
      outcome: "not-attempted",
      covered: false,
      humanFailure: null,
      note: "not attempted — the run was rejected before any phone was started, so nothing about the app was checked"
    }));
  }
  const died = deviceDied(outcomes);
  const devicePassed = outcomes.length > 0 && outcomes.every((o) => o.outcome === "pass");
  const evidence = readEvidence(outDir);

  return plan.journeys.map((name) => {
    const journey = catalog.journeys.find((j) => j.name === name);
    const seeded = process.env.GOATOS_ANDROID_SESSION_SEEDED === "1";
    // A journey that needs a session and had none was NOT ATTEMPTED. It is never a pass.
    if (journey.automation.tier === "needs-seeded-session" && !seeded) {
      return {
        name,
        screen: journey.screen,
        outcome: "skipped",
        covered: false,
        humanFailure: null,
        note: "not attempted — no signed-in session was built into this APK"
      };
    }
    const outcome = died ? "fail" : devicePassed ? "pass" : "fail";
    return {
      name,
      screen: journey.screen,
      outcome,
      covered: outcome === "pass",
      humanFailure: outcome === "fail"
        ? (died ? "The app stopped working on the phone part-way through, so nothing on it could be checked." : journey.humanFailure)
        : null,
      screenshot: evidence[name] ?? null,
      seen: evidence[`${name}.seen`] ?? null
    };
  });
}

/** Why the submission never booked a device, in words the operator can act on. */
export function blockedReason(submitted) {
  const text = `${submitted.stdout ?? ""}\n${submitted.stderr ?? ""}`;
  const service = text.match(/API \[([\w.]+)\] not enabled|serviceTitle: ([^\n]+)/);
  if (/SERVICE_DISABLED|not enabled on project|has not been used in project/i.test(text)) {
    const name = (service?.[1] || service?.[2] || "a required Google Cloud API").trim();
    return `${name} is not enabled on the project, so Test Lab refused the run before starting a phone. Enabling it is a project-configuration change and is Ravi's to make.`;
  }
  if (/PERMISSION_DENIED|does not have permission/i.test(text)) {
    return "the account running this does not have permission to start a Test Lab run on the project.";
  }
  if (/quota|exceeded/i.test(text)) {
    return "Test Lab refused the run on quota. Nothing was spent; it runs again tomorrow.";
  }
  return "Test Lab refused the run before starting a phone; the reason is in testlab-stderr.txt beside this receipt.";
}

function readEvidence(outDir) {
  const found = {};
  const walk = (dir) => {
    if (!existsSync(dir)) return;
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.name.endsWith(".png")) found[entry.name.replace(/\.png$/, "")] = full;
      else if (entry.name.endsWith(".seen.txt")) found[`${entry.name.replace(/\.seen\.txt$/, "")}.seen`] = readFileSync(full, "utf8").trim();
    }
  };
  walk(outDir);
  return found;
}

// ---------------------------------------------------------------------------
// Plumbing
// ---------------------------------------------------------------------------
function spendPath() {
  return process.env.GOATOS_ANDROID_TESTLAB_SPEND_FILE ||
    path.join(repo, ".codex-goatos-render/android-journeys/free-tier-spend.json");
}

function todayKey(now = new Date()) {
  return now.toISOString().slice(0, 10);
}

function stamp() {
  return new Date().toISOString().replaceAll(/[:.]/g, "-");
}

function redactPlan(plan) {
  return JSON.parse(redactText(JSON.stringify(plan)));
}

function writeOut(file, value) {
  const text = `${JSON.stringify(value, null, 2)}\n`;
  if (containsUnredactedSecret(text)) throw new Error("refusing to write a receipt that appears to contain an unredacted secret");
  mkdirSync(path.dirname(path.resolve(file)), { recursive: true });
  writeFileSync(path.resolve(file), text);
}

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--dry-run") parsed.dryRun = true;
    else if (arg === "--out") parsed.out = raw[++i];
    else if (arg === "--out-dir") parsed.outDir = raw[++i];
    else if (arg === "--only") parsed.only = raw[++i];
    else if (arg === "--model") parsed.model = raw[++i];
    else if (arg === "--version") parsed.version = raw[++i];
    else if (arg === "--locale") parsed.locale = raw[++i];
    else if (arg === "--orientation") parsed.orientation = raw[++i];
    else if (arg === "--shards") parsed.shards = raw[++i];
    else if (arg === "--timeout-minutes") parsed.timeoutMinutes = raw[++i];
    else if (arg === "--app-apk") parsed.appApk = raw[++i];
    else if (arg === "--test-apk") parsed.testApk = raw[++i];
    else if (arg === "--physical") parsed.physical = true;
    else if (arg === "--api-base-url") parsed.apiBaseUrl = raw[++i];
    else throw new Error(`unknown argument: ${arg}`);
  }
  return parsed;
}

function selfTest() {
  const assert = (condition, message) => { if (!condition) throw new Error(`self-test: ${message}`); };
  const catalog = readCatalog();

  assert(catalog.journeys.length === 47, `catalogue must carry all 47 lane 5 checks, found ${catalog.journeys.length}`);
  assert(catalog.journeys.filter((j) => j.device === "physical").length === 5,
    "the five physical-device checks must stay marked physical");
  for (const journey of catalog.journeys) {
    assert(journey.humanFailure && journey.humanFailure.length > 10, `${journey.name}: no humanFailure`);
    assert(journey.story && journey.story.length > 20, `${journey.name}: no story`);
    assert(Array.isArray(journey.sourceCommits), `${journey.name}: no sourceCommits`);
    if (journey.device === "physical") {
      assert(journey.physicalReason, `${journey.name}: a physical journey must say why a virtual device would be a false green`);
    }
  }

  // The guard exists and bites.
  const fits = planMatrix(catalog, { timeoutMinutes: 10 });
  assert(freeTierVerdict(fits, { tests: 0, deviceMinutes: 0 }).allowed, "a single-device 10-minute run must be allowed");
  const tooMany = planMatrix(catalog, { model: "MediumPhone.arm,SmallPhone.arm,Pixel2.arm", version: "30,31,32,33", timeoutMinutes: 10 });
  assert(!freeTierVerdict(tooMany, { tests: 0, deviceMinutes: 0 }).allowed, "a 12-test matrix must be refused");
  const tooLong = planMatrix(catalog, { timeoutMinutes: 90 });
  assert(!freeTierVerdict(tooLong, { tests: 0, deviceMinutes: 0 }).allowed, "a 90-minute timeout must be refused");
  assert(!freeTierVerdict(fits, { tests: 10, deviceMinutes: 0 }).allowed, "a spent daily test budget must be refused");
  assert(!freeTierVerdict(planMatrix(catalog, { physical: true }), { tests: 0, deviceMinutes: 0 }).allowed,
    "a physical device must always be refused");

  // The data guard matters more than the money guard: a stg- or prod-flavoured APK
  // would put automation WRITES into real farm data.
  assert(applicationIdVerdict("sg.mesha.goatos.dev").allowed, "the dev flavour must be allowed");
  assert(!applicationIdVerdict("sg.mesha.goatos").allowed, "the production flavour must be refused");
  assert(!applicationIdVerdict("sg.mesha.goatos.stg").allowed, "the stg flavour must be refused");
  assert(!applicationIdVerdict("").allowed, "an APK whose flavour cannot be read must be refused, not assumed safe");
  assert(!applicationIdVerdict(null).allowed, "an unreadable application id must be refused");
  assert(/real farm data/i.test(applicationIdVerdict("sg.mesha.goatos").reason),
    "the production refusal must say plainly that real farm data is at stake");
  // Proved against INVENTED hostnames. The guard must never be exercised by naming a
  // host that holds real farm data, even in a string.
  const pretend = [{ host: "prod.example.invalid", what: "production (real farm data)" }];
  assert(!backendUrlVerdict("https://prod.example.invalid/", pretend).allowed, "a forbidden host must be refused");
  assert(!backendUrlVerdict("https://sub.prod.example.invalid/x", pretend).allowed, "a subdomain of a forbidden host must be refused");
  assert(backendUrlVerdict("http://localhost:8080/", pretend).allowed, "a local backend must be allowed");
  assert(backendUrlVerdict("", pretend).allowed, "naming no url must leave the APK's own backend to the flavour guard");
  assert(!backendUrlVerdict("not a url", pretend).allowed, "an unparseable url must be refused, not assumed safe");
  assert(FORBIDDEN_BACKENDS.length === 2, "both production-backed hosts must stay on the forbidden list");
  // The guard must be incapable of reaching anything. No sockets, ever.
  {
    const source = readFileSync(fileURLToPath(import.meta.url), "utf8");
    // Assembled, not written out, so this check does not trip over its own list.
    const networking = ["net", "dns", "http", "https", "tls"].map((mod) => `node:${mod}`).concat([`${"fetch"}(`]);
    const used = networking.filter((capability) => source.includes(capability));
    if (used.length) throw new Error(`self-test: this runner must open no socket, but it uses ${used.join(", ")}`);
  }

  // A virtual run can never claim a physical check.
  const runnable = runnableJourneys(catalog);
  assert(runnable.every((j) => j.device === "virtual"), "a physical journey must never be submitted to a virtual device");
  let refusedPhysical = false;
  try { runnableJourneys(catalog, "proof-capture"); } catch { refusedPhysical = true; }
  assert(refusedPhysical, "--only on a physical journey must be refused");
  // …and the refusal must read as a sentence, not as a crash.
  const refusal = spawnSync(process.execPath, [fileURLToPath(import.meta.url), "--only", "proof-capture", "--dry-run"], { encoding: "utf8", env: process.env });
  assert(refusal.status === 2, `a refused --only must exit 2, got ${refusal.status}`);
  assert(/REFUSED/.test(refusal.stderr) && !/at Object\.|node:internal/.test(refusal.stderr),
    `a refusal must be one sentence, not a stack trace: ${refusal.stderr.slice(0, 200)}`);

  // A dead device is a failure, never a pass and never a silent gap.
  assert(normaliseOutcome("Passed") === "pass", "Passed must read as a pass");
  assert(normaliseOutcome("Crashed") === "fail", "a crashed device must read as a failure");
  assert(normaliseOutcome("Timed out") === "fail", "a timed-out device must read as a failure");
  assert(normaliseOutcome("Inconclusive") === "fail", "an inconclusive device must read as a failure");
  assert(normaliseOutcome("something new google invented") === "fail", "an unknown outcome must read as a failure, not a pass");

  // Exercise the real pre-submit path. A binding used by main() but declared below
  // main()'s call site is in its temporal dead zone and throws only at submit time —
  // which is exactly when it costs a run to find out.
  assert(testClassFor("login-and-session", catalog) === "sg.mesha.goatos.journeys.GoatOsColdBootJourneys",
    "the runner must resolve a journey to its test class");
  assert(testClassFor("proof-capture", catalog) === null,
    "a physical journey must resolve to no test class");
  {
    const planned = planMatrix(catalog, {});
    const classes = [...new Set(planned.journeys.map((name) => testClassFor(name, catalog)).filter(Boolean))];
    assert(classes.length > 0, "a planned run must resolve to at least one test class");
    assert(classes.every((c) => c.startsWith("sg.mesha.goatos.journeys.")),
      "every resolved test class must be one of this lane's own");
  }

  assert(readFileSync(fileURLToPath(import.meta.url), "utf8").includes("freeTierVerdict"),
    "the free-tier guard must stay in this runner");
  // A physical allowance must never appear inside anything labelled "free tier":
  // the free tier grants none, and a number there reads as permission to spend.
  assert(!Object.keys(FREE_TIER).some((key) => /physical/i.test(key)),
    "FREE_TIER must carry no physical allowance");
  assert(JSON.stringify(catalog.budget.freeTier).includes("none"),
    "the catalogue's free-tier block must say plainly that it grants no physical device time");

  // The catalogue must not claim more than it proves.
  const provenToday = catalog.journeys.filter((j) => j.automation?.tier === "runs-on-virtual");
  for (const journey of provenToday) {
    assert(journey.automation.coversPartially,
      `${journey.name}: a journey claimed as covered must say which half of its check it actually proves`);
  }
  assert(catalog.coverageToday.journeysAVirtualRunCanProveToday === provenToday.length,
    "the catalogue's headline coverage number must match the rows");

  console.log("android journeys runner: self-test passed");
}
