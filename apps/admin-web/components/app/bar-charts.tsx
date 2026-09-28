"use client";

// Bar charts on the licensed MUI Minimal template's ApexCharts `Chart` + `useChart`, with the base
// options untouched: no data labels (figures live in the tooltip and on the value axis), no
// hover-state override, the template tooltip, palette colours (components/app/chart-colors). The
// server wrappers in components/svg-bars.tsx and components/svg-column-bars.tsx precompute every
// visible string and hand these charts only serializable props; the formatters here only look
// those strings up by dataPointIndex / seriesIndex.
//
// Template sources (next-ts/src/sections/overview):
// - HorizontalBars / StackedHorizontalBars: AnalyticsConversionRates (horizontal, barHeight 48%,
//   borderRadius 2, transparent 2px stroke); stacked adds AppAreaInstalled (chart.stacked,
//   ChartLegends above the plot).
// - ColumnBars: AnalyticsWebsiteVisits (grouped columns, transparent 2px stroke).
//
// Mesha data rules kept: a negative value draws left of zero in the error colour; category labels
// truncate at a width that follows the card and the full text is the tooltip title.

import { useMemo } from "react";


import { chartColor, useChartTheme } from "@/components/app/chart-colors";
import { Chart, ChartLegends, useChart, type ChartOptions } from "@/components/minimal/chart";

const escapeHtml = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);

type FormatterOpts = { seriesIndex?: number; dataPointIndex?: number };

/** Height of one bar row in CSS px; the template bar (barHeight 48%) sits inside it. */
const BAR_ROW_PX = 40;
/** Value axis + plot padding. */
const BAR_CHROME_PX = 72;

/** Chart height for `count` rows: the chart grows with its rows, never scrolls inside the card. */
export function barsViewHeight(count: number): number {
  return Math.max(3, count) * BAR_ROW_PX + BAR_CHROME_PX;
}

const chartSx = (height: number) => ({ pl: 1, py: 2.5, pr: 2.5, height });

// ----------------------------------------------------------------------

export type HorizontalBarRow = {
  key: string;
  label: string;
  value: number;
  /** Tooltip value line, e.g. "1,061 cases · 63%". */
  tipValue: string;
};

