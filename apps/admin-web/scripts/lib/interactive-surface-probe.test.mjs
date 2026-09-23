// Everything the probe decides BEFORE it opens a browser, tested without one.
import test from "node:test";
import assert from "node:assert/strict";
import { buildProbePlan, describeLock, fingerprintOf, identityOf, identityRefusal, navigationRefusal, planSummary, principalRefusal, targetRefusal } from "./interactive-surface-probe.mjs";
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
    /nothing is never an answer here/,
  );
});

// ---------------------------------------------- the third missing input: the route
test("a route that does not exist is refused, loudly", () => {
  assert.match(navigationRefusal("/people", "http://127.0.0.1:3300/people", 404), /answered 404/);
  assert.match(navigationRefusal("/people", "http://127.0.0.1:3300/people", 500), /answered 500/);
});

test("a route that REDIRECTS somewhere real is refused — the quiet case", () => {
  // §7: a stale token made every route render a sign-in page beautifully. Two readings of that
  // page AGREE, and agreeing is how a value gets promoted — so without this the sign-in page's
  // controls become the thing /people owes, and every later run accuses the real /people.
  const refusal = navigationRefusal("/people", "http://127.0.0.1:3300/login?next=%2Fpeople", 200);
  assert.match(refusal, /asked for \/people and ended on \/login/);
  assert.match(refusal, /must never be recorded as that route's/);
  // The same shape within the app: a route that quietly forwards to its first tab.
  assert.ok(navigationRefusal("/weighing", "http://127.0.0.1:3300/weighing/weights", 200));
});

test("the page actually asked for is accepted, trailing slash and query aside", () => {
  assert.equal(navigationRefusal("/people", "http://127.0.0.1:3300/people", 200), null);
  assert.equal(navigationRefusal("/people", "http://127.0.0.1:3300/people/", 200), null);
  assert.equal(navigationRefusal("/people", "http://127.0.0.1:3300/people?tab=roster", 200), null);
  assert.equal(navigationRefusal("/", "http://127.0.0.1:3300/", 200), null);
});

test("an unknown status is refused rather than assumed to be fine", () => {
  assert.match(navigationRefusal("/people", "http://127.0.0.1:3300/people", null), /did not report a status/);
  assert.match(navigationRefusal("/people", "not a url", 200), /which is not a URL/);
});

test("an observation the probe could not take reads as not-checked, never as a value", () => {
  const notReached = {
    ...RECEIPT,
    observations: [{ id: "o1", notReached: "asked for /people and ended on /login", readings: [] }],
  };
  const graded = deriveMeasured({ receipt: "r.json", runId: "probe-1", observation: "o1" }, () => notReached, "verifier");
  assert.match(graded.error, /was not taken/);
  assert.doesNotMatch(graded.error, /nothing is never an answer here/, "the emptiness was reported instead of the reason for it");
});

// ---------------------------------------------- the probe's own inputs, corrupted not just absent
test("every corruption of the base URL that is not really loopback is refused", () => {
  for (const [name, url] of [
    ["absent", ""],
    ["userinfo smuggling", ["http://127.0.0.1:3300", "dashboard.mesha.sg/"].join("@")],
    ["lookalike host", "http://127.0.0.1.dashboard.mesha.sg:3300/"],
    ["subdomain of local", "http://local.127.0.0.1/"],
    ["loopback-mapped public name", "http://localtest.me:3300/"],
    ["decimal-encoded PRIVATE address", "http://3232235777:3300/"],
    ["data url", "data:text/html,<h1>hi"],
    ["no scheme", "127.0.0.1:3300"],
  ]) {
    assert.ok(targetRefusal(url), `${name} (${url}) was ALLOWED`);
  }
});

test("forms that really are loopback are allowed, however they are written", () => {
  // Each of these normalises to 127.0.0.1 or localhost; checked against URL's own parsing rather
  // than assumed, because "it looks like an IP" is not the same as "it is loopback".
  for (const url of [
    "http://127.0.0.1:3300",
    "http://LOCALHOST:3300/",
    "http://[::1]:3300/",
    "http://2130706433:3300/",
    "http://0177.0.0.1:3300/",
    "  http://127.0.0.1:3300  ",
  ]) {
    assert.equal(targetRefusal(url), null, `${url} was refused`);
  }
});

test("a principal that names nobody is refused, and the unverifiable case is stated not hidden", () => {
  for (const who of ["", "  ", "unknown", "dev", "TEST", "me", "x", "n/a"]) {
    assert.ok(principalRefusal(who), `${JSON.stringify(who)} was accepted as a principal`);
  }
  assert.equal(principalRefusal("pc_director"), null);
  // The honest limit: a real-looking but WRONG label passes here and is not caught later either.
  assert.equal(principalRefusal("park_head"), null, "a plausible label cannot be verified from here");
});

test("a leftover lock refuses either way, and says which kind it is", () => {
  const live = describeLock("pid=4242 started=2026-09-23T10:00:00Z", () => true);
  assert.match(live, /still running/);
  const dead = describeLock("pid=4242 started=2026-09-23T10:00:00Z", () => false);
  assert.match(dead, /leftover from a killed run/);
  assert.match(dead, /stopping a parent does not stop them/);
  assert.match(describeLock("", () => false), /does not say which process made it/);
});

// ---------------------------------------------- the drift WINDOW during a run
test("the fingerprint moves when any watched file does, and only then", () => {
  const base = [
    { path: "features/a.tsx", mtimeMs: 1000, size: 10 },
    { path: "features/b.tsx", mtimeMs: 2000, size: 20 },
  ];
  assert.equal(fingerprintOf(base), fingerprintOf([...base].reverse()), "order must not matter");
  assert.notEqual(fingerprintOf(base), fingerprintOf([{ ...base[0], mtimeMs: 1001 }, base[1]]), "a touched file");
  assert.notEqual(fingerprintOf(base), fingerprintOf([{ ...base[0], size: 11 }, base[1]]), "a resized file");
  assert.notEqual(fingerprintOf(base), fingerprintOf(base.slice(1)), "a deleted file");
  assert.notEqual(fingerprintOf(base), fingerprintOf([...base, { path: "features/c.tsx", mtimeMs: 1, size: 1 }]), "a new file");
});

test("a reading taken after the source moved mid-run is not a reading of the same thing", () => {
  const mixed = {
    ...RECEIPT,
    startedFingerprint: "aaaaaaaa",
    observations: [{ id: "o1", sourceFingerprint: "bbbbbbbb", readings: [["Close"], ["Close"]] }],
  };
  const graded = deriveMeasured({ receipt: "r.json", runId: "probe-1", observation: "o1" }, () => mixed, "verifier");
  // Two readings that AGREE, from either side of an edit. Agreement is exactly what would have
  // promoted them, which is why this cannot be left to the stability check.
  assert.match(graded.error, /read after the source changed mid-run/);

  const same = { ...mixed, observations: [{ id: "o1", sourceFingerprint: "aaaaaaaa", readings: [["Close"], ["Close"]] }] };
  assert.deepEqual(deriveMeasured({ receipt: "r.json", runId: "probe-1", observation: "o1" }, () => same, "verifier").value, ["Close"]);
});

// ---------------------------------------------- the wrong-but-plausible principal
test("identity comes from the grants the API reports, not from the label someone typed", () => {
  const me = {
    actor_id: "a1",
    grants: [
      { role: "pc_director", scope_type: "tenant", scope_id: "t1", status: "active" },
      { role: "operator", scope_type: "park", scope_id: "p2", status: "revoked" },
    ],
  };
  const id = identityOf(me);
  assert.equal(id.actorId, "a1");
  assert.deepEqual(id.grants, ["pc_director@tenant:t1"], "a revoked grant is not a grant");
  // Order must not matter: the same person read twice reduces to the same string.
  const shuffled = identityOf({ ...me, grants: [...me.grants].reverse() });
  assert.deepEqual(shuffled.grants, id.grants);
});

test("a caller with no identity, or no grants, is refused rather than recorded", () => {
  assert.match(identityOf({}).error, /no actor_id/);
  assert.match(identityOf({ actor_id: "a1", grants: [] }).error, /no active grants/);
  assert.match(identityOf({ actor_id: "a1", grants: [{ role: "x", status: "revoked" }] }).error, /no active grants/);
});

test("two runs under different grants are different screens, whatever they called themselves", () => {
  assert.equal(identityRefusal(["pc_director@tenant:t1"], ["pc_director@tenant:t1"]), null);
  assert.equal(identityRefusal(["a@t:1", "b@p:2"], ["b@p:2", "a@t:1"]), null, "order must not matter");
  assert.match(identityRefusal(["park_head@park:p1"], ["park_head@park:p2"]), /different grants/);
  // The hole this closes: both runs claimed the same label and saw different screens.
  assert.match(identityRefusal(["operator@park:p1"], ["pc_director@tenant:t1"]), /whatever the two runs called themselves/);
  assert.ok(identityRefusal([], ["a@t:1"]), "a run with no grants cannot be compared");
});
