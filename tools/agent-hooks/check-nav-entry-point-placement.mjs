#!/usr/bin/env node

// check-nav-entry-point-placement.mjs — a FEATURE ENTRY POINT may never live in the
// top-right app bar. It belongs in the bottom bar (or the module drawer, per the 2+ modules
// rule), because a user must find the same thing in the same place in every module.
//
// THE DISTINCTION THIS GUARD ENCODES
//   LEGITIMATE app-bar action — an action ON the current screen: refresh/sync, filter,
//     search-within-this-list, a screen-scoped overflow, a create/record drill for the very
//     list being viewed.
//   NOT legitimate — a doorway to a DIFFERENT feature surface: alerts, inbox, videos,
//     profile, settings-as-a-feature, another module. Those are nav destinations. Nav
//     destinations are composed by the backend nav registry and rendered as nav chrome.
//
// WORKED EXAMPLE (the incident that produced this rule)
//   The weighing operator screen ("My work") shipped a Bell in the top-right of the app bar
//   next to the sync icon. Vaccination ships the same concept correctly: a bottom bar of
//   [ Drives | Alerts ] with the bell on the Alerts TAB and nothing top-right. One app, one
//   concept, two places — that inconsistency is the defect.
//
// WHY THESE CHECKS AND NOT A `grep -i bell`
//   A literal token list enforces the string from one incident, not the rule. The structural
//   fact that generalizes is that the backend nav registry
//   (backend/internal/workforce/app/bootstrap_copy.go) is the SSOT for what IS a navigation
//   destination: every L0 `href` and every `nav.*` English label is declared there. This guard
//   reads that file and derives its forbidden set, so the day someone adds a "Reports" nav item
//   the guard starts rejecting a Reports icon in an app bar without being edited.
//
// RULES (evaluated inside every `actions = { ... }` block — the Compose app-bar action slot)
//   nav-route-in-appbar   the block routes somewhere: `navController.navigate(`, a `Routes.X`
//                         constant whose AppNavHost value is a registry L0 href, or a literal
//                         registry L0 href. An app-bar action must act, not travel.
//   nav-named-appbar-entry a control in the block is NAMED after a nav destination — its
//                         contentDescription string resource resolves (default values/strings.xml)
//                         to a registry nav label ("Alerts", "Calendar", "Videos", ...), or the
//                         literal label appears. Being named after a destination is what makes it
//                         an entry point, whether or not it is wired yet.
//   inert-appbar-entry    a control with a no-op handler (`onClick = {}`). A control that does
//                         nothing to the current screen is by definition not an action on it; it
//                         is a parked entry point. This is exactly how the weighing bell shipped.
//
// BLIND SPOTS — stated, not hidden. This is a textual Kotlin scan with no type resolution.
//   1. Only `actions = { ... }` slots are seen. A feature that hand-rolls its own header Row, or
//      wraps the actions in a custom composable defined in another file, is INVISIBLE here.
//   2. Indirect navigation is only caught when the route constant / literal href / nav-label
//      naming is present in the block. A neutrally-named lambda (`onOpenThing()`) whose target is
//      chosen in AppNavHost is MISSED.
//   3. Nav labels are matched on ENGLISH copy against the default `values/strings.xml`. A
//      localized-only string, or a label built at runtime by concatenation, is MISSED.
//   4. It cannot judge intent: a create/record drill ("+" → add a birth to THIS list) navigates
//      and is deliberately allowed via the ALLOWED_ROUTE_HINTS list below; that allowance is a
//      hole a determined author can drive a feature through by naming a route "…/add".
//   5. Android only. apps/admin-web (the global shell top bar) is NOT scanned — its disabled
//      notifications bell is recorded in the survey and in the decision doc, not here.
//   6. Diff-scoped by default: a violation already on main stays green until its file is touched.
//      Pre-existing violations are pinned in tools/agent-hooks/nav-entry-point-baseline.txt.
//
// Modes:
//   (default)     diff-scoped: scan only android .kt changed vs $MOBILE_GUARD_BASE (or origin/main).
//   --all         audit the whole apps/goatos-android tree (backlog view).
//   --self-test   run the adversarial fixtures and exit.
//
// Escape hatch: append `nav-placement:ignore: <reason>` on the offending line.

import { execSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const REGISTRY = "backend/internal/workforce/app/bootstrap_copy.go";
const BASELINE = "tools/agent-hooks/nav-entry-point-baseline.txt";

const isAndroidKt = (rel) =>
  rel.startsWith("apps/goatos-android/") &&
  rel.endsWith(".kt") &&
  rel.includes("/src/main/") &&
  !rel.includes("/build/") &&
  !/(?:Test|Fake|Preview)\w*\.kt$/.test(rel);

// A create/record drill for the list on screen is an action on that screen, not a doorway to
// another feature. Recognized by route shape, and deliberately narrow.
const ALLOWED_ROUTE_HINTS = [/_ADD$/, /_NEW$/, /_CREATE$/];

// ---------------------------------------------------------------------------
// Registry-derived forbidden set (nav hrefs + nav labels)
// ---------------------------------------------------------------------------

export function registryFacts(registrySource) {
  const hrefs = new Set();
  for (const m of registrySource.matchAll(/href:\s*"([^"]+)"/g)) {
    hrefs.add(m[1].split("?")[0]);
  }
  for (const m of registrySource.matchAll(/landingHref:\s*"([^"]+)"/g)) {
    if (m[1]) hrefs.add(m[1].split("?")[0]);
  }
  const labels = new Set();
  const en = /"en":\s*\{([\s\S]*?)\n\t\t\},/.exec(registrySource);
  const scope = en ? en[1] : registrySource;
  for (const m of scope.matchAll(/"nav\.[a-z_.]+":\s*"([^"]+)"/g)) labels.add(m[1]);
  return { hrefs, labels };
}

// Routes.KT_NAME -> "/href" from AppNavHost.kt
export function routeConstants(hostSource) {
  const out = new Map();
  for (const m of hostSource.matchAll(/const\s+val\s+(\w+)\s*=\s*"([^"]*)"/g)) out.set(m[1], m[2]);
  return out;
}

// string resource name -> English text, from every default values/strings.xml
export function stringResources(files) {
  const out = new Map();
  for (const src of files) {
    for (const m of src.matchAll(/<string\s+name="([^"]+)"[^>]*>([\s\S]*?)<\/string>/g)) {
      out.set(m[1], m[2].replace(/\\'/g, "'").trim());
    }
  }
  return out;
}

// ---------------------------------------------------------------------------
// Extract every `actions = { ... }` slot with its start line
// ---------------------------------------------------------------------------

export function actionBlocks(source) {
  const blocks = [];
  const re = /\bactions\s*=\s*\{/g;
  let m;
  while ((m = re.exec(source)) !== null) {
    let depth = 1;
    let i = m.index + m[0].length;
    while (i < source.length && depth > 0) {
      const c = source[i];
      if (c === "{") depth++;
      else if (c === "}") depth--;
      i++;
    }
    blocks.push({
      line: source.slice(0, m.index).split("\n").length,
      body: source.slice(m.index + m[0].length, i - 1),
    });
    re.lastIndex = i;
  }
  return blocks;
}

export function findingsForSource(source, facts) {
  const { navHrefs, navLabels, routes, strings } = facts;
  const findings = [];
  // Mask comments so a KDoc that explains why the bell MOVED does not trip the guard, but keep
  // newlines (line numbers must stay true) and keep the `nav-placement:ignore` escape hatch,
  // which is by definition written in a comment.
  const mask = (text) =>
    (/nav-placement:ignore/.test(text) ? "nav-placement:ignore" : "") +
    text.replace(/[^\n]/g, "");
  const clean = source
    .replace(/\/\*[\s\S]*?\*\//g, mask)
    .replace(/\/\/[^\n]*/g, mask);
  for (const block of actionBlocks(clean)) {
    const body = block.body;
    if (/nav-placement:ignore/.test(body)) continue;

    // Rule 1 — navigation out of an app-bar action.
    if (/\bnavController\s*\.\s*navigate\s*\(/.test(body)) {
      findings.push({
        line: block.line,
        rule: "nav-route-in-appbar",
        message: "app-bar action navigates (navController.navigate); a destination belongs in the bottom bar/drawer",
      });
    }
    for (const m of body.matchAll(/\bRoutes\s*\.\s*([A-Z][A-Z0-9_]*)\b/g)) {
      const name = m[1];
      if (ALLOWED_ROUTE_HINTS.some((r) => r.test(name))) continue;
      const href = routes.get(name);
      if (href && navHrefs.has(href)) {
        findings.push({
          line: block.line,
          rule: "nav-route-in-appbar",
          message: `app-bar action targets Routes.${name} ("${href}"), a backend nav-registry destination; render it as a nav item, not a top-right icon`,
        });
      }
    }
    for (const m of body.matchAll(/"(\/[a-z0-9/_-]+)"/g)) {
      if (navHrefs.has(m[1])) {
        findings.push({
          line: block.line,
          rule: "nav-route-in-appbar",
          message: `app-bar action references the nav-registry href "${m[1]}"; that is an L0 destination, not a screen action`,
        });
      }
    }

    // Rule 2 — a control NAMED after a nav destination.
    const named = new Set();
    for (const m of body.matchAll(/R\.string\.(\w+)/g)) {
      const text = strings.get(m[1]);
      if (text && navLabels.has(text)) named.add(`${text} (R.string.${m[1]})`);
    }
    for (const m of body.matchAll(/contentDescription\s*=\s*"([^"]+)"/g)) {
      if (navLabels.has(m[1])) named.add(`"${m[1]}"`);
    }
    for (const label of named) {
      findings.push({
        line: block.line,
        rule: "nav-named-appbar-entry",
        message: `app-bar action is named after the nav destination ${label}; a nav destination is composed by the backend registry and rendered as a bottom-bar/drawer item`,
      });
    }

    // Rule 3 — an inert control: a parked entry point, not an action on this screen.
    if (/onClick\s*=\s*\{\s*\}/.test(body)) {
      findings.push({
        line: block.line,
        rule: "inert-appbar-entry",
        message: "app-bar action has a no-op onClick; a control that does nothing to this screen is a parked feature entry point — put it in the bottom bar or delete it",
      });
    }
  }
  return findings;
}

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

function walkKt(dir, out = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "build") continue;
      walkKt(full, out);
    } else if (entry.name.endsWith(".kt")) {
      const rel = relative(repo, full);
      if (isAndroidKt(rel)) out.push(rel);
    }
  }
  return out;
}

