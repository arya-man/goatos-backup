// Temporal flicker detection: the symptom half of the mobile-webview flicker lane.
//
// Why this exists. Every other check in this directory looks at ONE settled
// screenshot of a route. Flicker does not exist in one screenshot — it only exists
// across frames in time, so no amount of per-route screenshotting can ever see it.
// This module is the part that looks at a sequence of frames.
//
// ---------------------------------------------------------------------------
// The signature, measured rather than invented
// ---------------------------------------------------------------------------
// Taken from a real phone recording of the Tasks page (576x1280, 11.5s, filter
// panel open). What the frames actually show is the filter panel and the page
// underneath painted ON TOP OF EACH OTHER for a fifth of a second, then snapping
// back to the correct picture. Six times in the clip, roughly once a second.
//
// So the thing to detect is an EXCURSION THAT RETURNS:
//
//   • the picture changes materially, and
//   • within a fraction of a second it comes back to what it was, and
//   • it got there abruptly — one frame to the next, not eased, and
//   • once back, it STAYS back, and
//   • it happens again, and again.
//
// The four things it must NOT call flicker, and why each is excluded:
//
//   • Scrolling. Every frame differs from the next, but the page does not come
//     back to where it started, so there is no excursion to report.
//   • The moment a scroll REVERSES. Here the picture genuinely does go one step
//     further and come straight back, abruptly — the one shape that looks exactly
//     like flicker and is not. It carries straight on moving afterwards, where a
//     flicker settles, and that is what `restSeconds` separates.
//   • A legitimate transition (the filter panel sliding shut at 1.47-1.80s in the
//     same recording). It changes and STAYS changed: no return, so no finding.
//     The detector counts these separately and the recording's own panel-close is
//     correctly among them.
//   • A legitimate animation — a pulse, a fade, a spinner. It does return, but it
//     eases: no single frame carries most of the change. `abruptFraction` is what
//     separates "it jumped" from "it moved".
//
// The limit this leaves, stated rather than hidden: tearing DURING a steady scroll
// cannot be found by a return test at all, because the scroll carries the page
// onward and it never comes back to the frame it started from. What this detector
// finds is flicker on a page that is otherwise sitting still — which is exactly
// what the recording shows, and what a person notices.
//
// ---------------------------------------------------------------------------
// Zero dependencies, on purpose: the detector is pure, so its tests need no
// browser, no node_modules and no video. `flicker-capture.mjs` feeds it frames
// from a live page over CDP; `check-mobile-flicker.mjs` feeds it frames from a
// recording.

/** A frame is { t: seconds, width, height, gray: Uint8Array of width*height }. */

export const FLICKER_DEFAULTS = Object.freeze({
  // Per-pixel luminance step that counts as "this pixel changed". Below this is
  // video/JPEG noise and sub-pixel text antialiasing.
  pixelDelta: 12,
  // Fraction of the frame that must change for a step to be material at all.
  // 1% of a 390x844 phone viewport is ~3300 pixels — far more than a caret blink.
  changeRatio: 0.01,
  // How much of the change has to come back for it to count as a return. 0.25 =
  // at least three quarters of what changed changed back. It is a FRACTION of the
  // excursion, not an absolute, because real state can change underneath a
  // flicker — in the recording a filter checkbox genuinely changed during one of
  // the six events, leaving a residual that an absolute threshold would fail on.
  returnFraction: 0.25,
  // Longer than this and it is a state change a person meant, not a flicker.
  // This is also what keeps "scroll down and scroll back" out of the findings.
  maxExcursionSeconds: 0.5,
  // At least one single frame-to-frame step must carry this much of the whole
  // excursion. Flicker jumps; an eased animation spread over N frames cannot.
  abruptFraction: 0.4,
  // Once is a repaint. Repeatedly is flicker.
  minEvents: 3,
  // After the picture comes back, it has to STAY back for this long. This is the one
  // condition that separates flicker from the moment a scroll reverses direction —
  // which is the only other thing that goes one step further and comes straight back,
  // abruptly, and is not flicker. At a scroll turn the page carries straight on
  // moving; after a flicker it settles.
  //
  // It is deliberately a one-sided test. The symmetric "and it was still BEFORE"
  // looks obvious and is wrong: when a page flickers repeatedly, the frame just
  // before one flicker is the previous flicker, so every event would rule out its
  // neighbour and a page flickering once a second would be filed as movement. That
  // is not hypothetical — it is what happened the first time this ran against a page
  // built to flicker.
  restSeconds: 0.15,
});

