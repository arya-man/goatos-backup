#!/usr/bin/env node
// Lane 6 (delta) — judge the change between two readings of the farm.
//
// This script touches no database. It reads two snapshots that capture-delta-snapshot.mjs took,
// refuses to compare them unless they are readings of the same thing, and then asks each
// transition check whether the change between them was legal.
//
// Every outcome is one of four, and they are kept apart on purpose:
//   "did not change illegally" — the check ran and compared real things
//   "changed illegally"        — the check ran and found a breach
//   "nothing to compare"       — the check ran and found nothing of its kind had moved
//   "not checked"              — the check could not run, and the reason says why
// The fourth is the one that matters. Two absent readings agree with each other, and that
// agreement is how a false pass is born.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { DELTA_CHECKS } from "./delta-checks.mjs";
import { defaultStoreDir, listSnapshots } from "./capture-delta-snapshot.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

/**
 * Whether two readings are readings of the SAME THING. Consistency is not correctness when the
 * two things compared are not the same thing: across a migration or a redeploy the difference
 * between them is a difference in the software, and reporting that as the farm changing
 * illegally is a false accusation of the people who run it.
 */
export function comparability(before, after) {
  const refuse = (reason) => ({ comparable: false, reason });
  if (!before || !after) return refuse("only one reading is on record, so there is nothing to compare it with");
  if (!before.businessDate || !after.businessDate) return refuse("one of the readings is not filed under a farm day");
  if (!(after.businessDate > before.businessDate)) {
    return refuse("the two readings are from the same farm day or the wrong way round, so no overnight change can be read from them");
  }
  const b = before.takenAgainst ?? {};
  const a = after.takenAgainst ?? {};
  if (!b.schemaVersion || !a.schemaVersion) return refuse("one of the readings does not record what it was taken against, so it cannot safely be compared with another");
  if (b.schemaVersion !== a.schemaVersion) {
    return refuse("the farm's records were upgraded between the two readings, so the difference between them is a change in the software and not a change on the farm");
  }
  if (b.databaseName !== a.databaseName) return refuse("the two readings were taken from different stores of the farm's records");
  if (b.appVersion && a.appVersion && b.appVersion !== a.appVersion) {
    return refuse("a new version of the software was released between the two readings, so the difference between them is not a change on the farm");
  }
  const farmsBefore = [...(b.farms ?? [])].sort().join(",");
  const farmsAfter = [...(a.farms ?? [])].sort().join(",");
  if (farmsBefore !== farmsAfter) return refuse("the two readings cover different farms, so they are not readings of the same thing");
  if (after.movement?.since && after.movement.since !== before.businessDate) {
    return refuse("today's reading of what moved was bounded to a different day than the reading it is being compared with, so the two do not line up");
  }
  const gapDays = Math.round((Date.parse(`${after.businessDate}T00:00:00Z`) - Date.parse(`${before.businessDate}T00:00:00Z`)) / 86400000);
  return { comparable: true, gapDays };
}

/** Every check gets a line, every run: it ran, or it did not and here is why. */
export function startLedger(checks) {
  return checks.map((check) => ({
    name: check.name,
    question: check.question,
    page: check.page ?? null,
    ran: false,
    outcome: "not checked",
    examined: 0,
    reason: "this check did not run in this comparison"
  }));
}

export function runChecks(before, after, checks = DELTA_CHECKS) {
  const ledger = startLedger(checks);
  const findings = [];
  const fit = comparability(before, after);
  if (!fit.comparable) {
    for (const row of ledger) row.reason = fit.reason;
    return { ledger, findings, comparability: fit };
  }
  for (const check of checks) {
    const row = ledger.find((entry) => entry.name === check.name);
    let result;
    try {
      result = check.run(before, after);
    } catch (error) {
      row.reason = `this check could not be carried out: ${String(error?.message ?? error).slice(0, 200)}`;
      continue;
    }
    if (result?.notChecked) {
      row.reason = result.notChecked;
      continue;
    }
    // Counted from what was READ. A check that compared nothing says so; it never borrows the
    // word "pass" from a check that compared something.
    row.ran = true;
    row.reason = null;
    row.examined = Number(result?.examined ?? 0);
    const breaches = result?.breaches ?? [];
    if (row.examined === 0) {
      row.outcome = "nothing to compare";
      row.reason = "nothing of this kind changed between the two readings, so there was nothing to judge";
      continue;
    }
    if (breaches.length === 0) {
      row.outcome = "did not change illegally";
      continue;
    }
    row.outcome = "changed illegally";
    row.breachCount = breaches.length;
    findings.push({
      name: check.name,
      question: check.question,
      rule: check.rule,
      severity: check.severity ?? "medium",
      page: check.page ?? null,
      countUnit: check.countUnit,
      examined: row.examined,
      breachCount: breaches.length,
      examples: breaches.slice(0, 10).map((breach) => breach.sentence)
    });
  }
  return { ledger, findings, comparability: fit };
}

