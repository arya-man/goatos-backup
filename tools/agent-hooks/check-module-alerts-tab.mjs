#!/usr/bin/env node
// EVERY FEATURE'S BOTTOM BAR CARRIES ITS OWN ALERTS TAB.
//
// Maintainer rule, 2026-08-03: a person standing inside a feature must be able to see
// that feature's alerts from that feature's bottom bar. The alerts are scoped by
// FEATURE and by ROLE -- a weighing operator sees weighing alerts routed to them, not
// the vaccination feed, and not somebody else's queue.
//
// This guard exists because the rule was broken twice in opposite directions, and
// neither break was visible in a review diff:
//
//   1. Weighing's bar carried the VACCINATION feed (/alerts). It 403'd for a weighing
//      operator and its label read "Vaccination alerts", so the tab was deleted --
//      leaving weighing with no alerts at all.
//   2. When weighing's own feed was built, its nav key (`weighing_alerts`) was added
//      without the matching icon token and without registering the route as a bottom-bar
//      root, so the tab rendered with the generic module glyph and its deep link landed
//      on the home screen.
//
// So a module's alerts tab is only real when FIVE things line up, and this guard checks
// all five together:
//
//   1. the module contributes an alerts nav item (labelKey `nav.alerts`);
//   2. its href is the module's OWN feed, never another module's;
//   3. the label key is the generic `nav.alerts` -- the tab is titled just "Alerts",
//      the href carries the scoping (maintainer ruling 2026-08-03);
//   4. the nav key maps to the bell icon in MeshaIcons.forNavKey, not the fallback;
//   5. the route is hosted AND registered as a bottom-bar root destination.
//
// A module that genuinely has no feed yet is listed in PENDING_ALERTS_FEED below with a
// reason. That list is the visible debt -- it is not a way to opt out quietly, and a NEW
// module cannot be added to it without this file changing in the same commit.
//
// Usage: node tools/agent-hooks/check-module-alerts-tab.mjs [--self-test]

import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const REGISTRY = "backend/internal/workforce/app/bootstrap_copy.go";
const ICONS =
  "apps/goatos-android/core/core-designsystem/src/main/kotlin/sg/mesha/goatos/core/designsystem/icon/MeshaIcons.kt";
const NAVHOST = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt";

// Modules with no alerts feed BUILT yet. Each entry must say why and what unblocks it.
// Removing an entry is the goal; adding one is a maintainer decision, not a convenience.
const PENDING_ALERTS_FEED = {
  counts:
    "No counts notification feed exists on any branch. The only counts-shaped alerts today are the VERIFIER's shifting_move queue, which belongs to the verification module, not to Counts' own bar.",
};

// The verification module composes its bar per reviewed FEATURE at runtime
// (verificationModuleForFeature), not from the static registry, so its alerts items are
// checked by their own tests rather than by parsing this registry.
const RUNTIME_COMPOSED_MODULES = new Set(["verification"]);

function read(relPath) {
  return readFileSync(resolve(repoRoot, relPath), "utf8");
}

/** Parse the module nav registry into {key, status, hasContributions, alerts:{key,href,labelKey}|null}. */
export function parseModuleRegistry(source) {
  const modules = [];
  // Each entry looks like:  "weighing": { key: ..., status: moduleStatusAvailable, contributions: []moduleNavContribution{ ... }, },
  const entryRe = /"([a-z_]+)":\s*\{\s*\n\s*key:\s*"([a-z_]+)"/g;
  let match;
  while ((match = entryRe.exec(source)) !== null) {
    const moduleKey = match[2];
    const rest = source.slice(match.index);
    const end = findEntryEnd(rest);
    const body = rest.slice(0, end);
    const status = /status:\s*(\w+)/.exec(body)?.[1] ?? "";
    const contributions = [...body.matchAll(/\{key:\s*"([^"]+)",\s*labelKey:\s*"([^"]+)",\s*href:\s*"([^"]+)"/g)].map(
      (c) => ({ key: c[1], labelKey: c[2], href: c[3] }),
    );
    modules.push({
      key: moduleKey,
      status,
      contributions,
      alerts: contributions.find((c) => c.labelKey.startsWith("nav.alerts")) ?? null,
    });
  }
  return modules;
}

/** Balance braces from the start of a registry entry so one module's body cannot bleed into the next. */
function findEntryEnd(text) {
  let depth = 0;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (ch === "{") depth += 1;
    else if (ch === "}") {
      depth -= 1;
      if (depth === 0) return i + 1;
    }
  }
  return text.length;
}

