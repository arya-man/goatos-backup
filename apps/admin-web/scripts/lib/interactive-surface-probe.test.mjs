// Everything the probe decides BEFORE it opens a browser, tested without one.
import test from "node:test";
import assert from "node:assert/strict";
import { buildProbePlan, planSummary, targetRefusal } from "./interactive-surface-probe.mjs";
import { deriveMeasured, routeOfPageFile, stableReading, RECEIPT_VERSION } from "./interactive-surfaces.mjs";

test("every deployed host is refused, by name and by not being local", () => {
  for (const host of [
    "https://dashboard.mesha.sg/vaccination",
    "https://api.goatos.mesha.sg/app/bootstrap",
    "https://stg-api.dashboard.mesha.sg/",
    "https://goatos-api-stg-abc.a.run.app/",
    // The allow-list half: a host nobody thought to name is still refused.
    "http://some-new-staging-host.example.com:3300/",
    "http://192.168.1.50:3300/",
  ]) {
    assert.ok(targetRefusal(host), `${host} was NOT refused`);
  }
});

test("a local stack is allowed, and nonsense is refused rather than guessed at", () => {
  assert.equal(targetRefusal("http://127.0.0.1:3300"), null);
  assert.equal(targetRefusal("http://localhost:3300/vaccination"), null);
  assert.match(targetRefusal("file:///etc/passwd"), /speaks http to a local stack only/);
  assert.match(targetRefusal("not a url"), /not a URL this probe can check/);
  assert.match(targetRefusal(""), /not a URL this probe can check/);
});

const ENTRIES = [
  { key: "b", kind: "modal", where: "f.tsx:2", routes: ["/people", "/verify"] },
  { key: "a", kind: "edit-form", where: "f.tsx:1", routes: ["/people"] },
];

test("the plan is grouped by route, then viewport, and covers both widths", () => {
  const plan = buildProbePlan(ENTRIES);
  assert.deepEqual(
    plan.map((s) => `${s.route}@${s.viewport}`),
    ["/people@1440", "/people@390", "/verify@1440", "/verify@390"],
  );
  assert.deepEqual(plan[0].surfaces.map((s) => s.key), ["a", "b"], "surfaces are ordered so a run is repeatable");
  assert.deepEqual(planSummary(plan), { routes: 2, pageLoads: 4, surfaceOpenings: 12, pageLoadsWithRepeats: 8 });
});

test("a route group at the very end is not a route called /(admin)", () => {
  assert.equal(routeOfPageFile("app/(admin)/layout.tsx"), "/");
  assert.equal(routeOfPageFile("app/(admin)/people/loading.tsx"), "/people");
  assert.equal(routeOfPageFile("app/(admin)/weighing/sops/page.tsx"), "/weighing/sops");
});

test("a single reading is refused: it cannot tell a stable label from a timestamp", () => {
  assert.throws(() => buildProbePlan(ENTRIES, { repeats: 1 }), /cannot tell a stable label from a timestamp/);
  assert.equal(stableReading([["Close"]]).stable, false);
  assert.equal(stableReading([["Close"], ["Close"]]).stable, true);
  const varying = stableReading([["Updated 10:04"], ["Updated 10:05"]]);
  assert.equal(varying.stable, false);
  assert.match(varying.reason, /something the page varies, not something it owes/);
});

// ---- the seam: a measured value is COPIED from a receipt, never typed by an author
const RECEIPT = {
  version: RECEIPT_VERSION,
  runId: "probe-1",
  principal: "verifier",
  // The agreed contract revision: the backend build sha, never a placeholder.
  contractRevision: "9f3c1ab",
  observations: [{ id: "o1", readings: [["Close", "Approve"], ["Close", "Approve"]] }],
};
const read = () => RECEIPT;

test("a measured value is read out of the receipt, and mismatched provenance is refused", () => {
  assert.deepEqual(
    deriveMeasured({ receipt: "r.json", runId: "probe-1", observation: "o1" }, read, "verifier").value,
    ["Close", "Approve"],
  );
  assert.match(deriveMeasured({ receipt: "r.json", runId: "probe-2", observation: "o1" }, read, "verifier").error, /holds run probe-1/);
  assert.match(deriveMeasured({ receipt: "r.json", runId: "probe-1", observation: "nope" }, read, "verifier").error, /no observation/);
  assert.match(deriveMeasured({ receipt: "r.json", runId: "probe-1", observation: "o1" }, () => null, "verifier").error, /not a receipt this gate can read/);
});

test("a reading taken as one principal cannot be claimed for another", () => {
  const graded = deriveMeasured({ receipt: "r.json", runId: "probe-1", observation: "o1" }, read, "park head");
  assert.match(graded.error, /those are different screens/);
});

test("a receipt with no real build identity is refused before anything is compared", () => {
  const placeholder = { ...RECEIPT, contractRevision: "unknown" };
  assert.match(
    deriveMeasured({ receipt: "r.json", runId: "probe-1", observation: "o1" }, () => placeholder, "verifier").error,
    /a placeholder makes two different builds look like one/,
  );
});

test("an unstable observation cannot become an expectation", () => {
  const wobbly = { ...RECEIPT, observations: [{ id: "o1", readings: [["A"], ["B"]] }] };
  assert.match(
    deriveMeasured({ receipt: "r.json", runId: "probe-1", observation: "o1" }, () => wobbly, "verifier").error,
    /something the page varies, not something it owes/,
  );
  // Direction of failure: two absences agree, and agreeing is how a value gets promoted.
  const empty = { ...RECEIPT, observations: [{ id: "o1", readings: [[], []] }] };
  assert.match(
    deriveMeasured({ receipt: "r.json", runId: "probe-1", observation: "o1" }, () => empty, "verifier").error,
    /"nothing" is not an expectation/,
  );
});
