// Row geometry shared by the horizontal bar charts (components/svg-bars.tsx wrappers and the
// client charts in ./bar-charts.tsx). A plain module, no "use client": the server wrappers and
// the client charts must read the SAME numbers, and a constant imported from a client module
// into a server component arrives as a client reference, not as the value.

/** Height of one bar row in CSS px. The template bar (barHeight 48%) sits inside it. */
export const BAR_ROW_PX = 32;

/** Plot chrome above and below the rows (Apex grid padding), in CSS px. */
export const BAR_CHROME_PX = 16;

/**
 * How many rows stand in the card once a chart scrolls (maintainer, 2026-08-12).
 *
 * A horizontal Apex bar chart draws each row at `height / rows`, so the window is a PIXEL height:
 * the row height no longer depends on the card width the way the old viewBox drawing did.
 */
export const VISIBLE_BARS = 10;

/**
 * Up to this many rows the chart simply grows. Past it, the chart keeps its full height inside a
 * window of VISIBLE_BARS rows that scrolls within the card (overscroll contained), so a 130-pen
 * chart never turns the page into a scroll trap and a 12-row chart never gains a needless one.
 */
export const GROW_BARS = 15;

/** Full chart height for `count` rows -- the one place row geometry is turned into height. */
export function barsViewHeight(count: number): number {
  return count * BAR_ROW_PX + BAR_CHROME_PX;
}

/** Whether a chart of `count` rows gets the scroll window. */
export function barsScroll(count: number): boolean {
  return count > GROW_BARS;
}

/** Height of the visible window: the whole chart, or VISIBLE_BARS rows once it scrolls. */
export function barsWindowHeight(count: number): number {
  return barsScroll(count) ? barsViewHeight(VISIBLE_BARS) : barsViewHeight(count);
}
