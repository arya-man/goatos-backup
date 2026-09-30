"use client";

// Client half of components/svg-series.tsx: the template's ApexCharts `Chart` + `useChart`, fed
// ONLY serializable props. The server wrapper has already composed every visible string (short
// categories, tooltip figures per series per point, y tick labels for a fixed axis top), so
// nothing here formats a number or a date and nothing here owns copy.
//
// Template sources (next-ts/src/sections/overview): the column chart is AppAreaInstalled /
// BankingBalanceStatistics, the area chart EcommerceYearlySales, the donut AppCurrentDownload, and
// the chart card (title, subheader, year select, legend with totals, one chart) EcommerceYearlySales.
// The base options are useChart's, untouched: no data labels, no hover-state override, the
// template tooltip, colours from the theme palette (components/app/chart-colors).

import { useMemo, useState } from "react";
import useMediaQuery from "@mui/material/useMediaQuery";
import type { Theme } from "@mui/material/styles";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Divider from "@mui/material/Divider";
import { chartColor, useChartTheme } from "@/components/app/chart-colors";
import { EmptyState } from "@/components/app/empty-state";
import { Chart, ChartLegends, ChartSelect, useChart, type ChartOptions } from "@/components/minimal/chart";
import { chartClasses } from "@/components/minimal/chart/classes";

export type AxisTick = { value: number; label: string };

type TipRow = { label: string; value: string };

type Series<T> = {
  name: string;
  /** A palette channel ("primary", "info.dark") or a legacy Mesha token; see chart-colors. */
  color: string;
  data: T[];
  /** Tooltip figure per point, composed by the server; "" where the point has no figure. */
  tips: string[];
};

const PIE_SIZE = 240;
/** EcommerceYearlySales chart height. */
const LINES_HEIGHT = 320;

const escapeHtml = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);

/** Same figure within floating-point noise of the scale. */
const near = (a: number, b: number, scale: number) => scale * 1e-6 >= Math.abs(a - b);

/**
 * Phones (below sm): a day axis of 7-90 slots cannot print every date. Five flat dd/mm labels, Apex
 * hiding any that would still touch, instead of a crammed -45deg fan whose dates run into each
 * other ("01/09/202603/09/2026" on /feed/analytics at 390). guard: axis-label-overlap (r2 text-fit)
 * + phone-day-axis (series-charts-phone-axis.test.mjs).
 */
export const PHONE_DAY_AXIS_LABELS = {
  tickAmount: 4,
  labels: {
    // No rotate override (guard: chart-template-anatomy): short ticks leave Apex nothing to rotate.
    hideOverlappingLabels: true,
    // "01/09/2026" -> "01/09": ten characters at five ticks still touch on a 390 plot; the tooltip
    // title keeps the full date (its own formatter).
    formatter: (value: string | number) => String(value ?? "").replace(/^(\d{2}\/\d{2})\/\d{4}$/, "$1"),
  },
};

/** True below the theme's sm breakpoint; known at the first client render (noSsr). */
function usePhone() {
  return useMediaQuery((theme: Theme) => theme.breakpoints.down("sm"), { noSsr: true });
}

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

/** Tooltip title: the full category (e.g. "Apr 2025"), then any extra pre-formatted rows. */
function tipTitle(titles: string[], extras?: TipRow[][]) {
  return (_v: number | string, opts?: { dataPointIndex?: number }) => {
    const i = opts?.dataPointIndex ?? -1;
    const title = escapeHtml(titles[i] ?? String(_v));
    const rows = extras?.[i] ?? [];
    return rows.length === 0 ? title : `${title}${rows.map((r) => `<br/>${escapeHtml(r.label)}: <b>${escapeHtml(r.value)}</b>`).join("")}`;
  };
}

// ----------------------------------------------------------------------

export type StackedColumnsChartProps = {
  /** Axis labels, short ("Apr"). */
  categories: string[];
  /** Tooltip titles, full ("Apr 2025"); defaults to the categories. */
  titles?: string[];
  series: Series<number>[];
  /** Extra tooltip rows per slot in a different unit (e.g. the rupees behind a head count). */
  extras: TipRow[][];
  max: number;
  yTicks: AxisTick[];
  hideZeroInTip: boolean;
  chartLabel: string;
  /** Chart box sx height (template cards: 320). */
  height?: number;
};

/**
 * A shared (whole-column) tooltip lists one row per series; past this many rows it grows taller
 * than the chart card and the card clips it (/feed/analytics "Daily directed feed", 14 feeds).
 * Beyond it the tooltip names only the hovered segment (the legend carries the series).
 */
