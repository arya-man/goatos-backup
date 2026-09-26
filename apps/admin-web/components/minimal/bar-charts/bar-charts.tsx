"use client";

// Bar charts on the licensed MUI Minimal template's ApexCharts `Chart` + `useChart` base options.
// The server wrappers in components/svg-bars.tsx and components/svg-column-bars.tsx precompute
// every visible string (value labels, tooltip lines, category labels) and hand these charts only
// serializable props; the formatters here just look those strings up by dataPointIndex /
// seriesIndex, so no function ever crosses the server/client boundary.
//
// Template sources (next-ts/src/sections/overview):
// - HorizontalBars / StackedHorizontalBars: AnalyticsConversionRates (plotOptions.bar.horizontal,
//   barHeight 48%, borderRadius 2, dataLabels on, position top, offsetX -6, fontSize 10px).
//   Stacked adds AppAreaInstalled (chart.stacked, stroke 0, ChartLegends above the plot) with
//   borderRadiusWhenStacked "last".
// - ColumnBars: AnalyticsWebsiteVisits (grouped columns, stroke 2 transparent).
//
// Mesha rules kept on top of the template options:
// - every bar carries its value label, a zero included (it draws at the baseline). Labels sit
//   OUTSIDE the bar end (offsetX +6, textAnchor start) rather than the template's white text inside
//   it at -6, because a short bar cannot hold "1,061 · 63%" and Apex would hide it; the value axis
//   gets exactly enough headroom for the longest label, so nothing is clipped or dropped. Type is
//   12px, not the template's 10px: the mobile render-integrity floor is 11px;
// - a negative value draws left of a zero rule in the error colour, with its value text in the
//   same colour (weights losses, 339302965);
// - category labels are never dropped: a long pen name truncates with an ellipsis at a width that
//   follows the card (yaxis.labels.maxWidth) and the full text is the tooltip title;
// - many rows grow the chart up to GROW_BARS, then scroll inside the card in a VISIBLE_BARS window
//   with overscroll contained, never trapping the page scroll.

import { useEffect, useRef, useState } from "react";

import Box from "@mui/material/Box";
import { useTheme } from "@mui/material/styles";

import { chartColors } from "@/components/app/chart-colors";
import { axisCeiling } from "@/components/chart-scale";
import { Chart, ChartLegends, useChart, type ChartOptions } from "@/components/minimal/chart";

import { barsScroll, barsViewHeight, barsWindowHeight } from "./geometry";

// ----------------------------------------------------------------------

const escapeHtml = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);

/** Rough advance width of one glyph of the 12px value label; sizes the headroom that holds it. */
const VALUE_CHAR_PX = 7;
const VALUE_OFFSET_PX = 6;

type FormatterOpts = { seriesIndex?: number; dataPointIndex?: number };

