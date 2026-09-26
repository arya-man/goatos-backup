"use client";

// Client half of components/svg-series.tsx: the template's ApexCharts `Chart` + `useChart` base
// options, fed ONLY serializable props. The server wrapper in svg-series.tsx has already composed
// every visible string (DD/MM/YYYY categories, tooltip figures per series per point, y tick labels
// for a fixed axis top), so nothing here formats a number or a date and nothing here owns copy.
// Style follows components/app/trend-chart.tsx (TrendChart).

import { useEffect, useRef, useState } from "react";
import Divider from "@mui/material/Divider";
import { useTheme } from "@mui/material/styles";
import { Chart, ChartLegends, useChart, type ChartOptions } from "@/components/minimal/chart";
import { chartClasses } from "@/components/minimal/chart/classes";

export type AxisTick = { value: number; label: string };

type TipRow = { label: string; value: string };

type Series<T> = {
  name: string;
  /** A theme token (`var(--x)`), resolved against the page for Apex (a past-twelve tint may be a blend). */
  color: string;
  data: T[];
  /** Tooltip figure per point, composed by the server; "" where the point has no figure. */
  tips: string[];
};

/** Chart heights (template overview cards run 280-364 on desktop). */
const STACKED_HEIGHT = { xs: 260, md: 320 } as const;
const LINES_HEIGHT = { xs: 240, md: 280 } as const;
const PIE_SIZE = 240;
/** Smallest reserved width for a y tick column. */
const Y_MIN_WIDTH = 36;

const escapeHtml = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);

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

/**
 * Time-axis labels at a REGULAR step from the first point, sized to the plot width; the final
 * point is always labelled, and a step tick closer than one full step to it yields so the last two
 * labels never crowd (TrendChart's rule).
 */
function regularTickIndexes(categories: string[], plotW: number): Set<number> {
  const out = new Set<number>();
  if (categories.length === 0) return out;
  const longest = categories.reduce((m, l) => Math.max(m, l.length), 1);
  const labelPx = Math.min(longest, 16) * 6.6 + 12;
  const step = Math.max(1, Math.ceil(categories.length / Math.max(1, Math.floor(plotW / (labelPx * 1.5)))));
  const idx: number[] = [];
  for (let i = 0; i < categories.length; i += step) idx.push(i);
  const last = categories.length - 1;
  if (idx[idx.length - 1] !== last) {
    if (last - idx[idx.length - 1] < step && idx.length > 1) idx.pop();
    idx.push(last);
  }
  idx.forEach((i) => out.add(i));
  return out;
}

/** Tick column width from its widest precomputed label, so a long figure widens the axis, never clips. */
const yWidthOf = (ticks: AxisTick[]) => Math.max(Y_MIN_WIDTH, ticks.reduce((m, t) => Math.max(m, t.label.length), 0) * 7 + 12);

/** Same figure within floating-point noise of the scale. */
const near = (a: number, b: number, scale: number) => scale * 1e-6 >= Math.abs(a - b);

/** The precomputed label for an axis value; every other value Apex asks about stays blank. */
const tickLabel = (ticks: AxisTick[], max: number) => (v: number) => ticks.find((t) => near(t.value, v, max))?.label ?? "";

/**
 * Tooltip figure for one point. With `hideZero`, Apex's hideEmptySeries drops a row whose figure
 * parses to 0, so a zero reading answers "0" (hidden) and a real figure that merely starts with a
 * zero digit ("0 kg" for 0.04) is guarded with a word joiner so it stays.
 */
function tipFigure(series: { tips: string[] }[], hideZero: boolean) {
  return (value: number, opts?: { seriesIndex?: number; dataPointIndex?: number }) => {
    const label = series[opts?.seriesIndex ?? -1]?.tips[opts?.dataPointIndex ?? -1] ?? "";
    if (!hideZero) return label;
    if (value === 0) return "0";
    return Number.parseFloat(label) === 0 ? `⁠${label}` : label;
  };
}

