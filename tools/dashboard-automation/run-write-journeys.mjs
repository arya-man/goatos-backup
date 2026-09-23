#!/usr/bin/env node
// Lane 4 runner: the write-path journeys production cannot test.
//
// Per journey, in this order, every time:
//   guard the target database -> snapshot exactly the tables the journey declares ->
//   drive the screen -> assert what is on screen -> assert the row the write created ->
//   check nothing undeclared was written -> restore -> PROVE the restore -> record all of it.
//
// On any failure it still restores and still proves the restore. A restore that cannot be proved
// is a hard blocker and is reported louder than the journey failure that preceded it, because a
// clone left with test data in it makes every later check meaningless.
//
// Usage:
//   node tools/dashboard-automation/run-write-journeys.mjs --self-test
//   node tools/dashboard-automation/run-write-journeys.mjs --stamp-clone
//   GOATOS_WRITE_JOURNEY_DATABASE_URL=... node tools/dashboard-automation/run-write-journeys.mjs \
//     --out <file> [--only <name>] [--no-ui]
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { containsUnredactedSecret, redactText } from "./lib/redact.mjs";
import writeJourneysKind from "./lib/finding-kinds/write-journeys.mjs";
import {
  assertDeclaredTables,
  assertWriteTarget,
  classifyWriteTarget,
  createTableSnapshotEngine,
  describeTarget,
  psqlRunner
} from "./lib/table-snapshot.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const cataloguePath = path.join(repo, "tools/dashboard-automation/write-journeys.json");

export function loadCatalogue(file = cataloguePath) {
  return JSON.parse(readFileSync(file, "utf8"));
}

/** Screen names a person would use, derived from the route the journey runs on. */
export function pageNameFor(journey) {
  return String(journey.route ?? "")
    .replaceAll("-", " ")
    .replace(/\b\w/g, (c) => c.toUpperCase())
    .trim() || "This screen";
}

/** One run token, so every row a journey creates can be found and nothing else can match it. */
export function makeToken(now = Date.now(), random = Math.random) {
  return `A${String(now).slice(-8)}${String(random()).slice(2, 6)}`;
}

export function fillTokens(value, token) {
  const numeric = `${100 + (Number(String(token).replace(/\D/g, "").slice(-4)) % 800)}.${String(token).slice(-2).replace(/\D/g, "0")}`;
  return JSON.parse(
    JSON.stringify(value)
      .replaceAll("{{token}}", token)
      .replaceAll("{{numericToken}}", numeric)
  );
}

/**
 * The catalogue contract. A journey that does not say what it writes, or whose farm-manager
 * sentence contains engineer-speak, must not run at all.
 */
export function validateCatalogue(catalogue, { assertPlainEnglish } = writeJourneysKind) {
  const problems = [];
  const journeys = catalogue.journeys ?? [];
  if (journeys.length === 0) problems.push("the write-journey catalogue is empty");
  const seen = new Set();
  for (const journey of journeys) {
    const name = journey.name ?? "<unnamed>";
    if (!journey.name) problems.push("a journey has no name");
    if (seen.has(name)) problems.push(`${name} is listed twice`);
    seen.add(name);
    for (const field of ["story", "route", "url", "humanFailure"]) {
      if (!journey[field]) problems.push(`${name} has no ${field}`);
    }
    if (!Array.isArray(journey.steps) || journey.steps.length === 0) problems.push(`${name} has no steps`);
    if (!journey.screenAssertion) problems.push(`${name} has no screenAssertion`);
    if (!journey.dbAssertion?.sql) problems.push(`${name} has no dbAssertion`);
    if (!Array.isArray(journey.sourceCommits) || journey.sourceCommits.length === 0) {
      problems.push(`${name} names no source commit; a check with no shipped feature or real bug behind it does not run`);
    }
    try {
      assertDeclaredTables(journey.writesTables, name);
    } catch (error) {
      problems.push(String(error.message));
    }
    if (journey.generatedTables?.length) {
      try {
        assertDeclaredTables(journey.generatedTables, `${name} generatedTables`, { generated: true });
      } catch (error) {
        problems.push(String(error.message));
      }
    }
    try {
      assertPlainEnglish(journey.humanFailure, `${name} humanFailure`);
    } catch (error) {
      problems.push(String(error.message));
    }
    if (!/[.!]$/.test(String(journey.humanFailure ?? ""))) {
      problems.push(`${name} humanFailure must be a sentence a farm manager reads, ending in a full stop`);
    }
  }
  return problems;
}

