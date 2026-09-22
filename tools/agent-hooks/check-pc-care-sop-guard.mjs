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
const MIGRATION = "backend/migrations/postgres/000386_pc_care_sop.sql";
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


// ---- 8. The page's OWN copy map carries every key its screens read ------------------------
//
// A key declared SOMEWHERE in service.go is not a key this page serves: `copy()` throws on the
// page CONTRACT, and each SOP page merges its own editor map. On 2026-09-22 the Preventive Care
// editor rendered the SHARED capture card (features/sops/feed-editor.tsx `SlotCard`), which names
// `fsop.proof.title` / `.hint` / `.remove` -- declared, but only inside feedSOPEditorCopy(), which
// this page does not merge. The whole editor threw behind the error boundary ("Something went
// wrong") while the repo-wide copy-keys test stayed green, because the key existed. So this rule
// resolves the PAGE's merged map and checks the keys the PAGE's screens actually read, the
// imported shared cards included.
const PC_CARE_SCREENS = "apps/admin-web/features/sops";
// Keys composed at runtime from a closed vocabulary; the screens never write them as literals.
const PC_CARE_DYNAMIC_KEYS = ["video", "photo", "either"].map((k) => `wsop.proof.kind.${k}`)
  .concat(["choice", "multi", "text", "number"].map((k) => `inspection.kind.${k}`));


/** Every key a Go copy function declares, following the maps it merges in. */
function goCopyFnKeys(adminui, fn, seen = new Set()) {
  if (seen.has(fn)) return new Set();
  seen.add(fn);
  const at = adminui.indexOf(`func ${fn}() map[string]string {`);
  if (at < 0) return new Set();
  // Bounded STRUCTURALLY (to the function's closing brace at column 0), never by counting
  // braces: Go copy strings carry "{n}" placeholders and a lone "}" would run the slice into
  // the next function, which is how this rule first read another page's map as its own.
  const end = adminui.indexOf("\n}\n", at);
  const body = adminui.slice(at, end < 0 ? adminui.length : end);
  const keys = new Set([...body.matchAll(/"([a-z0-9_.]+)"\s*:/g)].map((m) => m[1]));
  for (const m of body.matchAll(/range (\w+)\(\)/g)) for (const k of goCopyFnKeys(adminui, m[1], seen)) keys.add(k);
  return keys;
}

/** The keys the /pc-care/sops page contract actually serves. */
function pcCarePageKeys(adminui) {
  const shared = adminui.indexOf('case "counts-sops",');
  if (shared < 0) return null;
  // The shared map runs from the outer case to the first per-page case inside it.
  let sharedEnd = adminui.indexOf('\n\t\tcase "', shared);
  if (sharedEnd < 0) sharedEnd = adminui.length;
  const keys = new Set([...adminui.slice(shared, sharedEnd).matchAll(/"([a-z0-9_.]+)"\s*:/g)].map((m) => m[1]));
  const own = adminui.indexOf('case "pc-care-sops":', shared);
  if (own < 0) return keys;
  let end = adminui.indexOf("\n\t\tcase \"", own + 8);
  if (end < 0) end = adminui.length;
  const block = adminui.slice(own, end);
  for (const m of block.matchAll(/m\["([a-z0-9_.]+)"\]/g)) keys.add(m[1]);
  for (const m of block.matchAll(/range (\w+)\(\)/g)) for (const k of goCopyFnKeys(adminui, m[1])) keys.add(k);
  return keys;
}

/**
 * The text of one exported TSX function, from its signature to the next top-level declaration.
 * NOT by brace count: a destructured parameter list opens a brace of its own, so counting from
 * the signature returns the PARAMETERS and never sees the body -- which made this rule vacuous
 * the first time it was written.
 */
function tsxFnBody(src, name) {
  const at = src.search(new RegExp(`export function ${name}\\b`));
  if (at < 0) return "";
  const rest = src.slice(at + 1);
  const next = rest.search(/\nexport (?:function|const|type) |\nfunction /);
  return next < 0 ? rest : rest.slice(0, next);
}