export function HorizontalBars({
  rows,
  chartLabel,
  multiTone = false,
}: {
  rows: HorizontalBarRow[];
  chartLabel: string;
  /** Rotating categorical palette instead of one brand tone. */
  multiTone?: boolean;
}) {
  const theme = useChartTheme();
  // One tone by default: a ranked bar list is ONE measure, so a colour per row encodes nothing.
  const colors = rows.map((r, i) =>
    r.value < 0 ? chartColor(theme, "error") : multiTone ? chartColor(theme, ["primary.dark", "warning", "info", "secondary", "grey.500"][i % 5]) : chartColor(theme, "primary.dark"),
  );
  const options = useMemo<ChartOptions>(
    () => ({
      colors,
      stroke: { width: 2, colors: ["transparent"] },
      plotOptions: {
        // Distributed so each row owns its colour: a loss row turns red.
        bar: { horizontal: true, barHeight: "48%", distributed: true },
      },
      xaxis: { categories: rows.map((r) => r.label) },
      tooltip: {
        shared: true,
        intersect: false,
        x: { formatter: (v: number | string, opts?: FormatterOpts) => escapeHtml(rows[opts?.dataPointIndex ?? -1]?.label ?? String(v)) },
        y: {
          formatter: (_v: number, opts?: FormatterOpts) => escapeHtml(rows[opts?.dataPointIndex ?? -1]?.tipValue ?? ""),
          title: { formatter: () => "" },
        },
      },
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [colors.join(), rows],
  );
  const chartOptions = useChart(options);
  return (
    <Chart
      type="bar"
      series={[{ name: chartLabel, data: rows.map((r) => r.value) }]}
      options={chartOptions}
      role="img"
      aria-label={chartLabel}
      slotProps={{ loading: { p: 2.5 } }}
      sx={chartSx(barsViewHeight(rows.length))}
    />
  );
}

// ----------------------------------------------------------------------

export type StackedBarSeries = {
  key: string;
  name: string;
  /** A palette channel ("info") or a legacy Mesha token; see chart-colors. */
  color: string;
  values: number[];
  /** Tooltip value per row, e.g. "28 animals". */
  tipValues: string[];
};

export type StackedBarRow = {
  key: string;
  label: string;
  /** Tooltip title: the row and its split, e.g. "Pen 4 · 41 · 28 female · 13 male". */
  title: string;
};

export function StackedHorizontalBars({
  rows,
  series,
  chartLabel,
}: {
  rows: StackedBarRow[];
  series: StackedBarSeries[];
  chartLabel: string;
}) {
  const theme = useChartTheme();
  const colors = series.map((s) => chartColor(theme, s.color));
  const options = useMemo<ChartOptions>(
    () => ({
      chart: { stacked: true },
      colors,
      stroke: { width: 0 },
      plotOptions: { bar: { horizontal: true, barHeight: "48%" } },
      xaxis: { categories: rows.map((r) => r.label) },
      tooltip: {
        shared: true,
        intersect: false,
        hideEmptySeries: true,
        x: { formatter: (v: number | string, opts?: FormatterOpts) => escapeHtml(rows[opts?.dataPointIndex ?? -1]?.title ?? String(v)) },
        y: {
          formatter: (_v: number, opts?: FormatterOpts) => escapeHtml(series[opts?.seriesIndex ?? -1]?.tipValues[opts?.dataPointIndex ?? -1] ?? ""),
        },
      },
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [colors.join(), rows, series],
  );
  const chartOptions = useChart(options);
  return (
    <>
      {/* The legend is not optional on a stacked chart: without it the colours inside a bar name
          nothing. Template ChartLegends, above the plot as in AppAreaInstalled. */}
      <ChartLegends labels={series.map((s) => s.name)} colors={colors} sx={{ px: 3, gap: (theme) => theme.spacing(3) }} />
      <Chart
        type="bar"
        series={series.map((s) => ({ name: s.name, data: s.values }))}
        options={chartOptions}
        role="img"
      aria-label={chartLabel}
        slotProps={{ loading: { p: 2.5 } }}
        sx={chartSx(barsViewHeight(rows.length))}
      />
    </>
  );
}

// ----------------------------------------------------------------------

export type ColumnBarSeries = {
  key: string;
  name: string;
  /** A palette channel ("primary.dark") or a legacy Mesha token; see chart-colors. */
  color: string;
  values: (number | null)[];
  /** Tooltip value per column. */
  labels: string[];
};

export function ColumnBars({
  categories,
  series,
  chartLabel,
}: {
  categories: string[];
  series: ColumnBarSeries[];
  chartLabel: string;
}) {
  const theme = useChartTheme();
  const colors = series.map((s) => chartColor(theme, s.color));
  const options = useMemo<ChartOptions>(
    () => ({
      colors,
      stroke: { width: 2, colors: ["transparent"] },
      legend: { show: series.length > 1 },
      xaxis: { categories },
      yaxis: { min: 0, labels: { formatter: (v: number) => (Number.isFinite(v) ? Math.round(v).toLocaleString("en-IN") : "") } },
      tooltip: {
        shared: true,
        intersect: false,
        x: { formatter: (v: number | string, opts?: FormatterOpts) => escapeHtml(categories[opts?.dataPointIndex ?? -1] ?? String(v)) },
        y: {
          formatter: (_v: number, opts?: FormatterOpts) => escapeHtml(series[opts?.seriesIndex ?? -1]?.labels[opts?.dataPointIndex ?? -1] ?? ""),
        },
      },
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [colors.join(), categories, series],
  );
  const chartOptions = useChart(options);
  return (
    <Chart
      type="bar"
      series={series.map((s) => ({ name: s.name, data: s.values }))}
      options={chartOptions}
      role="img"
      aria-label={chartLabel}
      slotProps={{ loading: { p: 2.5 } }}
      sx={chartSx(364)}
    />
  );
}
