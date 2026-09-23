import test from "node:test";
import assert from "node:assert/strict";
import { gunzipSync } from "node:zlib";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { FLICKER_DEFAULTS, detectFlicker, frameDistance, grayFrameFromRgba } from "./flicker-detector.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../..");
const W = 40;
const H = 80;

/** A frame whose pixels are `fill(x, y)`. */
function frame(t, fill) {
  const gray = new Uint8Array(W * H);
  for (let y = 0; y < H; y += 1) for (let x = 0; x < W; x += 1) gray[y * W + x] = fill(x, y);
  return { t, width: W, height: H, gray, step: 8 };
}
// Deliberately NOT a repeating pattern. A periodic picture scrolls back onto itself,
// which really is "changed and changed back" — the detector is right to say so, and a
// test built on one proves nothing about scrolling. A fixed pseudo-random field, long
// enough that nothing in these tests ever wraps round to a picture it has already had.
const FIELD = (() => {
  const field = new Uint8Array(600 * W);
  let seed = 12345;
  for (let i = 0; i < field.length; i += 1) {
    seed = (Math.imul(seed, 1103515245) + 12345) >>> 0;
    field[i] = (seed >>> 16) & 255;
  }
  return field;
})();
const CLEAN = (x, y) => FIELD[(y % 600) * W + x];
/** The same picture with a second layer painted over most of it — what the recording shows. */
const DOUBLE = (x, y) => (y > 4 ? (CLEAN(x, y) + 120) % 255 : CLEAN(x, y));
/** The same content scrolled up by `n` rows — new rows keep arriving, none repeat. */
const shifted = (n) => (x, y) => FIELD[((y + n) % 600) * W + x];

// ---------------------------------------------------------------------------
// The measure
// ---------------------------------------------------------------------------
test("frameDistance reports what fraction changed and where", () => {
  const a = frame(0, () => 0);
  const b = frame(0, (x, y) => (y < 8 ? 255 : 0));
  const d = frameDistance(a, b);
  assert.equal(d.ratio, 8 / H);
  assert.deepEqual(d.box, { x: 0, y: 0, width: W, height: 8 });
});

test("frames of different sizes are a programming error, not a finding", () => {
  const a = frame(0, () => 0);
  const b = { t: 0, width: 4, height: 4, gray: new Uint8Array(16) };
  assert.throws(() => frameDistance(a, b), /same size/);
});

test("noise below the per-pixel threshold is not change", () => {
  const a = frame(0, () => 100);
  const b = frame(0, () => 100 + FLICKER_DEFAULTS.pixelDelta - 1);
  assert.equal(frameDistance(a, b).ratio, 0);
});

// ---------------------------------------------------------------------------
// What it must catch
// ---------------------------------------------------------------------------
test("catches a picture that changes and changes straight back, over and over", () => {
  // Three excursions a second apart, each two frames long: the recording's shape.
  const frames = [];
  let t = 0;
  for (let event = 0; event < 3; event += 1) {
    for (let i = 0; i < 20; i += 1) frames.push(frame((t += 1 / 60), CLEAN));
    frames.push(frame((t += 1 / 60), DOUBLE));
    frames.push(frame((t += 1 / 60), DOUBLE));
    for (let i = 0; i < 20; i += 1) frames.push(frame((t += 1 / 60), CLEAN));
  }
  const result = detectFlicker(frames);
  assert.equal(result.flicker, true);
  assert.equal(result.events.length, 3);
  assert.match(result.reason, /changed and changed straight back/);
  assert.ok(result.extent.coverage > 80, `a near-whole-screen repaint, got ${result.extent.coverage}%`);
});

test("a single-frame flicker is caught, which is the whole reason for filming", () => {
  const frames = [];
  let t = 0;
  for (let event = 0; event < 4; event += 1) {
    for (let i = 0; i < 10; i += 1) frames.push(frame((t += 1 / 60), CLEAN));
    frames.push(frame((t += 1 / 60), DOUBLE));
  }
  // The capture has to keep watching after the last excursion: an excursion only
  // counts once the picture has come back and STAYED back, and a capture that stops
  // the instant it returns cannot show that.
  for (let i = 0; i < 20; i += 1) frames.push(frame((t += 1 / 60), CLEAN));
  assert.equal(detectFlicker(frames).events.length, 4);
});

// ---------------------------------------------------------------------------
// What it must NOT catch
// ---------------------------------------------------------------------------
test("scrolling is not flicker", () => {
  // Every frame differs from the last, and the page never comes back.
  const frames = [];
  for (let i = 0; i < 60; i += 1) frames.push(frame(i / 60, shifted(i * 3)));
  const result = detectFlicker(frames);
  assert.equal(result.flicker, false);
  assert.ok(result.settledChanges > 0, "the movement is counted, just not as flicker");
});

test("scrolling down and back up is not flicker, including at the turn", () => {
  // The moment a person stops scrolling down and starts scrolling up, the picture
  // really does go one step further and come straight back, abruptly. That is the
  // one shape that looks exactly like flicker and is not, and it is why an excursion
  // only counts when the page was still either side of it.
  const frames = [];
  let t = 0;
  for (let i = 0; i < 40; i += 1) frames.push(frame((t += 1 / 60), shifted(i * 3)));
  for (let i = 40; i >= 0; i -= 1) frames.push(frame((t += 1 / 60), shifted(i * 3)));
  const result = detectFlicker(frames);
  assert.equal(result.flicker, false);
  assert.equal(result.events.length, 0, "nothing that happened while the page was moving is a finding");
  assert.ok(result.duringMotion > 0, "the turn is seen and counted, it is simply not reported as flicker");
});

