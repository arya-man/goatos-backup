#!/usr/bin/env node
// Guard for the admin-web backend-driven UI contract rule.
//
// Active admin pages must render visible labels/options/copy from
// GET /admin-web/bootstrap via apps/admin-web/lib/admin-ui-contract.ts. This
// scanner intentionally allows pre-contract auth and emergency contract-failure
// screens, plus technical constants such as key names, locale IDs, and time
// zones.
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const ROOT = process.cwd();
const SCAN_PATHS = ["app", "components", "features", "lib/scope.ts"];
const SKIP_PATH_PARTS = [
  "components/auth/",
  "components/admin-shell.tsx",
  // Emergency fallback for uncaught React render errors (OBSERVABILITY_DESIGN.md §2.4). Same
  // rationale as components/admin-shell.tsx above: if rendering itself failed, the page cannot
  // assume the backend contract fetch that would supply this copy is safe or reachable.
  "components/observability/error-boundary.tsx",
  "app/layout.tsx",
  "app/loading.tsx",
  "app/auth/",
  "app/login/",
  "features/people/",  // /people page contract being extended; documented exception in context/frontend/admin-web-backend-ui-contract.md
  "features/procurement/work-state.ts",
  "features/process-integrity/process-integrity.ts",
  "features/vaccination-execution/work-state.ts",
  "features/verification-review/",  // /verification is new; no backend page contract yet (Verification module ships on a parallel branch) — documented exception in context/frontend/admin-web-backend-ui-contract.md
];
const ALLOW_LINE = [
  /Intl\.DateTimeFormat/,
  /\.key\s*[!=]==?\s*["'](Escape|Enter| )["']/,
  /startsWith\(["']TMP-/,
  /toLowerCase\(\)/,
  /const\s+PATH\s*=/,
  /AppApiComponents\["schemas"\]/,
  /Vaccine facts/, // accordion UI section label (documented exception in admin-web-backend-ui-contract.md)
];
const JSX_TEXT = />\s*[A-Z][^<{}`]{2,}\s*</;
const VISIBLE_ATTR = /\b(?:placeholder|aria-label|title)=["'][A-Z][^"']{2,}["']/;
const CAPITAL_STRING = /["'][A-Z][^"']{2,}["']/;
const FORBIDDEN_RENDER_META = /\b(?:SEVERITY_META|WORK_STATE_META|PROC_[A-Z_]+_META)\b/;
const STRICT_OPTION_LOOKUP = /\boption(?:Label|Tone|Title)\s*\(/;
const LIVE_ENTITY_ID = /\.(?:park|location|vendor|operator|supplier|farm|shed|goat|lot)_id\b/;
const LIVE_OPTION_HINT = /(?:park|location|vendor|operator|supplier|farm|shed|goat|lot)[A-Za-z]*Options|["'][^"']*(?:park|location|vendor|operator|supplier|farm|shed|goat|lot)[^"']*["']/i;
const LIVE_LOCATION_LITERAL = /["'][^"']*\b(?:CBE|CPT|Coimbatore|Channapatna)\b[^"']*["']/;
const SERVER_ACTION_MESSAGE_VAR = /\blet\s+message\s*=/;
const SERVER_ACTION_VISIBLE_ASSIGN = /\b(?:message|actionKey)\s*=\s*(?:`[^`]*[A-Z][^`]*`|["'][A-Z][^"']*["'])/;

function walk(path, out) {
  if (!existsSync(path)) return;
  const st = statSync(path);
  if (st.isDirectory()) {
    for (const entry of readdirSync(path)) walk(join(path, entry), out);
    return;
  }
  const rel = relative(ROOT, path).replaceAll("\\", "/");
  if (/\.tsx$/.test(path)) out.push(path);
  if (rel === "lib/scope.ts") out.push(path);
  if (/\.ts$/.test(path) && (/(^|\/)(actions|.*-actions)\.ts$/.test(rel) || rel === "features/calendar/calendar-contract.ts")) out.push(path);
}

function isSkipped(file) {
  const normalized = relative(ROOT, file).replaceAll("\\", "/");
  return SKIP_PATH_PARTS.some((part) => normalized.includes(part));
}

function isCommentOrBlank(line) {
  const trimmed = line.trim();
  return trimmed === "" || trimmed.startsWith("//") || trimmed.startsWith("/*") || trimmed.startsWith("*") || trimmed.startsWith("{/*");
}

const files = [];
for (const scanPath of SCAN_PATHS) walk(join(ROOT, scanPath), files);

const findings = [];
for (const file of files) {
  if (isSkipped(file)) continue;
  const rel = relative(ROOT, file).replaceAll("\\", "/");
  const lines = readFileSync(file, "utf8").split("\n");
  lines.forEach((line, index) => {
    if (isCommentOrBlank(line)) return;
    const code = line.split("//")[0] ?? "";
    if (ALLOW_LINE.some((pattern) => pattern.test(code))) return;
    if (/\.tsx$/.test(rel) && FORBIDDEN_RENDER_META.test(code)) {
      findings.push(`${rel}:${index + 1}  renderer must consume backend option_groups, not local *_META label/tone maps`);
      return;
    }
    if (LIVE_LOCATION_LITERAL.test(code)) {
      findings.push(`${rel}:${index + 1}  live park/location labels must come from backend data or DB-compiled contract, not frontend literals`);
      return;
    }
    if (STRICT_OPTION_LOOKUP.test(code) && LIVE_ENTITY_ID.test(code) && LIVE_OPTION_HINT.test(code)) {
      findings.push(`${rel}:${index + 1}  live entity IDs need backend row labels plus optionalOption overrides, not strict optionLabel/optionTone lookups`);
      return;
    }
    if (/\.ts$/.test(rel) && /(^|\/)(actions|.*-actions)\.ts$/.test(rel)) {
      if (code.includes("action_message") || SERVER_ACTION_MESSAGE_VAR.test(code) || SERVER_ACTION_VISIBLE_ASSIGN.test(code)) {
        findings.push(`${rel}:${index + 1}  server actions must redirect action_key values resolved through backend page copy, not visible messages`);
      }
      return;
    }
    if (!JSX_TEXT.test(code) && !VISIBLE_ATTR.test(code) && !CAPITAL_STRING.test(code)) return;
    findings.push(`${rel}:${index + 1}  visible/admin text literal should come from AdminWebPageContract.copy or option_groups`);
  });
}

if (findings.length > 0) {
  console.error("admin UI contract literal guard failed:");
  for (const finding of findings) console.error(`  ${finding}`);
  console.error("\nMove visible copy/options into backend/internal/adminui/app/service.go and the OpenAPI AdminWeb* contract, or document an exception in context/frontend/admin-web-backend-ui-contract.md.");
  process.exit(1);
}

console.log("admin UI contract literal guard passed.");
