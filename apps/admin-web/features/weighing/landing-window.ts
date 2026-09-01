// The Weights landing window, shared by /weighing/weights and /weighing/analytics.
//
// ONE implementation, imported by both, rather than a copy each. The two screens are read against
// each other — a reader moves from the estate's weights to the analysis of them — and a window
// that starts on the proper Aug 2026 weighing history on one page and something else on the other would
// make every figure look like it disagreed when only the period differed. Two copies of a rule
// this fiddly (a lookback, a second-to-last date, a separate end date, three fallbacks) drift on
// the first change to either.
//
// Business dates throughout: both ends are inclusive "YYYY-MM-DD" Asia/Kolkata days, which is what
// the API's from/to expect. A weigh belongs to the IST day it happened on, never a clock offset.
import { istDayPlus } from "@/lib/format";
import { getWeighingDates } from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";

/** Both pages carry the window in the same two parameters, so a link survives moving between them. */
export const WINDOW_FROM_PARAM = "wt_from";
export const WINDOW_TO_PARAM = "wt_to";

/**
 * The farm asked for the default period to start at the first dense/proper weighing history.
 * The shared staging data currently has the reliable run from 2026-08-03 onward; before that, July rows are
 * sparse weekly checks and make the default read noisy. Keep this fixed until enough later history
 * exists to replace it with a true long-term rolling window.
 */
export const DEFAULT_WINDOW_FROM = "2026-08-03";
export const LATEST_LUMP_LOOKBACK_DAYS = 400;
export const BUSINESS_DAY = /^\d{4}-\d{2}-\d{2}$/;

export type Window = { from: string; to: string };

export function defaultWindow(today: string): Window {
  return { from: DEFAULT_WINDOW_FROM > today ? today : DEFAULT_WINDOW_FROM, to: today };
}

/**
 * The explicitly selected window, or null when the reader has chosen nothing.
 *
 * Malformed, inverted or half-supplied parameters fall back to the default rather than throwing: a
 * hand-edited URL must not take the page down. A future end is clamped to today, because a weigh
 * cannot have happened tomorrow and the reads would return an empty span for it.
 */
export function explicitWindow(params: RouteSearchParams, today: string): Window | null {
  const rawFrom = one(params, WINDOW_FROM_PARAM)?.trim();
  const rawTo = one(params, WINDOW_TO_PARAM)?.trim();
  if (rawFrom && rawTo && BUSINESS_DAY.test(rawFrom) && BUSINESS_DAY.test(rawTo) && rawFrom <= rawTo) {
    return { from: rawFrom > today ? today : rawFrom, to: rawTo > today ? today : rawTo };
  }
  if (rawFrom || rawTo) return defaultWindow(today);
  return null;
}

/** The window a page opens on: the reader's own selection, else 2026-08-03 through latest weighing. */
export async function landingWindow(
  params: RouteSearchParams,
  today: string,
  parkID: string,
  sexFilter: string,
  originFilter = "",
  weighingCategoryFilter = "",
): Promise<Window> {
  const selected = explicitWindow(params, today);
  if (selected) return selected;

  const lookback = {
    from: istDayPlus(today, -(LATEST_LUMP_LOOKBACK_DAYS - 1)),
    to: today,
  };
  // Same page-level scope as the reads below. Now that absent filters can have meaning, the
  // default landing window must be chosen from the exact herd slice the page will render.
  // The NARROW read, not getShedWeights. This lookback spans 400 days, and the full shed read runs
  // four queries over it -- the shed table, per-load growth, the summary -- of which this function
  // uses exactly two date fields. Locally that was ~570ms against ~140ms for the real windowed
  // read, and it is paid before every page load and every tab switch, blocking them: the reads
  // below cannot start until the window is known. Against a cloud database, where each of those
  // queries carries its own round trip, it was the dominant cost of the screen.
  const result = await getWeighingDates({
    park_id: parkID || undefined,
    ...lookback,
    sex: sexFilter || undefined,
    origin: originFilter || undefined,
    weighing_category: weighingCategoryFilter || undefined,
  });
  if (!result.ok) return defaultWindow(today);

  // END from the last day the farm weighed ANYTHING, not just a lump-sum day.
  // The backend owns the date (`latest_weighing_date`, whole-filter over both weighing grains);
  // an empty value falls back to today so the page still renders.
  const latest = result.data.latest_weighing_date ?? "";
  const end = BUSINESS_DAY.test(latest) ? latest : today;
  return {
    from: DEFAULT_WINDOW_FROM > today ? today : DEFAULT_WINDOW_FROM,
    to: end > today ? today : end,
  };
}
