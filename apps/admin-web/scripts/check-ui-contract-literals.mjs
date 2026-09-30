#!/usr/bin/env node
// Guard for the admin-web backend-driven UI contract rule.
//
// Active admin pages must render visible labels/options/copy from
// GET /admin-web/bootstrap via apps/admin-web/lib/admin-ui-contract.ts. This
// scanner intentionally allows pre-contract auth and emergency contract-failure
// screens, plus technical constants such as key names, locale IDs, and time
// zones.
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

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
  "features/leadership-tasks/",  // new Tasks screen still carries local mock/preview copy while the live route is backed by AdminWebPageContract; documented exception in context/frontend/admin-web-backend-ui-contract.md
  // A local-only gallery of the kit primitives with invented
  // sample rows, not a production admin route and not in backend nav, so there is no
  // AdminWebPageContract to source its demo copy from. Documented exception in
  // context/frontend/admin-web-backend-ui-contract.md.
  "app/kit-preview/",
  // Same rationale as components/admin-shell.tsx and app/loading.tsx above: global-error renders
  // when rendering itself failed, so it cannot assume the contract fetch is reachable.
  "app/global-error.tsx",
  // The route shell of the ADMIN/ENGINEERING-only assistant debug surface already skipped as
  // features/ceo-ai-admin/ (ceo_internal gate enforced server-side, not in backend nav).
  "app/(admin)/ceo-ai-admin/",
  // Kit PRIMITIVES carry no page copy: their only literals are default aria-labels and fallback
  // labels ("Close", "Row actions", "Dense") on shared chrome that belongs to no single page and
  // therefore to no AdminWebPageContract. Every page-level string a kit component shows is passed
  // in by its caller, and the caller is still checked. Documented exception in
  // context/frontend/admin-web-backend-ui-contract.md.
  "components/kit/",
];
const ALLOW_LINE = [
  /Intl\.DateTimeFormat/,
  /\.key\s*[!=]==?\s*["'](Escape|Enter| )["']/,
  // Keyboard key NAMES are DOM constants, not copy: a listbox's roving-focus handler compares
  // event.key against "ArrowDown"/"Home"/"End" and an array of the same, and those strings can
  // never come from a backend contract.
  /\.key\s*[!=]==?\s*["'](ArrowDown|ArrowUp|Home|End|Tab|PageUp|PageDown|Escape|Enter)["']/,
  /\[["'](ArrowDown|ArrowUp|Enter|Escape|Home|End)["'][^\]]*\]\.includes\(/,
  // copy(pageContract, "key", "Fallback") IS contract-sourced: the literal is the rollout
  // fallback for a key the backend has not published yet, which is the pattern this guard asks for.
  /\bcopy\(\s*\w+\s*,\s*["'][\w.-]+["']\s*,/,
  // A select's defaultValue is an option KEY matched against the backend option_groups list, not
  // rendered copy -- the visible label still comes from the option the key selects.
  /\bdefaultValue\s*=\s*["']/,
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
  /\bmethod:\s*["'](?:GET|POST|PUT|PATCH|DELETE)["']/,  // an HTTP verb on a fetch init is a wire constant, never rendered copy
  /^type\s+[a-z]\w*</,  // private generic helper aliases, not rendered copy
  /^async function\s+[a-z]\w*</,  // private generic helper functions, not rendered copy
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
const SELF_TEST = process.argv.includes("--self-test");

function readIfExists(path) {
  return existsSync(path) ? readFileSync(path, "utf8") : "";
}

function addMatches(set, source, pattern) {
  for (const match of source.matchAll(pattern)) {
    if (match[1]) set.add(match[1]);
  }
}

function union(...sets) {
  const out = new Set();
  for (const set of sets) {
    for (const value of set) out.add(value);
  }
  return out;
}

function setForRoute(map, routeId) {
  if (!map.has(routeId)) map.set(routeId, new Set());
  return map.get(routeId);
}

function addRouteMatches(map, routeId, source, pattern) {
  addMatches(setForRoute(map, routeId), source, pattern);
}

function readProductionAdminUiService(repoRoot) {
  return readIfExists(join(repoRoot, "backend/internal/adminui/app/service.go"));
}

function findMatchingBrace(source, openIndex) {
  let depth = 0;
  let quote = "";
  let escaped = false;
  for (let i = openIndex; i < source.length; i += 1) {
    const ch = source[i];
    if (quote) {
      if (escaped) {
        escaped = false;
      } else if (ch === "\\") {
        escaped = true;
      } else if (ch === quote) {
        quote = "";
      }
      continue;
    }
    if (ch === '"' || ch === "`") {
      quote = ch;
      continue;
    }
    if (ch === "{") depth += 1;
    if (ch === "}") {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  return -1;
}

function routeBlocksFromSwitch(source) {
  const blocks = [];
  const scanSource = functionBody(source, "pageSpecificCopy");
  if (!scanSource) return blocks;
  const casePattern = /case\s+((?:"[^"]+"\s*,?\s*)+):/g;
  const matches = [...scanSource.matchAll(casePattern)];
  const matchByRoute = new Map();
  matches.forEach((match, index) => {
    for (const label of match[1].matchAll(/"([^"]+)"/g)) {
      matchByRoute.set(label[1], { match, index });
    }
  });
  const extractRouteBodies = (routeId, seen = new Set()) => {
    if (seen.has(routeId)) return [];
    seen.add(routeId);
    const entry = matchByRoute.get(routeId);
    if (!entry) return [];
    const start = entry.match.index;
    const end = matches[entry.index + 1]?.index ?? scanSource.length;
    const caseBody = scanSource.slice(start, end);
    const blockBodies = [];
    const mapPattern = /map\[string\]string\s*\{/g;
    for (const mapMatch of caseBody.matchAll(mapPattern)) {
      const openIndex = scanSource.indexOf("{", start + mapMatch.index);
      const closeIndex = findMatchingBrace(scanSource, openIndex);
      if (closeIndex === -1 || closeIndex > end) continue;
      blockBodies.push(scanSource.slice(openIndex + 1, closeIndex));
    }
    // Keys a case assigns directly (m["key"] = "...") are produced for that route too.
    const assigned = [...caseBody.matchAll(/\bm\[\s*"[^"]+"\s*\]\s*=/g)].map((m) => m[0]);
    if (assigned.length > 0) blockBodies.push(assigned.join("\n"));
    for (const helperMatch of caseBody.matchAll(/\b([a-zA-Z][A-Za-z0-9]*)Copy\(\)/g)) {
      const helperBody = functionBody(source, `${helperMatch[1]}Copy`);
      for (const helperMapMatch of helperBody.matchAll(mapPattern)) {
        const openIndex = helperBody.indexOf("{", helperMapMatch.index);
        const closeIndex = findMatchingBrace(helperBody, openIndex);
        if (closeIndex === -1) continue;
        blockBodies.push(helperBody.slice(openIndex + 1, closeIndex));
      }
    }
    for (const includeMatch of caseBody.matchAll(/pageSpecificCopy\("([^"]+)"\)/g)) {
      blockBodies.push(...extractRouteBodies(includeMatch[1], seen));
    }
    return blockBodies;
  };
  for (let i = 0; i < matches.length; i += 1) {
    const match = matches[i];
    for (const label of match[1].matchAll(/"([^"]+)"/g)) {
      const routeId = label[1];
      const blockBodies = extractRouteBodies(routeId);
      if (blockBodies.length > 0) blocks.push({ routeId, body: blockBodies.join("\n") });
    }
  }
  if (blocks.length > 0) return blocks;

  const fallbackCasePattern = /case\s+"([^"]+)"\s*:\s*return\s+map\[string\]string\s*\{/g;
  for (const match of source.matchAll(fallbackCasePattern)) {
    const routeId = match[1];
    const openIndex = source.indexOf("{", match.index);
    const closeIndex = findMatchingBrace(source, openIndex);
    if (closeIndex === -1) continue;
    blocks.push({ routeId, body: source.slice(openIndex + 1, closeIndex) });
  }
  return blocks;
}

function functionBody(source, name) {
  const start = source.indexOf(`func ${name}`);
  if (start === -1) return "";
  const openIndex = source.indexOf("{", start);
  const closeIndex = findMatchingBrace(source, openIndex);
  return closeIndex === -1 ? "" : source.slice(openIndex + 1, closeIndex);
}

function caseBodiesFromFunction(source, name) {
  const body = functionBody(source, name);
  const matches = [...body.matchAll(/case\s+([^:]+)\s*:/g)];
  return matches.map((match, index) => ({
    labels: [...match[1].matchAll(/"([^"]+)"/g)].map((label) => label[1]),
    body: body.slice(match.index, matches[index + 1]?.index ?? body.length),
  }));
}

function mapBodyAfter(source, marker) {
  const start = source.indexOf(marker);
  if (start === -1) return "";
  const openIndex = source.indexOf("{", start);
  const closeIndex = findMatchingBrace(source, openIndex);
  return closeIndex === -1 ? "" : source.slice(openIndex + 1, closeIndex);
}

function routeBlocksFromTsRecord(source, recordName) {
  const start = source.indexOf(`const ${recordName}`);
  if (start === -1) return [];
  const firstBrace = source.indexOf("{", start);
  const end = findMatchingBrace(source, firstBrace);
  if (end === -1) return [];
  const blocks = [];
  let i = firstBrace + 1;
  while (i < end) {
    while (i < end && /[\s,]/.test(source[i])) i += 1;
    while (source.startsWith("//", i)) {
      const nextLine = source.indexOf("\n", i);
      i = nextLine === -1 ? end : nextLine + 1;
      while (i < end && /[\s,]/.test(source[i])) i += 1;
    }
    if (i >= end) break;
    let routeId = "";
    if (source[i] === '"' || source[i] === "'") {
      const quote = source[i];
      const close = source.indexOf(quote, i + 1);
      if (close === -1 || close > end) break;
      routeId = source.slice(i + 1, close);
      i = close + 1;
    } else {
      const match = /^[A-Za-z0-9_.-]+/.exec(source.slice(i));
      if (!match) {
        i += 1;
        continue;
      }
      routeId = match[0];
      i += routeId.length;
    }
    while (i < end && /\s/.test(source[i])) i += 1;
    if (source[i] !== ":") continue;
    i += 1;
    while (i < end && /\s/.test(source[i])) i += 1;
    if (source[i] !== "{") continue;
    const openIndex = i;
    const closeIndex = findMatchingBrace(source, openIndex);
    if (closeIndex === -1 || closeIndex > end) continue;
    blocks.push({ routeId, body: source.slice(openIndex + 1, closeIndex) });
    i = closeIndex + 1;
  }
  return blocks;
}

function idsFromGoBody(body) {
  const ids = new Set();
  addMatches(ids, body, /\bID:\s*"([^"]+)"/g);
  return ids;
}

function tableIdsFromGoBody(body) {
  const ids = new Set();
  addMatches(ids, body, /\btableP?\(\s*"([^"]+)"/g);
  return ids;
}

function addAll(target, values) {
  for (const value of values) target.add(value);
}

function controlIdsFromFunction(source, name, seen = new Set()) {
  if (seen.has(name)) return new Set();
  seen.add(name);
  const body = functionBody(source, name);
  const ids = idsFromGoBody(body);
  for (const match of body.matchAll(/\b([a-zA-Z][A-Za-z0-9]*(?:Control|Controls))\(/g)) {
    addAll(ids, controlIdsFromFunction(source, match[1], seen));
  }
  return ids;
}

const ROUTE_BY_PATH_PREFIX = [
  ["features/procurement/sales-record-drawer", ["sales-sold", "sales-config"]],
  ["features/procurement/loadwise-section", ["sales-loads", "sales-config"]],
  ["features/procurement/load-cost-drawer", ["sales-loads", "sales-config"]],
  ["features/procurement/load-detail", "source-load"],
  ["features/procurement/sales-buyer-analytics", "sales-buyer-analytics"],
  ["features/procurement/sales-farm-born", "sales-farm-born"],
  ["features/procurement/farm-born-sold-table", "sales-farm-born"],
  ["features/procurement/sales-loads", "sales-loads"],
  ["features/action-center/", "action-center"],
  ["features/alerts/", "alerts"],
  ["features/calendar/", "calendar"],
  ["features/counts/herd-passport", "herd-register"],
  ["features/counts/herd-register", "herd-register"],
  ["features/counts/counts-breakdown", "counts-breakdown"],
  ["features/feed/feed-analytics", "feed-analytics"],
  ["features/feed/feed-config", "feed-config"],
  ["features/feed/feed-direction", "feed-direction"],
  ["features/feed/feed-packing", "feed-packing"],
  ["features/health/health-analytics", "health-analytics"],
  ["features/health/health-config", "health-config"],
  ["features/goat-passport/", "goat-passport"],
  ["features/leave/", "leave"],
  ["features/pen-routines/", "pen-routines"],
  ["features/procurement/animal-purchases", "animal-purchases"],
  ["features/procurement/feed-purchase", "feed-purchases"],
  ["features/procurement/source-load", "source-load"],
  ["features/process-integrity/action-center", "action-center"],
  ["features/process-integrity/protocol-adherence", "protocol-adherence"],
  ["features/process-integrity/workflow-drilldown", "workflow-record"],
  ["features/process-integrity/workflow", "workflows"],
  ["features/sales/buyer", "sales-buyer-analytics"],
  ["features/sales/farm-value", "sales-farm-value"],
  ["features/sales/sales-config", "sales-config"],
  ["features/sales/sales-loads", "sales-loads"],
  ["features/sales/sales-sold", "sales-sold"],
  ["features/sales/market", "sales-market-analytics"],
  ["features/sales/vendors", "sales-vendors"],
  ["features/sales/", "sales-sold"],
  ["features/verification-review/", "verification-review"],
  ["features/preventive-care-vaccination/", "vaccination"],
  ["features/vaccination-sheds/", "vaccination"],
  ["features/vaccination-execution/execution-board", "vaccination"],
  ["features/vaccination-execution/", "shed-execution"],
  ["features/weighing/weights-analytics", "weighing-analytics"],
  ["features/weighing/weights-export", "weighing-analytics"],
  // The Assumptions drawer lives in features/weighing/ but is mounted on /weighing/sops.
  ["features/weighing/weights-assumptions", "weighing-sops"],
  ["features/weighing/weights", "weighing-weights"],
  ["features/work-board/", "work-board"],
  ["app/(admin)/action-center/", "action-center"],
  ["app/(admin)/alerts/", "alerts"],
  ["app/(admin)/calendar/drive/", "calendar"],
  ["app/(admin)/calendar/", "calendar"],
  ["app/(admin)/counts/breakdown/", "counts-breakdown"],
  ["app/(admin)/counts/herd/", "herd-register"],
  ["app/(admin)/feed/analytics/", "feed-analytics"],
  ["app/(admin)/feed/config/", "feed-config"],
  ["app/(admin)/feed/direction/", "feed-direction"],
  ["app/(admin)/feed/packing/", "feed-packing"],
  ["app/(admin)/health/analytics/", "health-analytics"],
  ["app/(admin)/health/config/", "health-config"],
  ["app/(admin)/goats/", "goat-passport"],
  ["app/(admin)/leave/", "leave"],
  ["app/(admin)/procurement/animal-purchases/", "animal-purchases"],
  ["app/(admin)/procurement/feed-purchases/", "feed-purchases"],
  ["app/(admin)/procurement/source-entry/loads/", "source-load"],
  ["app/(admin)/procurement/source-entry/", "source-entry"],
  ["app/(admin)/procurement/vendors/", "vendors"],
  ["app/(admin)/protocol-adherence/", "protocol-adherence"],
  ["app/(admin)/routines/", "pen-routines"],
  ["app/(admin)/sales/buyer-analytics/", "sales-buyer-analytics"],
  ["app/(admin)/sales/farm-born/", "sales-farm-born"],
  ["app/(admin)/sales/config/", "sales-config"],
  ["app/(admin)/sales/farm-value/", "sales-farm-value"],
  ["app/(admin)/sales/loads/", "sales-loads"],
  ["app/(admin)/sales/market-analytics/", "sales-market-analytics"],
  ["app/(admin)/sales/vendors/", "sales-vendors"],
  ["app/(admin)/sales/sold/", "sales-sold"],
  ["app/(admin)/vaccination/", "vaccination"],
  ["app/(admin)/verification/", "verification-review"],
  ["app/(admin)/verify/", "verification-review"],
  ["app/(admin)/weighing/analytics/", "weighing-analytics"],
  ["app/(admin)/weighing/weights/", "weighing-weights"],
  ["app/(admin)/work-board/", "work-board"],
  ["app/(admin)/workflows/[row_id]/", "workflow-record"],
  ["app/(admin)/workflows/", "workflows"],
  ["app/vaccination/", "vaccination"],
];

function routeForFile(rel) {
  if (rel.startsWith("app/(admin)/vaccination/plan/")) return undefined;
  if (rel === "features/preventive-care-vaccination/passport-section.tsx") return undefined;
  const route = ROUTE_BY_PATH_PREFIX.find(([prefix]) => rel.startsWith(prefix))?.[1];
  if (!route) return [];
  return Array.isArray(route) ? route : [route];
}

function contractSourceIndex() {
  const repoRoot = resolve(ROOT, "../..");
  const backendService = readProductionAdminUiService(repoRoot);
  const backendCompiler = readIfExists(join(repoRoot, "backend/internal/adminui/app/compiler.go"));
  const frontendContract = readIfExists(join(ROOT, "lib/admin-ui-contract.ts"));
  const copyKeysByRoute = new Map();
  const optionGroupsByRoute = new Map();
  const tablesByRoute = new Map();
  const controlsByRoute = new Map();
  const global = {
    copyKeys: new Set(),
    optionGroups: new Set(),
    tables: new Set(),
    controls: new Set(),
  };
  const commonCopyKeys = new Set();
  const broadCopyKeys = new Set();

  // Backend page contracts and frontend stale-contract fallbacks are both valid producers. This
  // scans production producers only: service.go and explicit frontend rollout fallbacks. Test
  // fixtures and unrelated Go maps are intentionally excluded so they cannot bless a lookup that
  // the real bootstrap response never emits.
  for (const { routeId, body } of routeBlocksFromSwitch(backendService)) {
    addRouteMatches(copyKeysByRoute, routeId, body, /"([^"]+)"\s*:/g);
    addRouteMatches(copyKeysByRoute, routeId, body, /\[\s*"([^"]+)"\s*\]\s*=/g);
  }
  addMatches(commonCopyKeys, mapBodyAfter(backendService, "copy := map[string]string"), /"([^"]+)"\s*:/g);
  addMatches(broadCopyKeys, backendService, /"([^"]+)"\s*:/g);
  addMatches(broadCopyKeys, backendService, /\[\s*"([^"]+)"\s*\]\s*=/g);
  addMatches(broadCopyKeys, frontendContract, /"([^"]+)"\s*:/g);
  for (const { routeId, body } of routeBlocksFromTsRecord(frontendContract, "COPY_FALLBACKS")) {
    addRouteMatches(copyKeysByRoute, routeId, body, /"([^"]+)"\s*:/g);
  }

  const optionHelperIDs = new Map([
    ["genericOptionGroups", idsFromGoBody(functionBody(backendService, "genericOptionGroups"))],
    ["salesOptionGroups", idsFromGoBody(functionBody(backendService, "salesOptionGroups"))],
    ["healthConfigOptionGroups", idsFromGoBody(functionBody(backendService, "healthConfigOptionGroups"))],
    ["configOptionGroups", idsFromGoBody(functionBody(backendService, "configOptionGroups"))],
    ["liveTrackerOptionGroups", idsFromGoBody(functionBody(backendService, "liveTrackerOptionGroups"))],
    ["countsBreakdownOptionGroups", idsFromGoBody(functionBody(backendService, "countsBreakdownOptionGroups"))],
    ["feedOptionGroups", idsFromGoBody(functionBody(backendService, "feedOptionGroups"))],
    ["herdRegisterOptionGroups", idsFromGoBody(functionBody(backendService, "herdRegisterOptionGroups"))],
    ["calendarOptionGroups", idsFromGoBody(functionBody(backendService, "calendarOptionGroups"))],
    ["procurementOptionGroups", idsFromGoBody(functionBody(backendService, "procurementOptionGroups"))],
    ["processIntegrityOptionGroups", idsFromGoBody(functionBody(backendService, "processIntegrityOptionGroups"))],
    ["weighingWeightsOptionGroups", idsFromGoBody(functionBody(backendService, "weighingWeightsOptionGroups"))],
    ["sopOptionGroups", idsFromGoBody(functionBody(backendService, "sopOptionGroups"))],
    ["shiftingSOPOptionGroups", idsFromGoBody(functionBody(backendService, "shiftingSOPOptionGroups"))],
    ["weighingSOPOptionGroups", idsFromGoBody(functionBody(backendService, "weighingSOPOptionGroups"))],
    ["assumptionVocabularyOptionGroups", idsFromGoBody(functionBody(backendService, "assumptionVocabularyOptionGroups"))],
    ["inspectionOptionGroups", idsFromGoBody(functionBody(backendService, "inspectionOptionGroups"))],
  ]);
  optionHelperIDs.set("sopOptionGroupsFor", optionHelperIDs.get("sopOptionGroups") ?? new Set());
  optionHelperIDs.set("shedStatusOptionGroup", idsFromGoBody(functionBody(backendService, "shedStatusOptionGroup")));
  optionHelperIDs.set("capacityOptionGroup", idsFromGoBody(functionBody(backendService, "capacityOptionGroup")));

  const genericOptionIDs = optionHelperIDs.get("genericOptionGroups") ?? new Set();
  for (const { labels, body } of caseBodiesFromFunction(backendService, "pageOptionGroups")) {
    const ids = idsFromGoBody(body);
    if (/withGenericOptionGroups|genericOptionGroups/.test(body)) addAll(ids, genericOptionIDs);
    for (const [helperName, helperIDs] of optionHelperIDs) {
      if (body.includes(`${helperName}(`)) addAll(ids, helperIDs);
    }
    for (const routeId of labels) addAll(setForRoute(optionGroupsByRoute, routeId), ids);
  }
  for (const { labels, body } of caseBodiesFromFunction(backendCompiler, "compilePages")) {
    const ids = idsFromGoBody(body);
    addMatches(ids, body, /replaceOptionGroup\([^,]+,\s*"([^"]+)"/g);
    addMatches(ids, body, /mergeOptionGroupReferences\([^,]+,\s*"([^"]+)"/g);
    for (const routeId of labels) addAll(setForRoute(optionGroupsByRoute, routeId), ids);
  }

  const pagePattern = /page\(\s*"([^"]+)"[\s\S]*?(?=\n\s*(?:\/\/[^\n]*\n\s*)*page\(|\n\s*\)\s*$)/g;
  for (const match of backendService.matchAll(pagePattern)) {
    const routeId = match[1];
    const body = match[0];
    addRouteMatches(tablesByRoute, routeId, body, /\btableP?\(\s*"([^"]+)"/g);
    for (const helperName of ["loadwiseTable", "buyerAnalyticsTable", "farmBornSoldTable", "animalPurchaseLoadTable", "animalPurchaseAnimalTable", "feedPurchaseTable", "weightsGainThresholdTable", "vaccinationShedTable", "feedWeightBandTable", "feedWeightBandUnmatchedTable", "feedWeightBandExitsTable"]) {
      if (body.includes(`${helperName}(`)) addAll(setForRoute(tablesByRoute, routeId), tableIdsFromGoBody(functionBody(backendService, helperName)));
    }
  }

  for (const { labels, body } of caseBodiesFromFunction(backendCompiler, "compilePages")) {
    const called = [...body.matchAll(/\b(compile[A-Za-z0-9]+Controls|alertsConfigureControl|penRoutineControls)\(/g)].map((match) => match[1]);
    const ids = idsFromGoBody(body);
    for (const name of called) addAll(ids, controlIdsFromFunction(backendCompiler, name));
    for (const routeId of labels) addAll(setForRoute(controlsByRoute, routeId), ids);
  }
  for (const { routeId, body } of routeBlocksFromTsRecord(frontendContract, "OPTION_GROUP_FALLBACKS")) {
    addRouteMatches(optionGroupsByRoute, routeId, body, /\b([A-Za-z0-9_.-]+)\s*:\s*\[/g);
  }
  for (const { routeId, body } of routeBlocksFromTsRecord(frontendContract, "TABLE_FALLBACKS")) {
    addRouteMatches(tablesByRoute, routeId, body, /"([^"]+)"\s*:\s*\{\s*\n\s*id:\s*"[^"]+"/g);
  }

  // Shared/route-parametric frontend components cannot be assigned to exactly one page contract.
  // For those files only, keep a broad structural index so the legacy guard still catches typos
  // without pretending that an arbitrary copy string is an option/control producer.
  addMatches(global.optionGroups, backendService, /\bID:\s*"([^"]+)"/g);
  addMatches(global.tables, backendService, /\btableP?\(\s*"([^"]+)"/g);
  addMatches(global.controls, backendCompiler, /\bID:\s*"([^"]+)"/g);

  for (const set of copyKeysByRoute.values()) for (const key of set) global.copyKeys.add(key);
  for (const key of commonCopyKeys) global.copyKeys.add(key);
  for (const set of optionGroupsByRoute.values()) for (const key of set) global.optionGroups.add(key);
  for (const set of tablesByRoute.values()) for (const key of set) global.tables.add(key);
  for (const set of controlsByRoute.values()) for (const key of set) global.controls.add(key);

  return { copyKeysByRoute, optionGroupsByRoute, tablesByRoute, controlsByRoute, commonCopyKeys, broadCopyKeys, global };
}

const contractIndex = contractSourceIndex();

const CONTRACT_LOOKUPS = [
  { kind: "copy key", routeSets: contractIndex.copyKeysByRoute, globalSet: contractIndex.broadCopyKeys, routeGlobalSet: contractIndex.commonCopyKeys, pattern: /\bcopy\(\s*[^,\n]+,\s*["']([^"'$]+)["']/g },
  { kind: "action feedback copy key", routeSets: contractIndex.copyKeysByRoute, globalSet: contractIndex.broadCopyKeys, routeGlobalSet: contractIndex.commonCopyKeys, pattern: /\bactionFeedbackCopy\(\s*[^,\n]+,\s*[^,\n]+,\s*["']([^"'$]+)["']/g },
  { kind: "option group", routeSets: contractIndex.optionGroupsByRoute, globalSet: contractIndex.global.optionGroups, pattern: /\boptionGroup\(\s*[^,\n]+,\s*["']([^"'$]+)["']/g },
  { kind: "option group", routeSets: contractIndex.optionGroupsByRoute, globalSet: contractIndex.global.optionGroups, pattern: /\b(?:optionLabel|optionTitle|optionTone)\(\s*[^,\n]+,\s*["']([^"'$]+)["']/g },
  { kind: "table", routeSets: contractIndex.tablesByRoute, globalSet: contractIndex.global.tables, pattern: /\b(?:table|tableLabels|tablePageSizes)\(\s*[^,\n]+,\s*["']([^"'$]+)["']/g },
  { kind: "control", routeSets: contractIndex.controlsByRoute, globalSet: contractIndex.global.controls, pattern: /\bcontrol\(\s*[^,\n]+,\s*["']([^"'$]+)["']/g },
];

function checkContractReferences(rel, code, lineNumber) {
  const routeIds = routeForFile(rel) ?? [];
  for (const lookup of CONTRACT_LOOKUPS) {
    lookup.pattern.lastIndex = 0;
    for (const match of code.matchAll(lookup.pattern)) {
      const key = match[1];
      const missingRoutes = routeIds.filter((routeId) => !union(lookup.routeSets.get(routeId) ?? new Set(), lookup.routeGlobalSet ?? new Set()).has(key));
      const isValid = routeIds.length > 0 ? missingRoutes.length === 0 : lookup.globalSet.has(key);
      if (!isValid) {
        const routeText = routeIds.length > 0 ? `${missingRoutes.join(", ")} ` : "";
        findings.push(
          `${rel}:${lineNumber}  ${lookup.kind} "${key}" has no ${routeText}production AdminWebPageContract producer or explicit frontend fallback`,
        );
      }
    }
  }
}

function selfTest() {
  const before = findings.length;
  checkContractReferences("features/work-board/work-board-modal.tsx", 'copy(pageContract, "definitely_missing_self_test_key")', 1);
  checkContractReferences("features/preventive-care-vaccination/self-test.tsx", 'copy(pageContract, "player.play_proof")', 2);
  checkContractReferences("features/preventive-care-vaccination/self-test.tsx", 'optionGroup(pageContract, "inventory_task_work_states")', 3);
  checkContractReferences("features/preventive-care-vaccination/self-test.tsx", 'optionGroup(pageContract, "inventory_progress.title")', 4);
  checkContractReferences("features/preventive-care-vaccination/self-test.tsx", 'control(pageContract, "inventory_progress.title")', 5);
  checkContractReferences("features/goat-passport/self-test.tsx", 'copy(pageContract, "inventory_progress.title")', 6);
  checkContractReferences("features/counts/herd-register-self-test.tsx", 'copy(pageContract, "inventory_progress.title")', 7);
  const added = findings.slice(before);
  const hasMissingUnknownRoute = added.some((finding) => finding.includes("features/work-board/work-board-modal.tsx:1") && finding.includes("definitely_missing_self_test_key"));
  const hasCrossRouteCopy = added.some((finding) => finding.includes("features/preventive-care-vaccination/self-test.tsx:2") && finding.includes("player.play_proof"));
  const hasFallbackOptionAccepted = !added.some((finding) => finding.includes("features/preventive-care-vaccination/self-test.tsx:3"));
  const hasCopyNotOption = added.some((finding) => finding.includes("features/preventive-care-vaccination/self-test.tsx:4") && finding.includes("option group"));
  const hasCopyNotControl = added.some((finding) => finding.includes("features/preventive-care-vaccination/self-test.tsx:5") && finding.includes("control"));
  const hasGoatPassportRouteScope = added.some((finding) => finding.includes("features/goat-passport/self-test.tsx:6") && finding.includes("goat-passport"));
  const hasHerdRegisterRouteScope = added.some((finding) => finding.includes("features/counts/herd-register-self-test.tsx:7") && finding.includes("herd-register"));
  findings.length = before;
  const fallbackRouteIds = routeBlocksFromTsRecord(readIfExists(join(ROOT, "lib/admin-ui-contract.ts")), "COPY_FALLBACKS").map((block) => block.routeId);
  const duplicateFallbackRoutes = fallbackRouteIds.filter((routeId, index) => fallbackRouteIds.indexOf(routeId) !== index);
  if (!hasMissingUnknownRoute || !hasCrossRouteCopy || !hasFallbackOptionAccepted || !hasCopyNotOption || !hasCopyNotControl || !hasGoatPassportRouteScope || !hasHerdRegisterRouteScope || duplicateFallbackRoutes.length > 0) {
    throw new Error(`contract guard self-test failed: ${JSON.stringify({ hasMissingUnknownRoute, hasCrossRouteCopy, hasFallbackOptionAccepted, hasCopyNotOption, hasCopyNotControl, hasGoatPassportRouteScope, hasHerdRegisterRouteScope, duplicateFallbackRoutes })}`);
  }
  console.log("admin UI contract literal/reference guard self-test passed.");
}

if (SELF_TEST) {
  selfTest();
  process.exit(0);
}

for (const file of files) {
  if (isSkipped(file)) continue;
  const rel = relative(ROOT, file).replaceAll("\\", "/");
  const lines = readFileSync(file, "utf8").split("\n");
  lines.forEach((line, index) => {
    if (isCommentOrBlank(line)) return;
    const code = line.split("//")[0] ?? "";
    checkContractReferences(rel, code, index + 1);
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
  console.error("\nMove visible copy/options into backend/internal/adminui/app/service.go and the OpenAPI AdminWeb* contract, add an explicit stale-contract fallback in apps/admin-web/lib/admin-ui-contract.ts, or document an exception in context/frontend/admin-web-backend-ui-contract.md.");
  process.exit(1);
}

console.log("admin UI contract literal/reference guard passed.");
