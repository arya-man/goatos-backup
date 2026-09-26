'use client';

// Copied from the licensed MUI Minimal template
// (next-ts src/sections/overview/e-commerce/ecommerce-yearly-sales.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - the select starts on the FIRST series (the template hard-codes its demo '2023');
//  - each selectable series carries its own legend figures (the template shows demo constants)
//    and a `format` for its axis/tooltip, because rupees, heads and kg never share a scale and a
//    server page cannot hand this client card a formatter function;
//  - the chart is keyed by the selection so a new unit redraws cleanly, and a series may carry a
//    per-month `notes` line for its tooltip;
//  - the series' `empty` (or the card's) renders in place of the chart when it has no data.

import type { CardProps } from '@mui/material/Card';
import type { ChartOptions } from '@/components/minimal/chart';

import { useState, useCallback } from 'react';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import { useTheme } from '@mui/material/styles';
import CardHeader from '@mui/material/CardHeader';

import { Chart, useChart, ChartSelect, ChartLegends } from '@/components/minimal/chart';

// ----------------------------------------------------------------------

export type YearlySalesFormat = 'inr' | 'number';

type Props = Omit<CardProps, 'title'> & {
  title?: React.ReactNode;
  subheader?: React.ReactNode;
  /** Shown in place of the chart when the selected series has no data. */
  empty?: React.ReactNode;
  /** Formats one figure for the axis and the tooltip. */
  formatters: Record<YearlySalesFormat, { axis: (value: number, axisMax: number) => string; tooltip: (value: number) => string }>;
  chart: {
    colors?: string[];
    series: {
      name: string;
      categories: string[];
      format: YearlySalesFormat;
      /** Legend figure per data row, pre-formatted by the page. */
      totals: string[];
      /** Optional second line per category for the tooltip (e.g. the rupees a month's heads earned). */
      notes?: string[];
      /** This series' own empty state (falls back to `empty`). */
      empty?: React.ReactNode;
      data: {
        name: string;
        data: number[];
      }[];
    }[];
    options?: ChartOptions;
  };
};

export function EcommerceYearlySales({ title, subheader, empty, formatters, chart, sx, ...other }: Props) {
  const theme = useTheme();

  const [selectedSeries, setSelectedSeries] = useState(chart.series[0]?.name ?? '');

  const chartColors = chart.colors ?? [theme.palette.primary.main, theme.palette.warning.main];

  const currentSeries = chart.series.find((i) => i.name === selectedSeries) ?? chart.series[0];
  const format = formatters[currentSeries?.format ?? 'number'];
  const axisMax = Math.max(0, ...(currentSeries?.data ?? []).flatMap((row) => row.data));

  const chartOptions = useChart({
    colors: chartColors,
    xaxis: { categories: currentSeries?.categories ?? [] },
    yaxis: { labels: { formatter: (value: number) => format.axis(value, axisMax) } },
    tooltip: {
      y: {
        formatter: (value: number, opts?: { dataPointIndex?: number }) => {
          const note = currentSeries?.notes?.[opts?.dataPointIndex ?? -1];
          return note ? `${format.tooltip(value)} · ${note}` : format.tooltip(value);
        },
      },
    },
    ...chart.options,
  });

  const handleChangeSeries = useCallback((newValue: string) => {
    setSelectedSeries(newValue);
  }, []);

  const hasData = Boolean(currentSeries && currentSeries.categories.length > 0);

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

      <ChartLegends
        colors={chartOptions?.colors}
        labels={currentSeries?.data.map((item) => item.name) ?? []}
        values={currentSeries?.totals ?? []}
        sx={{ px: 3, gap: 3 }}
      />

      {hasData ? (
        <Chart
          key={currentSeries?.name}
          type="area"
          series={currentSeries?.data}
          options={chartOptions}
          slotProps={{ loading: { p: 2.5 } }}
          sx={{
            pl: 1,
            py: 2.5,
            pr: 2.5,
            height: 320,
          }}
        />
      ) : (
        <Box sx={{ px: 3, py: 2.5 }}>{currentSeries?.empty ?? empty}</Box>
      )}
    </Card>
  );
}
