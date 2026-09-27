'use client';

// Template-derived (docs/design/template-derived.json): next-ts
// src/sections/overview/e-commerce/ecommerce-yearly-sales.tsx. Demo wiring as props (anatomy guarded):
//  - the select starts on the FIRST series (the template hard-codes its demo '2023');
//  - each selectable series carries its own legend figures (the template shows demo constants)
//    and a `format` for its axis/tooltip, because rupees, heads and kg never share a scale and a
//    server page cannot hand this client card a formatter function;
//  - the chart is keyed by the selection so a new unit redraws cleanly, and a series may carry a
//    per-month `notes` line for its tooltip;
//  - the series' `empty` text (or the card's) is the chart's own noData message (no extra markup);
//  - month categories ("Apr 2025") draw SHORT on the axis ("Apr", as the template's demo months)
//    and the year or range they cover rides in the select ("Revenue · 2025-26"); the tooltip
//    title keeps the full month.

import type { CardProps } from '@mui/material/Card';
import type { ChartOptions } from '@/components/minimal/chart';

import { useState, useCallback } from 'react';

import Card from '@mui/material/Card';
import { useTheme } from '@mui/material/styles';
import CardHeader from '@mui/material/CardHeader';

import { Chart, useChart, ChartSelect, ChartLegends } from '@/components/minimal/chart';

// ----------------------------------------------------------------------

const MONTH = /^([A-Z][a-z]{2}) (\d{4})$/;

/** "Apr 2025".."Sep 2026" -> axis ["Apr", ..., "Sep"] and range "2025-26"; null when not months. */
function shortMonths(categories: string[]): { axis: string[]; range: string } | null {
  const parts = categories.map((c) => MONTH.exec(c));
  if (parts.length === 0 || parts.some((m) => !m)) return null;
  const years = [...new Set(parts.map((m) => m![2]))];
  const range = years.length === 1 ? years[0] : `${years[0]}-${years[years.length - 1].slice(2)}`;
  return { axis: parts.map((m) => m![1]), range };
}

// ----------------------------------------------------------------------

export type YearlySalesFormat = 'inr' | 'number';

type Props = Omit<CardProps, 'title'> & {
  title?: React.ReactNode;
  subheader?: React.ReactNode;
  /** The chart's noData message when the selected series has no data. */
  empty?: string;
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
      /** This series' own noData message (falls back to `empty`). */
      empty?: string;
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
  const categories = currentSeries?.categories ?? [];
  const months = shortMonths(categories);
  const optionLabel = (item: Props['chart']['series'][number]) => {
    const range = shortMonths(item.categories)?.range;
    return range ? `${item.name} · ${range}` : item.name;
  };

  const chartOptions = useChart({
    colors: chartColors,
    xaxis: { categories: months?.axis ?? categories },
    yaxis: { labels: { formatter: (value: number) => format.axis(value, axisMax) } },
    tooltip: {
      x: { formatter: (value: number | string, opts?: { dataPointIndex?: number }) => categories[opts?.dataPointIndex ?? -1] ?? String(value) },
      y: {
        formatter: (value: number, opts?: { dataPointIndex?: number }) => {
          const note = currentSeries?.notes?.[opts?.dataPointIndex ?? -1];
          return note ? `${format.tooltip(value)} · ${note}` : format.tooltip(value);
        },
      },
    },
    noData: { text: currentSeries?.empty ?? empty },
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
          chart.series.length > 1 || months ? (
            <ChartSelect
              options={chart.series.map(optionLabel)}
              value={currentSeries ? optionLabel(currentSeries) : ''}
              onChange={(label) => handleChangeSeries(chart.series.find((item) => optionLabel(item) === label)?.name ?? label)}
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

      <Chart
        key={currentSeries?.name}
        type="area"
        series={hasData ? currentSeries?.data : []}
        options={chartOptions}
        slotProps={{ loading: { p: 2.5 } }}
        sx={{
          pl: 1,
          py: 2.5,
          pr: 2.5,
          height: 320,
        }}
      />
    </Card>
  );
}
