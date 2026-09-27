"use client";

// Grouped (multi-series) column chart for full-width report cards: the load-wise counts and money
// charts on the sales board and Counts Breakdown.
//
// The template's ApexCharts `Chart` on AnalyticsWebsiteVisits' options (grouped columns,
// transparent 2px stroke between bars) with the template's ChartLegends above the plot
// (AppAreaInstalled). useChart's base options are untouched: no data labels (the figures live in
// the tooltip), no hover-state override, the template tooltip, palette colours. NO copy of its own:
// every visible string arrives resolved from the backend page contract by the caller.
import { useMemo } from "react";
import { useTheme } from "@mui/material/styles";
import { chartColor } from "./app/chart-colors";
import { EmptyState } from "./app/empty-state";
import { Chart, useChart, type ChartOptions } from "./minimal/chart";
import { niceCeiling } from "./chart-scale";
import { SeriesLegendView } from "./series-charts";
import { inrAxisTick, numAxisTick } from "@/features/procurement/sales-format";

// Series colour per tone, a palette channel each (components/app/chart-colors), so no two tone
// names paint the same bar.
const TONE_CHANNEL: Record<GroupedSeriesTone, string> = {
  info: "info",
  ok: "primary.dark",
  danger: "error",
  warn: "warning",
  okHatch: "primary.dark",
};

// `okHatch` is the sold green, striped: a figure that is sold-LIKE but not realised -- animals
// tagged to a sale that has not closed, or stock carried at an assumed price (main 7765efb29). A
// fifth solid colour would read as one of the four; the stripe keeps it distinct at a glance, on
// the bar (Apex pattern fill) and on its legend dot.
export type GroupedSeriesTone = "info" | "ok" | "danger" | "warn" | "okHatch";

/** The okHatch stripe: an SVG pattern tile edge and its line weight (chart units, not layout). */
const HATCH_TILE = 6;
const HATCH_STROKE = 2;
/** AnalyticsWebsiteVisits chart height. */
export const GROUPED_COLUMNS_HEIGHT = 364;
const CHART_HEIGHT = GROUPED_COLUMNS_HEIGHT;

export type GroupedSeries = {
  key: string;
  /** Resolved from the page contract by the caller; names the series in the legend and tooltips. */
  label: string;
  tone: GroupedSeriesTone;
  /**
   * The key of an EARLIER series this one stacks on top of, in the same slot (e.g. assumed value
   * on top of sold value). A stacked series draws no slot of its own; the pair is one column whose
   * height is their sum, and the scale is computed over those sums.
   */
  stackOn?: string;
};

export type GroupedDatum = {
  key: string;
  /** Short axis label, e.g. "12 Aug". */
  axisLabel: string;
  /** Full tooltip label, e.g. "Nutriplus · 12 Aug 2026". */
  label: string;
  /**
   * One entry per series, in series order. `null` means the figure is NOT RECORDED — the bar is
   * absent (not zero-height) and the tooltip shows the caller's display string for that state.
   */
  values: (number | null)[];
  /** One display string per series, shown in tooltips (e.g. "₹5.2L" or "Cost not recorded"). */
  displays: string[];
  /** Optional second line under the full label in the tooltip (e.g. the vendor, or "97 of 100 sold"). */
  subLabel?: string;
  /** Extra lines shown ONLY in the hover card (e.g. the day the load reached the farm). */
  tipLines?: string[];
};

const escapeHtml = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);

