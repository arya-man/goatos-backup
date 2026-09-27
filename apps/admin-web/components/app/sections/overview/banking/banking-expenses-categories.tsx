'use client';

// Template-derived (docs/design/template-derived.json): next-ts
// src/sections/overview/banking/banking-expenses-categories.tsx. Demo wiring as props (anatomy
// guarded): fCurrency legend sub-labels / tooltip -> series display; the two demo footer cells
// ('Categories' 9 / $18,765) -> footer. Colours come in through chart.colors (template prop).
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
  /** The two footer cells (the template shows demo 'Categories' 9 / $18,765). */
  footer: [{ label: React.ReactNode; value: React.ReactNode }, { label: React.ReactNode; value: React.ReactNode }];
  chart: {
    colors?: string[];
    icons?: React.ReactNode[];
    series: {
      label: string;
      value: number;
      /** Page-formatted figure for the legend sub-label and tooltip (the template prints fCurrency). */
      display: string;
    }[];
    options?: ChartOptions;
  };
};

export function BankingExpensesCategories({ title, subheader, footer, chart, sx, ...other }: Props) {
  const theme = useTheme();

  const chartColors = chart.colors ?? [
    theme.palette.secondary.dark,
    theme.palette.error.main,
    theme.palette.primary.main,
    theme.palette.warning.main,
    theme.palette.info.dark,
    theme.palette.info.main,
    theme.palette.success.main,
    theme.palette.warning.dark,
  ];

  const chartSeries = chart.series.map((item) => item.value);

  const chartOptions = useChart({
    chart: { offsetY: 12 },
    colors: chartColors,
    labels: chart.series.map((item) => item.label),
    stroke: { width: 1, colors: [theme.palette.background.paper] },
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
          gridTemplateColumns: 'repeat(2, 1fr)',
        }}
      >
        <Box sx={{ py: 2, borderRight: `dashed 1px ${theme.vars.palette.divider}` }}>
          <Box sx={{ mb: 1, typography: 'body2', color: 'text.secondary' }}>{footer[0].label}</Box>
          {footer[0].value}
        </Box>

        <Box sx={{ py: 2 }}>
          <Box sx={{ mb: 1, typography: 'body2', color: 'text.secondary' }}>{footer[1].label}</Box>
          {footer[1].value}
        </Box>
      </Box>
    </Card>
  );
}
