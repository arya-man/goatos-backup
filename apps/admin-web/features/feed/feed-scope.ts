import "server-only";

import { istDayPlus, todayIso } from "@/lib/format";
import { backendScope, parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import type { LocationOption } from "@/lib/api/herd-locations";

// Park + feed-day resolution shared by the three Feed pages.
//
// Every feed endpoint REQUIRES a park and (for the generated day) a date — the ration grid, the
// session split and the dispatch clock are all park-scoped, and a feed sheet is generated for exactly
// one Asia/Kolkata business day. So neither is an optional filter that can sit at "All".
//
// Precedence, and why:
//   1. the top-bar park scope, which owns park scope app-wide (Scope Chrome Rule); when it is set
//      the page's own park control renders disabled-with-reason rather than disappearing.
//   2. the page's own park param, used when the top bar is company-wide.
//   3. the first park in the locations master — a real canonical park, so the select visibly shows
//      which one is being read, rather than the page silently rendering empty.
//
// THE FEED DAY DEFAULTS TO TOMORROW, NOT TODAY.
//
// A direction is issued today FOR TOMORROW'S FEED: it is packed today, transported before the
// afternoon cutoff, and fed the following morning (see the feed day clock on /feed/config). Today's
// feed was directed and packed yesterday and is already sitting at the shed, so a sheet dated today
// describes a day that has already been fed — an operator landing on it is looking at work that can
// no longer be changed, and any correction they make there is applied to nothing.
//
// The business day comes from todayIso() (Asia/Kolkata, resolved server-side — this module is
// server-only, so no browser clock is involved), and istDayPlus does the +1 as calendar arithmetic
// on the resolved day string rather than re-deriving a timezone. A UTC "today" would roll the sheet
// a day early for part of every IST evening.
const FEED_DAY_LEAD_DAYS = 1;

export type FeedScope = {
  /** Empty only when the locations master returned no parks at all. */
  parkId: string;
  /** True when the top bar owns the park, which makes the page's park control read-only. */
  parkLockedByTopBar: boolean;
  /**
   * Which precedence rung supplied `parkId`.
   *
   * "fallback" is the one the operator must be told about: the top bar is company-wide and the page
   * has no park param, so this page picked the first park in the locations master on its own. The
   * screen then shows ONE park while the top bar reads "All parks", which is how a whole park's
   * sheds go missing without any on-screen sign.
   */
  parkSource: "top_bar" | "page" | "fallback";
  targetDate: string;
  /**
   * The inclusive feed-day window bounds, in Asia/Kolkata business dates. The date picker is bound to
   * these so the operator physically cannot pick outside [today, tomorrow].
   */
  minDate: string;
  maxDate: string;
};

/** The day a feed sheet opened right now is for: the next feed day, in Asia/Kolkata. */
export function defaultFeedDay(): string {
  return istDayPlus(todayIso(), FEED_DAY_LEAD_DAYS);
}

/**
 * The valid feed-day window [today, tomorrow] in Asia/Kolkata.
 *
 * The projected shed count that drives a sheet — live herd + approved-but-unexecuted shiftings — is
 * only meaningful for today (being fed, packed yesterday) and tomorrow (being packed now). Beyond
 * tomorrow the counts depend on shiftings not yet approved; a past day's herd is not what it is now.
 * So a sheet for any other day is fabricated, and the backend refuses to generate it. `today` and
 * `tomorrow` come from the SAME Asia/Kolkata helpers the default feed day uses (todayIso + istDayPlus),
 * resolved server-side — never a raw browser clock or a hardcoded offset.
 */
export function feedDayWindow(): { min: string; max: string } {
  const today = todayIso();
  return { min: today, max: istDayPlus(today, FEED_DAY_LEAD_DAYS) };
}

/**
 * Clamps a requested feed day into [today, tomorrow]. A stale bookmark to a far-future or past day
 * (e.g. `?fd_date=2026-08-15`) is pulled to the nearest valid day rather than requesting a day the
 * backend can only answer with a fabricated or beyond-horizon sheet. An unparseable value falls back
 * to the default (tomorrow).
 */
export function clampFeedDay(requested: string): string {
  const { min, max } = feedDayWindow();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(requested)) return defaultFeedDay();
  if (requested < min) return min;
  if (requested > max) return max;
  return requested;
}

export function resolveFeedScope(
  sp: RouteSearchParams,
  parkParam: string,
  dateParam: string,
  parks: LocationOption[],
): FeedScope {
  const { parkId: topBarParkId } = backendScope(parseScope(sp));
  const pageParkId = one(sp, parkParam);
  const selected = topBarParkId || pageParkId || parks[0]?.id || "";
  const { min, max } = feedDayWindow();
  const requested = one(sp, dateParam);
  // Default to tomorrow when absent; clamp an out-of-window value into [today, tomorrow] so the page
  // never requests a fabricated/beyond-horizon day even from a stale URL.
  const targetDate = requested ? clampFeedDay(requested) : defaultFeedDay();
  return {
    parkId: selected,
    parkLockedByTopBar: Boolean(topBarParkId),
    // WHICH of the three precedence rungs actually supplied the park. Only "fallback" is silent:
    // nobody chose that park, so the page must say so rather than let a company-wide top-bar scope
    // read as "all parks shown" when exactly one park is being read.
    parkSource: topBarParkId ? "top_bar" : pageParkId ? "page" : "fallback",
    targetDate,
    minDate: min,
    maxDate: max,
  };
}

