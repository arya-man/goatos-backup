#!/usr/bin/env node
// check-guardrail-registration.mjs — meta-guard: keeps tools/ci/guardrail-manifest.json
// in sync with the guard scripts on disk, the Makefile `guardrails:` target, and
// tools/ci/run-local-ci.sh. Governance invariant (docs/runbooks/local-ci.md): a guard
// that exists but is unregistered, has no self-test, or is required-but-unwired is a
// silent hole — CI must fail closed on it.
//
// Fails when:
//   (1) a check-*.mjs OR check-*.sh guard script exists under tools/agent-hooks/ or
//       tools/ci/ but is absent from manifest.guards[].script. (check-*.test.sh files are
//       self-test harnesses, not guards, and are excluded.) Enumerating shell guards is
//       deliberate: before it, six .sh guards under tools/agent-hooks/ were unregistered
//       and this guard still reported "all accounted for".
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

// F-B: rules (3) and (4) below prove a guard is "wired" by SUBSTRING match. A bare
// COMMENT mentioning the guard's name satisfied that — so deleting a real guard
// invocation while leaving `# make foo-guard` nearby kept this green. Demonstrated
// 2026-08-05 against run-local-ci.sh. Strip comment-only lines before matching so
// only a real invocation counts. (Still a substring test, not an execution probe —
// stated plainly here rather than implied to be stronger than it is.)
function stripCommentOnlyLines(text) {
  return String(text || "")
    .split("\n")
    .filter((line) => !/^\s*#/.test(line))
    .join("\n");
}

function commandLineInvokesTarget(line, target) {
  const escaped = target.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(?:^|[;&|\\s])(?:\\$\\(MAKE\\)|\\$\\{MAKE\\}|make)\\s+[^\\n#]*\\b${escaped}\\b`).test(line);
}

function makeTargetWired(makeGuardrailsBody, target) {
  return stripCommentOnlyLines(makeGuardrailsBody)
    .split("\n")
    .some((line) => commandLineInvokesTarget(line, target));
}

function ciStepWired(runLocalCiText, ciStep) {
  const standardJobs = stripCommentOnlyLines(standardCiJobText(runLocalCiText));
  return standardJobs
    .split("\n")
    .some((line) => {
      if (!/\bstep(?:_cached)?\b/.test(line) || !line.includes(ciStep)) return false;
      if (/\bstep(?:_cached)?\b[\s\S]*\becho\b/.test(line)) return false;
      return true;
    });
}

export function validate({ manifest, guardScripts, makeGuardrailsBody, runLocalCiText, owningDocs = new Map() }) {
  const problems = [];
  const guards = manifest.guards || [];
  const registered = new Set(guards.map((g) => g.script));

  // (1) every enumerated guard script (.mjs and .sh) must be registered.
  for (const script of guardScripts) {
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

    if (g.owningDoc) {
      const docText = owningDocs.get(g.owningDoc);
      if (docText == null) {
        problems.push(`guard ${id}: owningDoc "${g.owningDoc}" does not exist`);
      } else if (id === "android-proof-media-egress" || id === "backend-proof-media-egress" || id === "admin-web-proof-media-egress") {
        const requiredPhrases = ["--all", "adjacent", "Cloud Monitoring", "Slack", "live", "configured"];
        if (id === "admin-web-proof-media-egress") {
          requiredPhrases.push("admin-web", "drawer", "<img>", "<video>", "explicit open");
        }
        for (const phrase of requiredPhrases) {
          if (!docText.includes(phrase)) {
            problems.push(`guard ${id}: owningDoc "${g.owningDoc}" must mention "${phrase}"`);
          }
        }
      }
    }

    if (g.requiredInCI === true) {
      // (3) required guard's makeTarget must be wired into `make guardrails`.
      if (g.makeTarget && !makeTargetWired(makeGuardrailsBody, g.makeTarget)) {
        problems.push(`required guard ${id}: makeTarget "${g.makeTarget}" missing from Makefile guardrails: target`);
      }
      // (4) required guard's ciStep must appear in a standard local-CI job. A mention only
      // in run_guardrails (the compatibility mode) is not enforcement over pull requests.
      const ciStep = g.ciStep || g.makeTarget;
      if (!ciStep || !ciStepWired(runLocalCiText, ciStep)) {
        problems.push(`required guard ${id}: ciStep "${ciStep}" missing from a standard CI job in tools/ci/run-local-ci.sh`);
      }
    }
  }
  return problems;
}

// Enumerate guard scripts on disk. A guard is check-<name>.<ext>; a *.test.<ext> sibling is
// its self-test harness, not a guard, so it is excluded (registering harnesses would demand
// self-tests-for-self-tests). Shell guards are enumerated for exactly the same reason as mjs
// ones: an unregistered guard is a silent hole regardless of what it is written in.
export function enumerateGuardScripts(readDir = (d) => (existsSync(resolve(repo, d)) ? readdirSync(resolve(repo, d)) : [])) {
  const dirs = ["tools/agent-hooks", "tools/ci"];
  const mjs = [];
  const sh = [];
  for (const d of dirs) {
    for (const name of readDir(d)) {
      if (!name.startsWith("check-")) continue;
      if (name.endsWith(".test.mjs") || name.endsWith(".test.sh")) continue;
      if (name.endsWith(".mjs")) mjs.push(`${d}/${name}`);
      else if (name.endsWith(".sh")) sh.push(`${d}/${name}`);
    }
  }
  return { mjs: mjs.sort(), sh: sh.sort() };
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
      { id: "a", script: "tools/ci/check-a.mjs", makeTarget: "a-guard", selfTest: "x --self-test", owningDoc: "docs/a.md", requiredInCI: true, ciStep: "a-guard" },
      { id: "c", script: "tools/agent-hooks/check-c.sh", makeTarget: null, selfTestExemptReason: "shell", owningDoc: "docs/c.md", requiredInCI: false, ciStep: "check-c.sh" },
      { id: "b", script: "tools/agent-hooks/check-b.mjs", makeTarget: null, selfTestExemptReason: "driven e2e", owningDoc: "docs/b.md", requiredInCI: false, ciStep: "check-b.mjs" },
    ],
  };
  const mjs = ["tools/ci/check-a.mjs", "tools/agent-hooks/check-b.mjs", "tools/agent-hooks/check-c.sh"];
  const makeBody = "\t$(MAKE) a-guard\n";
  const ci = "run_common() {\n  step a-guard make a-guard\n}\n";
  const docs = new Map([
    ["docs/a.md", "doc"],
    ["docs/b.md", "doc"],
    ["docs/c.md", "doc"],
  ]);

  const clean = validate({ manifest: goodManifest, guardScripts: mjs, makeGuardrailsBody: makeBody, runLocalCiText: ci, owningDocs: docs });
  if (clean.length !== 0) throw new Error(`self-test: expected clean, got ${JSON.stringify(clean)}`);

  // orphan script present on disk but not in manifest -> detected.
  const orphan = validate({ manifest: goodManifest, guardScripts: [...mjs, "tools/ci/check-orphan.mjs"], makeGuardrailsBody: makeBody, runLocalCiText: ci, owningDocs: docs });
  if (!orphan.some((p) => p.includes("unregistered guard script: tools/ci/check-orphan.mjs"))) {
    throw new Error("self-test: orphan script not detected");
  }

  // guard with no self-test and no exemption -> detected.
  const noSelf = { guards: [{ id: "d", script: "tools/ci/check-d.mjs", makeTarget: null, requiredInCI: false, ciStep: "check-d.mjs" }] };
  const noSelfProblems = validate({ manifest: noSelf, guardScripts: ["tools/ci/check-d.mjs"], makeGuardrailsBody: "", runLocalCiText: "check-d.mjs", owningDocs: docs });
  if (!noSelfProblems.some((p) => p.includes("no selfTest and no selfTestExemptReason"))) {
    throw new Error("self-test: missing self-test not detected");
  }

  // required guard missing from make guardrails -> detected.
  const missMake = validate({ manifest: goodManifest, guardScripts: mjs, makeGuardrailsBody: "", runLocalCiText: ci, owningDocs: docs });
  if (!missMake.some((p) => p.includes("missing from Makefile guardrails"))) {
    throw new Error("self-test: required-missing-from-make not detected");
  }

  // required guard missing from run-local-ci -> detected.
  const missCi = validate({ manifest: goodManifest, guardScripts: mjs, makeGuardrailsBody: makeBody, runLocalCiText: "", owningDocs: docs });
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
    guardScripts: mjs,
    makeGuardrailsBody: makeBody,
    runLocalCiText: compatibilityOnlyCi,
    owningDocs: docs,
  });
  if (!compatibilityOnly.some((p) => p.includes("standard CI job"))) {
    throw new Error("self-test: compatibility-only guard wiring was not detected");
  }

  const echoOnlyMake = validate({
    manifest: goodManifest,
    guardScripts: mjs,
    makeGuardrailsBody: "\techo a-guard\n",
    runLocalCiText: ci,
    owningDocs: docs,
  });
  if (!echoOnlyMake.some((p) => p.includes("missing from Makefile guardrails"))) {
    throw new Error("self-test: echo-only make wiring was not detected");
  }

  const echoOnlyCi = validate({
    manifest: goodManifest,
    guardScripts: mjs,
    makeGuardrailsBody: makeBody,
    runLocalCiText: "run_common() {\n  step a-guard echo a-guard\n}\n",
    owningDocs: docs,
  });
  if (!echoOnlyCi.some((p) => p.includes("standard CI job"))) {
    throw new Error("self-test: echo-only CI wiring was not detected");
  }

  // Enumeration itself: shell guards must be picked up, *.test.sh harnesses must not.
  // Before this, .sh guards were invisible to rule (1) and six were silently unregistered.
  const fakeDirs = {
    "tools/agent-hooks": ["check-x.sh", "check-x.test.sh", "check-y.mjs", "helper.sh", "notacheck.sh"],
    "tools/ci": ["check-z.sh", "run-local-ci.sh"],
  };
  const enumerated = enumerateGuardScripts((d) => fakeDirs[d] || []);
  const expectSh = ["tools/agent-hooks/check-x.sh", "tools/ci/check-z.sh"];
  if (JSON.stringify(enumerated.sh) !== JSON.stringify(expectSh)) {
    throw new Error(`self-test: shell enumeration wrong, got ${JSON.stringify(enumerated.sh)}`);
  }
  if (JSON.stringify(enumerated.mjs) !== JSON.stringify(["tools/agent-hooks/check-y.mjs"])) {
    throw new Error(`self-test: mjs enumeration wrong, got ${JSON.stringify(enumerated.mjs)}`);
  }
  // ...and an enumerated-but-unregistered SHELL guard must be a failure, not a shrug.
  const orphanSh = validate({
    manifest: goodManifest,
    guardScripts: [...mjs, "tools/agent-hooks/check-orphan.sh"],
    makeGuardrailsBody: makeBody,
    runLocalCiText: ci,
    owningDocs: docs,
  });
  if (!orphanSh.some((p) => p.includes("unregistered guard script: tools/agent-hooks/check-orphan.sh"))) {
    throw new Error("self-test: orphan SHELL guard not detected");
  }

  const adminProofManifest = {
    guards: [
      {
        id: "admin-web-proof-media-egress",
        script: "tools/agent-hooks/check-admin-web-proof-media-egress.mjs",
        makeTarget: null,
        selfTest: "node tools/agent-hooks/check-admin-web-proof-media-egress.mjs --self-test",
        owningDoc: "docs/proof-egress.md",
        requiredInCI: false,
        ciStep: "admin-web-proof-media-egress-guard",
      },
    ],
  };
  const adminDocProblems = validate({
    manifest: adminProofManifest,
    guardScripts: ["tools/agent-hooks/check-admin-web-proof-media-egress.mjs"],
    makeGuardrailsBody: "",
    runLocalCiText: "",
    owningDocs: new Map([["docs/proof-egress.md", "--all adjacent Cloud Monitoring Slack live configured"]]),
  });
  if (!adminDocProblems.some((p) => p.includes("must mention \"admin-web\""))) {
    throw new Error("self-test: admin-web proof-media owning-doc phrases were not enforced");
  }

  console.log("guardrail-registration guard: self-test passed");
}

