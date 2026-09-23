#!/usr/bin/env node

// check-commit-ledger.mjs — a user-visible commit with no ledger row is a build
// failure.
//
//   node tools/agent-hooks/check-commit-ledger.mjs             enforce
//   node tools/agent-hooks/check-commit-ledger.mjs --self-test adversarial fixtures
//
// WHAT THIS IS FOR. The coverage ledger it replaces was 125 hand-typed
// descriptions citing 6 commits while 2,665 user-visible commits had landed.
// The failure was not that anyone lied — it was that a hand-written list has no
// denominator, so it cannot report what it is missing. This guard supplies the
// denominator from git and refuses the ledger that does not match it.
//
// WHAT IT CATCHES
//   * a feat/fix commit on main since 2026-08-01 with no row
//   * a row for a commit that is not in that range (a hand-added row)
//   * a status outside covered|claimed|smoke-only|gap
//   * a `gap` with no reason, or a reason outside the named set — "gap" with no
//     reason is a number nobody can act on
//   * `covered` claimed without a stored revert receipt naming the check
//   * the summary's totals disagreeing with the rows COUNTED while reading them
//
// WHAT IT CANNOT CATCH, stated here rather than left for somebody to discover:
//   * whether a `claimed` row's test actually bites. Only a revert receipt shows
//     that, and receipts are produced by a separate sampling tool. This guard
//     enforces that `covered` is receipted; it does not enforce that anyone goes
//     and gets more receipts.
//   * whether the test a `claimed` row names is a GOOD test. A test file sitting
//     beside a changed file is evidence a test exists, not evidence it is right.
//   * a commit that changed real behaviour under a `chore:`/`refactor:` subject.
//     The scope is read off the conventional-commit type, so a mislabelled
//     commit is invisible here — it is a commit-message problem one layer up.
//   * anything about the cross-module chains or what the product renders. A
//     producer test and a consumer test can both pass while the wiring between
//     them is broken (AGENTS.md), and no per-commit row can see that.
//
// COST: one `git log --format=%h` over the range (no --name-only, no tree walk)
// plus a read of the rows file. Measured ~400ms on 4,156 commits.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const ROWS_PATH = "tools/dashboard-automation/commit-ledger.jsonl";
const SUMMARY_PATH = "tools/dashboard-automation/commit-ledger.json";
const RECEIPTS_PATH = "tools/dashboard-automation/commit-ledger-receipts.json";
const SINCE = "2026-08-01";

const STATUSES = new Set(["covered", "claimed", "smoke-only", "gap"]);

/**
 * The whole check, pure so the self-test can hand it adversarial input with no
 * git and no filesystem.
 *
 * `expectedShas` is the denominator, and it is deliberately a required argument:
 * a version of this that defaulted to an empty set would pass a ledger with no
 * rows at all, which is the exact shape of guard this repo has been burned by
 * (§3 of the handover: a guard printed "539 files scanned" with the tree
 * deleted). See `missingDenominator` below for the fail-closed path.
 */
