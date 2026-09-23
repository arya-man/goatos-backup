// Real-DOM test for the phone tap-target rule's zoom allowance.
//
// The rule measures what a finger lands on, which is right until the control sits inside a
// diagram the reader can zoom. The SOP Flow canvas draws its steps on a scaled, pannable
// surface with its own minus / plus / Fit controls; fitted to a phone that lands near 39%, and
// at 39% every control in it measures 39% of itself. The "insert step" button, which the
// stylesheet deliberately sizes at 40px below 1100px wide, measured 16px and went to Slack as
// "Buttons too small to tap — Insert step here" on Sales SOP and Counts SOP, both runs.
//
// canvasZoom lives inside the page.evaluate body of assertLayoutHealthy, so it is lifted out of
// the source here and run in a real browser against the markup the app ships.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { chromium } from "playwright";

const source = readFileSync(new URL("./smoke-visual-live.mjs", import.meta.url), "utf8");

function liftFunction(name) {
  const start = source.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `${name} must still exist in smoke-visual-live.mjs`);
  let depth = 0;
  for (let i = source.indexOf("{", start); i < source.length; i += 1) {
    if (source[i] === "{") depth += 1;
    else if (source[i] === "}") {
      depth -= 1;
      if (depth === 0) return source.slice(start, i + 1);
    }
  }
  throw new Error(`could not read ${name} out of smoke-visual-live.mjs`);
}
const canvasZoomSource = liftFunction("canvasZoom");

// The SOP Flow canvas, as features/sops/flow-canvas.tsx renders it and mesha-theme.css sizes it.
const FLOW_CANVAS = `
  <div class="studio-flow-canvas" style="position:relative;overflow:auto;width:340px;height:300px">
    <div class="studio-flow-scale" style="position:relative;transform-origin:0 0;transform:scale(0.39);width:900px;height:1200px">
      <button class="studio-flow-plus" aria-label="Insert step here" style="position:absolute;left:400px;top:200px;min-width:40px;min-height:40px;border:1px solid #7ac143;border-radius:50%">+</button>
    </div>
  </div>`;
// The same button with the canvas at 1:1, and a genuinely tiny control in ordinary page flow.
const FLOW_CANVAS_UNZOOMED = FLOW_CANVAS.replace("scale(0.39)", "scale(1)");
const BARE_TINY_BUTTON = `<div style="padding:20px"><button aria-label="Close" style="width:20px;height:20px">x</button></div>`;
// A scaled-down control with nothing to pan or zoom is not a canvas and earns nothing.
const SHRUNK_TOOLBAR = `
  <div style="overflow:hidden;width:340px">
    <div style="transform:scale(0.4);transform-origin:0 0">
      <button aria-label="Save" style="width:40px;height:40px">S</button>
    </div>
  </div>`;

let browser;
let page;
test.before(async () => {
  browser = await chromium.launch({ headless: true });
  page = await browser.newPage({ viewport: { width: 390, height: 900 } });
});
test.after(async () => {
  await browser?.close();
});

async function measure(html, selector) {
  await page.setContent(`<!doctype html><html><body style="margin:0;font-family:system-ui"><main>${html}</main></body></html>`);
  return page.evaluate(
    ([fnSource, sel]) => {
      // eslint-disable-next-line no-new-func
      const canvasZoom = new Function(`${fnSource}; return canvasZoom;`)();
      const element = document.querySelector(sel);
      const rect = element.getBoundingClientRect();
      const zoom = canvasZoom(element);
      return { onScreen: Math.round(rect.width), zoom: Number(zoom.toFixed(3)), measured: Math.round(rect.width / zoom) };
    },
    [canvasZoomSource, selector],
  );
}

test("a 40px button in a canvas zoomed to 39% is measured at its own 40px, not at 16px", async () => {
  const result = await measure(FLOW_CANVAS, ".studio-flow-plus");
  assert.equal(result.onScreen, 16, "the raw box really is 16px on screen — that is what used to be reported");
  assert.equal(result.zoom, 0.39);
  assert.ok(result.measured >= 40, `expected 40px in the canvas's own units, got ${result.measured}`);
});

test("the same button at 1:1 needs no allowance", async () => {
  const result = await measure(FLOW_CANVAS_UNZOOMED, ".studio-flow-plus");
  assert.equal(result.zoom, 1);
  assert.ok(result.measured >= 40);
});

test("a genuinely tiny control in ordinary page flow is still measured as tiny", async () => {
  const result = await measure(BARE_TINY_BUTTON, "button");
  assert.equal(result.zoom, 1);
  assert.equal(result.measured, 20, "a bare 20px icon button stays a 20px failure");
});

test("a control shrunk by a transform with nothing to pan is not treated as zoomed", async () => {
  const result = await measure(SHRUNK_TOOLBAR, "button");
  assert.equal(result.zoom, 1, "only a pannable surface earns the allowance");
  assert.equal(result.measured, 16);
});
