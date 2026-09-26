"use client";

// Grouped (multi-series) column chart for full-width report cards — the load-wise counts and
// money charts on the sales board and Counts Breakdown.
//
// Drawn with the template's ApexCharts `Chart` on AnalyticsWebsiteVisits' options (grouped
// columns, transparent 2px stroke between bars) with the template's ChartLegends above the plot
// (AppAreaInstalled). NO copy of its own — every visible string arrives already resolved from the
// backend page contract by the caller. Wide load lists scroll inside the chart's own overflow box,
// never the page; the legend stays outside that box so every series is named on screen.
import { useTheme } from "@mui/material/styles";
import { Chart, useChart } from "./minimal/chart";
import { niceCeiling } from "./chart-scale";
import { SeriesLegendView } from "./series-charts";
import { inrAxisTick, numAxisTick } from "@/features/procurement/sales-format";

// Series colour per tone: each tone is ONE slot of the validated chart ramp (--chart-1..4, apart
// from the status colours; see chart-series-ramp.test.mjs), and the tone is named for the colour
// it draws, so no two names paint the same bar.
const TONE_VAR: Record<GroupedSeriesTone, string> = {
  info: "var(--chart-1)",
  ok: "var(--chart-2)",
  danger: "var(--chart-3)",
  warn: "var(--chart-4)",
  okHatch: "var(--chart-2)",
};

// `okHatch` is the sold green, striped: a figure that is sold-LIKE but not realised -- animals
// tagged to a sale that has not closed, or stock carried at an assumed price (main 7765efb29). A
// fifth solid colour would read as one of the four; the stripe keeps it distinct at a glance, on
// the bar (Apex pattern fill) and on its legend dot.
export type GroupedSeriesTone = "info" | "ok" | "danger" | "warn" | "okHatch";

/** Share of a load's slot the bar group fills (template default 48% leaves no room per figure). */
const GROUP_WIDTH = 0.72;
/** Rough px per character of a caption-size figure / axis line, for sizing a slot. */
const FIGURE_CHAR_PX = 7.2;
const AXIS_CHAR_PX = 6.6;
/** The okHatch stripe: an SVG pattern tile edge and its line weight (chart units, not layout). */
const HATCH_TILE = 6;
const HATCH_STROKE = 2;

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
  /** Optional second line under the axis label (e.g. the vendor, or "97 of 100 sold"). */
  subLabel?: string;
  /**
   * Optional short figure printed ABOVE each bar (maintainer request 2026-09-03: numbers on the
   * chart, not only on hover). One entry per series; `null` prints nothing. When absent, the bar
   * carries the leading part of its `displays` string, up to the first " · " -- the callers put
   * the figure first and the qualifier after it, so "27.9 kg · 126 weighed" prints "27.9 kg".
   */
  barLabels?: (string | null)[];
  /** Extra lines shown ONLY in the hover card (e.g. the day the load reached the farm). */
  tipLines?: string[];
};

function barLabelFor(datum: GroupedDatum, index: number): string | null {
  const explicit = datum.barLabels?.[index];
  if (explicit !== undefined) return explicit;
  const display = datum.displays[index];
  if (!display) return null;
  return display.split(" · ")[0];
}

/** Load labels carry a pen bracket ("129 (CPT Godel 2 - Part 1, ...)"): wrap in full at word
 *  boundaries (codes stay whole) instead of clipping, one axis line per chunk. */
