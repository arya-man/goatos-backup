import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { readFileSync } from "node:fs";
import { compareBlocks, isVisuallyHidden, discoverRoutes, gateFailures, groupPatterns, iou, isP0, parsePageMap, routePattern, scrollJumped, skeletonRoutesFor, templateFor } from "./r2-visual-audit.mjs";

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
  for (const p of ["interact|Tab|full-reload", "interact|Filter link|skeleton-flash", "off-palette|color|x", "drawer|no-backdrop|drawer", "drawer|overflow|clipped", "skeleton|mismatch|tabs", "skeleton|missing|kpi-row", "tap|button", "sideways-scroll|390", "route|error", "interact|Tab|tab-not-selected", "interact|Filter select|stale-panel", "interact|Tab|scroll-jump"]) assert.ok(isP0(p), p);
  for (const p of ["interact|Tab|layout-jump", "contrast|x", "drawer|width|narrower", "skeleton|not-shown", "drawer|overflow|table-scroll"]) assert.ok(!isP0(p), p);
});

// guard: tab-scroll-kept (TR3-P1-3). A /vaccination park tab / shed status tab click must leave the
// page where the reader was: the interact check records scrollY at the click (after any pre-scroll)
// and after settling; a >150px move is a P0 unless the page only got shorter and clamped to its bottom.
test("guard: tab-scroll-kept - scrollJumped flags a jump, not a clamp to a shorter page", () => {
  assert.equal(scrollJumped(null), false, "no click recorded");
  assert.equal(scrollJumped({ clickScroll: 3420, nowScroll: 3420, maxScroll: 6900 }), false, "kept");
  assert.equal(scrollJumped({ clickScroll: 3420, nowScroll: 3500, maxScroll: 6900 }), false, "small settle");
  assert.equal(scrollJumped({ clickScroll: 3441, nowScroll: 0, maxScroll: 6900 }), true, "jumped to the top");
  assert.equal(scrollJumped({ clickScroll: 0, nowScroll: 3441, maxScroll: 6900 }), true, "jumped down to an anchor");
  assert.equal(scrollJumped({ clickScroll: 5134, nowScroll: 4927, maxScroll: 4927 }), false, "fewer rows: clamped to the new bottom");
  assert.equal(scrollJumped({ clickScroll: 5134, nowScroll: 2000, maxScroll: 4927 }), true, "scrolled above the clamp = a jump");
  const src = readFileSync(join(appRoot, "scripts", "r2-visual-audit.mjs"), "utf8");
  assert.match(src, /w\.clickScroll = scrollY;/, "the watch records the scroll at the click");
  assert.match(src, /scrollJumped\(w\.scroll\)\) fails\.push\(\["scroll-jump"/, "the interact check fails on a jump");
});

// guard: skeleton-on-touched (TR3 FINAL). TR-3's P0s were layout fixes that changed a page and not its
// loading twin. The fast / pre-push lane runs the skeleton twin check (1440 + 390 dark) on every
// route the push touched.
test("guard: skeleton-on-touched - the fast lane runs the skeleton check on touched routes", () => {
  assert.deepEqual([...skeletonRoutesFor([{ route: "/vaccination", distance: 1 }, { route: "/procurement/source-entry", distance: 2 }])], ["/vaccination", "/procurement/source-entry"]);
  const src = readFileSync(join(appRoot, "scripts", "r2-visual-audit.mjs"), "utf8");
  assert.match(src, /if \(fast && !args\.checks\) skeletonTouched = skeletonRoutesFor\(t\.routes\);/);
  assert.match(src, /if \(checks\.has\("skeleton"\) \|\| skeletonTouched\?\.has\(route\.route\)\) for \(const p of skeletonProfiles\)/);
  assert.match(src, /: fast \? \["1440-dark", "390-dark"\]/, "fast skeleton runs at 1440 and 390 dark");
});

test("compareBlocks: an optional skeleton block the page skipped is not an extra", () => {
  const skel = [{ x: 0, y: 0, w: 100, h: 40, kind: "header" }, { x: 0, y: 64, w: 100, h: 140, kind: "kpi-row", optional: true }, { x: 0, y: 228, w: 100, h: 400, kind: "table" }];
  const loaded = [{ x: 0, y: 0, w: 100, h: 40, kind: "header" }, { x: 0, y: 64, w: 100, h: 400, kind: "table" }];
  const cmp = compareBlocks(skel, loaded);
  assert.equal(cmp.extra.length, 0);
  // The blocks under a skipped optional block still move up: that jump stays a mismatch.
  assert.equal(cmp.mismatched.length, 1);
  const required = compareBlocks(skel.map(({ optional, ...b }) => b), loaded);
  assert.equal(required.extra.length + required.mismatched.length > 0, true);
});

test("compareBlocks: a card grid is judged by its card, not its data-driven item count (TR1-#1)", () => {
  // one page of three placeholder cards vs the one SOP the module holds (a lone loaded card)
  const card = { x: 340, y: 460, w: 337, h: 284 };
  const grid = { x: 340, y: 460, w: 1060, h: 284, item: card };
  assert.equal(compareBlocks([grid], [{ ...card, kind: "block" }]).mismatched.length, 0);
  // a loaded grid with more rows than the placeholder row: first cards match
  assert.equal(compareBlocks([grid], [{ x: 340, y: 460, w: 1060, h: 440, item: { ...card, h: 286 }, kind: "card-grid" }]).mismatched.length, 0);
  // the wrong card anatomy (two columns, a shorter card) still fails
  const wrong = { x: 340, y: 460, w: 1060, h: 200, item: { x: 340, y: 460, w: 518, h: 200 } };
  assert.equal(compareBlocks([wrong], [{ ...card, kind: "block" }]).mismatched.length + compareBlocks([wrong], [{ ...card, kind: "block" }]).missing.length > 0, true);
  // blocks without `item` (a KPI row: count is static) keep plain IoU
  assert.equal(compareBlocks([{ x: 340, y: 178, w: 1060, h: 118 }], [{ x: 340, y: 178, w: 700, h: 118, kind: "kpi-row" }]).mismatched.length, 1);
});

test("compareBlocks: a table card is judged by its chrome above the rows (TR1-#1)", () => {
  const skel = { x: 340, y: 320, w: 1060, h: 580, head: { x: 340, y: 320, w: 1060, h: 190 } };
  assert.equal(compareBlocks([skel], [{ x: 340, y: 320, w: 1060, h: 300, head: { x: 340, y: 320, w: 1060, h: 192 }, kind: "table" }]).mismatched.length, 0);
  // a toolbar row the skeleton lacks moves the head: still a failure
  assert.equal(compareBlocks([skel], [{ x: 340, y: 320, w: 1060, h: 300, head: { x: 340, y: 320, w: 1060, h: 290 }, kind: "table" }]).mismatched.length, 1);
});

test("chart-black reads drawn marks only: a series group / legend svg wrapper inherits black but paints nothing (TR1-#3)", async () => {
  const { readFileSync } = await import("node:fs");
  const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "r2-visual-audit.mjs"), "utf8");
  const mark = src.match(/function isChartMark\(el\) \{ return (\/.*?\/i)\.test\(el\.tagName\); \}/);
  assert.ok(mark, "isChartMark gate missing from the page lib");
  const re = new Function(`return ${mark[1]}`)();
  for (const tag of ["path", "rect", "circle", "polygon"]) assert.equal(re.test(tag), true, tag);
  for (const tag of ["g", "svg", "text", "foreignObject"]) assert.equal(re.test(tag), false, tag);
  assert.match(src, /inSvg && isChartMark\(el\) && el\.closest\("\.apexcharts-series, \.apexcharts-legend-marker"\)/);
  // a transparent mark paints nothing; an opaque black one is still the P0
  assert.match(src, /fo > 0\.05 && f\[0\] === 0 && f\[1\] === 0 && f\[2\] === 0/);
});

