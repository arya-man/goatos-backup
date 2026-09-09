import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const rootPageSource = readFileSync(
  new URL("../../app/(admin)/page.tsx", import.meta.url),
  "utf8",
);

// ADG Analytics is the landing page (maintainer request 2026-09-09): "/" redirects to the
// weighing-analytics page contract's href whenever the principal holds it.
test("admin root route lands on ADG Analytics when its page contract is present", () => {
  assert.match(rootPageSource, /const LANDING_ROUTE_ID = "weighing-analytics"/);
  assert.match(rootPageSource, /const CONTROL_TOWER_LENS = "control-tower"/);
  assert.match(rootPageSource, /one\(sp, "lens"\) === CONTROL_TOWER_LENS/);
  assert.match(rootPageSource, /item\.route_id === LANDING_ROUTE_ID/);
  assert.match(rootPageSource, /import \{ parseScope, scopeHref \} from "@\/lib\/scope";/);
  assert.match(rootPageSource, /if \(!requestedControlTower && landing\?\.href\) \{\s*redirect\(scopeHref\(landing\.href, parseScope\(sp\)\)\);/);
});

test("admin root route keeps explicit Control Tower deep links reachable", () => {
  assert.match(rootPageSource, /getAdminWebBootstrap/);
  assert.doesNotMatch(rootPageSource, /requireAdminWebPageContract\("control-tower"\)/);
  assert.match(rootPageSource, /route_id === "control-tower"/);
  assert.match(rootPageSource, /if \(requestedControlTower && controlTower\) \{\s*return <ControlTowerPage searchParams=\{sp\} pageContract=\{controlTower\} \/>;/);
  assert.match(rootPageSource, /if \(controlTower\?\.href && !landing\?\.href\) \{\s*redirect\(scopeHref\("\/", parseScope\(sp\), \{\}, \{ lens: CONTROL_TOWER_LENS \}\)\);/);
  assert.match(rootPageSource, /enabledPublished\.find\(\(item\) => item\.href === "\/verify"\)/);
  assert.match(rootPageSource, /redirect\(firstEnabledPublishedHref\(contract\) \?\? "\/vaccination"\)/);
});
