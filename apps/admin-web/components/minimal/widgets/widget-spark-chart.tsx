'use client';

// The template widgets' mini chart as a standalone piece, for cards that are not a template widget
// (the kit KpiCard). Options are copied from the template sections named below.
import { Chart, useChart } from '../chart';

/**
 * The template widget mini charts, figures on hover, no axes:
 * - line = EcommerceWidgetSummary (gradient stroke light -> main, 6px grid padding, 100x66);
 * - analytics = AnalyticsWidgetSummary (solid `dark` stroke, no markers, 84x56);
 * - bar = AppWidgetSummary (radius 1.5, 64% columns, no stroke, 84x56).
 * `colors` is [light, main] for line; the first colour is used by the others.
 */
export function WidgetSparkChart({ data, variant, colors, className }: { data: number[]; variant: 'bar' | 'line' | 'analytics'; colors: [string, string]; className?: string }) {
  const grid = { padding: { top: 6, left: 6, right: 6, bottom: 6 } };
  const chartOptions = useChart({
    chart: { sparkline: { enabled: true } },
    ...(variant === 'bar'
      ? { colors: [colors[0]], stroke: { width: 0 }, plotOptions: { bar: { borderRadius: 1.5, columnWidth: '64%' } } }
      : variant === 'analytics'
        ? { colors: [colors[0]], grid, markers: { strokeWidth: 0 } }
        : {
            colors: [colors[1]],
            grid,
            fill: {
              type: 'gradient',
              gradient: {
                colorStops: [
                  { offset: 0, color: colors[0], opacity: 1 },
                  { offset: 100, color: colors[1], opacity: 1 },
                ],
              },
            },
          }),
    tooltip: {
      x: { show: false },
      y: { formatter: (value: number) => value.toLocaleString('en-IN'), title: { formatter: () => '' } },
    },
  });
  const size = variant === 'line' ? { width: 100, height: 66 } : { width: 84, height: 56 };
  return <Chart type={variant === 'bar' ? 'bar' : 'line'} series={[{ data }]} options={chartOptions} className={className} sx={{ ...size, flexShrink: 0 }} />;
}
