#!/usr/bin/env node
// prove-shell-test-reverts.mjs — shell sibling of prove-guard-selftest-reverts.mjs: for a commit that ships a
// `*.test.sh` beside the script it tests, revert every NON-test file the commit
// touched, keep the test files at the commit, and require the test to go red.
// Same three steps, same reasons.
//
//   node tools/dashboard-automation/prove-shell-test-reverts.mjs <sha>:<test path> ...
//
// TWO RULES HERE EXIST BECAUSE THE FIRST RUN WITHOUT THEM PRODUCED FIVE FALSE
// FINDINGS, every one of them an accusation against a sound test:
//
//   covers()          a test is only run against the script it NAMES. Reverting one
//                     guard and running ANOTHER guard's test passes, and that reads
//                     as "the test does not notice its own fix". All five `disproved`
//                     verdicts on the first pass were this, not a defect.
//   carriesBehaviour  a commit whose only non-test change is markdown has no fix to
//                     revert. Calling that `disproved` blames the test for the
//                     classifier.
//
// VERDICTS: proved · disproved (a real finding) · unpaired (this test is for another
// script; says nothing about the row) · test-only (the commit IS the test) ·
// no-baseline (red at the commit here, often an environment-scoped test).
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const repo = process.env.REPO ?? process.cwd();
const git = (...a) => execFileSync("git", ["-C", repo, ...a], { encoding: "utf8", maxBuffer: 64 << 20 });
const isTest = (f) => f.endsWith(".test.sh") || f.endsWith("_test.sh");

function run(wt, rel) {
  try {
    execFileSync("bash", [rel], { cwd: wt, encoding: "utf8", stdio: "pipe", timeout: 120000 });
    return { green: true, out: "" };
  } catch (e) {
    return { green: false, out: `${e.stdout ?? ""}${e.stderr ?? ""}`.trim() };
  }
}

/** Does this test file test that script? `check-X.test.sh` covers `check-X.sh` /
 *  `check-X.mjs` and nothing else. Without this the prover happily reverts one guard
 *  and runs ANOTHER guard's test, which passes -- and that reads as "the test does not
 *  notice its own fix", a false accusation against five sound tests on the first run. */
function covers(testPath, file) {
  const stem = testPath.replace(/\.(test\.sh|test\.mjs)$/, "").replace(/_test\.sh$/, "");
  // `check-` is a prefix some of these carry and their subject does not:
  // check-gradle-worktree-lock.test.sh does test gradle-worktree-lock.sh.
  const norm = (n) => n.replace(/^check-/, "");
  const base = norm(stem.split("/").pop());
  const other = norm(file.split("/").pop().replace(/\.(sh|mjs|cjs)$/, ""));
  return base === other;
}

/** Docs carry no behaviour: a commit whose only non-test change is markdown has no fix
 *  to revert, and calling that "disproved" blames the test for the classifier. */
const carriesBehaviour = (f) => !/\.(md|mdc|txt)$/.test(f);

function prove(sha, testPath) {
  const files = git("show", "--name-only", "--format=", sha).split("\n").map((l) => l.trim()).filter(Boolean);
  const nonTest = files.filter((f) => !isTest(f));
  const behavioural = nonTest.filter(carriesBehaviour);
  if (behavioural.length === 0) {
    return { sha, testPath, verdict: "test-only", why: `no behaviour changed (${nonTest.length} non-test file(s), all docs); there is no fix to revert` };
  }
  const paired = behavioural.filter((f) => covers(testPath, f));
  if (paired.length === 0) {
    return { sha, testPath, verdict: "unpaired", why: `this test covers a different script; the commit changed ${behavioural.join(", ")}` };
  }

  const wt = mkdtempSync(join(tmpdir(), "sh-prove-"));
  try {
    execFileSync("git", ["-C", repo, "worktree", "add", "--detach", "-f", wt, sha], { stdio: "pipe" });
    if (!existsSync(join(wt, testPath))) return { sha, testPath, verdict: "skipped", why: "test file absent at the commit" };

    const baseline = run(wt, testPath);
    if (!baseline.green) return { sha, testPath, verdict: "no-baseline", why: `already red at the commit: ${baseline.out.split("\n").filter(Boolean).slice(-1)[0] ?? ""}`.slice(0, 180) };

    let reverted = 0;
    for (const f of paired) {
      try { writeFileSync(join(wt, f), git("show", `${sha}^:${f}`)); reverted += 1; }
      catch { rmSync(join(wt, f), { force: true }); reverted += 1; }
    }
    const mutant = run(wt, testPath);
    return mutant.green
      ? { sha, testPath, verdict: "disproved", why: `reverted ${reverted} file(s) and the test still passes` }
      : { sha, testPath, verdict: "proved", why: mutant.out.split("\n").filter(Boolean).slice(-1)[0]?.slice(0, 160) ?? "went red" };
  } finally {
    try { execFileSync("git", ["-C", repo, "worktree", "remove", "--force", wt], { stdio: "pipe" }); } catch { /* best effort */ }
    rmSync(wt, { recursive: true, force: true });
  }
}

const results = [];
for (const pair of process.argv.slice(2)) {
  const i = pair.indexOf(":");
  let r;
  try { r = prove(pair.slice(0, i), pair.slice(i + 1)); }
  catch (e) { r = { sha: pair.slice(0, i), testPath: pair.slice(i + 1), verdict: "error", why: String(e.message).slice(0, 160) }; }
  results.push(r);
  console.log(`${r.verdict.padEnd(11)} ${r.sha.slice(0, 9)}  ${r.testPath.split("/").pop()}  ${r.why}`);
}
const n = (v) => results.filter((r) => r.verdict === v).length;
console.log(`\nproved ${n("proved")} · disproved ${n("disproved")} · unpaired ${n("unpaired")} · test-only ${n("test-only")} · no-baseline ${n("no-baseline")} · skipped ${n("skipped")} · error ${n("error")}`);
