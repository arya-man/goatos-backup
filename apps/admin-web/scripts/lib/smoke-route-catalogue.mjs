// The route table lane 1 sweeps, readable by other checks.
//
// The table lives inside `smoke-visual-live.mjs`, built by `buildRoutes(...)` from
// fixtures that are looked up at run time. That script cannot be imported — it opens a
// browser at the top level — and it is being edited by other lanes right now, so
// refactoring it to export the table would be the wrong thing to do this week.
//
// So the table is read out of the source. That is deterministic and needs no change to
// a shared file, and the cost is stated rather than hidden: a route whose path is built
// from a fixture (`/goats/${goatId}`) cannot be resolved without running the fixture
// lookups, so it is returned marked `needsFixture` and the caller reports it as
// unreachable rather than quietly sweeping 130 routes and calling it 146.
//
// `routeCount` exists so a test can fail the day the table stops being readable this
// way, instead of coverage silently dropping.
import { readFileSync } from "node:fs";

const ROUTE_LINE = /\{\s*name:\s*"([^"]+)"\s*,\s*path:\s*(`[^`]*`|"[^"]*")/g;

export const SMOKE_SOURCE = "apps/admin-web/scripts/smoke-visual-live.mjs";

/**
 * @returns Array<{ name, path, needsFixture }> in the order lane 1 sweeps them.
 */
