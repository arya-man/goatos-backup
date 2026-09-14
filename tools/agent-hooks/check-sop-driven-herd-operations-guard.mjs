#!/usr/bin/env node
// check-sop-driven-herd-operations-guard.mjs
//
// SOP-DRIVEN HERD OPERATIONS (maintainer decision 2026-09-13,
// docs/decisions/sop-driven-herd-operations.md). The operator's steps for a birth, death,
// shifting completion and reconcile -- which questions, which proof, when due -- are compiled
// from the PUBLISHED SOP's `follow_up` section at workflow open, never from Go constants.
// The old code templates survive in tasks/domain/templates.go ONLY as the golden oracle for
// the seeded documents.
//
// WHAT IT CHECKS:
//   1. template-stamped-from-go  -- no production Go outside tasks/domain calls
//                                   TemplateByKeyAt / TemplateBirthKidAt / TemplateBirthMother /
//                                   TemplateDeath (the compiler path is the only way to open).
//   2. seed-drifted-from-migration -- every embedded document in tasks/domain/sopseed/*.json is
//                                   present verbatim in migration 000308 (the Go test pins the
//                                   same thing; this catches it before a compile).
//   3. hardcoded-step-copy-on-phone -- the Android workflow drill-in must not hardcode an
//                                   operator step title (e.g. "Is the kid clean?", "1st Colostrum",
//                                   "Record death video"): the phone renders backend rows verbatim.
// BLIND SPOTS: it matches names and strings, not behaviour. A new Go template under another
// name, or a step title typed with different wording, slips through -- the golden test and the
// bootstrap copy tests are the runtime half.

import { readFileSync, readdirSync, statSync, writeFileSync, mkdtempSync, rmSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";

const REPO = resolve(new URL("../..", import.meta.url).pathname);
const TEMPLATE_CALLS = /\bdomain\.(TemplateByKeyAt|TemplateBirthKidAt|TemplateBirthMother|TemplateDeath)\s*\(/g;
const STEP_COPY = [
  "Is the kid clean?", "Iodine dipping of umbilical cord", "1st Colostrum", "Take Weight of Kid",
  "Record death video", "Record post-mortem video", "Tag the kid", "Return the animal to its registered pen",
];

function walk(dir, acc = []) {
  let entries = [];
  try { entries = readdirSync(dir); } catch { return acc; }
  for (const e of entries) {
    const full = join(dir, e);
    if (e === "node_modules" || e === "build" || e === ".git") continue;
    const st = statSync(full);
    if (st.isDirectory()) walk(full, acc); else acc.push(full);
  }
  return acc;
}

export function check(root) {
  const findings = [];
  // 1. template calls outside tasks/domain and tests
  for (const f of walk(join(root, "backend/internal"))) {
    if (!f.endsWith(".go") || f.endsWith("_test.go")) continue;
    if (f.includes("/backend/internal/tasks/domain/")) continue;
    const text = readFileSync(f, "utf8");
    for (const m of text.matchAll(TEMPLATE_CALLS)) {
      findings.push({ rule: "template-stamped-from-go", file: f.slice(root.length + 1), detail: m[0].trim() });
    }
  }
  // 2. seed vs migration
  const seedDir = join(root, "backend/internal/tasks/domain/sopseed");
  const migration = (() => {
    try { return readFileSync(join(root, "backend/migrations/postgres/000308_sop_driven_herd_operations.sql"), "utf8"); } catch { return ""; }
  })();
  let seeds = [];
  try { seeds = readdirSync(seedDir).filter((n) => n.endsWith(".json")); } catch { seeds = []; }
  for (const n of seeds) {
    const doc = readFileSync(join(seedDir, n), "utf8").trim();
    if (!migration.includes("$seed$" + doc + "$seed$")) {
      findings.push({ rule: "seed-drifted-from-migration", file: "backend/internal/tasks/domain/sopseed/" + n, detail: "not embedded verbatim in migration 000308" });
    }
  }
  // 3. phone hardcoded step copy
  const android = join(root, "apps/goatos-android");
  for (const f of walk(android)) {
    if (!f.endsWith(".kt") || f.includes("/src/test/") || f.includes("/src/androidTest/")) continue;
    if (!/WorkflowDetail|WorkflowList|PenReconciliation/.test(f)) continue;
    const text = readFileSync(f, "utf8");
    for (const copy of STEP_COPY) {
      if (text.includes(`"${copy}"`)) findings.push({ rule: "hardcoded-step-copy-on-phone", file: f.slice(root.length + 1), detail: copy });
    }
  }
  return findings;
}

function selfTest() {
  const tmp = mkdtempSync(join(tmpdir(), "sop-guard-"));
  try {
    mkdirSync(join(tmp, "backend/internal/counts/app"), { recursive: true });
    mkdirSync(join(tmp, "backend/internal/tasks/domain/sopseed"), { recursive: true });
    mkdirSync(join(tmp, "backend/migrations/postgres"), { recursive: true });
    mkdirSync(join(tmp, "apps/goatos-android/app/src/main"), { recursive: true });
    // bad: production call, drifted seed, hardcoded copy
    writeFileSync(join(tmp, "backend/internal/counts/app/x.go"), 'package app\nfunc f() { _ = domain.TemplateDeath() }\n');
    writeFileSync(join(tmp, "backend/internal/tasks/domain/sopseed/counts_death.json"), '{"a":1}');
    writeFileSync(join(tmp, "backend/migrations/postgres/000308_sop_driven_herd_operations.sql"), "$seed${\"a\":2}$seed$");
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/WorkflowDetailScreen.kt"), 'val t = "Is the kid clean?"\n');
    const bad = check(tmp);
    const rules = new Set(bad.map((b) => b.rule));
    for (const r of ["template-stamped-from-go", "seed-drifted-from-migration", "hardcoded-step-copy-on-phone"]) {
      if (!rules.has(r)) { console.error(`self-test: expected ${r}`); process.exit(1); }
    }
    // the golden test file is allowed to call the templates; tests are skipped
    writeFileSync(join(tmp, "backend/internal/counts/app/x.go"), "package app\n");
    writeFileSync(join(tmp, "backend/internal/counts/app/x_test.go"), 'package app\nfunc f() { _ = domain.TemplateDeath() }\n');
    writeFileSync(join(tmp, "backend/migrations/postgres/000308_sop_driven_herd_operations.sql"), "$seed${\"a\":1}$seed$");
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/WorkflowDetailScreen.kt"), 'val t = stringResource(R.string.x)\n');
    const good = check(tmp);
    if (good.length) { console.error("self-test: expected clean, got", good); process.exit(1); }
    console.log("sop-driven-herd-operations-guard: self-test passed");
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
}

if (process.argv.includes("--self-test")) {
  selfTest();
} else {
  const findings = check(REPO);
  if (findings.length) {
    for (const f of findings) console.error(`${f.rule}  ${f.file}  ${f.detail}`);
    console.error(`sop-driven-herd-operations-guard: ${findings.length} finding(s). See docs/decisions/sop-driven-herd-operations.md.`);
    process.exit(1);
  }
  console.log("sop-driven-herd-operations-guard: ok");
}
