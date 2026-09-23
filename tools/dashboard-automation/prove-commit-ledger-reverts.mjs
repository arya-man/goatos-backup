#!/usr/bin/env node

// prove-commit-ledger-reverts.mjs — turns `claimed` into `covered`, one commit
// at a time, by REVERTING the commit and watching a named test go red.
//
//   node tools/dashboard-automation/prove-commit-ledger-reverts.mjs --sample 25
//   node tools/dashboard-automation/prove-commit-ledger-reverts.mjs --sha abc1234
//   node tools/dashboard-automation/prove-commit-ledger-reverts.mjs --self-test
//
// WHY A SAMPLE AND NOT ALL OF THEM. `covered` means a check fails when the fix
// is reverted, and the only way to know that is to do it. At 2,665 rows that is
// not affordable in one pass, so this runs a sample and writes receipts for
// exactly what it proved. The rest stay `claimed`, which is the honest word for
// "a test exists and nobody has watched it bite".
//
// THE PROCEDURE, and each step exists because skipping it lets a proof pass
// against nothing:
//
//   1. check the commit out in a scratch worktree, run the package's tests.
//      They must PASS first. Without this baseline a test that was already
//      broken reads as "reverting the fix broke it".
//   2. restore only the NON-test files to their parent state. The test files
//      stay at the commit — reverting them too would just delete the check and
//      prove nothing.
//   3. run the same tests again. They must FAIL, and the receipt records WHICH
//      test failed by name, because "the package went red" does not say that
//      this commit's own check is what bit.
//
// A candidate that passes step 3 is not a silent skip: it is recorded as
// `disproved`, because a test shipped beside a fix that does not notice the fix
// being undone is a finding, not an absence.

import { execFileSync, execSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const LEDGER_ROWS = "tools/dashboard-automation/commit-ledger.jsonl";
const RECEIPTS = "tools/dashboard-automation/commit-ledger-receipts.json";

/** Go packages to run, derived from the test files the commit touched. */
export function goPackagesFor(files) {
  const pkgs = new Set();
  for (const f of files ?? []) {
    if (!/^backend\/.*_test\.go$/.test(f)) continue;
    pkgs.add(`./${dirname(f).replace(/^backend\//, "")}`);
  }
  return [...pkgs].sort();
}

/** Non-test Go files — the half that gets reverted. */
export function revertableFor(files) {
  return (files ?? []).filter((f) => /^backend\/.*\.go$/.test(f) && !/_test\.go$/.test(f));
}

/** The failing test names in `go test` output, read from what was printed. */
export function failedTestNames(output) {
  const names = new Set();
  for (const line of String(output).split("\n")) {
    const m = /^\s*---\s+FAIL:\s+([A-Za-z0-9_/]+)/.exec(line);
    if (m) names.add(m[1]);
  }
  return [...names];
}

function run(cmd, args, cwd) {
  try {
    return { ok: true, out: execFileSync(cmd, args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], timeout: 600000 }) };
  } catch (err) {
    return { ok: false, out: `${err.stdout ?? ""}${err.stderr ?? ""}` };
  }
}

function proveOne(worktree, row, files) {
  const pkgs = goPackagesFor(files);
  const revertable = revertableFor(files);
  if (!pkgs.length) return { verdict: "skipped", why: "no Go test file in this commit" };
  if (!revertable.length) return { verdict: "skipped", why: "nothing to revert — the commit changed only tests" };

  const co = run("git", ["checkout", "--detach", "--force", row.sha], worktree);
  if (!co.ok) return { verdict: "skipped", why: "could not check the commit out" };
  run("git", ["clean", "-fdq"], worktree);

  const before = run("go", ["test", "-count=1", ...pkgs], join(worktree, "backend"));
  if (!before.ok) {
    return { verdict: "skipped", why: "the package did not pass at this commit, so a later failure would prove nothing", detail: before.out.slice(-400) };
  }

  // Revert only the non-test half. A file the commit ADDED has no parent
  // version, so `git checkout <sha>^ -- <f>` fails for it; delete it instead.
  for (const f of revertable) {
    const r = run("git", ["checkout", `${row.sha}^`, "--", f], worktree);
    if (!r.ok) run("git", ["rm", "-fq", "--", f], worktree);
  }

  const after = run("go", ["test", "-count=1", ...pkgs], join(worktree, "backend"));
  run("git", ["checkout", "--force", "."], worktree);

  if (after.ok) {
    return { verdict: "disproved", why: "the tests still pass with this commit's code removed — they do not notice it being undone", packages: pkgs };
  }
  const names = failedTestNames(after.out);
  if (!names.length) {
    return { verdict: "inconclusive", why: "the package went red but no test name was printed (a build break, not a failing check)", packages: pkgs, detail: after.out.slice(-400) };
  }
  return { verdict: "proved", check: names[0], allFailing: names.slice(0, 6), packages: pkgs };
}

