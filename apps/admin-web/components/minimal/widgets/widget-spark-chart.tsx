'use client';

// The template widgets' mini chart as a standalone piece, for cards that are not a template widget
// (the kit KpiCard). Options are copied from the template sections named below.
import { Chart, useChart } from '../chart';

/**
 * The template widget mini chart: bars = AppWidgetSummary (radius 1.5, 64% columns, no stroke),
 * line = AnalyticsWidgetSummary (6px grid padding, no markers). Figures on hover, no axes.
 */
export function WidgetSparkChart({ data, variant, color, className }: { data: number[]; variant: "bar" | "line"; color: string; className?: string }) {
  const bar = variant === "bar";
  const chartOptions = useChart({
    chart: { sparkline: { enabled: true } },
    colors: [color],
    ...(bar
      ? { stroke: { width: 0 }, plotOptions: { bar: { borderRadius: 1.5, columnWidth: "64%" } } }
      : { grid: { padding: { top: 6, left: 6, right: 6, bottom: 6 } }, markers: { strokeWidth: 0 } }),
    tooltip: {
      x: { show: false },
      y: { formatter: (value: number) => value.toLocaleString("en-IN"), title: { formatter: () => "" } },
    },
  });
  return <Chart type={variant} series={[{ data }]} options={chartOptions} className={className} sx={{ width: 84, height: 56 }} />;
}
