// "The panel went see-through": an overlay that is opaque by design showing the page
// behind it mid-transition.
//
// ---------------------------------------------------------------------------
// Why this is its own check and not more of the generic flicker detector
// ---------------------------------------------------------------------------
// The generic detector (flicker-detector.mjs) asks a question about a whole screen:
// did the picture change and change straight back, repeatedly? That is the right
// question for a shimmer, and it needs REPETITION before it will say anything,
// because one change-and-back is a repaint.
//
// The bug this file is for does not need repetition to be certain, and the certainty
// comes from somewhere much better than pixel statistics: the browser will tell you
// the panel's background is `rgb(22,31,26)` with **no alpha**. An element that is
// opaque by design can never legitimately show what is behind it. So one frame where
// the page is legible through it is already a defect, and no amount of "is this just
// an animation?" reasoning is needed.
//
// Reproduced on /tasks at 390px: change the assignee filter while the filter panel is
// open and, for roughly 0.15-0.3s, the panel's own background is not painted. The
// task list and page header show through the 42%-opaque dimmer behind it, and the two
// sets of text sit on top of each other. Then it snaps back.
//
// It generalises, which is the point of writing it this way: any drawer, sheet, dialog
// or panel that computes an opaque background and briefly stops painting it is the
// same defect, and this check does not know or care that the first one was a filter
// panel.
//
// ---------------------------------------------------------------------------
// The measure
// ---------------------------------------------------------------------------
// For each opaque overlay, in each captured frame, the fraction of pixels inside the
// overlay that are still its own background colour. When the panel is painted this
// sits at whatever its text and controls leave (typically most of it). When the
// background is missing, the page behind replaces it and the fraction collapses. A
// finding is a collapse that RECOVERS — because a collapse that does not recover is
// simply the overlay closing, which is what it is supposed to do.

export const OVERLAY_DEFAULTS = Object.freeze({
  // How close a pixel must be to the overlay's own background colour to count as it.
  // Wide enough for JPEG/PNG rounding and subpixel edges, far below the distance to
  // any real page content.
  colourTolerance: 12,
  // The overlay has to actually be mostly its own colour when settled, or "share of
  // background pixels" is not measuring anything. A panel that is 90% photograph is
  // not something this check can judge, and it says so rather than guessing.
  minSettledShare: 0.3,
  // A collapse to below this fraction of the settled share is the background missing,
  // not content changing inside the panel.
  dropFraction: 0.6,
  // ...and it has to come back to at least this much of settled to be a flicker
  // rather than the overlay closing.
  recoverFraction: 0.85,
  // Longer than this and it is a state the person can see and act on, not a flash.
  maxSeconds: 1.5,
  // How long the background has to be missing before it is a defect rather than a
  // decoding artefact. This is a DURATION and not a frame count, and that distinction
  // is not academic: a CDP screencast emits a frame only when the compositor produces
  // one, so a 200ms flash on an otherwise still page is exactly ONE frame. Counting
  // frames rejected the real bug the first time this ran; counting milliseconds finds
  // it from either kind of capture.
  minSeconds: 0.05,
});

/**
 * Fraction of pixels inside `rect` that are within `tolerance` of `rgb`.
 *
 * `rect` is in CSS pixels; `scale` converts to the captured frame's pixels, because a
 * screencast frame is not necessarily the viewport's size.
 */
export function opaqueShare(rgba, width, height, rect, rgb, options = {}) {
  const tolerance = options.colourTolerance ?? OVERLAY_DEFAULTS.colourTolerance;
  const scale = options.scale ?? 1;
  // Pull in from the edges: borders, rounded corners and shadows are not background.
  const inset = options.inset ?? 6;
  const left = Math.max(0, Math.round((rect.x + inset) * scale));
  const top = Math.max(0, Math.round((rect.y + inset) * scale));
  const right = Math.min(width, Math.round((rect.x + rect.width - inset) * scale));
  const bottom = Math.min(height, Math.round((rect.y + rect.height - inset) * scale));
  if (right <= left || bottom <= top) return null;
  const [wantR, wantG, wantB] = rgb;
  let matched = 0;
  let total = 0;
  // Sampling every other pixel is 4x cheaper and changes the fraction by nothing that
  // matters at these thresholds.
  for (let y = top; y < bottom; y += 2) {
    for (let x = left; x < right; x += 2) {
      const p = (y * width + x) * 4;
      total += 1;
      if (
        Math.abs(rgba[p] - wantR) <= tolerance &&
        Math.abs(rgba[p + 1] - wantG) <= tolerance &&
        Math.abs(rgba[p + 2] - wantB) <= tolerance
      ) {
        matched += 1;
      }
    }
  }
  return total ? matched / total : null;
}

const round = (value, places = 3) => Number(value.toFixed(places));

/**
 * Find the moments an opaque overlay stopped painting its own background.
 *
 * @param series [{ t, share }] in time order, share possibly null where unmeasurable
 * @returns {{ showedThrough, settledShare, judged, events, reason }}
 */