export function summary(ledger) {
  const ran = ledger.filter((row) => row.ran);
  return {
    total: ledger.length,
    ran: ran.length,
    notChecked: ledger.length - ran.length,
    comparedSomething: ran.filter((row) => row.examined > 0).length,
    nothingToCompare: ran.filter((row) => row.examined === 0).length,
    thingsCompared: ran.reduce((sum, row) => sum + Number(row.examined ?? 0), 0)
  };
}

export function layerSentence(findings, stats, fit) {
  if (!fit?.comparable) return `Last night's changes on the farm were not checked: ${fit?.reason ?? "the two readings could not be compared"}.`;
  if (!stats.ran) return "Last night's changes on the farm were not checked; none of the checks could be run.";
  const parts = [];
  if (findings.length) {
    const breaches = findings.reduce((sum, item) => sum + Number(item.breachCount ?? 0), 0);
    parts.push(`${breaches} thing${breaches === 1 ? "" : "s"} changed on the farm overnight in a way the farm's own rules do not allow`);
  } else if (stats.comparedSomething) {
    parts.push(`Everything that changed on the farm overnight changed the way it should have (${stats.thingsCompared} change${stats.thingsCompared === 1 ? "" : "s"} looked at)`);
  } else {
    parts.push("Nothing that these checks watch changed on the farm overnight");
  }
  if (stats.notChecked) parts.push(`${stats.notChecked} of ${stats.total} checks could not be run`);
  return `${parts.join(", and ")}.`;
}

const args = parseArgs(process.argv.slice(2));
const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) process.exit(args.selfTest ? selfTest() : main());

function loadSnapshot(file) {
  if (!existsSync(file)) throw new Error(`no reading of the farm was found at ${file}`);
  return JSON.parse(readFileSync(file, "utf8"));
}

function main() {
  let before = null;
  let after = null;
  let source = "";
  if (args.before && args.after) {
    before = loadSnapshot(path.resolve(args.before));
    after = loadSnapshot(path.resolve(args.after));
    source = "two readings named on the command line";
  } else {
    const store = path.resolve(args.store ?? defaultStoreDir());
    const all = listSnapshots(store);
    // The two most recent readings from DIFFERENT farm days. Two readings taken on the same day
    // describe no overnight change, and comparing them would report a quiet night for a day
    // nobody looked at.
    const byDay = [];
    for (const entry of all) {
      if (!byDay.length || byDay[byDay.length - 1].snapshot.businessDate !== entry.snapshot.businessDate) byDay.push(entry);
      else byDay[byDay.length - 1] = entry;
    }
    after = byDay.length ? byDay[byDay.length - 1].snapshot : null;
    before = byDay.length > 1 ? byDay[byDay.length - 2].snapshot : null;
    source = `the two most recent readings in ${path.relative(repo, store) || store}`;
  }

  const { ledger, findings, comparability: fit } = runChecks(before, after);
  const stats = summary(ledger);
  const report = {
    generatedAt: new Date().toISOString(),
    lane: "delta",
    comparedFrom: before?.businessDate ?? null,
    comparedTo: after?.businessDate ?? null,
    snapshotSource: source,
    comparability: fit,
    readingsIntegrity: {
      yesterday: before?.integrity ?? null,
      today: after?.integrity ?? null
    },
    status: !fit.comparable || !stats.ran ? "parked" : (findings.length ? "fail" : "pass"),
    ledger,
    summary: stats,
    findings
  };
  report.slackLayerMessage = layerSentence(findings, stats, fit);
  const out = path.resolve(args.out ?? path.join(repo, ".codex-goatos-render/dashboard-automation/delta.json"));
  mkdirSync(path.dirname(out), { recursive: true });
  writeFileSync(out, `${JSON.stringify(report, null, 2)}\n`);
  const line = `delta ${report.status}: ${report.slackLayerMessage} (${stats.ran} of ${stats.total} checks ran, ${stats.notChecked} not checked, report ${path.relative(repo, out)})`;
  if (report.status === "pass") { console.log(line); return 0; }
  console.error(line);
  return report.status === "fail" ? 1 : 2;
}

function parseArgs(argv) {
  const out = { selfTest: false };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--self-test") out.selfTest = true;
    else if (arg === "--before") out.before = argv[++i];
    else if (arg === "--after") out.after = argv[++i];
    else if (arg === "--store") out.store = argv[++i];
    else if (arg === "--out") out.out = argv[++i];
  }
  return out;
}

function selfTest() {
  const names = new Set();
  for (const check of DELTA_CHECKS) {
    for (const field of ["name", "question", "rule", "countUnit"]) {
      if (typeof check[field] !== "string" || !check[field].trim()) throw new Error(`${check.name}: ${field} is missing`);
    }
    if (!Array.isArray(check.needs) || !check.needs.length) throw new Error(`${check.name}: names no readings it needs`);
    if (names.has(check.name)) throw new Error(`${check.name}: two checks share a name`);
    names.add(check.name);
  }
  console.log(`delta self-test: ${DELTA_CHECKS.length} transition checks, each naming the rule it came from and the readings it needs`);
  return 0;
}
