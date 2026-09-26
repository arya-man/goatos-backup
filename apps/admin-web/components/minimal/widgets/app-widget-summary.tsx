// Copied from the licensed MUI Minimal template (sections/overview/app/app-widget-summary.tsx).
// Change: values accept pre-formatted strings; the chart hides below two points.
import type { WidgetSummaryBaseProps } from './types';

import { hasWidgetChart } from './types';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import { useTheme } from '@mui/material/styles';

import { Iconify } from '../iconify';
import { Chart, useChart } from '../chart';
import { fNumber, fPercent } from '../_shared/format-number';

export type AppWidgetSummaryProps = WidgetSummaryBaseProps;

export function AppWidgetSummary({ title, percent, total, caption = 'last 7 days', chart, sx, ...other }: AppWidgetSummaryProps) {
  const theme = useTheme();

  const chartColors = chart?.colors ?? [theme.palette.primary.main];

  const chartOptions = useChart({
    chart: { sparkline: { enabled: true } },
    colors: chartColors,
    stroke: { width: 0 },
    xaxis: { categories: chart?.categories },
    tooltip: {
      y: { formatter: (value: number) => fNumber(value), title: { formatter: () => '' } },
    },
    plotOptions: { bar: { borderRadius: 1.5, columnWidth: '64%' } },
    ...chart?.options,
  });

  const renderTrending = () =>
    percent === undefined ? null : (
      <Box sx={{ gap: 0.5, display: 'flex', alignItems: 'center', flexWrap: 'wrap' }}>
        <Iconify
          width={24}
          icon={percent < 0 ? 'solar:double-alt-arrow-down-bold-duotone' : 'solar:double-alt-arrow-up-bold-duotone'}
          sx={{ flexShrink: 0, color: 'success.main', ...(percent < 0 && { color: 'error.main' }) }}
        />
        <Box component="span" sx={{ typography: 'subtitle2' }}>
          {percent > 0 && '+'}
          {fPercent(percent)}
        </Box>
        {caption ? (
          <Box component="span" sx={{ typography: 'body2', color: 'text.secondary' }}>
            {caption}
          </Box>
        ) : null}
      </Box>
    );

  return (
    <Card
      sx={[
        () => ({ p: 3, display: 'flex', zIndex: 'unset', overflow: 'unset', alignItems: 'center', gap: 2 }),
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        <Box sx={{ typography: 'subtitle2' }}>{title}</Box>
        <Box sx={{ mt: 1.5, mb: 1, typography: 'h3' }}>{typeof total === 'number' ? fNumber(total) : total}</Box>
        {renderTrending()}
      </Box>
      {hasWidgetChart(chart) ? (
        <Chart type="bar" series={[{ data: chart.series }]} options={chartOptions} sx={{ width: 60, height: 40 }} />
      ) : null}
    </Card>
  );
}
