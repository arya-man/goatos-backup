// Every performance event the admin shell sends must be accepted by the beacon route, or the
// browser console shows a 400 `unsupported_event` on each client-side navigation (gate-1 #19).
import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { isAllowedPerformanceEvent } from "./performance-events-allowlist.ts";

const here = dirname(fileURLToPath(import.meta.url));

test("the query-navigation beacons the shell sends are accepted", () => {
  assert.equal(isAllowedPerformanceEvent("admin_query_navigation_start"), true);
  assert.equal(isAllowedPerformanceEvent("admin_query_navigation_render"), true);
  assert.equal(isAllowedPerformanceEvent("admin_route_navigation_commit"), true);
  assert.equal(isAllowedPerformanceEvent("admin_backend_api_read"), true);
  assert.equal(isAllowedPerformanceEvent("feed_config_filter_apply_start"), true);
});

test("unknown and empty event names are still refused", () => {
  assert.equal(isAllowedPerformanceEvent(""), false);
  assert.equal(isAllowedPerformanceEvent("random_event"), false);
  assert.equal(isAllowedPerformanceEvent("admin_"), false);
});

test("every literal event name the shell reports is on the allow-list", () => {
  const shell = readFileSync(join(here, "..", "components", "mesha-shell.tsx"), "utf8");
  const names = [...shell.matchAll(/reportAdminPerformanceEvent\(\s*"([a-z_]+)"/g)].map((m) => m[1]);
  assert.ok(names.length >= 7, `expected the shell to report events, found ${names.length}`);
  for (const name of names) {
    assert.equal(isAllowedPerformanceEvent(name), true, `${name} would be answered 400 unsupported_event`);
  }
});

test("the route uses the shared allow-list rather than its own copy", () => {
  const route = readFileSync(join(here, "..", "app", "api", "admin-web", "performance-events", "route.ts"), "utf8");
  assert.match(route, /isAllowedPerformanceEvent/);
  assert.doesNotMatch(route, /const ALLOWED_EVENT_PREFIXES/);
});
