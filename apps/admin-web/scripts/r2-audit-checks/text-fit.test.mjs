import test from "node:test";
import assert from "node:assert/strict";
import { chromium } from "playwright";

import plugin, { probeTextFit } from "./text-fit.mjs";

// guards: button-label-wrap (J3 P1-3), axis-label-overlap (J3 P1-4), raw-id-text (J2 P1-3/P1-4),
// dead-primary (J2 P1-1). Each probe flags the defect and passes the fixed shape.
test("text-fit plugin is a P0 r2 audit check", () => {
  assert.equal(plugin.name, "text-fit");
  assert.equal(plugin.p0, true);
  assert.equal(typeof plugin.run, "function");
});

test("probe flags a wrapped button label, overlapping axis labels, raw ids and a dead header primary", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 800 } });
    await page.setContent(`<main style="font:14px sans-serif">
      <header data-page-header><button class="MuiButton-root MuiButton-contained Mui-disabled" disabled>New task</button></header>
      <div style="display:flex;width:120px"><button class="MuiButton-root" style="width:40px">Open the draft</button>
      <button class="MuiButton-root" style="white-space:nowrap">Keep it</button></div>
      <div class="apexcharts-canvas"><svg width="300" height="60">
        <text class="apexcharts-xaxis-label" x="10" y="20">Sirohi</text>
        <text class="apexcharts-xaxis-label" x="40" y="20">Anantapur Sheep</text>
        <text class="apexcharts-xaxis-label" x="200" y="50" transform="rotate(-45 200 50)">Beetal</text>
        <text class="apexcharts-xaxis-label" x="210" y="50" transform="rotate(-45 210 50)">Sojat</text>
      </svg></div>
      <table><tr><td>cee6e124</td><td>Beetal</td><td>2026</td></tr></table>
      <div class="MuiCardHeader-subheader">goat_identity_events</div>
      <div class="MuiCardHeader-subheader">Identity changes</div>
      <input style="width:120px;font:14px sans-serif" placeholder="Search animal, RFID, smart tag, BLE MAC, pen, breed or gateway" />
      <input style="width:220px;font:14px sans-serif" placeholder="Search" />
    </main>`);
    const found = await page.evaluate(probeTextFit);
    const kinds = found.map((f) => f.kind).sort();
    assert.deepEqual(kinds, ["axis-label-overlap", "button-label-wrap", "dead-primary", "placeholder-clipped", "raw-id-text", "raw-id-text"], JSON.stringify(found));
    assert.ok(found.some((f) => /Open the draft/.test(f.detail)));
    assert.ok(!found.some((f) => /Keep it/.test(f.detail)));
    assert.ok(found.some((f) => /cee6e124/.test(f.detail)) && found.some((f) => /goat_identity_events/.test(f.detail)));
    assert.ok(!found.some((f) => /Beetal|2026/.test(f.detail)), "words, years and rotated labels pass");
  } finally {
    await browser.close();
  }
});

test("axis-label-overlap names an Apex label once (tspan + title copy)", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 400 } });
    await page.setContent(`<main style="font:12px sans-serif"><div class="apexcharts-canvas"><svg width="300" height="40">
      <text class="apexcharts-xaxis-label" x="10" y="20"><tspan>60,000</tspan><title>60,000</title></text>
      <text class="apexcharts-xaxis-label" x="40" y="20"><tspan>80,000</tspan><title>80,000</title></text>
    </svg></div></main>`);
    const found = await page.evaluate(probeTextFit);
    assert.equal(found.length, 1, JSON.stringify(found));
    assert.match(found[0].detail, /^"60,000" x "80,000" overlap/);
  } finally {
    await browser.close();
  }
});

// guard: chart-label-every-route (FIXJ5). The fast pre-push lane audits the shell routes + at most 8
// touched routes, so a chart route beyond the cap (/feed/analytics) is judged only by the FULL run.
// The full run audits every app/(admin) route with `scan` on every profile and the text-fit plugin
// declares no profile filter, so every route that renders an Apex chart gets the 390 label check.
test("the full audit runs the chart-label check on every chart route at every profile", async () => {
  const { discoverRoutes, routesForFiles, fastRouteSet, PROFILES } = await import("../r2-visual-audit.mjs");
  const { readFileSync } = await import("node:fs");
  const { fileURLToPath } = await import("node:url");
  const { dirname, join, resolve } = await import("node:path");
  const appRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
  assert.equal(plugin.profiles, undefined, "text-fit must run at 390 dark as well as 1440");
  assert.ok(PROFILES.some((p) => p.label === "390-dark"));
  const src = readFileSync(join(appRoot, "scripts/r2-visual-audit.mjs"), "utf8");
  assert.match(src, /const ALL_CHECKS = \["scan",/, "the full run includes scan (where plugins run)");
  assert.match(src, /if \(fast && !exactRoutes && !only\) routes = allRoutes\.filter/, "the route cap applies to --fast only");
  const all = discoverRoutes(join(appRoot, "app", "(admin)"));
  const chartModules = ["components/minimal/chart/index.ts", "components/minimal/chart/chart.tsx"];
  const chartRoutes = routesForFiles(chartModules, all, appRoot).routes.map((r) => r.route);
  assert.ok(chartRoutes.includes("/feed/analytics"), "the chart import graph reaches /feed/analytics");
  assert.ok(chartRoutes.length > 8, "more chart routes than the fast cap: the full run must hold them");
  assert.ok(fastRouteSet(chartRoutes.map((route) => ({ route }))).skipped.length > 0);
  const full = new Set(all.map((r) => r.route));
  for (const r of chartRoutes) assert.ok(full.has(r), `${r} is in the full run`);
});
