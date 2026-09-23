import test from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { ROUTE_HOLES, farmDate, fixtureIdProblem, reachableRoutes, resolveRoutes, smokeRoutes, windowDates } from "./smoke-route-catalogue.mjs";

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
  const { all, resolved, assumed, unresolved } = resolveRoutes(repo);
  assert.equal(resolved.length + assumed.length + unresolved.length, all.length, "every route is accounted for exactly once");
  // The date-window and year routes need no database at all. If this drops back to 118
  // someone has reintroduced the bulk excuse.
  assert.ok(resolved.length >= 135, `expected at least 135 routes resolvable from the clock alone, got ${resolved.length}`);
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
      assert.ok(gap.why.includes(route.name), `${route.name} must name its own page`);
      assert.ok(!/[`$]/.test(gap.why), `a finding may not print code at a person: "${gap.why}"`);
      assert.ok("needs" in gap, `${route.name} carries the machine name in a field, not the sentence`);
      reasons.add(gap.why);
    }
  }
  // Ten routes, ten distinct things missing. One reason covering all of them is the
  // bulk excuse this replaced.
  assert.equal(reasons.size, unresolved.flatMap((r) => r.gaps).length,
    "every gap has its own sentence; two toxin pages used to share one");
  assert.ok(unresolved.some((r) => r.name === "goat-passport" && r.gaps[0].needs === "goatId"),
    "and the machine name rides a field beside the sentence");
});

const UUID = (n) => `3f2504e0-4f89-41d3-9a0c-0305e82c33${String(n).padStart(2, "0")}`;
const REAL_IDS = {
  goatId: UUID(1), toxinSopId: UUID(2), workflowRowId: UUID(3),
  calendarEventId: UUID(4), procurementLoadId: UUID(5),
  vaccinationShedPath: `/vaccination/execution/sheds/${UUID(6)}?scope_mode=company`,
  sopFlowIds: { "counts-sop-flow": UUID(7), "weighing-sop-flow": UUID(8), "feed-sop-flow": UUID(9), "sales-sop-flow": UUID(10) },
};

test("an id nobody checked is ASSUMED, never resolved", () => {
  // This is the finding that mattered: junk ids used to take the sweep from
  // 136 of 146 to a clean 146 of 146, so the receipt read best exactly when
  // the fixtures were worst.
  const { all, resolved, assumed, unresolved } = resolveRoutes(repo, { fixtures: REAL_IDS });
  assert.equal(unresolved.length, 0, "a well-formed id is not a gap");
  assert.equal(assumed.length, all.length - resolved.length, "it is an assumption instead");
  assert.ok(assumed.some((r) => r.name === "goat-passport"));
  assert.ok(!resolved.some((r) => r.name === "goat-passport"), "and is never counted as resolved");
  for (const route of assumed) {
    assert.match(route.why, /never checked against a real record/);
    assert.ok(!/[`$]/.test(route.why), "an assumed route's sentence prints no code either");
    assert.ok(route.unverified.length > 0, `${route.name} names the id it trusted`);
  }
});

test("a caller that checked the id against a real record gets a resolved route", () => {
  const { all, resolved, assumed } = resolveRoutes(repo, {
    fixtures: REAL_IDS,
    verifiedKeys: ["goatId", "toxinSopId", "workflowRowId", "calendarEventId", "procurementLoadId", "vaccinationShedPath",
      "sopFlowIds.counts-sop-flow", "sopFlowIds.weighing-sop-flow", "sopFlowIds.feed-sop-flow", "sopFlowIds.sales-sop-flow"],
  });
  assert.equal(assumed.length, 0, "nothing is assumed once every id was checked");
  assert.equal(resolved.length, all.length, "and the whole table resolves");
  assert.equal(resolved.find((r) => r.name === "goat-passport").path, `/goats/${REAL_IDS.goatId}`);
});

