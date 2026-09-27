'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/overview/banking/banking-balance-statistics.tsx with ONLY its demo wiring as props:
// the default select (demo 'Yearly') is the first real series; the fCurrency tooltip and the fixed
// legend values / +43% sublabels become each series' own unit / digits / values / sublabels / notes;
// `children` render at the card foot. Markup, sx and chart options are the template's
// (guard: template-derived-anatomy).
import type { CardProps } from '@mui/material/Card';
import type { ChartOptions } from '@/components/minimal/chart';

import { useState, useCallback } from 'react';

import Card from '@mui/material/Card';
import { useTheme } from '@mui/material/styles';
import CardHeader from '@mui/material/CardHeader';

import { Chart, useChart, ChartSelect, ChartLegends } from '@/components/minimal/chart';

// ----------------------------------------------------------------------

export type BalanceStatisticsSeries = {
  name: string;
  categories: string[];
  /** Printed after every tooltip value (e.g. "g", "kg"); a unit ending in "·" is a prefix ("₹·"). */
  unit?: string;
  digits?: number;
  values?: string[];
  sublabels?: string[];
  data: {
    name: string;
    data: (number | null)[];
    notes?: (string | null)[];
  }[];
};

type Props = Omit<CardProps, 'title'> & {
  title?: React.ReactNode;
  subheader?: React.ReactNode;
  chart: {
    colors?: string[];
    series: BalanceStatisticsSeries[];
    options?: ChartOptions;
  };
};

export function formatSeriesValue(value: number | null | undefined, unit = '', digits = 0) {
  if (value == null || Number.isNaN(value)) return '–';
  const text = value.toLocaleString('en-IN', { minimumFractionDigits: digits, maximumFractionDigits: digits });
  if (unit.endsWith('·')) return `${value < 0 ? '−' : ''}${unit.slice(0, -1)}${text.replace('-', '')}`;
  return unit ? `${text} ${unit}` : text;
}

export function BankingBalanceStatistics({ title, subheader, chart, sx, children, ...other }: Props) {
  const theme = useTheme();

  const [selectedSeries, setSelectedSeries] = useState(chart.series[0]?.name ?? '');

  const currentSeries = chart.series.find((i) => i.name === selectedSeries) ?? chart.series[0];

  const chartColors = chart.colors ?? [
    theme.palette.primary.dark,
    theme.palette.warning.main,
    theme.palette.info.main,
  ];

  const chartOptions = useChart({
    stroke: { width: 2, colors: ['transparent'] },
    colors: chartColors,
    xaxis: { categories: currentSeries?.categories },
    tooltip: {
      y: {
        formatter: (value: number, opts?: { seriesIndex: number; dataPointIndex: number }) => {
          const note = opts == null ? null : currentSeries?.data[opts.seriesIndex]?.notes?.[opts.dataPointIndex];
          const text = formatSeriesValue(value, currentSeries?.unit ?? '', currentSeries?.digits ?? 0);
          return note ? `${text} · ${note}` : text;
        },
      },
    },
    ...chart.options,
  });

  const handleChangeSeries = useCallback((newValue: string) => {
    setSelectedSeries(newValue);
  }, []);

  return (
    <Card sx={sx} {...other}>
      <CardHeader
        title={title}
        subheader={subheader}
        action={
          <ChartSelect
            options={chart.series.map((item) => item.name)}
            value={currentSeries?.name ?? ''}
            onChange={handleChangeSeries}
          />
        }
        sx={{ mb: 3 }}
      />

      <ChartLegends
        colors={chartOptions?.colors}
        labels={currentSeries?.data.map((item) => item.name)}
        sublabels={currentSeries?.sublabels}
        values={currentSeries?.values}
        sx={{ px: 3, gap: 3 }}
      />

      <Chart
        type="bar"
        series={currentSeries?.data as { name: string; data: number[] }[]}
        options={chartOptions}
        slotProps={{ loading: { p: 2.5 } }}
        sx={{
          pl: 1,
          py: 2.5,
          pr: 2.5,
          height: 320,
        }}
      />

      {children}
    </Card>
  );
}
