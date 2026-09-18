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
//                                   present verbatim in a $seed$-bearing migration (000308 for herd
//                                   operations, 000354 for the general SOP; the Go test pins the
//                                   same thing; this catches it before a compile).
//   3. hardcoded-step-copy-on-phone -- the Android workflow drill-in must not hardcode an
//                                   operator step title (e.g. "Is the kid clean?", "1st Colostrum",
//                                   "Record death video"): the phone renders backend rows verbatim.
//   4. death-pair-hardcoded      -- (2026-09-16, SOP capture parity) production Go outside
//                                   tasks/domain must not name ActionKeyDeathVideo /
//                                   ActionKeyPostMortemVideo or compare the death proofs against a
//                                   fixed pair (`len(DeathProofRefs(...)) != 2`): a death waits on
//                                   EVERY authored step (tasks/domain.DeathStepsComplete) and its
//                                   bundle is every step's proofs (DeathEvidenceBundle).
//   5. death-pair-on-phone       -- production Android must not gate the death workflow on a fixed
//                                   pair: no `"death_video"` literal, no `actionsTotal == 2` /
//                                   `deathDraftCount`, no `LIMIT 2` on the death draft query. The
//                                   seeded post-mortem step's recorder briefing may still key on
//                                   `"post_mortem_video"` -- that picks copy, it gates nothing.
// BLIND SPOTS: it matches names and strings, not behaviour. Rules 4/5 catch the pair spelled the
// way it was spelled (constants, `== 2` beside the death names, LIMIT 2 in the draft query); a
// pair re-derived under new names is caught only by the seeded-behaviour and N-step tests
// (TestDeathApprovalWaitsForEveryAuthoredStepPg, WorkflowDetailViewModelDeathFollowsSopTest). A new Go template under another
// name, or a step title typed with different wording, slips through -- the golden test and the
// bootstrap copy tests are the runtime half.

