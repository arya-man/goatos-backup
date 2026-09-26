'use client';

// Copied from the licensed MUI Minimal template
// (next-ts src/sections/overview/banking/banking-balance-statistics.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - the select starts on the FIRST series (the template hard-codes its demo 'Yearly') and is
//    hidden when there is only one series to choose;
//  - each selectable series names its own `unit`/`digits` for the axis and tooltip (g/day, kg and
//    head counts never share a scale, and a server page cannot pass a formatter function), and its
//    own legend `values`/`sublabels` (the template shows demo constants);
//  - a data series may carry per-point `notes` (e.g. "29 animals") appended to its tooltip value;
//  - `empty` renders in place of the legend + chart when the selected series has no categories;
//  - title/subheader take nodes; `children` render at the foot of the card.

import type { CardProps } from '@mui/material/Card';
import type { ChartOptions } from '@/components/minimal/chart';

import { useState, useCallback } from 'react';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import { useTheme } from '@mui/material/styles';
import CardHeader from '@mui/material/CardHeader';

import { Chart, useChart, ChartSelect, ChartLegends } from '@/components/minimal/chart';

// ----------------------------------------------------------------------

export type BalanceStatisticsSeries = {
  name: string;
  categories: string[];
  /** Printed after every axis tick and tooltip value (e.g. "g", "kg"); "" for a plain count. */
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
  empty?: React.ReactNode;
  chart: {
    colors?: string[];
    series: BalanceStatisticsSeries[];
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

export function BankingBalanceStatistics({ title, subheader, empty, chart, sx, children, ...other }: Props) {
  const theme = useTheme();

  const [selectedSeries, setSelectedSeries] = useState(chart.series[0]?.name ?? '');

  const currentSeries = chart.series.find((i) => i.name === selectedSeries) ?? chart.series[0];

  const chartColors = chart.colors ?? [
    theme.palette.primary.dark,
    theme.palette.warning.main,
    theme.palette.info.main,
    theme.palette.secondary.main,
    theme.palette.grey[500],
  ];

  const unit = currentSeries?.unit ?? '';
  const digits = currentSeries?.digits ?? 0;

  const chartOptions = useChart({
    stroke: { width: 2, colors: ['transparent'] },
    colors: chartColors,
    xaxis: { categories: currentSeries?.categories },
    yaxis: { labels: { formatter: (value: number) => formatValue(value, unit, 0) } },
    tooltip: {
      y: {
        formatter: (value: number, opts?: { seriesIndex: number; dataPointIndex: number }) => {
          const note =
            opts == null ? null : currentSeries?.data[opts.seriesIndex]?.notes?.[opts.dataPointIndex];
          return note ? `${formatValue(value, unit, digits)} · ${note}` : formatValue(value, unit, digits);
        },
      },
    },
    ...chart.options,
  });

  const handleChangeSeries = useCallback((newValue: string) => {
    setSelectedSeries(newValue);
  }, []);

  const hasData = (currentSeries?.categories.length ?? 0) > 0;

  return (
    <Card sx={sx} {...other}>
      <CardHeader
        title={title}
        subheader={subheader}
        action={
          chart.series.length > 1 ? (
            <ChartSelect
              options={chart.series.map((item) => item.name)}
              value={currentSeries?.name ?? ''}
              onChange={handleChangeSeries}
            />
          ) : null
        }
        sx={{ mb: 3 }}
      />

      {hasData ? (
        <>
          <ChartLegends
            colors={chartOptions?.colors}
            labels={currentSeries?.data.map((item) => item.name)}
            sublabels={currentSeries?.sublabels}
            values={currentSeries?.values}
            sx={{ px: 3, gap: 3 }}
          />

          <Chart
            key={currentSeries?.name}
            type="bar"
            series={currentSeries?.data.map((item) => ({ name: item.name, data: item.data }))}
            options={chartOptions}
            slotProps={{ loading: { p: 2.5 } }}
            sx={{
              pl: 1,
              py: 2.5,
              pr: 2.5,
              height: 320,
            }}
          />
        </>
      ) : (
        <Box sx={{ px: 3, pb: 3 }}>{empty}</Box>
      )}

      {children}
    </Card>
  );
}