/** Everything a journey is allowed to touch: what it writes plus what its own write generates. */
export function declaredTablesFor(journey) {
  return [...new Set([...(journey.writesTables ?? []), ...(journey.generatedTables ?? [])])];
}

/**
 * Runs one journey. Pure of process concerns: `engine`, `driveUi` and `queryRows` are injected so
 * the whole failure/restore contract is unit tested without a database or a browser.
 */
export function runJourney(journey, { engine, driveUi, queryRows, token, screenshotDir }) {
  const filled = fillTokens(journey, token);
  const declared = declaredTablesFor(filled);
  const record = {
    name: filled.name,
    story: filled.story,
    page: pageNameFor(filled),
    url: filled.url,
    status: "pass",
    humanFailure: null,
    failedStep: null,
    screenshot: null,
    writesTables: filled.writesTables,
    generatedTables: filled.generatedTables ?? [],
    sourceCommits: filled.sourceCommits ?? [],
    screen: { status: "not_run", reason: null },
    database: { status: "not_run", reason: null },
    undeclaredTables: [],
    restore: { attempted: false, restored: false, verified: false, strategy: null, tables: [], humanRestore: null }
  };

  let handle = null;
  try {
    handle = engine.snapshot(declared, filled.name);
  } catch (error) {
    record.status = "fail";
    record.screen.status = "not_run";
    record.database.status = "not_run";
    record.humanFailure = "This check could not take a safe copy of the farm's data first, so it did not run at all.";
    record.blocked = redactText(String(error?.message ?? error));
    return record;
  }

  try {
    const ui = driveUi(filled, { screenshotDir, token });
    record.screen = { status: ui.ok ? "pass" : "fail", reason: ui.reason ?? null };
    record.screenshot = ui.screenshot ?? null;
    if (ui.status === "parked" || ui.status === "simulated") {
      record.screen = { status: "parked", reason: ui.reason ?? "the screen was not driven in this run" };
      record.screenshot = null;
    } else if (!ui.ok) {
      record.status = "fail";
      record.failedStep = ui.failedStep ?? null;
      record.humanFailure = filled.humanFailure;
    }

    if (record.screen.status !== "fail") {
      const rows = queryRows(filled.dbAssertion.sql);
      const found = dbAssertionHolds(filled.dbAssertion, rows);
      record.database = { status: found ? "pass" : "fail", reason: found ? null : filled.dbAssertion.description };
      if (!found) {
        record.status = "fail";
        record.humanFailure = filled.humanFailure;
      }
    }

    const undeclared = engine.undeclaredWrites(handle);
    if (undeclared.length) {
      record.undeclaredTables = undeclared;
      record.status = "fail";
      // Deliberately not the journey's own sentence: this is the check misbehaving, not the site.
      record.humanFailure = "This check changed more of the farm's data than it said it would, so its result cannot be trusted.";
    }
  } catch (error) {
    record.status = "fail";
    record.humanFailure = record.humanFailure ?? filled.humanFailure;
    record.error = redactText(String(error?.message ?? error));
  } finally {
    // A failure never skips the restore, and the restore is never believed without the proof.
    record.restore.attempted = true;
    const restored = engine.restore(handle);
    record.restore.restored = restored.restored === true;
    record.restore.strategy = restored.strategy;
    if (restored.error) record.restore.error = redactText(String(restored.error));
    const verified = engine.verifyRestore(handle);
    record.restore.verified = verified.verified === true;
    record.restore.tables = verified.tables;
    if (!record.restore.restored) {
      record.restore.humanRestore = "Nothing should be read from that copy, and no further check should run against it, until someone puts it back.";
    } else if (!record.restore.verified) {
      const missing = verified.mismatches.reduce((sum, row) => sum + Math.max(0, (row.rowCount ?? 0) - (row.actualRowCount ?? 0)), 0);
      record.restore.humanRestore = missing > 0
        ? `${missing} record${missing === 1 ? "" : "s"} did not come back.`
        : "The records came back but they are not the same as they were.";
    }
    if (record.restore.restored && record.restore.verified) engine.cleanup(handle);
  }
  return record;
}

