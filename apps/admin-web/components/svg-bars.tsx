// (The svg- file name is historical: nothing here draws SVG by hand any more; the importers keep
// the path so no page changes.)
//
// Horizontal bar charts (plain and stacked) on the licensed MUI Minimal template's ApexCharts
// `Chart`: AnalyticsConversionRates for the bars, AppAreaInstalled for the stack. The drawing
// lives in the client component components/app/bar-charts.tsx.
//
// These exports stay SERVER-SAFE wrappers (no "use client") so the server pages that call them do
// not change: every visible string -- the value label beside each bar, its share, the tooltip
// line, the stacked split -- is composed HERE and handed to the client chart as plain data, so no
// formatter or function ever crosses the server/client boundary.
//
// Every colour is a palette channel name (or a legacy token), resolved to the active scheme's
// theme colour by components/app/chart-colors in the client chart.
//
// This component renders NO copy of its own. Titles, captions, legends, empty states and the
// value noun are all passed in already resolved from the backend page contract by the caller.

import { EmptyState } from "./app/empty-state";
import { HorizontalBars, StackedHorizontalBars, type HorizontalBarRow, type StackedBarRow, type StackedBarSeries } from "./app/bar-charts";


export type SvgBarDatum = {
  key: string;
  label: string;
  value: number;
};

const count = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 1 });

/**
 * A bar's share of the series, as the label beside its count.
 *
 * The denominator is the sum of EVERY datum handed in, not of the bars actually drawn:
 * with `maxBars` truncating a long tail, sharing against the visible bars would inflate
 * each one and the column would stop adding to 100.
 *
 * A non-zero value that rounds to nothing renders "<1%" rather than "0%", because a bar
 * that is visibly there while its label says zero reads as a bug. Whole percentages
 * otherwise — a herd census does not support a decimal place.
 */
function shareLabel(value: number, total: number): string {
  if (total <= 0) return "";
  const share = (value / total) * 100;
  if (share > 0 && share < 0.5) return "<1%";
  return `${Math.round(share)}%`;
}

export function SvgBars({
  data,
  emptyLabel,
  valueNoun,
  chartLabel,
  maxBars = 8,
  showShare = false,
  multiTone = false,
}: {
  data: SvgBarDatum[];
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
  /** Resolved from the page contract by the caller; used in per-bar tooltips. */
  valueNoun: string;
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  maxBars?: number;
  /**
   * Renders each bar's share of the series beside its count.
   *
   * OPT-IN, because a share is only meaningful where the series PARTITIONS one whole.
   * On a series of unrelated magnitudes, or one capped by `maxBars` so the visible bars are a
   * sample rather than the set, a percentage would invent a denominator the chart cannot back.
   */
  showShare?: boolean;
  /**
   * Rotating series palette instead of one brand tone. A plain ranked bar list is one measure,
   * so one tone; only a caller whose rows are genuinely different series should paint them
   * differently, and such a caller owes the reader a legend.
   */
  multiTone?: boolean;
}) {
  // A zero draws no bar here (callers name empty buckets under the chart); a loss is real data
  // and draws left of the zero rule in the error colour.
  const bars = data.filter((d) => Number.isFinite(d.value) && d.value !== 0).slice(0, maxBars);
  // Shared against EVERY datum handed in, including any the cap or the zero filter dropped,
  // so the visible percentages never add to more than the whole they came from.
  const total = showShare ? data.reduce((sum, d) => sum + d.value, 0) : 0;

  if (bars.length === 0) {
    return <EmptyState title={emptyLabel} />;
  }

  const rows: HorizontalBarRow[] = bars.map((datum) => {
    const share = shareLabel(datum.value, total);
    const figure = count(datum.value);
    return {
      key: datum.key,
      label: datum.label,
      value: datum.value,
      tipValue: share ? `${figure} ${valueNoun} · ${share}` : `${figure} ${valueNoun}`,
    };
  });

  return <HorizontalBars rows={rows} chartLabel={chartLabel} multiTone={multiTone} />;
}

// ---------------------------------------------------------------------------------------------
// Stacked bars.
//
// Same row geometry and scroll window as SvgBars, so a stacked chart sitting under a plain one
// lines up with it. One bar is DIVIDED between named segments, and the colour follows the
// SEGMENT, because the third bar's blue must mean the same thing as the first bar's blue. Every
// segment actually drawn is named in the chart's legend.

export type SvgStackedSegment = {
  key: string;
  /** Resolved from the page contract by the caller; used in the legend and tooltip. */
  label: string;
  value: number;
  /** A CSS custom property, never a hex literal — see the file header. */
  colorVar: string;
};

export type SvgStackedDatum = {
  key: string;
  label: string;
  /** Head count printed beside the bar. The caller supplies it rather than it being summed here,
   *  so the figure is the backend's own head count. */
  total: number;
  segments: SvgStackedSegment[];
};

/**
 * The split that sits beside a bar's head count, as "28 female · 13 male".
 *
 * A bar with only ONE segment filled prints just the segment's NAME — "female" after the 803 —
 * rather than repeating the number it already shows. Naming it matters: a stage that is female by
 * definition still has to say so, or a reader running down the column cannot tell a single-sex
 * stage from one whose split was never worked out.
 */
function segmentText(datum: SvgStackedDatum): string {
  const filled = datum.segments.filter((segment) => segment.value > 0);
  if (filled.length === 0) return "";
  if (filled.length === 1) return filled[0].label.toLowerCase();
  return filled
    .map((segment) => `${segment.value.toLocaleString("en-IN")} ${segment.label.toLowerCase()}`)
    .join(" · ");
}

export function SvgStackedBars({
  data,
  emptyLabel,
  valueNoun,
  chartLabel,
  maxBars = 8,
}: {
  data: SvgStackedDatum[];
  emptyLabel: string;
  valueNoun: string;
  chartLabel: string;
  maxBars?: number;
}) {
  const bars = data.filter((datum) => datum.total > 0).slice(0, maxBars);
  if (bars.length === 0) {
    return <EmptyState title={emptyLabel} />;
  }

  // Segment order and identity come from the first bar; every bar carries the same segment keys.
  // A segment that carries no animal on ANY bar is left out, so the legend never advertises a key
  // the chart does not draw.
  const series: StackedBarSeries[] = bars[0].segments
    .map((segment) => {
      const values = bars.map((bar) => bar.segments.find((s) => s.key === segment.key)?.value ?? 0);
      return {
        key: segment.key,
        name: segment.label,
        color: segment.colorVar,
        values,
        tipValues: values.map((value) => `${value.toLocaleString("en-IN")} ${valueNoun}`),
      };
    })
    .filter((s) => s.values.some((value) => value > 0));

  const rows: StackedBarRow[] = bars.map((datum) => {
    const breakdown = segmentText(datum);
    const figure = datum.total.toLocaleString("en-IN");
    return { key: datum.key, label: datum.label, title: `${datum.label} · ${breakdown ? `${figure} · ${breakdown}` : figure}` };
  });

  return <StackedHorizontalBars rows={rows} series={series} chartLabel={chartLabel} />;
}