function run() {
  const manifest = JSON.parse(readFileSync(resolve(repo, MANIFEST), "utf8"));
  const { mjs: mjsScripts, sh: shScripts } = enumerateGuardScripts();
  const makeGuardrailsBody = extractGuardrailsBody(readFileSync(resolve(repo, "Makefile"), "utf8"));
  const runLocalCiText = readFileSync(resolve(repo, "tools/ci/run-local-ci.sh"), "utf8");
  const owningDocs = new Map();
  for (const g of manifest.guards || []) {
    if (!g.owningDoc || owningDocs.has(g.owningDoc)) continue;
    const path = resolve(repo, g.owningDoc);
    owningDocs.set(g.owningDoc, existsSync(path) ? readFileSync(path, "utf8") : null);
  }

  const problems = validate({
    manifest,
    guardScripts: [...mjsScripts, ...shScripts],
    makeGuardrailsBody,
    runLocalCiText,
    owningDocs,
  });
  if (problems.length > 0) {
    console.error("guardrail-registration guard: FAIL");
    for (const p of problems) console.error(`  - ${p}`);
    console.error(`Fix ${MANIFEST} or wire the guard into make guardrails / run-local-ci.sh.`);
    process.exit(1);
  }
  console.log(
    `guardrail-registration guard: ${manifest.guards.length} guards registered; ` +
      `enumeration covers check-*.mjs and check-*.sh under tools/agent-hooks/ and tools/ci/ ` +
      `(${mjsScripts.length} mjs + ${shScripts.length} sh = ${mjsScripts.length + shScripts.length} on disk, all registered). ` +
      `Guards outside those two directories, and non-mjs/non-sh guards, are registered by hand and NOT auto-enumerated.`,
  );
}

if (process.argv.includes("--self-test")) selfTest();
else run();
