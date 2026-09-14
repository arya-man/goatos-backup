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
// BLIND SPOTS: names and strings, not behaviour. A new Go catalog under another name, or a title
// typed with different wording, slips through -- the golden test and the VM tests are the runtime half.

import { readFileSync, readdirSync, statSync, writeFileSync, mkdtempSync, rmSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";

const REPO = resolve(new URL("../..", import.meta.url).pathname);
const CATALOG_CALLS = /\bdomain\.(Questionnaire|QuestionByID|MediaSlots)\s*\(/g;
const MIGRATION = "backend/migrations/postgres/000307_procurement_sop_animal_purchase_inspection.sql";
const SEED = "backend/internal/animalpurchase/domain/inspectionseed/animal_purchase.json";
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
    const bad = check(tmp);
    const rules = new Set(bad.map((b) => b.rule));
    for (const r of ["catalog-read-from-go", "seed-drifted-from-migration", "hardcoded-question-on-phone"]) {
      if (!rules.has(r)) { console.error(`self-test: expected ${r}`); process.exit(1); }
    }
    writeFileSync(join(tmp, "backend/internal/animalpurchase/app/x.go"), "package app\n");
    writeFileSync(join(tmp, "backend/internal/animalpurchase/app/x_test.go"), "package app\nfunc f() { _ = domain.Questionnaire() }\n");
    writeFileSync(join(tmp, MIGRATION), "$seed${\"a\":1}$seed$");
    writeFileSync(join(tmp, "apps/goatos-android/app/src/main/AnimalPurchaseScreen.kt"), "val t = q.title\n");
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