test("a smooth animation that returns is not flicker", () => {
  // A 20-frame fade out and back: it DOES return, so only the abruptness test can
  // tell it apart from flicker. No single frame carries much of the change.
  const frames = [];
  let t = 0;
  for (let event = 0; event < 4; event += 1) {
    for (let i = 0; i < 10; i += 1) frames.push(frame((t += 1 / 240), CLEAN));
    for (let i = 1; i <= 10; i += 1) frames.push(frame((t += 1 / 240), (x, y) => (y > 4 && y % 40 < i * 4 ? (CLEAN(x, y) + 120) % 255 : CLEAN(x, y))));
    for (let i = 9; i >= 0; i -= 1) frames.push(frame((t += 1 / 240), (x, y) => (y > 4 && y % 40 < i * 4 ? (CLEAN(x, y) + 120) % 255 : CLEAN(x, y))));
  }
  const result = detectFlicker(frames);
  assert.equal(result.flicker, false, "an eased animation must not be reported as flicker");
  assert.ok(result.easedReturns > 0, "it is counted as an animation, not ignored");
});

test("one blip is a repaint, not flicker", () => {
  const frames = [];
  let t = 0;
  for (let i = 0; i < 20; i += 1) frames.push(frame((t += 1 / 60), CLEAN));
  frames.push(frame((t += 1 / 60), DOUBLE));
  for (let i = 0; i < 20; i += 1) frames.push(frame((t += 1 / 60), CLEAN));
  const result = detectFlicker(frames);
  assert.equal(result.flicker, false);
  assert.equal(result.events.length, 1);
  assert.match(result.reason, /not enough to call it flicker/);
});

test("a still page is silent, and too few frames says so honestly", () => {
  const still = detectFlicker(Array.from({ length: 30 }, (_, i) => frame(i / 60, CLEAN)));
  assert.equal(still.flicker, false);
  assert.match(still.reason, /never changed and changed straight back/);
  assert.match(detectFlicker([frame(0, CLEAN)]).reason, /not enough frames/);
});

// ---------------------------------------------------------------------------
// Ground truth: the bug, as filmed on Ravi's phone
// ---------------------------------------------------------------------------
// If the detector cannot find the flicker in the recording OF the flicker, it is
// worth nothing. The fixture is the recording's frames, downsampled to the exact
// form the detector consumes (1/8 scale, grayscale) so the ground truth is
// permanent without a recording of production data living in the repo.
test("finds the flicker in the phone recording that started this lane", () => {
  const raw = JSON.parse(gunzipSync(readFileSync(path.join(repo, "tools/dashboard-automation/testdata/phone-flicker-groundtruth.json.gz"))).toString("utf8"));
  const bytes = Buffer.from(raw.gray, "base64");
  const size = raw.width * raw.height;
  const frames = raw.timestamps.map((t, i) => ({
    t, width: raw.width, height: raw.height, step: raw.step,
    gray: new Uint8Array(bytes.subarray(i * size, (i + 1) * size)),
  }));
  assert.equal(frames.length, 275, "the whole recording");

  const result = detectFlicker(frames);
  assert.equal(result.flicker, true, "the detector must see the bug it was built from");
  assert.equal(result.events.length, 6);

  // It recurs about once a second, which is what Ravi described.
  assert.ok(result.cadenceSeconds > 0.8 && result.cadenceSeconds < 1.2, `about once a second, got ${result.cadenceSeconds}`);

  // Each event is brief, and each one jumps rather than eases.
  for (const event of result.events) {
    assert.ok(event.seconds <= 0.5, `an excursion is brief, got ${event.seconds}s`);
    assert.ok(event.abruptness >= 0.9, `it jumps in one frame, got ${event.abruptness}`);
  }

  // Near-whole-screen, as a blurred backdrop being redrawn would be — not a local
  // animation. The recording is 576x1280.
  assert.equal(raw.sourceWidth, 576);
  assert.ok(result.extent.coverage > 85, `a near-whole-screen repaint, got ${result.extent.coverage}%`);

  // And the honest other half: the filter panel sliding shut early in the clip is a
  // real state change, counted but never reported as flicker.
  assert.ok(result.settledChanges > 0, "the panel closing is movement the detector saw and did not report");
  assert.ok(!result.events.some((e) => e.startT > 1.4 && e.startT < 1.9),
    "the panel sliding shut must not be reported as flicker");
});

// ---------------------------------------------------------------------------
test("grayFrameFromRgba averages blocks so one thin line cannot dominate", () => {
  const rgba = new Uint8Array(16 * 16 * 4);
  for (let i = 0; i < 16 * 16; i += 1) rgba[i * 4 + 3] = 255;
  for (let x = 0; x < 16; x += 1) { const p = (0 * 16 + x) * 4; rgba[p] = rgba[p + 1] = rgba[p + 2] = 255; }
  const grayFrame = grayFrameFromRgba(rgba, 16, 16, { step: 8 });
  assert.equal(grayFrame.width, 2);
  assert.equal(grayFrame.height, 2);
  assert.equal(grayFrame.gray[0], Math.round(255 / 8), "one white row in eight, averaged");
  assert.equal(grayFrame.gray[2], 0);
});