/** Tooltip title: the DD/MM/YYYY category, then any extra pre-formatted rows under it. */
function tipTitle(categories: string[], extras?: TipRow[][]) {
  return (_v: number | string, opts?: { dataPointIndex?: number }) => {
    const i = opts?.dataPointIndex ?? -1;
    const title = escapeHtml(categories[i] ?? String(_v));
    const rows = extras?.[i] ?? [];
    return rows.length === 0 ? title : `${title}${rows.map((r) => `<br/>${escapeHtml(r.label)}: <b>${escapeHtml(r.value)}</b>`).join("")}`;
  };
}

export type StackedColumnsChartProps = {
  categories: string[];
  series: Series<number>[];
  /** Extra tooltip rows per slot in a different unit (e.g. the rupees behind a head count). */
  extras: TipRow[][];
  max: number;
  yTicks: AxisTick[];
  hideZeroInTip: boolean;
  chartLabel: string;
  /**
   * Figure printed on top of each column (the stack total), composed by the server; "" prints
   * nothing (no figure recorded). When given, EVERY slot carries its category and the plot keeps
   * a minimum column width, scrolling sideways inside the card on a phone instead of thinning.
   */
  columnFigures?: string[];
};

/** Two-line axis label ("Apr" over "2025") for a labelled-every-slot month axis. */
const twoLine = (label: string) => {
  const cut = label.lastIndexOf(" ");
  return cut > 0 ? [label.slice(0, cut), label.slice(cut + 1)] : label;
};

/** Stacked columns on the template's AppAreaInstalled options (stacked, stroke 0, 40% columns). */
export function StackedColumnsChart({ categories, series, extras, max, yTicks, hideZeroInTip, chartLabel, columnFigures }: StackedColumnsChartProps) {
  const theme = useTheme();
  const [hostRef, hostW] = useHostWidth();
  const colors = series.map((s) => s.color);
  const yWidth = yWidthOf(yTicks);
  const everySlot = columnFigures !== undefined;
  const ticks = regularTickIndexes(categories, Math.max(0, (hostW || 600) - yWidth - 24));
  const figureAt = (_v?: unknown, opts?: { dataPointIndex?: number }) => columnFigures?.[opts?.dataPointIndex ?? -1] ?? "";
  const figureStyle = { fontWeight: 600, color: theme.vars.palette.text.secondary };
  const chartOptions = useChart({
    colors,
    chart: { stacked: true },
    stroke: { width: 0 },
    legend: { show: false },
    xaxis: everySlot
      ? { categories: categories.map(twoLine), labels: { rotate: 0, hideOverlappingLabels: false, trim: false }, tooltip: { enabled: false } }
      : {
          categories,
          overwriteCategories: categories.map((l, i) => (ticks.has(i) ? l : "")),
          labels: { rotate: 0, hideOverlappingLabels: false, trim: false },
          tooltip: { enabled: false },
        },
    yaxis: { min: 0, max, tickAmount: 4, labels: { minWidth: yWidth, formatter: tickLabel(yTicks, max) } },
    // One series: the figure rides the column top (a measured 0 sits on the baseline). Stacked:
    // one total above the whole stack, never a figure per segment.
    dataLabels:
      everySlot && series.length === 1
        ? { enabled: true, offsetY: -18, formatter: figureAt, style: { fontWeight: 600, colors: [figureStyle.color] } }
        : { enabled: false },
    plotOptions: {
      bar: {
        columnWidth: "40%",
        borderRadiusWhenStacked: "last",
        dataLabels: {
          position: "top",
          hideOverflowingLabels: false,
          ...(everySlot && series.length > 1 ? { total: { enabled: true, formatter: figureAt, style: figureStyle } } : {}),
        },
      },
    },
    tooltip: {
      shared: true,
      intersect: false,
      hideEmptySeries: hideZeroInTip,
      x: { formatter: tipTitle(categories, extras) },
      y: { formatter: tipFigure(series, hideZeroInTip) },
    },
  } satisfies ChartOptions);
  const chart = (
    <Chart
      type="bar"
      series={series.map((s) => ({ name: s.name, data: s.data }))}
      options={chartOptions}
      deps={[columnFigures, extras, series.map((s) => s.tips)]}
      sx={{ height: STACKED_HEIGHT }}
    />
  );
  if (!everySlot) {
    return (
      <div className="kit-chart" ref={hostRef} role="img" aria-label={chartLabel}>
        {chart}
      </div>
    );
  }
  return (
    <div className="kit-chart chart-slots-scroll" ref={hostRef} role="img" aria-label={chartLabel} tabIndex={0}>
      <div className="chart-slots-plot" style={{ ["--chart-slots" as string]: categories.length }}>
        {chart}
      </div>
    </div>
  );
}