/** Bounded page sizes for the feed reads, matching the contract's declared options. */
export function feedLimit(sp: RouteSearchParams, param: string, options: number[], fallback: number): number {
  const requested = Number(one(sp, param));
  return options.includes(requested) ? requested : fallback;
}

export function feedOffset(sp: RouteSearchParams, param: string): number {
  const requested = Number(one(sp, param));
  return Number.isFinite(requested) && requested > 0 ? Math.floor(requested) : 0;
}

// ---------------------------------------------------------------------------
// Feed PACKING browses by the PACKING day, not the feed day (maintainer decision 2026-07-27).
// ---------------------------------------------------------------------------
//
// A packer works TODAY on the sheet that will be fed TOMORROW, so the Feed Packing picker shows the
// PACKING day P and the backend is asked for feed day D = P + 1. The picker:
//   - DEFAULTS to today (the run being packed now), not tomorrow;
//   - is CAPPED at today — a packer cannot pack tomorrow's run before tomorrow morning generates it;
//   - reaches PACKING_HISTORY_DAYS back so previously packed sheets stay reviewable. Past days that
//     were never issued come back blank (beyond_horizon); that is expected, not an error.
//
// This is a relabel of the axis only: the backend still keys on the feed day D. Feed DIRECTION keeps
// the feed-day axis (resolveFeedScope) — only Feed Packing uses the packing-day axis.
const PACKING_HISTORY_DAYS = 30;

export type FeedPackingScope = {
  parkId: string;
  parkLockedByTopBar: boolean;
  /** The PACKING day shown and selected in the picker (Asia/Kolkata business date). */
  packingDay: string;
  /** The FEED day sent to the backend: packingDay + 1 (the day the animals eat). */
  targetDate: string;
  /** Picker bounds, in PACKING-day terms: [today - PACKING_HISTORY_DAYS, today]. */
  minDate: string;
  maxDate: string;
};

/** The packing day a Feed Packing page opened right now is for: today, in Asia/Kolkata. */
export function defaultPackingDay(): string {
  return todayIso();
}

/**
 * The valid packing-day window [today - PACKING_HISTORY_DAYS, today] in Asia/Kolkata.
 *
 * The maximum is TODAY: tomorrow's packing run does not exist until it is generated tomorrow morning,
 * so the picker cannot select a future packing day. The minimum reaches back so an operator can review
 * previously packed sheets. Both bounds come from the SAME Asia/Kolkata helper the default uses
 * (todayIso + istDayPlus), resolved server-side — never a raw browser clock or a hardcoded offset.
 */
export function packingDayWindow(): { min: string; max: string } {
  const today = todayIso();
  return { min: istDayPlus(today, -PACKING_HISTORY_DAYS), max: today };
}

/**
 * Clamps a requested packing day into [today - PACKING_HISTORY_DAYS, today]. A future packing day
 * (e.g. a stale link to tomorrow) is pulled back to today; a day older than the history window is
 * pulled up to the window floor. An unparseable value falls back to the default (today).
 */
export function clampPackingDay(requested: string): string {
  const { min, max } = packingDayWindow();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(requested)) return defaultPackingDay();
  if (requested < min) return min;
  if (requested > max) return max;
  return requested;
}

/**
 * Resolves the Feed Packing scope: the park, the PACKING day shown in the picker, and the FEED day
 * (packingDay + 1) sent to the backend. minDate/maxDate are in packing-day terms so the date input
 * clamps the packing day directly.
 */
export function resolveFeedPackingScope(
  sp: RouteSearchParams,
  parkParam: string,
  dateParam: string,
  parks: LocationOption[],
): FeedPackingScope {
  const { parkId: topBarParkId } = backendScope(parseScope(sp));
  const pageParkId = one(sp, parkParam);
  const selected = topBarParkId || pageParkId || parks[0]?.id || "";
  const { min, max } = packingDayWindow();
  const requested = one(sp, dateParam);
  const packingDay = requested ? clampPackingDay(requested) : defaultPackingDay();
  return {
    parkId: selected,
    parkLockedByTopBar: Boolean(topBarParkId),
    packingDay,
    targetDate: istDayPlus(packingDay, 1),
    minDate: min,
    maxDate: max,
  };
}

/**
 * Rewrites one search param while preserving every other (top-bar scope, sibling section paging).
 * Dropping the rest would silently reset the operator's park/day on every page click.
 */
export function feedHref(basePath: string, sp: RouteSearchParams, key: string, value: string): string {
  const next = new URLSearchParams();
  for (const [paramKey, paramValue] of Object.entries(sp)) {
    if (paramKey === key) continue;
    if (Array.isArray(paramValue)) {
      for (const item of paramValue) if (item) next.append(paramKey, item);
    } else if (paramValue) {
      next.set(paramKey, paramValue);
    }
  }
  if (value) next.set(key, value);
  const qs = next.toString();
  return qs ? `${basePath}?${qs}` : basePath;
}
