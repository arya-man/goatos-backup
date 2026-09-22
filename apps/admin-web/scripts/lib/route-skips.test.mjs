import { test } from "node:test";
import assert from "node:assert/strict";
import { noRoutesLeftError, planRouteSelection, routeSkippedLine } from "./route-skips.mjs";

test("a missing fixture skips its route and keeps the rest of the selection", () => {
  const plan = planRouteSelection({
    onlyRoutes: ["calendar", "vaccination-shed-execution-detail", "weighing-sop-flow"],
    builtRouteNames: ["calendar", "weighing-sop-flow"],
    skipReasons: new Map([["vaccination-shed-execution-detail", "vaccination shed unavailable in this run"]]),
  });
  assert.deepEqual(plan.selectedNames, ["calendar", "weighing-sop-flow"]);
  assert.deepEqual(plan.skipped, [
    { name: "vaccination-shed-execution-detail", why: "vaccination shed unavailable in this run" },
  ]);
  assert.equal(noRoutesLeftError({ onlyRoutes: ["calendar"], selectedNames: plan.selectedNames, skipped: plan.skipped }), null);
});

test("a backend error on the fixture lookup is carried through as the skip reason", () => {
  const plan = planRouteSelection({
    onlyRoutes: ["calendar", "vaccination-shed-execution-detail"],
    builtRouteNames: ["calendar"],
    skipReasons: new Map([["vaccination-shed-execution-detail", "backend smoke vaccination shed lookup failed: status 500"]]),
  });
  assert.deepEqual(plan.selectedNames, ["calendar"]);
  assert.equal(
    routeSkippedLine(plan.skipped[0]),
    "route_skipped=vaccination-shed-execution-detail:backend smoke vaccination shed lookup failed: status 500",
  );
});

test("a route with no recorded reason still reports why it was skipped", () => {
  const plan = planRouteSelection({ onlyRoutes: ["goat-passport", "calendar"], builtRouteNames: ["calendar"] });
  assert.equal(plan.skipped[0].why, "fixture id not available in this run");
  assert.equal(routeSkippedLine(plan.skipped[0]), "route_skipped=goat-passport:fixture id not available in this run");
});

test("plain object skip reasons work the same as a Map", () => {
  const plan = planRouteSelection({
    onlyRoutes: ["goat-passport"],
    builtRouteNames: [],
    skipReasons: { "goat-passport": "goat unavailable in this run" },
  });
  assert.equal(plan.skipped[0].why, "goat unavailable in this run");
});

test("no selection means the full sweep, with nothing skipped", () => {
  const plan = planRouteSelection({ onlyRoutes: [], builtRouteNames: ["calendar", "alerts"] });
  assert.deepEqual(plan.selectedNames, ["calendar", "alerts"]);
  assert.deepEqual(plan.skipped, []);
  assert.equal(noRoutesLeftError({ onlyRoutes: [], selectedNames: plan.selectedNames, skipped: [] }), null);
});

test("a selection where NO route can run is still fatal, and names every reason", () => {
  const plan = planRouteSelection({
    onlyRoutes: ["vaccination-shed-execution-detail", "goat-passport"],
    builtRouteNames: ["calendar"],
    skipReasons: new Map([
      ["vaccination-shed-execution-detail", "backend smoke vaccination shed lookup failed: status 500"],
      ["goat-passport", "goat unavailable in this run"],
    ]),
  });
  assert.deepEqual(plan.selectedNames, []);
  const error = noRoutesLeftError({
    onlyRoutes: ["vaccination-shed-execution-detail", "goat-passport"],
    selectedNames: plan.selectedNames,
    skipped: plan.skipped,
  });
  assert.match(error, /not available in this run/);
  assert.match(error, /vaccination-shed-execution-detail \(backend smoke vaccination shed lookup failed: status 500\)/);
  assert.match(error, /goat-passport \(goat unavailable in this run\)/);
});

test("an empty build list with no skips keeps the original zero-routes message", () => {
  assert.equal(
    noRoutesLeftError({ onlyRoutes: [], selectedNames: [], skipped: [] }),
    "GOATOS_SMOKE_ONLY_ROUTES selected zero routes",
  );
});

test("a full sweep still logs the routes a failed lookup cost it", () => {
  const plan = planRouteSelection({
    onlyRoutes: [],
    builtRouteNames: ["calendar", "alerts"],
    skipReasons: new Map([["vaccination-shed-execution-detail", "backend smoke vaccination shed lookup failed: status 500"]]),
  });
  assert.deepEqual(plan.selectedNames, ["calendar", "alerts"]);
  assert.deepEqual(plan.skipped, [
    { name: "vaccination-shed-execution-detail", why: "backend smoke vaccination shed lookup failed: status 500" },
  ]);
  // The sweep goes on: the other routes are still checked.
  assert.equal(noRoutesLeftError({ onlyRoutes: [], selectedNames: plan.selectedNames, skipped: plan.skipped }), null);
});

test("a full sweep does not report a route that was built as skipped", () => {
  const plan = planRouteSelection({
    onlyRoutes: [],
    builtRouteNames: ["calendar", "goat-passport"],
    skipReasons: new Map([["goat-passport", "stale reason"]]),
  });
  assert.deepEqual(plan.skipped, []);
});
