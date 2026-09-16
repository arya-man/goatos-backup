#!/usr/bin/env node
// check-weighing-sop-guard.mjs
//
// WEIGHING SOP (maintainer decision 2026-09-15, docs/decisions/weighing-sop.md). The rules a
// weighing task is planned on and runs under -- the capture modes offered, the default cap,
// whether the evening-before feed & water removal is required / optional / off, the removal
// card's copy and questions, the lump-sum video window -- come from the PUBLISHED
// weighing.session SOP version, pinned per task. No rule the document names may be a Go
// literal, a phone constant or a web string again.
//
// WHAT IT CHECKS:
//   1. seed-drifted-from-migration   -- the embedded seed document is present verbatim in the
//                                       migration that adds it (twice: fresh insert + in-place add).
//   2. cap-literal-in-service         -- no production Go under weighing/app assigns the planned
//                                       cap from a literal (`cmd.PlannedCapPerDay = 100`): the
//                                       default is the SOP's.
//   3. removal-required-outside-rules -- `ports.ErrFastingOperatorRequired` is returned ONLY from
//                                       app/sop_rules.go, where the rules decide whether the
//                                       task carries the removal; nowhere else may demand it.
//   4. sop-table-inside-weighing      -- backend/internal/weighing never names sop_versions /
//                                       sop_definitions: the only reader is backend/internal/
//                                       weighingsop (weighing stays ISOLATED).
//   5. phone-lump-sum-literal-policy  -- the phone's lump-sum proof policy must not be built from
//                                       the MAX_SHED_GROUP_VIDEOS constant directly; it reads the
//                                       task's pinned window.
// BLIND SPOTS: names and strings, not behaviour. A cap literal under another name, or a rule
// re-derived in a helper, slips through -- the service tests (sop_rules_test.go) and the wizard
// tests (WeighingPlanWizardSopRulesTest) are the runtime half.