function walkStrings(dir, out = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "build") continue;
      walkStrings(full, out);
    } else if (entry.name === "strings.xml" && /\/res\/values\/strings\.xml$/.test(full)) {
      out.push(readFileSync(full, "utf8"));
    }
  }
  return out;
}

function loadFacts() {
  const registry = readFileSync(join(repo, REGISTRY), "utf8");
  const { hrefs, labels } = registryFacts(registry);
  const host = readFileSync(
    join(repo, "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt"),
    "utf8",
  );
  return {
    navHrefs: hrefs,
    navLabels: labels,
    routes: routeConstants(host),
    strings: stringResources(walkStrings(join(repo, "apps/goatos-android"))),
  };
}

function loadBaseline() {
  const p = join(repo, BASELINE);
  if (!existsSync(p)) return new Set();
  return new Set(
    readFileSync(p, "utf8")
      .split("\n")
      .map((s) => s.replace(/#.*$/, "").trim())
      .filter(Boolean),
  );
}

function changedFiles() {
  const base = process.env.MOBILE_GUARD_BASE || "origin/main";
  for (const range of [`${base}...HEAD`, "HEAD~1...HEAD"]) {
    try {
      execSync(`git rev-parse --verify --quiet ${range.split("...")[0]}^{commit}`, { cwd: repo, stdio: "ignore" });
      return execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" })
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean)
        .filter(isAndroidKt);
    } catch {
      /* try next range */
    }
  }
  return null;
}

function selfTest() {
  const facts = {
    navHrefs: new Set(["/alerts", "/weighing", "/calendar"]),
    navLabels: new Set(["Alerts", "Calendar", "Videos"]),
    routes: new Map([
      ["ALERTS", "/alerts"],
      ["COUNTS_BIRTH_ADD", "/counts/birth-death/add"],
    ]),
    strings: new Map([
      ["weighing_alerts", "Alerts"],
      ["weighing_refresh", "Refresh"],
      ["sheds_filter", "Filter park"],
    ]),
  };

  // ADVERSARIAL 1 — the EXACT shipped violation: a bell in the app bar, unwired.
  const shipped = `
    MeshaScreenHeader(
      title = "My work",
      actions = {
        SyncIconButton(isSyncing = state.loading, onSync = onRefresh, contentDescription = stringResource(R.string.weighing_refresh))
        MeshaIconButton(icon = MeshaIcons.Bell, contentDescription = stringResource(R.string.weighing_alerts), onClick = {})
      },
    )`;
  const shippedRules = findingsForSource(shipped, facts).map((f) => f.rule);
  for (const want of ["nav-named-appbar-entry", "inert-appbar-entry"]) {
    if (!shippedRules.includes(want)) {
      throw new Error(`self-test: shipped weighing-bell violation not caught by ${want} (got ${shippedRules.join(",")})`);
    }
  }

  // ADVERSARIAL 2 — a WIRED top-right entry point that navigates to a nav destination.
  const wired = `
    MeshaScreenHeader(actions = {
      MeshaIconButton(icon = MeshaIcons.Inbox, contentDescription = "Inbox", onClick = { navController.navigate(Routes.ALERTS) })
    })`;
  if (!findingsForSource(wired, facts).some((f) => f.rule === "nav-route-in-appbar")) {
    throw new Error("self-test: navigating app-bar action was not caught");
  }

  // ADVERSARIAL 3 — same, spelled with a literal href instead of the constant.
  const literal = `
    MeshaScreenHeader(actions = {
      MeshaIconButton(icon = MeshaIcons.Bell, contentDescription = "Notifications", onClick = { onOpen("/alerts") })
    })`;
  if (!findingsForSource(literal, facts).some((f) => f.rule === "nav-route-in-appbar")) {
    throw new Error("self-test: literal nav href in an app-bar action was not caught");
  }

  // GOOD 1 — legitimate screen-scoped actions: refresh + filter.
  const good = `
    MeshaScreenHeader(actions = {
      HeaderIconButton(onClick = onOpenFilters, icon = MeshaIcons.Filter, contentDescription = stringResource(R.string.sheds_filter))
      SyncIconButton(isSyncing = state.isRefreshing, onSync = onRefresh, contentDescription = stringResource(R.string.weighing_refresh))
    })`;
  let f = findingsForSource(good, facts);
  if (f.length) throw new Error(`self-test: false positive on refresh/filter actions: ${JSON.stringify(f)}`);

  // GOOD 2 — a create drill for the list on screen (allowed by ALLOWED_ROUTE_HINTS).
  const create = `
    MeshaScreenHeader(actions = {
      Box(modifier = Modifier.clickable { navigateTo(Routes.COUNTS_BIRTH_ADD) }) { Icon(MeshaIcons.Plus, null) }
    })`;
  f = findingsForSource(create, facts);
  if (f.length) throw new Error(`self-test: false positive on create-drill action: ${JSON.stringify(f)}`);

  // GOOD 3 — a KDoc that merely NAMES the moved bell must not trip the guard.
  const documented = `
    // The Alerts bell used to live here; it now lives on the Alerts bottom-bar tab.
    MeshaScreenHeader(actions = {
      SyncIconButton(isSyncing = state.loading, onSync = onRefresh, contentDescription = stringResource(R.string.weighing_refresh))
    })`;
  f = findingsForSource(documented, facts);
  if (f.length) throw new Error(`self-test: false positive on documentation comment: ${JSON.stringify(f)}`);

  // GOOD 4 — the escape hatch is honored.
  const excused = `
    MeshaScreenHeader(actions = {
      MeshaIconButton(icon = MeshaIcons.Bell, contentDescription = stringResource(R.string.weighing_alerts), onClick = {}) // nav-placement:ignore: baseline
    })`;
  if (findingsForSource(excused, facts).length) throw new Error("self-test: nav-placement:ignore was not honored");

  // The registry parser must actually find the real hrefs/labels, or every rule silently no-ops.
  const real = registryFacts(readFileSync(join(repo, REGISTRY), "utf8"));
  if (!real.hrefs.has("/alerts") || !real.labels.has("Alerts")) {
    throw new Error("self-test: registry parse did not recover /alerts + \"Alerts\" from bootstrap_copy.go");
  }

  console.log("nav-entry-point-placement self-test: ok (3 adversarial, 4 compliant, registry parse verified)");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const facts = loadFacts();
const baseline = loadBaseline();
const all = process.argv.includes("--all");
let files;
if (all) {
  files = walkKt(join(repo, "apps/goatos-android"));
} else {
  files = changedFiles();
  if (files === null) {
    console.log("nav-entry-point-placement: skipped (no git diff base; run with --all to audit the whole tree)");
    process.exit(0);
  }
  if (files.length === 0) {
    console.log("nav-entry-point-placement: ok (no android Kotlin changed)");
    process.exit(0);
  }
}

const findings = [];
let baselined = 0;
for (const rel of files) {
  let source;
  try {
    source = readFileSync(join(repo, rel), "utf8");
  } catch {
    continue;
  }
  for (const f of findingsForSource(source, facts)) {
    const key = `${rel}:${f.rule}`;
    if (baseline.has(key)) {
      baselined++;
      continue;
    }
    findings.push({ ...f, rel });
  }
}

if (findings.length) {
  console.error(
    `nav-entry-point-placement: ${findings.length} feature entry point(s) in the app bar ` +
      "(see docs/decisions/nav-entry-point-placement.md)",
  );
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error("A feature entry point belongs in the bottom bar (or the module drawer). The app bar is for actions ON this screen.");
  process.exit(1);
}
console.log(
  `nav-entry-point-placement: ok (${files.length} android file(s) scanned; ${baselined} baselined)`,
);
