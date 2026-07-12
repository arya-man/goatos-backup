#!/usr/bin/env node

// check-nav-composition.mjs — blocks the "hardcoded per-role / per-module nav template"
// anti-pattern. Navigation (nav bar, bottom-bar icons/labels, screens) must be COMPOSED from
// the person's granted modules (department_module_grants) and reused across modules, not a fixed
// literal list that names a specific vertical/module. See
// docs/decisions/role-module-nav-composition.md.
//
// Modes:
//   (default)     diff-scoped: scan only nav-relevant files changed vs $NAV_GUARD_BASE (or origin/main).
//                 Nothing relevant changed -> PASS instantly.
//   --all         audit the whole nav surface (backlog view; used by `make nav-composition-guard`).
//   --self-test   run the built-in fixtures and exit.
//
// Escape hatch: a genuinely-fixed system nav item (e.g. global You/Settings) may append
// `nav-composition:ignore: <reason>` on the line.

import { execSync } from "node:child_process";
import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// Files that OWN navigation composition. New hardcoded nav templates elsewhere are caught too,
// but these are the authoritative nav builders scanned in --all mode.
const NAV_FILES = [
  "backend/internal/workforce/app/bootstrap_copy.go",
];

// Known module/vertical routes — hardcoding one of these into a nav-item literal in a nav builder
// is the smell (nav must come from the module-grant registry, not a literal vertical route).
const MODULE_ROUTES = [
  "vaccination", "feed", "feed-direction", "breeding", "procurement",
  "health", "growth", "infrastructure", "milk", "sales", "deworming",
];

// Baselined existing offenders (tracked debt for the nav-generalization; replaced by the registry).
// name -> reason. New hardcoded nav templates beyond these FAIL.
const BASELINE = {
  operatorNavigation: "nav-generalization: replace with module-grant registry composition (expires 2026-09-30)",
  leadershipNavigation: "nav-generalization: replace with module-grant registry composition (expires 2026-09-30)",
};

const TEMPLATE_DECL = /\b(\w+Navigation)\s*=\s*\[\]navigationTemplate\s*\{/;
const HARDCODED_MODULE_HREF = new RegExp(
  `href:\\s*"/(?:${MODULE_ROUTES.join("|")})(?:/|")`
);

function findings(rel, text) {
  const out = [];
  const lines = text.split("\n");
  lines.forEach((line, i) => {
    if (line.includes("nav-composition:ignore:")) return;
    const decl = line.match(TEMPLATE_DECL);
    if (decl && !(decl[1] in BASELINE)) {
      out.push({ rel, line: i + 1, msg: `hardcoded nav template '${decl[1]}' — compose nav from module grants (registry), do not declare a fixed per-role/per-module nav array` });
    }
    if (HARDCODED_MODULE_HREF.test(line)) {
      // only flag inside a nav template context (heuristic: line has a nav-item literal)
      if (/href:\s*"/.test(line) && /(key:|labelKey:)/.test(line)) {
        // allowed if the surrounding declared var is baselined — approximate by skipping the 2 known files' baselined vars
        out.push({ rel, line: i + 1, msg: `nav item hardcodes a module/vertical route — route must come from the module-grant registry, not a literal (${line.trim().slice(0, 80)})` });
      }
    }
  });
  return out;
}

// Suppress findings that fall inside a baselined var block (operator/leadershipNavigation)
function filterBaselined(rel, text, raw) {
  const lines = text.split("\n");
  const baseRanges = [];
  let cur = null;
  lines.forEach((line, i) => {
    const d = line.match(TEMPLATE_DECL);
    if (d && d[1] in BASELINE) cur = { start: i, end: null };
    if (cur && cur.end === null && line.trim() === "}") { cur.end = i; baseRanges.push(cur); cur = null; }
  });
  return raw.filter((f) => !baseRanges.some((r) => f.line - 1 > r.start && f.line - 1 <= r.end));
}

function scan(rel) {
  const abs = resolve(repo, rel);
  if (!existsSync(abs)) return [];
  const text = readFileSync(abs, "utf8");
  return filterBaselined(rel, text, findings(rel, text));
}

function selfTest() {
  const bad = `var feedNavigation = []navigationTemplate{\n {key:"feed", labelKey:"nav.feed", href:"/feed"},\n}`;
  const good = `func visibleNavigationFor(mods []Module) []Nav { return composeFromModuleGrants(mods) }`;
  const f1 = filterBaselined("x.go", bad, findings("x.go", bad));
  const f2 = filterBaselined("y.go", good, findings("y.go", good));
  const ok = f1.length >= 1 && f2.length === 0;
  console.log(ok ? "nav-composition self-test: ok" : `nav-composition self-test: FAIL (bad=${f1.length} good=${f2.length})`);
  process.exit(ok ? 0 : 1);
}

function changedNavFiles() {
  const base = process.env.NAV_GUARD_BASE || "origin/main";
  try {
    const out = execSync(`git -C "${repo}" diff --name-only ${base}...HEAD`, { encoding: "utf8" });
    return out.split("\n").map((s) => s.trim()).filter((s) => s.endsWith(".go") && /nav|bootstrap/i.test(s));
  } catch { return NAV_FILES; }
}

const mode = process.argv[2];
if (mode === "--self-test") selfTest();
const files = mode === "--all" ? NAV_FILES : [...new Set([...changedNavFiles(), ...NAV_FILES.filter(() => mode === "--all")])];
const targets = mode === "--all" ? NAV_FILES : changedNavFiles();
if (targets.length === 0) { console.log("nav-composition: ok (no nav files changed)"); process.exit(0); }
const all = targets.flatMap(scan);
if (all.length) {
  console.error("nav-composition-guard FAILED — hardcoded per-role/per-module nav (compose from module grants; see docs/decisions/role-module-nav-composition.md):");
  all.forEach((f) => console.error(`  ${f.rel}:${f.line}  ${f.msg}`));
  process.exit(1);
}
console.log(`nav-composition: ok (${targets.length} nav file(s) scanned; baselined offenders: ${Object.keys(BASELINE).join(", ")})`);