export function dbAssertionHolds(assertion, rows) {
  const list = rows ?? [];
  if (assertion.expect === "zero-count") {
    const value = Number(list[0]?.[0] ?? 0);
    return Number.isFinite(value) && value === 0;
  }
  if (assertion.expect === "zero-rows") return list.length === 0;
  return list.length > 0;
}

export function summarise(journeys) {
  const failed = journeys.filter((j) => j.status !== "pass");
  const restoreProblems = journeys.filter((j) => j.restore.attempted && (!j.restore.restored || !j.restore.verified));
  return {
    total: journeys.length,
    failed: failed.length,
    restoreProblems: restoreProblems.length,
    // A clone left with test data in it is worse than any journey failure.
    status: restoreProblems.length ? "blocked" : failed.length ? "fail" : "pass"
  };
}

// ---------------------------------------------------------------------------------------------
// Process wiring below. Everything above is pure and unit tested.
// ---------------------------------------------------------------------------------------------

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--stamp-clone") parsed.stampClone = true;
    else if (arg === "--no-ui") parsed.noUi = true;
    else if (arg === "--simulate-write") parsed.simulateWrite = true;
    else if (arg === "--out") parsed.out = raw[++i];
    else if (arg === "--only") parsed.only = raw[++i];
    else throw new Error(`unknown argument: ${arg}`);
  }
  return parsed;
}

const UI_DRIVER_SCRIPT = "smoke:write-journey:live";

/**
 * Whether the screen driver is absent. Exported so the guarantee it protects -- a missing driver is
 * a RUNNER blocker, never a journey failure -- stays under test now that the driver itself exists
 * in apps/admin-web, which is what used to make the command-line version of this test meaningful.
 */
export function screenDriverMissing(scripts = adminWebScripts()) {
  return !scripts?.[UI_DRIVER_SCRIPT];
}

function adminWebScripts() {
  try {
    return JSON.parse(readFileSync(path.join(repo, "apps/admin-web/package.json"), "utf8")).scripts ?? {};
  } catch {
    return {};
  }
}

/** Default screen driver: the same live-smoke shape lane 1 uses, one journey per invocation. */
function spawnUiDriver(journey, { screenshotDir, token }) {
  const result = spawnSync("npm", ["--prefix", "apps/admin-web", "run", UI_DRIVER_SCRIPT], {
    cwd: repo,
    encoding: "utf8",
    env: {
      ...process.env,
      GOATOS_WRITE_JOURNEY: JSON.stringify(journey),
      GOATOS_WRITE_JOURNEY_TOKEN: token,
      GOATOS_WRITE_JOURNEY_SHOT_DIR: screenshotDir
    }
  });
  const stdout = redactText(result.stdout ?? "");
  const screenshot = [...stdout.matchAll(/^screenshot_path=(.+)$/gm)].map((m) => path.resolve(repo, m[1].trim())).pop() ?? null;
  const failedStep = stdout.match(/^write_journey_failed_step=(.+)$/m)?.[1]?.trim() ?? null;
  const reason = stdout.match(/^write_journey_reason=(.+)$/m)?.[1]?.trim() ?? null;
  return { ok: result.status === 0, status: "ran", reason, failedStep, screenshot };
}

