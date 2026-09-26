'use client';

// Copied from the licensed MUI Minimal template
// (next-ts src/sections/overview/banking/banking-expenses-categories.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - legend sub-labels are pre-formatted by the page (`display`) instead of fCurrency, and the
//    tooltip shows the same figure;
//  - the two footer cells take the page's label/value pairs (the template shows demo constants);
//  - the default colours are the locked Mesha categorical ramp (no error red for an ordinary
//    category), passed as scheme-aware CSS variables the chart resolves at draw time.

import type { CardProps } from '@mui/material/Card';
import type { ChartOptions } from '@/components/minimal/chart';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Divider from '@mui/material/Divider';
import { useTheme } from '@mui/material/styles';
import CardHeader from '@mui/material/CardHeader';

import { Chart, useChart, ChartLegends } from '@/components/minimal/chart';

// ----------------------------------------------------------------------

type Props = Omit<CardProps, 'title'> & {
  title?: React.ReactNode;
  subheader?: React.ReactNode;
  footer: { label: React.ReactNode; value: React.ReactNode }[];
  chart: {
    colors?: string[];
    icons?: React.ReactNode[];
    series: {
      label: string;
      value: number;
      display: string;
    }[];
    options?: ChartOptions;
  };
};

export function BankingExpensesCategories({ title, subheader, footer, chart, sx, ...other }: Props) {
  const theme = useTheme();

  const chartColors = chart.colors ?? [
    theme.vars.palette.primary.main,
    theme.vars.palette.info.main,
    theme.vars.palette.secondary.main,
    theme.vars.palette.warning.main,
    'var(--teal)',
    theme.vars.palette.grey[500],
    'var(--violet-ink)',
  ];

  const chartSeries = chart.series.map((item) => item.value);

  const chartOptions = useChart({
    chart: { offsetY: 12 },
    colors: chartColors,
    labels: chart.series.map((item) => item.label),
    stroke: { width: 1, colors: [theme.vars.palette.background.paper] },
    fill: { opacity: 0.88 },
    tooltip: {
      y: {
        formatter: (value: number, opts?: { seriesIndex?: number }) =>
          chart.series[opts?.seriesIndex ?? -1]?.display ?? String(value),
      },
    },
    plotOptions: { pie: { donut: { labels: { show: false } } } },
    ...chart.options,
  });

  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} subheader={subheader} />

      <Box
        sx={{
          pt: 4,
          pb: 3,
          px: 3,
          rowGap: 3,
          columnGap: 5,
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Chart
          type="polarArea"
          series={chartSeries}
          options={chartOptions}
          slotProps={{ loading: { p: 3 } }}
          sx={{ width: { xs: 240, md: 280 }, height: { xs: 240, md: 280 } }}
        />

        <ChartLegends
          colors={chartOptions?.colors}
          labels={chartOptions?.labels}
          icons={chart.icons}
          sublabels={chart.series.map((item) => item.display)}
          sx={{ gap: 2.5, display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)' }}
        />
      </Box>

      <Divider sx={{ borderStyle: 'dashed' }} />
      <Box
        sx={{
          display: 'grid',
          typography: 'h4',
          textAlign: 'center',
          gridTemplateColumns: `repeat(${Math.max(footer.length, 1)}, 1fr)`,
        }}
      >
        {footer.map((cell, index) => (
          <Box
            key={index}
            sx={{ py: 2, ...(index < footer.length - 1 && { borderRight: `dashed 1px ${theme.vars.palette.divider}` }) }}
          >
            <Box sx={{ mb: 1, typography: 'body2', color: 'text.secondary' }}>{cell.label}</Box>
            {cell.value}
          </Box>
        ))}
      </Box>
    </Card>
  );
}