test("per-route ratchet: any new failure on a shell/touched route fails, other routes keep the P0 ratchet", () => {
  const pat = (pattern, routes, p0 = false) => ({ pattern, label: pattern, p0, routes, routeCount: routes.length });
  const baseline = { "tap|button": 2 };
  const routeBaseline = { "/verify": ["contrast|a|x", "tap|button"], "/other": ["tap|button"] };
  const patterns = [
    pat("contrast|a|x", ["/verify"]), // known on /verify
    pat("contrast|b|y", ["/verify"]), // NEW on a strict route -> fails although not P0
    pat("contrast|c|z", ["/elsewhere"]), // new but not strict, not P0 -> passes
    pat("tap|button", ["/verify", "/other"], true), // P0, not grown
  ];
  const fails = gateFailures(patterns, baseline, false, { strictRoutes: new Set(["/verify"]), routeBaseline });
  assert.deepEqual(fails.map((f) => f.pattern), ["contrast|b|y"]);
  assert.match(fails[0].why, /new on \/verify/);
  // without a per-route baseline the old P0-only behaviour holds
  assert.deepEqual(gateFailures(patterns, baseline, false, {}).map((f) => f.pattern), []);
});

test("touched files map to routes through the import graph; shell files flag the shell", async () => {
  const { routesForFiles, fastRouteSet, SHELL_ROUTES } = await import("./r2-visual-audit.mjs");
  const routes = discoverRoutes(join(appRoot, "app", "(admin)"));
  const verifyPage = routes.find((r) => r.route === "/verify");
  assert.ok(verifyPage, "/verify page exists");
  const direct = routesForFiles([`app/(admin)/verify/page.tsx`], routes, appRoot);
  assert.equal(direct.routes[0].route, "/verify");
  assert.equal(direct.routes[0].distance, 0);
  const shell = routesForFiles(["components/mesha-shell.tsx"], routes, appRoot);
  assert.equal(shell.shell, true, "the shell component is reached from the (admin) layout");
  const css = routesForFiles(["app/mesha-theme.css"], routes, appRoot);
  assert.equal(css.shell, true, "global css is shell");
  const none = routesForFiles(["docs/x.md"], routes, appRoot);
  assert.equal(none.routes.length, 0);
  const set = fastRouteSet([{ route: "/verify", distance: 0 }, { route: "/a", distance: 1 }, { route: "/b", distance: 2 }], { cap: 1 });
  assert.deepEqual(set.routes, [...SHELL_ROUTES, "/a"]);
  assert.deepEqual(set.skipped, ["/b"]);
});

