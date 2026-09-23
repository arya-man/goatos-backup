#!/usr/bin/env node

// check-guard-input-presence.mjs — closes the "a guard's input went missing and
// the guard reported ok" class in ONE place.
//
// Measured 2026-09-23 (mutation audit): 87 (guard, input) pairs across 29 guards
// behave like this. Each named input file was moved aside, one at a time, and
// the guard re-run:
//
//   $ mv backend/internal/weighing/domain/sopseed/weighing_session.json /tmp/
//   $ node tools/agent-hooks/check-weighing-sop-guard.mjs
//   weighing-sop-guard: ok            <- exit 0. It checked nothing.
//
// Same shape as the dashboard loader that read extra.lane2 while the rows lived
// at lanes.lane2.checks: 112 checks silently dropped, reported as one tidy
// parked line. A check that did not run must never render a verdict
// (CONTRACT.md §4) — so a missing input has to be a failure, not a shrug.
//
// Rather than rewrite 29 guards' file reads, this guard asserts the whole set at
// once: every file path hard-coded in a guard script must exist. If a guard
// names a path it no longer reads, the reference is stale and that is itself the
// bug — fix the guard, then regenerate.
//
// COST: it stats ~350 paths. No tree walk, no diff, no subprocess. Measured
// under 20 ms, which is why it sits with the sub-second whole-tree ratchets in
// run_common rather than behind a diff scope: a guard input can go missing
// because ANY commit renamed a file, not only one that touched tools/.
//
//   (default)      assert every declared input exists.
//   --self-test    adversarial fixtures against the pure checker.
//   --regenerate   re-derive tools/ci/guard-inputs.json from the guard sources.
//                  Shrinking that manifest is a weakening — check-guard-weakening.mjs
//                  requires an explicit ACK for it.