/**
 * Compare two gray frames. Returns the fraction of pixels that moved by more than
 * `pixelDelta`, and the bounding box of those pixels in frame coordinates.
 */
export function frameDistance(a, b, pixelDelta = FLICKER_DEFAULTS.pixelDelta) {
  if (a.width !== b.width || a.height !== b.height) {
    throw new Error("flicker detector: frames must all be the same size");
  }
  const { width, height } = a;
  let changed = 0;
  let minX = width;
  let minY = height;
  let maxX = -1;
  let maxY = -1;
  for (let y = 0; y < height; y += 1) {
    const row = y * width;
    for (let x = 0; x < width; x += 1) {
      const i = row + x;
      const delta = a.gray[i] - b.gray[i];
      if (delta > pixelDelta || delta < -pixelDelta) {
        changed += 1;
        if (x < minX) minX = x;
        if (x > maxX) maxX = x;
        if (y < minY) minY = y;
        if (y > maxY) maxY = y;
      }
    }
  }
  return {
    ratio: changed / (width * height),
    changed,
    box: maxX < 0 ? null : { x: minX, y: minY, width: maxX - minX + 1, height: maxY - minY + 1 },
  };
}

function unionBox(boxes) {
  const present = boxes.filter(Boolean);
  if (!present.length) return null;
  const left = Math.min(...present.map((b) => b.x));
  const top = Math.min(...present.map((b) => b.y));
  const right = Math.max(...present.map((b) => b.x + b.width));
  const bottom = Math.max(...present.map((b) => b.y + b.height));
  return { x: left, y: top, width: right - left, height: bottom - top };
}

/** Scale a box found in a downsampled frame back to the pixels a person saw. */
export function boxInSourcePixels(box, frame) {
  if (!box) return null;
  const step = frame?.step ?? 1;
  return { x: box.x * step, y: box.y * step, width: box.width * step, height: box.height * step };
}

const round = (value, places = 3) => Number(value.toFixed(places));

/**
 * The detector.
 *
 * @param frames  ordered gray frames, all the same size
 * @param options overrides for FLICKER_DEFAULTS
 * @returns {{
 *   flicker: boolean,
 *   frames: number,
 *   spanSeconds: number,
 *   events: Array<object>,
 *   easedReturns: number,
 *   settledChanges: number,
 *   duringMotion: number,
 *   extent: object|null,
 *   cadenceSeconds: number|null,
 *   reason: string
 * }}
 */
