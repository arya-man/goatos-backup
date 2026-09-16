#!/usr/bin/env node
// check-shifting-sop-guard.mjs
//
// SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md). What a raise asks
// for, what a completion must capture and answer, and what a high-priority movement adds are the
// PUBLISHED `shifting` SOP version's cards, pinned per movement at raise. No card the document
// names may be a Go literal, a phone constant or a hand-written table read again.
//
// WHAT IT CHECKS:
//   1. seed-drifted-from-migration    -- the embedded seed document is present verbatim, exactly
//                                        once, in migration 000324 (the in-place add).
//   2. sop-table-inside-counts        -- backend/internal/counts never names sop_versions /
//                                        sop_definitions: the only reader is backend/internal/
//                                        shiftingsop (counts receives rules through its port).
//   3. slot-refusal-outside-judge     -- `ports.ErrShiftingProofSlotInvalid` is MINTED (wrapped or
//                                        returned) ONLY in counts/app/shifting_sop.go, the judge; no
//                                        other production file may refuse a capture by slot on its
//                                        own. Reading it with errors.Is is fine.
//   4. phone-hardcoded-slot-title     -- ShiftingExecute*.kt / ShiftingRaise*.kt hold no seeded
//                                        slot title as a string literal ("Shifting video",
//                                        "Feed packing video", "Feed given to animal video"): the
//                                        phone renders the pinned card's titles.
// BLIND SPOTS: names and strings, not behaviour. A slot list re-derived in a helper under another
// name, or a title split across two literals, slips through -- the judge tests
// (counts/app/shifting_sop_test.go) and the phone tests (ShiftingExecuteSopCardTest) are the
// runtime half.

import { readFileSync, readdirSync, statSync, writeFileSync, mkdtempSync, rmSync, mkdirSync } from "node:fs";
import { join, resolve, basename } from "node:path";
import { tmpdir } from "node:os";

const REPO = resolve(new URL("../..", import.meta.url).pathname);
const MIGRATION = "backend/migrations/postgres/000324_shifting_sop.sql";
const SEED = "backend/internal/counts/domain/sopseed/shifting.json";
const JUDGE_FILE = "backend/internal/counts/app/shifting_sop.go";
const COUNTS_DIR = "backend/internal/counts";
const ANDROID_DIRS = ["apps/goatos-android/app/src/main", "apps/goatos-android/feature/feature-counts/src/main"];

const SOP_TABLE = /\b(sop_versions|sop_definitions)\b/;
// MINTING shapes only: wrapping it into a new error or returning it bare. An errors.Is(...) check
// (the HTTP mapper turning the judge's refusal into 422) is a legitimate reader, not a second judge.
const SLOT_REFUSAL = /(?:fmt\.Errorf\([^)]*\bports\.ErrShiftingProofSlotInvalid\b|return\s+ports\.ErrShiftingProofSlotInvalid\b|OnAbsent:\s*[^,}]*\bports\.ErrShiftingProofSlotInvalid\b)/;
const PHONE_TITLE = /"(Shifting video|Feed packing video|Feed given to animal video)[^"]*"/;
const PHONE_FILE = /^Shifting(Execute|Raise)[A-Za-z]*\.kt$/;

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

// Strip comments so a sentence EXPLAINING a rule is never a finding.
function stripComments(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
}

export function check(root) {
  const findings = [];
  let seed = "";
  let migration = "";
  try { seed = readFileSync(join(root, SEED), "utf8").trim(); } catch { seed = ""; }
  try { migration = readFileSync(join(root, MIGRATION), "utf8"); } catch { migration = ""; }
  if (seed) {
    const marker = "$seed$" + seed + "$seed$";
    const count = migration.split(marker).length - 1;
    if (count !== 1) {
      findings.push({ rule: "seed-drifted-from-migration", file: SEED, detail: `embedded verbatim ${count} time(s) in migration 000324, want 1` });
    }
  }
  for (const f of walk(join(root, COUNTS_DIR))) {
    if (!f.endsWith(".go") || f.endsWith("_test.go")) continue;
    const rel = f.slice(root.length + 1);
    const text = stripComments(readFileSync(f, "utf8"));
    if (SOP_TABLE.test(text)) {
      findings.push({ rule: "sop-table-inside-counts", file: rel, detail: "names a sop_* table; counts reads the shifting cards through ports.ShiftingSOPRulesSource (backend/internal/shiftingsop)" });
    }
    if (rel !== JUDGE_FILE && SLOT_REFUSAL.test(text)) {
      findings.push({ rule: "slot-refusal-outside-judge", file: rel, detail: "mints ErrShiftingProofSlotInvalid outside the judge (counts/app/shifting_sop.go); which capture a movement needs is the pinned card's call" });
    }
  }
  for (const dir of ANDROID_DIRS) {
    for (const f of walk(join(root, dir))) {
      if (!PHONE_FILE.test(basename(f))) continue;
      const rel = f.slice(root.length + 1);
      const text = stripComments(readFileSync(f, "utf8"));
      const m = PHONE_TITLE.exec(text);
      if (m) {
        findings.push({ rule: "phone-hardcoded-slot-title", file: rel, detail: `holds the seeded slot title ${m[0]} as a literal; render the pinned card's titles from the pending-execution item` });
      }
    }
  }
  return findings;
}