export function smokeRoutes(repoRoot, source = SMOKE_SOURCE) {
  const text = readFileSync(`${repoRoot}/${source}`, "utf8");
  const routes = [];
  const seen = new Set();
  ROUTE_LINE.lastIndex = 0;
  for (let m = ROUTE_LINE.exec(text); m; m = ROUTE_LINE.exec(text)) {
    const name = m[1];
    const raw = m[2];
    const path = raw.slice(1, -1);
    // A template hole is a fixture the sweep resolves at run time from live data.
    const needsFixture = raw.startsWith("`") && /\$\{/.test(raw);
    if (seen.has(name)) continue;
    seen.add(name);
    routes.push({ name, path, raw, needsFixture });
  }
  return routes;
}

/** Split into the ones this check can reach on its own and the ones it cannot. */
export function reachableRoutes(repoRoot, source = SMOKE_SOURCE) {
  const all = smokeRoutes(repoRoot, source);
  return {
    all,
    reachable: all.filter((route) => !route.needsFixture),
    needFixture: all.filter((route) => route.needsFixture),
  };
}


// ---------------------------------------------------------------------------
// Resolving the 28 routes whose path is a template
// ---------------------------------------------------------------------------
//
// `reachableRoutes` calls them all "needs a fixture" and that single sentence was
// doing too much work: it covered 21 routes whose only hole is a DATE WINDOW the
// smoke script computes from the clock, with no live lookup anywhere, and 7 that
// genuinely need an id out of the database. Reporting 118 of 146 with one bulk
// excuse hid 21 pages that could have been swept all along.
//
// So every hole is resolved against a named table. A hole this table does not know
// is unresolved WITH ITS OWN EXPRESSION in the reason — never folded into a group —
// because the day someone adds a fourteenth hole the receipt must say which one.

/** The date window smoke-visual-live.mjs computes (see its `smokeWideWindow*`). */
export function windowDates(now = new Date()) {
  const to = new Date(now.getTime());
  const from = new Date(now.getTime() - 43 * 24 * 60 * 60 * 1000);
  return { from: from.toISOString().slice(0, 10), to: to.toISOString().slice(0, 10) };
}

/**
 * Every template hole in the route table, and what fills it.
 *
 * `clock` holes need nothing but the current time. `fixture` holes need an id that
 * only exists in the database; each one names the id it wants, so an unresolved
 * route can say exactly what it is missing instead of "unreachable".
 */
export const ROUTE_HOLES = Object.freeze({
  "${smokeWideWindowFrom}": { kind: "clock", fill: (ctx) => ctx.dates.from },
  "${smokeWideWindowTo}": { kind: "clock", fill: (ctx) => ctx.dates.to },
  "${new Date().getFullYear()}": { kind: "clock", fill: (ctx) => String(ctx.now.getFullYear()) },
  '${vaccinationShedPath ?? "/vaccination/execution/sheds/placeholder?scope_mode=company"}': {
    // The source itself supplies a literal fallback, so this one is never a gap.
    kind: "clock",
    fill: (ctx) => ctx.fixtures.vaccinationShedPath ?? "/vaccination/execution/sheds/placeholder?scope_mode=company",
  },
  "${encodeURIComponent(toxinSopId)}": { kind: "fixture", needs: "toxinSopId", what: "the id of the seeded toxin SOP" },
  "${encodeURIComponent(workflowRowId)}": { kind: "fixture", needs: "workflowRowId", what: "the id of a workflow row" },
  "${encodeURIComponent(calendarEventId)}": { kind: "fixture", needs: "calendarEventId", what: "the id of a calendar drive event" },
  "${encodeURIComponent(goatId)}": { kind: "fixture", needs: "goatId", what: "the id of one animal" },
  "${encodeURIComponent(procurementLoadId)}": { kind: "fixture", needs: "procurementLoadId", what: "the id of a procurement load" },
  '${encodeURIComponent(sopFlowIds["counts-sop-flow"])}': { kind: "fixture", needs: "sopFlowIds.counts-sop-flow", what: "the id of the seeded counts.birth SOP" },
  '${encodeURIComponent(sopFlowIds["weighing-sop-flow"])}': { kind: "fixture", needs: "sopFlowIds.weighing-sop-flow", what: "the id of the seeded weighing.session SOP" },
  '${encodeURIComponent(sopFlowIds["feed-sop-flow"])}': { kind: "fixture", needs: "sopFlowIds.feed-sop-flow", what: "the id of the seeded feed.packing SOP" },
  '${encodeURIComponent(sopFlowIds["sales-sop-flow"])}': { kind: "fixture", needs: "sopFlowIds.sales-sop-flow", what: "the id of the seeded sales.deal SOP" },
});

function lookupFixture(fixtures, needs) {
  if (!needs.includes(".")) return fixtures[needs];
  const [group, key] = needs.split(".");
  return fixtures[group]?.[key];
}

/**
 * Fill in every hole this table knows, from the clock and from whatever fixture ids
 * the caller was able to supply.
 *
 * @returns {{ all, resolved, unresolved }} — `unresolved` entries carry `gaps`, one
 * per hole, each with its own sentence. There is deliberately no group reason.
 */
export function resolveRoutes(repoRoot, { fixtures = {}, now = new Date(), source = SMOKE_SOURCE } = {}) {
  const ctx = { fixtures, now, dates: windowDates(now) };
  const all = smokeRoutes(repoRoot, source);
  const resolved = [];
  const unresolved = [];
  for (const route of all) {
    if (!route.needsFixture) { resolved.push({ ...route, resolvedPath: route.path }); continue; }
    const holes = route.raw.match(/\$\{[^}]*\}/g) ?? [];
    let path = route.path;
    const gaps = [];
    for (const hole of holes) {
      const spec = ROUTE_HOLES[hole];
      if (!spec) {
        gaps.push({ hole, why: `this route's address is built from \`${hole}\`, an expression the route resolver has never been taught to fill` });
        continue;
      }
      if (spec.kind === "clock") { path = path.split(hole).join(spec.fill(ctx)); continue; }
      const value = lookupFixture(fixtures, spec.needs);
      if (value === undefined || value === null || value === "") {
        gaps.push({ hole, why: `this route's address needs ${spec.what}, and no \`${spec.needs}\` was supplied to the sweep` });
        continue;
      }
      path = path.split(hole).join(encodeURIComponent(String(value)));
    }
    if (gaps.length) unresolved.push({ ...route, gaps });
    else resolved.push({ ...route, resolvedPath: path, path });
  }
  return { all, resolved, unresolved };
}
