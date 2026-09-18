/**
 * WHERE THE NOTIFICATION PANEL SITS -- the arithmetic, on its own, as a pure function.
 *
 * It lives outside the component for two reasons. First, it is the part that was wrong once: the
 * shared `.parkmenu` idiom (`position:absolute;top:46px;right:0`) lays a 300px panel out
 * LEFTWARDS from a 40px button, which works for the park and account menus because they sit at the
 * END of the top bar -- the bell does not. With the top bar wrapped to two rows at <=560px the
 * panel's left edge landed at -191px (320px viewport) and -250px (360px viewport), and
 * `html,body{overflow-x:hidden;max-width:100vw}` at <=860px means the rest could NEVER be
 * scrolled to. A long park name reproduced it at 700px too, so it is a wrap-POSITION problem, not
 * a 320px problem: nothing in CSS knows where the bell wrapped to.
 *
 * Second, a pure function is the only version of this a test can share with the component. The
 * layout test measures the real bell against the real stylesheet and then asks THIS function where
 * the panel belongs, so the test cannot drift from the shipped arithmetic.
 */

/** The panel's ideal width -- the same 300px the shared `.parkmenu` uses on a wide bar. */
export const NOTIFICATION_PANEL_WIDTH = 300;
/** The smallest gap the panel keeps from either viewport edge. */
export const NOTIFICATION_PANEL_GUTTER = 8;
/** The gap between the bell's bottom edge and the panel's top edge. */
export const NOTIFICATION_PANEL_OFFSET = 6;

export type NotificationPanelAnchor = { readonly right: number; readonly bottom: number };
export type NotificationPanelBox = { readonly top: number; readonly left: number; readonly width: number };

/**
 * Right-aligns the panel to the bell when there is room, flips it rightwards when there is not,
 * and never lets it come closer than the gutter to either viewport edge. Coordinates are in
 * VIEWPORT space; the caller is responsible for the containing block (see `placePanel`).
 */
export function placeNotificationPanel(anchor: NotificationPanelAnchor, viewportWidth: number): NotificationPanelBox {
  const width = Math.min(NOTIFICATION_PANEL_WIDTH, viewportWidth - NOTIFICATION_PANEL_GUTTER * 2);
  // Preferred: right edge flush with the bell, exactly as `.parkmenu` does on a wide bar.
  const preferred = anchor.right - width;
  const rightmost = viewportWidth - NOTIFICATION_PANEL_GUTTER - width;
  const left = Math.max(NOTIFICATION_PANEL_GUTTER, Math.min(preferred, rightmost));
  return {
    top: Math.round(anchor.bottom + NOTIFICATION_PANEL_OFFSET),
    left: Math.round(left),
    width: Math.round(width),
  };
}