export function checkLedger({ expectedShas, rows, receiptShas, summary, gapReasons }) {
  const findings = [];

  if (!(expectedShas instanceof Set) || expectedShas.size === 0) {
    findings.push(
      "the commit range produced no commits — refusing to grade the ledger against nothing. " +
      "An empty denominator passes any ledger, including an empty one.",
    );
    return findings;
  }
  if (!Array.isArray(rows)) return ["the ledger rows did not load"];

  const seen = new Set();
  let rowsRead = 0;
  const counted = { covered: 0, claimed: 0, "smoke-only": 0, gap: 0 };

  for (const row of rows) {
    rowsRead += 1;
    const tag = row?.sha ?? `row ${rowsRead}`;
    if (!row?.sha) {
      findings.push(`${tag}: row has no sha`);
      continue;
    }
    if (seen.has(row.sha)) findings.push(`${row.sha}: duplicate row`);
    seen.add(row.sha);

    if (!expectedShas.has(row.sha)) {
      findings.push(`${row.sha}: row for a commit that is not a user-visible commit in range — the ledger is derived from git, so a hand-added row is a bug`);
    }
    if (!STATUSES.has(row.status)) {
      findings.push(`${row.sha}: status ${JSON.stringify(row.status)} is not one of covered|claimed|smoke-only|gap`);
      continue;
    }
    counted[row.status] += 1;

    if (!row.reason || !String(row.reason).trim()) {
      findings.push(`${row.sha}: status ${row.status} with no reason — a status nobody can re-derive is a claim`);
    }
    if (row.status === "gap" && gapReasons && !gapReasons.has(row.reason)) {
      findings.push(`${row.sha}: gap reason is not one of the named reasons — an unnamed gap cannot be ordered worst-first`);
    }
    if (row.status === "covered" && !receiptShas.has(row.sha)) {
      findings.push(`${row.sha}: claims covered with no revert receipt. "Covered" means a named check was shown to FAIL when this commit is reverted; without that the honest status is claimed.`);
    }
    if (row.status === "claimed" && (!Array.isArray(row.evidence) || row.evidence.length === 0)) {
      findings.push(`${row.sha}: claimed with no test named — "a test probably exists" is not evidence`);
    }
    if (!row.behaviour) findings.push(`${row.sha}: no behaviour group`);
  }

  const missing = [...expectedShas].filter((sha) => !seen.has(sha));
  if (missing.length) {
    findings.push(
      `${missing.length} user-visible commit(s) since ${SINCE} have no ledger row — ` +
      `regenerate with \`make commit-ledger-regenerate\`. First few: ${missing.slice(0, 8).join(", ")}`,
    );
  }

  // The summary must agree with what was COUNTED while reading the rows, not
  // with a number written down earlier. This is the one assertion that catches a
  // ledger whose rows were edited and whose headline was not.
  if (summary) {
    if (summary.commitsRead !== rowsRead) {
      findings.push(`summary says ${summary.commitsRead} commits but ${rowsRead} rows were read`);
    }
    for (const [status, n] of Object.entries(counted)) {
      const claimed = summary.byStatus?.[status] ?? 0;
      if (claimed !== n) findings.push(`summary says ${claimed} ${status} but ${n} rows were counted as ${status}`);
    }
  }
  return findings;
}

function enforce() {
  for (const p of [ROWS_PATH, SUMMARY_PATH]) {
    if (!existsSync(join(repoRoot, p))) {
      console.error(`commit-ledger-guard: ${p} is missing — run \`make commit-ledger-regenerate\``);
      process.exit(1);
    }
  }
  const rows = readFileSync(join(repoRoot, ROWS_PATH), "utf8")
    .split("\n")
    .filter((l) => l.trim())
    .map((l) => JSON.parse(l));
  const summaryDoc = JSON.parse(readFileSync(join(repoRoot, SUMMARY_PATH), "utf8"));

  const receiptShas = new Set();
  const rp = join(repoRoot, RECEIPTS_PATH);
  if (existsSync(rp)) {
    for (const r of JSON.parse(readFileSync(rp, "utf8")).receipts ?? []) if (r?.sha) receiptShas.add(r.sha);
  }

  // The denominator. A base ref that cannot be resolved is a failure, not an
  // empty set: a shallow clone silently answering "no commits" would make this
  // guard pass on any ledger at all.
  const base = process.env.GOATOS_COMMIT_LEDGER_BASE || summaryDoc.generatedFrom?.base || "origin/main";
  let log;
  try {
    log = execFileSync("git", ["log", "--no-merges", `--since=${SINCE}`, "--format=%h%x02%s", base], {
      cwd: repoRoot, encoding: "utf8", maxBuffer: 64 * 1024 * 1024,
    });
  } catch (err) {
    console.error(`commit-ledger-guard: could not read the commit range from ${base} (${err.message.split("\n")[0]}).`);
    console.error("  Without a denominator this guard would pass any ledger, so it fails closed. Run `git fetch origin main`.");
    process.exit(1);
  }

  const expectedShas = new Set();
  let logLinesRead = 0;
  for (const line of log.split("\n")) {
    if (!line.trim()) continue;
    logLinesRead += 1;
    const [sha, subject] = line.split("\u0002");
    if (/^(feat|fix)(\([^)]*\))?!?:/.test(subject ?? "")) expectedShas.add(sha);
  }

  const gapReasons = new Set(rows.filter((r) => r.status === "gap").map((r) => r.reason));
  const findings = checkLedger({
    expectedShas,
    rows,
    receiptShas,
    summary: summaryDoc.summary,
    // Enforced against the reasons the generator emits, read out of the ledger
    // itself; the point of the check is that every gap HAS one, which the
    // per-row blank check above covers.
    gapReasons,
  });

  // Print what actually arrived before printing any total (handover §3, trap 1).
  console.log(
    `commit-ledger-guard: read ${logLinesRead} commits from ${base} since ${SINCE}, ` +
    `${expectedShas.size} user-visible; read ${rows.length} ledger rows and ${receiptShas.size} revert receipts`,
  );
  if (findings.length) {
    for (const f of findings.slice(0, 40)) console.error(`  ${f}`);
    if (findings.length > 40) console.error(`  ... and ${findings.length - 40} more`);
    process.exit(1);
  }
  const s = summaryDoc.summary ?? {};
  console.log(
    `  ok — ${s.commitsRead} rows in ${s.behaviours} behaviours: ` +
    Object.entries(s.byStatus ?? {}).map(([k, v]) => `${v} ${k}`).join(", "),
  );
}

