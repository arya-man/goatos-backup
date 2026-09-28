"use client";

import type { ReactElement } from "react";
import { chartColor, useChartTheme } from "@/components/app/chart-colors";
import { Chart, ChartLegends, useChart } from "@/components/minimal/chart";
import {
  CHART_PALETTE,
  chartAccessibleLabel,
  chartLayout,
  formatChartValue,
  isRenderableChart,
  type CeoAiChart as CeoAiChartData,
} from "./ceo-ai-chart-geometry";

// The leadership assistant's optional answer chart, drawn with the template's ApexCharts Chart:
// bars = AnalyticsConversionRates (horizontal; the figure is in the tooltip), trends =
// EcommerceYearlySales (line). ceo-ai-chart-geometry still decides WHAT is drawable (at least two
// real readings, null = a gap, series capped at the palette); this file only draws it.
//
// Renders nothing when the chart is absent or not renderable (< 2 points), so the assistant bubble
// falls back to its text answer.

/** Category labels are kept WHOLE (distinct bars must never look identical): wrapped at word
 *  boundaries into axis lines instead of truncated. */
function wrap(text: string, max = 22): string[] {
  const lines: string[] = [];
  for (const word of text.split(/\s+/).filter(Boolean)) {
    const last = lines[lines.length - 1];
    if (last !== undefined && `${last} ${word}`.length <= max) lines[lines.length - 1] = `${last} ${word}`;
    else lines.push(word);
  }
  return lines.length ? lines : [""];
}

export function CeoAiChart({ chart }: { chart: CeoAiChartData | undefined }): ReactElement | null {
  const layout = isRenderableChart(chart) ? chartLayout(chart) : null;
  const isBar = layout?.kind === "bar";
  const labels = chart?.x.map(String) ?? [];
  const series = layout
    ? layout.legend.length
      ? layout.legend.map((item) => ({ name: item.name, data: (chart?.series.find((s) => String(s.name) === item.name)?.data ?? []).map((v) => (typeof v === "number" && Number.isFinite(v) ? v : null)) }))
      : [{ name: chart?.series[0]?.name ?? "", data: (chart?.series[0]?.data ?? []).map((v) => (typeof v === "number" && Number.isFinite(v) ? v : null)) }]
    : [];
  const theme = useChartTheme();
  const colors = (layout?.legend.length ? layout.legend.map((item) => item.color) : [CHART_PALETTE[0]]).map((c) => chartColor(theme, c));
  const wrapped = labels.map((label) => wrap(label));
  const lineCount = wrapped.reduce((sum, lines) => sum + Math.max(1, lines.length), 0);

  const chartOptions = useChart({
    colors,
    legend: { show: false },
    tooltip: { shared: true, intersect: false, y: { formatter: (v: number) => (v == null ? "—" : formatChartValue(v)) } },
    ...(isBar
      ? {
          stroke: { width: 2, colors: ["transparent"] },
          xaxis: { categories: wrapped, labels: { formatter: (v: string) => formatChartValue(Number(v)) } },
          yaxis: { labels: { maxWidth: 160 } },
          plotOptions: { bar: { horizontal: true, barHeight: "48%" } },
        }
      : {
          xaxis: { categories: labels },
          yaxis: { labels: { formatter: (v: number) => formatChartValue(v) } },
          markers: { size: 3, strokeWidth: 0 },
        }),
  });

  if (!chart || !layout) return null;
  const height = isBar ? Math.max(140, lineCount * 18 + labels.length * series.length * 14 + 40) : 200;
  return (
    <figure className="mzai-chart" role="img" aria-label={chartAccessibleLabel(chart)}>
      <figcaption className="mzai-chart-title">{chart.title}</figcaption>
      {layout.legend.length ? <ChartLegends labels={layout.legend.map((l) => l.name)} colors={colors} sx={{ gap: 1.5, mb: 1 }} /> : null}
      <Chart type={isBar ? "bar" : "line"} series={series} options={chartOptions} sx={{ height }} />
    </figure>
  );
}