import { readFileSync, readdirSync, statSync, writeFileSync, mkdtempSync, rmSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";

const REPO = resolve(new URL("../..", import.meta.url).pathname);
const TEMPLATE_CALLS = /\bdomain\.(TemplateByKeyAt|TemplateBirthKidAt|TemplateBirthMother|TemplateDeath)\s*\(/g;
const DEATH_PAIR_GO = /\b(ActionKeyDeathVideo|ActionKeyPostMortemVideo)\b|len\(\s*(domain\.)?DeathProofRefs\([^)]*\)\s*\)\s*[!=<>]=?\s*2\b/g;
const DEATH_PAIR_KT = [
  { re: /"death_video"/g, detail: '"death_video" literal' },
  { re: /actionsTotal\s*==\s*2\b/g, detail: "actionsTotal == 2" },
  { re: /\bdeathDraftCount\b/g, detail: "deathDraftCount" },
  { re: /workflow_death_draft'[^"]*"[^;]*?LIMIT 2\b|LIMIT 2"[\s\S]{0,80}observeWorkflowDeathDrafts/g, detail: "LIMIT 2 on the death draft query" },
];
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
  // A seed document is pinned to whichever migration embeds it between $seed$ quotes: 000308
  // carries the herd-operations set, 000354 the first general SOP (SOP studio phase 2). Every
  // migration that embeds a $seed$ document is read, so a later SOP shipped by its own
  // migration is pinned the same way rather than silently unchecked.
  const migrationDir = join(root, "backend/migrations/postgres");
  const migration = (() => {
    let names = [];
    try { names = readdirSync(migrationDir).filter((n) => /^\d{6}_.*\.sql$/.test(n)); } catch { return ""; }
    return names
      .map((n) => { try { return readFileSync(join(migrationDir, n), "utf8"); } catch { return ""; } })
      .filter((text) => text.includes("$seed$"))
      .join("\n");
  })();
  let seeds = [];
  try { seeds = readdirSync(seedDir).filter((n) => n.endsWith(".json")); } catch { seeds = []; }
  for (const n of seeds) {
    const doc = readFileSync(join(seedDir, n), "utf8").trim();
    if (!migration.includes("$seed$" + doc + "$seed$")) {
      findings.push({ rule: "seed-drifted-from-migration", file: "backend/internal/tasks/domain/sopseed/" + n, detail: "not embedded verbatim in any $seed$-bearing migration" });
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
  // 4. the death pair in backend production code outside tasks/domain
  for (const f of walk(join(root, "backend"))) {
    if (!f.endsWith(".go") || f.endsWith("_test.go")) continue;
    if (f.includes("/backend/internal/tasks/domain/")) continue;
    const text = readFileSync(f, "utf8");
    for (const m of text.matchAll(DEATH_PAIR_GO)) {
      findings.push({ rule: "death-pair-hardcoded", file: f.slice(root.length + 1), detail: m[0].trim() });
    }
  }
  // 5. the death pair on the phone
  for (const f of walk(android)) {
    if (!f.endsWith(".kt") || f.includes("/src/test/") || f.includes("/src/androidTest/")) continue;
    const text = readFileSync(f, "utf8");
    for (const { re, detail } of DEATH_PAIR_KT) {
      if (text.match(re)) findings.push({ rule: "death-pair-on-phone", file: f.slice(root.length + 1), detail });
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
    mkdirSync(join(tmp, "backend/internal/counts/adapters/postgres"), { recursive: true });
    writeFileSync(join(tmp, "backend/internal/counts/adapters/postgres/death.go"), 'package postgres\nfunc g() bool { return len(domain.DeathProofRefs(a)) != 2 }\n');
    writeFileSync(join(tmp, "backend/internal/counts/adapters/postgres/keys.go"), 'package postgres\nvar k = domain.ActionKeyPostMortemVideo\n');
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/DeathGate.kt"), 'val ok = actionsTotal == 2 && deathDraftCount == 2\n');
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/DeathKeys.kt"), 'val k = "death_video"\n');
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/CaptureEntities.kt"), '"AND proofSubject = \'workflow_death_draft\' " + "ORDER BY capturedAtMs ASC, id ASC LIMIT 2",\n)\nfun observeWorkflowDeathDrafts(id: String)\n');
    const bad = check(tmp);
    const rules = new Set(bad.map((b) => b.rule));
    const phoneDetails = new Set(bad.filter((b) => b.rule === "death-pair-on-phone").map((b) => b.detail));
    for (const d of ['"death_video" literal', "actionsTotal == 2", "deathDraftCount", "LIMIT 2 on the death draft query"]) {
      if (!phoneDetails.has(d)) { console.error(`self-test: expected death-pair-on-phone for ${d}`); process.exit(1); }
    }
    if (bad.filter((b) => b.rule === "death-pair-hardcoded").length !== 2) { console.error("self-test: expected two death-pair-hardcoded findings", bad); process.exit(1); }
    for (const r of ["template-stamped-from-go", "seed-drifted-from-migration", "hardcoded-step-copy-on-phone", "death-pair-hardcoded", "death-pair-on-phone"]) {
      if (!rules.has(r)) { console.error(`self-test: expected ${r}`); process.exit(1); }
    }
    // the golden test file is allowed to call the templates; tests are skipped
    writeFileSync(join(tmp, "backend/internal/counts/app/x.go"), "package app\n");
    writeFileSync(join(tmp, "backend/internal/counts/app/x_test.go"), 'package app\nfunc f() { _ = domain.TemplateDeath() }\n');
    writeFileSync(join(tmp, "backend/migrations/postgres/000308_sop_driven_herd_operations.sql"), "$seed${\"a\":1}$seed$");
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/WorkflowDetailScreen.kt"), 'val t = stringResource(R.string.x)\n');
    // the pair inside tasks/domain (the seeded oracle) and in tests is allowed; the post-mortem
    // briefing key and a bounded draft query are allowed on the phone
    writeFileSync(join(tmp, "backend/internal/counts/adapters/postgres/death.go"), 'package postgres\nfunc g() bool { return domain.DeathStepsComplete(a) }\n');
    writeFileSync(join(tmp, "backend/internal/counts/adapters/postgres/keys.go"), "package postgres\n");
    writeFileSync(join(tmp, "backend/internal/counts/adapters/postgres/keys_test.go"), 'package postgres\nvar k = domain.ActionKeyPostMortemVideo\n');
    writeFileSync(join(tmp, "backend/internal/tasks/domain/templates.go"), 'package domain\nconst ActionKeyDeathVideo = "death_video"\n');
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/DeathGate.kt"), 'val ok = deathStepsReady && !isSubmittingDeath\n');
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/DeathKeys.kt"), 'private const val WORKFLOW_ACTION_KEY_POST_MORTEM = "post_mortem_video"\n');
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/CaptureEntities.kt"), '"AND proofSubject = \'workflow_death_draft\' " + "ORDER BY capturedAtMs ASC, id ASC LIMIT 64",\n)\nfun observeWorkflowDeathDrafts(id: String)\n');
    mkdirSync(join(tmp, "apps/goatos-android/app/src/test"), { recursive: true });
    writeFileSync(join(tmp, "apps/goatos-android/app/src/test/DeathTest.kt"), 'val k = "death_video"\n');
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