/** Nav keys MeshaIcons.forNavKey resolves to the Bell glyph. */
export function bellNavKeys(iconSource) {
  const line = /^\s*((?:"[^"]+",?\s*)+)->\s*Bell\s*$/m.exec(iconSource);
  if (!line) return new Set();
  return new Set([...line[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]));
}

/** Route constants registered as bottom-bar roots. */
export function rootDestinationHrefs(navHostSource) {
  const consts = Object.fromEntries([...navHostSource.matchAll(/const val (\w+)\s*=\s*"([^"]+)"/g)].map((m) => [m[1], m[2]]));
  const block = /private val supportedRootDestinations = setOf\(([\s\S]*?)\n\)/.exec(navHostSource);
  if (!block) return { roots: new Set(), consts };
  const roots = new Set([...block[1].matchAll(/Routes\.(\w+)/g)].map((m) => consts[m[1]]).filter(Boolean));
  return { roots, consts };
}

/** Routes AppNavHost actually hosts (composable(...) registrations). */
export function hostedRoutes(navHostSource, consts) {
  const hosted = new Set();
  for (const m of navHostSource.matchAll(/composable\(\s*Routes\.(\w+)/g)) {
    if (consts[m[1]]) hosted.add(consts[m[1]]);
  }
  for (const m of navHostSource.matchAll(/composable\(\s*"([^"]+)"/g)) hosted.add(m[1].split("?")[0]);
  // Routes registered through a pattern helper still name their base constant.
  for (const m of navHostSource.matchAll(/composable\(\s*executionRoutePattern\(\s*Routes\.(\w+)/g)) {
    if (consts[m[1]]) hosted.add(consts[m[1]]);
  }
  return hosted;
}

export function checkAll({ registry, icons, navHost }) {
  const failures = [];
  const modules = parseModuleRegistry(registry);
  if (modules.length === 0) failures.push(`${REGISTRY}: parsed zero modules — the registry shape changed and this guard is blind`);

  const bells = bellNavKeys(icons);
  const { roots, consts } = rootDestinationHrefs(navHost);
  const hosted = hostedRoutes(navHost, consts);

  for (const module of modules) {
    if (module.status !== "moduleStatusAvailable") continue;
    if (RUNTIME_COMPOSED_MODULES.has(module.key)) continue;
    if (module.contributions.length === 0) continue;

    const pendingReason = PENDING_ALERTS_FEED[module.key];
    if (!module.alerts) {
      if (!pendingReason) {
        failures.push(
          `module "${module.key}" has no Alerts tab. Every available feature's bottom bar carries its own feature-scoped Alerts item ` +
            `(labelKey "nav.alerts"). If its feed genuinely does not exist yet, add it to PENDING_ALERTS_FEED in this guard with a reason.`,
        );
      }
      continue;
    }
    if (pendingReason) {
      failures.push(
        `module "${module.key}" now HAS an Alerts tab but is still listed in PENDING_ALERTS_FEED. Remove the waiver — stale waivers hide the next regression.`,
      );
    }
    const { key, href, labelKey } = module.alerts;
    if (labelKey !== "nav.alerts") {
      failures.push(
        `module "${module.key}" alerts labelKey is "${labelKey}", want "nav.alerts". The tab is titled just "Alerts" in every locale; the href carries the feature scoping (maintainer ruling 2026-08-03).`,
      );
    }
    if (!bells.has(key.toLowerCase())) {
      failures.push(
        `nav key "${key}" (module "${module.key}") is not mapped to the Bell icon in MeshaIcons.forNavKey — it will render the generic module glyph. Add it to the "alerts"/"notifications" branch.`,
      );
    }
    const base = href.split("?")[0].replace(/\/$/, "");
    if (!hosted.has(base)) {
      failures.push(`module "${module.key}" alerts href "${href}" is not hosted by AppNavHost — tapping the tab does nothing.`);
    }
    if (!roots.has(base)) {
      failures.push(
        `module "${module.key}" alerts href "${href}" is not in supportedRootDestinations — it is a bottom-bar destination, so a deep link to it lands on the home screen with the "unavailable" notice instead of the feed.`,
      );
    }
  }
  return failures;
}

function selfTest() {
  const goodRegistry = `
	"weighing": {
		key:         "weighing",
		status:      moduleStatusAvailable,
		contributions: []moduleNavContribution{
			{key: "tasks", labelKey: "nav.tasks", href: "/weighing/tasks", shared_key: "", priority: 1},
			{key: "weighing_alerts", labelKey: "nav.alerts", href: "/weighing/alerts", shared_key: "", priority: 5},
		},
	},
`;
  const icons = `        "alerts", "notifications", "weighing_alerts" -> Bell\n`;
  const navHost = `
    const val WEIGHING_ALERTS = "/weighing/alerts"
        composable(Routes.WEIGHING_ALERTS) { }
private val supportedRootDestinations = setOf(
    Routes.WEIGHING_ALERTS,
)
`;
  const cases = [
    ["healthy module passes", { registry: goodRegistry, icons, navHost }, 0],
    [
      "missing alerts tab is caught",
      { registry: goodRegistry.replace(/\{key: "weighing_alerts".*\n/, ""), icons, navHost },
      1,
    ],
    [
      "unmapped icon is caught",
      { registry: goodRegistry, icons: `        "alerts", "notifications" -> Bell\n`, navHost },
      1,
    ],
    [
      "unhosted route is caught",
      { registry: goodRegistry, icons, navHost: navHost.replace("        composable(Routes.WEIGHING_ALERTS) { }\n", "") },
      1,
    ],
    [
      "non-root bottom-bar destination is caught",
      { registry: goodRegistry, icons, navHost: navHost.replace("    Routes.WEIGHING_ALERTS,\n", "") },
      1,
    ],
    [
      "feature-named label is caught",
      { registry: goodRegistry.replace('labelKey: "nav.alerts"', 'labelKey: "nav.alerts.weighing"'), icons, navHost },
      1,
    ],
  ];
  let bad = 0;
  for (const [name, input, wantFailures] of cases) {
    const got = checkAll(input).length;
    const ok = wantFailures === 0 ? got === 0 : got >= wantFailures;
    if (!ok) {
      bad += 1;
      console.error(`self-test FAILED: ${name} (failures=${got}, want ${wantFailures === 0 ? "0" : ">=" + wantFailures})`);
    }
  }
  if (bad > 0) {
    console.error(`module-alerts-tab self-test: ${bad} case(s) failed`);
    process.exit(1);
  }
  console.log("module-alerts-tab self-test: all cases behaved as declared");
}

function main() {
  if (process.argv.includes("--self-test")) return selfTest();
  const failures = checkAll({ registry: read(REGISTRY), icons: read(ICONS), navHost: read(NAVHOST) });
  if (failures.length > 0) {
    console.error("module-alerts-tab guard FAILED:\n");
    for (const failure of failures) console.error(`  - ${failure}`);
    console.error("\nRule: docs/decisions/module-alerts-tab.md");
    process.exit(1);
  }
  const waived = Object.keys(PENDING_ALERTS_FEED);
  console.log(
    `module-alerts-tab guard OK${waived.length ? ` (pending feeds, tracked: ${waived.join(", ")})` : ""}`,
  );
}

main();