/** Every literal copy key the pc-care screens read, the shared cards they import included. */
function pcCareScreenKeys(root) {
  const dir = join(root, PC_CARE_SCREENS);
  let names = [];
  try { names = readdirSync(dir); } catch { return []; }
  const out = [];
  for (const name of names.filter((n) => /^pc-care-.*\.tsx?$/.test(n) && !n.includes(".test."))) {
    const src = readFileSync(join(dir, name), "utf8");
    for (const m of src.matchAll(/\bcopy\(\s*\w+\s*,\s*"([a-z0-9_.]+)"/g)) out.push({ file: `${PC_CARE_SCREENS}/${name}`, key: m[1] });
    // A shared card imported here reads ITS OWN keys on THIS page's contract.
    for (const imp of src.matchAll(/import \{([^}]+)\} from "\.\/([a-z-]+)"/g)) {
      let shared = "";
      try { shared = readFileSync(join(dir, `${imp[2]}.tsx`), "utf8"); } catch { continue; }
      for (const sym of imp[1].split(",").map((s) => s.trim()).filter((s) => /^[A-Z]/.test(s))) {
        for (const m of tsxFnBody(shared, sym).matchAll(/\bcopy\(\s*\w+\s*,\s*"([a-z0-9_.]+)"/g)) {
          out.push({ file: `${PC_CARE_SCREENS}/${imp[2]}.tsx:${sym}`, key: m[1] });
        }
      }
    }
  }
  return out;
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

  // 8. Every key the page's screens read is in the page's OWN merged map.
  if (adminui) {
    const served = pcCarePageKeys(adminui);
    if (served) {
      for (const k of PC_CARE_DYNAMIC_KEYS) served.add(k);
      const seen = new Set();
      for (const { file, key } of pcCareScreenKeys(root)) {
        if (served.has(key) || seen.has(key)) continue;
        seen.add(key);
        findings.push({
          rule: "page-copy-key-not-served",
          file,
          detail: `reads ${key}, which /pc-care/sops does not serve; copy() throws and the whole editor renders the error boundary`,
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
    // No page registration at all (rule 7), and a copy map that serves the page's own key
    // while the SHARED capture card the editor imports reads one it does not (rule 8) --
    // the exact 2026-09-22 defect: the key existed in the file, in ANOTHER page's map.
    writeFileSync(join(tmp, ADMINUI_SERVICE), 'package app\nfunc pageSpecificCopy(id string) map[string]string {\n\tswitch id {\n\tcase "counts-sops", "pc-care-sops":\n\t\tm := map[string]string{\n\t\t\t"crumb": "SOPs",\n\t\t}\n\t\tswitch id {\n\t\tcase "pc-care-sops":\n\t\t\tfor k, v := range pcCareCopy() {\n\t\t\t\tm[k] = v\n\t\t\t}\n\t\t}\n\t\treturn m\n\t}\n\treturn nil\n}\n\nfunc pcCareCopy() map[string]string {\n\treturn map[string]string{"pcsop.title": "Preventive Care SOP"}\n}\n\nfunc feedCopy() map[string]string {\n\treturn map[string]string{"fsop.proof.remove": "Remove this capture"}\n}\n');
    mkdirSync(join(tmp, PC_CARE_SCREENS), { recursive: true });
    writeFileSync(join(tmp, PC_CARE_SCREENS, "feed-editor.tsx"), 'export function SlotCard({ pc }) {\n  return <button aria-label={copy(pc, "fsop.proof.remove")} />;\n}\n');
    writeFileSync(join(tmp, PC_CARE_SCREENS, "pc-care-editor.tsx"), 'import { SlotCard } from "./feed-editor";\nexport function PcCareEditor({ pc }) {\n  return <h1>{copy(pc, "pcsop.title")}</h1>;\n}\n');

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
      "page-copy-key-not-served",
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
    writeFileSync(join(tmp, ADMINUI_SERVICE), 'package app\nvar n = navLeaf("pc-care-sops", "Preventive Care SOP", "/pc-care/sops", nil)\nvar p = page("pc-care-sops", "/pc-care/sops")\n\nfunc pageSpecificCopy(id string) map[string]string {\n\tswitch id {\n\tcase "counts-sops", "pc-care-sops":\n\t\tm := map[string]string{\n\t\t\t"crumb": "SOPs",\n\t\t}\n\t\tswitch id {\n\t\tcase "pc-care-sops":\n\t\t\tfor k, v := range pcCareSOPEditorCopy() {\n\t\t\t\tm[k] = v\n\t\t\t}\n\t\t}\n\t\treturn m\n\t}\n\treturn nil\n}\n\nfunc pcCareSOPEditorCopy() map[string]string {\n\treturn map[string]string{\n\t\t"pcsop.title":       "Preventive Care SOP",\n\t\t"fsop.proof.remove": "Remove this capture",\n\t}\n}\n');

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
