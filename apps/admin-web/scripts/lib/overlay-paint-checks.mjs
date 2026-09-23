// "The panel went see-through": an overlay that is opaque by design showing the page
// behind it mid-transition.
//
// ---------------------------------------------------------------------------
// Why this is its own check
// ---------------------------------------------------------------------------
// The generic detector (flicker-detector.mjs) asks a question about the WHOLE screen:
// did the picture change and change straight back, repeatedly? Two things make it miss
// this bug. It insists on repetition before it will speak, because one change-and-back
// over a whole screen is a repaint. And the flash is a small fraction of the screen, so
// it is under the threshold that a whole-screen measure has to use.
//
// This check fixes both by narrowing the question. It asks the browser which elements
// are OPAQUE BY DESIGN — a computed background colour with no alpha — and then watches
// only inside those. That changes what the evidence means: an element the stylesheet
// declares solid can never legitimately show what is behind it, so ONE flash is already
// a defect and no "is this just an animation?" judgement is needed.
//
// Reproduced on /tasks at 390px: change the assignee filter while the filter panel is
// open and, for roughly 0.15-0.3s, the page header, the New task button and the task
// list are drawn through the panel. Then it snaps back. Once per change.
//
// It generalises, which is the point of writing it this way: any drawer, sheet, dialog
// or panel that computes an opaque background and briefly shows what is behind it is
// the same defect, and this check does not know the first one was a filter panel.
//
// ---------------------------------------------------------------------------
// The measure, and the one this replaced
// ---------------------------------------------------------------------------
// It measures how much of the OVERLAY S INTERIOR changed from one frame to the next,
// and whether it changed straight back — the same excursion-and-return test the generic
// detector uses, narrowed to the panel.
//
// The first version of this file measured something else: the share of pixels inside
// the panel still matching the panel s own background colour, on the theory that the
// background is replaced by the page behind it. That theory was wrong and the check was
// blind. Held against frames that are KNOWN to contain the defect, the share moved from
// 0.91 to 0.86 — nothing a threshold could catch — because the panel s background is
// still dark behind the bleed-through; what leaks through is the page s TEXT and
// buttons, a few per cent of the pixels. The same frames, measured as an excursion in
// the interior, give six events, one per filter change. The lesson is worth keeping:
// a measure that passes its unit tests can still be measuring the wrong thing, and the
// only thing that catches that is holding it against frames known to contain the bug.

import { detectFlicker, grayFrameFromRgba } from "./flicker-detector.mjs";

export const OVERLAY_DEFAULTS = Object.freeze({
  // Pull in from the edges: borders, rounded corners and shadows are not the panel.
  inset: 6,
  // The overlay interior is downsampled before it is compared, which is also the
  // low-pass filter that stops text antialiasing reading as change.
  downsampleStep: 4,
  // How much of the panel has to be involved. The measured events on real frames were
  // 3% to 16% of the panel; below this it is a checkbox redrawing, not the page
  // showing through.
  minPeak: 0.02,
  // ONE is deliberate, and it is the whole reason this check is separate from the
  // whole-screen one. The browser told us this element is opaque, so a single frame
  // where the page shows through it is already wrong. Repetition is evidence the
  // whole-screen detector needs because it cannot tell a defect from an animation;
  // here the stylesheet has already settled that question.
  minEvents: 1,
});


/**
 * Cut the overlay s interior out of one captured frame, as a gray frame the excursion
 * detector can compare.
 *
 * `rect` is in CSS pixels; `scale` converts to the captured frame s pixels, because a
 * screencast frame is not necessarily the viewport s size.
 */
export function interiorFrame(rgba, width, height, rect, options = {}) {
  const o = { ...OVERLAY_DEFAULTS, ...options };
  const scale = o.scale ?? 1;
  const left = Math.max(0, Math.round((rect.x + o.inset) * scale));
  const top = Math.max(0, Math.round((rect.y + o.inset) * scale));
  const right = Math.min(width, Math.round((rect.x + rect.width - o.inset) * scale));
  const bottom = Math.min(height, Math.round((rect.y + rect.height - o.inset) * scale));
  if (right - left < o.downsampleStep || bottom - top < o.downsampleStep) return null;
  const cropW = right - left;
  const cropH = bottom - top;
  const crop = new Uint8Array(cropW * cropH * 4);
  for (let y = 0; y < cropH; y += 1) {
    const from = ((top + y) * width + left) * 4;
    crop.set(rgba.subarray(from, from + cropW * 4), y * cropW * 4);
  }
  return grayFrameFromRgba(crop, cropW, cropH, { t: o.t ?? 0, step: o.downsampleStep });
}

/**
 * Did this overlay ever show what is behind it?
 *
 * @param frames interior gray frames in time order (nulls where unmeasurable)
 * @returns {{ showedThrough, judged, events, reason }}
 */
export function detectShowThrough(frames, options = {}) {
  const o = { ...OVERLAY_DEFAULTS, ...options };
  const usable = frames.filter(Boolean);
  if (usable.length < 4) {
    return { showedThrough: false, judged: false, events: [], reason: "not enough of the overlay was captured to judge it" };
  }
  const result = detectFlicker(usable, { ...o, minEvents: o.minEvents });
  // A change too small to be the page showing through is a control redrawing itself.
  const events = result.events.filter((event) => event.peak >= o.minPeak);
  return {
    showedThrough: events.length > 0,
    judged: true,
    events,
    // Kept so a report can say the panel moved and none of it was a defect.
    settledChanges: result.settledChanges,
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