/**
 * Applies the write the journey's screen would make, directly, so the snapshot -> assert ->
 * restore -> prove machinery can be exercised against a real clone with no browser stack up.
 * The receipt records screenDriven: false: this NEVER claims the screen was proved.
 */
function simulatedWriteDriver(queryRows) {
  return (journey) => {
    if (!journey.simulatedWrite?.sql) {
      return { ok: true, status: "parked", reason: "this journey has no stand-in for its screen; only a real browser run can prove it", screenshot: null };
    }
    queryRows(journey.simulatedWrite.sql);
    return { ok: true, status: "simulated", reason: `the screen was not driven; ${journey.simulatedWrite.description}`, screenshot: null };
  };
}

function parkedUiDriver(journey, context) {
  return {
    ok: true,
    status: "parked",
    reason: "the screen was not driven in this run; only the data half of the journey ran",
    screenshot: null
  };
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const catalogue = loadCatalogue();

  if (args.selfTest) {
    selfTest(catalogue);
    return 0;
  }

  const databaseUrl = process.env.GOATOS_WRITE_JOURNEY_DATABASE_URL;
  if (!databaseUrl) {
    console.error("GOATOS_WRITE_JOURNEY_DATABASE_URL is required; this lane never reads a production or STG DSN");
    return 1;
  }
  // Guard first, always. Nothing below runs against a database this lane may not write.
  const verdict = assertWriteTarget(databaseUrl, process.env);
  // A missing screen driver must be a runner blocker, never a journey failure: reporting
  // "publishing a plan version did not save" because an npm script is absent would be a lie
  // about the product, and a farm manager would act on it. Checked after the target guard, before anything connects.
  if (!args.simulateWrite && !args.noUi && !args.stampClone && !adminWebScripts()[UI_DRIVER_SCRIPT]) {
    console.error(`the screen driver \`${UI_DRIVER_SCRIPT}\` is not present in apps/admin-web; run with --simulate-write to exercise the data half, or build the driver. Refusing to report a missing driver as a broken screen.`);
    return 1;
  }

  const sql = psqlRunner(databaseUrl, { spawnSync, psqlBin: process.env.GOATOS_PSQL_BIN || "psql", cwd: repo, redact: redactText });
  const engine = createTableSnapshotEngine({ sql });

  if (args.stampClone) {
    // Stamping is the one write allowed before the marker exists. Every other guard still applies:
    // a managed Cloud SQL instance, a read-only session or a non-loopback server is still refused.
    const unmarked = createTableSnapshotEngine({ sql, requireMarker: false });
    unmarked.assertLive();
    unmarked.stampClone();
    console.log(`write journeys: stamped ${verdict.target} as a disposable clone`);
    return 0;
  }

  engine.assertLive();

  const outPath = path.resolve(args.out ?? path.join(repo, ".codex-goatos-render/dashboard-automation/write-journeys/write-journeys-receipt.json"));
  mkdirSync(path.dirname(outPath), { recursive: true });
  const screenshotDir = path.join(path.dirname(outPath), "screenshots");
  mkdirSync(screenshotDir, { recursive: true });

  const problems = validateCatalogue(catalogue);
  if (problems.length) {
    console.error("write journeys refuse to run; the catalogue is not valid:");
    for (const problem of problems) console.error(`- ${problem}`);
    return 1;
  }

  const selected = args.only ? catalogue.journeys.filter((j) => j.name === args.only) : catalogue.journeys;
  if (selected.length === 0) throw new Error(`no journey named ${args.only}`);

  const driveUi = args.simulateWrite ? simulatedWriteDriver(sql) : args.noUi ? parkedUiDriver : spawnUiDriver;
  const token = makeToken();
  const receipt = {
    startedAt: new Date().toISOString(),
    target: verdict.target,
    targetKind: verdict.kind,
    screenDriven: !args.noUi && !args.simulateWrite,
    screenParkedReason: args.simulateWrite
      ? "the write each journey's screen makes was applied directly; the screen itself was not proved in this run"
      : args.noUi ? "only the data half of each journey ran" : null,
    journeys: []
  };
  for (const journey of selected) {
    receipt.journeys.push(runJourney(journey, { engine, driveUi, queryRows: sql, token, screenshotDir }));
    write(outPath, receipt);
  }
  const summary = summarise(receipt.journeys);
  receipt.finishedAt = new Date().toISOString();
  receipt.status = summary.status;
  receipt.summary = summary;
  write(outPath, receipt);
  console.log(`write journeys ${summary.status}: ${summary.total - summary.failed}/${summary.total} passed, ${summary.restoreProblems} restore problem(s); receipt ${path.relative(repo, outPath)}`);
  return summary.status === "pass" ? 0 : 1;
}

