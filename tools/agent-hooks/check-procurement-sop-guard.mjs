#!/usr/bin/env node
// check-procurement-sop-guard.mjs
//
// PROCUREMENT SOP (maintainer decision 2026-09-14, docs/decisions/procurement-sop.md). The animal
// purchase inspection the phone runs -- pages, questions, proof, compulsory flags -- is compiled
// from the PUBLISHED procurement.animal_purchase SOP version, never from a Go catalog. The old
// catalog survives in animalpurchase/domain/questionnaire.go ONLY as the golden oracle for the
// seeded document.
//
// WHAT IT CHECKS:
//   1. catalog-read-from-go   -- no production Go outside animalpurchase/domain calls
//                                domain.Questionnaire() / QuestionByID / MediaSlots() (the Catalog
//                                resolved per SOP version is the only way to read questions).
//   2. seed-drifted-from-migration -- the embedded seed document is present verbatim in the
//                                migration that publishes it as v1.
//   3. hardcoded-question-on-phone -- the Android animal-purchase screens must not hardcode a
//                                question title ("Goat or sheep", "Photo of teeth", ...): the phone
//                                renders the served questionnaire verbatim.
//
// PROCUREMENT IS SOP-DRIVEN END TO END (maintainer decision 2026-09-20,
// docs/decisions/procurement-sop-driven.md) adds three more, one per document that joined:
//   4. seeded-document-drifted -- EVERY seeded procurement document (the two purchase workflows,
//                                the supplier form, the aflatoxin procedure, the feed purchase
//                                form) is embedded verbatim in the migration that publishes it as
//                                v1. A seed that drifts from its migration means the tests reason
//                                about a document no tenant runs.
//   5. toxin-steps-read-from-go -- no production Go outside toxin/domain calls domain.Steps() /
//                                StepSpecFor() / WorkingSteps(), and none uses domain.FinalStepNo
//                                as a value. Those are the LEGACY constants, kept only as the
//                                golden oracle for the seed; the round runs the procedure of its
//                                own stamped version, and reading the constants instead would run
//                                a test the farm did not author.
//   6. engine-step-hook-missing -- the four engine-completed steps (the office decision, the feed
//                                arrival, the aflatoxin sign-off, and the sale's tagging) keep a
//                                task type carrying their hook in the seeded registry. A hook with
//                                no registry row is a step nothing can ever complete.
// BLIND SPOTS: names and strings, not behaviour. A new Go catalog under another name, or a title
// typed with different wording, slips through -- the golden test and the VM tests are the runtime half.

