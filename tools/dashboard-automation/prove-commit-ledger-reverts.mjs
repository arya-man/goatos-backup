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
import { cpSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
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

/**
 * Did the baseline run actually EXERCISE anything?
 *
 * THE DEFECT THIS EXISTS TO STOP, and it is the whole reason this file changed.
 * Postgres tests are opt-in (`pgtest.Enabled`); with the flag unset every test in
 * an `adapters/postgres` package SKIPS and `go test` still exits 0. The sweep read
 * that zero as "the package passed", reverted the fix, read the second zero as
 * "the tests still pass", and filed `disproved` — a public accusation that a
 * perfectly good test does not notice its own fix being undone. 32 of the 38
 * disproved rows in the stored receipts are that, not a real finding.
 *
 * A missing input must push toward SILENCE, never toward accusing correct work,
 * so a run with nothing but `[no tests to run]` / `--- SKIP` is `inconclusive`.
 */
export function ranRealTests(output) {
  // A SKIP is not an exercise. `--- SKIP:` prints for every test in an
  // opt-in-gated Postgres package, so counting it would reopen the exact hole
  // this function closes: the run looks populated and proves nothing. Only a
  // test that reached a verdict — PASS or FAIL — counts. Callers pass -v so
  // these lines are always printed.
  return /^\s*---\s+(PASS|FAIL):/m.test(String(output));
}

/**
 * The Go test functions this commit ADDED or CHANGED, read from the `+` side of
 * its own diff.
 *
 * WHY THE PROOF NARROWS TO THESE. The baseline step demands the package be green
 * before the revert, but a large package carries pre-existing reds that have
 * nothing to do with this commit — the weighing Postgres package shipped with 33
 * of them, named in the commit message. Judging the whole package there answers a
 * question nobody asked: the claim under test is "THIS commit's check bites when
 * THIS commit's code is undone", so the run is scoped to the checks the commit
 * brought with it. A commit that touched a test file but added no new Test
 * function falls back to the whole package, which is the old behaviour.
 */
export function addedTestNames(diff) {
  const names = new Set();
  for (const line of String(diff).split("\n")) {
    if (!line.startsWith("+")) continue;
    const m = /^\+\s*func\s+(Test[A-Za-z0-9_]*)\s*\(/.exec(line);
    if (m) names.add(m[1]);
  }
  return [...names].sort();
}

/** An anchored -run pattern, so TestFoo never drags in TestFooBar. */
export function runPatternFor(names) {
  return `^(${names.map((n) => n.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})$`;
}

function run(cmd, args, cwd, env) {
  try {
    return { ok: true, out: execFileSync(cmd, args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], timeout: 1800000, env: { ...process.env, ...(env ?? {}) } }) };
  } catch (err) {
    return { ok: false, out: `${err.stdout ?? ""}${err.stderr ?? ""}` };
  }
}

/**
 * Put TODAY's pgtest harness into the checked-out tree.
 *
 * GOATOS_PGTEST_ADMIN_DSN -- the sanctioned no-Docker path -- landed on 2026-08-31. A commit older
 * than that checks out a pgtest that knows only Docker, so on a machine without it SkipIfNoDocker
 * skips every Postgres test, the package goes green having run nothing, and the row is unprovable.
 * That is ~205 of the 795 backend rows: a third of the ledger unreachable for a reason that has
 * nothing to do with whether their tests are any good.
 *
 * WHY THIS IS NOT CHEATING, and where the line is. pgtest is the HARNESS, never the code under
 * test: it decides where the database comes from and applies the migrations found in the tree it
 * is running in -- the CHECKED-OUT commit's migrations, not today's. The commit's own test files
 * and its own production code are untouched. Overlaying anything else would be tampering; this
 * package is copied whole and nothing else is.
 *
 * If the old tests do not compile against today's harness the package fails to build, which this
 * file already reports as `inconclusive` rather than as a verdict. Silent on failure by design.
 */