test("chart-black: an unresolved chart colour (black series mark) is a P0", () => {
  assert.equal(isP0("chart-black|path.apexcharts-bar-area"), true);
});

// guard: theme-palette-follows-mode + chart-light-scheme (2026-09-27, /sales/sold #54A02C bars in dark).
test("chart-light-scheme: light-only palette hexes are derived from theme-config and are P0", async () => {
  const { readFileSync } = await import("node:fs");
  const mod = await import("./r2-visual-audit.mjs");
  const cfg = readFileSync(new URL("../theme/theme-config.ts", import.meta.url), "utf8");
  const lightOnly = mod.lightOnlyPaletteHexes(cfg);
  assert.ok(lightOnly.includes("#54a02c"), "light primary.main is light-only");
  assert.ok(!lightOnly.includes("#7ccb45"), "dark primary.main is not in the list");
  assert.ok(mod.isP0("chart-light-scheme|#54a02c"));
});

test("theme.palette follows the active scheme (forceThemeRerender on the app ThemeProvider)", async () => {
  const { readFileSync } = await import("node:fs");
  const src = readFileSync(new URL("../theme/app-theme-provider.tsx", import.meta.url), "utf8");
  assert.match(src, /<ThemeProvider[^>]*\bforceThemeRerender\b/);
});

// guard: tap-skip-visually-hidden (TR1-#8). An `.sr-only` submit ("Apply search" on /leave, /routines)
// was reported as a 1x44 tap target; it is not on screen. A real small control still counts.
test("tap-target check skips visually hidden controls only", () => {
  assert.equal(isVisuallyHidden({ clip: "rect(0px, 0px, 0px, 0px)", overflow: "hidden" }, { width: 1, height: 44 }), true);
  assert.equal(isVisuallyHidden({ clip: "auto", clipPath: "inset(50%)", overflow: "hidden" }, { width: 1, height: 1 }), true);
  assert.equal(isVisuallyHidden({ clip: "auto", clipPath: "none", overflow: "visible" }, { width: 30, height: 30 }), false);
  assert.equal(isVisuallyHidden({ clip: "auto", clipPath: "none", overflow: "hidden" }, { width: 1, height: 44 }), false);
  const src = readFileSync(join(appRoot, "scripts", "r2-visual-audit.mjs"), "utf8");
  const lib = src.slice(src.indexOf("function r2PageLib()"));
  const body = (s) => s.replace(/\s+/g, " ").match(/const clipped = .*?;\s*return clipped && \(.*?\);/)?.[0];
  assert.ok(body(lib), "page lib keeps its own isVisuallyHidden copy");
  assert.equal(body(lib), body(src.slice(src.indexOf("export function isVisuallyHidden"))), "page-lib copy equals the exported rule");
  assert.match(lib, /if \(isVisuallyHidden\(cs, r\)\) continue;/, "tap loop consults the rule");
});

// guard: popover-not-dialog (TR1-#7). The /counts/breakdown tag picker is the template CustomPopover
// (invisible backdrop by design); it was reported as a dialog with no backdrop. Real Dialog / Drawer
// papers are still measured.
test("overlay probe ignores anchored popovers, keeps dialogs and drawers", () => {
  const src = readFileSync(join(appRoot, "scripts", "r2-visual-audit.mjs"), "utf8");
  const probe = src.slice(src.indexOf("function overlayInfo()"), src.indexOf("function overlayInfo()") + 1200);
  assert.match(probe, /\.MuiDrawer-paper, \.MuiDialog-paper, \[role=dialog\]/);
  assert.match(probe, /!p\.closest\("\.MuiPopover-root"\)/);
});
