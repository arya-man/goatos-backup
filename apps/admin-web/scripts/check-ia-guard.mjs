#!/usr/bin/env node
// Static IA guard: command-room/authority screens are top-level lenses, not vertical pages.
// This is intentionally generic. It blocks the same drift for Procurement, Preventive Care (PC),
// Parks, Feed Direction, or any future vertical/module.
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";

const APP_ROOT = "app";
const SOURCE_ROOTS = ["app", "components", "features"];

const COMMAND_SEGMENTS = new Set([
  "action-center",
  "calendar",
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
  "/calendar",
  "/protocol-adherence",
  "/workflows",
  "/workflows/{param}",
  "/calendar/drive/{param}",
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

// Narrow, deliberate exceptions: module surfaces whose PATH happens to contain a command/authority
// segment but which are not a duplicate of that top-level lens.
//
// `/feed/config`, approved by explicit maintainer decision recorded in
// backend/internal/adminui/app/service.go (see the comment above the "feed" nav group). It authors
// the ration grid, per-shed factors, session template and dispatch clock that ONLY Feed consumes —
// its own data model and endpoints (/feed-config/*). The backend contract classifies it
// "module-surface", not "authority-screen", and ships it as a Feed nav leaf.
//
// The generic `/config` screen it was once contrasted against no longer exists: it authored
// protocol rules for a category set that turned out to be vaccination-only, and Preventive Care /
// Vaccination plan replaced it. Each module now owns its own config surface.
//
// `/health/config`, approved by explicit maintainer decision 2026-08-06 and recorded in
// docs/decisions/health-config-authoring.md plus the "health" nav group comment in the same backend
// contract. It is the same shape of exception for the same reason: a treatment protocol is a
// day-by-day medication document (disease x age band -> ordered steps carrying medicine, dosage,
// unit and route) owned by the Health module and served by /health-config/*, not a protocol
// `rule_dsl` row, and `/config?category=health` cannot render a per-day medicine grid. `/config`
// stays the single generic protocol-rule authority screen; this is classified "module-surface" and
// ships as a Health nav leaf.
//
// Widening this set is a deliberate scope decision (same standing as SUPPORTED_COUNTS_HREFS below),
// not a routine edit: it must be backed by a maintainer decision recorded in the backend contract.
// No command lens (Control Tower, Action Center, Calendar, Protocol Adherence, Workflows) is
// exempted for any vertical, and none may be.
// The FIVE module SOP pages: three approved by explicit maintainer decision 2026-08-18, plus
// /milk/sops and /weighing/sops approved by maintainer decision 2026-08-22 (recorded in
// AGENTS.md and adminui service.go alongside migration 000186's milk.preparation / milk.feeding /
// weighing.session library documents). The original 2026-08-18 record follows (recorded in
// backend/internal/adminui/app/service.go, PC nav-group note, and AGENTS.md): the top-level
// Admin/Data Ops SOP Library (/sops) is RETIRED, and each module owns its SOP documents as a
// module-surface — /counts/sops (Herd Operations: birth / death /
// shifting), /feed/sops (distribution / packing / transport). They are not duplicates of a
// top-level lens: no /sops route exists any more, /config remains the single generic authority
// screen, and no command lens (Control Tower, Action Center, Calendar, Protocol Adherence,
// Workflows) is exempted for any vertical. The "sops" segment stays in COMMAND_SEGMENTS so any
// OTHER nested sops route (e.g. /procurement/sops) still fails without its own recorded decision.
//
// `/sales/config`, approved by explicit maintainer decision 2026-09-01 and recorded in AGENTS.md
// plus the "sales" nav group comment in the backend contract. Same shape as the two Config entries
// above and admitted for the same reason: it is Sales' own DATA-ENTRY surface, not a second copy
// of a top-level lens. It authors nothing generic -- it is where a sale, its animals, the buyer
// and farmer-group pipeline, market quotes, tag lists, weight checks and a purchased load's
// landed cost are RECORDED, against /sales/* and /procurement/loads/*. The two Sales read pages
// became read-only in the same change (their page contracts declare no write control), so entry
// exists in exactly one place. No generic `/config` authority screen is duplicated: the "config"
// segment stays in COMMAND_SEGMENTS, so any OTHER nested config route still fails without its own
// recorded decision.
const MODULE_SURFACE_ROUTE_EXCEPTIONS = new Set([
  "/feed/config",
  "/health/config",
  "/sales/config",
  "/counts/sops",
  "/feed/sops",
  "/milk/sops",
  "/weighing/sops",
]);

function isAllowedRoute(route) {
  if (TOP_LEVEL_COMMAND_ROUTES.has(route)) return true;
  if (MODULE_SURFACE_ROUTE_EXCEPTIONS.has(route)) return true;
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

function escapeRegex(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function groupBodyFor(sourceFile, text, groupId) {
  const idKey = sourceFile.endsWith(".go") ? "ID" : "id";
  const idRe = new RegExp(`\\b${idKey}:\\s*["'\`]${escapeRegex(groupId)}["'\`]`);
  const match = idRe.exec(text);
  if (!match) return null;

  const nextIdRe = new RegExp(`\\n\\s*${idKey}:\\s*["'\`]`, "g");
  nextIdRe.lastIndex = match.index + match[0].length;
  const next = nextIdRe.exec(text);
  return text.slice(match.index, next?.index ?? text.length);
}

function navLeavesForGroup(sourceFile, text, groupId) {
  const body = groupBodyFor(sourceFile, text, groupId);
  if (!body) return null;

  if (sourceFile.endsWith(".go")) {
    // Match both navLeaf(id, label, href, ...) and navLeafDomain(id, label, href, module, ...).
    return [...body.matchAll(/navLeaf(?:Domain)?\(\s*"[^"]+"\s*,\s*"([^"]+)"\s*,\s*"([^"]+)"/g)]
      .map((m) => ({ label: m[1], href: m[2] }));
  }

  const labels = [...body.matchAll(/label:\s*["'`]([^"'`]+)["'`]/g)].map((m) => m[1]);
  const hrefs = [...body.matchAll(/href:\s*["'`]([^"'`]+)["'`]/g)].map((m) => m[1]);
  return labels.map((label, i) => ({ label, href: hrefs[i] ?? "" }));
}

function navItemsFromGoSource(text) {
  return [...text.matchAll(/navItem\(\s*"([^"]+)"\s*,\s*"([^"]+)"\s*,\s*"([^"]+)"/g)]
    .map((m) => ({ id: m[1], label: m[2], href: m[3] }));
}

function pageHrefsFromGoSource(text) {
  return new Set([...text.matchAll(/page\(\s*"[^"]+"\s*,\s*"([^"]+)"/g)].map((m) => m[1]));
}

const pageFiles = [];
walk(APP_ROOT, pageFiles, (file) => file.endsWith("page.tsx"));
const pageRoutes = new Set(pageFiles.map(normalizeRouteFromPage).filter(Boolean));

const findings = [];

// Vaccination trigger-closure scope guard: the shell may mirror the broad mock sidebar, but Counts must not
// create new unsupported route trees. Counts has four real pages in this slice — Herd Register (the per-goat
// register, whose left-bar leaf is withheld for now but whose route stays reachable), Herd Analytics (the
// leadership read: live composition beside month-by-month births/exits/pen movements), Counts Breakdown
// (the farm x stage x breed x gender x shed census), and Milk Preparation
// (the current K1/K2/K3 preparation worklist). Every other broad Counts
// label from the mock must still route into one of those or a top-level command lens.
//
// SUPPORTED_COUNTS_HREFS is an allowlist on purpose: widening it is a deliberate scope decision recorded in
// context/frontend/current-admin-web-scope.md, not a routine edit. Tagging & identity, Weights & ADG, and
// Count reconciliation remain out of scope and must not be added here without that doc changing too.
const SUPPORTED_COUNTS_HREFS = new Set([
  "/counts/herd",
  "/counts/analytics",
  "/counts/breakdown",
  "/counts/milk-preparation",
  // Herd Operations SOP page — part of the SOP split (maintainer decision 2026-08-18, see
  // MODULE_SURFACE_ROUTE_EXCEPTIONS above).
  "/counts/sops",
  "/action-center",
]);
const backendUiContractFile = "../../backend/internal/adminui/app/service.go";
const legacyShellFile = "components/mesha-shell.tsx";
const visibleIaFile = existsSync(backendUiContractFile) ? backendUiContractFile : legacyShellFile;
if (existsSync(visibleIaFile)) {
  const visibleIaRawText = readFileSync(visibleIaFile, "utf8");
  const visibleIaText = stripComments(visibleIaRawText);
  if (visibleIaFile.endsWith(".go")) {
    const pageHrefs = pageHrefsFromGoSource(visibleIaText);
    const navItems = [
      ...navItemsFromGoSource(visibleIaText),
      ...[...visibleIaText.matchAll(/navLeaf(?:Domain)?\(\s*"([^"]+)"\s*,\s*"([^"]+)"\s*,\s*"([^"]+)"/g)]
        .map((m) => ({ id: m[1], label: m[2], href: m[3] })),
    ];
    for (const item of navItems) {
      if (!pageHrefs.has(item.href)) {
        findings.push(
          `${visibleIaFile} publishes nav item ${item.id} (${item.label}) -> ${item.href}, ` +
            "but no AdminWebPageContract page(...) publishes that href.",
        );
      }
      if (!pageRoutes.has(item.href)) {
        findings.push(
          `${visibleIaFile} publishes nav item ${item.id} (${item.label}) -> ${item.href}, ` +
            `but apps/admin-web has no matching page.tsx route. Current routes: [${[...pageRoutes].sort().join(", ")}].`,
        );
      }
    }
  }
  const countsLeaves = navLeavesForGroup(visibleIaFile, visibleIaText, "counts");
  if (!countsLeaves) {
    findings.push(
      `${visibleIaFile} must define the backend Counts sidebar group explicitly. ` +
        "Current vaccination trigger scope exposes only Counts -> Herd Register.",
    );
  } else {
    const labels = countsLeaves.map((leaf) => leaf.label);
    const hrefs = countsLeaves.map((leaf) => leaf.href);
    const unsupportedCountsHrefs = hrefs.filter((href) => !SUPPORTED_COUNTS_HREFS.has(href));
    // Herd Register may be WITHHELD from the sidebar (maintainer decision 2026-08-20, recorded in
    // context/frontend/current-admin-web-scope.md), the same way Feed Packing is. What must never
    // happen is withholding turning into DELETING: the page route has to stay reachable, and the
    // commented restore line has to stay in the nav so the next reader can see the leaf is
    // deliberately parked rather than gone. Checking the raw text is the point — the stripped text
    // this guard otherwise reads cannot see a commented line at all.
    const herdRegisterLeafVisible = labels.includes("Herd register") || labels.includes("Herd Register");
    if (!herdRegisterLeafVisible) {
      if (!pageRoutes.has("/counts/herd")) {
        findings.push(
          `${visibleIaFile} withholds the Counts -> Herd Register leaf, but apps/admin-web no longer serves ` +
            "/counts/herd. Hiding a leaf must keep its route reachable; deleting the route is a separate " +
            "scope decision.",
        );
      }
      if (!/navLeaf\("counts-herd",/.test(visibleIaRawText)) {
        findings.push(
          `${visibleIaFile} withholds the Counts -> Herd Register leaf without leaving its commented ` +
            'navLeaf("counts-herd", ...) restore line in place. Keep it so the leaf reads as parked, not lost.',
        );
      }
    }
    if (labels.length === 0) {
      findings.push(`${visibleIaFile} publishes an empty Counts sidebar group.`);
    }
    if (unsupportedCountsHrefs.length > 0) {
      findings.push(
        `${visibleIaFile} routes Counts mock labels to unsupported paths [${unsupportedCountsHrefs.join(", ")}]. ` +
          `Counts mock labels may appear, but this slice may only route them to [${[...SUPPORTED_COUNTS_HREFS].join(", ")}].`,
      );
    }

    // Audit Log is a business surface under Others — NOT its own "Operations" vertical. The
    // `/operations/audit` route is an implementation detail; the visible IA must place Audit Log
    // in the catch-all group requested by maintainers, and must not surface a separate Operations
    // sidebar group.
    if (/\bLabel:\s*["'`]Operations["'`]|\blabel:\s*["'`]Operations["'`]/.test(visibleIaText)) {
      findings.push(
        `${visibleIaFile} defines an "Operations" sidebar group. Audit Log belongs under Others; ` +
          "do not surface a separate Operations vertical.",
      );
    }
    const othersLeaves = navLeavesForGroup(visibleIaFile, visibleIaText, "others");
    const othersLabels = othersLeaves?.map((leaf) => leaf.label) ?? [];
    if (!othersLabels.includes("Audit Log")) {
      findings.push(
        `${visibleIaFile} must list "Audit Log" under the Others group.`,
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
        "Control Tower, Action Center, Calendar, Protocol Adherence, Workflows, Config, and SOP Library are top-level lenses only.",
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
    if (MODULE_SURFACE_ROUTE_EXCEPTIONS.has(route)) continue;

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

  // The Preventive Care (PC) / Vaccination surface must not render command-lens shortcut chrome (.lensbar / .lenslink bands
  // for Control Tower / Action Center / Calendar / Protocol Adherence / Workflows). /vaccination LINKS OUT to those
  // top-level lenses; it is not a shortcut hub. Applies to the whole features/preventive-care-vaccination feature.
  if (rel.startsWith("features/preventive-care-vaccination/") && /\blensbar\b|\blenslink\b/.test(text)) {
    findings.push(
      `${file} renders command-lens shortcut chrome inside the Preventive Care (PC) / Vaccination module. ` +
        "/vaccination is an operational module surface; Action Center, Protocol Adherence, and Workflows stay top-level.",
    );
  }

  // Command-lens ownership: nothing may import the retired "@/features/vaccination" barrel. Command lenses are
  // owned by features/process-integrity; the Preventive Care (PC) module is features/preventive-care-vaccination.
  if (/from\s+["'`]@\/features\/vaccination(["'`]|\/)/.test(text)) {
    findings.push(
      `${file} imports from @/features/vaccination (retired). ` +
        "Import command lenses from @/features/process-integrity and the Preventive Care (PC) module from @/features/preventive-care-vaccination.",
    );
  }
}

// ---- features/vaccination must not return as a command-lens home ----
// Command lenses (Action Center / Calendar / Protocol Adherence / Workflows / their shared model) live in
// features/process-integrity; the Preventive Care (PC) operations surface is features/preventive-care-vaccination. A reappearing
// features/vaccination directory is the exact ownership drift this guard exists to block.
if (existsSync("features/vaccination")) {
  findings.push(
    "features/vaccination/ exists again. Command lenses live in features/process-integrity and the Preventive Care (PC) " +
      "operations surface is features/preventive-care-vaccination. Do not recreate a features/vaccination home for " +
      "command-lens code.",
  );
}

// ---- Parks owns NO vaccination route/folder/API path/literal (anywhere) ----
// Preventive Care (PC) / Vaccination owns vaccination operations AND execution context. Park/shed is only a top-bar scope,
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
  "features/preventive-care-vaccination/operations.tsx",
  "features/preventive-care-vaccination/execution-section.tsx",
  "features/vaccination-execution/execution-board.tsx",
  "features/vaccination-execution/shed-drilldown.tsx",
  "features/vaccination-live-tracker/live-tracker-board.tsx",
  "features/vaccination-live-tracker/params.ts",
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
      "/calendar?owner_key=<owner>, /workflows?domain=<vertical>, /workflows/{row_id}?domain=<vertical>, " +
      "Vaccination config lives at /vaccination/plan. SOP pages are per-module surfaces: /counts/sops, /feed/sops.\n" +
      "Vertical route trees should contain operational screens only.",
  );
  process.exit(1);
}

console.log("✓ IA guard passed — no nested vertical command-room/authority routes detected.");
