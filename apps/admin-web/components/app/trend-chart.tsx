"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import Box from "@mui/material/Box";
import { useTheme } from "@mui/material/styles";
import { Chart, useChart, type ChartOptions } from "@/components/minimal/chart";
import { EmptyState } from "@/components/app/empty-state";
import { chartColors } from "@/components/app/chart-colors";

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
  /**
   * Bar categories that are two short words ("Sat 19") print on two lines, upright and whole, so
   * seven days fit a phone with no overlap and no ellipsis.
   */
  xLines?: boolean;
};

/**
 * Area / line / bar chart on the template's ApexCharts `Chart` + `useChart` base options.
 * Bar: AnalyticsWebsiteVisits (stacked: AppAreaInstalled). Area / line: EcommerceYearlySales.
 * Mesha rules kept on top of the template options:
 * - a time axis labels points at a REGULAR step and always labels the final point (a step tick
 *   closer than one step to it yields), so the last two labels never crowd;
 * - every bar category keeps a label (rotated and trimmed when the band is narrow, never dropped);
 * - the value axis groups thousands ("1,000"), a count axis never shows half units and always
 *   carries at least two labelled ticks, an all-zero window still gets a readable 0..4 axis;
 * - a null value is ABSENT (a gap, and the missing mark in the tooltip), never 0;
 * - a point with no neighbour draws a dot, since a line cannot show it.
 */
export function TrendChart({ data, xKey, series, height = 320, kind = "area", stacked, valueFormat, xFormat, showLegend = series.length > 1, yWidth = 44, integerY, emptyLabel = "No data in this period", yDomain, missingLabel = "—", detailKey, hideZeroInTip = false, xLines = false }: TrendChartProps) {
  const [hostRef, hostW] = useHostWidth();
  const isCat = kind === "bar";
  const labelOf = (v: unknown) => String(xFormat ? xFormat(v as string | number) : v ?? "");
  const categories = data.map((r) => labelOf(r[xKey]));
  const ramp = chartColors(useTheme());
  const color = (s: ChartSeries, i: number) => s.color ?? ramp[i % ramp.length];
  const valueOf = (r: Row, k: string): number | null => {
    const v = r[k];
    if (v == null || v === "") return null;
    const n = typeof v === "number" ? v : Number(v);
    return Number.isFinite(n) ? n : null;
  };
  const has = (s: ChartSeries, k: number) => k >= 0 && k < data.length && valueOf(data[k], s.key) != null;

  // Regular step from the first point, and the LAST point is always labelled. Each label gets 1.5x
  // its width: ApexCharts pulls the edge labels inward, which crowded the first two on a phone.
  const plotW = Math.max(0, (hostW || 600) - (yWidth + 8) - 24);
  const longest = categories.reduce((m, l) => Math.max(m, l.length), 1);
  const labelPx = Math.min(longest, 16) * 6.6 + 12;
  const step = Math.max(1, Math.ceil(data.length / Math.max(1, Math.floor(plotW / (labelPx * 1.5)))));
  const tickIdx = new Set<number>();
  if (!isCat && data.length) {
    const idx: number[] = [];
    for (let i = 0; i < data.length; i += step) idx.push(i);
    const last = data.length - 1;
    if (idx[idx.length - 1] !== last) {
      if (last - idx[idx.length - 1] < step && idx.length > 1) idx.pop();
      idx.push(last);
    }
    idx.forEach((i) => tickIdx.add(i));
  }

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

  const chartOptions = useChart({
    colors: series.map(color),
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
            data.flatMap((_, k) => (has(s, k) && !has(s, k - 1) && !has(s, k + 1) ? [{ seriesIndex: i, dataPointIndex: k, size: 5, fillColor: color(s, i), strokeColor: color(s, i) }] : [])),
          ),
    },
    xaxis: {
      categories,
      ...(isCat && xLines
        ? { categories: categories.map((l) => (l.includes(" ") ? [l.slice(0, l.lastIndexOf(" ")), l.slice(l.lastIndexOf(" ") + 1)] : l)), labels: { rotate: 0, hideOverlappingLabels: false, trim: false } }
        : isCat
        ? { labels: { rotate: -40, hideOverlappingLabels: false, trim: true, maxHeight: 96 } }
        : { overwriteCategories: categories.map((l, i) => (tickIdx.has(i) ? l : "")), labels: { rotate: 0, hideOverlappingLabels: false }, tooltip: { enabled: false } }),
    },
    yaxis: { ...yaxis, labels: { minWidth: yWidth, formatter: (v: number) => (valueFormat ? valueFormat(v) : groupThousands(v)) } },
    plotOptions: {
      bar: {
        ...(data.length <= 3 ? { columnWidth: "24%" } : {}),
        ...(stacked ? { borderRadiusWhenStacked: "last" as const } : {}),
      },
    },
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
  } satisfies ChartOptions);

  if (data.length === 0 || series.length === 0) {
    return <EmptyState title={emptyLabel} />;
  }
  const chartSeries = series.map((s) => ({ name: s.label, data: data.map((r) => valueOf(r, s.key)) }));
  return (
    <Box ref={hostRef} sx={{ position: "relative", width: 1, minWidth: 0 }}>
      <Chart type={kind} series={chartSeries} options={chartOptions} deps={[data, categories, missingLabel]} sx={{ height }} />
    </Box>
  );
}
