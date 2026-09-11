import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const smokeSource = readFileSync(new URL("./smoke-visual-live.mjs", import.meta.url), "utf8");
const smokeRouteBlock = smokeSource.match(/function buildRoutes\(\{ goatId, procurementLoadId, workflowRowId, calendarEventId, vaccinationShedPath \}\) \{[\s\S]*?const pagerMinimums = new Map/)?.[0] ?? "";
const routeEntries = Array.from(
  smokeRouteBlock.matchAll(/name:\s*"([^"]+)"[\s\S]{0,500}?path:\s*([`"])([^`"]+)/g),
  ([, name, quote, path]) => [name, quote === "`" ? path.replace(/\$\{[^}]+\}/g, "${dynamic}") : path],
);

test("visual smoke visits every live sidebar navigation leaf", () => {
  const routes = new Map(routeEntries);
  const required = new Map([
    ["control-tower", "/?scope_mode=company&lens=control-tower"],
    ["action-center", "/action-center?scope_mode=company"],
    ["calendar", "/calendar?scope_mode=company&day=week"],
    ["calendar-month", "/calendar?scope_mode=company&view=month"],
    ["calendar-history", "/calendar?scope_mode=company&status=completed"],
    ["calendar-owner-pc", "/calendar?scope_mode=company&day=week&owner_key=pc"],
    ["protocol-adherence", "/protocol-adherence?scope_mode=company"],
    ["workflows", "/workflows?scope_mode=company"],
    ["procurement-source-entry", "/procurement/source-entry?scope_mode=company"],
    ["procurement-vendors", "/procurement/vendors?scope_mode=company"],
    ["procurement-feed-purchases", "/procurement/feed-purchases?scope_mode=company"],
    ["approvals", "/approvals?scope_mode=company"],
    ["verify", "/verify?scope_mode=company"],
    ["actions", "/actions?scope_mode=company"],
    ["verification", "/verification?scope_mode=company"],
    ["vaccination", "/vaccination?scope_mode=company"],
    ["vaccination-execution", "/vaccination?scope_mode=company#execution"],
    ["vaccination-live-tracker", "/vaccination/live-tracker?scope_mode=company"],
    ["vaccination-plan", "/vaccination/plan?scope_mode=company"],
    ["vaccination-plan-edit", "/vaccination/plan/edit?scope_mode=company"],
    ["sales-sold", "/sales/sold?scope_mode=company"],
    ["sales-farm-value", "/sales/farm-value?scope_mode=company"],
    ["sales-loads", "/sales/loads?scope_mode=company"],
    ["sales-loads-farm-born", "/sales/loads?scope_mode=company&view=farm_born"],
    ["sales-config", "/sales/config?scope_mode=company"],
    ["sales-vendors", "/sales/vendors?scope_mode=company"],
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
    ["weighing-analytics", "/weighing/analytics?scope_mode=company"],
    ["weighing-analytics-breed", "/weighing/analytics?scope_mode=company&tab=breed"],
    ["weighing-analytics-birth", "/weighing/analytics?scope_mode=company&tab=birth"],
    ["weighing-analytics-shed", "/weighing/analytics?scope_mode=company&tab=shed"],
    ["weighing-analytics-weight", "/weighing/analytics?scope_mode=company&tab=weight"],
    ["weighing-analytics-time", "/weighing/analytics?scope_mode=company&tab=time"],
    ["weighing-analytics-load", "/weighing/analytics?scope_mode=company&tab=load"],
    ["weighing-sops", "/weighing/sops?scope_mode=company"],
    ["weighing-weights", "/weighing/weights?scope_mode=company"],
    ["counts-analytics", "/counts/analytics?scope_mode=company"],
    ["counts-breakdown", "/counts/breakdown?scope_mode=company"],
    ["counts-sops", "/counts/sops?scope_mode=company"],
    ["counts-sops-builder", "/counts/sops?compose=1&scope_mode=company"],
    ["counts-herd", "/counts/herd?scope_mode=company"],
    ["counts-milk-preparation", "/counts/milk-preparation?scope_mode=company"],
    ["milk-sops", "/milk/sops?scope_mode=company"],
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
    ["operations-audit", "/operations/audit?scope_mode=company"],
    ["operations-dlq", "/operations/dlq?scope_mode=company"],
    ["people", "/people?scope_mode=company"],
    ["people-vaccination", "/people?scope_mode=company&tab=vaccination"],
    ["people-clock", "/people?scope_mode=company&tab=clock"],
    ["people-notifications", "/people?scope_mode=company&tab=notifications"],
    ["ceo-ai-admin", "/ceo-ai-admin?scope_mode=company"],
    ["leave", "/leave?scope_mode=company"],
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

test("pager-required routes cannot pass silently when the pager is missing", () => {
  const pagerBlock = smokeSource.match(/async function assertPaginationControls[\s\S]*?\n}\n\nasync function exerciseFirstPagerRoundTrip/)?.[0] ?? "";
  assert.match(pagerBlock, /const bodyText = \(await page\.locator\("body"\)\.innerText\(\)\.catch\(\(\) => ""\)\)\.replace/);
  assert.match(pagerBlock, /0 rows\|0 results\|Nothing\|No rows\|No data/);
  assert.match(pagerBlock, /expected at least \$\{minimum\} pager2 footer\(s\), found none/);
});
