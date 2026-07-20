#!/usr/bin/env node
// check-guardrail-registration.mjs — meta-guard: keeps tools/ci/guardrail-manifest.json
// in sync with the guard scripts on disk, the Makefile `guardrails:` target, and
// tools/ci/run-local-ci.sh. Governance invariant (docs/runbooks/local-ci.md): a guard
// that exists but is unregistered, has no self-test, or is required-but-unwired is a
// silent hole — CI must fail closed on it.
//
// Fails when:
//   (1) a check-*.mjs guard script exists under tools/agent-hooks/ or tools/ci/ but is
//       absent from manifest.guards[].script;
//   (2) a manifest guard declares neither `selfTest` nor `selfTestExemptReason`;
//   (3) a manifest guard with requiredInCI:true has a `makeTarget` that is absent from the
//       Makefile `guardrails:` target body;
//   (4) a manifest guard with requiredInCI:true has a `ciStep` absent from tools/ci/run-local-ci.sh.
//
// Deterministic, offline: pure text/JSON parsing. No network, no build.

import { readFileSync, readdirSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const MANIFEST = "tools/ci/guardrail-manifest.json";
const STANDARD_CI_FUNCTIONS = [
  "run_common",
  "run_backend",
  "run_query_plans",
  "run_admin_web",
  "run_android_guards",
  "run_android",
];

// Extract top-level Bash function bodies by their column-zero closing brace. The local-CI runner
// follows this shape deliberately; parsing only these standard jobs prevents a guard mentioned in
// comments or in the optional compatibility `run_guardrails` helper from posing as PR enforcement.
function extractShellFunction(text, name) {
  const lines = text.split("\n");
  const start = lines.findIndex((line) => line.trim() === `${name}() {`);
  if (start < 0) return "";
  const body = [];
  for (let i = start + 1; i < lines.length; i += 1) {
    if (lines[i] === "}") return body.join("\n");
    body.push(lines[i]);
  }
  return "";
}

function standardCiJobText(runLocalCiText) {
  return STANDARD_CI_FUNCTIONS.map((name) => extractShellFunction(runLocalCiText, name)).join("\n");
}

// Pure validator — all inputs injected so the self-test can feed adversarial fixtures.
export function validate({ manifest, mjsScripts, makeGuardrailsBody, runLocalCiText }) {
  const problems = [];
  const guards = manifest.guards || [];
  const registered = new Set(guards.map((g) => g.script));
  const standardJobs = standardCiJobText(runLocalCiText);

  // (1) every enumerated check-*.mjs must be registered.
  for (const script of mjsScripts) {
    if (!registered.has(script)) {
      problems.push(`unregistered guard script: ${script} (add it to ${MANIFEST})`);
    }
  }

  for (const g of guards) {
    const id = g.id || g.script || "<unknown>";

    // (2) self-test presence (or explicit exemption).
    const hasSelfTest = typeof g.selfTest === "string" && g.selfTest.trim().length > 0;
    const hasExempt = typeof g.selfTestExemptReason === "string" && g.selfTestExemptReason.trim().length > 0;
    if (!hasSelfTest && !hasExempt) {
      problems.push(`guard ${id}: no selfTest and no selfTestExemptReason`);
    }

    if (g.requiredInCI === true) {
      // (3) required guard's makeTarget must be wired into `make guardrails`.
      if (g.makeTarget && !makeGuardrailsBody.includes(g.makeTarget)) {
        problems.push(`required guard ${id}: makeTarget "${g.makeTarget}" missing from Makefile guardrails: target`);
      }
      // (4) required guard's ciStep must appear in a standard local-CI job. A mention only
      // in run_guardrails (the compatibility mode) is not enforcement over pull requests.
      const ciStep = g.ciStep || g.makeTarget;
      if (!ciStep || !standardJobs.includes(ciStep)) {
        problems.push(`required guard ${id}: ciStep "${ciStep}" missing from a standard CI job in tools/ci/run-local-ci.sh`);
      }
    }
  }
  return problems;
}

function enumerateMjsGuards() {
  const dirs = ["tools/agent-hooks", "tools/ci"];
  const found = [];
  for (const d of dirs) {
    const abs = resolve(repo, d);
    if (!existsSync(abs)) continue;
    for (const name of readdirSync(abs)) {
      if (name.startsWith("check-") && name.endsWith(".mjs")) found.push(`${d}/${name}`);
    }
  }
  return found.sort();
}

function extractGuardrailsBody(makefileText) {
  // Grab the recipe lines of the `guardrails:` target (until the next target/blank-at-col-0).
  const lines = makefileText.split("\n");
  const start = lines.findIndex((l) => /^guardrails:/.test(l));
  if (start < 0) return "";
  const body = [];
  for (let i = start + 1; i < lines.length; i++) {
    const l = lines[i];
    if (l.startsWith("\t")) body.push(l);
    else if (l.trim() === "") continue;
    else break; // next target
  }
  return body.join("\n");
}

function selfTest() {
  const goodManifest = {
    guards: [
      { id: "a", script: "tools/ci/check-a.mjs", makeTarget: "a-guard", selfTest: "x --self-test", requiredInCI: true, ciStep: "a-guard" },
      { id: "b", script: "tools/agent-hooks/check-b.mjs", makeTarget: null, selfTestExemptReason: "driven e2e", requiredInCI: false, ciStep: "check-b.mjs" },
    ],
  };
  const mjs = ["tools/ci/check-a.mjs", "tools/agent-hooks/check-b.mjs"];
  const makeBody = "\t$(MAKE) a-guard\n";
  const ci = "run_common() {\n  step a-guard\n}\n";

  const clean = validate({ manifest: goodManifest, mjsScripts: mjs, makeGuardrailsBody: makeBody, runLocalCiText: ci });
  if (clean.length !== 0) throw new Error(`self-test: expected clean, got ${JSON.stringify(clean)}`);

  // orphan script present on disk but not in manifest -> detected.
  const orphan = validate({ manifest: goodManifest, mjsScripts: [...mjs, "tools/ci/check-orphan.mjs"], makeGuardrailsBody: makeBody, runLocalCiText: ci });
  if (!orphan.some((p) => p.includes("unregistered guard script: tools/ci/check-orphan.mjs"))) {
    throw new Error("self-test: orphan script not detected");
  }

  // guard with no self-test and no exemption -> detected.
  const noSelf = { guards: [{ id: "c", script: "tools/ci/check-c.mjs", makeTarget: null, requiredInCI: false, ciStep: "check-c.mjs" }] };
  const noSelfProblems = validate({ manifest: noSelf, mjsScripts: ["tools/ci/check-c.mjs"], makeGuardrailsBody: "", runLocalCiText: "check-c.mjs" });
  if (!noSelfProblems.some((p) => p.includes("no selfTest and no selfTestExemptReason"))) {
    throw new Error("self-test: missing self-test not detected");
  }

  // required guard missing from make guardrails -> detected.
  const missMake = validate({ manifest: goodManifest, mjsScripts: mjs, makeGuardrailsBody: "", runLocalCiText: ci });
  if (!missMake.some((p) => p.includes("missing from Makefile guardrails"))) {
    throw new Error("self-test: required-missing-from-make not detected");
  }

  // required guard missing from run-local-ci -> detected.
  const missCi = validate({ manifest: goodManifest, mjsScripts: mjs, makeGuardrailsBody: makeBody, runLocalCiText: "" });
  if (!missCi.some((p) => p.includes("missing from a standard CI job"))) {
    throw new Error("self-test: required-missing-from-ci not detected");
  }

  // A required guard mentioned only by the compatibility `run_guardrails` helper is NOT wired to
  // the standard `common`/component jobs that hosted CI and default `make ci-local` actually run.
  // This was the false green that left domain-event-architecture-guard out of every normal PR run.
  const compatibilityOnlyCi = [
    "run_common() {",
    "  step other-guard",
    "}",
    "run_guardrails() {",
    "  step a-guard",
    "}",
  ].join("\n");
  const compatibilityOnly = validate({
    manifest: goodManifest,
    mjsScripts: mjs,
    makeGuardrailsBody: makeBody,
    runLocalCiText: compatibilityOnlyCi,
  });
  if (!compatibilityOnly.some((p) => p.includes("standard CI job"))) {
    throw new Error("self-test: compatibility-only guard wiring was not detected");
  }

  console.log("guardrail-registration guard: self-test passed");
}

function run() {
  const manifest = JSON.parse(readFileSync(resolve(repo, MANIFEST), "utf8"));
  const mjsScripts = enumerateMjsGuards();
  const makeGuardrailsBody = extractGuardrailsBody(readFileSync(resolve(repo, "Makefile"), "utf8"));
  const runLocalCiText = readFileSync(resolve(repo, "tools/ci/run-local-ci.sh"), "utf8");

  const problems = validate({ manifest, mjsScripts, makeGuardrailsBody, runLocalCiText });
  if (problems.length > 0) {
    console.error("guardrail-registration guard: FAIL");
    for (const p of problems) console.error(`  - ${p}`);
    console.error(`Fix ${MANIFEST} or wire the guard into make guardrails / run-local-ci.sh.`);
    process.exit(1);
  }
  console.log(`guardrail-registration guard: ${manifest.guards.length} guards registered, ${mjsScripts.length} mjs scripts all accounted for`);
}

if (process.argv.includes("--self-test")) selfTest();
else run();
