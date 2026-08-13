#!/usr/bin/env node
// check-weighing-close-gate-guard.mjs
//
// THE CLOSE GATE IS UNCONDITIONAL (maintainer decision 2026-08-03, AGENTS.md).
// Weighing has exactly two verbs: CLOSE a task, or REOPEN one already closed. There is no
// third verb and no force/override/skip variant of close. A bucket cannot close while
// verification is pending; when it will not close, the answer is to RESOLVE the verification,
// never to add a path around the gate.
//
// WHY THIS FILE EXISTS: AGENTS.md has claimed "Machine-enforced by
// tools/agent-hooks/check-weighing-close-gate-guard.mjs" since 2026-08-03. The file did not
// exist. The gate was real in close.go and covered by tests, but nothing stopped a refactor
// from deleting it -- and every author who read that line believed a machine was watching.
// A documented guarantee with no implementation is worse than none: it buys the trust without
// doing the work. Five such claims were found on this branch; this is the one whose fix is a
// guard rather than a doc correction.
//
// WHAT IT CHECKS (and its blind spots -- stated here so the next author does not over-trust it
// the way this rule was over-trusted):
//   1. close-gate-removed      -- the close path must still reference ErrVerificationPending.
//   2. close-bypass-parameter  -- no force/override/skip/ignore-verification flag may appear on
//                                 a close request/param/DTO in weighing (Go, Kotlin, TS).
//   3. abandon-verb-returns    -- "abandon" is a deleted verb; it must not come back.
// BLIND SPOTS: it cannot tell whether the gate is reached at RUNTIME on every path (that is
// close.go's tests), and it matches on names, so a bypass called something inventive
// ("expedite", "fastPath") slips through. Extend the vocabulary when you meet one.

import { readFileSync, readdirSync, statSync, writeFileSync, mkdtempSync, rmSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";

const REPO = resolve(new URL("../..", import.meta.url).pathname);

const BYPASS_TOKENS = [
  "forceclose", "closeforce", "overrideclose", "closeoverride",
  "skipverification", "ignoreverification", "bypassverification",
  "skipverificationgate", "forcecloseverification",
];
const ABANDON_TOKENS = ["abandonbucket", "abandoncampaign", "abandonweighing", "weighingabandon"];

function walk(dir, out = []) {
  let entries;
  try { entries = readdirSync(dir); } catch { return out; }
  for (const e of entries) {
    if (e === "node_modules" || e === ".git" || e === "build" || e === ".next") continue;
    const p = join(dir, e);
    let st;
    try { st = statSync(p); } catch { continue; }
    if (st.isDirectory()) walk(p, out);
    else if (/\.(go|kt|ts|tsx)$/.test(e)) out.push(p);
  }
  return out;
}

function scan(roots) {
  const problems = [];
  const files = roots.flatMap((r) => walk(r));
  let sawGate = false;

  for (const file of files) {
    const rel = file.startsWith(REPO) ? file.slice(REPO.length + 1) : file;
    if (rel.includes("check-weighing-close-gate-guard")) continue; // this file names the tokens
    let text;
    try { text = readFileSync(file, "utf8"); } catch { continue; }
    const isWeighing = /weighing/i.test(rel);
    if (!isWeighing) continue;
    const isTest = /_test\.go$|Test\.kt$|\.test\.(ts|tsx)$/.test(rel);

    if (/ErrVerificationPending/.test(text)) sawGate = true;

    text.split("\n").forEach((line, i) => {
      const squashed = line.toLowerCase().replace(/[_\s-]/g, "");
      if (line.trimStart().startsWith("//")) return;
      for (const tok of BYPASS_TOKENS) {
        if (squashed.includes(tok)) {
          problems.push(`${rel}:${i + 1}: [close-bypass-parameter] '${tok}' -- the close gate is UNCONDITIONAL; resolve the verification instead of adding a path around it`);
          break;
        }
      }
      if (isTest) return;
      for (const tok of ABANDON_TOKENS) {
        if (squashed.includes(tok)) {
          problems.push(`${rel}:${i + 1}: [abandon-verb-returns] 'abandon' is a DELETED weighing verb; the vocabulary is close or reopen only`);
          break;
        }
      }
    });
  }
  return { problems, sawGate };
}

function selfTest() {
  const dir = mkdtempSync(join(tmpdir(), "close-gate-guard-"));
  const cases = [
    { name: "weighing/bad_force.go", body: "func Close(forceClose bool) {}\n", expect: "close-bypass-parameter" },
    { name: "weighing/bad_skip.go", body: "type Req struct{ SkipVerification bool }\n", expect: "close-bypass-parameter" },
    { name: "weighing/bad_abandon.go", body: "func AbandonBucket() {}\n", expect: "abandon-verb-returns" },
    { name: "weighing/good.go", body: "if pending { return ports.ErrVerificationPending }\n", expect: null },
    { name: "vaccination/other.go", body: "func forceClose() {}\n", expect: null }, // not weighing: out of scope
  ];
  let failed = 0;
  for (const c of cases) {
    const sub = join(dir, c.name.split("/")[0]);
    mkdirSync(sub, { recursive: true });
    const p = join(dir, c.name);
    writeFileSync(p, c.body);
    const { problems } = scan([sub]);
    const hit = problems.some((x) => c.expect && x.includes(c.expect));
    if (c.expect && !hit) { console.error(`SELF-TEST FAIL: ${c.name} should trip ${c.expect}, got: ${problems.join(", ") || "nothing"}`); failed++; }
    if (!c.expect && problems.length) { console.error(`SELF-TEST FAIL: ${c.name} should be clean, got: ${problems.join(", ")}`); failed++; }
    rmSync(p, { force: true });
  }
  rmSync(dir, { recursive: true, force: true });
  if (failed) { console.error(`weighing close-gate guard self-test: ${failed} failure(s)`); process.exit(1); }
  console.log("weighing close-gate guard self-test: OK");
}

if (process.argv.includes("--self-test")) {
  selfTest();
} else {
  const { problems, sawGate } = scan([join(REPO, "backend"), join(REPO, "apps")]);
  if (!sawGate) {
    problems.push("backend/internal/weighing: [close-gate-removed] no ErrVerificationPending reference found anywhere in weighing -- the unconditional close gate appears to have been deleted");
  }
  if (problems.length) {
    console.error("weighing close-gate guard FAILED:\n" + problems.join("\n"));
    console.error("\nWeighing has two verbs: close, or reopen. The close gate is unconditional.\nSee AGENTS.md and context/repo-audits/weighing-implementation-do-not-reopen-ledger.md -> D-5");
    process.exit(1);
  }
  console.log("weighing close-gate guard: OK");
}