export function detectShowThrough(series, options = {}) {
  const o = { ...OVERLAY_DEFAULTS, ...options };
  const usable = series.filter((s) => typeof s.share === "number");
  if (usable.length < 4) {
    return { showedThrough: false, settledShare: null, judged: false, events: [], reason: "not enough of the overlay was captured to judge it" };
  }

  // Settled = what the overlay looks like when it IS painting itself. The high end of
  // the distribution, not the mean: the mean is dragged down by the very frames we are
  // looking for, which would hide them.
  const sorted = [...usable].map((s) => s.share).sort((a, b) => b - a);
  const settled = sorted[Math.floor(sorted.length * 0.1)];
  if (settled < o.minSettledShare) {
    return {
      showedThrough: false,
      settledShare: round(settled, 3),
      judged: false,
      reason: "this overlay is not mostly one flat colour even when it is painted, so this check cannot judge it",
      events: [],
    };
  }

  const floor = settled * o.dropFraction;
  const ceiling = settled * o.recoverFraction;
  const events = [];
  let run = null;
  for (const sample of usable) {
    if (sample.share < floor) {
      if (!run) run = { startT: sample.t, endT: sample.t, frames: 0, lowest: sample.share };
      run.endT = sample.t;
      run.frames += 1;
      run.lowest = Math.min(run.lowest, sample.share);
    } else if (run) {
      // It came back. Only then is it a flash rather than the overlay closing.
      const seconds = sample.t - run.startT;
      if (sample.share >= ceiling && seconds >= o.minSeconds && seconds <= o.maxSeconds) {
        events.push({
          startT: round(run.startT),
          endT: round(sample.t),
          seconds: round(seconds),
          frames: run.frames,
          lowest: round(run.lowest, 3),
          settled: round(settled, 3),
        });
      }
      run = null;
    }
  }
  // A run still open at the end of the capture never came back, so it is the overlay
  // closing or the capture stopping, and it is not reported.
  return {
    showedThrough: events.length > 0,
    settledShare: round(settled, 3),
    judged: true,
    events,
    reason: events.length
      ? "the page behind it was visible through an overlay that is supposed to be solid"
      : "the overlay stayed solid the whole time",
  };
}

/**
 * Runs inside the browser. Finds every overlay that is OPAQUE BY DESIGN — a computed
 * background colour with no alpha — sitting above the page on its own stacking level.
 *
 * Opacity is the whole basis of the check: an element the stylesheet declares solid can
 * never legitimately show what is behind it, so a single frame where it does is a
 * defect rather than a judgement call. Anything translucent is skipped, because for
 * those "you can see through it" is the design.
 */
export function collectOpaqueOverlays({ minSide = 140 } = {}) {
  const overlays = [];
  const text = (el) => (el?.textContent ?? "").replace(/\s+/g, " ").trim();
  for (const el of document.body.querySelectorAll("*")) {
    const style = getComputedStyle(el);
    if (style.position !== "fixed" && style.position !== "absolute") continue;
    if (style.display === "none" || style.visibility === "hidden" || Number(style.opacity) < 1) continue;
    const z = Number.parseInt(style.zIndex, 10);
    if (!Number.isFinite(z) || z < 1) continue;
    const match = String(style.backgroundColor).match(/rgba?\(([^)]+)\)/);
    if (!match) continue;
    const parts = match[1].split(",").map((piece) => Number(piece.trim()));
    if (parts.length > 3 && parts[3] < 1) continue; // translucent by design: not ours
    if (parts.length < 3) continue;
    const rect = el.getBoundingClientRect();
    if (rect.width < minSide || rect.height < minSide) continue;
    if (rect.bottom <= 0 || rect.top >= innerHeight || rect.right <= 0 || rect.left >= innerWidth) continue;
    // It has to be the thing on top at its own centre, or it is not an overlay.
    const cx = Math.min(Math.max(rect.left + rect.width / 2, 1), innerWidth - 1);
    const cy = Math.min(Math.max(rect.top + rect.height / 2, 1), innerHeight - 1);
    const hit = document.elementFromPoint(cx, cy);
    if (!hit || !(hit === el || el.contains(hit))) continue;
    // A name a person would recognise, taken from the overlay's own heading or label.
    const heading = el.querySelector("h1,h2,h3,h4,legend,[class*='-hd'],[class*='head']");
    const label =
      el.getAttribute("aria-label") ||
      (heading && text(heading).length <= 40 ? text(heading) : "") ||
      "";
    overlays.push({
      label,
      z,
      rgb: [parts[0], parts[1], parts[2]],
      rect: { x: rect.left, y: rect.top, width: rect.width, height: rect.height },
    });
  }
  // Deepest stacking level first: the panel, not the thing behind it.
  overlays.sort((a, b) => b.z - a.z);
  return overlays.slice(0, 4);
}