test("a value that is not a record id resolves nothing at all", () => {
  const base = resolveRoutes(repo).resolved.length;
  for (const junk of ["NOT-A-REAL-ID", "placeholder", "7", "", "undefined", "../admin/secret"]) {
    const { resolved, assumed, unresolved } = resolveRoutes(repo, { fixtures: { goatId: junk } });
    assert.equal(resolved.length, base, `"${junk}" must not resolve a route`);
    assert.equal(assumed.length, 0, `"${junk}" must not even be assumed`);
    const passport = unresolved.find((r) => r.name === "goat-passport");
    assert.ok(passport, `"${junk}" leaves the goat passport a gap`);
    assert.ok(passport.gaps[0].why.length > 40, "with a sentence saying what was wrong with it");
  }
});

test("fixtureIdProblem names what is wrong, and passes a real id", () => {
  assert.equal(fixtureIdProblem(UUID(1)), null);
  assert.match(fixtureIdProblem(undefined), /was not given one/);
  assert.match(fixtureIdProblem("placeholder"), /stand-in rather than a real one/);
  assert.match(fixtureIdProblem("7"), /not shaped like anything this farm has a record of/);
  assert.equal(fixtureIdProblem("/vaccination/execution/sheds/abc", { isPath: true }), null);
  assert.match(fixtureIdProblem("/vaccination/execution/sheds/placeholder", { isPath: true }), /stand-in page nobody opens/);
});

test("the vaccination shed page is a gap, not a shed called placeholder", () => {
  // It was classified a clock hole because the source ships a literal fallback.
  // It was never a gap because it pointed at a page nobody opens, and it was
  // filmed and judged clean every run.
  const { resolved, unresolved } = resolveRoutes(repo);
  assert.ok(!resolved.some((r) => r.name === "vaccination-shed-execution-detail"),
    "a fallback to a page that does not exist is not a resolved route");
  const shed = unresolved.find((r) => r.name === "vaccination-shed-execution-detail");
  assert.ok(shed, "it is a named gap");
  assert.match(shed.gaps[0].why, /real vaccination shed execution page/);
  assert.equal(shed.gaps[0].needs, "vaccinationShedPath");
});

test("the sweep window is an India business date, never a UTC instant", () => {
  // Between 00:00 and 05:30 IST, toISOString() returns YESTERDAY, so an
  // early-morning sweep asked every analytics route for the wrong window.
  const earlyMorningIST = new Date("2026-09-24T01:00:00+05:30");
  assert.equal(farmDate(earlyMorningIST), "2026-09-24", "01:00 IST is still the 24th on the farm");
  assert.equal(earlyMorningIST.toISOString().slice(0, 10), "2026-09-23", "which UTC disagrees with");
  const { from, to } = windowDates(earlyMorningIST);
  assert.equal(to, "2026-09-24");
  assert.equal(from, "2026-08-12", "43 farm days back");
  // And it holds across the whole IST day, not just at noon.
  for (const hour of ["00:05", "05:29", "12:00", "23:55"]) {
    assert.equal(windowDates(new Date(`2026-09-24T${hour}:00+05:30`)).to, "2026-09-24", `at ${hour} IST`);
  }
});

test("a hole nobody taught the resolver about is named, not silently dropped", () => {
  const table = { ...ROUTE_HOLES };
  // Simulate the day a fourteenth expression lands: by deleting one we know.
  const dates = windowDates(new Date("2026-09-23T12:00:00+05:30"));
  assert.equal(dates.to, "2026-09-23");
  assert.equal(dates.from, "2026-08-11", "the 43-day window the smoke script computes");
  assert.ok(Object.keys(table).length === 13, `the route table has 13 kinds of hole, the resolver knows ${Object.keys(table).length}`);
});

test("a route table that is read but yields nothing is a failure, not an empty sweep", () => {
  // A file that opens and says nothing would make every downstream fraction
  // read "0 of 0 pages judged" — a clean-looking receipt counted from nothing.
  // The missing-file case was already covered; this is the present-but-silent one.
  assert.throws(() => smokeRoutes(repo, "apps/admin-web/scripts/lib/pen-label-vocabulary.json"),
    /no route could be found in it/);
});
