#!/usr/bin/env node
// Static IA guard: command-room/authority screens are top-level lenses, not vertical pages.
// This is intentionally generic. It blocks the same drift for Procurement, PHC,
// Parks, Feed Direction, or any future vertical/module.
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";

const APP_ROOT = "app";
const SOURCE_ROOTS = ["app", "components", "features"];

const COMMAND_SEGMENTS = new Set([
  "action-center",
  "protocol-adherence",
  "adherence",
  "control-tower",
  "workflows",
  "config",
  "sops",
  "sop-library",
]);

const TOP_LEVEL_COMMAND_ROUTES = new Set([
  "/",
  "/action-center",
  "/protocol-adherence",
  "/workflows",
  "/workflows/{param}",
  "/config",
  "/sops",
]);

function walk(dir, out, predicate) {
  if (!existsSync(dir)) return;
  const st = statSync(dir);
  if (st.isDirectory()) {
    for (const entry of readdirSync(dir)) walk(join(dir, entry), out, predicate);
    return;
  }
  if (predicate(dir)) out.push(dir);
}

function normalizeRouteFromPage(file) {
  const rel = relative(APP_ROOT, file).split(sep);
  if (rel.at(-1) !== "page.tsx") return null;

  const segments = rel
    .slice(0, -1)
    .filter((segment) => !segment.startsWith("("))
    .filter((segment) => segment !== "api" && segment !== "fonts")
    .map((segment) => (segment.startsWith("[") && segment.endsWith("]") ? "{param}" : segment));

  if (segments.length === 0) return "/";
  return `/${segments.join("/")}`;
}

function hasCommandSegment(route) {
  return route
    .split("/")
    .filter(Boolean)
    .some((segment) => COMMAND_SEGMENTS.has(segment));
}

function isAllowedRoute(route) {
  if (TOP_LEVEL_COMMAND_ROUTES.has(route)) return true;
  return false;
}

