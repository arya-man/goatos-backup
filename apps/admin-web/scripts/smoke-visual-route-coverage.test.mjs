import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const smokeSource = readFileSync(new URL("./smoke-visual-live.mjs", import.meta.url), "utf8");
const smokeRouteBlock = smokeSource.match(/function buildRoutes\(\{ toxinSopId, goatId, procurementLoadId, workflowRowId, calendarEventId, vaccinationShedPath \}\) \{[\s\S]*?const pagerMinimums = new Map/)?.[0] ?? "";
const routeEntries = Array.from(
  smokeRouteBlock.matchAll(/name:\s*"([^"]+)"[\s\S]{0,500}?path:\s*([`"])([^`"]+)/g),
  ([, name, quote, path]) => [name, quote === "`" ? path.replace(/\$\{[^}]+\}/g, "${dynamic}") : path],
);

test("visual smoke visits every live sidebar navigation leaf", () => {
  const routes = new Map(routeEntries);
  const required = new Map([
    ["control-tower", "/?scope_mode=company&lens=control-tower"],
    ["action-center", "/action-center?scope_mode=company"],
    ["action-center-verify", "/action-center?scope_mode=company&bucket=verify"],
    ["action-center-overdue", "/action-center?scope_mode=company&state=overdue"],
    ["action-center-due", "/action-center?scope_mode=company&state=due"],
    ["calendar", "/calendar?scope_mode=company&day=week"],
    ["calendar-month", "/calendar?scope_mode=company&view=month"],
    ["calendar-history", "/calendar?scope_mode=company&status=completed"],
    ["calendar-owner-pc", "/calendar?scope_mode=company&day=week&owner_key=pc"],
    ["protocol-adherence", "/protocol-adherence?scope_mode=company"],
    ["protocol-adherence-high", "/protocol-adherence?scope_mode=company&severity=high"],
    ["protocol-adherence-overdue", "/protocol-adherence?scope_mode=company&state=overdue"],
    ["work-board", "/work-board?scope_mode=company"],
    ["work-board-populated", "/work-board?scope_mode=company&date=2026-08-10"],
    ["alerts", "/alerts?scope_mode=company"],
    ["alerts-populated", "/alerts?scope_mode=company&date=2026-09-10&park=00000000-0000-4000-8000-000000003001"],
    ["workflows", "/workflows?scope_mode=company"],
    ["procurement", "/procurement?scope_mode=company"],
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
    ["sales", "/sales?scope_mode=company"],
    ["approvals", "/approvals?scope_mode=company"],
    ["approvals-approved", "/approvals?scope_mode=company&status=approved"],
    ["approvals-rejected", "/approvals?scope_mode=company&status=rejected"],
    ["verify", "/verify?scope_mode=company"],
    ["verify-all", "/verify?scope_mode=company&status=all"],
    ["verify-approved", "/verify?scope_mode=company&status=approved"],
    ["verify-rejected", "/verify?scope_mode=company&status=rejected"],
    ["verify-toxin", "/verify?scope_mode=company&toxin=1"],
    ["actions", "/actions?scope_mode=company"],
    ["verification", "/verification?scope_mode=company"],
    ["vaccination", "/vaccination?scope_mode=company"],
    ["vaccination-execution", "/vaccination?scope_mode=company#execution"],
    ["vaccination-sheds-status-action", "/vaccination?scope_mode=company&sheds_status=needs_review#execution"],
    ["vaccination-sheds-capacity-action", "/vaccination?scope_mode=company&sheds_capacity=capacity_breach#execution"],
    ["vaccination-live-tracker", "/vaccination/live-tracker?scope_mode=company"],
    ["vaccination-plan", "/vaccination/plan?scope_mode=company"],
    ["vaccination-plan-edit", "/vaccination/plan/edit?scope_mode=company"],
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
    ["weighing-analytics", "/weighing/analytics?scope_mode=company&tab=general"],
    ["weighing-analytics-breed", "/weighing/analytics?scope_mode=company&tab=breed"],
    ["weighing-analytics-breed-wide", "/weighing/analytics?scope_mode=company&tab=breed&wt_from=${dynamic}&wt_to=${dynamic}"],
    ["weighing-analytics-birth", "/weighing/analytics?scope_mode=company&tab=birth"],
    ["weighing-analytics-shed", "/weighing/analytics?scope_mode=company&tab=shed"],
    ["weighing-analytics-weight", "/weighing/analytics?scope_mode=company&tab=weight"],
    ["weighing-analytics-weight-not-shown", "/weighing/analytics?scope_mode=company&tab=weight&fb_view=unmatched"],
    ["weighing-analytics-weight-band-filter", "/weighing/analytics?scope_mode=company&tab=weight&fb_band=25_30&fb_animals=all"],
    ["weighing-analytics-time", "/weighing/analytics?scope_mode=company&tab=time"],
    ["weighing-analytics-load", "/weighing/analytics?scope_mode=company&tab=load"],
    ["weighing-sops", "/weighing/sops?scope_mode=company"],
    ["weighing-weights", "/weighing/weights?scope_mode=company"],
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
    ["people-vaccination", "/people?scope_mode=company&tab=vaccination"],
    ["people-clock", "/people?scope_mode=company&tab=clock"],
    ["people-notifications", "/people?scope_mode=company&tab=notifications"],
    ["ceo-ai-admin", "/ceo-ai-admin?scope_mode=company"],
    ["routines", "/routines?scope_mode=company"],
    ["leave", "/leave?scope_mode=company"],
    ["leave-approved", "/leave?scope_mode=company&status=approved"],
    ["leave-rejected", "/leave?scope_mode=company&status=rejected"],
    ["leave-withdrawn", "/leave?scope_mode=company&status=withdrawn"],
    ["routines", "/routines?scope_mode=company"],
    ["tasks", "/tasks?scope_mode=company"],
  ]);

  for (const [routeName, path] of required) {
    assert.equal(routes.get(routeName), path, `${routeName} must stay in the live visual smoke sweep`);
  }
  for (const routeName of [
    "vaccination-schedule",
    "goat-passport",
    "procurement-load-detail",
    "workflow-record",
    "calendar-drive-detail",
    "vaccination-shed-execution-detail",
  ]) {
    assert.ok(routes.has(routeName), `${routeName} dynamic route must stay in the live visual smoke sweep`);
  }
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

test("pager-required routes cannot pass silently when the pager is missing", () => {
  const pagerBlock = smokeSource.match(/async function assertPaginationControls[\s\S]*?\n}\n\nasync function exerciseFirstPagerRoundTrip/)?.[0] ?? "";
  assert.match(pagerBlock, /const bodyText = \(await page\.locator\("body"\)\.innerText\(\)\.catch\(\(\) => ""\)\)\.replace/);
  assert.match(pagerBlock, /0 rows\|0 results\|Nothing\|No rows\|No data/);
  assert.match(pagerBlock, /expected at least \$\{minimum\} pager2 footer\(s\), found none/);
});

test("PR264 routes record route-specific product signals in browser evidence", () => {
  assert.match(smokeSource, /const routeSignals = await assertRouteLoadedSignal\(page, route\.name, visibleText\);/);
  assert.match(smokeSource, /route_signals: routeSignals/);
  assert.match(smokeSource, /routeName === "work-board"[\s\S]*lane_counts/);
  assert.match(smokeSource, /routeName === "weighing-weights"[\s\S]*has_losing_weight_table/);
  assert.match(smokeSource, /routeName === "weighing-analytics"[\s\S]*has_weighing_kpis/);
  assert.match(smokeSource, /routeName === "weighing-analytics-breed" \|\| routeName === "weighing-analytics-breed-wide"[\s\S]*has_breed_breakdown/);
  assert.match(smokeSource, /routeName === "weighing-analytics-time"[\s\S]*has_weekly_growth/);
  assert.match(smokeSource, /routeName === "weighing-analytics-load"[\s\S]*has_load_breakdown/);
});

test("historical Work Board proof requires actual rendered cards", () => {
  assert.ok(smokeSource.includes('page.locator(".card[data-filter-row]")'));
  assert.match(smokeSource, /routeName === "work-board-populated" && hasWorkCards <= 0/);
  assert.ok(!smokeSource.includes('[data-work-board-row]'));
});
