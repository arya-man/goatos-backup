import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const smokeSource = readFileSync(new URL("./smoke-visual-live.mjs", import.meta.url), "utf8");
const smokeRouteBlock = smokeSource.match(/function buildRoutes\(goatId, procurementLoadId\) \{[\s\S]*?const pagerMinimums = new Map/)?.[0] ?? "";

test("visual smoke visits every live sidebar navigation leaf", () => {
  const routeEntries = Array.from(
    smokeSource.matchAll(/name:\s*"([^"]+)"[\s\S]{0,220}?path:\s*([`"])([^`"]+)/g),
    ([, name, quote, path]) => [name, quote === "`" ? path.replace(/\$\{[^}]+\}/g, "${dynamic}") : path],
  );
  const routes = new Map(routeEntries);
  const required = new Map([
    ["control-tower", "/?scope_mode=company"],
    ["action-center", "/action-center?scope_mode=company"],
    ["calendar", "/calendar?scope_mode=company&day=week"],
    ["protocol-adherence", "/protocol-adherence?scope_mode=company"],
    ["workflows", "/workflows?scope_mode=company"],
    ["procurement-source-entry", "/procurement/source-entry?scope_mode=company"],
    ["procurement-vendors", "/procurement/vendors?scope_mode=company"],
    ["procurement-feed-purchases", "/procurement/feed-purchases?scope_mode=company"],
    ["approvals", "/approvals?scope_mode=company"],
    ["verify", "/verify?scope_mode=company"],
    ["sales", "/sales?scope_mode=company"],
    ["sales-loads", "/sales/loads?scope_mode=company"],
    ["sales-config", "/sales/config?scope_mode=company"],
    ["feed-config", "/feed/config?scope_mode=company"],
    ["feed-analytics", "/feed/analytics?scope_mode=company"],
    ["feed-sops", "/feed/sops?scope_mode=company"],
    ["weighing-analytics", "/weighing/analytics?scope_mode=company"],
    ["weighing-sops", "/weighing/sops?scope_mode=company"],
    ["weighing-weights", "/weighing/weights?scope_mode=company"],
    ["counts-analytics", "/counts/analytics?scope_mode=company"],
    ["counts-breakdown", "/counts/breakdown?scope_mode=company"],
    ["counts-sops", "/counts/sops?scope_mode=company"],
    ["counts-herd", "/counts/herd?scope_mode=company"],
    ["counts-milk-preparation", "/counts/milk-preparation?scope_mode=company"],
    ["milk-sops", "/milk/sops?scope_mode=company"],
    ["herd-signals", "/herd-signals?scope_mode=company"],
    ["health-config", "/health/config?scope_mode=company"],
    ["operations-audit", "/operations/audit?scope_mode=company"],
    ["operations-dlq", "/operations/dlq?scope_mode=company"],
    ["people", "/people?scope_mode=company"],
  ]);

  for (const [routeName, path] of required) {
    assert.equal(routes.get(routeName), path, `${routeName} must stay in the live visual smoke sweep`);
  }
  for (const routeName of ["vaccination-schedule", "goat-passport", "procurement-load-detail"]) {
    assert.ok(routes.has(routeName), `${routeName} dynamic route must stay in the live visual smoke sweep`);
  }
});

test("visual smoke keeps every live sidebar leaf covered on desktop and narrow/mobile", () => {
  assert.match(smokeSource, /label:\s*"desktop"[\s\S]*?width:\s*1440[\s\S]*?height:\s*1000/);
  assert.match(smokeSource, /label:\s*"narrow"[\s\S]*?width:\s*390[\s\S]*?height:\s*900/);

  const sidebarRouteNames = [
    "control-tower",
    "action-center",
    "calendar",
    "protocol-adherence",
    "workflows",
    "procurement-source-entry",
    "procurement-vendors",
    "procurement-feed-purchases",
    "approvals",
    "verify",
    "sales",
    "sales-loads",
    "sales-config",
    "feed-config",
    "feed-analytics",
    "feed-sops",
    "weighing-analytics",
    "weighing-sops",
    "weighing-weights",
    "counts-analytics",
    "counts-breakdown",
    "counts-sops",
    "counts-herd",
    "counts-milk-preparation",
    "milk-sops",
    "herd-signals",
    "health-config",
    "operations-audit",
    "operations-dlq",
    "people",
  ];

  for (const routeName of sidebarRouteNames) {
    const routeEntry = smokeRouteBlock.match(new RegExp(`name:\\s*"${routeName}"[\\s\\S]*?(?=\\n\\s*\\{|\\n\\s*\\];)`))?.[0] ?? "";
    assert.ok(routeEntry, `${routeName} must stay in the live visual smoke sweep`);
    assert.doesNotMatch(routeEntry, /viewports:\s*\[/, `${routeName} must run in both desktop and narrow visual sweeps`);
  }
});