import { readFileSync, readdirSync, statSync, writeFileSync, mkdtempSync, rmSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";

const REPO = resolve(new URL("../..", import.meta.url).pathname);
const MIGRATION = "backend/migrations/postgres/000315_weighing_sop_rules.sql";
const SEED = "backend/internal/weighing/domain/sopseed/weighing_session.json";
const RULES_FILE = "backend/internal/weighing/app/sop_rules.go";
const WEIGHING_APP = "backend/internal/weighing/app";
const WEIGHING_DIR = "backend/internal/weighing";
const CAPTURE_VM = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/WeighingViewModel.kt";

const CAP_LITERAL = /\bPlannedCapPerDay\s*=\s*\d+\b/;
const REMOVAL_REQUIRED = /\bports\.ErrFastingOperatorRequired\b/;
const SOP_TABLE = /\b(sop_versions|sop_definitions)\b/;
const PHONE_POLICY_LITERAL = /maximumCount(?:PerSubject)?\s*=\s*MAX_SHED_GROUP_VIDEOS\b/;

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

export function check(root) {
  const findings = [];
  let seed = "";
  let migration = "";
  try { seed = readFileSync(join(root, SEED), "utf8").trim(); } catch { seed = ""; }
  try { migration = readFileSync(join(root, MIGRATION), "utf8"); } catch { migration = ""; }
  if (seed) {
    const marker = "$seed$" + seed + "$seed$";
    const count = migration.split(marker).length - 1;
    if (count !== 2) {
      findings.push({ rule: "seed-drifted-from-migration", file: SEED, detail: `embedded verbatim ${count} time(s) in migration 000315, want 2` });
    }
  }
  for (const f of walk(join(root, WEIGHING_DIR))) {
    if (!f.endsWith(".go") || f.endsWith("_test.go")) continue;
    const rel = f.slice(root.length + 1);
    const text = goCode(readFileSync(f, "utf8"));
    if (SOP_TABLE.test(text)) {
      findings.push({ rule: "sop-table-inside-weighing", file: rel, detail: "names a sop_* table; weighing reads the rules through ports.SOPRulesSource (backend/internal/weighingsop)" });
    }
    if (!rel.startsWith(WEIGHING_APP + "/")) continue;
    if (CAP_LITERAL.test(text)) {
      findings.push({ rule: "cap-literal-in-service", file: rel, detail: "assigns the planned cap from a literal; the default is the SOP's planning.default_cap_per_day" });
    }
    if (rel !== RULES_FILE && REMOVAL_REQUIRED.test(text)) {
      findings.push({ rule: "removal-required-outside-rules", file: rel, detail: "demands the removal operator outside sop_rules.go; whether a task carries the removal is the SOP's call" });
    }
  }
  try {
    const kt = readFileSync(join(root, CAPTURE_VM), "utf8").replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
    if (PHONE_POLICY_LITERAL.test(kt)) {
      findings.push({ rule: "phone-lump-sum-literal-policy", file: CAPTURE_VM, detail: "builds the lump-sum proof policy from MAX_SHED_GROUP_VIDEOS; read the task's pinned video window" });
    }
  } catch {
    // No capture VM in this tree (a backend-only checkout): nothing to check.
  }
  return findings;
}

function selfTest() {
  const tmp = mkdtempSync(join(tmpdir(), "weighing-sop-guard-"));
  try {
    mkdirSync(join(tmp, WEIGHING_APP), { recursive: true });
    mkdirSync(join(tmp, "backend/internal/weighing/domain/sopseed"), { recursive: true });
    mkdirSync(join(tmp, "backend/internal/weighing/adapters/postgres"), { recursive: true });
    mkdirSync(join(tmp, "backend/migrations/postgres"), { recursive: true });
    mkdirSync(join(tmp, "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel"), { recursive: true });
    // Adversarial fixtures, one per rule.
    writeFileSync(join(tmp, SEED), '{"a":1}');
    writeFileSync(join(tmp, MIGRATION), "$seed${\"a\":1}$seed$ only once");
    writeFileSync(join(tmp, WEIGHING_APP, "service.go"), "package app\nfunc f() {\n\tif cmd.PlannedCapPerDay <= 0 {\n\t\tcmd.PlannedCapPerDay = 100\n\t}\n\treturn ports.ErrFastingOperatorRequired\n}\n");
    writeFileSync(join(tmp, "backend/internal/weighing/adapters/postgres/rules.go"), "package postgres\nconst q = `SELECT form_dsl FROM sop_versions`\n");
    writeFileSync(join(tmp, CAPTURE_VM), "val p = ProofPolicy(maximumCount = MAX_SHED_GROUP_VIDEOS)\n");
    const bad = check(tmp);
    const rules = new Set(bad.map((b) => b.rule));
    for (const r of ["seed-drifted-from-migration", "cap-literal-in-service", "removal-required-outside-rules", "sop-table-inside-weighing", "phone-lump-sum-literal-policy"]) {
      if (!rules.has(r)) { console.error(`self-test: expected ${r}`, bad); process.exit(1); }
    }
    // A comment explaining a rule is not a finding; the rules file may name the error; the
    // out-of-package adapter may name the table; a seed embedded twice is clean.
    writeFileSync(join(tmp, MIGRATION), "$seed${\"a\":1}$seed$ and $seed${\"a\":1}$seed$");
    writeFileSync(join(tmp, WEIGHING_APP, "service.go"), "package app\n// the old literal was cmd.PlannedCapPerDay = 100 and ports.ErrFastingOperatorRequired\nfunc f() { cmd.PlannedCapPerDay = rules.Planning.DefaultCapPerDay }\n");
    writeFileSync(join(tmp, WEIGHING_APP, "sop_rules.go"), "package app\nfunc g() error { return ports.ErrFastingOperatorRequired }\n");
    writeFileSync(join(tmp, "backend/internal/weighing/adapters/postgres/rules.go"), "package postgres\n// reads no sop_versions row\n");
    mkdirSync(join(tmp, "backend/internal/weighingsop/adapters/postgres"), { recursive: true });
    writeFileSync(join(tmp, "backend/internal/weighingsop/adapters/postgres/rules_source.go"), "package postgres\nconst q = `SELECT form_dsl FROM sop_versions`\n");
    writeFileSync(join(tmp, CAPTURE_VM), "val p = ProofPolicy(maximumCount = videoMax)\n");
    const good = check(tmp);
    if (good.length) { console.error("self-test: expected clean, got", good); process.exit(1); }
    console.log("weighing-sop-guard: self-test passed");
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
    console.error(`weighing-sop-guard: ${findings.length} finding(s). See docs/decisions/weighing-sop.md.`);
    process.exit(1);
  }
  console.log("weighing-sop-guard: ok");
}
