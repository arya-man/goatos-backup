#!/usr/bin/env node
// Lane 6 (delta) mutation prover.
//
// "Covered" means the check FAILS when the rule is broken. For a transition check that means
// three things, and all three must hold before the check counts:
//   1. On the LEGAL overnight change it stays quiet — AND it examined something while staying
//      quiet, so its silence is a judgement and not an empty list.
//   2. On the ILLEGAL change, with the same readings otherwise identical, it fires.
//   3. With a reading it needs taken away, it says "not checked" — never "pass". A missing
//      input must push the answer towards not knowing, never towards everything being fine.
//
// It touches no database: the readings are JSON, which is exactly why every check here can be
// proved. A check with no fixture is reported as a named gap, never as coverage.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { DELTA_CHECKS } from "./delta-checks.mjs";
import { runChecks } from "./check-delta.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../..");
export const fixturesPath = path.join(here, "delta-mutations.json");

export function loadFixtures(file = fixturesPath) {
  // Fail closed: a deleted fixture file must not read as a clean proof.
  if (!existsSync(file)) throw new Error("the delta mutation fixture file is missing, so nothing can be proved; refusing to report a pass");
  const parsed = JSON.parse(readFileSync(file, "utf8"));
  if (!parsed || typeof parsed.mutations !== "object" || Array.isArray(parsed.mutations)) {
    throw new Error("the delta mutation fixture file has no mutations, so nothing can be proved");
  }
  return parsed;
}

export function buildSnapshot(fixtures, day, sections) {
  return {
    takenAt: `${day}T04:00:00.000Z`,
    businessDate: day,
    lane: "delta",
    movement: { since: fixtures.days.before, bounded: "fixture" },
    takenAgainst: { ...fixtures.provenance },
    readOnly: { declared: true, proven: true },
    sections,
    integrity: { sectionsListed: Object.keys(sections).length, sectionsRead: Object.keys(sections).length, sectionsNotRead: 0, sectionsCapped: 0, complete: true }
  };
}

/** The single check's row out of a full comparison, so the prover exercises the real runner. */
function outcomeOf(check, before, after) {
  const { ledger } = runChecks(before, after, [check]);
  return ledger[0];
}

const args = parseArgs(process.argv.slice(2));
const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) process.exit(main());

