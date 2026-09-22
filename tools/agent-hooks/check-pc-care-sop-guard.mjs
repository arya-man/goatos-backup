#!/usr/bin/env node
// check-pc-care-sop-guard.mjs
//
// PC CARE SOP (maintainer decision 2026-09-22, docs/decisions/pc-care-sop.md). What a PC Care
// operator captures per animal, what they answer at submit, and whether a tablet-in-feed
// deworming removes feed & water the evening before come from the PUBLISHED pc_care.tasks SOP
// version, pinned per task. No rule the document names may go back to being a Go slot table, a
// hardcoded category branch on the phone, or a fixed proof column in SQL.
//
// WHAT IT CHECKS:
//   1. seed-drifted-from-migration    -- the embedded seed document is present verbatim in the
//                                        migration that seeds it (exactly once: there was no PC
//                                        Care SOP before, so there is no in-place add).
//   2. slot-table-read-at-runtime     -- production Go under pccare/{app,adapters} never calls
//                                        domain.SlotsForCategory / IsValidSlotForCategory /
//                                        IsSingleVideoCategory / SlotDisplayLabel for one of the
//                                        FIVE AUTHORED categories' per-animal cards. The table
//                                        survives as the SEED ORACLE and for inventory_vaccine,
//                                        whose fridge check is kernel-owned and not authored.
//                                        Allowlisted: the seed oracle's own file (domain/sop.go),
//                                        the inventory/legacy readers named below.
//   3. sop-table-inside-pccare        -- backend/internal/pccare never names sop_versions /
//                                        sop_definitions: the only reader on its behalf is
//                                        backend/internal/pccaresop (the weighingsop shape).
//   4. removal-category-literal       -- no production Go decides the removal from the CATEGORY
//                                        (`== domain.CategoryDeworming` beside a removal field):
//                                        which categories carry it is `feed_water_removal.
//                                        applies_to` on the pinned document.
//   5. fixed-proof-column-readiness   -- the submit / board readiness SQL never counts the four
//                                        legacy proof columns (video_/before_/during_/after_
//                                        proof_ref) to decide whether an animal is done: the
//                                        predicate is the pinned card's own required_slot_keys.
//                                        Those columns stay as the MIRROR the old readers read.
//   6. phone-hardcodes-care-slots     -- the Android PC Care code never builds a slot list from
//                                        a category branch; it renders the served expected_slots.
//   7. page-copy-registered           -- /pc-care/sops is a real page: the nav leaf, the page
//                                        contract and the `pcsop.` copy namespace are all in the
//                                        adminui service, so the editor never opens with a
//                                        missing copy key (the weighing wsop.title crash).
//
// BLIND SPOTS: names and strings, not behaviour. A slot list re-derived under another name, or a
// category branch spelled with a raw "deworming" string in a file this guard does not read,
// slips through -- the domain tests (sop_test.go), the service tests and the phone's
// PcCareSopRulesTest are the runtime half.