function main() {
  const args = process.argv.slice(2);
  if (args.includes("--self-test")) return selfTest();
  const sampleIdx = args.indexOf("--sample");
  const sample = sampleIdx !== -1 ? Number(args[sampleIdx + 1]) : 20;
  const shaIdx = args.indexOf("--sha");
  const only = shaIdx !== -1 ? args[shaIdx + 1] : null;

  const rowsPath = join(repoRoot, LEDGER_ROWS);
  if (!existsSync(rowsPath)) {
    console.error(`prove-commit-ledger-reverts: ${LEDGER_ROWS} is missing — run build-commit-ledger.mjs first`);
    process.exit(1);
  }
  const rows = readFileSync(rowsPath, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l));

  let candidates = rows.filter((r) => r.claimTier === "ships-own-test" && r.area === "backend");
  if (only) candidates = rows.filter((r) => r.sha.startsWith(only));
  // Deterministic order: newest first, so a re-run proves the same commits.
  candidates.sort((a, b) => (a.date < b.date ? 1 : a.date > b.date ? -1 : a.sha.localeCompare(b.sha)));
  candidates = candidates.slice(0, only ? candidates.length : sample);

  const worktree = mkdtempSync(join(tmpdir(), "goatos-revert-"));
  rmSync(worktree, { recursive: true, force: true });
  execSync(`git worktree add --detach -f ${JSON.stringify(worktree)} HEAD`, { cwd: repoRoot, stdio: "ignore" });

  const receipts = [];
  const rejected = [];
  let attempted = 0;
  try {
    for (const row of candidates) {
      attempted += 1;
      const files = run("git", ["show", "--name-only", "--format=", row.sha], repoRoot).out.split("\n").map((s) => s.trim()).filter(Boolean);
      const result = proveOne(worktree, row, files);
      const line = `${row.sha} ${result.verdict.padEnd(12)} ${result.check ?? result.why ?? ""}`;
      console.log(line);
      if (result.verdict === "proved") {
        receipts.push({
          sha: row.sha,
          check: result.check,
          allFailing: result.allFailing,
          packages: result.packages,
          method: "reverted the commit's non-test files at its own tree and re-ran the package tests",
          recordedAt: new Date().toISOString().slice(0, 10),
        });
      } else {
        rejected.push({ sha: row.sha, verdict: result.verdict, why: result.why, packages: result.packages ?? [] });
      }
    }
  } finally {
    execSync(`git worktree remove --force ${JSON.stringify(worktree)}`, { cwd: repoRoot, stdio: "ignore" });
  }

  // Merge with any receipts already stored; never drop one.
  const path = join(repoRoot, RECEIPTS);
  const prior = existsSync(path) ? JSON.parse(readFileSync(path, "utf8")) : { receipts: [], rejected: [] };
  const byS = new Map((prior.receipts ?? []).map((r) => [r.sha, r]));
  for (const r of receipts) byS.set(r.sha, r);
  const rejS = new Map((prior.rejected ?? []).map((r) => [r.sha, r]));
  for (const r of rejected) if (!byS.has(r.sha)) rejS.set(r.sha, r);

  const out = {
    _comment:
      "Revert receipts — the ONLY thing that makes a commit-ledger row `covered`. Each was produced by checking the commit out, confirming its package passed, restoring only its non-test files to the parent, and re-running the same tests until a NAMED test failed. `rejected` records candidates that did not prove: `disproved` means a test shipped beside a fix that does not notice the fix being undone, which is a finding rather than an absence.",
    attempted,
    proved: byS.size,
    rejectedCount: rejS.size,
    receipts: [...byS.values()].sort((a, b) => a.sha.localeCompare(b.sha)),
    rejected: [...rejS.values()].sort((a, b) => a.sha.localeCompare(b.sha)),
  };
  writeFileSync(path, `${JSON.stringify(out, null, 1)}\n`);
  console.log(`prove-commit-ledger-reverts: attempted ${attempted}, proved ${receipts.length} this run, ${byS.size} stored`);
}

function selfTest() {
  const assert = (cond, msg) => {
    if (!cond) {
      console.error(`prove-commit-ledger-reverts self-test FAILED: ${msg}`);
      process.exit(1);
    }
  };
  // A build break is not a failing check — the distinction the `inconclusive`
  // verdict exists for.
  assert(failedTestNames("ok  \tfoo\n").length === 0, "clean output must name no failing test");
  assert(failedTestNames("--- FAIL: TestThing (0.01s)\n").join() === "TestThing", "must read the failing test name");
  assert(failedTestNames("# backend/internal/x\n./a.go:3:2: undefined: y\nFAIL\n").length === 0,
    "a compile error prints no --- FAIL line and must not be mistaken for a failing check");
  assert(goPackagesFor(["backend/internal/a/b_test.go", "backend/internal/a/b.go"]).join() === "./internal/a",
    "package list comes from the test files");
  assert(goPackagesFor(["apps/admin-web/x.test.mjs"]).length === 0, "only Go packages here");
  assert(revertableFor(["backend/internal/a/b_test.go", "backend/internal/a/b.go"]).join() === "backend/internal/a/b.go",
    "the test half must never be reverted — that would delete the check instead of testing it");
  console.log("prove-commit-ledger-reverts: self-test ok");
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) main();