function overlayCurrentHarness(worktree) {
  const from = join(repoRoot, "backend/internal/platform/pgtest");
  const to = join(worktree, "backend/internal/platform/pgtest");
  if (!existsSync(from) || !existsSync(to)) return;
  try {
    cpSync(from, to, { recursive: true });
  } catch {
    // An older tree without the package: leave it alone and let the run report what it finds.
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
  overlayCurrentHarness(worktree);

  const diff = run("git", ["show", "--format=", "--unified=0", "--", ...files.filter((f) => /^backend\/.*_test\.go$/.test(f))], worktree).out;
  const own = addedTestNames(diff);
  const scope = own.length ? ["-run", runPatternFor(own)] : [];

  const before = run("go", ["test", "-count=1", "-v", ...scope, ...pkgs], join(worktree, "backend"));
  if (!before.ok) {
    return { verdict: "skipped", why: "the package did not pass at this commit, so a later failure would prove nothing", detail: before.out.slice(-400) };
  }
  if (!ranRealTests(before.out)) {
    // Green because nothing ran. Reverting the fix cannot make a skip go red, so
    // the only verdicts reachable from here are false ones.
    return { verdict: "inconclusive", why: "every test in the package skipped (Postgres tests are opt-in: set GOATOS_RUN_POSTGRES_TESTS=1 and GOATOS_PGTEST_ADMIN_DSN), so a green run proves nothing", packages: pkgs };
  }

  // Revert only the non-test half. A file the commit ADDED has no parent
  // version, so `git checkout <sha>^ -- <f>` fails for it; delete it instead.
  for (const f of revertable) {
    const r = run("git", ["checkout", `${row.sha}^`, "--", f], worktree);
    if (!r.ok) run("git", ["rm", "-fq", "--", f], worktree);
  }

  const after = run("go", ["test", "-count=1", "-v", ...scope, ...pkgs], join(worktree, "backend"));
  run("git", ["checkout", "--force", "."], worktree);

  if (after.ok) {
    return { verdict: "disproved", why: "the tests still pass with this commit's code removed — they do not notice it being undone", packages: pkgs };
  }
  const names = failedTestNames(after.out);
  if (!names.length) {
    return { verdict: "inconclusive", why: "the package went red but no test name was printed (a build break, not a failing check)", packages: pkgs, detail: after.out.slice(-400) };
  }
  return { verdict: "proved", check: names[0], allFailing: names.slice(0, 6), packages: pkgs, scopedTo: own };
}

function main() {
  const args = process.argv.slice(2);
  if (args.includes("--self-test")) return selfTest();
  const sampleIdx = args.indexOf("--sample");
  const sample = sampleIdx !== -1 ? Number(args[sampleIdx + 1]) : 20;
  const shaIdx = args.indexOf("--sha");
  const only = shaIdx !== -1 ? args[shaIdx + 1] : null;
  // Batching + parallelism. 795 backend rows do not fit in one serial pass, so several workers
  // take disjoint --offset/--sample windows of the SAME deterministic ordering and each writes its
  // own --receipts file; the files merge afterwards because every receipt is keyed by sha.
  const offIdx = args.indexOf("--offset");
  const offset = offIdx !== -1 ? Number(args[offIdx + 1]) : 0;
  // Every backend `claimed` row, not only the ships-own-test tier. A commit whose evidence is a
  // SIBLING test still deserves a verdict; it will come back `skipped` naming why, which is the
  // honest word for unprovable rather than a row nobody looked at.
  const allTiers = args.includes("--all-tiers");
  // An explicit worklist, so a re-run does not spend an hour re-proving rows that already carry a
  // verdict. One sha per line; the ordering below still applies within it.
  const listIdx = args.indexOf("--shas-file");
  const shaList = listIdx !== -1
    ? new Set(readFileSync(args[listIdx + 1], "utf8").split("\n").map((l) => l.trim()).filter(Boolean))
    : null;
  const recIdx = args.indexOf("--receipts");
  const receiptsPath = recIdx !== -1 ? args[recIdx + 1] : RECEIPTS;

  const rowsPath = join(repoRoot, LEDGER_ROWS);
  if (!existsSync(rowsPath)) {
    console.error(`prove-commit-ledger-reverts: ${LEDGER_ROWS} is missing — run build-commit-ledger.mjs first`);
    process.exit(1);
  }
  const rows = readFileSync(rowsPath, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l));

  let candidates = rows.filter((r) => r.area === "backend" && r.status === "claimed" && (allTiers || r.claimTier === "ships-own-test"));
  if (only) candidates = rows.filter((r) => r.sha.startsWith(only));
  // Deterministic order: newest first, so a re-run proves the same commits.
  candidates.sort((a, b) => (a.date < b.date ? 1 : a.date > b.date ? -1 : a.sha.localeCompare(b.sha)));
  if (shaList) candidates = candidates.filter((r) => shaList.has(r.sha));
  candidates = only ? candidates : candidates.slice(offset, offset + sample);

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
  const path = join(repoRoot, receiptsPath);
  const prior = existsSync(path) ? JSON.parse(readFileSync(path, "utf8")) : { receipts: [], rejected: [] };
  const byS = new Map((prior.receipts ?? []).map((r) => [r.sha, r]));
  for (const r of receipts) byS.set(r.sha, r);
  const rejS = new Map((prior.rejected ?? []).map((r) => [r.sha, r]));
  for (const r of rejected) if (!byS.has(r.sha)) rejS.set(r.sha, r);
  // A receipt RETIRES the accusation. `disproved` is published as a finding
  // against a named commit's test, so leaving a stale one beside a fresh proof
  // keeps accusing work that has since been shown to bite — which is the worse
  // of the two ways to be wrong here. Anything now proved leaves `rejected`.
  for (const sha of byS.keys()) rejS.delete(sha);

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
  // The silent-skip trap: a Postgres package with the opt-in flag unset exits 0
  // having run nothing. Reading that as a pass is how 32 good tests were filed
  // as `disproved`.
  assert(ranRealTests("ok  \tgithub.com/x/y\t1.4s [no tests to run]\n") === false,
    "a package where every test skipped must not count as a baseline pass");
  assert(ranRealTests("=== RUN   TestA\n--- SKIP: TestA (0.00s)\nPASS\nok\tx\t0.1s\n") === false,
    "a package whose every test SKIPPED exercised nothing — counting it is the same hole one line over");
  assert(ranRealTests("--- SKIP: TestA (0.00s)\n--- PASS: TestB (0.01s)\n") === true,
    "one real verdict among skips is still a real baseline");
  assert(addedTestNames("+func TestNewThing(t *testing.T) {\n-func TestGone(t *testing.T) {\n").join() === "TestNewThing",
    "only the added side of the diff names a check this commit brought");
  assert(runPatternFor(["TestFoo"]) === "^(TestFoo)$",
    "the -run pattern is anchored so TestFoo does not drag TestFooBar in with it");
  assert(ranRealTests("=== RUN   TestA\n--- PASS: TestA (0.01s)\nok\tx\t0.1s\n") === true,
    "a passing named test is a real baseline");
  console.log("prove-commit-ledger-reverts: self-test ok");
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) main();