export type SeriesLinesChartProps = {
  categories: string[];
  series: Series<number | null>[];
  max: number;
  yTicks: AxisTick[];
  /** One more series in another unit on its OWN right-hand scale, dashed. */
  secondary?: Series<number | null> & { max: number; yTicks: AxisTick[] };
  hideZeroInTip: boolean;
  chartLabel: string;
};

/**
 * Multi-line chart on the template's EcommerceYearlySales area options: the gradient sits under the
 * PRIMARY series only, nulls stay gaps, a point with no neighbour draws a dot. A secondary series
 * rides a second y axis (BankingBalanceStatistics-style apex multi-yaxis, `opposite: true`), dashed.
 */
export function SeriesLinesChart({ categories, series, max, yTicks, secondary, hideZeroInTip, chartLabel }: SeriesLinesChartProps) {
  const [hostRef, hostW] = useHostWidth();
  const all = secondary ? [...series, secondary] : series;
  const colors = all.map((s) => s.color);
  const yWidth = yWidthOf(yTicks);
  const secondaryWidth = secondary ? yWidthOf(secondary.yTicks) : 0;
  const ticks = regularTickIndexes(categories, Math.max(0, (hostW || 600) - yWidth - secondaryWidth - 24));
  const has = (s: Series<number | null>, k: number) => k >= 0 && k < s.data.length && s.data[k] != null;
  const primaryAxis = { min: 0, max, tickAmount: 4, labels: { minWidth: yWidth, formatter: tickLabel(yTicks, max) } };
  // Set AFTER useChart: its deep merge would fold an axis ARRAY into the base yaxis object
  // ({ 0: …, 1: …, tickAmount }), which Apex reads as one broken axis.
  const yaxis: ChartOptions["yaxis"] = secondary
    ? [
        // Apex maps series i to yaxis i: every primary series shares the one visible left scale.
        ...series.map((_, i) => ({ ...primaryAxis, show: i === 0 })),
        { min: 0, max: secondary.max, tickAmount: 4, opposite: true, labels: { minWidth: secondaryWidth, formatter: tickLabel(secondary.yTicks, secondary.max) } },
      ]
    : primaryAxis;
  const chartOptions = useChart({
    colors,
    legend: { show: false },
    stroke: { width: all.map((_, i) => (secondary && i === all.length - 1 ? 2 : 2.5)), dashArray: all.map((_, i) => (secondary && i === all.length - 1 ? 4 : 0)) },
    fill: { type: all.map((_, i) => (i === 0 ? "gradient" : "solid")), opacity: all.map((_, i) => (i === 0 ? 1 : 0)) },
    markers: {
      discrete: all.flatMap((s, i) =>
        s.data.flatMap((_, k) => (has(s, k) && !has(s, k - 1) && !has(s, k + 1) ? [{ seriesIndex: i, dataPointIndex: k, size: 5, fillColor: colors[i], strokeColor: colors[i] }] : [])),
      ),
    },
    xaxis: {
      categories,
      overwriteCategories: categories.map((l, i) => (ticks.has(i) ? l : "")),
      labels: { rotate: 0, hideOverlappingLabels: false },
      tooltip: { enabled: false },
    },
    tooltip: {
      shared: true,
      intersect: false,
      hideEmptySeries: hideZeroInTip,
      x: { formatter: tipTitle(categories) },
      y: { formatter: tipFigure(all, hideZeroInTip) },
    },
  } satisfies ChartOptions);
  return (
    <div className="kit-chart" ref={hostRef} role="img" aria-label={chartLabel}>
      <Chart type="area" series={all.map((s) => ({ name: s.name, data: s.data }))} options={{ ...chartOptions, yaxis }} deps={[categories, all.map((s) => s.tips)]} sx={{ height: LINES_HEIGHT }} />
    </div>
  );
}

