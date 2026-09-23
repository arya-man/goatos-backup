#!/usr/bin/env node

// prove-guard-selftest-reverts.mjs — the revert-prover for GUARD SCRIPTS.
//
//   node tools/dashboard-automation/prove-guard-selftest-reverts.mjs --all
//   node tools/dashboard-automation/prove-guard-selftest-reverts.mjs <sha>:<path> ...
//   node tools/dashboard-automation/prove-guard-selftest-reverts.mjs --self-test
//
// WHY THIS EXISTS. prove-commit-ledger-reverts.mjs matches `^backend/.*_test\.go$`
// and nothing else, so it can only ever prove Go. A guard script keeps its
// adversarial fixtures in a `--self-test` INSIDE THE SAME FILE as the logic they
// exercise, which the ledger's gap detector reads as "no test file, therefore no
// test". 25 rows on this branch were marked `gap` while shipping a fixture that
// does bite. Those are misclassified, not uncovered, and the only way to know
// which is which is to do the revert.
//
// THE PROCEDURE, the same three steps as the Go prover, each of which exists
// because skipping it lets a proof pass against nothing:
//
//   1. check the commit out in a scratch WORKTREE and run its self-test. It must
//      PASS first. Running the guard file alone in a temp dir looked simpler and
//      was wrong: several of these guards read fixture files next to themselves,
//      so an isolated run dies in node:fs and reads as "already red at the
//      commit" -- a missing input pushing the answer toward silence, which
//      quietly shrinks the denominator instead of failing loudly.
//   2. rebuild the tree as PARENT logic + COMMIT selfTest(): the fix reverted,
//      the fixture kept. Reverting the fixture too would just delete the check.
//      EVERY other file the commit touched is reverted as well, because several
//      guards keep their rules in a sibling JSON -- reverting only the .mjs
//      leaves the fix in place and the fixture passes, which reads as "the
//      fixture does not notice its own fix" and accuses a sound test.
//   3. the hybrid must self-test RED. Green is recorded as `disproved`, because a
//      fixture shipped beside a fix that does not notice the fix being undone is
//      a finding, not an absence.
//
// VERDICTS and what each one means for a ledger row:
//   proved       the fixture bites  -> the row is COVERED, not a gap
//   disproved    the fixture does not notice  -> a real finding
//   no-fixture   logic changed, no fixture shipped  -> a real gap
//   no-baseline  the guard did not even run at that commit  -> unprovable by revert
//   skipped      new file, or no selfTest() to graft  -> out of this prover's reach

import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const repo = process.env.REPO ?? process.cwd();
const git = (...a) => execFileSync("git", ["-C", repo, ...a], { encoding: "utf8", maxBuffer: 64 << 20 });

/** The selfTest function: from its `function selfTest(` line to the next line that
 *  is a closing brace at column 0. Brace COUNTING is wrong here -- these guards are
 *  full of regex literals containing `{`, which a naive counter reads as a block and
 *  never closes, so it silently reports "no selfTest()" on the files that need it
 *  most. Column-0 is the file style and, unlike counting, cannot be fooled by a
 *  string or a pattern. */
