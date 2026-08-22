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
  "features/approvals/",  // /approvals is new (maintainer decision 2026-07-21); no backend page contract yet — documented exception in context/frontend/admin-web-backend-ui-contract.md
  "features/ceo-ai/",  // leadership CEO/CXO assistant chrome (sidebar/feedback/mode-footer/state copy); no backend AdminWebPageContract exists for the floating assistant yet — backend-owned starters/title/subtitle already flow via CEOAIChatCopy; the remaining local literals are the documented exception in context/frontend/admin-web-backend-ui-contract.md
  // The vaccination plan console's copy is still local while the page contract carries the
  // DELETED /config screen's keys (capacity fields, rule-editor labels) rather than this
  // screen's. Wiring 51 strings through a contract that describes a different screen would
  // pin the wrong vocabulary in place; the copy migration is tracked as its own change so
  // the keys can be authored against what this screen actually says. Documented in
  // context/frontend/admin-web-backend-ui-contract.md.
  "features/vaccination-plan/",
  "features/ceo-ai-admin/",  // ADMIN/ENGINEERING-only assistant step-trace debug surface (ceo_internal gate enforced server-side); internal diagnostic tool, not a leadership product screen and not in backend nav — no AdminWebPageContract; documented exception in context/frontend/admin-web-backend-ui-contract.md
  "features/herd-signals/",  // pre-existing feature-wide gap: built against mock/herd-signals-mock.html before being wired through AdminWebPageContract.copy/option_groups for every literal; documented exception in context/frontend/admin-web-backend-ui-contract.md
];
const ALLOW_LINE = [
  /Intl\.DateTimeFormat/,
  /\.key\s*[!=]==?\s*["'](Escape|Enter| )["']/,
  /startsWith\(["']TMP-/,
  /toLowerCase\(\)/,
  /const\s+PATH\s*=/,
  /AppApiComponents\["schemas"\]/,
  /Vaccine facts/, // accordion UI section label (documented exception in admin-web-backend-ui-contract.md)
  /["']use client["']/,  // Next.js use client directive
  /["']use server["']/,  // Next.js use server directive
  /\bid\s*=\s*["']/,  // HTML id attributes (technical identifiers, not UI copy)
  /\bclassName\s*=\s*["']/,  // className attributes
  /\bdata-\w+\s*=\s*["']/,  // data-* attributes
  /\bkey\s*=\s*["']/,  // React key attributes
  /\bnoun\s*=\s*["']/,  // technical parameters like noun="animal"
  /\.displayName\s*=\s*["']/,  // React component displayName assignments (technical identifiers for DevTools)
];
const JSX_TEXT = />\s*[A-Z][^<{}`]{2,}\s*</;
const VISIBLE_ATTR = /\b(?:placeholder|aria-label|title)=["'][A-Z][^"']{2,}["']/;
const CAPITAL_STRING = /["'][A-Z][^"']{2,}["']/;
// Lowercase visible copy detector: catches natural-language prose in JSX text and visible attributes
// Must start with lowercase, be 2+ words OR a single word 3+ letters, and not be a technical term
// JSX text: must have word/space characters only (not operators), not spanning code syntax
const LOWERCASE_JSX_TEXT = />\s*[a-z][\w\s,.–—'"!?&()]{0,}[a-z0-9)\]'"!?]\s*</;
const LOWERCASE_VISIBLE_ATTR = /\b(?:placeholder|aria-label|title)=["'][a-z][a-z0-9\s,.–—'"!?&()]*[a-z0-9]["']/;

// Common technical single words that should NOT be flagged as prose
const TECHNICAL_WORDS = new Set([
  'id', 'key', 'add', 'add', 'day', 'run', 'fix', 'set', 'get', 'put', 'use', 'api', 'url', 'uri', 'xml', 'css',
  'sql', 'cli', 'uri', 'jwt', 'org', 'app', 'net', 'sys', 'tmp', 'var', 'req', 'res', 'ctx', 'env', 'dev',
]);

// Detect whether a string is natural-language prose (not a technical identifier)
function isLowercaseProseString(str) {
  // Remove leading/trailing whitespace and quotes
  const cleaned = str.trim().replace(/^["']|["']$/g, '').trim();
  if (!cleaned || !/^[a-z]/.test(cleaned)) return false;

  // Single lowercase letter or digit is likely technical (e.g., 'x', 'i', '1')
  if (cleaned.length < 2) return false;

  // Single technical word: lowercase short identifiers (e.g., 'id', 'key', 'href')
  if (cleaned.length < 3 && !/\s/.test(cleaned)) return false;

  // Check common technical words (even if 3+ letters)
  const lowerStr = cleaned.toLowerCase();
  if (TECHNICAL_WORDS.has(lowerStr)) return false;

  // Multiple words (separated by space/dash/underscore) is clearly prose (unless all technical)
  if (/[\s\-_]/.test(cleaned)) return true;

  // Single word 4+ letters: likely prose if it's a real English word pattern
  // Require 4+ letters to avoid catching short technical words
  if (/^[a-z]+$/.test(cleaned) && cleaned.length >= 4) return true;

  return false;
}

// Extract and validate lowercase strings from JSX/attribute patterns
function extractLowercaseStringFromMatch(match, pattern) {
  if (pattern === 'jsx') {
    // Extract text from JSX: >text<
    const extracted = match.match(/>\s*([^<{}`]+)\s*</);
    return extracted ? extracted[1] : null;
  } else if (pattern === 'attr') {
    // Extract text from attribute: attribute="text"
    const extracted = match.match(/=["']([^"']+)["']/);
    return extracted ? extracted[1] : null;
  }
  return null;
}
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
    // Check for uppercase visible copy (capital-letter strings in JSX or attributes)
    if (JSX_TEXT.test(code) || VISIBLE_ATTR.test(code) || CAPITAL_STRING.test(code)) {
      findings.push(`${rel}:${index + 1}  visible/admin text literal should come from AdminWebPageContract.copy or option_groups`);
      return;
    }

    // Check for lowercase visible copy (natural-language prose starting with lowercase)
    let lowercaseMatch = null;
    let lowerPattern = null;

    const jsxTextMatch = code.match(LOWERCASE_JSX_TEXT);
    if (jsxTextMatch) {
      const extracted = extractLowercaseStringFromMatch(jsxTextMatch[0], 'jsx');
      if (extracted && isLowercaseProseString(extracted)) {
        lowercaseMatch = jsxTextMatch[0];
        lowerPattern = 'jsx';
      }
    }

    const visibleAttrMatch = code.match(LOWERCASE_VISIBLE_ATTR);
    if (!lowercaseMatch && visibleAttrMatch) {
      const extracted = extractLowercaseStringFromMatch(visibleAttrMatch[0], 'attr');
      if (extracted && isLowercaseProseString(extracted)) {
        lowercaseMatch = visibleAttrMatch[0];
        lowerPattern = 'attr';
      }
    }

    if (lowercaseMatch) {
      findings.push(`${rel}:${index + 1}  visible/admin text literal should come from AdminWebPageContract.copy or option_groups`);
      return;
    }
  });
}

if (findings.length > 0) {
  console.error("admin UI contract literal guard failed:");
  for (const finding of findings) console.error(`  ${finding}`);
  console.error("\nMove visible copy/options into backend/internal/adminui/app/service.go and the OpenAPI AdminWeb* contract, or document an exception in context/frontend/admin-web-backend-ui-contract.md.");
  process.exit(1);
}

console.log("admin UI contract literal guard passed.");
