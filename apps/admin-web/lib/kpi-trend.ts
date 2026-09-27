// Trend figures for the KPI adapter (components/app/kpi-widget.tsx). Display-only comparisons of
// series the page already holds; the template widget prints the percent with its own period text,
// so each helper must match that text exactly.

type Series = readonly (number | null | undefined)[];

const finite = (series: Series) => series.filter((v): v is number => typeof v === "number" && Number.isFinite(v));
const pct = (cur: number, prev: number) => (prev === 0 ? null : Math.round(((cur - prev) / Math.abs(prev)) * 1000) / 10);

/** Percent change of the last point against the one before it (null when not comparable). */
export function lastStepPercent(series: Series): number | null {
  const values = finite(series);
  if (values.length < 2) return null;
  return pct(values[values.length - 1], values[values.length - 2]);
}

/** Sum of the last 7 points against the 7 before them, as a percent (null without 14 points). */
export function sevenDayPercent(series: Series): number | null {
  const values = series.map((v) => (typeof v === "number" && Number.isFinite(v) ? v : 0));
  if (values.length < 14) return null;
  const sum = (xs: number[]) => xs.reduce((a, b) => a + b, 0);
  return pct(sum(values.slice(-7)), sum(values.slice(-14, -7)));
}

/** True when an ISO day (YYYY-MM-DD) is not the last day of its month, so that month is partial. */
export function monthIsPartial(isoDay: string | null | undefined): boolean {
  if (!isoDay || !/^\d{4}-\d{2}-\d{2}/.test(isoDay)) return true;
  const [y, m, d] = isoDay.slice(0, 10).split("-").map(Number);
  const lastDay = new Date(Date.UTC(y, m, 0)).getUTCDate();
  return d < lastDay;
}

/**
 * Month-on-month change between the last two COMPLETE months of a monthly series whose last point
 * is the month containing `windowTo`. While that month is still running it is left out: comparing a
 * part month with a whole one reads as a false drop for most of the month.
 */
export function completeMonthPercent(series: Series, windowTo: string | null | undefined): number | null {
  const complete = monthIsPartial(windowTo) ? series.slice(0, -1) : series;
  return lastStepPercent(complete);
}
