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
const RE = /\b([a-zA-Z_][\w.]*[Rr]ole)\s*(==|!=)|(==|!=)\s*[A-Za-z_]*Role\.|when\s*\(\s*[a-zA-Z_][\w.]*[Rr]ole\s*\)/;
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
    if (!preview && REASON_RE.test(line)) out.push({ rel, line: i + 1, msg: `hardcoded disabled/blocked reason — a disabled reason is backend-owned; render it from the bootstrap contract (${line.trim().slice(0,70)})` });
  });
  return out;
}
function walk(dir, acc = []) { let ents; try { ents = readdirSync(dir); } catch { return acc; } for (const e of ents) { const p = join(dir, e); const s = statSync(p); if (s.isDirectory()) walk(p, acc); else if (p.endsWith(".kt") && !/test/i.test(p)) acc.push(relative(repo, p)); } return acc; }
function selfTest() {
  const bad = `if (role == Role.OPERATOR) { ShowCapture() }`;
  const good = `if (bootstrap.canCapture) { ShowCapture() }`;
  const ok = scanText("x.kt", bad).length === 1 && scanText("y.kt", good).length === 0;
  console.log(ok ? "mobile-contract self-test: ok" : "mobile-contract self-test: FAIL"); process.exit(ok ? 0 : 1);
}
function scanText(rel, text){ const out=[]; text.split("\n").forEach((line,i)=>{ if(isComment(line)||line.includes("mobile-contract:ignore:"))return; if(RE.test(line)) out.push({rel,line:i+1}); }); return out; }
const mode = process.argv[2];
if (mode === "--self-test") selfTest();
let targets;
if (mode === "--all") { targets = ROOTS.flatMap((r) => walk(resolve(repo, r))); }
else { const base = process.env.MOBILE_CONTRACT_BASE || "origin/main"; try { targets = execSync(`git -C "${repo}" diff --name-only ${base}...HEAD`, {encoding:"utf8"}).split("\n").map(s=>s.trim()).filter(s=>s.endsWith(".kt") && ROOTS.some(r=>s.startsWith(r)) && !/test/i.test(s)); } catch { targets = []; } }
if (!targets.length) { console.log("mobile-contract: ok (no mobile UI files changed)"); process.exit(0); }
const all = targets.flatMap(scan);
if (all.length) { console.error("mobile-contract-ownership-guard FAILED — backend contract owns visibility; mobile must not gate on role:"); all.forEach(f=>console.error(`  ${f.rel}:${f.line}  ${f.msg}`)); process.exit(1); }
console.log(`mobile-contract: ok (${targets.length} mobile UI file(s) scanned; 0 role-gating)`);
