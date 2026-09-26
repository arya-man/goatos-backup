"use client";

import { useMemo, type ReactNode } from "react";
import { useTheme } from "@mui/material/styles";
import { Chart, useChart, type ChartOptions } from "@/components/minimal/chart";
import { EmptyState } from "@/components/app/empty-state";
import { chartColor, chartRamp } from "@/components/app/chart-colors";

export type ChartSeries = {
  /** data key */
  key: string;
  label: string;
  color?: string;
  /** Area charts: gradient fill under the line. Defaults to the largest series only (reference/target series stay as lines). */
  fill?: boolean;
  /** Dashed stroke, e.g. for a target or benchmark series. */
  dashed?: boolean;
};

type YAxis = Exclude<NonNullable<ChartOptions["yaxis"]>, unknown[]>;

type Row = Record<string, string | number | null | undefined>;

/** Default y labels group thousands ("1,000"), matching every formatted axis beside them. */
const groupThousands = (v: number) => (Number.isFinite(v) ? v.toLocaleString("en-IN") : "");

const escapeHtml = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);

export type TrendChartProps = {
  data: Row[];
  xKey: string;
  series: ChartSeries[];
  /** default 320 */
  height?: number;
  /** "area" (gradient fill, default), "line", or "bar". */
  kind?: "area" | "line" | "bar";
  stacked?: boolean;
  valueFormat?: (v: number) => string;
  xFormat?: (v: string | number) => string;
  showLegend?: boolean;
  yWidth?: number;
  /** Counts (head, deaths, doses): no half-unit ticks on the value axis. */
  integerY?: boolean;
  /** One line shown instead of the plot when there is no point (or no series). */
  emptyLabel?: ReactNode;
  /** Fixed value-axis range, so sibling charts (the pens of one name) share ONE scale. */
  yDomain?: [number, number];
  /** Tooltip value for a series with no figure on that category (null in the row). Default "—". */
  missingLabel?: string;
  /** Row field carrying extra tooltip lines, newline-separated, composed by the caller. */
  detailKey?: string;
  /** Leave a series out of a category's tooltip when its value there is 0. */
  hideZeroInTip?: boolean;
};

/**
 * Area / line / bar chart on the template's ApexCharts `Chart` + `useChart` base options, untouched
 * (no data labels, no hover-state override, template tooltip, palette colours).
 * Bar: AnalyticsWebsiteVisits (stacked: AppAreaInstalled). Area / line: EcommerceYearlySales.
 * Mesha data rules kept on top of the template options:
 * - the value axis groups thousands ("1,000"), a count axis never shows half units and always
 *   carries at least two labelled ticks, an all-zero window still gets a readable 0..4 axis;
 * - a null value is ABSENT (a gap, and the missing mark in the tooltip), never 0;
 * - a point with no neighbour draws a dot, since a line cannot show it.
 * Axis labels are the caller's short labels; Apex hides the ones that would overlap, as in the
 * template.
 */