function main() {
  const fixtures = loadFixtures();
  const report = {
    generatedAt: new Date().toISOString(),
    lane: "delta",
    database: "none — these checks read two stored readings, so nothing was contacted",
    howProved: "each check stayed quiet on a legal overnight change while examining it, fired on the illegal one, and said it had not been checked when a reading it needs was taken away",
    whatThisNumberIs: "coverage of the fixture file: how many transition checks have an illegal overnight change written for them that this run could plant and show them discriminate. It is not a measurement of the live farm.",
    totalChecks: DELTA_CHECKS.length,
    proved: [],
    notProved: [],
    missingInputIsNotAPass: [],
    missingInputTreatedAsAPass: [],
    partialInputIsNotAPass: [],
    partialInputTreatedAsAPass: []
  };

  for (const check of DELTA_CHECKS) {
    const fixture = fixtures.mutations[check.name];
    if (!fixture?.before || !fixture?.after || !fixture?.illegalAfter) {
      report.notProved.push({ name: check.name, reason: "no illegal overnight change has been written for this check yet, so it has never been shown to fire" });
      continue;
    }
    const before = buildSnapshot(fixtures, fixtures.days.before, fixture.before);
    const legalAfter = buildSnapshot(fixtures, fixtures.days.after, fixture.after);
    const illegalAfter = buildSnapshot(fixtures, fixtures.days.after, fixture.illegalAfter);

    const legal = outcomeOf(check, before, legalAfter);
    const illegal = outcomeOf(check, before, illegalAfter);

    // Half three, run for every check whether or not the other two pass: taking a reading away
    // must move the answer to "not checked".
    for (const needed of check.needs) {
      const stripped = buildSnapshot(fixtures, fixtures.days.after, Object.fromEntries(
        Object.entries(fixture.after).filter(([name]) => name !== needed)
      ));
      const blind = outcomeOf(check, before, stripped);
      const row = { name: check.name, reading: needed };
      // And the half-read case, which is the subtler one: a reading that hit its row cap is a
      // PARTIAL reading, and a partial reading compared with another is two things agreeing
      // about a part of the farm while saying nothing about the rest.
      const half = buildSnapshot(fixtures, fixtures.days.after, {
        ...fixture.after,
        [needed]: { ...fixture.after[needed], capped: true }
      });
      const partial = outcomeOf(check, before, half);
      if (partial.ran === false && partial.outcome === "not checked") {
        report.partialInputIsNotAPass.push({ name: check.name, reading: needed });
      } else {
        report.partialInputTreatedAsAPass.push({ name: check.name, reading: needed, reason: `with this reading only half taken the check reported "${partial.outcome}" instead of saying it had not been checked` });
      }
      if (blind.ran === false && blind.outcome === "not checked" && typeof blind.reason === "string" && blind.reason.includes(needed.replaceAll("_", " "))) {
        report.missingInputIsNotAPass.push(row);
      } else {
        report.missingInputTreatedAsAPass.push({ ...row, reason: `with this reading missing the check reported "${blind.outcome}" instead of saying it had not been checked` });
      }
    }

    if (!legal.ran) {
      report.notProved.push({ name: check.name, reason: `on the legal overnight change the check did not run at all: ${legal.reason}` });
      continue;
    }
    if (legal.examined === 0) {
      // Quiet on nothing is not quiet. This is the "two readings that found nothing agree" trap.
      report.notProved.push({ name: check.name, reason: "on the legal overnight change the check examined nothing, so its silence is not a judgement" });
      continue;
    }
    if (legal.outcome !== "did not change illegally") {
      report.notProved.push({ name: check.name, reason: `the legal overnight change was reported as "${legal.outcome}", so this check accuses the farm of doing the right thing` });
      continue;
    }
    if (!illegal.ran || illegal.outcome !== "changed illegally") {
      report.notProved.push({ name: check.name, reason: `the illegal overnight change was reported as "${illegal.outcome}", so this check cannot fail and is not coverage` });
      continue;
    }
    report.proved.push({
      name: check.name,
      defect: fixture.defect ?? null,
      examinedOnTheLegalNight: legal.examined,
      breachesOnTheIllegalNight: illegal.breachCount ?? 0
    });
  }

  report.coverage = `${report.proved.length}/${report.totalChecks}`;
  report.missingInputCoverage = `${report.missingInputIsNotAPass.length}/${report.missingInputIsNotAPass.length + report.missingInputTreatedAsAPass.length}`;
  report.partialInputCoverage = `${report.partialInputIsNotAPass.length}/${report.partialInputIsNotAPass.length + report.partialInputTreatedAsAPass.length}`;
  const out = path.resolve(args.out ?? path.join(repo, ".codex-goatos-render/dashboard-automation/delta-mutations.json"));
  mkdirSync(path.dirname(out), { recursive: true });
  writeFileSync(out, `${JSON.stringify(report, null, 2)}\n`);
  console.log(`delta mutation proof: ${report.proved.length} of ${report.totalChecks} transition checks stayed quiet on a legal overnight change they actually examined, and fired on the illegal one`);
  console.log(`  a missing reading: ${report.missingInputIsNotAPass.length} of ${report.missingInputIsNotAPass.length + report.missingInputTreatedAsAPass.length} cases said the change had not been checked, rather than reporting it fine`);
  for (const gap of report.notProved) console.log(`  not proved — ${gap.name}: ${gap.reason}`);
  console.log(`  a half-taken reading: ${report.partialInputIsNotAPass.length} of ${report.partialInputIsNotAPass.length + report.partialInputTreatedAsAPass.length} cases said the change had not been checked, rather than judging the farm on part of it`);
  for (const gap of report.partialInputTreatedAsAPass) console.log(`  a half-taken reading read as fine — ${gap.name} (${gap.reading}): ${gap.reason}`);
  for (const gap of report.missingInputTreatedAsAPass) console.log(`  a missing reading read as fine — ${gap.name} (${gap.reading}): ${gap.reason}`);
  return report.notProved.length === 0 && report.missingInputTreatedAsAPass.length === 0 && report.partialInputTreatedAsAPass.length === 0 ? 0 : 1;
}

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i += 1) if (argv[i] === "--out") out.out = argv[++i];
  return out;
}
