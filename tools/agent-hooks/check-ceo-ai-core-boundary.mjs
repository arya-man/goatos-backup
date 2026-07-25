#!/usr/bin/env node

// check-ceo-ai-core-boundary.mjs — enforces the ceo_ai reporting/assistant
// boundary: the leadership-assistant namespace (`ceo_ai` SQL schema + ceo_ai_*
// assistant tables, and the `/api/ceo-ai` client surface) must NEVER sit
// between the core Backend <-> Frontend <-> Mobile operator layers.
//
// Direction of allowed dependency:
//   core BE  -> DB / read models                (operator source of truth)
//   assistant (ceoai) -> reads core data via Mesha read APIs / MCP / read-only SQL
// NEVER: a core operator read path (processintegrity/Control Tower, calendar,
// vaccination, counts, ...) reaching INTO `ceo_ai.*` for its own runtime data,
// and NEVER a core FE/mobile screen routing its data through `/api/ceo-ai/*`.
//
// This exact anti-pattern shipped once: the Control Tower query joined
// `ceo_ai.vaccine_label_for(...)` purely for a display label, so dropping the
// reporting schema 500'd a core operator screen (SQLSTATE 3F000). Label
// composition now lives in Go (vaccination/domain.DoseDisplayLabel). See
// docs/decisions/ceo-ai-reporting-boundary.md.
//
// Two enforcement kinds:
//   - "sql"    (backend/internal/** except ceoai/**): no SQL access to the
//              `ceo_ai` schema or `ceo_ai_*` tables — a schema-qualified
//              function/view call, or FROM/JOIN/INTO/UPDATE against ceo_ai
//              (matched across newlines so split `FROM\n ceo_ai.x` is caught).
//   - "client" (apps/admin-web/**, apps/goatos-android/** except assistant-owned
//              dirs + the global assistant-bubble chrome): no `/api/ceo-ai` /
//              `/ceo-ai/` data routing and no import of an assistant client
//              module into a core screen.
// Comment-only lines, test files, and generated/migration dirs are exempt.
// Cube models and backend/migrations legitimately own ceo_ai and are out of scope.
//
// Modes:
//   (default)     scan the tree, fail on any violation.
//   --self-test   run the built-in adversarial fixtures.
//
// Exception (must be COMPLETE — owner, issue, scope, expiry all present):
//   ceo-ai-boundary:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// SQL access to the ceo_ai namespace. `\s+` spans newlines, so split
// `FROM\n  ceo_ai.view` and `LEFT JOIN\n ceo_ai_table` are caught by a
// whole-source (not per-line) scan.
const SQL_RES = [
  /\bceo_ai\.[a-z_][a-z0-9_]*\s*\(/gi, // ceo_ai.vaccine_label_for(
  /\b(?:from|join|into|update)\s+ceo_ai[._][a-z0-9_]+/gi, // FROM/JOIN/INTO/UPDATE ceo_ai(.|_)...
];

// Client-layer coupling to the assistant surface.
const CLIENT_RES = [
  // `/api/ceo-ai` is an unambiguous assistant route: the `api/ceo-ai` sequence
  // never appears in a feature import path like `@/features/ceo-ai`. Matches
  // anywhere in a string/template, incl. `${base}/api/ceo-ai/ask`.
  /\/api\/ceo-ai(?:\/|["'`])/gi,
  // A bare `/ceo-ai/` route only at a string/template boundary (preceded by a
  // quote, backtick, or `}` from `${...}`) — so `features/ceo-ai/` (a module
  // path, caught by the import regex instead) does NOT false-positive.
  /(?<=["'`}])\/ceo-ai(?:\/|["'`])/gi,
  // Assistant client-module import: "@/.../ceo-ai-chat" | ".../features/ceo-ai/..." | barrel "@/features/ceo-ai"
  /\b(?:import|from|require)\b[^\n;]*['"][^'"\n]*\/ceo-ai(?:[-/]|['"`])/gi,
];

const IGNORE_RE =
  /ceo-ai-boundary:ignore:\s*owner=\S+\s+issue=\S+\s+scope=\S+\s+expiry=\d{4}-\d{2}-\d{2}/;

// Core runtime roots, each with the assistant-owned prefixes allowed to
// reference ceo_ai, and the enforcement kind.
const SCAN_ROOTS = [
  { root: "backend/internal/", kind: "sql", allow: ["backend/internal/ceoai/"] },
  {
    root: "apps/admin-web/",
    kind: "client",
    allow: [
      "apps/admin-web/app/api/ceo-ai/",
      "apps/admin-web/app/(admin)/ceo-ai-admin/", // assistant admin route page
      "apps/admin-web/features/ceo-ai/",
      "apps/admin-web/features/ceo-ai-admin/",
      "apps/admin-web/components/ceo-ai-chat", // the assistant chat client component
      "apps/admin-web/components/mesha-shell", // global chrome that mounts the bubble
      "apps/admin-web/lib/ceo-ai-stream", // assistant SSE stream client helper
    ],
  },
  {
    root: "apps/goatos-android/",
    kind: "client",
    allow: [
      "apps/goatos-android/app/src/main/java/sg/mesha/goatos/feature/assistant/",
      "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/feature/assistant/",
      "apps/goatos-android/feature/feature-leadership/",
    ],
  },
];

const SCAN_EXT = /\.(go|sql|ts|tsx|js|jsx|mjs|kt)$/;

function isCommentLine(line) {
  const t = (line ?? "").trim();
  return (
    t.startsWith("//") ||
    t.startsWith("--") ||
    t.startsWith("*") ||
    t.startsWith("/*") ||
    t.startsWith("#")
  );
}

function isExcluded(rel) {
  if (rel.endsWith("_test.go")) return true;
  if (/\.(test|spec)\.[cm]?[jt]sx?$/.test(rel)) return true;
  if (/Test\.kt$/.test(rel)) return true;
  if (/^backend\/internal\/[^/]+\/adapters\/postgres\/sqlc\/schema\.sql$/.test(rel)) return true;
  if (/(^|\/)(__tests__|node_modules|build|dist|\.next|generated)\//.test(rel)) return true;
  // the guard itself carries ceo_ai fixtures in its self-test
  if (rel.endsWith("tools/agent-hooks/check-ceo-ai-core-boundary.mjs")) return true;
  return false;
}

function rootFor(rel) {
  for (const entry of SCAN_ROOTS) {
    if (rel.startsWith(entry.root)) {
      if (entry.allow.some((a) => rel.startsWith(a))) return null; // assistant-owned, allowed
      return entry;
    }
  }
  return null;
}

function lineOfIndex(source, index) {
  return source.slice(0, index).split("\n").length; // 1-indexed
}

// Whole-source scan so multiline SQL (split FROM/JOIN) is caught. A match is
// reported at its START line; skipped if that line is a comment or a complete
// ignore directive sits on it / the line above.
export function findingsForSource(source, kind) {
  const regexes = kind === "client" ? CLIENT_RES : SQL_RES;
  const lines = source.split("\n");
  const findings = [];
  for (const re of regexes) {
    re.lastIndex = 0;
    let m;
    while ((m = re.exec(source)) !== null) {
      if (m.index === re.lastIndex) re.lastIndex++; // guard against zero-width
      const ln = lineOfIndex(source, m.index); // 1-indexed
      if (isCommentLine(lines[ln - 1])) continue;
      const window = [lines[ln - 1] ?? "", lines[ln - 2] ?? ""].join("\n");
      if (IGNORE_RE.test(window)) continue;
      findings.push({ line: ln, text: (lines[ln - 1] ?? "").trim() });
    }
  }
  return findings;
}

function trackedFiles() {
  const out = execSync("git ls-files", { cwd: repo, encoding: "utf8" });
  return out
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean)
    .filter((rel) => SCAN_EXT.test(rel))
    .filter((rel) => !isExcluded(rel))
    .map((rel) => ({ rel, entry: rootFor(rel) }))
    .filter((f) => f.entry !== null);
}

function runScan() {
  const violations = [];
  for (const { rel, entry } of trackedFiles()) {
    let src;
    try {
      src = readFileSync(resolve(repo, rel), "utf8");
    } catch {
      continue;
    }
    for (const f of findingsForSource(src, entry.kind)) {
      violations.push(`${rel}:${f.line}: core layer references ceo_ai reporting/assistant namespace -> ${f.text}`);
    }
  }
  if (violations.length > 0) {
    console.error("ceo-ai-boundary-guard: core BE/FE/mobile layers must not depend on the ceo_ai reporting/assistant namespace:\n");
    for (const v of violations) console.error("  " + v);
    console.error(
      "\nThe assistant reads core data (read APIs / MCP / read-only SQL); core layers must not read ceo_ai.* or route data through /api/ceo-ai.",
    );
    console.error("Move the logic into a core Go/SQL source, or (if genuinely assistant-owned) into an assistant-owned dir.");
    console.error("See docs/decisions/ceo-ai-reporting-boundary.md");
    process.exit(1);
  }
  console.log("ceo-ai-boundary-guard: no core-layer dependency on the ceo_ai reporting/assistant namespace");
}

function runSelfTest() {
  const cases = [
    // --- SQL kind ---
    { name: "sql_function_call", kind: "sql", pass: false, src: "SELECT ceo_ai.vaccine_label_for(x) FROM t" },
    { name: "sql_from_view", kind: "sql", pass: false, src: "  SELECT a FROM ceo_ai.animal_current_scope" },
    { name: "sql_join_table", kind: "sql", pass: false, src: "  LEFT JOIN ceo_ai_messages m ON m.id = x" },
    // multiline / split forms (the P1 bypass)
    { name: "sql_split_from", kind: "sql", pass: false, src: "SELECT *\nFROM\n  ceo_ai.action_center_current" },
    { name: "sql_split_join", kind: "sql", pass: false, src: "SELECT *\n  LEFT JOIN\n    ceo_ai.vaccination_shed_status s ON true" },
    { name: "sql_split_function", kind: "sql", pass: false, src: "SELECT ceo_ai.vaccine_label_for\n  (x)" },
    { name: "go_comment_exempt", kind: "sql", pass: true, src: "// former SQL ceo_ai.vaccine_label_for join, now in Go" },
    { name: "sql_comment_exempt", kind: "sql", pass: true, src: "  -- reporting schema (ceo_ai) note" },
    { name: "i18n_copy_key_ok", kind: "sql", pass: true, src: '"ceo_ai.title": "Ask Mesha",' },
    { name: "telemetry_name_ok", kind: "sql", pass: true, src: 'Open: "ceo_ai_admin_trace_open",' },
    { name: "unrelated_ok", kind: "sql", pass: true, src: "SELECT vaccine_label FROM protocol_rules" },
    {
      name: "sql_complete_ignore_passes",
      kind: "sql",
      pass: true,
      src: "SELECT ceo_ai.foo(x) -- ceo-ai-boundary:ignore: owner=ravi issue=GH-1 scope=legacy expiry=2026-12-31",
    },
    { name: "sql_incomplete_ignore_fails", kind: "sql", pass: false, src: "SELECT ceo_ai.foo(x) -- ceo-ai-boundary:ignore: legacy" },
    // --- client kind ---
    { name: "client_api_route", kind: "client", pass: false, src: 'const r = await fetch("/api/ceo-ai/ask", opts);' },
    { name: "client_bare_route", kind: "client", pass: false, src: 'forwardStream("/ceo-ai/starters", init)' },
    { name: "client_template_route", kind: "client", pass: false, src: "const url = `${base}/api/ceo-ai/ask`;" },
    { name: "client_import", kind: "client", pass: false, src: 'import { CEOAIChat } from "@/components/ceo-ai-chat";' },
    { name: "client_feature_import", kind: "client", pass: false, src: 'import x from "@/features/ceo-ai/panel";' },
    { name: "client_barrel_import", kind: "client", pass: false, src: 'import { CeoAiPanel } from "@/features/ceo-ai";' },
    { name: "client_unrelated_ok", kind: "client", pass: true, src: 'const r = await fetch("/api/vaccination/action-center");' },
    { name: "client_comment_ok", kind: "client", pass: true, src: '// see /api/ceo-ai/ask for the assistant proxy' },
    {
      name: "client_complete_ignore_passes",
      kind: "client",
      pass: true,
      src: 'fetch("/api/ceo-ai/ask") // ceo-ai-boundary:ignore: owner=ravi issue=GH-2 scope=demo expiry=2026-12-31',
    },
  ];
  let failures = 0;
  for (const tc of cases) {
    const clean = findingsForSource(tc.src, tc.kind).length === 0;
    if (clean !== tc.pass) {
      failures++;
      console.error(`ceo-ai-boundary-guard self-test ${tc.name}: got ${clean ? "pass" : "fail"}, want ${tc.pass ? "pass" : "fail"}`);
    }
  }
  if (failures > 0) {
    console.error(`ceo-ai-boundary-guard: ${failures} self-test(s) failed`);
    process.exit(1);
  }
  console.log(`ceo-ai-boundary-guard: all ${cases.length} self-tests passed`);
}

const mode = process.argv[2];
if (mode === "--self-test") {
  runSelfTest();
} else {
  runScan();
}