function selfTestBlock(src) {
  const lines = src.split("\n");
  const first = lines.findIndex((l) => /^function selfTest\s*\(/.test(l));
  if (first < 0) return null;
  const last = lines.findIndex((l, i) => i > first && l === "}");
  if (last < 0) return null;
  const start = lines.slice(0, first).join("\n").length + (first ? 1 : 0);
  const text = lines.slice(first, last + 1).join("\n");
  return { start, end: start + text.length, text };
}

/** Run a guard's self-test IN A WORKTREE AT THE COMMIT. Running the file alone in
 *  /tmp looked simpler and was wrong: several of these guards read fixture files
 *  next to themselves, so an isolated run dies in node:fs and reads as "already
 *  red at the commit" -- a missing input that pushes the answer toward silence,
 *  which is the direction that quietly shrinks the denominator. */
function run(worktree, relPath) {
  try {
    execFileSync(process.execPath, [relPath, "--self-test"], {
      cwd: worktree, encoding: "utf8", stdio: "pipe",
    });
    return { green: true, out: "" };
  } catch (e) {
    return { green: false, out: `${e.stdout ?? ""}${e.stderr ?? ""}`.trim() };
  }
}

export function prove(sha, path) {
  const at = git("show", `${sha}:${path}`);
  const parent = (() => { try { return git("show", `${sha}^:${path}`); } catch { return null; } })();
  if (parent === null) return { sha, path, verdict: "skipped", why: "file is new in this commit; nothing to revert to" };

  const atBlock = selfTestBlock(at);
  const parentBlock = selfTestBlock(parent);
  if (!atBlock) return { sha, path, verdict: "skipped", why: "no selfTest() in the guard at this commit" };
  if (!parentBlock) return { sha, path, verdict: "skipped", why: "the parent had no selfTest() to graft onto" };
  if (atBlock.text === parentBlock.text) {
    return { sha, path, verdict: "no-fixture", why: "the commit changed logic but shipped no new fixture" };
  }

  const wt = mkdtempSync(join(tmpdir(), "guard-wt-"));
  try {
    execFileSync("git", ["-C", repo, "worktree", "add", "--detach", "-f", wt, sha], { stdio: "pipe" });

    const baseline = run(wt, path);
    if (!baseline.green) {
      return { sha, path, verdict: "no-baseline", why: `self-test is already red at the commit: ${baseline.out.split("\n").filter(Boolean).slice(-1)[0] ?? ""}` };
    }

    // Revert EVERY other file the commit touched, not just the guard script. Several of
    // these guards keep their rules in a sibling JSON (component-paths.json), so reverting
    // the .mjs alone leaves the fix in place and the fixture passes -- which reads as
    // "the fixture does not notice its own fix", accusing a sound test. Deleting a file the
    // commit created is part of the revert; a file it deleted is restored.
    for (const other of git("show", "--name-only", "--format=", sha).split("\n").map((l) => l.trim()).filter(Boolean)) {
      if (other === path) continue;
      const abs = join(wt, other);
      try {
        writeFileSync(abs, git("show", `${sha}^:${other}`));
      } catch {
        rmSync(abs, { force: true });
      }
    }
    writeFileSync(join(wt, path), parent.slice(0, parentBlock.start) + atBlock.text + parent.slice(parentBlock.end));
    const mutant = run(wt, path);
    return mutant.green
      ? { sha, path, verdict: "disproved", why: "parent logic + this commit's fixture still passes -- the fixture does not notice the fix being undone" }
      : { sha, path, verdict: "proved", why: mutant.out.split("\n").filter(Boolean).slice(-1)[0] ?? "self-test went red" };
  } finally {
    try { execFileSync("git", ["-C", repo, "worktree", "remove", "--force", wt], { stdio: "pipe" }); } catch { /* best effort */ }
    rmSync(wt, { recursive: true, force: true });
  }
}


/** The prover's own self-test. Each case is a trap this prover actually fell into. */
function selfTest() {
  const failures = [];
  const check = (name, cond) => { if (!cond) failures.push(name); };

  // Brace COUNTING is wrong for these files: they are full of regex literals
  // containing `{`, which a counter reads as an unclosed block, so it reports
  // "no selfTest()" on exactly the guards that have one. This is the shape that
  // silently skipped four rows before it was found.
  const regexTrap = [
    "function selfTest() {",
    "  // an UNBALANCED brace inside a regex literal, which is what these guards are full of:",
    "  const ifHead = /\\bif\\s*\\([^)]*\\)\\s*\\{?/;",
    "  console.log(ifHead.source);",
    "}",
    "",
    "selfTest();",
  ].join("\n");
  const block = selfTestBlock(regexTrap);
  check("selfTestBlock finds the block past regex braces", block !== null);
  check("selfTestBlock starts at the function", block?.text.startsWith("function selfTest()"));
  check("selfTestBlock ends at the column-0 brace", block?.text.trimEnd().endsWith("}"));
  check(
    "selfTestBlock keeps the whole body",
    Boolean(block?.text.includes("console.log(ifHead.source);")),
  );
  check("selfTestBlock slices back out of the source", Boolean(block) && regexTrap.slice(block.start, block.end) === block.text);

  check("a file with no selfTest yields null", selfTestBlock("const a = 1;\n") === null);

  console.log(
    failures.length === 0
      ? "prove-guard-selftest-reverts self-test: ok"
      : `prove-guard-selftest-reverts self-test: FAIL\n  - ${failures.join("\n  - ")}`,
  );
  process.exit(failures.length === 0 ? 0 : 1);
}

if (process.argv[1]?.endsWith("prove-guard-selftest-reverts.mjs")) {
  if (process.argv.includes("--self-test")) selfTest();
  const pairs = process.argv.slice(2).filter((a) => !a.startsWith("--"));
  const results = [];
  for (const pair of pairs) {
    const [sha, path] = pair.split(":");
    let r;
    try { r = prove(sha, path); } catch (e) { r = { sha, path, verdict: "error", why: String(e.message).slice(0, 200) }; }
    results.push(r);
    console.log(`${r.verdict.padEnd(11)} ${r.sha.slice(0, 9)}  ${r.why}`);
  }
  const n = (v) => results.filter((r) => r.verdict === v).length;
  console.log(
    `\nproved ${n("proved")} · disproved ${n("disproved")} · no-fixture ${n("no-fixture")} ` +
      `· no-baseline ${n("no-baseline")} · skipped ${n("skipped")} · error ${n("error")}`,
  );
  const out = process.argv.find((a) => a.startsWith("--receipts="));
  if (out) {
    writeFileSync(
      join(repo, out.slice("--receipts=".length)),
      `${JSON.stringify({ generatedFrom: "prove-guard-selftest-reverts.mjs", results }, null, 2)}\n`,
    );
  }
}
