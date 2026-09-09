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
  assert.match(rootPageSource, /item\.route_id === LANDING_ROUTE_ID/);
  assert.match(rootPageSource, /if \(landing\?\.href\) \{\s*redirect\(landing\.href\);/);
});

test("admin root route redirects when control tower is absent from the role bootstrap contract", () => {
  assert.match(rootPageSource, /getAdminWebBootstrap/);
  assert.doesNotMatch(rootPageSource, /requireAdminWebPageContract\("control-tower"\)/);
  assert.match(rootPageSource, /route_id === "control-tower"/);
  assert.match(rootPageSource, /enabledPublished\.find\(\(item\) => item\.href === "\/verify"\)/);
  assert.match(rootPageSource, /redirect\(firstEnabledPublishedHref\(contract\) \?\? "\/vaccination"\)/);
});
