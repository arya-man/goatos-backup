#!/usr/bin/env node
// Which tests need a browser, answered by watching, never by reading the source.
//
// There are at least three ways a test reaches one and no grep sees all three:
//   direct          it imports Playwright and launches
//   transitive      a library it imports launches (compositing-checks)
//   child process   it execFiles a fixture script that launches (procurement-answer-accessibility,
//                   which greps ZERO for chromium or playwright)
// What they share is the last step: a browser binary is looked for and launched. So the probe
// watches for that, and the manifest is DERIVED from what it saw. A hand-listed manifest is the
// census defect again - a list nobody measured, going stale the day after it is written.
//
// Drift is an error in BOTH directions: a test that needs a browser and is not listed, and a
// listing for a test that no longer needs one.
//
// On a machine with no browser installed the answer is NOT CHECKED - never a pass, never a
// failure. That case is live: a missing binary makes a browser test fail with "Executable doesn't
// exist", which reads as a defect in the test and is a false accusation.
import { existsSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const MANIFEST = "tools/ci/browser-tests.json";
const RECORDER = "tools/ci/lib/browser-probe-recorder.cjs";

/** Where the tests this answers for live. */
export const TEST_DIRS = ["apps/admin-web/scripts", "apps/admin-web/scripts/lib", "tools/dashboard-automation"];

export function enumerateTests(dirs = TEST_DIRS, readDir = (d) => (existsSync(path.join(repo, d)) ? readdirSync(path.join(repo, d)) : [])) {
  const found = [];
  for (const dir of dirs) {
    for (const entry of readDir(dir)) {
      if (entry.endsWith(".test.mjs")) found.push(`${dir}/${entry}`);
    }
  }
  return found.sort();
}

/** Runs one test file and reports whether it went looking for a browser. */
export function probeTest(file, { timeoutMs = 90000 } = {}) {
  const dir = mkdtempSync(path.join(os.tmpdir(), "goatos-browser-probe-"));
  const log = path.join(dir, "browser.txt");
  writeFileSync(log, "");
  try {
    const result = spawnSync(process.execPath, [path.join(repo, file)], {
      cwd: repo,
      encoding: "utf8",
      timeout: timeoutMs,
      env: {
        ...process.env,
        // Reaches child node processes too, which is the third route.
        NODE_OPTIONS: `${process.env.NODE_OPTIONS ?? ""} --require ${path.join(repo, RECORDER)}`.trim(),
        GOATOS_BROWSER_PROBE_LOG: log
      }
    });
    const lines = readFileSync(log, "utf8").split("\n").filter(Boolean);
    return {
      needsBrowser: lines.some((line) => line.startsWith("browser\t")),
      browserMissing: lines.some((line) => line.startsWith("missing\t")),
      timedOut: result.error?.code === "ETIMEDOUT",
      status: result.status
    };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

export function compare(observed, listed) {
  const listedSet = new Set(listed);
  const observedSet = new Set(observed);
  return {
    unlisted: observed.filter((file) => !listedSet.has(file)),
    stale: listed.filter((file) => !observedSet.has(file))
  };
}

const args = parse(process.argv.slice(2));
const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) process.exit(args.selfTest ? selfTest() : run());

function run() {
  const tests = enumerateTests();
  if (!tests.length) {
    console.error("no test files were found where they are expected, so nothing could be judged");
    return 1;
  }
  const observed = [];
  const notChecked = [];
  for (const file of tests) {
    const seen = probeTest(file);
    if (seen.needsBrowser && seen.browserMissing) {
      // The test asked for a browser this machine does not have. It NEEDS one - that is answered -
      // but whether it passes is not, and saying otherwise would blame the test for the machine.
      observed.push(file);
      notChecked.push({ file, reason: "needs a browser this machine does not have installed, so whether it passes was not checked" });
      continue;
    }
    if (seen.needsBrowser) observed.push(file);
    else if (seen.timedOut) notChecked.push({ file, reason: "did not finish in time, so whether it needs a browser was not established" });
  }

  const manifestPath = path.join(repo, MANIFEST);
  if (args.update) {
    writeFileSync(manifestPath, `${JSON.stringify({
      description: "Tests that go looking for a browser. DERIVED by tools/ci/check-browser-tests.mjs --update, never hand-written: a grep cannot answer this, because a test can reach a browser directly, through a library it imports, or through a child process it runs.",
      tests: observed
    }, null, 2)}\n`);
    console.log(`recorded ${observed.length} test(s) that need a browser`);
    return 0;
  }
  if (!existsSync(manifestPath)) {
    console.error(`${MANIFEST} is missing, so nothing says which tests need a browser; run with --update`);
    return 1;
  }
  const listed = JSON.parse(readFileSync(manifestPath, "utf8"))?.tests ?? [];
  const { unlisted, stale } = compare(observed, listed);
  for (const row of notChecked) console.log(`NOT CHECKED — ${row.file}: ${row.reason}`);
  if (unlisted.length || stale.length) {
    console.error("the list of tests that need a browser no longer matches what they do");
    for (const file of unlisted) console.error(`  - ${file} goes looking for a browser and is not listed`);
    for (const file of stale) console.error(`  - ${file} is listed and no longer goes looking for one`);
    console.error("Run with --update to record what they actually do.");
    return 1;
  }
  console.log(`browser tests: ${observed.length} of ${tests.length} test file(s) go looking for a browser, and the list matches - watched, not grepped`);
  return 0;
}

function parse(raw) {
  const parsed = {};
  for (const arg of raw) {
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--update") parsed.update = true;
    else throw new Error(`unknown argument: ${arg}`);
  }
  return parsed;
}

function selfTest() {
  // Drift in both directions is an error, like the guard ledger.
  const both = compare(["a.test.mjs", "b.test.mjs"], ["b.test.mjs", "c.test.mjs"]);
  if (!both.unlisted.includes("a.test.mjs")) throw new Error("self-test: an unlisted browser test must be caught");
  if (!both.stale.includes("c.test.mjs")) throw new Error("self-test: a stale listing must be caught");
  if (compare(["a"], ["a"]).unlisted.length || compare(["a"], ["a"]).stale.length) {
    throw new Error("self-test: a matching list must pass");
  }
  // The enumeration must find the real test files, or the check answers for nothing.
  const tests = enumerateTests();
  if (tests.length < 20) throw new Error(`self-test: only ${tests.length} test file(s) found, which is not the tree`);

  // The probe itself: one that launches, one that does not.
  const dir = mkdtempSync(path.join(os.tmpdir(), "goatos-browser-selftest-"));
  try {
    const pure = path.join(dir, "pure.test.mjs");
    writeFileSync(pure, "export default 1;\n");
    if (probeTest(path.relative(repo, pure)).needsBrowser) {
      throw new Error("self-test: a test that touches no browser must not be listed as needing one");
    }
    const viaChild = path.join(dir, "child.test.mjs");
    writeFileSync(viaChild, `import { existsSync } from "node:fs";\nexistsSync(${JSON.stringify(path.join(os.homedir(), "Library/Caches/ms-playwright/chromium-1/chrome"))});\n`);
    if (!probeTest(path.relative(repo, viaChild)).needsBrowser) {
      throw new Error("self-test: looking for a browser binary must count as needing one, whether or not it is there");
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  console.log(`browser test list: self-test passed (${tests.length} test file(s) in scope)`);
  return 0;
}
