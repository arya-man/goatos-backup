// (The svg- file name is historical: nothing here draws SVG by hand any more; the importers keep
// the path so no page changes.)
//
// Series charts (stacked columns, multi-line, spend-share donut, legend) on the licensed MUI
// Minimal template's ApexCharts `Chart` (components/minimal/chart), drawn by the client half in
// components/series-charts.tsx.
//
// The exports here stay SERVER-SAFE and keep their names and props, because the pages that draw
// them are server components and pass functions (formatValue, formatTick). Every visible string is
// composed HERE, on the server — DD/MM/YYYY categories, the tooltip figure of every series at every
// point, the y tick labels of a fixed round axis top — and only strings and numbers cross into the
// client. Nothing below owns copy: every word arrives resolved from the backend page contract.
//
// These live in components/ rather than inside one feature because several pages draw the same
// marks (Feed Analytics over feed days, Herd Analytics / Mortality over calendar months, Sales
// month by month, Market analytics over survey mornings). The feature files re-export from here so
// there is exactly one implementation.
//
// Series colours reuse the exact ordering of components/svg-bars.tsx so an entity keeps the same
// colour on every chart of a page (colour follows the entity, never its rank on one chart).

// Twelve DISTINCT hues, every one a LOCKED Mesha token (app/mesha-theme.css), no blends: the six
// base tokens first (an entity keeps its colour on every chart), then six token inks chosen for
// the widest separation in BOTH modes (worst pair CIE76 dE 14.1 light, well above the old
// info/purple blend). The old tail mixed tokens with color-mix(), which put seven colours on
// screen that were in no palette (judge 4 P1-8); mui-palette-lock now fails a blend in a series
// array. Past twelve series, seriesColorVar tints these with ink/panel.
export const SERIES_VARS = [
  "var(--brand)",
  "var(--info)",
  "var(--amber)",
  "var(--purple)",
  "var(--teal)",
  "var(--danger)",
  "var(--info-ink)",
  "var(--warning-ink)",
  "var(--success-ink)",
  "var(--gain-under)",
  "var(--gain-hi)",
  "var(--danger-tag-ink)",
] as const;

import { EmptyState } from "./app/empty-state";
import { axisCeiling, quarterTicks } from "./chart-scale";
import { SeriesLegendView, SeriesLinesChart, SeriesPieChart, StackedColumnsChart, type AxisTick } from "./series-charts";

export function seriesColorVar(index: number) {
  const base = SERIES_VARS[index % SERIES_VARS.length];
  const cycle = Math.floor(index / SERIES_VARS.length);
  if (cycle === 0) return base;
  const mix = cycle % 2 === 1 ? "var(--ink)" : "var(--panel)";
  const share = cycle % 2 === 1 ? 74 : 82;
  return `color-mix(in srgb, ${base} ${share}%, ${mix})`;
}

const nf = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 1 });

/**
 * A figure with its noun. A rupee noun leads, the way every money figure reads ("₹58,518",
 * "₹412 per animal"), never "58,518 ₹"; any other noun follows the figure ("1,838 kg").
 */
const withNoun = (value: number, noun: string) =>
  noun.startsWith("₹") ? `₹${nf(value)}${noun.slice(1).replace(/^\s+/, " ").trimEnd()}` : `${nf(value)} ${noun}`;

// Axis dates render DD/MM/YYYY, the same shape as every other visible date
// (maintainer decision 2026-09-10, superseding the 2026-08-21 compact dd-mm-yy).
// A reader should never have to learn a second date format to read an axis, so
// the axis carries the full year rather than a two-digit one. A label that is
// not a plain YYYY-MM-DD date renders unchanged.
const fmtDay = (label: string) => {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(label);
  return m ? `${m[3]}/${m[2]}/${m[1]}` : label;
};

/**
 * The five gridline positions of a round axis top (0 and the quarters), each with its label.
 * An integer scale leaves a fractional quarter unlabelled (no "0.75 animals"); axisCeiling already
 * guarantees at least two labelled quarters, so the axis always reads.
 */
