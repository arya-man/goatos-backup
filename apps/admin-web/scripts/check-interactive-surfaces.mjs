#!/usr/bin/env node
// Gate: every edit form, inline editor, row action and modal in admin-web carries a decision, and
// every decision claimed as "covered" carries an assertion that can go red.
//
// Offline and instant: it reads the source tree and one JSON ledger. No browser, no network, no DB.
//
// Trap, handover §8: "--self-test proves nothing about code the dry-run path returns before
// reaching". So --self-test does NOT short-circuit. It runs the REAL scan and the REAL ledger
// validation first, exactly as the default path does, and only then adds its fixture cases.
import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  coverageSentence,
  gradeAssertion,
  scanInteractiveSurfaces,
  validateLedger,
} from "./lib/interactive-surfaces.mjs";

const adminWeb = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const LEDGER = path.join(adminWeb, "scripts/interactive-surface-ledger.json");
const ROOTS = ["features", "components", "app"];

export function readSourceFiles(root = adminWeb, roots = ROOTS) {
  const files = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      if (name === "node_modules" || name.startsWith(".")) continue;
      const full = path.join(dir, name);
      const info = statSync(full);
      if (info.isDirectory()) walk(full);
      else if (/\.(tsx|jsx)$/.test(name)) {
        files.push({ path: path.relative(root, full).replaceAll(path.sep, "/"), text: readFileSync(full, "utf8") });
      }
    }
  };
  for (const dir of roots) {
    const full = path.join(root, dir);
    try {
      if (statSync(full).isDirectory()) walk(full);
    } catch {
      /* a root that is not there is reported by the scan being empty, not by a crash */
    }
  }
  return files;
}

function run() {
  const surfaces = scanInteractiveSurfaces(readSourceFiles());
  let ledger;
  try {
    ledger = JSON.parse(readFileSync(LEDGER, "utf8"));
  } catch (error) {
    console.error(`interactive-surface ledger could not be read: ${error.message}`);
    return { exit: 1, surfaces, coverage: null };
  }
  const { problems, coverage } = validateLedger(surfaces, ledger);
  if (problems.length) {
    console.error("interactive-surface coverage gate failed:");
    for (const problem of problems) console.error(`- ${problem}`);
    return { exit: 1, surfaces, coverage };
  }
  console.log(coverageSentence(coverage));
  const notChecked = ledger.entries.filter((e) => e.status === "not-checked").length;
  console.log(`not checked, each with a stated reason: ${notChecked}`);
  return { exit: 0, surfaces, coverage };
}

function selfTest(realResult) {
  const failures = [];
  const expect = (label, condition) => {
    if (!condition) failures.push(label);
  };

  // The real scan must actually have found something; an empty inventory would make the whole
  // gate vacuous and it would otherwise report a cheerful 0/0.
  expect("the real scan found no interactive surfaces at all", realResult.surfaces.length > 0);

  // The exact failure that broke the old ledger: a heading check against a blank screen.
  expect(
    "a heading assertion that survives a blank screen was accepted",
    !gradeAssertion({ subject: "the Spend share heading", operator: "text-matches", expected: ".*", blankScreenValue: "" }).ok,
  );
  expect(
    "the four value-less operators were accepted",
    ["visible", "absent", "count", "url"].every(
      (operator) => !gradeAssertion({ subject: "s", operator, expected: "x", blankScreenValue: "" }).ok,
    ),
  );
  // ...and the same shape, stated properly, must be accepted.
  expect(
    "a discriminating assertion was rejected",
    gradeAssertion({
      subject: "the feeds named in the Spend share slices",
      operator: "field-set-equals",
      expected: ["Maize", "Soya", "Mineral mix"],
      blankScreenValue: [],
    }).ok,
  );
  // A surface added to the tree must not be able to hide.
  const planted = scanInteractiveSurfaces([
    { path: "features/x/new-thing.tsx", text: '<form action={saveAction} className="x-form">' },
  ]);
  expect("a newly added edit form was not detected", planted.length === 1 && planted[0].kind === "edit-form");
  expect("test files were scanned", scanInteractiveSurfaces([{ path: "features/x/a.test.tsx", text: "<form>" }]).length === 0);

  if (failures.length) {
    console.error("interactive-surface self-test failed:");
    for (const failure of failures) console.error(`- ${failure}`);
    return 1;
  }
  console.log(`interactive-surface self-test: ${5} cases green, on top of the real scan above`);
  return 0;
}

// Only when run as a command; importing this module (the generator, the tests) must not exit.
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const real = run();
  const wantsSelfTest = process.argv.includes("--self-test");
  process.exit(wantsSelfTest ? (real.exit || selfTest(real)) : real.exit);
}

export { run, selfTest };