export function GroupedColumns({
  series,
  data,
  chartLabel,
  emptyLabel,
  money = false,
}: {
  series: GroupedSeries[];
  data: GroupedDatum[];
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
  /** The series are rupees: the axis ticks carry the ₹ prefix. */
  money?: boolean;
}) {
  const theme = useTheme();
  const hasAnyValue = data.some((d) => d.values.some((v) => v !== null && v !== 0));
  // A series with `stackOn` draws on top of that EARLIER series in the same column (Apex grouped
  // stacking: both share one `group`); the scale is computed over the column sums.
  const baseIndex = (i: number) => {
    const s = series[i];
    if (!s.stackOn) return -1;
    const base = series.findIndex((b) => b.key === s.stackOn);
    return base >= 0 && base < i ? base : -1;
  };
  const hasStack = series.some((_, i) => baseIndex(i) >= 0);
  const positive = (v: number | null | undefined) => (v != null && v > 0 ? v : 0);
  const columnValue = (d: GroupedDatum, i: number) =>
    baseIndex(i) >= 0
      ? 0
      : positive(d.values[i]) + series.reduce((sum, _, j) => (baseIndex(j) === i ? sum + positive(d.values[j]) : sum), 0);
  const max = niceCeiling(Math.max(1, ...data.flatMap((d) => series.map((_, i) => columnValue(d, i)))));
  // Apex groups: every series is its own column unless it stacks on an earlier one.
  const groupOf = (i: number) => series[baseIndex(i) >= 0 ? baseIndex(i) : i].key;
  const axisTick = money ? inrAxisTick : numAxisTick;
  const colors = series.map((s) => chartColor(theme, TONE_CHANNEL[s.tone]));
  const options = useMemo<ChartOptions>(
    () => ({
      colors,
      ...(hasStack ? { chart: { stacked: true } } : null),
      // Solid bars, apart from the striped okHatch series.
      fill: {
        type: series.map((s) => (s.tone === "okHatch" ? "pattern" : "solid")),
        opacity: 1,
        pattern: { style: "slantedLines", width: HATCH_TILE, height: HATCH_TILE, strokeWidth: HATCH_STROKE },
      },
      stroke: { width: 2, colors: ["transparent"] },
      xaxis: { categories: data.map((d) => d.axisLabel) },
      // Four round ticks on a nice ceiling, ONE unit per axis picked from the top.
      yaxis: { min: 0, max, tickAmount: 4, labels: { formatter: (v: number) => axisTick(v, max) } },
      tooltip: {
        shared: true,
        intersect: false,
        x: {
          formatter: (_v: unknown, opts?: { dataPointIndex?: number }) => {
            const datum = data[opts?.dataPointIndex ?? -1];
            if (!datum) return "";
            // Extra hover-only lines (e.g. the day the load reached the farm) ride under the label.
            const extra = datum.tipLines?.map((line) => `<br/>${escapeHtml(line)}`).join("") ?? "";
            return `${escapeHtml(datum.label)}${extra}${datum.subLabel ? `<br/>${escapeHtml(datum.subLabel)}` : ""}`;
          },
        },
        y: {
          formatter: (v: number, opts?: { seriesIndex: number; dataPointIndex: number }) =>
            (opts ? data[opts.dataPointIndex]?.displays[opts.seriesIndex] : undefined) ?? (v == null ? "" : v.toLocaleString("en-IN")),
        },
      },
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [colors.join(), series, data, max, money, hasStack],
  );
  const chartOptions = useChart(options);

  if (data.length === 0 || !hasAnyValue) {
    return <EmptyState title={emptyLabel} />;
  }
  const chartSeries = series.map((s, i) => ({
    name: s.label,
    data: data.map((d) => d.values[i]),
    ...(hasStack ? { group: groupOf(i) } : null),
  }));
  return (
    <>
      <SeriesLegendView entries={series.map((s) => ({ label: s.label, colorVar: TONE_CHANNEL[s.tone], hatched: s.tone === "okHatch" }))} />
      <Chart
        type="bar"
        series={chartSeries}
        options={chartOptions}
        role="img"
      aria-label={chartLabel}
        slotProps={{ loading: { p: 2.5 } }}
        sx={{ pl: 1, py: 2.5, pr: 2.5, height: CHART_HEIGHT }}
      />
    </>
  );
}