export type SeriesPieChartProps = {
  slices: { label: string; value: number; color: string; /** Centre figure on hover. */ valueLabel: string; /** Legend figure (with noun). */ legendValue: string; pct: string }[];
  totalLabel: string;
  centerCaption: string;
  chartLabel: string;
};

/**
 * Donut on the template's AppCurrentDownload (72% hole, total in the centre, ChartLegends under a
 * dashed Divider). Hover reads from the centre label, as the template AppCurrentDownload does. The legend puts
 * each figure BESIDE its label and lets a long label wrap inside the card.
 */
export function SeriesPieChart({ slices, totalLabel, centerCaption, chartLabel }: SeriesPieChartProps) {
  const colors = slices.map((s) => s.color);
  const valueFor = (value: number | string) => slices.find((s) => near(s.value, Number(value), 1e-3))?.valueLabel ?? String(value);
  const chartOptions = useChart({
    chart: { sparkline: { enabled: true } },
    colors,
    labels: slices.map((s) => s.label),
    stroke: { width: 0 },
    tooltip: { enabled: false },
    plotOptions: {
      pie: {
        donut: {
          size: "72%",
          labels: {
            value: { formatter: valueFor },
            total: { label: centerCaption, formatter: () => totalLabel },
          },
        },
      },
    },
  } satisfies ChartOptions);
  return (
    <div className="kit-donut-wrap" role="img" aria-label={chartLabel}>
      <Chart type="donut" series={slices.map((s) => s.value)} options={chartOptions} deps={[slices, totalLabel]} sx={{ my: 3, mx: "auto", width: PIE_SIZE, height: PIE_SIZE, maxWidth: 1 }} />
      <Divider sx={{ borderStyle: "dashed" }} />
      <ChartLegends
        labels={slices.map((s) => s.label)}
        colors={slices.map((s) => s.color)}
        sublabels={slices.map((s) => s.pct)}
        values={slices.map((s) => s.legendValue)}
        sx={{ p: 3, justifyContent: "center", columnGap: 3, rowGap: 1.5 }}
        slotProps={{
          wrapper: { sx: { flexDirection: "row", alignItems: "baseline", flexWrap: "wrap", columnGap: 1, minWidth: 0, maxWidth: 1 } },
          root: { sx: { minWidth: 0 } },
          label: { sx: { flexShrink: 1, minWidth: 0, overflowWrap: "anywhere" } },
          value: { sx: { mt: 0, typography: "subtitle2", whiteSpace: "nowrap" } },
        }}
      />
    </div>
  );
}

/** The template's ChartLegends as the shared cartesian legend: dot, label, optional figure, top-right. */
export function SeriesLegendView({ entries }: { entries: { label: string; colorVar: string; value?: string; hatched?: boolean }[] }) {
  const withValues = entries.some((e) => e.value);
  // A hatched series (a striped bar) gets a striped dot, so its key reads like its bars.
  const hatchSx = Object.fromEntries(
    entries.flatMap((e, i) =>
      e.hatched
        ? [[`& > li:nth-of-type(${i + 1}) .${chartClasses.legends.item.dot}`, { backgroundColor: "transparent", backgroundImage: `repeating-linear-gradient(135deg, ${e.colorVar} 0 2px, var(--palette-background-paper) 2px 4px)` }]]
        : [],
    ),
  );
  return (
    <ChartLegends
      labels={entries.map((e) => e.label)}
      colors={entries.map((e) => e.colorVar)}
      values={withValues ? entries.map((e) => e.value ?? "") : undefined}
      sx={{ justifyContent: "flex-end", columnGap: 2, rowGap: 1, pb: 1.5, ...hatchSx }}
      slotProps={withValues ? undefined : { value: { sx: { display: "none" } } }}
    />
  );
}