export function TrendChart({ data, xKey, series, height = 320, kind = "area", stacked, valueFormat, xFormat, showLegend = series.length > 1, yWidth = 44, integerY, emptyLabel = "No data in this period", yDomain, missingLabel = "—", detailKey, hideZeroInTip = false }: TrendChartProps) {
  const theme = useTheme();
  const isCat = kind === "bar";
  const labelOf = (v: unknown) => String(xFormat ? xFormat(v as string | number) : v ?? "");
  const categories = data.map((r) => labelOf(r[xKey]));
  const ramp = chartRamp(theme, isCat ? "bar" : "line");
  const colors = series.map((s, i) => (s.color ? chartColor(theme, s.color) : ramp[i % ramp.length]));
  const valueOf = (r: Row, k: string): number | null => {
    const v = r[k];
    if (v == null || v === "") return null;
    const n = typeof v === "number" ? v : Number(v);
    return Number.isFinite(n) ? n : null;
  };
  const has = (s: ChartSeries, k: number) => k >= 0 && k < data.length && valueOf(data[k], s.key) != null;

  // Value axis: grouped thousands, integer steps for counts, at least two labelled ticks.
  const magnitude = (k: string) => data.reduce((m, r) => Math.max(m, Math.abs(valueOf(r, k) ?? 0)), 0);
  const allZero = series.every((s) => magnitude(s.key) === 0);
  const top = stacked
    ? data.reduce((m, r) => Math.max(m, series.reduce((sum, s) => sum + Math.max(0, valueOf(r, s.key) ?? 0), 0)), 0)
    : series.reduce((m, s) => Math.max(m, magnitude(s.key)), 0);
  const yaxis: YAxis = yDomain
    ? { min: yDomain[0], max: yDomain[1], tickAmount: integerY && yDomain[1] - yDomain[0] <= 5 ? Math.max(1, yDomain[1] - yDomain[0]) : 4 }
    : allZero
      ? { min: 0, max: 4, tickAmount: 4 }
      : integerY && top <= 5
        ? { min: 0, max: Math.max(1, Math.ceil(top)), tickAmount: Math.max(1, Math.ceil(top)) }
        : integerY
          ? { min: 0, forceNiceScale: true, decimalsInFloat: 0 }
          : { forceNiceScale: true };
  const leadIndex = series.reduce((best, s, i) => (magnitude(s.key) > magnitude(series[best].key) ? i : best), 0);
  const filled = series.map((s, i) => s.fill ?? (stacked || i === leadIndex));

  const formatValue = (v: number | null | undefined) => (v == null || !Number.isFinite(v) ? missingLabel : valueFormat ? valueFormat(v) : v.toLocaleString("en-IN"));

  const options = useMemo<ChartOptions>(
    () => ({
      colors,
      chart: { stacked: Boolean(stacked) },
      legend: { show: showLegend },
      stroke: isCat
        ? { width: 2, colors: ["transparent"] }
        : { width: series.map((s) => (s.dashed ? 2 : 2.5)), dashArray: series.map((s) => (s.dashed ? 4 : 0)) },
      ...(kind === "area" ? { fill: { type: filled.map((f) => (f ? "gradient" : "solid")), opacity: filled.map((f) => (f ? 1 : 0)) } } : {}),
      markers: {
        discrete: isCat
          ? []
          : series.flatMap((s, i) =>
              data.flatMap((_, k) => (has(s, k) && !has(s, k - 1) && !has(s, k + 1) ? [{ seriesIndex: i, dataPointIndex: k, size: 5, fillColor: colors[i], strokeColor: colors[i] }] : [])),
            ),
      },
      xaxis: { categories, ...(isCat ? {} : { tooltip: { enabled: false } }) },
      yaxis: { ...yaxis, labels: { minWidth: yWidth, formatter: (v: number) => (valueFormat ? valueFormat(v) : groupThousands(v)) } },
      ...(stacked ? { plotOptions: { bar: { columnWidth: "40%" } } } : {}),
      tooltip: {
        shared: true,
        intersect: false,
        hideEmptySeries: hideZeroInTip,
        x: {
          formatter: (_v: number | string, opts?: { dataPointIndex?: number }) => {
            const i = opts?.dataPointIndex ?? -1;
            const title = escapeHtml(categories[i] ?? String(_v));
            const detail = detailKey && i >= 0 ? data[i]?.[detailKey] : undefined;
            return typeof detail === "string" && detail !== "" ? `${title}${detail.split("\n").map((l) => `<br/>${escapeHtml(l)}`).join("")}` : title;
          },
        },
        y: { formatter: (v: number) => formatValue(v) },
      },
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [colors.join(), data, xKey, series, kind, stacked, showLegend, yWidth, integerY, yDomain?.join(), missingLabel, detailKey, hideZeroInTip],
  );
  const chartOptions = useChart(options);

  if (data.length === 0 || series.length === 0) {
    return <EmptyState title={emptyLabel} />;
  }
  const chartSeries = series.map((s) => ({ name: s.label, data: data.map((r) => valueOf(r, s.key)) }));
  return <Chart type={kind} series={chartSeries} options={chartOptions} slotProps={{ loading: { p: 2.5 } }} sx={{ pl: 1, py: 2.5, pr: 2.5, height }} />;
}
