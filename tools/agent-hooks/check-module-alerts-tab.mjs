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
// PARSING IS STRUCTURAL, NOT ORDER-BASED. Adversarial testing (2026-08-03) found the
// original parser passed two REAL violations silently, because it anchored on the source
// ORDER of struct fields: a new available module with no alerts tab was invisible if it
// declared `labelKey` above `key`, and a nav item was invisible if it declared `href`
// above `labelKey`. gofmt does not normalise field order, so both are legal edits that no
// reviewer would flag. The parser now balances braces and looks fields up BY NAME, and
// anything it cannot read is reported instead of skipped.
//
// Known remaining blind spots, stated so nobody mistakes green for proof:
//   - the parser reads Go SOURCE TEXT, not the compiled registry. A module assembled at
//     runtime (appended to the map, built by a helper) is invisible; that is why
//     RUNTIME_COMPOSED_MODULES exists and why those modules carry their own Go tests.
//   - permission gating is not modelled: an alerts item every principal is filtered out
//     of still passes here.
//   - it proves the ROUTE is hosted and rooted, not that the screen behind it renders a
//     feed scoped to the caller.
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

// Modules with no activated Alerts lens over shared task/contact truth yet. A legacy
// notification_requests compatibility feed does not close this gap. Each entry must
// say why and what unblocks it. Removing an entry is the goal; adding one is a
// maintainer decision, not a convenience.
const PENDING_ALERTS_FEED = {
  counts:
    "GET /app/counts/alerts exists as a legacy per-recipient notification_requests compatibility feed, but Counts has no activated module-scoped lens over shared task/contact truth. The legacy source must be suppressed and retired at cutover, not promoted to canonical work state.",
  feed_direction:
    "GET /app/feed/alerts exists as a legacy per-recipient notification_requests compatibility feed, but Feed has no activated module-scoped lens/nav over shared task/contact truth. The direct feed must be suppressed and retired during cutover, not preserved as work authority.",
  aas_health:
    "Health shipped its execution screens in this change with no notification feed behind them: no /health/alerts route and no health alert producer exists on any branch. Giving the bar an Alerts tab now would route operators to an empty screen.",
  milk:
    "Milk shipped in this change with no notification feed of its own. Its alert-shaped items are the VERIFIER's milk_feeding queue, which belongs to the verification module, not to Milk's own bar. Same shape as counts above.",
  pc_care:
    "PC Care shipped its execution screens (deworming / ticks removal / hoof trimming / hair trimming) with no notification feed behind them: no /app/pc-care/alerts route and no pc_care operator-alert producer exists on any branch (the pc_care entries in verification_notify_consumer.go are verifier/leadership pushes, not an operator feed). Giving the bar an Alerts tab now would route operators to an empty screen. Same shape as aas_health above.",
  toxin:
    "Toxin shipped its guided strip-test worklist before a module-scoped alert feed exists: tasks are born from feed purchases and testers open /toxin directly, but there is no /toxin/alerts route or toxin operator-alert producer yet. Giving the bar an Alerts tab now would route testers to an empty screen. Unblocked when toxin submissions/verdicts get a real feed over shared task/contact truth.",
  clock:
    "Clock In / Out shipped (maintainer decisions 2026-08-27/28) with no notification feed of its own: no /clock/alerts route and no clock alert producer exists on any branch. Its one alert-shaped message is the not-clocked-in reminder, which is deliberately a SHELL-GLOBAL banner on every screen (docs/features/clock-in-out/plan.md §4.3) rather than a feed nobody would open — an Alerts tab would route people to an empty screen. Unblocked if a real attendance feed (e.g. leadership notified of chronic non-clockers) ever ships.",
  approvals:
    "Approvals returned to the phone (maintainer decision 2026-08-05) with no notification feed of its own: no /approvals/alerts route and no approval alert producer exists on any branch. The module IS the queue -- an approver opens it to see what is waiting on them, so an Alerts tab would duplicate the one screen the module has. Unblocked when a producer notifies an approver that a request was raised; at that point the queue and the alerts feed become genuinely different lists (everything pending vs what arrived since you last looked).",
};

// The verification module composes its bar per reviewed FEATURE at runtime
// (verificationModuleForFeature), not from the static registry, so its alerts items are
// checked by their own tests rather than by parsing this registry.
const RUNTIME_COMPOSED_MODULES = new Set(["verification"]);

function read(relPath) {
  return readFileSync(resolve(repoRoot, relPath), "utf8");
}

/**
 * Parse the module nav registry into {key, status, contributions, alerts}.
 *
 * FIELD ORDER IS NOT PART OF THE RULE. An earlier version anchored on the literal
 * source order `key:` then `labelKey:` then `href:`, so simply declaring `labelKey`
 * above `key` in a module entry (or `href` above `labelKey` in a nav item) made that
 * entry INVISIBLE and the guard passed a module with no alerts tab at all. gofmt does
 * not normalise field order, so the reordering is a legal, review-invisible edit.
 * Parsing is now structural: balance the braces, then look each field up by NAME.
 *
 * Anything that fails to parse is reported as a shape error rather than skipped —
 * silence is how a guard goes blind.
 */
