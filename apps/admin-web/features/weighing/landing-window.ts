// The Weights landing window, shared by /weighing/weights and /weighing/analytics.
//
// ONE implementation, imported by both, rather than a copy each. The two screens are read against
// each other — a reader moves from the estate's weights to the analysis of them — and a window
// that meant "the last two whole-shed weighs" on one page and something else on the other would
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
 * Fallback window only. The real landing default is resolved below from the latest two lump-sum
 * weighing dates, because leadership reads these screens as "latest available weigh minus the one
 * before it" rather than a clock-calendar fortnight. If that lookup cannot produce two dates, a
 * stable page still has to render.
 */
export const DEFAULT_WINDOW_DAYS = 15;
export const LATEST_LUMP_LOOKBACK_DAYS = 400;
export const BUSINESS_DAY = /^\d{4}-\d{2}-\d{2}$/;

export type Window = { from: string; to: string };

export function defaultWindow(today: string): Window {
  return { from: istDayPlus(today, -(DEFAULT_WINDOW_DAYS - 1)), to: today };
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

/** The window a page opens on: the reader's own selection, else the latest two whole-shed weighs. */
export async function landingWindow(
  params: RouteSearchParams,
  today: string,
  parkID: string,
  sexFilter: string,
): Promise<Window> {
  const selected = explicitWindow(params, today);
  if (selected) return selected;

  const lookback = {
    from: istDayPlus(today, -(LATEST_LUMP_LOOKBACK_DAYS - 1)),
    to: today,
  };
  // Same sex scope as the page reads below. Now that an absent `sex` means Male, the default
  // landing window must be chosen from the male herd too; explicit `sex=all` still reaches this
  // helper as "", which preserves the old all-kid lookup.
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
  });
  if (!result.ok) return defaultWindow(today);

  const dates = [...new Set(result.data.lump_weighing_dates ?? [])].sort();
  if (dates.length < 2) return defaultWindow(today);
  // START from the lump dates, END from the last day the farm weighed ANYTHING.
  //
  // The two are different questions and were answered by one list. A shed-average movement needs two
  // whole-shed weighs, so the START has to be the second-to-last of those. The END does not: on
  // 25 Aug 2026 the farm scanned 199 kids across 17 sheds and no shed was weighed whole, so that day
  // was absent from lump_weighing_dates entirely and a window closing on the later lump date shut
  // one day early -- dropping every one of those kids from the KPIs, the gain charts and Fair fight,
  // with the period label reading as if nothing had been missed.
  //
  // The backend owns the date (`latest_weighing_date`, whole-filter over both weighing grains); a max
  // taken across the returned rows here would be the page deriving business truth from its own rows.
  // An empty value falls back to the lump date, which is the behaviour this replaces.
  const latest = result.data.latest_weighing_date ?? "";
  const end = BUSINESS_DAY.test(latest) && latest > dates[dates.length - 1] ? latest : dates[dates.length - 1];
  return {
    from: dates[dates.length - 2],
    to: end > today ? today : end,
  };
}
