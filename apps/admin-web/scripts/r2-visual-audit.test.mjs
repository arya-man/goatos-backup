import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { compareBlocks, discoverRoutes, gateFailures, groupPatterns, iou, isP0, parsePageMap, routePattern, templateFor } from "./r2-visual-audit.mjs";

const appRoot = join(dirname(fileURLToPath(import.meta.url)), "..");

test("discovers every app/(admin) page, strips route groups, flags dynamic segments", () => {
  const routes = discoverRoutes(join(appRoot, "app", "(admin)"));
  const byRoute = new Map(routes.map((r) => [r.route, r]));
  assert.ok(routes.length >= 50, `expected the admin route set, got ${routes.length}`);
  assert.ok(byRoute.has("/sales/config"));
  assert.ok(![...byRoute.keys()].some((r) => r.includes("(")), "route groups must not leak into paths");
  const goat = byRoute.get("/goats/[goat_id]");
  assert.equal(goat?.dynamic, true);
  assert.ok(goat.pattern.test("/goats/abc-123"));
  assert.ok(!goat.pattern.test("/goats/abc/extra"));
});

test("routePattern matches only the same shape", () => {
  const re = routePattern("/vaccination/execution/sheds/[shedId]");
  assert.ok(re.test("/vaccination/execution/sheds/42"));
  assert.ok(!re.test("/vaccination/execution/sheds"));
});

test("page map parse resolves template paths, aliases and redirect-only rows", () => {
  const map = parsePageMap([
    "| Route | Template | x |",
    "| `/pc-care/sops` | `/dashboard/user/list` + `/dashboard/post/[title]` | a |",
    "| `/feed/sops` | as `/pc-care/sops` | as |",
    "| `/actions` | redirect-only | none |",
    "| `/goats/[goat_id]` (dynamic) | `/dashboard/user/[id]` (profile) | b |",
  ].join("\n"));
  assert.equal(map.get("/pc-care/sops"), "/dashboard/user/list");
  assert.equal(map.get("/feed/sops"), "/dashboard/user/list");
  assert.equal(map.get("/actions"), null);
  assert.equal(map.get("/goats/[goat_id]"), "/dashboard/user/[id]");
  assert.equal(templateFor("/unknown/deep", map), "/dashboard");
});

test("IoU and block matching flag shape mismatch, missing and extra blocks", () => {
  assert.equal(iou({ x: 0, y: 0, w: 10, h: 10 }, { x: 0, y: 0, w: 10, h: 10 }), 1);
  assert.equal(iou({ x: 0, y: 0, w: 10, h: 10 }, { x: 20, y: 20, w: 5, h: 5 }), 0);
  const skel = [{ x: 0, y: 0, w: 100, h: 40, kind: "skeleton" }, { x: 0, y: 60, w: 100, h: 50, kind: "skeleton" }, { x: 0, y: 500, w: 100, h: 50, kind: "skeleton" }];
  const loaded = [{ x: 0, y: 0, w: 100, h: 40, kind: "header" }, { x: 0, y: 60, w: 100, h: 200, kind: "kpi-row" }, { x: 0, y: 300, w: 100, h: 100, kind: "table" }];
  const cmp = compareBlocks(skel, loaded);
  assert.equal(cmp.matches.length, 2);
  assert.deepEqual(cmp.mismatched.map((m) => m.loaded.kind), ["kpi-row"]);
  assert.deepEqual(cmp.missing.map((b) => b.kind), ["table"]);
  assert.equal(cmp.extra.length, 1);
});

test("patterns group identical failures across routes and rank by route count; gate ratchets", () => {
  const f = (route, pattern) => ({ route, pattern, label: pattern, check: "x", severity: "fail", profile: "1440-dark" });
  const findings = [f("/a", "dark-bright-bg|kpi"), f("/b", "dark-bright-bg|kpi"), f("/b", "dark-bright-bg|kpi"), f("/c", "interact|Tab|layout-jump"), { ...f("/d", "route|no-sample-id"), severity: "info" }];
  const patterns = groupPatterns(findings);
  assert.equal(patterns[0].pattern, "dark-bright-bg|kpi");
  assert.equal(patterns[0].routeCount, 2);
  assert.equal(patterns[0].count, 3);
  assert.equal(patterns[0].p0, true);
  assert.equal(patterns.find((p) => p.pattern.startsWith("interact")).p0, false);
  assert.ok(!patterns.some((p) => p.pattern === "route|no-sample-id"), "info findings are not failure patterns");
  assert.equal(gateFailures(patterns, null, false).length, 1);
  assert.equal(gateFailures(patterns, { "dark-bright-bg|kpi": 2 }, false).length, 0);
  assert.equal(gateFailures(patterns, { "dark-bright-bg|kpi": 1 }, false).length, 1, "a grown pattern fails");
  assert.equal(gateFailures(patterns, { "dark-bright-bg|kpi": 2 }, true).length, 1, "strict ignores the baseline");
});

test("P0 classification covers the gate's pattern families", () => {
  for (const p of ["interact|Tab|full-reload", "interact|Filter link|skeleton-flash", "off-palette|color|x", "drawer|no-backdrop|drawer", "drawer|overflow|clipped", "skeleton|mismatch|tabs", "skeleton|missing|kpi-row", "tap|button", "sideways-scroll|390", "route|error"]) assert.ok(isP0(p), p);
  for (const p of ["interact|Tab|layout-jump", "contrast|x", "drawer|width|narrower", "skeleton|not-shown", "drawer|overflow|table-scroll"]) assert.ok(!isP0(p), p);
});