function wrapLabel(text: string, maxChars = 14): string[] {
  const lines: string[] = [];
  for (const word of text.split(/\s+/).filter(Boolean)) {
    const last = lines[lines.length - 1];
    if (last !== undefined && (last + " " + word).length <= maxChars) lines[lines.length - 1] = `${last} ${word}`;
    else lines.push(word);
  }
  return lines.length ? lines : [""];
}

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
  const columns = series.filter((_, i) => baseIndex(i) < 0).length;
  const categories = data.map((d) => [...wrapLabel(d.axisLabel), ...(d.subLabel ? wrapLabel(d.subLabel) : [])]);
  // Each load's slot is wide enough for one figure per bar side by side and for its axis lines, so
  // no two figures collide at any width; the plot scrolls inside the card when that is wider.
  const longestFigure = Math.max(1, ...data.flatMap((d) => series.map((_, i) => (d.values[i] === null || baseIndex(i) >= 0 ? 0 : (barLabelFor(d, i) ?? "").length))));
  const longestAxisLine = Math.max(1, ...categories.flat().map((l) => l.length));
  const slotPx = Math.ceil(Math.max(64, (columns * (longestFigure * FIGURE_CHAR_PX + 10)) / GROUP_WIDTH, longestAxisLine * AXIS_CHAR_PX + 16));
  const axisTick = money ? inrAxisTick : numAxisTick;
  const chartOptions = useChart({
    colors: series.map((s) => TONE_VAR[s.tone]),
    ...(hasStack ? { chart: { stacked: true } } : null),
    // Solid bars, apart from the striped okHatch series.
    fill: {
      type: series.map((s) => (s.tone === "okHatch" ? "pattern" : "solid")),
      opacity: 1,
      pattern: { style: "slantedLines", width: HATCH_TILE, height: HATCH_TILE, strokeWidth: HATCH_STROKE },
    },
    stroke: { width: 2, colors: ["transparent"] },
    legend: { show: false },
    xaxis: {
      // The sub-label rides as the category's second line.
      categories,
      labels: { rotate: 0, hideOverlappingLabels: false, trim: false },
    },
    // Four round ticks on a nice ceiling, ONE unit per axis picked from the top (₹0.6L beside
    // ₹2.4L, never 60k beside 2.4L).
    yaxis: { min: 0, max, tickAmount: 4, labels: { formatter: (v: number) => axisTick(v, max) } },
    // Every bar carries its figure, a measured zero included (printed on the baseline); an absent
    // value (null) prints nothing, so "not recorded" and "0" stay apart.
    dataLabels: {
      enabled: true,
      offsetY: -18,
      style: { fontSize: theme.typography.caption.fontSize as string, fontWeight: 600, colors: ["var(--palette-text-secondary)"] },
      formatter: (_v: number, opts?: { seriesIndex: number; dataPointIndex: number }) => {
        if (!opts) return "";
        const datum = data[opts.dataPointIndex];
        if (!datum || datum.values[opts.seriesIndex] === null) return "";
        // A stacked segment prints nothing of its own: the column's figure is the base series'
        // bar label (the caller states the total there when it wants one).
        if (baseIndex(opts.seriesIndex) >= 0) return "";
        return barLabelFor(datum, opts.seriesIndex) ?? "";
      },
    },
    plotOptions: { bar: { columnWidth: `${GROUP_WIDTH * 100}%`, dataLabels: { position: "top", hideOverflowingLabels: false } } },
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
  });

  if (data.length === 0 || !hasAnyValue) {
    return <div className="cx-empty muted small">{emptyLabel}</div>;
  }
  const chartSeries = series.map((s, i) => ({
    name: s.label,
    data: data.map((d) => d.values[i]),
    ...(hasStack ? { group: groupOf(i) } : null),
  }));
  return (
    <div className="gcols-chart" role="img" aria-label={chartLabel}>
      <SeriesLegendView entries={series.map((s) => ({ label: s.label, colorVar: TONE_VAR[s.tone], hatched: s.tone === "okHatch" }))} />
      <div className="gcols-scroll" tabIndex={0}>
        <div className="gcols-plot" style={{ ["--gcols-n" as string]: data.length, ["--gcols-slot" as string]: `${slotPx}px` }}>
          <Chart type="bar" series={chartSeries} options={chartOptions} deps={[data, money]} sx={{ height: 1 }} />
        </div>
      </div>
    </div>
  );
}