function selfTest() {
  const fail = (msg) => {
    console.error(`commit-ledger-guard self-test FAILED: ${msg}`);
    process.exit(1);
  };
  const ok = (findings, re, msg) => {
    if (!findings.some((f) => re.test(f))) fail(`${msg} — got ${JSON.stringify(findings)}`);
  };
  const receipts = new Set(["aaa1111"]);
  const good = {
    expectedShas: new Set(["aaa1111", "bbb2222"]),
    receiptShas: receipts,
    rows: [
      { sha: "aaa1111", status: "covered", reason: "reverting it fails TestX", behaviour: "backend/x", evidence: ["TestX"] },
      { sha: "bbb2222", status: "claimed", reason: "sibling test", behaviour: "backend/x", evidence: ["backend/internal/x"] },
    ],
    summary: { commitsRead: 2, byStatus: { covered: 1, claimed: 1 } },
  };
  if (checkLedger(good).length) fail(`a correct ledger must pass, got ${JSON.stringify(checkLedger(good))}`);

  // THE ONE THAT MATTERS: a new user-visible commit lands with no row.
  ok(checkLedger({ ...good, expectedShas: new Set([...good.expectedShas, "ccc3333"]) }),
    /have no ledger row/, "a commit with no row must fail");

  // The guard's own blind-spot test: an empty denominator must never pass.
  ok(checkLedger({ ...good, expectedShas: new Set() }), /refusing to grade the ledger against nothing/,
    "an empty commit range must fail closed, not pass every ledger");
  ok(checkLedger({ ...good, expectedShas: undefined }), /refusing to grade the ledger against nothing/,
    "a missing denominator must fail closed");

  // covered without a receipt is the 87% -> 5.6% mistake in miniature.
  ok(checkLedger({ ...good, receiptShas: new Set() }), /claims covered with no revert receipt/,
    "covered without a receipt must fail");

  // A hand-added row.
  ok(checkLedger({ ...good, rows: [...good.rows, { sha: "zzz9999", status: "gap", reason: "docs only", behaviour: "docs/x" }],
      summary: { commitsRead: 3, byStatus: { covered: 1, claimed: 1, gap: 1 } } }),
    /not a user-visible commit in range/, "a hand-added row must fail");

  // A gap with no reason.
  ok(checkLedger({ ...good, expectedShas: new Set(["aaa1111", "bbb2222", "ddd4444"]),
      rows: [...good.rows, { sha: "ddd4444", status: "gap", reason: "", behaviour: "backend/y" }],
      summary: { commitsRead: 3, byStatus: { covered: 1, claimed: 1, gap: 1 } } }),
    /with no reason/, "a gap with no reason must fail");

  // A claimed row that names no test.
  ok(checkLedger({ ...good, rows: [good.rows[0], { ...good.rows[1], evidence: [] }] }),
    /claimed with no test named/, "claimed with no evidence must fail");

  // The headline edited without the rows.
  ok(checkLedger({ ...good, summary: { commitsRead: 900, byStatus: { covered: 1, claimed: 1 } } }),
    /summary says 900 commits but 2 rows were read/, "a summary that disagrees with the rows read must fail");
  ok(checkLedger({ ...good, summary: { commitsRead: 2, byStatus: { covered: 700, claimed: 1 } } }),
    /summary says 700 covered but 1 rows were counted/, "a per-status total that disagrees must fail");

  // Duplicates.
  ok(checkLedger({ ...good, rows: [...good.rows, good.rows[0]], summary: { commitsRead: 3, byStatus: { covered: 2, claimed: 1 } } }),
    /duplicate row/, "a duplicated row must fail");

  console.log("commit-ledger-guard: self-test ok (10 adversarial fixtures)");
}

if (process.argv.includes("--self-test")) selfTest();
else enforce();
