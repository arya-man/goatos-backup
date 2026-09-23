import test from "node:test";
import assert from "node:assert/strict";
import { OVERLAY_DEFAULTS, collectOpaqueOverlays, detectShowThrough, opaqueShare } from "./overlay-paint-checks.mjs";

// ---------------------------------------------------------------------------
// The measure
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
const PAGE_THROUGH_SCRIM = [70, 96, 82];

test("a painted panel is almost all its own colour", () => {
  const rgba = canvas(100, 100, () => PANEL);
  const share = opaqueShare(rgba, 100, 100, { x: 0, y: 0, width: 100, height: 100 }, PANEL);
  assert.equal(share, 1);
});

test("a panel that stopped painting its background is almost none of it", () => {
  const rgba = canvas(100, 100, () => PAGE_THROUGH_SCRIM);
  const share = opaqueShare(rgba, 100, 100, { x: 0, y: 0, width: 100, height: 100 }, PANEL);
  assert.equal(share, 0);
});

test("the panel's own text and controls only take a bite out of it", () => {
  // A quarter of the panel covered in its own content, in one block rather than fine
  // stripes: a striped fixture aliases against the every-other-pixel sampling and
  // would be measuring the test, not the check.
  const rgba = canvas(100, 100, (x, y) => (x < 50 && y < 50 ? [230, 240, 235] : PANEL));
  const share = opaqueShare(rgba, 100, 100, { x: 0, y: 0, width: 100, height: 100 }, PANEL);
  assert.ok(share > 0.7 && share < 0.85, `content leaves most of the panel visible, got ${share}`);
});

test("compression wobble is not a change of colour", () => {
  const rgba = canvas(100, 100, () => PANEL.map((c) => c + OVERLAY_DEFAULTS.colourTolerance - 1));
  assert.equal(opaqueShare(rgba, 100, 100, { x: 0, y: 0, width: 100, height: 100 }, PANEL), 1);
});

test("a rect that falls outside the frame is unmeasurable, not zero", () => {
  const rgba = canvas(40, 40, () => PANEL);
  assert.equal(opaqueShare(rgba, 40, 40, { x: 200, y: 200, width: 100, height: 100 }, PANEL), null);
});

test("a screencast frame smaller or larger than the viewport is scaled, not misread", () => {
  // Panel occupies the lower half in CSS pixels; the frame is captured at half size.
  const rgba = canvas(100, 100, (x, y) => (y >= 50 ? PANEL : PAGE_THROUGH_SCRIM));
  const share = opaqueShare(rgba, 100, 100, { x: 0, y: 100, width: 200, height: 100 }, PANEL, { scale: 0.5, inset: 0 });
  assert.equal(share, 1);
});

// ---------------------------------------------------------------------------
// The finding
// ---------------------------------------------------------------------------
const steady = (n, share, from = 0, step = 0.033) =>
  Array.from({ length: n }, (_, i) => ({ t: from + i * step, share }));

test("finds a panel that went see-through and came back", () => {
  const series = [
    ...steady(20, 0.8),
    ...steady(6, 0.05, 0.66),
    ...steady(20, 0.8, 0.86),
  ];
  const result = detectShowThrough(series);
  assert.equal(result.showedThrough, true);
  assert.equal(result.events.length, 1);
  assert.ok(result.events[0].seconds < 0.5, "a flash, not a state");
  assert.match(result.reason, /supposed to be solid/);
});

test("finds it once per filter change, which is how it actually behaves", () => {
  const series = [];
  let t = 0;
  for (let change = 0; change < 4; change += 1) {
    for (let i = 0; i < 25; i += 1) series.push({ t: (t += 0.033), share: 0.8 });
    for (let i = 0; i < 6; i += 1) series.push({ t: (t += 0.033), share: 0.05 });
  }
  for (let i = 0; i < 25; i += 1) series.push({ t: (t += 0.033), share: 0.8 });
  assert.equal(detectShowThrough(series).events.length, 4);
});

test("a panel that closes is not a panel that flickered", () => {
  // It drops and never comes back: that is the overlay doing its job.
  const series = [...steady(20, 0.8), ...steady(20, 0.02, 0.66)];
  const result = detectShowThrough(series);
  assert.equal(result.showedThrough, false);
  assert.match(result.reason, /stayed solid/);
});

test("content changing inside the panel is not the background going missing", () => {
  // Ticking a box repaints some rows; the background is still there.
  const series = [...steady(20, 0.8), ...steady(6, 0.72, 0.66), ...steady(20, 0.8, 0.86)];
  assert.equal(detectShowThrough(series).showedThrough, false);
});

test("a blink too short to be real is rejected, and it is judged in time not frames", () => {
  // A live screencast emits a frame only when the picture changes, so a 200ms flash on
  // a still page is ONE frame. Counting frames threw the real bug away; counting
  // milliseconds keeps it and still rejects a single-sample artefact.
  const oneLongFrame = [...steady(20, 0.8), { t: 0.66, share: 0.05 }, ...steady(20, 0.8, 0.86)];
  assert.equal(detectShowThrough(oneLongFrame).showedThrough, true, "one frame that lasted 200ms is the real bug");
});

test("one frame is not enough to be sure, two is", () => {
  const one = [...steady(20, 0.8), { t: 0.66, share: 0.05 }, ...steady(20, 0.8, 0.7)];
  assert.equal(detectShowThrough(one).showedThrough, false);
  const two = [...steady(20, 0.8), { t: 0.66, share: 0.05 }, { t: 0.69, share: 0.05 }, ...steady(20, 0.8, 0.73)];
  assert.equal(detectShowThrough(two).showedThrough, true);
});

test("an overlay that is not mostly one colour is parked, not guessed at", () => {
  const result = detectShowThrough(steady(40, 0.12));
  assert.equal(result.judged, false);
  assert.equal(result.showedThrough, false);
  assert.match(result.reason, /cannot judge it/);
});

test("too little of the overlay captured says so rather than passing", () => {
  const result = detectShowThrough([{ t: 0, share: 0.8 }, { t: 0.1, share: null }]);
  assert.equal(result.judged, false);
  assert.match(result.reason, /not enough/);
});

test("the settled level is taken from the high end, so the defect cannot hide itself", () => {
  // Half the capture is the defect. A mean would call 0.4 normal and find nothing.
  const series = [...steady(20, 0.8), ...steady(18, 0.05, 0.66)];
  assert.ok(detectShowThrough(series).settledShare > 0.7, "the settled level must be what a painted panel looks like");
});

// ---------------------------------------------------------------------------
// The in-page probe (exercised for real in flicker-capture.test.mjs)
// ---------------------------------------------------------------------------
test("the probe is self-contained, because it is serialised into the browser", () => {
  const source = collectOpaqueOverlays.toString();
  for (const outsideReference of ["require(", "import ", "OVERLAY_DEFAULTS", "opaqueShare"]) {
    assert.ok(!source.includes(outsideReference), `the probe must not reach outside itself: ${outsideReference}`);
  }
});
