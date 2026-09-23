import test from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { reachableRoutes, smokeRoutes } from "./smoke-route-catalogue.mjs";

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
