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

/**
 * The date window smoke-visual-live.mjs computes (see its `smokeWideWindow*`),
 * as INDIA BUSINESS DATES.
 *
 * It used to be `.toISOString().slice(0,10)`, which is a UTC instant: between
 * 00:00 and 05:30 IST that returns YESTERDAY, so a sweep run early in the
 * morning asked every analytics route for a window ending the day before and
 * judged whatever that returned. The repo rule is the India business calendar;
 * a date on a farm screen is never a UTC day.
 */
export const FARM_TIME_ZONE = "Asia/Kolkata";

export function farmDate(instant, timeZone = FARM_TIME_ZONE) {
  // en-CA renders ISO-shaped YYYY-MM-DD, and the formatter does the zone shift.
  return new Intl.DateTimeFormat("en-CA", {
    timeZone, year: "numeric", month: "2-digit", day: "2-digit",
  }).format(instant);
}

export function windowDates(now = new Date()) {
  return {
    from: farmDate(new Date(now.getTime() - 43 * 24 * 60 * 60 * 1000)),
    to: farmDate(now),
  };
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
    // The source supplies a literal fallback and this used to be called a clock
    // hole because of it — "so this one is never a gap". It was never a gap
    // because it points at a shed named `placeholder`: a page nobody opens,
    // filmed and judged and counted clean every run. A fallback to a page that
    // does not exist is a gap wearing a value.
    kind: "fixture",
    needs: "vaccinationShedPath",
    what: "the path of a real vaccination shed execution page",
    isPath: true,
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

/**
 * Values that are not a record id, whatever they are spelled like.
 *
 * `placeholder` is the one the route table itself ships, and the rest are what
 * a half-filled environment variable produces.
 */
const SENTINEL_IDS = new Set(["", "placeholder", "undefined", "null", "none", "todo", "tbd", "changeme", "example"]);

/** A uuid, or a ULID/CUID-shaped key. Anything else is not an id this farm stores. */
const RECORD_ID = /^(?:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9A-HJKMNP-TV-Z]{26}|c[a-z0-9]{24})$/i;

/**
 * Is this supplied value usable as a record id?
 *
 * SHAPE ONLY, and that limit is stated rather than hidden: a well-formed uuid
 * for a record that was deleted yesterday passes this and still renders a
 * not-found page. That is why a shape-checked id produces an `assumed` route
 * and not a `resolved` one — see `resolveRoutes`.
 */
export function fixtureIdProblem(value, { isPath = false } = {}) {
  if (value === undefined || value === null) return "none was supplied to the sweep";
  const text = String(value).trim();
  if (SENTINEL_IDS.has(text.toLowerCase())) return `the value supplied was "${text}", which is a placeholder rather than a record`;
  if (isPath) {
    if (!text.startsWith("/")) return `the value supplied ("${text}") is not a page address`;
    if (/\bplaceholder\b/.test(text)) return `the address supplied ("${text}") points at a placeholder page nobody opens`;
    return null;
  }
  if (!RECORD_ID.test(text)) return `the value supplied ("${text}") is not shaped like a record id this farm stores`;
  return null;
}

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
export function resolveRoutes(repoRoot, { fixtures = {}, now = new Date(), source = SMOKE_SOURCE, verifiedKeys = [] } = {}) {
  const ctx = { fixtures, now, dates: windowDates(now) };
  const verified = new Set(verifiedKeys);
  const all = smokeRoutes(repoRoot, source);
  const resolved = [];
  const assumed = [];
  const unresolved = [];
  for (const route of all) {
    if (!route.needsFixture) { resolved.push({ ...route, resolvedPath: route.path }); continue; }
    const holes = route.raw.match(/\$\{[^}]*\}/g) ?? [];
    let path = route.path;
    const gaps = [];
    const unverified = [];
    for (const hole of holes) {
      const spec = ROUTE_HOLES[hole];
      if (!spec) {
        gaps.push({ hole, why: `this route's address is built from \`${hole}\`, an expression the route resolver has never been taught to fill` });
        continue;
      }
      if (spec.kind === "clock") { path = path.split(hole).join(spec.fill(ctx)); continue; }
      const value = lookupFixture(fixtures, spec.needs);
      const problem = fixtureIdProblem(value, { isPath: spec.isPath });
      if (problem) {
        gaps.push({ hole, why: `this route's address needs ${spec.what}, and ${problem} (\`${spec.needs}\`)` });
        continue;
      }
      const text = String(value).trim();
      path = path.split(hole).join(spec.isPath ? text : encodeURIComponent(text));
      // An id nobody checked against a real record is an ASSUMPTION, not a
      // resolution. Junk ids used to take the sweep from 136 of 146 to a clean
      // 146 of 146 — the receipt read BEST exactly when the fixtures were worst,
      // because a not-found page still satisfies a presence-only assertion.
      if (!verified.has(spec.needs)) unverified.push(spec.needs);
    }
    if (gaps.length) unresolved.push({ ...route, gaps });
    else if (unverified.length) {
      assumed.push({
        ...route,
        resolvedPath: path,
        path,
        unverified,
        why: `this route's address was built from ${unverified.map((n) => `\`${n}\``).join(" and ")}, which the sweep was handed but never checked against a real record, so a not-found page here would be judged as the page itself`,
      });
    } else resolved.push({ ...route, resolvedPath: path, path });
  }
  return { all, resolved, assumed, unresolved };
}