function axisTicks(max: number, integer: boolean, format: (value: number) => string): AxisTick[] {
  const labelled = new Set(quarterTicks(max, integer));
  return [0, 0.25, 0.5, 0.75, 1].map((f) => {
    const value = max * f;
    return { value, label: value === 0 || labelled.has(value) ? format(value) : "" };
  });
}

export type StackedDay = {
  key: string;
  /** Tooltip label for the day, resolved by the caller. */
  label: string;
  /** Segment values in series order; the caller aligns them with its legend. */
  segments: number[];
  /** Extra pre-formatted tooltip rows in a DIFFERENT unit (e.g. the rupees behind a head count). */
  extra?: { label: string; value: string }[];
};

/**
 * Slot-by-slot stacked columns on one shared round scale (template AppAreaInstalled).
 */
export function StackedColumns({
  days,
  seriesLabels,
  seriesColors,
  hideZeroInTip = false,
  valueNoun,
  chartLabel,
  emptyLabel,
  formatValue,
  formatTick,
  columnFigures = false,
}: {
  days: StackedDay[];
  seriesLabels: string[];
  /** Fill per series, when the caller's legend owns the colours; defaults to the palette by index. */
  seriesColors?: string[];
  /** Leave a series out of a day's tooltip when its value that day is 0. */
  hideZeroInTip?: boolean;
  valueNoun: string;
  chartLabel: string;
  emptyLabel: string;
  /** Tooltip figure formatter (default: en-IN number + valueNoun). Server-side only. */
  formatValue?: (value: number) => string;
  /** Y tick formatter (default: en-IN number). Gets the axis top so every tick on one axis uses the same unit (no "75k" beside "1L"). */
  formatTick?: (value: number, axisMax: number) => string;
  /**
   * Print each slot's total on its column and label EVERY slot (month-by-month report charts);
   * the plot keeps a minimum column width and scrolls inside the card on a phone. A measured 0
   * prints its figure on the baseline.
   */
  columnFigures?: boolean;
}) {
  const totals = days.map((d) => d.segments.reduce((a, b) => a + b, 0));
  if (days.length === 0 || !totals.some((total) => total > 0)) {
    return <EmptyState title={emptyLabel} />;
  }
  const rawMax = Math.max(1, ...totals);
  // Round ceiling so the quarter gridlines carry round figures; integer scale when every total is.
  const integer = totals.every((t) => Number.isInteger(t));
  const max = axisCeiling(rawMax, integer);
  const count = Math.max(seriesLabels.length, ...days.map((d) => d.segments.length));
  const series = Array.from({ length: count }, (_, s) => ({
    name: seriesLabels[s] ?? "",
    color: seriesColors?.[s] ?? seriesColorVar(s),
    data: days.map((d) => d.segments[s] ?? 0),
    tips: days.map((d) => {
      const v = d.segments[s] ?? 0;
      return formatValue ? formatValue(v) : withNoun(v, valueNoun);
    }),
  }));
  return (
    <StackedColumnsChart
      categories={days.map((d) => fmtDay(d.label))}
      series={series}
      extras={days.map((d) => d.extra ?? [])}
      max={max}
      yTicks={axisTicks(max, integer, (v) => (formatTick ? formatTick(v, max) : nf(v)))}
      hideZeroInTip={hideZeroInTip}
      chartLabel={chartLabel}
      columnFigures={columnFigures ? totals.map((t) => (formatValue ? formatValue(t) : nf(t))) : undefined}
    />
  );
}

export type LineSeries = {
  label: string;
  colorVar: string;
  /** One point per day slot; null = no figure that day (line breaks, honestly). */
  points: (number | null)[];
};

/**
 * Multi-series line chart on one shared scale (template EcommerceYearlySales, area under the
 * primary series only).
 *
 * `secondary` draws ONE more series in a different unit on its OWN scale — a dashed line with
 * its ticks on the right edge — so money and quantity can share a card without either being
 * squashed flat by the other's magnitude. Its points join the same tooltip in their own noun.
 */
