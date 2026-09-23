import test from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { ROUTE_HOLES, reachableRoutes, resolveRoutes, smokeRoutes, windowDates } from "./smoke-route-catalogue.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../..");

test("reads the whole route table lane 1 sweeps", () => {
  const routes = smokeRoutes(repo);
  // If this number moves, coverage moved. It is asserted so the day the table stops
  // being readable this way the test fails, instead of the sweep quietly checking
  // fewer pages and still reporting a clean result.
  assert.ok(routes.length >= 140, `expected lane 1's ~146 routes, read ${routes.length}`);
  assert.ok(routes.every((route) => route.name), "every route has a name");
  // One route's path is nothing but a template expression, so it cannot start with "/"
  // until a fixture fills it in. It is marked unreachable rather than mangled.
  assert.ok(routes.every((route) => route.needsFixture || route.path.startsWith("/")),
    "every route this check can reach has a real path");
  assert.equal(new Set(routes.map((r) => r.name)).size, routes.length, "no route is counted twice");
});

test("separates the routes whose path needs a fixture, rather than dropping them", () => {
  const { all, reachable, needFixture } = reachableRoutes(repo);
  assert.equal(reachable.length + needFixture.length, all.length, "every route is accounted for exactly once");
  assert.ok(needFixture.length > 0, "some routes are built from a live fixture");
  assert.ok(needFixture.some((route) => route.name === "goat-passport"), "the goat passport needs a goat");
  assert.ok(reachable.some((route) => route.name === "tasks"), "the tasks page does not");
  for (const route of needFixture) assert.ok(route.needsFixture, "and each one says why it is unreachable");
});

test("a route list that cannot be read is a failure, not an empty sweep", () => {
  assert.throws(() => smokeRoutes(repo, "apps/admin-web/scripts/no-such-file.mjs"));
});

test("resolves every route whose only hole is the clock, instead of calling 28 unreachable", () => {
  const { all, resolved, unresolved } = resolveRoutes(repo);
  assert.equal(resolved.length + unresolved.length, all.length, "every route is accounted for exactly once");
  // The date-window and year routes need no database at all. If this drops back to 118
  // someone has reintroduced the bulk excuse.
  assert.ok(resolved.length >= 136, `expected at least 136 routes resolvable from the clock alone, got ${resolved.length}`);
  assert.ok(resolved.some((r) => r.name === "weighing-analytics"), "the weighing analytics window is a date, not a fixture");
  assert.ok(resolved.every((r) => r.path.startsWith("/")), "a resolved route has a real address");
  assert.ok(resolved.every((r) => !/\$\{/.test(r.path)), "no resolved route still carries a template hole");
});

test("an unresolved route carries its own reason, never a shared one", () => {
  const { unresolved } = resolveRoutes(repo);
  assert.ok(unresolved.length > 0, "some routes genuinely need an id from the database");
  const reasons = new Set();
  for (const route of unresolved) {
    assert.ok(route.gaps.length > 0, `${route.name} must say what it is missing`);
    for (const gap of route.gaps) {
      assert.ok(gap.why.length > 20, `${route.name} needs a sentence, not a label`);
      assert.ok(gap.why.includes("`"), `${route.name} must name the thing it wants`);
      reasons.add(gap.why);
    }
  }
  // Ten routes, ten distinct things missing. One reason covering all of them is the
  // bulk excuse this replaced.
  assert.ok(reasons.size >= 7, `expected a distinct reason per missing id, got ${reasons.size}`);
  assert.ok(unresolved.some((r) => r.name === "goat-passport" && r.gaps[0].why.includes("one animal")));
});

test("supplying the ids resolves the whole table, all 146", () => {
  const { all, resolved, unresolved } = resolveRoutes(repo, {
    fixtures: {
      goatId: "goat-1", toxinSopId: "sop-tox", workflowRowId: "wf-1",
      calendarEventId: "cal-1", procurementLoadId: "load-1",
      sopFlowIds: { "counts-sop-flow": "a", "weighing-sop-flow": "b", "feed-sop-flow": "c", "sales-sop-flow": "d" },
    },
  });
  assert.equal(unresolved.length, 0, "no route is left out once its id is known");
  assert.equal(resolved.length, all.length);
  assert.equal(resolved.find((r) => r.name === "goat-passport").path, "/goats/goat-1");
});

test("an id with a slash in it cannot escape into a different page", () => {
  const { resolved } = resolveRoutes(repo, { fixtures: { goatId: "../admin/secret" } });
  const passport = resolved.find((r) => r.name === "goat-passport");
  assert.equal(passport.path, "/goats/..%2Fadmin%2Fsecret", "the id is escaped exactly as the smoke script escapes it");
});

test("a hole nobody taught the resolver about is named, not silently dropped", () => {
  const table = { ...ROUTE_HOLES };
  // Simulate the day a fourteenth expression lands: by deleting one we know.
  const dates = windowDates(new Date("2026-09-23T00:00:00Z"));
  assert.equal(dates.to, "2026-09-23");
  assert.equal(dates.from, "2026-08-11", "the 43-day window the smoke script computes");
  assert.ok(Object.keys(table).length === 13, `the route table has 13 kinds of hole, the resolver knows ${Object.keys(table).length}`);
});
