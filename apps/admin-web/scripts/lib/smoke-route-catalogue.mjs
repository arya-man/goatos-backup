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
    routes.push({ name, path, needsFixture });
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
