'use client';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import CardHeader from '@mui/material/CardHeader';

import { chartRamp, useChartTheme } from '@/components/app/chart-colors';
import { CATEGORY_AXIS_LABELS, fullCategoryTitle } from '@/components/chart-axis-label';
import { BankingBalanceStatistics, formatSeriesValue } from '@/components/app/sections/overview/banking/banking-balance-statistics';


/**
 * Webview (TR1-#6): Apex places a per-bar tooltip beside the hovered bar and, when it does not fit
 * to the right, flips it left WITHOUT clamping, so on a 390 plot a middle bar's tooltip ran off the
 * viewport. A shared tooltip is placed by Apex's clamped path (kept inside the plot). One-measure
 * cards (no ChartSelect) get it with the section's own value formatter restated for that measure;
 * a card with several measures keeps the template tooltip (its formatter follows the select).
 */
function withSharedTooltip(chart: Props['chart']) {
  const only = chart.series.length === 1 ? chart.series[0] : null;
  if (!only || chart.options?.tooltip) return withCategoryAxis(chart, chart.options);
  return withCategoryAxis(chart, {
    tooltip: {
      shared: true,
      intersect: false,
      y: {
        formatter: (value: number, opts?: { seriesIndex: number; dataPointIndex: number }) => {
          const note = opts == null ? null : only.data[opts.seriesIndex]?.notes?.[opts.dataPointIndex];
          const text = formatSeriesValue(value, only.unit ?? '', only.digits ?? 0);
          return note ? `${text} · ${note}` : text;
        },
      },
    },
    ...chart.options,
  });
}

/**
 * A one-series card's category axis keeps each label's head (a load number leads "131 (CPT Castro 1,
 * CPT Castro 2)") and its tooltip title names the category in full (components/chart-axis-label;
 * PR #294 E1/E2). The template sets `xaxis.categories` itself and the caller's options replace the
 * whole `xaxis`, so this restates the categories; a caller with its own xaxis, or a card with a
 * series select (categories follow the selection), keeps the template's axis.
 */
function withCategoryAxis(chart: Props['chart'], options: Props['chart']['options']) {
  const only = chart.series.length === 1 ? chart.series[0] : null;
  if (!only || options?.xaxis) return options;
  const tooltip = (options?.tooltip ?? {}) as NonNullable<NonNullable<Props['chart']['options']>['tooltip']>;
  return {
    ...options,
    xaxis: { categories: only.categories, labels: CATEGORY_AXIS_LABELS },
    tooltip: { ...tooltip, x: tooltip.x ?? { formatter: fullCategoryTitle(only.categories) } },
  };
}

type Props = React.ComponentProps<typeof BankingBalanceStatistics> & { empty?: React.ReactNode };

/** The template balance-statistics card, or (no categories in the first series) the same Card +
 * CardHeader with the page's empty state, so the template file never grows an empty branch. */
export function BalanceStatisticsCard({ empty, ...props }: Props) {
  // The categorical ramp, through the chart theme so the legend follows the scheme from the first
  // paint (N4). It was the template's trio, which Apex cycles: an eleven-breed chart painted three
  // colours over and over (PR #294 E4).
  const theme = useChartTheme();
  const colors = props.chart.colors ?? chartRamp(theme, 'bar');
  if ((props.chart.series[0]?.categories.length ?? 0) === 0) {
    return (
      <Card sx={props.sx}>
        <CardHeader title={props.title} subheader={props.subheader} sx={{ mb: 3 }} />
        <Box sx={{ p: 3 }}>{empty}</Box>
        {props.children}
      </Card>
    );
  }
  return <BankingBalanceStatistics {...props} chart={{ ...props.chart, colors, options: withSharedTooltip(props.chart) }} />;
}
