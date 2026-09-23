import test from "node:test";
import assert from "node:assert/strict";
import { gunzipSync } from "node:zlib";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { OVERLAY_DEFAULTS, collectOpaqueOverlays, detectShowThrough, interiorFrame } from "./overlay-paint-checks.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../..");

// ---------------------------------------------------------------------------
// Cutting the overlay out of a frame
// ---------------------------------------------------------------------------
function canvas(width, height, fill) {
  const rgba = new Uint8Array(width * height * 4);
  for (let y = 0; y < height; y += 1) {
    for (let x = 0; x < width; x += 1) {
      const [r, g, b] = fill(x, y);
      const p = (y * width + x) * 4;
      rgba[p] = r; rgba[p + 1] = g; rgba[p + 2] = b; rgba[p + 3] = 255;
    }
  }
  return rgba;
}

const PANEL = [22, 31, 26]; // the real filter panel's computed background
const PAGE = [180, 220, 195];

test("cuts out only what is inside the overlay", () => {
  // Page above, panel below. The interior must be all panel.
  const rgba = canvas(80, 80, (x, y) => (y >= 40 ? PANEL : PAGE));
  const frame = interiorFrame(rgba, 80, 80, { x: 0, y: 40, width: 80, height: 40 }, { inset: 0, downsampleStep: 4 });
  assert.equal(frame.width, 20);
  assert.equal(frame.height, 10);
  assert.ok(frame.gray.every((v) => Math.abs(v - 28) < 3), "the interior is the panel, not the page above it");
});

test("a screencast frame that is not the viewport's size is scaled, not misread", () => {
  const rgba = canvas(80, 80, (x, y) => (y >= 40 ? PANEL : PAGE));
  // Same panel described in CSS pixels at twice the size, captured at half scale.
  const frame = interiorFrame(rgba, 80, 80, { x: 0, y: 80, width: 160, height: 80 }, { inset: 0, scale: 0.5, downsampleStep: 4 });
  assert.ok(frame.gray.every((v) => Math.abs(v - 28) < 3), "the rect must be scaled into the frame's pixels");
});

test("an overlay that falls outside the frame is unmeasurable, not clean", () => {
  const rgba = canvas(40, 40, () => PANEL);
  assert.equal(interiorFrame(rgba, 40, 40, { x: 200, y: 200, width: 100, height: 100 }), null);
});

// ---------------------------------------------------------------------------
// The finding
// ---------------------------------------------------------------------------
const W = 40;
const H = 60;
function interior(t, fill) {
  const gray = new Uint8Array(W * H);
  for (let y = 0; y < H; y += 1) for (let x = 0; x < W; x += 1) gray[y * W + x] = fill(x, y);
  return { t, width: W, height: H, gray, step: 4 };
}
// A painted panel: flat background with its own rows of text.
const PAINTED = (x, y) => (y % 12 < 2 ? 210 : 30);
// The page showing through: the panel's rows are still there, and page content is
// drawn over part of it. Only a few per cent of the panel moves — which is exactly
// why a "how much is still the background colour" measure could not see this.
const SHOWING_THROUGH = (x, y) => (y > 20 && y < 30 && x > 4 && x < 30 ? 150 : PAINTED(x, y));
// A checkbox ticking: a couple of rows change, and they STAY changed.
const TICKED = (x, y) => (y > 46 && y < 50 && x < 10 ? 150 : PAINTED(x, y));

function series(steps) {
  const frames = [];
  let t = 0;
  for (const [count, fill] of steps) for (let i = 0; i < count; i += 1) frames.push(interior((t += 0.033), fill));
  return frames;
}

test("finds the page showing through a panel that is supposed to be solid", () => {
  const result = detectShowThrough(series([[20, PAINTED], [6, SHOWING_THROUGH], [20, PAINTED]]));
  assert.equal(result.showedThrough, true);
  assert.equal(result.events.length, 1);
  assert.match(result.reason, /supposed to be solid/);
});

test("once is enough, because the browser already said the panel is solid", () => {
  // The whole-screen detector needs three before it will speak. This one does not,
  // and that difference is the point of the check.
  assert.equal(OVERLAY_DEFAULTS.minEvents, 1);
  assert.equal(detectShowThrough(series([[20, PAINTED], [4, SHOWING_THROUGH], [20, PAINTED]])).events.length, 1);
});

test("a checkbox ticking is not the page showing through", () => {
  // It changes and it STAYS changed, which is what a control doing its job looks like.
  const result = detectShowThrough(series([[20, PAINTED], [20, TICKED]]));
  assert.equal(result.showedThrough, false);
  assert.match(result.reason, /stayed solid/);
});

test("a change too small to be the page behind it is not reported", () => {
  const tiny = (x, y) => (y === 5 && x < 3 ? 150 : PAINTED(x, y));
  assert.equal(detectShowThrough(series([[20, PAINTED], [6, tiny], [20, PAINTED]])).showedThrough, false);
});

test("a panel that closes is not a panel that flickered", () => {
  const gone = () => 200;
  const result = detectShowThrough(series([[20, PAINTED], [20, gone]]));
  assert.equal(result.showedThrough, false);
});

test("too little of the overlay captured says so rather than passing", () => {
  const result = detectShowThrough([interior(0, PAINTED), null, interior(0.1, PAINTED)]);
  assert.equal(result.judged, false);
  assert.match(result.reason, /not enough/);
});

// ---------------------------------------------------------------------------
// The in-page probe (exercised for real in flicker-capture.test.mjs)
// ---------------------------------------------------------------------------
test("the probe is self-contained, because it is serialised into the browser", () => {
  const source = collectOpaqueOverlays.toString();
  for (const outsideReference of ["require(", "import ", "OVERLAY_DEFAULTS", "interiorFrame", "detectFlicker"]) {
    assert.ok(!source.includes(outsideReference), `the probe must not reach outside itself: ${outsideReference}`);
  }
});

// ---------------------------------------------------------------------------
// Ground truth: the defect, as reproduced in a browser on production
// ---------------------------------------------------------------------------
// This is the test that matters most, and it is here because the FIRST version of this
// check passed every synthetic test above while being completely blind to the real
// thing. Frames known to contain the defect are the only thing that catches that.
test("finds the page showing through the filter panel in frames known to contain it", () => {
  const raw = JSON.parse(gunzipSync(readFileSync(path.join(repo, "tools/dashboard-automation/testdata/panel-showthrough-groundtruth.json.gz"))).toString("utf8"));
  const bytes = Buffer.from(raw.gray, "base64");
  const size = raw.width * raw.height;
  const frames = raw.timestamps.map((t, i) => ({
    t, width: raw.width, height: raw.height, step: raw.step,
    gray: new Uint8Array(bytes.subarray(i * size, (i + 1) * size)),
  }));
  assert.equal(frames.length, 44, "the whole browser run");

  const result = detectShowThrough(frames);
  assert.equal(result.showedThrough, true, "the check must see the defect it was built from");
  // The assignee filter was changed six times and it happened on every change.
  assert.ok(result.events.length >= 5, `once per filter change, got ${result.events.length}`);
  for (const event of result.events) {
    assert.ok(event.abruptness >= 0.5, `it appears between one frame and the next, got ${event.abruptness}`);
    assert.ok(event.peak >= OVERLAY_DEFAULTS.minPeak, "and it involves a real part of the panel");
  }
  // The measure this replaced moved from 0.91 to 0.86 on these same frames — which is
  // why "share of the panel still its own background colour" is not in this file.
  assert.ok(Math.max(...result.events.map((e) => e.peak)) > 0.1,
    "the worst moment covers a good part of the panel");
});
