/** Both Weights pages carry the window in the same two parameters, so a link survives moving between them. */
export const WINDOW_FROM_PARAM = "wt_from";
export const WINDOW_TO_PARAM = "wt_to";

/**
 * The farm asked for the default period to start at the first dense/proper weighing history.
 * The shared staging data currently has the reliable run from 2026-08-03 onward; before that,
 * July rows are sparse weekly checks and make the default read noisy. Keep this fixed until enough
 * later history exists to replace it with a true long-term rolling window.
 */
export const DEFAULT_WINDOW_FROM = "2026-08-03";

/**
 * Seeded earliest selectable weighing history, including the sparse July checks.
 * The tenant database row can change this independently of the landing period.
 */
export const WINDOW_MIN_DATE = "2026-07-05";

export const LATEST_LUMP_LOOKBACK_DAYS = 400;
export const BUSINESS_DAY = /^\d{4}-\d{2}-\d{2}$/;

/**
 * The Weights pages' window settings are read from weighing_calendar_config and served on
 * both pages' contract copy by the backend. The constants only support an older contract
 * without the settings. These values are configured directly in the database.
 */
export type WeightsWindowSettings = {
  /** The day the pages open from. */
  defaultFrom: string;
  /** The earliest day the calendars offer; earlier days are disabled. */
  earliestDate: string;
};

export function weightsWindowSettings(copyMap: Record<string, string> | undefined, today: string): WeightsWindowSettings {
  const get = (key: string) => copyMap?.[key]?.trim() ?? "";
  const earliestRaw = get("weights.window.earliest_date");
  const earliestDate = BUSINESS_DAY.test(earliestRaw) ? earliestRaw : WINDOW_MIN_DATE;
  let defaultFrom = DEFAULT_WINDOW_FROM;
  const mode = get("weights.window.default_from_mode");
  if (mode === "rolling_days") {
    const days = Number.parseInt(get("weights.window.default_from_days"), 10);
    if (Number.isFinite(days) && days >= 1) defaultFrom = shiftIsoDay(today, -(days - 1));
  } else if (mode === "rolling_weeks") {
    const weeks = Number(get("weights.window.default_from_weeks"));
    // Calendar weeks back from the current IST day, not the latest weighing day.
    // Six weeks before 2026-09-16 is 2026-08-05 (both endpoints are selectable).
    if (Number.isInteger(weeks) && weeks >= 1 && weeks <= 520) defaultFrom = shiftIsoDay(today, -weeks * 7);
  } else if (mode === "fixed_date") {
    const fixed = get("weights.window.default_from_date");
    if (BUSINESS_DAY.test(fixed)) defaultFrom = fixed;
  }
  // The window never starts before the earliest offerable day, whatever the database configuration says.
  if (defaultFrom < earliestDate) defaultFrom = earliestDate;
  return { defaultFrom, earliestDate };
}

/** Adds `days` (negative allowed) to a "YYYY-MM-DD" business date, calendar arithmetic only. */
function shiftIsoDay(iso: string, days: number): string {
  const [y, m, d] = iso.split("-").map(Number);
  const t = Date.UTC(y, (m ?? 1) - 1, d ?? 1) + days * 86_400_000;
  return new Date(t).toISOString().slice(0, 10);
}

/** Keep the authored start when the latest weighing predates it: render an empty
 * current period instead of sending an inverted range to the reporting APIs. */
export function windowThroughLatest(today: string, latest: string, settings?: WeightsWindowSettings): { from: string; to: string } {
  const requestedFrom = settings?.defaultFrom ?? DEFAULT_WINDOW_FROM;
  const from = requestedFrom > today ? today : requestedFrom;
  const end = BUSINESS_DAY.test(latest) && latest <= today && latest >= from ? latest : today;
  return { from, to: end };
}
