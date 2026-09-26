'use client';

// Copied from the licensed MUI Minimal template
// (next-ts src/sections/overview/e-commerce/ecommerce-sale-by-gender.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - `total` is pre-formatted by the page and `totalLabel` names it (backend copy) instead of the
//    chart's default "Total";
//  - the default colour pairs are the locked Mesha palette (primary, info, secondary) instead of
//    warning/error, so an ordinary share is never painted in the "at risk" colours;
//  - the legend shows each series' own figure (`display`).

import type { CardProps } from '@mui/material/Card';
import type { ChartOptions } from '@/components/minimal/chart';

import { varAlpha } from 'minimal-shared/utils';

import Card from '@mui/material/Card';
import Divider from '@mui/material/Divider';
import CardHeader from '@mui/material/CardHeader';
import { useTheme } from '@mui/material/styles';

import { Chart, useChart, ChartLegends } from '@/components/minimal/chart';

// ----------------------------------------------------------------------

type Props = Omit<CardProps, 'title'> & {
  title?: React.ReactNode;
  subheader?: React.ReactNode;
  total: string;
  totalLabel?: string;
  chart: {
    colors?: string[][];
    series: {
      label: string;
      value: number;
      display?: string;
    }[];
    options?: ChartOptions;
  };
};

export function EcommerceSaleByGender({ title, subheader, total, totalLabel, chart, sx, ...other }: Props) {
  const theme = useTheme();

  const chartSeries = chart.series.map((item) => item.value);

  const chartColors = chart.colors ?? [
    [theme.vars.palette.primary.light, theme.vars.palette.primary.main],
    [theme.vars.palette.info.light, theme.vars.palette.info.main],
    [theme.vars.palette.secondary.light, theme.vars.palette.secondary.main],
  ];

  const chartOptions = useChart({
    chart: { sparkline: { enabled: true } },
    colors: chartColors.map((color) => color[1]),
    labels: chart.series.map((item) => item.label),
    stroke: { width: 0 },
    fill: {
      type: 'gradient',
      gradient: {
        colorStops: chartColors.map((color) => [
          { offset: 0, color: color[0], opacity: 1 },
          { offset: 100, color: color[1], opacity: 1 },
        ]),
      },
    },
    grid: { padding: { top: -40, bottom: -40 } },
    plotOptions: {
      radialBar: {
        hollow: { margin: 10, size: '32%' },
        track: {
          margin: 10,
          background: varAlpha(theme.vars.palette.grey['500Channel'], 0.08),
        },
        dataLabels: {
          total: { ...(totalLabel ? { label: totalLabel } : {}), formatter: () => total },
          value: { offsetY: 2, fontSize: theme.typography.h5.fontSize as string },
          name: { offsetY: -10 },
        },
      },
    },
    ...chart.options,
  });

  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} subheader={subheader} />

      <Chart
        type="radialBar"
        series={chartSeries}
        options={chartOptions}
        slotProps={{ loading: { p: 4 } }}
        sx={{
          my: 1.5,
          mx: 'auto',
          width: { xs: 280, sm: 300, xl: 320 },
          height: { xs: 280, sm: 300, xl: 320 },
        }}
      />

      <Divider sx={{ borderStyle: 'dashed' }} />

      <ChartLegends
        labels={chartOptions?.labels}
        colors={chartOptions?.colors}
        values={chart.series.map((item) => item.display ?? '')}
        sx={{ p: 3, justifyContent: 'center' }}
      />
    </Card>
  );
}
