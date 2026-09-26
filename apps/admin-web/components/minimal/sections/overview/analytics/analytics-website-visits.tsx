'use client';

// Copied from the licensed MUI Minimal template
// (next-ts src/sections/overview/analytics/analytics-website-visits.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - `unit`/`digits` (or the older `valueNoun`, same meaning) replace the demo "visits" tooltip
//    suffix (a server page cannot pass a formatter function), and a series may carry per-point
//    `notes` (e.g. "61 animals");
//  - a missing point is `null` and draws no bar (a week nobody weighed is absent, never zero);
//  - `empty` renders in place of the chart when there are no categories; title/subheader take
//    nodes, the header takes an `action`, and `children` render at the foot of the card.

import type { CardProps } from '@mui/material/Card';
import type { ChartOptions } from '@/components/minimal/chart';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import CardHeader from '@mui/material/CardHeader';
import { useTheme, alpha as hexAlpha } from '@mui/material/styles';

import { Chart, useChart } from '@/components/minimal/chart';

// ----------------------------------------------------------------------

type Props = Omit<CardProps, 'title'> & {
  title?: React.ReactNode;
  subheader?: React.ReactNode;
  action?: React.ReactNode;
  empty?: React.ReactNode;
  valueNoun?: string;
  chart: {
    colors?: string[];
    categories?: string[];
    unit?: string;
    digits?: number;
    series: {
      name: string;
      data: (number | null)[];
      notes?: (string | null)[];
    }[];
    options?: ChartOptions;
  };
};

/** A unit ending in "·" (e.g. "₹·") is a PREFIX: "₹1,234"; any other unit follows the figure. */
function formatValue(value: number | null | undefined, unit = '', digits = 0) {
  if (value == null || Number.isNaN(value)) return '–';
  const text = value.toLocaleString('en-IN', { minimumFractionDigits: digits, maximumFractionDigits: digits });
  if (unit.endsWith('·')) return `${value < 0 ? '−' : ''}${unit.slice(0, -1)}${text.replace('-', '')}`;
  return unit ? `${text} ${unit}` : text;
}

export function AnalyticsWebsiteVisits({ title, subheader, action, empty, valueNoun, chart, sx, children, ...other }: Props) {
  const theme = useTheme();
  const unit = chart.unit ?? valueNoun ?? '';

  const chartColors = chart.colors ?? [
    hexAlpha(theme.palette.primary.dark, 0.8),
    hexAlpha(theme.palette.warning.main, 0.8),
  ];

  const chartOptions = useChart({
    colors: chartColors,
    stroke: { width: 2, colors: ['transparent'] },
    xaxis: { categories: chart.categories },
    yaxis: { labels: { formatter: (value: number) => formatValue(value, chart.unit ?? '', 0) } },
    legend: { show: true },
    tooltip: {
      y: {
        formatter: (value: number, opts?: { seriesIndex: number; dataPointIndex: number }) => {
          const note = opts == null ? null : chart.series[opts.seriesIndex]?.notes?.[opts.dataPointIndex];
          const text = formatValue(value, unit, chart.digits ?? 0);
          return note ? `${text} · ${note}` : text;
        },
      },
    },
    ...chart.options,
  });

  const hasData = (chart.categories?.length ?? 0) > 0;

  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} subheader={subheader} action={action} />

      {hasData ? (
        <Chart
          type="bar"
          series={chart.series}
          options={chartOptions}
          slotProps={{ loading: { p: 2.5 } }}
          sx={{
            pl: 1,
            py: 2.5,
            pr: 2.5,
            height: 364,
          }}
        />
      ) : (
        <Box sx={{ p: 3 }}>{empty}</Box>
      )}

      {children}
    </Card>
  );
}
