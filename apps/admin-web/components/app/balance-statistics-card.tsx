'use client';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import CardHeader from '@mui/material/CardHeader';

import { chartColor, useChartTheme } from '@/components/app/chart-colors';
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
  if (!only || chart.options?.tooltip) return chart.options;
  return {
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
  };
}

type Props = React.ComponentProps<typeof BankingBalanceStatistics> & { empty?: React.ReactNode };

/** The template balance-statistics card, or (no categories in the first series) the same Card +
 * CardHeader with the page's empty state, so the template file never grows an empty branch. */
export function BalanceStatisticsCard({ empty, ...props }: Props) {
  // The template's default trio, through the chart theme so the legend follows the scheme from the
  // first paint (N4).
  const theme = useChartTheme();
  const colors = props.chart.colors ?? (["primary.dark", "warning", "info"] as const).map((key) => chartColor(theme, key));
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