export function detectFlicker(frames, options = {}) {
  const o = { ...FLICKER_DEFAULTS, ...options };
  const spanSeconds = frames.length > 1 ? round(frames[frames.length - 1].t - frames[0].t) : 0;
  const base = {
    flicker: false,
    frames: frames.length,
    spanSeconds,
    events: [],
    easedReturns: 0,
    settledChanges: 0,
    duringMotion: 0,
    extent: null,
    cadenceSeconds: null,
    reason: "",
  };
  if (frames.length < 3) return { ...base, reason: "not enough frames were captured to tell" };

  const events = [];
  let easedReturns = 0; // changed and came back, but smoothly: an animation.
  let settledChanges = 0; // changed and stayed changed: scrolling, or a real transition.
  let duringMotion = 0; // came back, but the page was already moving — see below.

  let i = 0;
  while (i < frames.length - 1) {
    const first = frameDistance(frames[i], frames[i + 1], o.pixelDelta);
    if (first.ratio < o.changeRatio) {
      i += 1;
      continue;
    }
    // Something started moving at frame i. Does it come back, and how fast did it go?
    let peak = first.ratio;
    let peakBox = first.box;
    let maxStep = first.ratio;
    let returned = null;
    for (let j = i + 2; j < frames.length; j += 1) {
      // Measured from the first CHANGED frame, not from the last settled one. With a
      // live screencast the settled frame before a flicker can be a second earlier,
      // because the compositor produces nothing while the page sits still — timing
      // the excursion from there would time the silence instead of the flicker.
      if (frames[j].t - frames[i + 1].t > o.maxExcursionSeconds) break;
      const step = frameDistance(frames[j - 1], frames[j], o.pixelDelta).ratio;
      if (step > maxStep) maxStep = step;
      const back = frameDistance(frames[i], frames[j], o.pixelDelta);
      if (back.ratio <= peak * o.returnFraction) {
        returned = { j, residual: back.ratio };
        break;
      }
      if (back.ratio > peak) {
        peak = back.ratio;
        peakBox = back.box;
      }
    }
    if (!returned) {
      settledChanges += 1;
      i += 1;
      continue;
    }
    const abruptness = peak > 0 ? maxStep / peak : 0;
    if (abruptness < o.abruptFraction) {
      easedReturns += 1;
      i = returned.j;
      continue;
    }
    // The picture came back — but did it STAY back? A scroll that reverses direction
    // also goes one step further and comes straight back; it just carries on moving
    // immediately afterwards. A flicker settles. This is the test that tells them
    // apart, and it is the reason a scroll turn is not reported.
    //
    // The limit it leaves, stated rather than papered over: tearing DURING a steady
    // scroll cannot be found by a return test at all, because the scroll carries the
    // page onward and it never comes back to the frame it started from. What this
    // finds is flicker on a page that is otherwise sitting still — which is what the
    // recording shows and what a person notices. These are counted so the report can
    // say the page moved and none of the movement was flicker.
    const observedAfter = frames[frames.length - 1].t - frames[returned.j].t;
    let held = observedAfter >= o.restSeconds;
    for (let k = returned.j + 1; held && k < frames.length; k += 1) {
      if (frames[k].t - frames[returned.j].t >= o.restSeconds) break;
      if (frameDistance(frames[i], frames[k], o.pixelDelta).ratio >= o.changeRatio) held = false;
    }
    if (!held) {
      duringMotion += 1;
      i = returned.j;
      continue;
    }
    events.push({
      startT: round(frames[i].t),
      endT: round(frames[returned.j].t),
      seconds: round(frames[returned.j].t - frames[i].t),
      frames: returned.j - i - 1,
      peak: round(peak, 4),
      residual: round(returned.residual, 4),
      abruptness: round(abruptness, 2),
      box: peakBox,
      sourceBox: boxInSourcePixels(peakBox, frames[i]),
    });
    i = returned.j;
  }

  if (events.length < o.minEvents) {
    return {
      ...base,
      events,
      easedReturns,
      settledChanges,
      duringMotion,
      reason: events.length
        ? "the picture changed and changed straight back once or twice, which is not enough to call it flicker"
        : "the picture never changed and changed straight back",
    };
  }

  const box = unionBox(events.map((e) => e.box));
  const { width, height } = frames[0];
  const step = frames[0].step ?? 1;
  const gaps = events.slice(1).map((e, n) => e.startT - events[n].startT);
  const extent = box
    ? {
        width: box.width * step,
        height: box.height * step,
        coverage: round(((box.width * box.height) / (width * height)) * 100, 1),
      }
    : null;

  return {
    flicker: true,
    frames: frames.length,
    spanSeconds,
    events,
    easedReturns,
    settledChanges,
    duringMotion,
    extent,
    cadenceSeconds: gaps.length ? round(gaps.reduce((a, b) => a + b, 0) / gaps.length, 2) : null,
    reason: "the picture changed and changed straight back, over and over",
  };
}

/**
 * Turn RGBA pixels into the gray frame the detector wants, averaging each
 * `step` x `step` block. Downsampling is not an optimisation detail: it is also
 * the low-pass filter that stops text antialiasing and compression noise from
 * reading as change.
 */
export function grayFrameFromRgba(rgba, width, height, { t = 0, step = 8 } = {}) {
  const outW = Math.max(1, Math.floor(width / step));
  const outH = Math.max(1, Math.floor(height / step));
  const gray = new Uint8Array(outW * outH);
  for (let y = 0; y < outH; y += 1) {
    for (let x = 0; x < outW; x += 1) {
      let sum = 0;
      let n = 0;
      for (let dy = 0; dy < step; dy += 1) {
        const sy = y * step + dy;
        if (sy >= height) break;
        for (let dx = 0; dx < step; dx += 1) {
          const sx = x * step + dx;
          if (sx >= width) break;
          const p = (sy * width + sx) * 4;
          sum += (rgba[p] * 299 + rgba[p + 1] * 587 + rgba[p + 2] * 114) / 1000;
          n += 1;
        }
      }
      gray[y * outW + x] = n ? Math.round(sum / n) : 0;
    }
  }
  return { t, width: outW, height: outH, gray, sourceWidth: width, sourceHeight: height, step };
}

/**
 * Plain English for Slack and for the receipt. No frame numbers, no timestamps,
 * no thresholds: a person reads this, and the numbers live in the HTML report.
 */
export function flickerSentence({ pageName, deviceName, whileDoing }) {
  const where = whileDoing ? ` while you ${whileDoing}` : "";
  return `${pageName} flickers${where} on ${deviceName}.`;
}