import { readFileSync, readdirSync, statSync, writeFileSync, existsSync } from "node:fs";
import { resolve, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const MANIFEST = "tools/ci/guard-inputs.json";
const GUARD_DIRS = ["tools/agent-hooks", "tools/ci"];

// A path literal inside a guard script: quoted, contains a slash, ends in a
// known extension. Deliberately narrow — a bare "foo.json" with no directory is
// usually a basename being composed at runtime, not a fixed input.
const PATH_LITERAL_RE =
  /["'`]((?:apps|backend|contracts|docs|tools|context|infra|deploy|fixtures|packages|analytics|\.agents|\.agent|\.github|scripts|load-tests|mock)\/[A-Za-z0-9_@./-]+\.(?:json|txt|md|ya?ml|sql|kt|kts|tsx?|jsx?|mjs|go|py|sh))["'`]/g;

// Repo-root files a guard can depend on by bare name. Without these, four of the
// 87 measured "input went missing and the guard said ok" pairs were outside the
// manifest — README.md, .agent/scope.json and the two lockfiles.
const ROOT_FILE_RE =
  /["'`](README\.md|AGENTS\.md|CLAUDE\.md|CODEX\.md|SKILLS\.md|Makefile|package\.json|package-lock\.json|pnpm-lock\.yaml|pnpm-workspace\.yaml|firebase\.json)["'`]/g;

export function extractPathLiterals(source) {
  const out = new Set();
  for (const m of String(source).matchAll(PATH_LITERAL_RE)) out.add(m[1]);
  for (const m of String(source).matchAll(ROOT_FILE_RE)) out.add(m[1]);
  return [...out];
}

// Pure checker: `exists` is injected so the self-test needs no filesystem.
export function findMissing(manifest, exists) {
  const missing = [];
  for (const g of manifest.guards || []) {
    for (const input of g.inputs || []) {
      if (!exists(input)) missing.push({ guard: g.guard, input });
    }
  }
  return missing;
}

function guardScripts() {
  const out = [];
  for (const dir of GUARD_DIRS) {
    const abs = join(repo, dir);
    if (!existsSync(abs)) continue;
    for (const f of readdirSync(abs)) {
      if (!/^check-[\w.-]+\.(mjs|sh)$/.test(f)) continue;
      if (/\.test\.sh$/.test(f)) continue;
      out.push(`${dir}/${f}`);
    }
  }
  return out.sort();
}

function fileExists(rel) {
  try {
    return statSync(join(repo, rel)).isFile();
  } catch {
    return false;
  }
}

function regenerate() {
  const guards = [];
  for (const script of guardScripts()) {
    // This file names example paths in its own header; it is not its own input.
    if (script.endsWith("check-guard-input-presence.mjs")) continue;
    const src = readFileSync(join(repo, script), "utf8");
    // Only declare inputs that exist TODAY. A literal that is already missing is
    // a pre-existing stale reference; baselining it here would be exactly the
    // "append your violation and go green" move this whole audit is about, so it
    // is reported instead.
    const all = extractPathLiterals(src);
    const inputs = all.filter((p) => fileExists(p));
    const stale = all.filter((p) => !fileExists(p));
    if (stale.length) {
      console.error(`  !! ${script} names ${stale.length} path(s) that do not exist: ${stale.join(", ")}`);
    }
    if (inputs.length) guards.push({ guard: script, inputs: inputs.sort() });
  }
  const manifest = {
    _comment:
      "Generated by tools/agent-hooks/check-guard-input-presence.mjs --regenerate. Every path here is hard-coded inside the named guard; if one goes missing that guard silently stops checking. Shrinking this file needs a GUARD-WEAKENING-ACK.",
    total: guards.reduce((n, g) => n + g.inputs.length, 0),
    guards,
  };
  writeFileSync(join(repo, MANIFEST), `${JSON.stringify(manifest, null, 1)}\n`);
  console.log(`guard-input-presence: regenerated ${MANIFEST} — ${guards.length} guards, ${manifest.total} inputs`);
  return 0;
}

function selfTest() {
  let failed = 0;
  const check = (name, cond) => {
    if (cond) console.log(`  ok   ${name}`);
    else {
      failed += 1;
      console.log(`  FAIL ${name}`);
    }
  };

  const manifest = {
    guards: [
      { guard: "tools/agent-hooks/check-weighing-sop-guard.mjs", inputs: ["backend/x/sopseed/weighing_session.json", "backend/x/rules.go"] },
      { guard: "tools/agent-hooks/check-other.mjs", inputs: ["docs/a.md"] },
    ],
  };
  // the planted violation: the input the guard depends on is gone
  const gone = new Set(["backend/x/sopseed/weighing_session.json"]);
  const missing = findMissing(manifest, (p) => !gone.has(p));
  check("a missing declared input is reported", missing.length === 1);
  check("it names the guard that went blind", missing[0]?.guard === "tools/agent-hooks/check-weighing-sop-guard.mjs");
  check("it names the input", missing[0]?.input === "backend/x/sopseed/weighing_session.json");
  check("nothing missing -> nothing reported", findMissing(manifest, () => true).length === 0);
  check("every input gone -> every one reported", findMissing(manifest, () => false).length === 3);
  check("an empty manifest reports nothing", findMissing({ guards: [] }, () => false).length === 0);

  // extraction
  const src = `const SEED = "backend/internal/weighing/domain/sopseed/weighing_session.json";\nreadFileSync("apps/admin-web/components/mesha-shell.tsx")\nconst name = "session.json";\n`;
  const lits = extractPathLiterals(src);
  check("extracts a rooted path literal", lits.includes("backend/internal/weighing/domain/sopseed/weighing_session.json"));
  check("extracts a readFileSync argument", lits.includes("apps/admin-web/components/mesha-shell.tsx"));
  check("ignores a bare basename", !lits.includes("session.json"));
  check("extracts a repo-root file named bare", extractPathLiterals('read("README.md")').includes("README.md"));

  // the manifest that ships must not be empty — an emptied manifest is a guard
  // that can no longer fire, which is the failure mode this whole file exists for.
  let shipped = null;
  try {
    shipped = JSON.parse(readFileSync(join(repo, MANIFEST), "utf8"));
  } catch {
    /* reported below */
  }
  check("the shipped manifest parses", !!shipped);
  check("the shipped manifest is not empty", (shipped?.guards || []).length > 10);

  console.log(failed === 0 ? "guard-input-presence self-test: ok" : `guard-input-presence self-test: ${failed} FAILED`);
  return failed === 0 ? 0 : 1;
}

function main() {
  if (process.argv.includes("--self-test")) return selfTest();
  if (process.argv.includes("--regenerate")) return regenerate();

  let manifest;
  try {
    manifest = JSON.parse(readFileSync(join(repo, MANIFEST), "utf8"));
  } catch (err) {
    // The manifest IS this guard's input. It does not get to park quietly either.
    console.error(`guard-input-presence: FAIL — cannot read ${MANIFEST}: ${err.message}`);
    console.error("  Regenerate it: make guard-input-presence-regenerate");
    return 1;
  }
  if (!Array.isArray(manifest.guards) || manifest.guards.length === 0) {
    console.error(`guard-input-presence: FAIL — ${MANIFEST} declares no guards. An empty manifest cannot fail.`);
    return 1;
  }
  const missing = findMissing(manifest, fileExists);
  if (!missing.length) {
    const total = manifest.guards.reduce((n, g) => n + g.inputs.length, 0);
    console.log(`guard-input-presence: ok — ${total} declared input(s) across ${manifest.guards.length} guard(s) present`);
    return 0;
  }
  console.error("guard-input-presence: FAIL — a guard names a file that is not there, so it is checking nothing:");
  for (const m of missing) console.error(`  - ${m.guard} -> ${m.input}`);
  console.error("  Restore the file, or fix the guard's reference and run: make guard-input-presence-regenerate");
  return 1;
}

if (import.meta.url === `file://${process.argv[1]}`) process.exit(main());