export function SeriesLines({
  series,
  dayLabels,
  valueNoun,
  chartLabel,
  emptyLabel,
  secondary,
  hideZeroInTip = false,
}: {
  series: LineSeries[];
  dayLabels: string[];
  valueNoun: string;
  chartLabel: string;
  emptyLabel: string;
  secondary?: { series: LineSeries; valueNoun: string };
  /** Leave a series out of a day's tooltip when its value that day is 0. */
  hideZeroInTip?: boolean;
}) {
  const values = series.flatMap((s) => s.points.filter((p): p is number => p !== null));
  const secondaryValues = secondary?.series.points.filter((p): p is number => p !== null) ?? [];
  if ((values.length === 0 && secondaryValues.length === 0) || dayLabels.length === 0) {
    return <EmptyState title={emptyLabel} />;
  }
  const integer = values.every((v) => Number.isInteger(v));
  const max = axisCeiling(Math.max(1, ...values), integer);
  const secondaryInteger = secondaryValues.every((v) => Number.isInteger(v));
  const secondaryMax = axisCeiling(Math.max(1, ...secondaryValues), secondaryInteger);
  // A null stays null (a gap, and no tooltip row), never 0.
  const toChart = (s: LineSeries, noun: string) => ({
    name: s.label,
    color: s.colorVar,
    data: dayLabels.map((_, i) => s.points[i] ?? null),
    tips: dayLabels.map((_, i) => {
      const v = s.points[i];
      return v === null || v === undefined ? "" : withNoun(v, noun);
    }),
  });
  return (
    <SeriesLinesChart
      categories={dayLabels.map(fmtDay)}
      series={series.map((s) => toChart(s, valueNoun))}
      max={max}
      yTicks={axisTicks(max, integer, nf)}
      secondary={
        secondary
          ? { ...toChart(secondary.series, secondary.valueNoun), max: secondaryMax, yTicks: axisTicks(secondaryMax, secondaryInteger, nf) }
          : undefined
      }
      hideZeroInTip={hideZeroInTip}
      chartLabel={chartLabel}
    />
  );
}

/**
 * Shared legend row (the template's ChartLegends); the caller resolves labels and keeps series
 * order stable. An optional pre-formatted figure sits under each label.
 */
export function SeriesLegend({
  entries,
}: {
  entries: { label: string; colorVar: string; /** Optional pre-formatted figure, resolved by the caller. */ value?: string }[];
}) {
  return <SeriesLegendView entries={entries} />;
}

/** One pie slice: a label, its share value, and the colour token it carries on the other charts. */
export type PieSlice = { label: string; value: number; colorVar: string };

/**
 * Share label that never lies at the edges: a real sliver is "<1%", never "0%", and a near-whole
 * wedge is ">99%", never "100%" while another wedge exists.
 */
function sharePct(value: number, total: number): string {
  const p = (value / total) * 100;
  if (value > 0 && p < 1) return "<1%";
  if (value < total && p > 99) return ">99%";
  return `${Math.round(p)}%`;
}

/**
 * A DONUT of shares (template AppCurrentDownload): the total in the hole, captioned with the
 * caller's `valueNoun`, and a legend of label / figure / share under it. Slices are drawn in the
 * order given so the caller decides the ranking; a zero or negative value draws nothing.
 */
export function SeriesPie({
  slices,
  valueNoun,
  chartLabel,
  emptyLabel,
  formatValue,
}: {
  slices: PieSlice[];
  valueNoun: string;
  chartLabel: string;
  emptyLabel: string;
  formatValue?: (value: number) => string;
}) {
  const live = slices.filter((s) => Number.isFinite(s.value) && s.value > 0);
  const total = live.reduce((acc, s) => acc + s.value, 0);
  if (live.length === 0 || total <= 0) {
    return <EmptyState title={emptyLabel} />;
  }
  const fmt = formatValue ?? nf;
  return (
    <SeriesPieChart
      slices={live.map((s) => ({
        label: s.label,
        value: s.value,
        color: s.colorVar,
        valueLabel: fmt(s.value),
        legendValue: `${fmt(s.value)} ${valueNoun}`,
        pct: sharePct(s.value, total),
      }))}
      totalLabel={fmt(total)}
      centerCaption={valueNoun}
      chartLabel={chartLabel}
    />
  );
}