export const SHARED_TIP_MAX_SERIES = 6;

/** Columns on the template's AppAreaInstalled options (stacked when there is more than one series). */
export function StackedColumnsChart({ categories, titles, series, extras, max, yTicks, hideZeroInTip, chartLabel, height = 320 }: StackedColumnsChartProps) {
  const theme = useChartTheme();
  const phone = usePhone();
  const colors = series.map((s) => chartColor(theme, s.color));
  const options = useMemo<ChartOptions>(
    () => ({
      colors,
      chart: { stacked: series.length > 1 },
      stroke: { width: 0 },
      xaxis: { categories, ...(phone ? PHONE_DAY_AXIS_LABELS : {}) },
      yaxis: { min: 0, max, tickAmount: 4, labels: { formatter: tickLabel(yTicks, max) } },
      tooltip: {
        shared: series.length <= SHARED_TIP_MAX_SERIES,
        intersect: series.length > SHARED_TIP_MAX_SERIES,
        hideEmptySeries: hideZeroInTip,
        x: { formatter: tipTitle(titles ?? categories, extras) },
        y: { formatter: tipFigure(series, hideZeroInTip) },
      },
      plotOptions: { bar: { columnWidth: "40%" } },
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [colors.join(), phone, categories, titles, series, extras, max, yTicks, hideZeroInTip],
  );
  const chartOptions = useChart(options);
  return (
    <Chart
      type="bar"
      series={series.map((s) => ({ name: s.name, data: s.data }))}
      options={chartOptions}
      role="img"
      aria-label={chartLabel}
      slotProps={{ loading: { p: 2.5 } }}
      sx={{ pl: 1, py: 2.5, pr: 2.5, height }}
    />
  );
}

// ----------------------------------------------------------------------

export type ColumnsCardView = {
  /** The select option: the year or range the view covers ("2025", "Apr 2025 - Mar 2026"). */
  label: string;
  categories: string[];
  titles: string[];
  series: Series<number>[];
  extras: TipRow[][];
  max: number;
  yTicks: AxisTick[];
  /** Legend figure per series for this view (its total), pre-formatted. */
  totals: string[];
};

/**
 * One column chart in the template's chart card (EcommerceYearlySales anatomy): CardHeader title +
 * subheader with the year/range select as its action, the legend with each series' total, then
 * the chart.
 */
export function ColumnsChartCard({
  title,
  subheader,
  views,
  hideZeroInTip,
  chartLabel,
  emptyLabel,
}: {
  title: string;
  subheader?: string;
  views: ColumnsCardView[];
  /** Shown in the card when no view has a figure. */
  emptyLabel?: string;
  hideZeroInTip: boolean;
  chartLabel: string;
}) {
  const theme = useChartTheme();
  const [selected, setSelected] = useState(views[views.length - 1]?.label ?? "");
  const view = views.find((v) => v.label === selected) ?? views[views.length - 1];
  return (
    <Card>
      <CardHeader
        title={title}
        subheader={subheader}
        action={view ? <ChartSelect options={views.map((v) => v.label)} value={view.label} onChange={setSelected} /> : undefined}
        sx={{ mb: 3 }}
      />
      {view ? (
        <>
          <ChartLegends
            colors={view.series.map((s) => chartColor(theme, s.color))}
            labels={view.series.map((s) => s.name)}
            values={view.totals}
            sx={{ px: 3, gap: (theme) => theme.spacing(3) }}
          />
          <StackedColumnsChart
            key={view.label}
            categories={view.categories}
            titles={view.titles}
            series={view.series}
            extras={view.extras}
            max={view.max}
            yTicks={view.yTicks}
            hideZeroInTip={hideZeroInTip}
            chartLabel={chartLabel}
          />
        </>
      ) : (
        <EmptyState title={emptyLabel} />
      )}
    </Card>
  );
}

// ----------------------------------------------------------------------

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
 * rides a second y axis (`opposite: true`), dashed.
 */
export function SeriesLinesChart({ categories, series, max, yTicks, secondary, hideZeroInTip, chartLabel }: SeriesLinesChartProps) {
  const theme = useChartTheme();
  const phone = usePhone();
  const all = useMemo(() => (secondary ? [...series, secondary] : series), [series, secondary]);
  const colors = all.map((s) => chartColor(theme, s.color));
  const options = useMemo<ChartOptions>(() => {
    const has = (s: Series<number | null>, k: number) => k >= 0 && k < s.data.length && s.data[k] != null;
    return {
      colors,
      stroke: { width: all.map((_, i) => (secondary && i === all.length - 1 ? 2 : 2.5)), dashArray: all.map((_, i) => (secondary && i === all.length - 1 ? 4 : 0)) },
      fill: { type: all.map((_, i) => (i === 0 ? "gradient" : "solid")), opacity: all.map((_, i) => (i === 0 ? 1 : 0)) },
      markers: {
        discrete: all.flatMap((s, i) =>
          s.data.flatMap((_, k) => (has(s, k) && !has(s, k - 1) && !has(s, k + 1) ? [{ seriesIndex: i, dataPointIndex: k, size: 5, fillColor: colors[i], strokeColor: colors[i] }] : [])),
        ),
      },
      xaxis: { categories, tooltip: { enabled: false }, ...(phone ? PHONE_DAY_AXIS_LABELS : {}) },
      tooltip: {
        shared: true,
        intersect: false,
        hideEmptySeries: hideZeroInTip,
        x: { formatter: tipTitle(categories) },
        y: { formatter: tipFigure(all, hideZeroInTip) },
      },
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [colors.join(), phone, all, categories, hideZeroInTip]);
  const chartOptions = useChart(options);
  const primaryAxis = { min: 0, max, tickAmount: 4, labels: { formatter: tickLabel(yTicks, max) } };
  // Set AFTER useChart: its deep merge would fold an axis ARRAY into the base yaxis object.
  const yaxis: ChartOptions["yaxis"] = secondary
    ? [
        // Apex maps series i to yaxis i: every primary series shares the one visible left scale.
        ...series.map((_, i) => ({ ...primaryAxis, show: i === 0 })),
        { min: 0, max: secondary.max, tickAmount: 4, opposite: true, labels: { formatter: tickLabel(secondary.yTicks, secondary.max) } },
      ]
    : primaryAxis;
  return (
    <Chart
      type="area"
      series={all.map((s) => ({ name: s.name, data: s.data }))}
      options={{ ...chartOptions, yaxis }}
      role="img"
      aria-label={chartLabel}
      slotProps={{ loading: { p: 2.5 } }}
      sx={{ pl: 1, py: 2.5, pr: 2.5, height: LINES_HEIGHT }}
    />
  );
}

// ----------------------------------------------------------------------

export type SeriesPieChartProps = {
  slices: { label: string; value: number; color: string; /** Centre figure on hover. */ valueLabel: string; /** Legend figure (with noun). */ legendValue: string; pct: string }[];
  totalLabel: string;
  centerCaption: string;
  chartLabel: string;
};

/**
 * Donut on the template's AppCurrentDownload (72% hole, total in the centre, ChartLegends under a
 * dashed Divider). Hover reads from the centre label, as the template does.
 */
export function SeriesPieChart({ slices, totalLabel, centerCaption, chartLabel }: SeriesPieChartProps) {
  const theme = useChartTheme();
  const colors = slices.map((s) => chartColor(theme, s.color));
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
    <>
      <Chart type="donut" series={slices.map((s) => s.value)} options={chartOptions} role="img"
      aria-label={chartLabel} sx={{ my: 6, mx: "auto", width: PIE_SIZE, height: PIE_SIZE }} />
      <Divider sx={{ borderStyle: "dashed" }} />
      <ChartLegends
        labels={slices.map((s) => s.label)}
        colors={colors}
        sublabels={slices.map((s) => s.pct)}
        values={slices.map((s) => s.legendValue)}
        sx={{ p: 3, justifyContent: "center" }}
      />
    </>
  );
}

/** The template's ChartLegends as the shared cartesian legend: dot, label, optional figure. */
export function SeriesLegendView({ entries }: { entries: { label: string; colorVar: string; value?: string; hatched?: boolean }[] }) {
  const theme = useChartTheme();
  const withValues = entries.some((e) => e.value);
  const colors = entries.map((e) => chartColor(theme, e.colorVar));
  // A hatched series (a striped bar) gets a striped dot, so its key reads like its bars.
  const hatchSx = Object.fromEntries(
    entries.flatMap((e, i) =>
      e.hatched
        ? [[`& > li:nth-of-type(${i + 1}) .${chartClasses.legends.item.dot}`, { backgroundColor: "transparent", backgroundImage: `repeating-linear-gradient(135deg, ${colors[i]} 0 2px, ${theme.vars.palette.background.paper} 2px 4px)` }]]
        : [],
    ),
  );
  return (
    <ChartLegends
      labels={entries.map((e) => e.label)}
      colors={colors}
      values={withValues ? entries.map((e) => e.value ?? "") : undefined}
      sx={{ px: 3, gap: (t) => t.spacing(3), ...hatchSx }}
    />
  );
}
