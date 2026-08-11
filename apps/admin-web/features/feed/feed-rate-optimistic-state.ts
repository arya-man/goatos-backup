// The two pure decisions behind showing a just-saved ration rate before the server catches up.
//
// React-free and in its own module so they can be unit-tested directly — the same split this folder
// already uses for feed-quantity-state.ts, and the reason matters here: the settle rule below was
// wrong in a way no typecheck or render test would have caught, and only a real browser edit
// exposed it.

/**
 * The feed-config join key, mirroring Postgres `feed_config_norm`: trim, casefold, collapse runs of
 * whitespace/underscore/hyphen to one underscore.
 *
 * PARK IS PART OF THE KEY. The same ration group / shed tag / feed item combination exists in BOTH
 * parks carrying different authored rates, so dropping it would let a save in one park paint the
 * other park's cell.
 *
 * Used only to match a just-saved value back to the row that produced it. A drift here shows a stale
 * number for one render at worst; it can never write anything.
 */
export function rationRateKey(parkId: string, rationGroup: string, shedTag: string, feedItem: string): string {
  const norm = (raw: string) => raw.trim().toLowerCase().replace(/[\s_-]+/g, "_");
  return [parkId, norm(rationGroup), norm(shedTag), norm(feedItem)].join("|");
}

/**
 * Whether the server has caught up, comparing the two as NUMBERS rather than as text.
 *
 * THE DEFECT THIS FIXES (found in the browser, 2026-08-11): the check compared STRINGS. An operator
 * types `2158` and the numeric(12,3) column returns `2158.000`, so the server's answer never matched
 * what was typed — the optimistic value never cleared and the cell kept an unconfirmed-looking
 * number long after the write had landed.
 *
 * A display equality check only. Nothing here decides what is stored, and the exact authored decimal
 * still comes from the server. Falls back to a trimmed string compare when either side is not a
 * number, so a non-numeric value can still settle rather than hanging until it expires.
 */
export function sameRate(a: string, b: string): boolean {
  const left = Number(a);
  const right = Number(b);
  // Number("") is 0, so a blank must be rejected BEFORE the numeric compare: an absent rate means
  // UNCONFIGURED and blocks the shed, while an authored 0 means "feed nothing of this item" and is
  // correct for milk-fed kids. Letting those two compare equal is the one mistake this file cannot
  // afford, even for a display decision.
  if (a.trim() === "" || b.trim() === "") return a.trim() === b.trim();
  if (Number.isFinite(left) && Number.isFinite(right)) return left === right;
  return a.trim() === b.trim();
}