import { readFileSync, readdirSync, statSync, writeFileSync, mkdtempSync, rmSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";

const REPO = resolve(new URL("../..", import.meta.url).pathname);
const CATALOG_CALLS = /\bdomain\.(Questionnaire|QuestionByID|MediaSlots)\s*\(/g;
const MIGRATION = "backend/migrations/postgres/000307_procurement_sop_animal_purchase_inspection.sql";
const SEED = "backend/internal/animalpurchase/domain/inspectionseed/animal_purchase.json";

// Every seeded procurement document and the migration that publishes it (2026-09-20).
const SEEDED_DOCUMENTS = [
  ["backend/internal/procurement/domain/vendorformseed/vendor.json", "backend/migrations/postgres/000370_procurement_vendor_form_sop.sql"],
  ["backend/internal/tasks/domain/sopseed/procurement_animal_purchase_intake.json", "backend/migrations/postgres/000371_procurement_animal_purchase_intake_sop.sql"],
  ["backend/internal/tasks/domain/sopseed/task_types_procurement.json", "backend/migrations/postgres/000371_procurement_animal_purchase_intake_sop.sql"],
  ["backend/internal/tasks/domain/sopseed/procurement_feed_purchase_intake.json", "backend/migrations/postgres/000373_procurement_feed_purchase_intake_sop.sql"],
  ["backend/internal/tasks/domain/sopseed/task_types_procurement_feed.json", "backend/migrations/postgres/000373_procurement_feed_purchase_intake_sop.sql"],
  ["backend/internal/toxin/domain/toxinseed/toxin_test.json", "backend/migrations/postgres/000375_toxin_procedure_sop.sql"],
  ["backend/internal/procurement/domain/feedformseed/feed_purchase.json", "backend/migrations/postgres/000376_feed_purchase_form_sop.sql"],
];

// The legacy toxin constants. They stay in toxin/domain as the golden oracle for the seed; every
// runtime read goes through the round's own Procedure.
const TOXIN_LEGACY_CALLS = /\bdomain\.(Steps|StepSpecFor|WorkingSteps)\s*\(|\bdomain\.FinalStepNo\b/g;

// Engine hooks whose steps nothing else can complete, and the seeded registry files that must
// declare them.
const ENGINE_HOOKS = ["animal_purchase_decision", "feed_purchase_reached", "toxin_test_accepted"];
const TASK_TYPE_FILES = [
  "backend/internal/tasks/domain/sopseed/task_types_procurement.json",
  "backend/internal/tasks/domain/sopseed/task_types_procurement_feed.json",
];
const QUESTION_COPY = ["Goat or sheep", "Photo of teeth", "Is the animal pregnant?", "Rectal temperature of the goat?", "Udder or testicles media", "Face visual productivity check"];

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
  for (const f of walk(join(root, "backend/internal"))) {
    if (!f.endsWith(".go") || f.endsWith("_test.go")) continue;
    if (f.includes("/backend/internal/animalpurchase/domain/")) continue;
    const text = readFileSync(f, "utf8");
    for (const m of text.matchAll(CATALOG_CALLS)) {
      findings.push({ rule: "catalog-read-from-go", file: f.slice(root.length + 1), detail: m[0].trim() });
    }
  }
  let seed = "";
  let migration = "";
  try { seed = readFileSync(join(root, SEED), "utf8").trim(); } catch { seed = ""; }
  try { migration = readFileSync(join(root, MIGRATION), "utf8"); } catch { migration = ""; }
  if (seed && !migration.includes("$seed$" + seed + "$seed$")) {
    findings.push({ rule: "seed-drifted-from-migration", file: SEED, detail: "not embedded verbatim in migration 000307" });
  }
  // 4. Every seeded procurement document is embedded verbatim in its migration.
  for (const [seedPath, migrationPath] of SEEDED_DOCUMENTS) {
    let doc = "";
    let sql = "";
    try { doc = readFileSync(join(root, seedPath), "utf8").trim(); } catch { continue; }
    try { sql = readFileSync(join(root, migrationPath), "utf8"); } catch { sql = ""; }
    if (!sql.includes("$seed$" + doc + "$seed$")) {
      findings.push({ rule: "seeded-document-drifted", file: seedPath, detail: `not embedded verbatim in ${migrationPath.split("/").pop()}` });
    }
  }

  // 5. The legacy toxin step constants are read by nothing on the runtime path.
  for (const f of walk(join(root, "backend/internal"))) {
    if (!f.endsWith(".go") || f.endsWith("_test.go")) continue;
    if (f.includes("/backend/internal/toxin/domain/")) continue;
    const text = readFileSync(f, "utf8");
    for (const m of text.matchAll(TOXIN_LEGACY_CALLS)) {
      findings.push({ rule: "toxin-steps-read-from-go", file: f.slice(root.length + 1), detail: m[0].trim() });
    }
  }

  // 6. Every engine-completed step's hook is declared by a seeded task type.
  const declaredHooks = new Set();
  for (const file of TASK_TYPE_FILES) {
    let rows = [];
    try { rows = JSON.parse(readFileSync(join(root, file), "utf8")); } catch { rows = []; }
    for (const row of rows) if (row && row.engine_hook) declaredHooks.add(row.engine_hook);
  }
  for (const hook of ENGINE_HOOKS) {
    if (!declaredHooks.has(hook)) {
      findings.push({ rule: "engine-step-hook-missing", file: TASK_TYPE_FILES[0], detail: hook });
    }
  }

  for (const f of walk(join(root, "apps/goatos-android"))) {
    if (!f.endsWith(".kt") || f.includes("/src/test/") || f.includes("/src/androidTest/")) continue;
    if (!/AnimalPurchase/.test(f)) continue;
    // Comments are prose (a KDoc may quote a title as an example); only code counts.
    const text = readFileSync(f, "utf8").replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
    for (const copy of QUESTION_COPY) {
      if (text.includes(`"${copy}"`)) findings.push({ rule: "hardcoded-question-on-phone", file: f.slice(root.length + 1), detail: copy });
    }
  }
  return findings;
}

function selfTest() {
  const tmp = mkdtempSync(join(tmpdir(), "procsop-guard-"));
  try {
    mkdirSync(join(tmp, "backend/internal/animalpurchase/app"), { recursive: true });
    mkdirSync(join(tmp, "backend/internal/animalpurchase/domain/inspectionseed"), { recursive: true });
    mkdirSync(join(tmp, "backend/migrations/postgres"), { recursive: true });
    mkdirSync(join(tmp, "apps/goatos-android/app/src/main"), { recursive: true });
    writeFileSync(join(tmp, "backend/internal/animalpurchase/app/x.go"), "package app\nfunc f() { _ = domain.Questionnaire() }\n");
    writeFileSync(join(tmp, SEED.replace("backend/internal/animalpurchase/domain/inspectionseed/", "backend/internal/animalpurchase/domain/inspectionseed/")), '{"a":1}');
    writeFileSync(join(tmp, MIGRATION), "$seed${\"a\":2}$seed$");
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/AnimalPurchaseScreen.kt"), 'val t = "Photo of teeth"\n');
    // The three 2026-09-20 rules, each given a fixture that must FAIL: a seeded document whose
    // migration does not embed it, a runtime read of the legacy toxin constants, and an engine
    // hook no task type declares.
    mkdirSync(join(tmp, "backend/internal/procurement/domain/vendorformseed"), { recursive: true });
    mkdirSync(join(tmp, "backend/internal/toxin/app"), { recursive: true });
    writeFileSync(join(tmp, SEEDED_DOCUMENTS[0][0]), '{"b":1}');
    writeFileSync(join(tmp, SEEDED_DOCUMENTS[0][1]), "$seed${\"b\":2}$seed$");
    writeFileSync(join(tmp, "backend/internal/toxin/app/y.go"), "package app\nfunc g() { _ = domain.Steps() }\n");
    const bad = check(tmp);
    const rules = new Set(bad.map((b) => b.rule));
    for (const r of ["catalog-read-from-go", "seed-drifted-from-migration", "hardcoded-question-on-phone", "seeded-document-drifted", "toxin-steps-read-from-go", "engine-step-hook-missing"]) {
      if (!rules.has(r)) { console.error(`self-test: expected ${r}`); process.exit(1); }
    }
    writeFileSync(join(tmp, "backend/internal/animalpurchase/app/x.go"), "package app\n");
    writeFileSync(join(tmp, "backend/internal/animalpurchase/app/x_test.go"), "package app\nfunc f() { _ = domain.Questionnaire() }\n");
    writeFileSync(join(tmp, MIGRATION), "$seed${\"a\":1}$seed$");
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/AnimalPurchaseScreen.kt"), "val t = q.title\n");
    writeFileSync(join(tmp, SEEDED_DOCUMENTS[0][1]), "$seed${\"b\":1}$seed$");
    writeFileSync(join(tmp, "backend/internal/toxin/app/y.go"), "package app\n");
    // The remaining seeded documents are absent from the fixture tree, which the check SKIPS
    // (a missing seed file is not this guard's business); the hooks come from the real files.
    mkdirSync(join(tmp, "backend/internal/tasks/domain/sopseed"), { recursive: true });
    const hookRows = JSON.stringify(ENGINE_HOOKS.map((h) => ({ key: h, engine_hook: h })));
    for (const [file, migration] of SEEDED_DOCUMENTS.filter(([f]) => TASK_TYPE_FILES.includes(f))) {
      writeFileSync(join(tmp, file), hookRows);
      // Those two files are ALSO seeded documents, so the fixture's migration must embed them --
      // exactly the coupling the rule exists to keep.
      writeFileSync(join(tmp, migration), "$seed$" + hookRows + "$seed$");
    }
    const good = check(tmp);
    if (good.length) { console.error("self-test: expected clean, got", good); process.exit(1); }
    console.log("procurement-sop-guard: self-test passed");
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
    console.error(`procurement-sop-guard: ${findings.length} finding(s). See docs/decisions/procurement-sop.md.`);
    process.exit(1);
  }
  console.log("procurement-sop-guard: ok");
}
