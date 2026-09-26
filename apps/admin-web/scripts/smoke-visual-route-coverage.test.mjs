import { existsSync, readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const smokeSource = readFileSync(new URL("./smoke-visual-live.mjs", import.meta.url), "utf8");
// module-journeys.json is owned by vgoats/mesha-ops (dashboard-automation/tooling) and only
// exists here when that tooling is overlaid at tools/dashboard-automation (the OCI runner).
const journeyManifestUrl = new URL("../../../tools/dashboard-automation/module-journeys.json", import.meta.url);
const journeyManifest = existsSync(journeyManifestUrl) ? JSON.parse(readFileSync(journeyManifestUrl, "utf8")) : null;
const smokeRouteBlock = smokeSource.match(/function buildRoutes\(\{[^)]*\}\) \{[\s\S]*?const pagerMinimums = new Map/)?.[0] ?? "";
const routeEntries = Array.from(
  smokeRouteBlock.matchAll(/name:\s*"([^"]+)"[\s\S]{0,500}?path:\s*([`"])([^`"]+)/g),
  ([, name, quote, path]) => [name, quote === "`" ? path.replace(/\$\{[^}]+\}/g, "${dynamic}") : path],
);

test("visual smoke visits every visible visual-overhaul route", () => {
  const routes = new Map(routeEntries);
  const required = new Map([
    ["work-board", "/work-board?scope_mode=company"],
    ["work-board-populated", "/work-board?scope_mode=company&date=2026-08-10"],
    ["alerts", "/alerts?scope_mode=company"],
    ["alerts-populated", "/alerts?scope_mode=company&date=2026-09-10&park=00000000-0000-4000-8000-000000003001"],
    ["routines", "/routines?scope_mode=company"],
    ["procurement-source-entry", "/procurement/source-entry?scope_mode=company"],
    ["procurement-source-entry-health-pending", "/procurement/source-entry?scope_mode=company&status=health_pending"],
    ["procurement-source-entry-arrival-review", "/procurement/source-entry?scope_mode=company&status=arrival_review"],
    ["procurement-source-entry-accepted-intake", "/procurement/source-entry?scope_mode=company&status=accepted_intake"],
    ["procurement", "/procurement?scope_mode=company"],
    ["procurement-vendors", "/procurement/vendors?scope_mode=company"],
    ["procurement-feed-purchases", "/procurement/feed-purchases?scope_mode=company"],
    ["procurement-animal-purchases", "/procurement/animal-purchases?scope_mode=company"],
    ["procurement-sops", "/procurement/sops?scope_mode=company"],
    ["procurement-toxin-list", "/procurement/sops?scope_mode=company&compose=1&edit=${dynamic}&view=list"],
    ["procurement-toxin-flow", "/procurement/sops?scope_mode=company&compose=1&edit=${dynamic}&view=flow"],
    ["approvals", "/approvals?scope_mode=company"],
    ["approvals-approved", "/approvals?scope_mode=company&status=approved"],
    ["approvals-rejected", "/approvals?scope_mode=company&status=rejected"],
    ["verify", "/verify?scope_mode=company"],
    ["verify-all", "/verify?scope_mode=company&status=all"],
    ["verify-approved", "/verify?scope_mode=company&status=approved"],
    ["verify-rejected", "/verify?scope_mode=company&status=rejected"],
    ["verify-toxin", "/verify?scope_mode=company&toxin=1"],
    ["vaccination", "/vaccination?scope_mode=company"],
    ["vaccination-execution", "/vaccination?scope_mode=company#execution"],
    ["vaccination-sheds-status-action", "/vaccination?scope_mode=company&sheds_status=needs_review#execution"],
    ["vaccination-sheds-capacity-action", "/vaccination?scope_mode=company&sheds_capacity=capacity_breach#execution"],
    ["vaccination-live-tracker", "/vaccination/live-tracker?scope_mode=company"],
    ["vaccination-care-coverage", "/vaccination/care-coverage?scope_mode=company"],
    ["vaccination-plan", "/vaccination/plan?scope_mode=company"],
    ["vaccination-plan-edit", "/vaccination/plan/edit?scope_mode=company&version=${dynamic}"],
    ["sales-sold", "/sales/sold?scope_mode=company"],
    ["sales-farm-value", "/sales/farm-value?scope_mode=company"],
    ["sales-loads", "/sales/loads?scope_mode=company"],
    ["sales-loads-farm-born", "/sales/loads?scope_mode=company&view=farm_born"],
    ["sales-market-analytics", "/sales/market-analytics?scope_mode=company"],
    ["sales-buyer-analytics", "/sales/buyer-analytics?scope_mode=company"],
    ["sales-farm-born", "/sales/farm-born?scope_mode=company"],
    ["sales-config", "/sales/config?scope_mode=company"],
    ["sales-sops", "/sales/sops?scope_mode=company"],
    ["sales-vendors", "/sales/vendors?scope_mode=company"],
    ["sales-sops", "/sales/sops?scope_mode=company"],
    ["feed-config", "/feed/config?scope_mode=company"],
    ["feed-analytics", "/feed/analytics?scope_mode=company"],
    ["feed-analytics-items", "/feed/analytics?scope_mode=company&tab=items"],
    ["feed-analytics-peranimal", "/feed/analytics?scope_mode=company&tab=peranimal"],
    ["feed-analytics-experiment", "/feed/analytics?scope_mode=company&tab=experiment"],
    ["feed-analytics-execution", "/feed/analytics?scope_mode=company&tab=execution"],
    ["feed-analytics-stock-only", "/feed/analytics?scope_mode=company&stock_only=1&tab=items"],
    ["feed-sops", "/feed/sops?scope_mode=company"],
    ["feed-direction", "/feed/direction?scope_mode=company"],
    ["feed-packing", "/feed/packing?scope_mode=company"],
    ["weighing-analytics", "/weighing/analytics?scope_mode=company&tab=general&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-breed", "/weighing/analytics?scope_mode=company&tab=breed&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-breed-wide", "/weighing/analytics?scope_mode=company&tab=breed&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-birth", "/weighing/analytics?scope_mode=company&tab=birth&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-shed", "/weighing/analytics?scope_mode=company&tab=shed&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-weight", "/weighing/analytics?scope_mode=company&tab=weight&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-weight-not-shown", "/weighing/analytics?scope_mode=company&tab=weight&fb_view=unmatched&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-weight-band-filter", "/weighing/analytics?scope_mode=company&tab=weight&fb_band=25_30&fb_animals=all&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-time", "/weighing/analytics?scope_mode=company&tab=time&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-load", "/weighing/analytics?scope_mode=company&tab=load&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-fcr", "/weighing/analytics?scope_mode=company&tab=fcr&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-sops", "/weighing/sops?scope_mode=company"],
    ["pc-care-sops", "/pc-care/sops?scope_mode=company"],
    ["weighing-weights", "/weighing/weights?scope_mode=company&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["counts-analytics", "/counts/analytics?scope_mode=company"],
    ["counts-mortality", "/counts/mortality?scope_mode=company"],
    ["counts-breakdown", "/counts/breakdown?scope_mode=company"],
    ["counts-sops", "/counts/sops?scope_mode=company"],
    ["counts-sops-builder", "/counts/sops?compose=1&scope_mode=company"],
    ["counts-herd", "/counts/herd?scope_mode=company"],
    ["counts-milk-preparation", "/counts/milk-preparation?scope_mode=company"],
    ["milk-sops", "/milk/sops?scope_mode=company"],
    ["configuration-items", "/configuration/items?scope_mode=company"],
    ["configuration-work-instructions", "/configuration/work-instructions?scope_mode=company"],
    ["herd-signals", "/herd-signals?scope_mode=company"],
    ["herd-signals-animals", "/herd-signals?scope_mode=company&hs_tab=animals"],
    ["herd-signals-mapping", "/herd-signals?scope_mode=company&hs_tab=mapping"],
    ["herd-signals-alerts", "/herd-signals?scope_mode=company&hs_tab=alerts"],
    ["herd-signals-gateways", "/herd-signals?scope_mode=company&hs_tab=gateways"],
    ["herd-signals-insights", "/herd-signals?scope_mode=company&hs_tab=insights"],
    ["health-analytics", "/health/analytics?scope_mode=company"],
    ["health-analytics-diseases", "/health/analytics?scope_mode=company&tab=diseases"],
    ["health-analytics-mortality", "/health/analytics?scope_mode=company&tab=mortality"],
    ["health-analytics-treatment", "/health/analytics?scope_mode=company&tab=treatment"],
    ["health-analytics-engine", "/health/analytics?scope_mode=company&tab=engine"],
    ["health-config", "/health/config?scope_mode=company"],
    ["configuration-items", "/configuration/items?scope_mode=company"],
    ["configuration-work-instructions", "/configuration/work-instructions?scope_mode=company"],
    ["operations-audit", "/operations/audit?scope_mode=company"],
    ["operations-audit-awaiting", "/operations/audit?scope_mode=company&status=verification_pending"],
    ["operations-audit-rejected", "/operations/audit?scope_mode=company&result=rejected"],
    ["operations-audit-proof-gaps", "/operations/audit?scope_mode=company&proof_gaps=true"],
    ["operations-dlq", "/operations/dlq?scope_mode=company"],
    ["operations-dlq-failed", "/operations/dlq?scope_mode=company&status=failed"],
    ["operations-dlq-discarded", "/operations/dlq?scope_mode=company&status=discarded"],
    ["people", "/people?scope_mode=company"],
    ["people-vaccination", "/people?scope_mode=company&tab=vaccination&park=${dynamic}"],
    ["people-clock", "/people?scope_mode=company&tab=clock"],
    ["people-notifications", "/people?scope_mode=company&tab=notifications"],
    ["routines", "/routines?scope_mode=company"],
    ["leave", "/leave?scope_mode=company"],
    ["leave-approved", "/leave?scope_mode=company&status=approved"],
    ["leave-rejected", "/leave?scope_mode=company&status=rejected"],
    ["leave-withdrawn", "/leave?scope_mode=company&status=withdrawn"],
    ["routines-create-drawer", "/routines?scope_mode=company&edit=new"],
    ["tasks", "/tasks?scope_mode=company"],
  ]);

  for (const [routeName, path] of required) {
    assert.equal(routes.get(routeName), path, `${routeName} must stay in the live visual smoke sweep`);
  }
  for (const routeName of [
    "vaccination-schedule",
    "goat-passport",
    "procurement-load-detail",
    "vaccination-shed-execution-detail",
  ]) {
    assert.ok(routes.has(routeName), `${routeName} dynamic route must stay in the live visual smoke sweep`);
  }
});

test("dashboard automation has a module-wise read-only journey contract for every smoke route", {
  skip: journeyManifest ? false : "mesha-ops dashboard-automation tooling not overlaid",
}, () => {
  assert.ok(Array.isArray(journeyManifest.journeys), "journey manifest must expose journeys");
  assert.ok(journeyManifest.journeys.length >= 10, "journey manifest must stay module-wise, not one generic smoke bucket");
  const routeNames = new Set(routeEntries.map(([name]) => name));
  const assignedRoutes = new Set();
  for (const journey of journeyManifest.journeys) {
    const moduleId = journey.module ?? journey.id;
    const journeyRoutes = journey.routeNames ?? journey.routes;
    const assertions = journey.assertions ?? journey.assertText ?? journey.coverage;
    assert.ok(moduleId, "journey module is required");
    assert.ok(Array.isArray(journeyRoutes) && journeyRoutes.length > 0, `${moduleId} must name route coverage`);
    assert.ok(Array.isArray(assertions) && assertions.length > 0, `${moduleId} must document user-visible assertions`);
    assert.ok(
      Array.isArray(journey.safeClicks) && journey.safeClicks.length > 0,
      `${moduleId} must declare at least one read-only safe click target`,
    );
    for (const routeName of journeyRoutes) {
      assert.ok(routeNames.has(routeName), `${moduleId} references smoke route ${routeName}`);
      assignedRoutes.add(routeName);
    }
  }
  for (const routeName of routeNames) {
    assert.ok(assignedRoutes.has(routeName), `${routeName} must be assigned to a read-only journey module`);
  }
});

test("module journey safe clicks must be observed by the live smoke runner", () => {
  assert.match(smokeSource, /const observedModuleSafeClicks = new Set\(\);/);
  assert.match(smokeSource, /observedModuleSafeClicks\.add\(moduleSafeClickKey\(click\)\)/);
  assert.match(smokeSource, /assertRequiredModuleSafeClicksObserved\(\);/);
  assert.match(smokeSource, /click\.requireObserved === true/);
});

test("read-only smoke is opt-in so the shared live smoke keeps Action Center submit coverage", () => {
  assert.match(smokeSource, /const readOnlySmoke = process\.env\.GOATOS_SMOKE_READ_ONLY === "1";/);
  assert.match(smokeSource, /if \(!readOnlySmoke\) \{\s+await submitActionCenterVerification\(page, routeName\);/);
});

test("visual smoke keeps every live sidebar leaf covered on desktop and narrow/mobile", () => {
  assert.match(smokeSource, /label:\s*"laptop"[\s\S]*?width:\s*1440[\s\S]*?height:\s*1000/);
  assert.match(smokeSource, /label:\s*"mobile"[\s\S]*?width:\s*390[\s\S]*?height:\s*900/);

  for (const [routeName] of routeEntries) {
    const routeEntry = smokeRouteBlock.match(new RegExp(`name:\\s*"${routeName}"[\\s\\S]*?(?=\\n\\s*\\{|\\n\\s*\\];)`))?.[0] ?? "";
    assert.ok(routeEntry, `${routeName} must stay in the live visual smoke sweep`);
    assert.doesNotMatch(routeEntry, /viewports:\s*\[/, `${routeName} must run in both laptop and mobile visual sweeps`);
  }
});

test("weighing visual smoke pins explicit date windows on every tabbed picker surface", () => {
  for (const [routeName, path] of routeEntries) {
    if (!routeName.startsWith("weighing-analytics") && routeName !== "weighing-weights") continue;
    assert.match(path, /[?&]wt_from=\$\{dynamic\}(?:&|$)/, `${routeName} must carry an explicit wt_from window`);
    assert.match(path, /[?&]wt_to=\$\{dynamic\}(?:&|$)/, `${routeName} must carry an explicit wt_to window`);
  }
});

test("feed config smoke checks experiment pen dropdown identity without writing", () => {
  assert.match(smokeSource, /if \(routeName === "feed-config"\) \{\s+await assertFeedConfigPenDropdownContracts\(page, routeName\);/);
  // The pen chooser is a checkbox PANEL, not a <select>: several pens are enrolled in one act, so
  // the smoke reads `.exp-pen-row` rather than `#exp-new-pen option`. What this coverage test is
  // protecting is unchanged -- that the smoke still opens the real chooser and still rejects a
  // duplicate pen and a mis-composed operational location.
  assert.match(smokeSource, /\.exp-pen-row/);
  assert.match(smokeSource, /duplicate option/);
  assert.match(smokeSource, /bare numeric pen with dash/);
  assert.match(smokeSource, /doubles the partition name/);
  assert.match(smokeSource, /Castro\|Gandhi\|Ho Chi Minh\|Mandela\|Yashoda/);
  // An empty-candidate park is a legitimate pass, and the smoke must say so rather than failing a
  // park whose every pen is already on the experiment. Matched on the branch, not on the copy: that
  // sentence is backend-owned and this test is not the place that pins its wording.
  assert.match(smokeSource, /empty-candidate explanation/);
});

test("counts herd smoke proves top-bar park changes preserve complete page-local windows", () => {
  assert.match(smokeSource, /if \(routeName === "counts-herd"\) \{\s+await assertTopBarScopePreservesPageWindow\(page, routeName\);/);
  assert.match(smokeSource, /counts\/herd\?scope_mode=company&from=2026-09-14&to=2026-09-22&status=live&status=icu/);
  assert.match(smokeSource, /top-bar park link dropped a complete page-local from\/to window/);
  assert.match(smokeSource, /top-bar park link collapsed repeated page filters/);
  assert.match(smokeSource, /await clickTopBarParkHref\(page, parkHref, routeName\);/);
  assert.match(smokeSource, /top-bar park click dropped a complete page-local from\/to window/);
  assert.match(smokeSource, /top-bar park click collapsed repeated page filters/);
  assert.match(smokeSource, /top-bar park link preserved a corrupt half window/);
});

test("sales sold smoke proves the sold-weight band card from the live page", () => {
  assert.match(smokeSource, /if \(routeName === "sales-sold"\) \{/);
  for (const required of [
    "Sold animals by weight",
    "40 kg and above",
    "35 to 40 kg",
    "20 to 35 kg",
    "Below 20 kg",
    "animals sold",
  ]) {
    assert.ok(smokeSource.includes(required), `sales-sold smoke must check ${required}`);
  }
  assert.match(smokeSource, /weighed\|at load average\|estimated\|sold without a recorded weight/);
});

test("visual smoke fails on the visible admin error boundary", () => {
  const markerBlock = smokeSource.match(/const failureScreenMarkers = \[[\s\S]*?\];/)?.[0] ?? "";
  for (const marker of [
    "Something went wrong",
    "This screen failed to render",
    "backend_down",
    "Admin-web contract unavailable",
    "The board could not be loaded",
    "Weights could not be loaded",
  ]) {
    assert.ok(markerBlock.includes(JSON.stringify(marker)), `failureScreenMarkers must include ${marker}`);
  }
  assert.match(smokeSource, /const visibleText = await page\.locator\("body"\)\.innerText/);
  assert.match(smokeSource, /assertHealthyHTML\(route\.name, html, visibleText, bearerToken\)/);
  assert.match(smokeSource, /rendered visible failure marker/);
});

test("visual smoke keeps mobile WebView layout regression checks wired", () => {
  assert.match(smokeSource, /route_failed=\$\{viewport\.label\}:\$\{route\.name\}/, "every failing page must be reported, not just the first");
  assert.match(smokeSource, /await check\(\(\) => assertLayoutHealthy\(page, route\.name, viewport\.label\)\);/);
  assert.match(smokeSource, /await check\(\(\) => assertMobileWideTableGestures\(page, route\.name, viewport\.label, screenshotDir\)\);/);
  assert.match(smokeSource, /await check\(\(\) => assertA11y\(page, route\.name, viewport\.label\)\);/);
  assert.match(smokeSource, /await check\(\(\) => assertTruncationContracts\(page, route\.name, viewport\.label\)\);/);
  assert.doesNotMatch(smokeSource, /function isAllowed(ClippedControl|SmallTarget|MobileScrollProblem|A11yFinding)/, "smoke must not waive real UI failures");
  assert.match(smokeSource, /await check\(\(\) => assertCoreInteractions\(page, route\.name, viewport\.label\)\);/);
  for (const requiredCheck of [
    "has horizontal overflow",
    "has cards/panels cut at the viewport edge",
    "has clipped button/link text",
    "has interactive targets below 40px",
    "has overlapping interactive elements",
    "has wide tables that cannot be horizontally scrolled on mobile",
    "mobile wide table did not respond to horizontal drag",
    "mobile expected one mobile navigation menu button",
  ]) {
    assert.ok(smokeSource.includes(requiredCheck), `mobile/WebView smoke must keep check: ${requiredCheck}`);
  }
});

test("pager-required routes report a missing pager instead of failing the run", () => {
  const pagerBlock = smokeSource.match(/async function assertPaginationControls[\s\S]*?\n}\n\nasync function exerciseFirstPagerRoundTrip/)?.[0] ?? "";
  assert.match(pagerBlock, /const bodyText = \(await page\.locator\("body"\)\.innerText\(\)\.catch\(\(\) => ""\)\)\.replace/);
  assert.match(pagerBlock, /0 rows\|0 results\|Nothing\|No rows\|No data/);
  assert.match(pagerBlock, /pager_warning=\$\{routeName\}:\$\{viewportLabel\}:no-pager2-footer/);
});

test("visual smoke fails on regression patterns with an annotated screenshot", () => {
  assert.match(smokeSource, /import \{ assertRegressionPatterns \} from "\.\/lib\/regression-checks\.mjs";/);
  assert.match(smokeSource, /await check\(\(\) => assertRegressionPatterns\(page, \{ routeName: route\.name, viewportLabel: viewport\.label, screenshotDir, relativeToRepo \}\)\);/);
  assert.doesNotMatch(smokeSource, /assertReadableText/);
});

test("PR264 routes record route-specific product signals in browser evidence", () => {
  assert.match(smokeSource, /const routeSignals = await assertRouteLoadedSignal\(page, route\.name, visibleText\);/);
  assert.match(smokeSource, /route_signals: routeSignals/);
  assert.match(smokeSource, /routeName === "work-board"[\s\S]*lane_counts/);
  assert.match(smokeSource, /routeName === "weighing-weights"[\s\S]*has_losing_weight_table/);
  assert.match(smokeSource, /routeName === "weighing-analytics"[\s\S]*has_weighing_kpis/);
  assert.match(smokeSource, /routeName === "weighing-analytics-breed" \|\| routeName === "weighing-analytics-breed-wide"[\s\S]*has_breed_breakdown/);
  assert.match(smokeSource, /routeName === "weighing-analytics-time"[\s\S]*has_weekly_growth/);
  assert.match(smokeSource, /assertFocusedPenAdgWindowSemantics/);
  assert.match(smokeSource, /assertShedWeightsGainSpanSemantics/);
  assert.match(smokeSource, /14\/21\/28 day windows collapsed/);
  assert.match(smokeSource, /routeName === "weighing-analytics-load"[\s\S]*has_load_breakdown/);
});

test("historical Work Board proof requires actual rendered cards", () => {
  assert.ok(smokeSource.includes('page.locator(".card[data-filter-row]")'));
  assert.match(smokeSource, /routeName === "work-board-populated" && hasWorkCards <= 0/);
  assert.ok(!smokeSource.includes('[data-work-board-row]'));
});

test("a route whose fixture is unavailable is skipped, not fatal to the whole module", () => {
  // Tonight's lane lost an entire module to "backend smoke vaccination shed lookup failed: status 500".
  // One fixture must cost one route.
  assert.match(smokeSource, /planRouteSelection\(/, "route selection must go through the testable planner");
  assert.match(smokeSource, /routeSkippedLine\(skip\)/, "every dropped route must log route_skipped=<name>:<why>");
  assert.doesNotMatch(
    smokeSource,
    /throw new Error\(`GOATOS_SMOKE_ONLY_ROUTES selected route\(s\) not available/,
    "a partially-available selection must skip the missing routes instead of throwing",
  );
  assert.match(
    smokeSource,
    /const noRoutesLeft = noRoutesLeftError\(/,
    "a selection where nothing can run must still be fatal",
  );
});

test("a failed fixture lookup is reported as a finding instead of aborting before the browser opens", () => {
  assert.match(smokeSource, /async function resolveFixture\(/, "fixture lookups must go through the catching helper");
  assert.match(smokeSource, /fixtureFindings\.push\(/, "a backend error on a fixture lookup must be recorded as a finding");
  assert.match(smokeSource, /var routeFailures = \[\.\.\.fixtureFindings\]/, "fixture findings must be reported with the route failures");
  for (const fixture of ["calendar event", "procurement load", "goat", "toxin SOP", "vaccination shed", "workflow row"]) {
    assert.ok(smokeSource.includes(`resolveFixture("${fixture}"`), `${fixture} lookup must be wrapped by resolveFixture`);
  }
});

test("routes that need a fixture id are dropped when that id is missing", () => {
  for (const route of ["goat-passport", "procurement-toxin-list", "procurement-toxin-flow", "vaccination-shed-execution-detail"]) {
    assert.ok(
      smokeSource.includes(`route.name === "${route}"`) || smokeSource.includes(`"${route}"`),
      `${route} must be filtered out when its fixture id is unavailable`,
    );
  }
  assert.match(smokeSource, /if \(route\.name === "goat-passport"\) return Boolean\(goatId\);/);
  assert.match(smokeSource, /return Boolean\(toxinSopId\);/);
});

test("the sweep visits each route once and never a retired redirect", () => {
  const names = routeEntries.map(([name]) => name);
  const repeated = names.filter((name, i) => names.indexOf(name) !== i);
  assert.deepEqual(repeated, [], "a repeated route costs two page loads per viewport for one verdict");
  const retiredBlock = smokeSource.match(/const RETIRED_ROUTES = Object\.freeze\(\{([\s\S]*?)\}\);/)?.[1] ?? "";
  const retired = Array.from(retiredBlock.matchAll(/^\s*"?([a-z0-9-]+)"?:/gm), ([, name]) => name);
  assert.ok(retired.includes("actions") && retired.includes("verification"), "retired names stay askable");
  for (const name of retired) assert.ok(!names.includes(name), `${name} is retired and must not be visited`);
});
