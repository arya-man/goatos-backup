'use client';

// Copied from the licensed MUI Minimal template
// (next-ts src/sections/overview/analytics/analytics-conversion-rates.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - `unit`/`digits` format the tooltip and data labels (a server page cannot pass a formatter
//    function), and a series may carry per-point `notes` appended to the tooltip;
//  - the plot grows with the number of categories (one pen per row, never squeezed below the
//    template's 360px), the white data-label colour comes from the theme instead of a raw hex;
//  - `empty` renders in place of the chart when there are no categories; title/subheader take
//    nodes and `children` render at the foot of the card.

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

/** Height of one category row, so a long pen list gets room instead of hairline bars. */
const ROW_HEIGHT = 36;

/** A unit ending in "·" (e.g. "₹·") is a PREFIX: "₹1,234"; any other unit follows the figure. */
function formatValue(value: number | null | undefined, unit = '', digits = 0) {
  if (value == null || Number.isNaN(value)) return '–';
  const text = value.toLocaleString('en-IN', { minimumFractionDigits: digits, maximumFractionDigits: digits });
  if (unit.endsWith('·')) return `${value < 0 ? '−' : ''}${unit.slice(0, -1)}${text.replace('-', '')}`;
  return unit ? `${text} ${unit}` : text;
}

export function AnalyticsConversionRates({ title, subheader, action, empty, chart, sx, children, ...other }: Props) {
  const theme = useTheme();

  const chartColors = chart.colors ?? [
    theme.palette.primary.dark,
    hexAlpha(theme.palette.primary.dark, 0.24),
  ];

  const chartOptions = useChart({
    colors: chartColors,
    stroke: { width: 2, colors: ['transparent'] },
    tooltip: {
      shared: true,
      intersect: false,
      y: {
        formatter: (value: number, opts?: { seriesIndex: number; dataPointIndex: number }) => {
          const note = opts == null ? null : chart.series[opts.seriesIndex]?.notes?.[opts.dataPointIndex];
          const text = formatValue(value, chart.unit, chart.digits ?? 0);
          return note ? `${text} · ${note}` : text;
        },
        title: { formatter: (seriesName: string) => `${seriesName}: ` },
      },
    },
    xaxis: {
      categories: chart.categories,
      labels: { formatter: (value: string) => formatValue(Number(value), chart.unit, 0) },
    },
    dataLabels: {
      enabled: true,
      offsetX: -6,
      formatter: (value: number) => formatValue(value, '', chart.digits ?? 0),
      style: { fontSize: '10px', colors: [theme.vars.palette.common.white, theme.vars.palette.text.primary] },
    },
    plotOptions: {
      bar: {
        horizontal: true,
        borderRadius: 2,
        barHeight: '48%',
        dataLabels: { position: 'top' },
      },
    },
    ...chart.options,
  });

  const rows = chart.categories?.length ?? 0;

  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} subheader={subheader} action={action} />

      {rows > 0 ? (
        <Chart
          type="bar"
          series={chart.series}
          options={chartOptions}
          slotProps={{ loading: { p: 2.5 } }}
          sx={{
            pl: 1,
            py: 2.5,
            pr: 2.5,
            height: Math.max(360, rows * ROW_HEIGHT * Math.max(1, chart.series.length) + 64),
          }}
        />
      ) : (
        <Box sx={{ p: 3 }}>{empty}</Box>
      )}

      {children}
    </Card>
  );
}
