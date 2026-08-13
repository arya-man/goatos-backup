import { test } from "node:test";
import assert from "node:assert";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// Pins the fix for the STG incident (2026-08-12): the CEO's oversight filters (module chips,
// capture-date range picker) on /verify rendered for every role that can open the page, including
// RoleVerifier, because /verify is a single role-agnostic component. See
// docs/decisions/role-scoped-ui-is-capability-gated.md.
//
// These are SOURCE-SHAPE assertions, not a rendered-DOM test: the renderer must gate the
// oversight-only chrome on the backend page contract's "oversight_filters" control -- never on a
// hardcoded literal, a role string, or a data shape a hand-crafted payload could satisfy.
const source = readFileSync(
  fileURLToPath(new URL("./verification-review-page.tsx", import.meta.url)),
  "utf8",
);

test("the oversight filters flag is read from the page contract's oversight_filters control", () => {
  assert.match(
    source,
    /const oversightFiltersEnabled = controlEnabled\(pageContract, "oversight_filters", false\)/,
    "oversightFiltersEnabled must come from controlEnabled(pageContract, \"oversight_filters\", ...), not be hardcoded or role-derived",
  );
});

// Two independent gates guard this row and BOTH must survive. `module_filter` is the backend's
// per-principal offer (it is withdrawn from the verifier, whose sidebar already carries one leaf
// per evidence module), and `oversight_filters` is the leadership capability that introduced the
// row's cross-module reach. Asserting only on the pair as a whole would let a later edit drop
// either one silently, so the test names each flag.
test("the module-chip row is gated on oversightFiltersEnabled", () => {
  const chipRow = source.match(/\{[^\n]*modules\.length > 1 \? \(/);
  assert.ok(chipRow, "expected a module chip row conditioned on modules.length > 1");
  assert.match(
    chipRow[0],
    /oversightFiltersEnabled/,
    "the module chip row must require oversightFiltersEnabled in addition to having more than one module option",
  );
  assert.match(
    chipRow[0],
    /moduleFilterOffered/,
    "the module chip row must also honour the backend's module_filter offer, which is withdrawn for the verifier",
  );
});

test("the capture-date range picker is gated on oversightFiltersEnabled", () => {
  const filterRow = source.match(/<div className="vr-frow">([\s\S]*?)\{sheds\.length \? \(/);
  assert.ok(filterRow, "expected the vr-frow filter row to precede the shed filter block");
  assert.match(
    filterRow[1],
    /\{oversightFiltersEnabled \? \(\s*<ActionsDateFilter/,
    "ActionsDateFilter must only render when oversightFiltersEnabled is true",
  );
});

// The verifier's original working-queue filters predate the oversight rollout (status chips:
// commit fe06be1ed, the screen's first commit; shed filter: commit 89b16c0fa, the same commit
// that introduced this screen). They must stay UNGATED so a verifier's queue is unaffected.
test("status chips and the shed filter are not gated behind oversightFiltersEnabled", () => {
  // The oversight analytics section (gated on its own "oversight_analytics" control) may render
  // between the status chip block and the results table heading -- it is optional additive
  // chrome, not part of this filter row, so the regex tolerates it sitting in between.
  const statusBlock = source.match(
    /\{statuses\.length \? \(([\s\S]*?)\) : null\}\s*\n[\s\S]{0,600}?<div className="vr-secthd">/,
  );
  assert.ok(statusBlock, "expected the status chip block ahead of the results table heading");
  assert.ok(
    !/oversightFiltersEnabled/.test(statusBlock[1]),
    "status chips predate the oversight rollout and must render for every role, including RoleVerifier",
  );

  const shedFormMatch = source.match(/id="verification-shed"[\s\S]{0,400}/);
  assert.ok(shedFormMatch, "expected the shed <select> in the filter row");
  assert.ok(
    !/oversightFiltersEnabled/.test(shedFormMatch[0]),
    "the shed filter predates the oversight rollout and must render for every role, including RoleVerifier",
  );
});

// No hardcoded filter chip labels: every visible string this screen shows for the oversight
// filters must be backend copy (pageContract), never a literal the frontend invented -- so a
// hand-edited role cannot resurrect the filters by literal text, and a copy change lands from one
// place (the contract), not two.
test("no hardcoded oversight filter chip labels: module/date/shed copy is backend-owned", () => {
  for (const literal of [
    "All modules",
    "To verify",
    "Accepted",
    "Rejected",
  ]) {
    assert.ok(
      !source.includes(`"${literal}"`) && !source.includes(`'${literal}'`) && !source.includes(`>${literal}<`),
      `filter chip label "${literal}" must come from the page contract via copy(pageContract, ...), not be hardcoded`,
    );
  }
});

// The analytics SECTION is gated on its own contract control, not on oversightFiltersEnabled and
// not on a role string -- the same capability (permissions.VerificationOversee) that gates the
// backend endpoint it reads, so a verifier's page never renders it AND never calls it. The backend
// half is pinned by TestVerifyPageOversightAnalyticsControlIsCapabilityGated (adminui/app) and by
// the VerificationOversee entry for this route in permissions/routes.go.
test("the oversight analytics section is gated on the oversight_analytics control", () => {
  assert.match(
    source,
    /const oversightAnalyticsEnabled = controlEnabled\(pageContract, "oversight_analytics", false\)/,
    'oversightAnalyticsEnabled must come from controlEnabled(pageContract, "oversight_analytics", ...)',
  );
  // The panel wraps the analytics, so BOTH must sit inside the gate: the trigger button is as
  // oversight-only as the numbers behind it.
  assert.match(
    source,
    /\{oversightAnalyticsEnabled \? \(\s*<AnalyticsPanel/,
    "the analytics panel (button + drawer) must only render when oversightAnalyticsEnabled is true",
  );
  const gatedBlock = source.match(/\{oversightAnalyticsEnabled \? \([\s\S]*?\) : null\}/);
  assert.ok(gatedBlock && gatedBlock[0].includes("<OversightAnalytics"), "the analytics data must render inside the gated panel");
});

// Module rows name a module in the operator's words. `module` on an analytics row is the SOURCE
// module code stored on the item ("feed"); the queue's own module vocabulary is keyed by the
// NAVIGATION module ("feed_direction"), so a renderer joining on the code alone misses and prints
// the raw config token -- which the copy firewall bans on a leadership screen. The backend-owned
// module_label must therefore be preferred over both the nav map and the raw code.
test("module backlog rows prefer the backend-owned module_label", () => {
  const analytics = readFileSync(
    fileURLToPath(new URL("./oversight-analytics.tsx", import.meta.url)),
    "utf8",
  );
  assert.match(
    analytics,
    /moduleLabel\?\.trim\(\) \|\| moduleLabels\?\.get\(module\) \|\| module/,
    "label() must resolve backend module_label first, then the nav-vocabulary fallback, then the raw code",
  );
  for (const call of ["label(row.module, row.module_label)", "label(largest.module, largest.module_label)"]) {
    assert.ok(analytics.includes(call), `${call} must pass the row's backend-owned module_label`);
  }
});

// A backlog row links to the queue filtered to THAT module, and the link is built from the backend's
// nav_module key -- never from `module`, the source code the rows are grouped by ("feed"), which the
// queue's nav_module filter does not accept. Getting this wrong is silent: the queue would render
// unfiltered and look like a working link.
test("module backlog rows link to the queue by the backend nav_module key", () => {
  const analytics = readFileSync(
    fileURLToPath(new URL("./oversight-analytics.tsx", import.meta.url)),
    "utf8",
  );
  assert.match(
    analytics,
    /const href = row\.nav_module \? moduleHrefs\?\.get\(row\.nav_module\) : undefined/,
    "the href must be keyed by row.nav_module, not row.module",
  );
  assert.ok(
    !/moduleHrefs\?\.get\(row\.module\)/.test(analytics),
    "row.module is the source module code and is not a queue filter value",
  );
  // A module the registry cannot map must stay a plain card rather than link somewhere that quietly
  // drops the filter.
  assert.match(analytics, /if \(!href\) \{\s*return \(\s*<div key=\{row\.module\} className="vr-omod">/);
  // The page builds those hrefs with the same reset the module chips use: a keyset cursor from the
  // previous filter points into a different sequence.
  assert.match(
    source,
    /moduleHrefs=\{[\s\S]*?hrefWith\(sp, \{ nav_module: option\.key, category: null, \.\.\.RESET_ON_FILTER \}\)/,
    "module hrefs must clear category and RESET_ON_FILTER, exactly as the module chip row does",
  );
});

// The four age buckets partition videos_waiting. Re-deriving the total from anything else (say the
// module backlog rows) would let the legend contradict the KPI above it.
test("the backlog age shape totals the same rows the buckets partition", () => {
  const analytics = readFileSync(
    fileURLToPath(new URL("./oversight-analytics.tsx", import.meta.url)),
    "utf8",
  );
  assert.match(
    analytics,
    /const ageTotal =\s*ageBuckets\.up_to_1_day \+ ageBuckets\.one_to_three_days \+ ageBuckets\.three_to_seven_days \+ ageBuckets\.over_seven_days/,
    "ageTotal must be the sum of the four backend buckets",
  );
});

// Trajectory is arrivals minus verdicts over the SAME zero-filled 14-day series the chart draws. Two
// differently-scoped windows subtracted from each other would produce a number that looks precise
// and means nothing.
test("backlog trajectory is computed over the same series the chart renders", () => {
  const analytics = readFileSync(
    fileURLToPath(new URL("./oversight-analytics.tsx", import.meta.url)),
    "utf8",
  );
  assert.match(analytics, /const arrived14d = dailyVolume\.reduce\(\(total, day\) => total \+ day\.arrived, 0\)/);
  assert.match(analytics, /const verdicts14d = dailyVolume\.reduce\(\(total, day\) => total \+ day\.verdicts, 0\)/);
  assert.match(analytics, /const netChange = arrived14d - verdicts14d/);
});