export function parseModuleRegistry(source) {
  const modules = [];
  const shapeErrors = [];
  const declRe = /map\[string\]moduleDefinition\{/g;
  let decl;
  let sawDecl = false;
  while ((decl = declRe.exec(source)) !== null) {
    sawDecl = true;
    const braceAt = decl.index + decl[0].length - 1;
    const body = source.slice(braceAt, braceAt + findEntryEnd(source.slice(braceAt)));
    parseRegistryBody(body, modules, shapeErrors);
  }
  if (!sawDecl) {
    shapeErrors.push(
      `${REGISTRY}: no "map[string]moduleDefinition{" declaration found — the registry shape changed and this guard is blind`,
    );
  }
  return { modules, shapeErrors };
}

function parseRegistryBody(body, modules, shapeErrors) {
  const entryRe = /"([a-z_]+)":\s*\{/g;
  let match;
  while ((match = entryRe.exec(body)) !== null) {
    const braceAt = match.index + match[0].length - 1;
    const entry = body.slice(braceAt, braceAt + findEntryEnd(body.slice(braceAt)));
    entryRe.lastIndex = braceAt + entry.length; // never re-scan inside an entry we consumed
    const mapKey = match[1];
    const head = entry.split("contributions:")[0];
    const key = /(?:^|[\s{,])key:\s*"([a-z_]+)"/.exec(head)?.[1];
    if (!key) {
      shapeErrors.push(`${REGISTRY}: registry entry "${mapKey}" has no parseable key: field — this guard cannot see it`);
      continue;
    }
    const status = /(?:^|[\s{,])status:\s*(\w+)/.exec(head)?.[1] ?? "";
    const { contributions, hasBlock } = parseContributions(entry, mapKey, shapeErrors);
    modules.push({
      key,
      status,
      contributions,
      hasContributionsBlock: hasBlock,
      alerts: contributions.find((c) => c.labelKey.startsWith("nav.alerts")) ?? null,
    });
  }
}

function parseContributions(entry, mapKey, shapeErrors) {
  const blockAt = /\[\]moduleNavContribution\{/.exec(entry);
  if (!blockAt) return { contributions: [], hasBlock: false };
  const braceAt = blockAt.index + blockAt[0].length - 1;
  const block = entry.slice(braceAt, braceAt + findEntryEnd(entry.slice(braceAt)));
  const contributions = [];
  for (const literal of braceLiterals(block.slice(1, -1))) {
    const field = (name) => new RegExp(`(?:^|[\\s{,])${name}:\\s*"([^"]*)"`).exec(literal)?.[1];
    const key = field("key");
    const labelKey = field("labelKey");
    const href = field("href");
    if (key === undefined && labelKey === undefined && href === undefined) continue;
    if (key === undefined || labelKey === undefined || href === undefined) {
      shapeErrors.push(
        `${REGISTRY}: nav item in module "${mapKey}" is missing a parseable key/labelKey/href — this guard cannot see it`,
      );
      continue;
    }
    contributions.push({ key, labelKey, href });
  }
  return { contributions, hasBlock: true };
}

/** Top-level `{...}` literals inside a composite-literal body. */
function braceLiterals(text) {
  const out = [];
  for (let i = 0; i < text.length; i += 1) {
    if (text[i] !== "{") continue;
    const end = findEntryEnd(text.slice(i));
    out.push(text.slice(i, i + end));
    i += end - 1;
  }
  return out;
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

/** Nav keys MeshaIcons.forNavKey resolves to the Bell glyph. Every branch, not just the first. */
export function bellNavKeys(iconSource) {
  const keys = new Set();
  for (const line of iconSource.matchAll(/^\s*((?:"[^"]+",?\s*)+)->\s*Bell\s*$/gm)) {
    for (const key of line[1].matchAll(/"([^"]+)"/g)) keys.add(key[1]);
  }
  return keys;
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
  const { modules, shapeErrors } = parseModuleRegistry(registry);
  failures.push(...shapeErrors);
  if (modules.length === 0) failures.push(`${REGISTRY}: parsed zero modules — the registry shape changed and this guard is blind`);

  const bells = bellNavKeys(icons);
  const { roots, consts } = rootDestinationHrefs(navHost);
  const hosted = hostedRoutes(navHost, consts);

  for (const module of modules) {
    if (module.status !== "moduleStatusAvailable") continue;
    if (RUNTIME_COMPOSED_MODULES.has(module.key)) continue;
    if (module.contributions.length === 0) {
      // No nav items at all is fine (a module can be drawer-only). A contributions BLOCK
      // that yielded nothing is not fine: the items exist in the file and this guard could
      // not read them, which is exactly how a module with no alerts tab slips through.
      if (module.hasContributionsBlock) {
        failures.push(
          `module "${module.key}" declares nav items but none of them parsed — this guard cannot see its bar, so it cannot certify the Alerts tab.`,
        );
      }
      continue;
    }

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
    // The ADDRESS must name the feature that owns the feed. A generically-named alerts
    // route reads as a shared feed, and that is not a hypothetical: "/alerts" was
    // vaccination's, looked shared, and was copied into weighing's bar, where it 403'd
    // for weighing operators. Verification is exempt (checked above) because it scopes by
    // query category across features by design.
    const expectedPrefix = `/${module.key.replace(/_/g, "-")}/`;
    const altPrefix = `/${module.key}/`;
    if (!base.startsWith(expectedPrefix) && !base.startsWith(altPrefix)) {
      failures.push(
        `module "${module.key}" alerts href "${href}" is not scoped to its own feature. Use "${altPrefix}alerts" — a generic address reads as a shared feed and gets copied into another feature's bar.`,
      );
    }
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
  const wrap = (entries) => `var moduleNavRegistry = map[string]moduleDefinition{${entries}}\n`;
  const goodRegistry = wrap(`
	"weighing": {
		key:         "weighing",
		status:      moduleStatusAvailable,
		contributions: []moduleNavContribution{
			{key: "tasks", labelKey: "nav.tasks", href: "/weighing/tasks", shared_key: "", priority: 1},
			{key: "weighing_alerts", labelKey: "nav.alerts", href: "/weighing/alerts", shared_key: "", priority: 5},
		},
	},
`);
  // A second available module with nav items and no alerts item of its own.
  const newModuleEntry = (fields, item) => `
	"breeding_ops": {
${fields}
		contributions: []moduleNavContribution{
			${item}
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
      "generically-named alerts route is caught",
      { registry: goodRegistry.replace('href: "/weighing/alerts"', 'href: "/alerts"'), icons, navHost },
      1,
    ],
    [
      "feature-named label is caught",
      { registry: goodRegistry.replace('labelKey: "nav.alerts"', 'labelKey: "nav.alerts.weighing"'), icons, navHost },
      1,
    ],
    // --- adversarial: the guard must not be blinded by legal, gofmt-stable reshuffling ---
    [
      "a NEW available module with nav items and no alerts item is caught",
      {
        registry: goodRegistry.replace(
          "}\n",
          newModuleEntry(
            '		key:         "breeding_ops",\n		status:      moduleStatusAvailable,',
            '{key: "breeding_ops", labelKey: "nav.overview", href: "/breeding-ops", shared_key: "", priority: 1},',
          ) + "}\n",
        ),
        icons,
        navHost,
      },
      1,
    ],
    [
      "a new module declaring labelKey ABOVE key is still seen (parser is field-order-blind)",
      {
        registry: goodRegistry.replace(
          "}\n",
          newModuleEntry(
            '		labelKey:    "module.breeding_ops",\n		key:         "breeding_ops",\n		status:      moduleStatusAvailable,',
            '{key: "breeding_ops", labelKey: "nav.overview", href: "/breeding-ops", shared_key: "", priority: 1},',
          ) + "}\n",
        ),
        icons,
        navHost,
      },
      1,
    ],
    [
      "a new module whose nav item declares href ABOVE labelKey is still seen",
      {
        registry: goodRegistry.replace(
          "}\n",
          newModuleEntry(
            '		key:         "breeding_ops",\n		status:      moduleStatusAvailable,',
            '{key: "breeding_ops", href: "/breeding-ops", labelKey: "nav.overview", shared_key: "", priority: 1},',
          ) + "}\n",
        ),
        icons,
        navHost,
      },
      1,
    ],
    [
      "weighing's alerts item reordered to href-before-labelKey is still seen",
      {
        registry: goodRegistry.replace(
          '{key: "weighing_alerts", labelKey: "nav.alerts", href: "/weighing/alerts"',
          '{key: "weighing_alerts", href: "/weighing/alerts", labelKey: "nav.alerts"',
        ),
        icons,
        navHost,
      },
      0,
    ],
    [
      "an unreadable nav item is reported, not silently skipped",
      {
        registry: goodRegistry.replace(
          '{key: "weighing_alerts", labelKey: "nav.alerts", href: "/weighing/alerts"',
          '{key: "weighing_alerts", labelKey: "nav.alerts", href: weighingAlertsRoute',
        ),
        icons,
        navHost,
      },
      1,
    ],
    [
      "a renamed/reshaped registry declaration is caught, not silently green",
      { registry: goodRegistry.replace("map[string]moduleDefinition{", "moduleRegistryTable{"), icons, navHost },
      1,
    ],
    [
      "an alerts icon mapped on a SECOND Bell branch is accepted",
      {
        registry: goodRegistry,
        icons: `        "alerts", "notifications" -> Bell\n        "weighing_alerts" -> Bell\n`,
        navHost,
      },
      0,
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
