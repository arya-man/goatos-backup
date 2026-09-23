// Rules that decide whether a measurement is a break a person can SEE.
//
// Why these live in Node and not in the page: the same reason pen labels do
// (see collectPenLabelIssues in regression-checks.mjs). The browser is good at
// reporting where things are painted; it is a terrible place to keep judgement,
// because judgement has to be unit-tested and a `page.evaluate` body cannot be.
// Every rule here takes plain numbers the page measured and returns either "" /
// false (nothing a person would call broken) or a short human detail.
//
// Each rule exists because the 2026-09-22 production sweep reported something a
// farm manager looked at and said was fine. The comment on each one names the
// case it must stop firing on AND the case it must still catch — both are
// covered in visible-break-rules.test.mjs.

const overlaps1D = (aStart, aEnd, bStart, bEnd) => Math.min(aEnd, bEnd) - Math.max(aStart, bStart);

/**
 * A table cell whose text is wider than the cell.
 *
 * Stop firing on: People / HRMS notifications, where an alert title runs a few
 * px past its column into a tick-box column that is empty on that line. Nothing
 * is covered; the title reads in full.
 * Still catch: text actually painted on top of the next column's words.
 *
 * @param {{rect: {left:number,right:number,top:number,bottom:number}, overflowPx: number,
 *          neighbourTexts: {rect:{left:number,right:number,top:number,bottom:number}, text:string}[]}} cell
 * @returns {string} human detail, or "" when nothing is painted over
 */
export function cellOverpaintDetail(cell) {
  const over = Number(cell?.overflowPx ?? 0);
  const rect = cell?.rect;
  if (!rect || over <= 1) return "";
  // The strip the text spills into: from the cell's right edge, as far as it overflows.
  const stripLeft = rect.right;
  const stripRight = rect.right + over;
  for (const neighbour of cell.neighbourTexts ?? []) {
    const nr = neighbour?.rect;
    if (!nr) continue;
    // Painted words, not empty boxes: a neighbour cell reserves its whole column
    // whether or not it has anything in it, so only its text rects count.
    if (!String(neighbour.text ?? "").trim()) continue;
    if (overlaps1D(stripLeft, stripRight, nr.left, nr.right) <= 0) continue;
    if (overlaps1D(rect.top, rect.bottom, nr.top, nr.bottom) <= 0) continue;
    return `painted over "${String(neighbour.text).trim().slice(0, 24)}" in the next column`;
  }
  return "";
}

/**
 * A button or link whose text is cut off.
 *
 * Stop firing on: the Tasks list, where a task link's words are 3px wider than
 * its grid track but the box does not clip, so every letter is on screen; and
 * on a whole Work Board card wrapped in a link, where the extra width is the
 * card's own layout, not a cut label.
 * Still catch: a control that really clips its label with no ellipsis.
 *
 * @param {{overflowX:string, overflowY:string, textOverflow:string, lineClamp:string,
 *          clientWidth:number, scrollWidth:number, clientHeight:number, scrollHeight:number}} control
 * @returns {boolean}
 */
export function controlTextIsCutOff(control) {
  if (!control) return false;
  const clipsX = /hidden|clip|scroll|auto/.test(String(control.overflowX ?? "visible"));
  const clipsY = /hidden|clip|scroll|auto/.test(String(control.overflowY ?? "visible"));
  // A box that does not clip cannot hide a letter: the glyphs paint outside it.
  const cutSideways =
    clipsX &&
    String(control.textOverflow ?? "clip") !== "ellipsis" &&
    Number(control.scrollWidth ?? 0) > Number(control.clientWidth ?? 0) + 2;
  const clamped = String(control.lineClamp ?? "none") !== "none";
  const cutDown =
    clipsY && !clamped && Number(control.scrollHeight ?? 0) > Number(control.clientHeight ?? 0) + 8;
  return Boolean(cutSideways || cutDown);
}

/**
 * The wide-table sideways-scroll check: which ancestor actually scrolls.
 *
 * Stop firing on: People / HRMS notifications, whose table sits in a wrapper
 * that scrolls sideways perfectly well — it just carries a class name that was
 * missing from a hand-kept allowlist. An allowlist of class names is stale the
 * day a page ships a new wrapper, and every time it goes stale it accuses a
 * working page.
 * Still catch: a wide table with no scrolling ancestor at all.
 *
 * @param {{overflowX:string, clientWidth:number, scrollWidth:number}[]} chain ancestors, nearest first
 * @returns {number} index of the scroll owner in `chain`, or -1
 */
export function scrollOwnerIndex(chain) {
  const list = Array.isArray(chain) ? chain : [];
  for (let i = 0; i < list.length; i += 1) {
    const node = list[i] ?? {};
    if (!/(auto|scroll)/.test(String(node.overflowX ?? ""))) continue;
    if (Number(node.scrollWidth ?? 0) <= Number(node.clientWidth ?? 0) + 2) continue;
    return i;
  }
  return -1;
}

/**
 * Two controls sitting on top of each other.
 *
 * Stop firing on: Verify with the video log open. A drawer and its backdrop are
 * meant to cover the page; every control behind them "overlaps" by geometry and
 * none of it is a bug — that is what an open drawer looks like.
 * Still catch: two controls colliding on the same layer of the same screen.
 *
 * @param {{first:{coveredByOverlay?:boolean, isOverlayChrome?:boolean},
 *          second:{coveredByOverlay?:boolean, isOverlayChrome?:boolean}}} pair
 * @returns {boolean}
 */
export function overlapIsVisibleBreak(pair) {
  const a = pair?.first ?? {};
  const b = pair?.second ?? {};
  // Backdrop/scrim/dialog chrome against anything it is covering is the design.
  if (a.isOverlayChrome || b.isOverlayChrome) return false;
  // Two controls where one is behind an open overlay are not on the same layer.
  if (a.coveredByOverlay !== b.coveredByOverlay) return false;
  return true;
}

/**
 * A drawer or dialog that opens with its title scrolled out of sight.
 *
 * Stop firing on: the Tasks card drawer, which opens with its breadcrumb and
 * Edit/Close bar on top and the title right underneath — all of it on screen.
 * The old rule demanded the title start within 48px of the overlay's top edge,
 * which is a guess about layout, not a report of anything a person sees.
 * Still catch: a drawer whose title is above its own top edge, off the bottom of
 * the screen, collapsed to nothing, or scrolled away before the reader touches it.
 *
 * @param {{overlayTop:number, headerTop:number, headerBottom:number, headerHeight:number,
 *          viewportHeight:number, scrollerScrollTop?:number}} o
 * @returns {string} human detail, or ""
 */
export function overlayHeaderOutOfView(o) {
  if (!o) return "";
  const height = Number(o.headerHeight ?? 0);
  if (height < 4) return `its title bar is ${Math.round(height)}px tall`;
  const top = Number(o.headerTop ?? 0);
  const bottom = Number(o.headerBottom ?? top + height);
  const overlayTop = Number(o.overlayTop ?? 0);
  const viewportHeight = Number(o.viewportHeight ?? 0);
  if (top < overlayTop - 1) return "its title sits above the panel's own top edge";
  if (top < -1) return "its title is above the top of the screen";
  if (viewportHeight > 0 && top > viewportHeight - 1) return "its title is below the bottom of the screen";
  if (viewportHeight > 0 && bottom > viewportHeight + 1) return "its title is cut off by the bottom of the screen";
  // Opened already scrolled: the reader has to scroll up to find out what they opened.
  if (Number(o.scrollerScrollTop ?? 0) > 1) return "it opens already scrolled down, past its own title";
  return "";
}
