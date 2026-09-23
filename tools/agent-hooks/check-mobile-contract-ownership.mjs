#!/usr/bin/env node
// check-mobile-contract-ownership.mjs — enforces the golden rule that the BACKEND contract owns
// what the mobile user sees. The clean, machine-enforceable slice: the mobile UI/VM layer must NOT
// decide visibility/affordances by role (`role ==`). Visibility comes from the backend-composed
// bootstrap/nav/actions contract. See AGENTS.md golden frontend rule + docs/decisions/role-module-nav-composition.md.
// Modes: (default) diff-scoped vs $MOBILE_CONTRACT_BASE|origin/main · --all whole tree · --self-test.
// Escape hatch: `mobile-contract:ignore: <reason>` on the line.
import { execSync } from "node:child_process";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
const repo = resolve(import.meta.dirname, "../..");
const ROOTS = ["apps/goatos-android/app/src/main", "apps/goatos-android/feature"];
// role-gating in UI/VM: `role ==`, `role !=`, `== Role.X`, `when (role)`. Comments excluded.
// `[a-zA-Z_][\w.]*[Rr]ole` required at least one character BEFORE the suffix, so
// the plainest spelling of the defect — a bare `role ==` — never matched. Measured
// 2026-09-23: `private fun careless(role: String) = role == "pc_director"` in a
// scanned mobile .kt exited 0 in both diff mode and --all, while `userRole ==` in
// the same file exited 1. The self-test only used `role == Role.OPERATOR`, which
// matched a DIFFERENT alternative, so it stayed green over the documented case.
// `[\w.]*` (not `[a-zA-Z_][\w.]*`) makes the prefix optional; the leading \b still
// stops it matching inside `payroll ==` or `myrole_id ==`.
const RE = /\b([\w.]*[Rr]ole)\s*(==|!=)|(==|!=)\s*[A-Za-z_]*Role\.|when\s*\(\s*[\w.]*[Rr]ole\s*\)/;
const WEIGHING_ROLE_ROUTING_PATTERNS = [
  /\bisWeighingLeadershipRole\b/,
  /\bleadershipWeighing\b/,
  /pc_director.*WEIGHING/i,
  /WEIGHING.*pc_director/i,
  /director_growth/i,
];
const hasWeighingRoleRouting = (line) => WEIGHING_ROLE_ROUTING_PATTERNS.some((pattern) => pattern.test(line));
// hardcoded disabled/blocked reason literal — a disabled reason is backend-owned (golden rule).
const REASON_RE = /\b(disabledReason|blockedReason|disabled_reason|blockReason)\s*=\s*"[^"]/;
// preview/sample/debug sources may hold literal reasons for @Preview — not production truth.
const isPreviewSrc = (rel) => /(Sample|Preview|Screenshot|\/src\/debug\/)/i.test(rel);
const isComment = (l) => { const t = l.trim(); return t.startsWith("//") || t.startsWith("*") || t.startsWith("/*"); };
function scan(rel) {
  const abs = resolve(repo, rel); let text; try { text = readFileSync(abs, "utf8"); } catch { return []; }
  const out = [];
  const preview = isPreviewSrc(rel);
  text.split("\n").forEach((line, i) => {
    if (isComment(line) || line.includes("mobile-contract:ignore:")) return;
    if (RE.test(line)) out.push({ rel, line: i + 1, msg: `mobile UI decides visibility by role — gate on the backend-composed contract, not \`role ==\` (${line.trim().slice(0,70)})` });
    if (hasWeighingRoleRouting(line)) out.push({ rel, line: i + 1, msg: `mobile weighing routing must come from backend feature_flags/nav, not role-label branches (${line.trim().slice(0,70)})` });
    if (!preview && REASON_RE.test(line)) out.push({ rel, line: i + 1, msg: `hardcoded disabled/blocked reason — a disabled reason is backend-owned; render it from the bootstrap contract (${line.trim().slice(0,70)})` });
  });
  return out;
}
function walk(dir, acc = []) { let ents; try { ents = readdirSync(dir); } catch { return acc; } for (const e of ents) { const p = join(dir, e); const s = statSync(p); if (s.isDirectory()) walk(p, acc); else if (p.endsWith(".kt") && !/test/i.test(p)) acc.push(relative(repo, p)); } return acc; }
function selfTest() {
  const bad = `if (role == Role.OPERATOR) { ShowCapture() }`;
  const badWeighing = `val leadershipWeighing = isWeighingLeadershipRole(profile.roleLabel)`;
  const good = `if (bootstrap.canCapture) { ShowCapture() }`;
  // The plainest spelling of the defect, and the one this guard missed for as
  // long as it did: a BARE `role`, with no prefix and no Role.X on the right.
  // `role == Role.OPERATOR` above matched a different alternative of the regex,
  // so it kept the self-test green while the documented case was undetectable.
  const bareRole = `private fun careless(role: String) = role == "pc_director"`;
  const bareRoleNe = `if (role != "verifier") { deny() }`;
  const bareRoleWhen = `when (role) { "ceo" -> Full() else -> None() }`;
  // And the words that must NOT be mistaken for it.
  const notRole = `val net = payroll != 0 && controller == 3`;
  const cases = [
    ["bare role ==", scanText("bare.kt", bareRole).length === 1],
    ["bare role !=", scanText("bare2.kt", bareRoleNe).length === 1],
    ["when (role)", scanText("bare3.kt", bareRoleWhen).length === 1],
    ["payroll/controller are not roles", scanText("no.kt", notRole).length === 0],
    ["role == Role.X", scanText("x.kt", bad).length === 1],
    ["roleLabel helper", scanText("w.kt", badWeighing).length === 1],
    ["capability check is clean", scanText("y.kt", good).length === 0],
  ];
  const ok = cases.every(([, pass]) => pass);
  for (const [name, pass] of cases) if (!pass) console.log(`  FAIL ${name}`);
  console.log(ok ? "mobile-contract self-test: ok" : "mobile-contract self-test: FAIL"); process.exit(ok ? 0 : 1);
}
function scanText(rel, text){ const out=[]; text.split("\n").forEach((line,i)=>{ if(isComment(line)||line.includes("mobile-contract:ignore:"))return; if(RE.test(line)||hasWeighingRoleRouting(line)) out.push({rel,line:i+1}); }); return out; }
const mode = process.argv[2];
if (mode === "--self-test") selfTest();
let targets;
if (mode === "--all") { targets = ROOTS.flatMap((r) => walk(resolve(repo, r))); }
else {
  // Committed + staged + unstaged, the same three sources changed_since_base()
  // uses in run-local-ci.sh. `<base>...HEAD` alone could not see a file an agent
  // had just written, which is the state this guard's editor hook runs in.
  const base = process.env.MOBILE_CONTRACT_BASE || "origin/main";
  const found = new Set();
  for (const args of [`diff --name-only ${base}...HEAD`, "diff --name-only --cached", "diff --name-only"]) {
    try {
      for (const line of execSync(`git -C "${repo}" ${args}`, { encoding: "utf8" }).split("\n")) {
        const t = line.trim();
        if (t) found.add(t);
      }
    } catch { /* a source that does not resolve contributes nothing */ }
  }
  targets = [...found].filter((s) => s.endsWith(".kt") && ROOTS.some((r) => s.startsWith(r)) && !/test/i.test(s));
}
if (!targets.length) { console.log("mobile-contract: ok (no mobile UI files changed)"); process.exit(0); }
const all = targets.flatMap(scan);
if (all.length) { console.error("mobile-contract-ownership-guard FAILED — backend contract owns visibility; mobile must not gate on role:"); all.forEach(f=>console.error(`  ${f.rel}:${f.line}  ${f.msg}`)); process.exit(1); }
console.log(`mobile-contract: ok (${targets.length} mobile UI file(s) scanned; 0 role-gating)`);