import { readFileSync, readdirSync, statSync, writeFileSync, mkdtempSync, rmSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";

const REPO = resolve(new URL("../..", import.meta.url).pathname);
const MIGRATION = "backend/migrations/postgres/000385_pc_care_sop.sql";
const SEED = "backend/internal/pccare/domain/sopseed/pc_care.json";
const PCCARE_DIR = "backend/internal/pccare";
const PCCARE_APP = "backend/internal/pccare/app";
const PCCARE_PG = "backend/internal/pccare/adapters/postgres";
const PCCARE_BOARD = "backend/internal/pccare/adapters/boardsource";
const ADMINUI_SERVICE = "backend/internal/adminui/app/service.go";
const PHONE_DIRS = [
  "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel",
  "apps/goatos-android/feature/feature-pccare/src/main/kotlin/sg/mesha/goatos/feature/pccare",
];

// The legacy slot table may still be READ for the kernel-owned fridge check and by the file that
// owns it as the seed oracle. Everywhere else the rules come from the pinned document.
const SLOT_TABLE_EXEMPT_FILES = new Set([
  "backend/internal/pccare/domain/sop.go",
  "backend/internal/pccare/domain/domain.go",
]);
const SLOT_TABLE_READ = /\bdomain\.(SlotsForCategory|IsValidSlotForCategory|IsSingleVideoCategory|SlotDisplayLabel)\b/;
const SOP_TABLE = /\b(sop_versions|sop_definitions)\b/;
// A category literal in the same statement as a removal field is the retired rule.
const REMOVAL_CATEGORY_LITERAL = /(FeedRemoval|RemovalOperator|feed_removal)[A-Za-z]*\s*(&&|\|\||[!=]=)[^\n]*CategoryDeworming|CategoryDeworming[^\n]*(&&|\|\||[!=]=)[^\n]*(FeedRemoval|RemovalOperator|feed_removal)/;
const FIXED_COLUMN_READINESS = /(video_proof_ref|before_proof_ref|during_proof_ref|after_proof_ref)\s+IS\s+(NOT\s+)?NULL/i;
const PHONE_CATEGORY_SLOTS = /(CATEGORY_DEWORMING|"deworming"|"hoof_trimming")[^\n]*->[^\n]*listOf\(|when\s*\(\s*category\s*\)[^\n]*\n[^}]*listOf\(\s*(PcCareSlot|Slot)/;

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

// Strip Go comments so a sentence EXPLAINING a rule is never a finding.
function goCode(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
}
// Same for Kotlin.
function ktCode(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
}

function read(root, rel) {
  try { return readFileSync(join(root, rel), "utf8"); } catch { return ""; }
}

export function check(root) {
  const findings = [];

  // 1. The seed IS the migration's document.
  const seed = read(root, SEED).trim();
  const migration = read(root, MIGRATION);
  if (seed) {
    const marker = "$seed$" + seed + "$seed$";
    const count = migration.split(marker).length - 1;
    if (count !== 1) {
      findings.push({
        rule: "seed-drifted-from-migration",
        file: MIGRATION,
        detail: `embeds the seeded pc_care document ${count} time(s), want exactly 1 (the v1 insert)`,
      });
    }
  }

  // 2/4/5. Production Go under pccare.
  for (const file of walk(join(root, PCCARE_APP)).concat(walk(join(root, PCCARE_PG)), walk(join(root, PCCARE_BOARD)))) {
    if (!file.endsWith(".go") || file.endsWith("_test.go")) continue;
    const rel = file.slice(root.length + 1);
    const code = goCode(readFileSync(file, "utf8"));
    if (!SLOT_TABLE_EXEMPT_FILES.has(rel) && SLOT_TABLE_READ.test(code)) {
      // inventory_vaccine's fridge pair is NOT authored: a read guarded by that category is fine.
      const forInventory = /CategoryInventoryVaccine/.test(code);
      if (!forInventory) {
        findings.push({
          rule: "slot-table-read-at-runtime",
          file: rel,
          detail: "reads the legacy slot table; the per-animal card comes from the task's pinned SOP rules",
        });
      }
    }
    if (REMOVAL_CATEGORY_LITERAL.test(code)) {
      findings.push({
        rule: "removal-category-literal",
        file: rel,
        detail: "decides the feed & water removal from the category; the document's applies_to decides it",
      });
    }
    if (FIXED_COLUMN_READINESS.test(code)) {
      findings.push({
        rule: "fixed-proof-column-readiness",
        file: rel,
        detail: "judges an animal's readiness from the legacy proof columns; the predicate is the pinned required_slot_keys",
      });
    }
  }

  // 3. PC Care stays out of the SOP tables.
  for (const file of walk(join(root, PCCARE_DIR))) {
    if (!file.endsWith(".go") || file.endsWith("_test.go")) continue;
    const rel = file.slice(root.length + 1);
    if (SOP_TABLE.test(goCode(readFileSync(file, "utf8")))) {
      findings.push({
        rule: "sop-table-inside-pccare",
        file: rel,
        detail: "names sop_versions / sop_definitions; only backend/internal/pccaresop may read them",
      });
    }
  }

  // 6. The phone renders the served slots.
  for (const dir of PHONE_DIRS) {
    for (const file of walk(join(root, dir))) {
      if (!file.endsWith(".kt") || !/PcCare/.test(file)) continue;
      const rel = file.slice(root.length + 1);
      if (PHONE_CATEGORY_SLOTS.test(ktCode(readFileSync(file, "utf8")))) {
        findings.push({
          rule: "phone-hardcodes-care-slots",
          file: rel,
          detail: "builds a slot list from the category; the phone renders the task's served expected_slots",
        });
      }
    }
  }

  // 7. The page exists end to end in the contract.
  const adminui = read(root, ADMINUI_SERVICE);
  if (adminui) {
    for (const [needle, what] of [
      ['navLeaf("pc-care-sops"', "the nav leaf"],
      ['page("pc-care-sops"', "the page contract"],
      ["pcCareSOPEditorCopy", "the pcsop.* copy namespace"],
    ]) {
      if (!adminui.includes(needle)) {
        findings.push({
          rule: "page-copy-registered",
          file: ADMINUI_SERVICE,
          detail: `${what} for /pc-care/sops is missing (${needle})`,
        });
      }
    }
  }

  return findings;
}

function selfTest() {
  const tmp = mkdtempSync(join(tmpdir(), "pc-care-sop-guard-"));
  try {
    mkdirSync(join(tmp, PCCARE_APP), { recursive: true });
    mkdirSync(join(tmp, PCCARE_PG), { recursive: true });
    mkdirSync(join(tmp, PCCARE_BOARD), { recursive: true });
    mkdirSync(join(tmp, "backend/internal/pccare/domain/sopseed"), { recursive: true });
    mkdirSync(join(tmp, "backend/migrations/postgres"), { recursive: true });
    mkdirSync(join(tmp, "backend/internal/adminui/app"), { recursive: true });
    for (const dir of PHONE_DIRS) mkdirSync(join(tmp, dir), { recursive: true });

    // Adversarial fixtures, one per rule.
    writeFileSync(join(tmp, SEED), '{"a":1}');
    writeFileSync(join(tmp, MIGRATION), "-- the seed is nowhere in here");
    writeFileSync(join(tmp, PCCARE_APP, "service.go"),
      "package app\nfunc f() {\n\tslots := domain.SlotsForCategory(task.Category)\n\tif in.FeedRemovalRequired && in.Category != domain.CategoryDeworming {\n\t\treturn err\n\t}\n\t_ = slots\n}\n");
    writeFileSync(join(tmp, PCCARE_PG, "submit.go"),
      "package postgres\nconst q = `SELECT count(*) FILTER (WHERE video_proof_ref IS NULL) FROM pc_care_task_animals`\n");
    writeFileSync(join(tmp, PCCARE_PG, "rules.go"), "package postgres\nconst r = `SELECT form_dsl FROM sop_versions`\n");
    writeFileSync(join(tmp, PHONE_DIRS[1], "PcCareTaskScreen.kt"),
      'fun slots(category: String) = when (category) {\n  "deworming" -> listOf(PcCareSlot("video"))\n  else -> emptyList()\n}\n');
    writeFileSync(join(tmp, ADMINUI_SERVICE), "package app\n// no pc care page at all\n");

    const bad = check(tmp);
    const rules = new Set(bad.map((b) => b.rule));
    for (const r of [
      "seed-drifted-from-migration",
      "slot-table-read-at-runtime",
      "sop-table-inside-pccare",
      "removal-category-literal",
      "fixed-proof-column-readiness",
      "phone-hardcodes-care-slots",
      "page-copy-registered",
    ]) {
      if (!rules.has(r)) { console.error(`self-test: expected ${r}`, bad); process.exit(1); }
    }

    // The clean half. Each line here is a shape that must NOT be a finding:
    //  - a COMMENT naming a retired rule;
    //  - the inventory_vaccine fridge pair still reading the legacy table;
    //  - the out-of-package pccaresop adapter naming sop_versions;
    //  - the legacy proof columns MIRRORED (written) rather than counted for readiness;
    //  - a phone file that renders the served slots.
    writeFileSync(join(tmp, MIGRATION), "$seed${\"a\":1}$seed$ once, as the v1 insert");
    writeFileSync(join(tmp, PCCARE_APP, "service.go"),
      "package app\n// the retired rule read domain.SlotsForCategory and keyed on domain.CategoryDeworming\nfunc f() { applies, err := rules.RemovalDecision(in.Category, in.FeedRemovalRequested) }\n");
    writeFileSync(join(tmp, PCCARE_APP, "inventory.go"),
      "package app\nfunc g() { if task.Category == domain.CategoryInventoryVaccine { _ = domain.SlotsForCategory(task.Category) } }\n");
    writeFileSync(join(tmp, PCCARE_PG, "submit.go"),
      "package postgres\nconst q = `SELECT count(*) FILTER (WHERE NOT (an.sop_proofs ?& t.required_slot_keys)) FROM pc_care_task_animals an`\n");
    writeFileSync(join(tmp, PCCARE_PG, "animals.go"),
      "package postgres\nconst m = `UPDATE pc_care_task_animals SET video_proof_ref = $1, sop_proofs = sop_proofs || $2`\n");
    writeFileSync(join(tmp, PCCARE_PG, "rules.go"), "package postgres\n// reads no sop_versions row of its own\n");
    mkdirSync(join(tmp, "backend/internal/pccaresop/adapters/postgres"), { recursive: true });
    writeFileSync(join(tmp, "backend/internal/pccaresop/adapters/postgres/rules_source.go"),
      "package postgres\nconst q = `SELECT v.version, v.form_dsl FROM public.sop_versions v`\n");
    writeFileSync(join(tmp, PHONE_DIRS[1], "PcCareTaskScreen.kt"),
      "@Composable fun Slots(state: PcCareTaskUiState) { state.expectedSlots.forEach { SlotRow(it) } }\n");
    writeFileSync(join(tmp, ADMINUI_SERVICE),
      'package app\nvar n = navLeaf("pc-care-sops", "Preventive Care SOP", "/pc-care/sops", nil)\nvar p = page("pc-care-sops", "/pc-care/sops")\nfunc pcCareSOPEditorCopy() map[string]string { return nil }\n');

    const good = check(tmp);
    if (good.length) { console.error("self-test: expected clean, got", good); process.exit(1); }
    console.log("pc-care-sop-guard: self-test passed");
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
    console.error(`pc-care-sop-guard: ${findings.length} finding(s). See docs/decisions/pc-care-sop.md.`);
    process.exit(1);
  }
  console.log("pc-care-sop-guard: ok");
}
