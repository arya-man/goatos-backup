/**
 * KPI sparkline series for the feed tiles.
 *
 * A trailing day whose figure is missing, zero, or below 60% of the median of the 7 real days
 * before it is a day still being recorded (a second park's sheet not issued yet, a half-fed day),
 * not a real drop, so it is dropped and the line ends at the last complete day. Only the TAIL is
 * trimmed; a genuine low day in the middle of the window stays on the line.
 */
export const PARTIAL_DAY_SHARE = 0.6;

function median(values: number[]): number {
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

export function completeDaySeries(values: (number | null)[]): number[] | undefined {
  let end = values.length;
  while (end > 0) {
    const last = values[end - 1];
    if (last === null || last <= 0) {
      end -= 1;
      continue;
    }
    const trailing = values.slice(Math.max(0, end - 8), end - 1).filter((v): v is number => v !== null && v > 0);
    if (trailing.length >= 3 && last < PARTIAL_DAY_SHARE * median(trailing)) {
      end -= 1;
      continue;
    }
    break;
  }
  const kept = values.slice(0, end);
  return kept.length > 1 && kept.every((v) => v !== null) ? (kept as number[]) : undefined;
}

/**
 * The day the "latest day" KPI tiles describe: the last COMPLETE sheet on or before the settled
 * day (yesterday). When yesterday has no sheet (the data stops days earlier) the tiles used to read
 * a bare "—" beside a 14-day trend that plainly had numbers (PR #294 O6); they now show the latest
 * day that has one, and the caption names that day. A trailing half-issued day is skipped the same
 * way the sparkline skips it, so the figure and the line end on one day.
 */
export function latestSheetDay<T extends { feed_day: string }>(days: readonly T[], settledDay: string, kg: (day: T) => number): T | undefined {
  const settled = days.filter((d) => d.feed_day <= settledDay).sort((a, b) => (a.feed_day < b.feed_day ? -1 : a.feed_day > b.feed_day ? 1 : 0));
  const complete = completeDaySeries(settled.map(kg))?.length ?? 0;
  if (complete > 0) return settled[complete - 1];
  for (let i = settled.length - 1; i >= 0; i -= 1) if (kg(settled[i]) > 0) return settled[i];
  return undefined;
}
