#!/usr/bin/env node

// check-interactive-surfaces.mjs — the registered guard for admin-web's interactive surfaces:
// every edit form, inline editor, row action and modal carries a decision, and every decision
// claimed as coverage carries an assertion the gate can re-derive and that goes red on a blank
// screen.
//
// The implementation lives with the code it reads:
//
//   apps/admin-web/scripts/check-interactive-surfaces.mjs      the gate
//   apps/admin-web/scripts/lib/interactive-surfaces.mjs        scan, provenance, coverage
//   apps/admin-web/scripts/interactive-surface-ledger.json     one decision per surface
//
// This file exists so the guard is REGISTERED rather than merely runnable. Two reasons, both
// measured on 2026-09-23:
//
//   1. `npm run check:interactive-surfaces` is not enforcement. A guard reaches CI through
//      tools/ci/guardrail-manifest.json + a Make target + tools/ci/run-local-ci.sh.
//   2. check-guard-input-presence.mjs only scans tools/agent-hooks and tools/ci for the paths a
//      guard reads. A guard whose inputs are named somewhere else declares NO inputs, and 42 of
//      138 guards were in exactly that state — so the presence guard could not tell that their
//      input had gone missing. Naming the three paths below, as literals, in this directory, is
//      what puts this guard inside that protection.
//
// A check that did not run must not report a pass, so a missing input fails closed here too:
// the ledger and the app tree are stat-ed before the gate runs, and their absence is an error,
// never "0/148, all clean".
//
//   (default)      run the gate over the real tree.
//   --self-test    the gate's own adversarial fixtures, AFTER the real run (handover §8: a
//                  self-test that short-circuits the real path proves nothing about it).

import { existsSync, statSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

const GATE = "apps/admin-web/scripts/check-interactive-surfaces.mjs";
const LEDGER = "apps/admin-web/scripts/interactive-surface-ledger.json";
const LIB = "apps/admin-web/scripts/lib/interactive-surfaces.mjs";
const SCANNED_ROOTS = ["apps/admin-web/features", "apps/admin-web/components", "apps/admin-web/app"];

/** Pure: which declared inputs are missing. Fails closed rather than reporting a clean scan. */
export function missingInputs(paths, exists = (p) => existsSync(join(repo, p))) {
  return paths.filter((p) => !exists(p));
}

function main() {
  const required = [GATE, LIB, LEDGER, ...SCANNED_ROOTS];
  const missing = missingInputs(required);
  if (missing.length) {
    console.error("interactive-surfaces guard cannot run — its input is missing:");
    for (const path of missing) console.error(`- ${path}`);
    console.error("A guard whose input is gone has checked nothing; it must not report a pass.");
    process.exit(1);
  }
  // An empty app tree would make the gate report a cheerful 0/0. The gate's own self-test asserts
  // the scan found something, which is why --self-test is passed through rather than dropped.
  for (const root of SCANNED_ROOTS) {
    if (!statSync(join(repo, root)).isDirectory()) {
      console.error(`interactive-surfaces guard: ${root} is not a directory`);
      process.exit(1);
    }
  }
  const args = process.argv.slice(2).filter((a) => a === "--self-test");
  const run = spawnSync(process.execPath, [join(repo, GATE), ...args], { stdio: "inherit" });
  process.exit(run.status ?? 1);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