function routeFromLiteral(value) {
  if (!value.startsWith("/")) return null;
  const [path] = value.split(/[?#]/);
  const normalized = path.replace(/\/\[[^\]]+\]/g, "/{param}");
  return normalized === "" ? "/" : normalized;
}

function stripComments(source) {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");
}

const pageFiles = [];
walk(APP_ROOT, pageFiles, (file) => file.endsWith("page.tsx"));

const findings = [];

// Vaccination trigger-closure scope guard: Counts is reopened only for Herd Register. The broad mock still
// contains future Counts leaves, but the admin shell must not surface them as active or disabled sidebar
// placeholders unless a future approved slice explicitly reopens them.
const shellFile = "components/mesha-shell.tsx";
if (existsSync(shellFile)) {
  const shellText = stripComments(readFileSync(shellFile, "utf8"));
  const countsGroup = shellText.match(/id:\s*["'`]counts["'`][\s\S]*?leaves:\s*\[([\s\S]*?)\]\s*,?\s*\}/);
  if (!countsGroup) {
    findings.push(
      `${shellFile} must define the Counts sidebar group explicitly. ` +
        "Current vaccination trigger scope exposes only Counts -> Herd Register.",
    );
  } else {
    const labels = [...countsGroup[1].matchAll(/label:\s*["'`]([^"'`]+)["'`]/g)].map((m) => m[1]);
    if (labels.length !== 1 || labels[0] !== "Herd Register") {
      findings.push(
        `${shellFile} exposes Counts sidebar leaves [${labels.join(", ") || "none"}]. ` +
          "Current vaccination trigger scope allows exactly one Counts leaf: Herd Register. " +
          "Do not show Counts overall, Count reconciliation, Tagging & identity, or Weights & ADG " +
          "as active or disabled placeholders.",
      );
    }
  }
}

for (const file of pageFiles) {
  const route = normalizeRouteFromPage(file);
  if (!route || !hasCommandSegment(route)) continue;
  if (!isAllowedRoute(route, file)) {
    findings.push(
      `${file} creates nested command-room/authority route ${route}. ` +
        "Control Tower, Action Center, Protocol Adherence, Workflows, Config, and SOP Library are top-level lenses only.",
    );
  }
}

const sourceFiles = [];
for (const root of SOURCE_ROOTS) {
  walk(root, sourceFiles, (file) => /\.(tsx|ts|mjs|js)$/.test(file));
}

const routeLiteral = /["'`]((?:\/[^"'`\s{}]+)+)["'`]/g;
for (const file of sourceFiles) {
  const text = stripComments(readFileSync(file, "utf8"));
  for (const match of text.matchAll(routeLiteral)) {
    const route = routeFromLiteral(match[1]);
    if (!route || !hasCommandSegment(route)) continue;
    if (route.startsWith("/admin/")) continue; // backend API contract path, not an admin-web route.
    if (TOP_LEVEL_COMMAND_ROUTES.has(route)) continue;

    const segments = route.split("/").filter(Boolean);
    const firstCommandIndex = segments.findIndex((segment) => COMMAND_SEGMENTS.has(segment));
    if (firstCommandIndex > 0) {
      findings.push(
        `${file} references forbidden nested command-room route ${route}. ` +
          "Use the existing top-level route with a domain/filter/lens instead.",
      );
    }
  }
}

const visibleScopeOwners = new Set([
  "components/mesha-shell.tsx",
]);

const forbiddenFutureDomainLabels = [
  "Counts",
  "Breeding",
  "Farmer Network",
  "Inventory",
  "HR",
];

const noInactiveDomainCatalogFiles = [
  "features/process-integrity/action-center.tsx",
  "features/process-integrity/workflows-landing.tsx",
  "features/sops/sop-library.tsx",
];

for (const file of sourceFiles) {
  const rel = relative(".", file).split(sep).join("/");
  const text = stripComments(readFileSync(file, "utf8"));

  if (rel.startsWith("features/") && !visibleScopeOwners.has(rel) && /\bAll parks\b/.test(text)) {
    findings.push(
      `${file} repeats park scope inside a page body. ` +
        "Park/date/source scope belongs in the shell top bar or behind Filters, not inline page chips.",
    );
  }

  if (noInactiveDomainCatalogFiles.includes(rel)) {
    if (/\bDOMAIN_PILLARS\b|\bDOMAIN_CHIPS\.map\b/.test(text)) {
      findings.push(
        `${file} hard-codes an inactive all-domain catalog. ` +
          "Current visible scope must show only active modules; future domains must appear from real enabled module data.",
      );
    }
    for (const label of forbiddenFutureDomainLabels) {
      const visibleLabel = new RegExp(`["'\`]${label.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}["'\`]`);
      if (visibleLabel.test(text)) {
        findings.push(
          `${file} leaks future/non-current domain label "${label}" in the visible active slice. ` +
            "Do not show disabled all-domain product breadth in vaccination/SOP surfaces.",
        );
      }
    }
  }

  // The PHC / Vaccination surface must not render command-lens shortcut chrome (.lensbar / .lenslink bands
  // for Control Tower / Action Center / Protocol Adherence / Workflows). /vaccination LINKS OUT to those
  // top-level lenses; it is not a shortcut hub. Applies to the whole features/phc-vaccination feature.
  if (rel.startsWith("features/phc-vaccination/") && /\blensbar\b|\blenslink\b/.test(text)) {
    findings.push(
      `${file} renders command-lens shortcut chrome inside the PHC / Vaccination module. ` +
        "/vaccination is an operational module surface; Action Center, Protocol Adherence, and Workflows stay top-level.",
    );
  }

  // Command-lens ownership: nothing may import the retired "@/features/vaccination" barrel. Command lenses are
  // owned by features/process-integrity; the PHC module is features/phc-vaccination.
  if (/from\s+["'`]@\/features\/vaccination(["'`]|\/)/.test(text)) {
    findings.push(
      `${file} imports from @/features/vaccination (retired). ` +
        "Import command lenses from @/features/process-integrity and the PHC module from @/features/phc-vaccination.",
    );
  }
}

// ---- features/vaccination must not return as a command-lens home ----
// Command lenses (Action Center / Protocol Adherence / Workflows / their shared model) live in
// features/process-integrity; the PHC operations surface is features/phc-vaccination. A reappearing
// features/vaccination directory is the exact ownership drift this guard exists to block.
if (existsSync("features/vaccination")) {
  findings.push(
    "features/vaccination/ exists again. Command lenses live in features/process-integrity and the PHC " +
      "operations surface is features/phc-vaccination. Do not recreate a features/vaccination home for " +
      "command-lens code.",
  );
}

// ---- Parks owns NO vaccination route/folder/API path/literal (anywhere) ----
// PHC / Vaccination owns vaccination operations AND execution context. Park/shed is only a top-bar scope,
// rendered INSIDE /vaccination (#execution); shed detail lives at /vaccination/execution/sheds/{id}.
// There is NO /parks/vaccination frontend route, redirect, feature folder, lib helper, backend path, or
// OpenAPI path — not even for backward compatibility. Command lenses are top-level for EVERY vertical, so
// no nested /procurement/source-entry/{action-center,adherence,control-tower,workflows} routes either.
const PARKS_VAX = /\/parks\/vaccination/;
const PROC_LENS = /\/procurement\/source-entry\/(action-center|adherence|control-tower|workflows)/;

// (a) frontend route tree / retired folders / lib helper must not exist.
if (existsSync("app/(admin)/parks")) {
  findings.push(
    "app/(admin)/parks/ exists. There is no /parks/vaccination frontend route (not even a redirect). " +
      "Render execution inside /vaccination#execution; shed detail is /vaccination/execution/sheds/{shedId}.",
  );
}
if (existsSync("features/parks-vaccination")) {
  findings.push("features/parks-vaccination/ exists. Rename to features/vaccination-execution — Parks owns no vaccination feature.");
}
if (existsSync("lib/api/parks-vaccination.ts")) {
  findings.push("lib/api/parks-vaccination.ts exists. Rename to lib/api/vaccination-execution.ts.");
}

// (b) any /parks/vaccination or nested-procurement-lens literal in admin-web source (incl. comments).
for (const file of sourceFiles) {
  const raw = readFileSync(file, "utf8");
  const rel = relative(".", file).split(sep).join("/");
  if (PARKS_VAX.test(raw)) {
    findings.push(`${rel} references /parks/vaccination. Use /vaccination#execution or /vaccination/execution/sheds/{shedId}; Parks owns no vaccination route.`);
  }
  if (PROC_LENS.test(raw)) {
    findings.push(`${rel} references a nested procurement command-lens route. Command lenses are top-level — use /action-center?domain=procurement etc.`);
  }
}

// (c) cross-layer: backend route strings, OpenAPI contracts, generated client must not expose these paths.
const crossRoots = ["../../backend", "../../contracts", "../../packages/api-client/src/generated"];
for (const root of crossRoots) {
  const crossFiles = [];
  walk(root, crossFiles, (f) => /\.(go|ya?ml|ts)$/.test(f));
  for (const file of crossFiles) {
    const raw = readFileSync(file, "utf8");
    if (PARKS_VAX.test(raw)) {
      findings.push(`${file} exposes /parks/vaccination. Backend/contract path must be /vaccination/execution[/sheds/{shed_id}] — Parks owns no vaccination path.`);
    }
    if (PROC_LENS.test(raw)) {
      findings.push(`${file} exposes a nested procurement command-lens path. Serve via top-level command screens (?domain=procurement), not /procurement/source-entry/{action-center,adherence,control-tower,workflows}.`);
    }
  }
}

// (d) docs must not SANCTION /parks/vaccination as a route/redirect/exception. Negation guidance
// ("do not create /parks/vaccination") is allowed; lines that present it as a working route/redirect are not.
const docRoots = ["../../AGENTS.md", "../../README.md", "../../context", "../../.agents", "AGENTS.md", "README.md"];
const ALLOWS = /(redirect|exception|sanctioned|compatibility|deep-?link)/i;
// Negation/removal guidance ("there is NO /parks/vaccination exception", "do not create …") is GOOD and
// must not be flagged — only lines that ASSERT /parks/vaccination as a live route/redirect are findings.
const NEGATED = /\b(no|not|never|forbidden|don'?t|remove|removed|deleted|must not|cannot|isn'?t|are not)\b/i;
for (const root of docRoots) {
  const docFiles = [];
  walk(root, docFiles, (f) => f.endsWith(".md"));
  for (const file of docFiles) {
    const lines = readFileSync(file, "utf8").split("\n");
    lines.forEach((line, i) => {
      if (PARKS_VAX.test(line) && ALLOWS.test(line) && !NEGATED.test(line)) {
        findings.push(`${file}:${i + 1} still presents /parks/vaccination as an allowed route/redirect/exception. Remove it — Parks owns no vaccination route.`);
      }
    });
  }
}

// ---- Top-bar scope contract enforcement ----
// Scope-aware screens (Control Tower / Action Center / Protocol Adherence / Workflows / Vaccination /
// Execution) MUST read the top-bar scope ONLY through lib/scope.ts (parseScope/backendScope/scopeHref).
// Reading a scope key directly (one(sp,"park")) or hand-rolling URLSearchParams for a link silently drops
// scope_mode/park/range/as_of/date_from/date_to — the exact drift where the bar says CBE but the screen
// ignores it. parseScope/backendScope/scopeHref live in lib/scope.ts (not scanned) and are the only sanctioned readers/builders.
const SCOPE_AWARE_FILES = new Set([
  "features/control-tower/index.tsx",
  "features/process-integrity/action-center.tsx",
  "features/process-integrity/protocol-adherence.tsx",
  "features/process-integrity/workflows-landing.tsx",
  "features/process-integrity/workflow-drilldown.tsx",
  "features/phc-vaccination/operations.tsx",
  "features/phc-vaccination/execution-section.tsx",
  "features/vaccination-execution/execution-board.tsx",
  "features/vaccination-execution/shed-drilldown.tsx",
]);
const SCOPE_KEY_READ = /\bone\([^,]+,\s*["'`](?:park|as_of|range|scope_mode|date_from|date_to)["'`]\)|\.get\(\s*["'`](?:park|as_of|range|scope_mode|date_from|date_to)["'`]\s*\)/;
const MANUAL_QS = /new URLSearchParams\(/;
for (const file of sourceFiles) {
  const rel = relative(".", file).split(sep).join("/");
  if (!SCOPE_AWARE_FILES.has(rel)) continue;
  const text = stripComments(readFileSync(file, "utf8"));
  if (SCOPE_KEY_READ.test(text)) {
    findings.push(
      `${rel} reads a top-bar scope key directly (park/as_of/range/scope_mode/date_from/date_to). ` +
        "Use parseScope()/backendScope() from @/lib/scope — a scope-aware screen must not read one(sp,\"park\").",
    );
  }
  if (MANUAL_QS.test(text)) {
    findings.push(
      `${rel} hand-rolls URLSearchParams for a link. Build scoped + filter links with scopeHref() from ` +
        "@/lib/scope so the full top-bar scope (scope_mode/park/range/as_of/date_from/date_to) is preserved.",
    );
  }
}

if (findings.length > 0) {
  console.error("✖ IA guard FAILED — command-room/authority screens must not be nested under verticals.\n");
  for (const finding of findings) console.error(`  ${finding}`);
  console.error(
    "\nAllowed pattern: /action-center?domain=<vertical>, /protocol-adherence?domain=<vertical>, " +
      "/workflows?domain=<vertical>, /workflows/{row_id}?domain=<vertical>, " +
      "/config?category=<module>, or /sops?domain=<module>.\n" +
      "Vertical route trees should contain operational screens only.",
  );
  process.exit(1);
}

console.log("✓ IA guard passed — no nested vertical command-room/authority routes detected.");
