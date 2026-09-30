import test from "node:test";
import assert from "node:assert/strict";
import { chromium } from "playwright";

import plugin, { probeDeadControls, probeDrawerPrimaryPlacement, probeFieldWidths, probeTextFit } from "./text-fit.mjs";

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
    assert.deepEqual(kinds, ["axis-label-overlap", "button-label-wrap", "placeholder-clipped", "raw-id-text", "raw-id-text"], JSON.stringify(found));
    assert.deepEqual((await page.evaluate(probeDeadControls, "main")).map((f) => f.detail), ['"New task" is disabled in the page header']);
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

// guard: no-dead-controls (J2B P2-8, DECIDED no dead controls). A dimmed idle Apply / Save is dead
// in the page BODY and in drawers too, not only in the page header. Pagers, a loading button and an
// enabled Apply pass.
test("probeDeadControls flags a disabled Apply / Save / contained button in the body and in a drawer", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    await page.setContent(`<main style="font:14px sans-serif">
      <div><button class="MuiButton-root MuiButton-contained Mui-disabled" disabled>Apply filters</button></div>
      <div><button class="MuiButton-root MuiButton-text Mui-disabled" disabled>Save</button></div>
      <div><button class="MuiButton-root MuiButton-outlined Mui-disabled" disabled>Clear</button></div>
      <div><button class="MuiButton-root MuiButton-contained MuiButton-loading Mui-disabled" disabled>Apply</button></div>
      <div class="MuiTablePagination-root"><button class="MuiButton-root MuiButton-contained Mui-disabled" disabled>Next</button></div>
      <div><button class="MuiButton-root MuiButton-contained">Apply</button></div>
      <div style="visibility:hidden"><button class="MuiButton-root MuiButton-contained Mui-disabled" disabled>Apply margin</button></div>
    </main>
    <div class="MuiDrawer-paper" data-r2-paper="1"><button class="MuiButton-root MuiButton-contained Mui-disabled" disabled>Save</button></div>`);
    const body = (await page.evaluate(probeDeadControls, "main")).map((f) => f.detail);
    assert.deepEqual(body, ['"Apply filters" is disabled in the page body', '"Save" is disabled in the page body'], JSON.stringify(body));
    const drawer = (await page.evaluate(probeDeadControls, "[data-r2-paper]")).map((f) => f.detail);
    assert.deepEqual(drawer, ['"Save" is disabled in the drawer / dialog']);
    const src = (await import("node:fs")).readFileSync(new URL("./text-fit.mjs", import.meta.url), "utf8");
    assert.match(src, /page\.evaluate\(probeDeadControls, "\.minimal__layout__main__content, main"\)/, "the plugin runs the body probe on every scanned page");
    const audit = (await import("node:fs")).readFileSync(new URL("../r2-visual-audit.mjs", import.meta.url), "utf8");
    assert.match(audit, /page\.evaluate\(probeDeadControls, "\[data-r2-paper\]"\)/, "the drawers lane runs it on every opened overlay");
  } finally {
    await browser.close();
  }
});

// guard: drawer-field-width (J2B P1-1). The /people Add person Department + Designation selects
// collapsed to ~60px next to full-width Role / Park fields. Form-column fields share one width.
test("probeFieldWidths flags a collapsed drawer field next to full-width siblings", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    const field = (w, label) => `<div class="MuiTextField-root" style="width:${w}px;height:40px"><label>${label}</label></div>`;
    await page.setContent(`<div class="MuiDrawer-paper" data-r2-paper="1" style="width:480px">
      ${field(432, "Role")}${field(432, "Park")}${field(60, "Department (optional)")}${field(210, "From")}${field(210, "To")}
      <table><tr><td>${field(80, "Cap")}</td></tr></table></div>`);
    const found = await page.evaluate(probeFieldWidths, "[data-r2-paper]");
    assert.deepEqual(found.map((f) => f.kind), ["field-collapsed"], JSON.stringify(found));
    assert.match(found[0].detail, /Department \(optional\).*60px/);
    await page.setContent(`<div class="MuiDrawer-paper" data-r2-paper="1">${field(432, "Role")}${field(432, "Department")}</div>`);
    assert.deepEqual(await page.evaluate(probeFieldWidths, "[data-r2-paper]"), []);
    const { isP0 } = await import("../r2-visual-audit.mjs");
    assert.ok(isP0("drawer|field-collapsed") && isP0("drawer|dead-control"));
  } finally {
    await browser.close();
  }
});

test("header-primary-height flags a 44px header primary at desktop, passes 36px", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    await page.setContent(`<main><header data-page-header>
      <a class="MuiButton-root MuiButton-contained" style="display:inline-block;height:44px">Add person</a>
      <a class="MuiButton-root MuiButton-contained" style="display:inline-block;height:36px">New task</a></header></main>`);
    const found = (await page.evaluate(probeTextFit)).filter((f) => f.kind === "header-primary-height");
    assert.deepEqual(found.map((f) => f.detail), ['"Add person" is 44px tall (template header action 36px)']);
  } finally {
    await browser.close();
  }
});

// guard: drawer-primary-in-footer (J2B P2-12). A drawer with a footer row keeps its form's Save there
// (template DialogActions order: dismiss, then primary), never inline in the scrolling body.
test("probeDrawerPrimaryPlacement flags a body Save when the drawer has a footer row", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    const drawer = (bodyBtn, footBtn) => `<div class="MuiDrawer-paper" data-r2-paper="1"><div>Edit park</div>
      <div class="minimal__scrollbar__root"><form>${bodyBtn}</form></div>
      <div>${footBtn}</div></div>`;
    await page.setContent(drawer('<button class="MuiButton-root MuiButton-contained">Save</button>', '<button class="MuiButton-root MuiButton-outlined">Close</button>'));
    assert.deepEqual((await page.evaluate(probeDrawerPrimaryPlacement, "[data-r2-paper]")).map((f) => f.kind), ["primary-in-body"]);
    await page.setContent(drawer("", '<button class="MuiButton-root MuiButton-outlined">Close</button><button class="MuiButton-root MuiButton-contained">Save</button>'));
    assert.deepEqual(await page.evaluate(probeDrawerPrimaryPlacement, "[data-r2-paper]"), []);
    const { isP0 } = await import("../r2-visual-audit.mjs");
    assert.ok(isP0("drawer|primary-in-body"));
  } finally {
    await browser.close();
  }
});