function selfTest() {
  const tmp = mkdtempSync(join(tmpdir(), "shifting-sop-guard-"));
  try {
    mkdirSync(join(tmp, "backend/internal/counts/app"), { recursive: true });
    mkdirSync(join(tmp, "backend/internal/counts/domain/sopseed"), { recursive: true });
    mkdirSync(join(tmp, "backend/internal/counts/adapters/postgres"), { recursive: true });
    mkdirSync(join(tmp, "backend/migrations/postgres"), { recursive: true });
    mkdirSync(join(tmp, "apps/goatos-android/feature/feature-counts/src/main/kotlin"), { recursive: true });
    // Adversarial fixtures, one per rule.
    writeFileSync(join(tmp, SEED), '{"a":1}');
    writeFileSync(join(tmp, MIGRATION), "$seed${\"a\":1}$seed$ and again $seed${\"a\":1}$seed$");
    writeFileSync(join(tmp, "backend/internal/counts/adapters/postgres/rules.go"), "package postgres\nconst q = `SELECT form_dsl FROM sop_versions`\n");
    writeFileSync(join(tmp, "backend/internal/counts/app/other.go"), "package app\nfunc f() error { return ports.ErrShiftingProofSlotInvalid }\n");
    writeFileSync(join(tmp, "apps/goatos-android/feature/feature-counts/src/main/kotlin/ShiftingExecuteScreen.kt"), 'val t = "Shifting video (required)"\n');
    const bad = check(tmp);
    const rules = new Set(bad.map((b) => b.rule));
    for (const r of ["seed-drifted-from-migration", "sop-table-inside-counts", "slot-refusal-outside-judge", "phone-hardcoded-slot-title"]) {
      if (!rules.has(r)) { console.error(`self-test: expected ${r}`, bad); process.exit(1); }
    }
    // Clean: seed embedded once; the judge may mint the error; the out-of-package adapter may name
    // the table; a comment explaining a rule is not a finding; a title read from the card is not.
    writeFileSync(join(tmp, MIGRATION), "$seed${\"a\":1}$seed$ once");
    writeFileSync(join(tmp, "backend/internal/counts/adapters/postgres/rules.go"), "package postgres\n// reads no sop_versions row\n");
    writeFileSync(join(tmp, "backend/internal/counts/app/other.go"), "package app\n// the judge returns ports.ErrShiftingProofSlotInvalid\nfunc f(err error) bool { return errors.Is(err, ports.ErrShiftingProofSlotInvalid) }\n");
    writeFileSync(join(tmp, JUDGE_FILE), "package app\nfunc g() error { return ports.ErrShiftingProofSlotInvalid }\n");
    mkdirSync(join(tmp, "backend/internal/shiftingsop/adapters/postgres"), { recursive: true });
    writeFileSync(join(tmp, "backend/internal/shiftingsop/adapters/postgres/rules_source.go"), "package postgres\nconst q = `SELECT form_dsl FROM sop_versions`\n");
    writeFileSync(join(tmp, "apps/goatos-android/feature/feature-counts/src/main/kotlin/ShiftingExecuteScreen.kt"), '// was "Shifting video"\nval t = slot.title\n');
    const good = check(tmp);
    if (good.length) { console.error("self-test: expected clean, got", good); process.exit(1); }
    console.log("shifting-sop-guard: self-test passed");
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
    console.error(`shifting-sop-guard: ${findings.length} finding(s). See docs/decisions/shifting-sop.md.`);
    process.exit(1);
  }
  console.log("shifting-sop-guard: ok");
}