function write(file, value) {
  const text = `${JSON.stringify(value, null, 2)}\n`;
  if (containsUnredactedSecret(text)) throw new Error("refusing to write a write-journey receipt that appears to contain an unredacted secret");
  writeFileSync(file, text);
}

function selfTest(catalogue = loadCatalogue()) {
  const problems = validateCatalogue(catalogue);
  if (problems.length) throw new Error(`self-test: catalogue is not valid:\n- ${problems.join("\n- ")}`);
  const roadmap = ["publish", "feed rate", "sop", "task", "sale", "verification"];
  const names = catalogue.journeys.map((j) => j.name).join(" ");
  for (const word of roadmap) {
    if (!new RegExp(word.replaceAll(" ", "[- ]"), "i").test(names)) {
      throw new Error(`self-test: the catalogue does not cover the roadmap's "${word}" journey`);
    }
  }
  const rules = catalogue.journeys.filter((j) => j.rule === true);
  if (rules.length < 3) throw new Error("self-test: the three business rules that keep regressing must all be covered");

  // The guard must refuse production and STG shapes, from this file, every time.
  const env = {
    GOATOS_STG_READONLY_DATABASE_URL: "postgres://reader@127.0.0.1:5455/goatos",
    GOATOS_WRITE_JOURNEY_TARGET: "oci-clone",
    GOATOS_WRITE_JOURNEY_CLONE_URL: "postgres://postgres@127.0.0.1:5432/goatos"
  };
  const refusals = [
    "postgres://user@10.20.30.40:5432/goatos",
    "postgres://user@goatos-stg-core-db.asia-south1.gcp:5432/goatos",
    "postgres://reader@127.0.0.1:5455/goatos",
    "postgres://user@127.0.0.1:5432/goatos_production"
  ];
  for (const url of refusals) {
    if (classifyWriteTarget(url, env).allowed) throw new Error(`self-test: the guard allowed ${describeTarget(url)}`);
  }
  if (!classifyWriteTarget("postgres://postgres@127.0.0.1:5432/goatos", env).allowed) {
    throw new Error("self-test: the guard must allow the declared OCI clone");
  }
  if (!classifyWriteTarget("postgres://p@127.0.0.1:5999/goatos_dashboard_automation_tmp", {}).allowed) {
    throw new Error("self-test: the guard must allow a disposable automation database");
  }
  // This lane can never point at production: there is no configuration in which it does.
  for (const attempt of [
    { url: "postgres://postgres@127.0.0.1:5432/goatos", env: {} },
    { url: "postgres://postgres@127.0.0.1:5432/goatos", env: { GOATOS_WRITE_JOURNEY_TARGET: "production" } },
    { url: "postgres://postgres@dashboard.mesha.sg:5432/goatos_dashboard_automation", env: {} }
  ]) {
    if (classifyWriteTarget(attempt.url, attempt.env).allowed) {
      throw new Error("self-test: this lane must never be pointable at anything but a declared clone or a disposable database");
    }
  }
  console.log(`write journeys: self-test passed (${catalogue.journeys.length} journeys, ${rules.length} business rules)`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    process.exit(main());
  } catch (error) {
    console.error(redactText(String(error?.message ?? error)));
    process.exit(1);
  }
}