/** Width of the element the chart draws into (0 before the first observation). */
function useHostWidth(): [React.RefObject<HTMLDivElement | null>, number] {
  const ref = useRef<HTMLDivElement | null>(null);
  const [w, setW] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return undefined;
    const ro = new ResizeObserver((es) => {
      const next = Math.floor(es[0]?.contentRect.width ?? 0);
      setW((prev) => (Math.abs(prev - next) < 1 ? prev : next));
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w];
}

/** Category label column: follows the card so a phone keeps ~18 readable characters of a pen name. */
function labelColumnPx(hostW: number): number {
  return Math.round(Math.min(Math.max(hostW * 0.36, 104), 280));
}

/**
 * Value-axis range with room for the value label at each bar end. `valuePx` is the widest label;
 * a side that holds bars (positive right, negative left) gets that much extra span.
 */
function valueDomain(values: number[], plotPx: number, valuePx: number): { min: number; max: number } {
  const hi = Math.max(0, ...values);
  const lo = Math.min(0, ...values);
  const sides = (hi > 0 || lo === 0 ? 1 : 0) + (lo < 0 ? 1 : 0);
  const span = hi - lo || 1;
  const usable = Math.max(plotPx - valuePx * sides, plotPx * 0.35);
  const perPx = span / usable;
  return {
    min: lo < 0 ? lo - valuePx * perPx : 0,
    max: hi > 0 || lo === 0 ? Math.max(hi, lo === 0 && hi === 0 ? 1 : hi) + valuePx * perPx : 0,
  };
}

/** The scroll window around a tall chart, or a plain host when every row fits. */
function BarsWindow({
  rows,
  chartLabel,
  hostRef,
  children,
}: {
  rows: number;
  chartLabel: string;
  hostRef: React.RefObject<HTMLDivElement | null>;
  children: React.ReactNode;
}) {
  const scrolls = barsScroll(rows);
  return (
    <Box
      ref={hostRef}
      role="img"
      aria-label={chartLabel}
      // Keyboard-reachable when it scrolls: a scroll region a keyboard user cannot focus is one
      // they cannot read past the window. A chart that fits adds no pointless tab stop.
      tabIndex={scrolls ? 0 : undefined}
      sx={
        scrolls
          ? {
              maxHeight: barsWindowHeight(rows),
              overflowY: "auto",
              overflowX: "hidden",
              overscrollBehavior: "contain",
              scrollbarGutter: "stable",
            }
          : undefined
      }
    >
      {children}
    </Box>
  );
}

// ----------------------------------------------------------------------

export type HorizontalBarRow = {
  key: string;
  label: string;
  value: number;
  /** Printed at the bar end, e.g. "1,061" or "1,061 · 63%". */
  valueLabel: string;
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
  const theme = useTheme();
  const [hostRef, hostW] = useHostWidth();
  const width = hostW || 600;
  const labelPx = labelColumnPx(width);
  const valuePx = rows.reduce((m, r) => Math.max(m, r.valueLabel.length), 1) * VALUE_CHAR_PX + VALUE_OFFSET_PX + 8;
  const domain = valueDomain(
    rows.map((r) => r.value),
    Math.max(80, width - labelPx - 32),
    valuePx,
  );
  const hasNegative = rows.some((r) => r.value < 0);
  // One tone by default: a ranked bar list is ONE measure, so a colour per row encodes nothing.
  const ramp = chartColors(theme);
  const colors = rows.map((r, i) => (r.value < 0 ? "var(--error)" : multiTone ? ramp[i % ramp.length] : "var(--primary)"));
  const labelColors = rows.map((r) => (r.value < 0 ? "var(--error)" : theme.vars.palette.text.primary));

  const chartOptions = useChart({
    colors,
    // One series, so `stacked` draws the same bars; it is set because Apex's unstacked path
    // clamps a start-anchored figure whose bar is shorter than the text back to the axis, which
    // printed "13,916" ON the short bars instead of after them.
    chart: { stacked: true },
    stroke: { width: 2, colors: ["transparent"] },
    legend: { show: false },
    plotOptions: {
      bar: {
        horizontal: true,
        borderRadius: 2,
        barHeight: "48%",
        // Distributed so each row owns its colour (and its value-label colour): a loss row turns red.
        distributed: true,
        dataLabels: { position: "top", hideOverflowingLabels: false },
      },
    },
    dataLabels: {
      enabled: true,
      textAnchor: "start",
      offsetX: VALUE_OFFSET_PX,
      formatter: (_v: unknown, opts?: FormatterOpts) => rows[opts?.dataPointIndex ?? -1]?.valueLabel ?? "",
      style: { fontSize: "12px", fontWeight: 600, colors: labelColors },
      background: { enabled: false },
      dropShadow: { enabled: false },
    },
    xaxis: {
      categories: rows.map((r) => r.label),
      min: domain.min,
      max: domain.max,
      // Every bar prints its own figure, so a value axis would only repeat them.
      labels: { show: false },
    },
    yaxis: {
      labels: {
        maxWidth: labelPx,
        style: { fontSize: "13px", colors: theme.vars.palette.text.secondary },
      },
    },
    grid: {
      padding: { top: -8, bottom: -8, right: 8 },
      xaxis: { lines: { show: false } },
      yaxis: { lines: { show: false } },
    },
    annotations: hasNegative
      ? { xaxis: [{ x: 0, borderColor: theme.vars.palette.text.disabled, strokeDashArray: 0 }] }
      : {},
    tooltip: {
      shared: true,
      intersect: false,
      x: { formatter: (v: number | string, opts?: FormatterOpts) => escapeHtml(rows[opts?.dataPointIndex ?? -1]?.label ?? String(v)) },
      y: {
        formatter: (_v: number, opts?: FormatterOpts) => escapeHtml(rows[opts?.dataPointIndex ?? -1]?.tipValue ?? ""),
        title: { formatter: () => "" },
      },
    },
  } satisfies ChartOptions);

  return (
    <BarsWindow rows={rows.length} chartLabel={chartLabel} hostRef={hostRef}>
      <Chart
        type="bar"
        series={[{ name: chartLabel, data: rows.map((r) => r.value) }]}
        options={chartOptions}
        deps={[rows]}
        sx={{ height: barsViewHeight(rows.length) }}
      />
    </BarsWindow>
  );
}

// ----------------------------------------------------------------------

export type StackedBarSeries = {
  key: string;
  name: string;
  /** A Mesha token, e.g. "var(--info)". */
  color: string;
  values: number[];
  /** Tooltip value per row, e.g. "28 animals". */
  tipValues: string[];
};

export type StackedBarRow = {
  key: string;
  label: string;
  /** Printed at the bar end: the head count and its split, e.g. "41 · 28 female · 13 male". */
  totalLabel: string;
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
  const theme = useTheme();
  const [hostRef, hostW] = useHostWidth();
  const width = hostW || 600;
  const labelPx = labelColumnPx(width);
  const valuePx = rows.reduce((m, r) => Math.max(m, r.totalLabel.length), 1) * VALUE_CHAR_PX + VALUE_OFFSET_PX + 8;
  const totals = rows.map((_, j) => series.reduce((sum, s) => sum + Math.max(0, s.values[j] ?? 0), 0));
  const domain = valueDomain(totals, Math.max(80, width - labelPx - 32), valuePx);

  const chartOptions = useChart({
    chart: { stacked: true },
    colors: series.map((s) => s.color),
    stroke: { width: 0 },
    legend: { show: false },
    plotOptions: {
      bar: {
        horizontal: true,
        borderRadius: 2,
        borderRadiusApplication: "end",
        borderRadiusWhenStacked: "last",
        barHeight: "48%",
        dataLabels: {
          // The head count and its split, printed beside the bar rather than left to a hover: a
          // segment can be one animal wide, which is a hover target nobody can hit.
          total: {
            enabled: true,
            offsetX: VALUE_OFFSET_PX,
            formatter: (_v: unknown, opts?: FormatterOpts) => rows[opts?.dataPointIndex ?? -1]?.totalLabel ?? "",
            style: { fontSize: "12px", fontWeight: 600, color: theme.vars.palette.text.primary },
          },
        },
      },
    },
    // Per-segment labels off: the total label carries every segment's figure.
    dataLabels: { enabled: false },
    xaxis: {
      categories: rows.map((r) => r.label),
      min: domain.min,
      max: domain.max,
      labels: { show: false },
    },
    yaxis: {
      labels: {
        maxWidth: labelPx,
        style: { fontSize: "13px", colors: theme.vars.palette.text.secondary },
      },
    },
    grid: {
      padding: { top: -8, bottom: -8, right: 8 },
      xaxis: { lines: { show: false } },
      yaxis: { lines: { show: false } },
    },
    tooltip: {
      shared: true,
      intersect: false,
      hideEmptySeries: true,
      x: { formatter: (v: number | string, opts?: FormatterOpts) => escapeHtml(rows[opts?.dataPointIndex ?? -1]?.label ?? String(v)) },
      y: {
        formatter: (_v: number, opts?: FormatterOpts) =>
          escapeHtml(series[opts?.seriesIndex ?? -1]?.tipValues[opts?.dataPointIndex ?? -1] ?? ""),
      },
    },
  } satisfies ChartOptions);

  return (
    <>
      {/* The legend is not optional on a stacked chart: without it the colours inside a bar name
          nothing. Template ChartLegends, above the plot as in AppAreaInstalled. */}
      <ChartLegends
        labels={series.map((s) => s.name)}
        colors={series.map((s) => s.color)}
        slotProps={{ value: { sx: { display: "none" } } }}
        sx={{ px: 1, pb: 1, gap: 3 }}
      />
      <BarsWindow rows={rows.length} chartLabel={chartLabel} hostRef={hostRef}>
        <Chart
          type="bar"
          series={series.map((s) => ({ name: s.name, data: s.values }))}
          options={chartOptions}
          deps={[rows, series]}
          sx={{ height: barsViewHeight(rows.length) }}
        />
      </BarsWindow>
    </>
  );
}

// ----------------------------------------------------------------------

export type ColumnBarSeries = {
  key: string;
  name: string;
  /** A Mesha token, e.g. "var(--brand)". */
  color: string;
  values: (number | null)[];
  /** Printed above each column (zero included) and used as the tooltip value. */
  labels: string[];
};

/** Narrowest a day's slot may get before the strip scrolls sideways inside the card. */
const COLUMN_SLOT_PX = 48;
const COLUMN_CHART_PX = 260;
/** Room the rotated category labels and the plot padding take out of the chart height. */
const COLUMN_AXIS_PX = 84;

export function ColumnBars({
  categories,
  series,
  chartLabel,
}: {
  categories: string[];
  series: ColumnBarSeries[];
  chartLabel: string;
}) {
  const theme = useTheme();
  const top = Math.max(0, ...series.flatMap((s) => s.values.map((v) => v ?? 0)));
  const longest = series.reduce((m, s) => s.labels.reduce((n, l) => Math.max(n, l.length), m), 1);
  // Value labels stand VERTICALLY above each column (two series per day leave ~10px per column),
  // so the value axis needs head-room for the longest one; axisCeiling keeps the ticks round.
  const plotPx = COLUMN_CHART_PX - COLUMN_AXIS_PX;
  const labelPx = longest * VALUE_CHAR_PX + VALUE_OFFSET_PX;
  const yMax = axisCeiling(Math.max(1, top) * (plotPx / Math.max(plotPx - labelPx, plotPx * 0.4)), true);

  const chartOptions = useChart({
    colors: series.map((s) => s.color),
    stroke: { width: 2, colors: ["transparent"] },
    legend: { show: false },
    plotOptions: {
      bar: {
        columnWidth: "64%",
        dataLabels: { position: "top", orientation: "vertical", hideOverflowingLabels: false },
      },
    },
    dataLabels: {
      enabled: true,
      offsetY: 4,
      formatter: (_v: unknown, opts?: FormatterOpts) => series[opts?.seriesIndex ?? -1]?.labels[opts?.dataPointIndex ?? -1] ?? "",
      style: { fontSize: "11px", fontWeight: 600, colors: [theme.vars.palette.text.secondary] },
      background: { enabled: false },
      dropShadow: { enabled: false },
    },
    xaxis: {
      categories,
      // Every day keeps its label: rotated when the slot is narrow, never thinned out.
      labels: { rotate: -45, rotateAlways: false, hideOverlappingLabels: false, trim: false, maxHeight: 80, style: { fontSize: "11px" } },
    },
    yaxis: {
      min: 0,
      max: yMax,
      // Whole-number steps only: 50 in quarters drew 12.5 rounded to "38" and "13".
      tickAmount: [4, 5, 3, 2].find((n) => Number.isInteger(yMax / n)) ?? 4,
      labels: { formatter: (v: number) => (Number.isFinite(v) ? Math.round(v).toLocaleString("en-IN") : "") },
    },
    tooltip: {
      shared: true,
      intersect: false,
      x: { formatter: (v: number | string, opts?: FormatterOpts) => escapeHtml(categories[opts?.dataPointIndex ?? -1] ?? String(v)) },
      y: {
        formatter: (_v: number, opts?: FormatterOpts) =>
          escapeHtml(series[opts?.seriesIndex ?? -1]?.labels[opts?.dataPointIndex ?? -1] ?? ""),
      },
    },
  } satisfies ChartOptions);

  return (
    // The strip keeps a readable slot per day and scrolls sideways INSIDE the card on a phone,
    // with overscroll contained so a swipe never drags the page.
    <Box role="img" aria-label={chartLabel} sx={{ overflowX: "auto", overflowY: "hidden", overscrollBehaviorX: "contain" }}>
      <Box sx={{ minWidth: categories.length * COLUMN_SLOT_PX }}>
        <Chart
          type="bar"
          series={series.map((s) => ({ name: s.name, data: s.values }))}
          options={chartOptions}
          deps={[categories, series]}
          sx={{ height: COLUMN_CHART_PX }}
        />
      </Box>
    </Box>
  );
}

