// Copied from the licensed MUI Minimal template (sections/overview/e-commerce/ecommerce-widget-summary.tsx).
// Change: values accept pre-formatted strings; the chart hides below two points.
import type { WidgetSummaryBaseProps } from './types';

import { hasWidgetChart } from './types';

import { varAlpha } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import { useTheme } from '@mui/material/styles';

import { Iconify } from '../iconify';
import { Chart, useChart } from '../chart';
import { fNumber, fPercent } from '../_shared/format-number';

export type EcommerceWidgetSummaryProps = WidgetSummaryBaseProps;

export function EcommerceWidgetSummary({ title, percent, total, chart, caption = 'last week', sx, ...other }: EcommerceWidgetSummaryProps) {
  const theme = useTheme();

  const chartColors = chart?.colors ?? [theme.palette.primary.light, theme.palette.primary.main];

  const chartOptions = useChart({
    chart: { sparkline: { enabled: true } },
    colors: [chartColors[1]],
    xaxis: { categories: chart?.categories },
    grid: { padding: { top: 6, left: 6, right: 6, bottom: 6 } },
    fill: {
      type: 'gradient',
      gradient: {
        colorStops: [
          { offset: 0, color: chartColors[0], opacity: 1 },
          { offset: 100, color: chartColors[1], opacity: 1 },
        ],
      },
    },
    tooltip: {
      y: { formatter: (value: number) => fNumber(value), title: { formatter: () => '' } },
    },
    ...chart?.options,
  });

  const renderTrending = () =>
    percent === undefined ? null : (
      <Box sx={{ gap: 0.5, display: 'flex', alignItems: 'center', flexWrap: 'wrap' }}>
        <Box
          component="span"
          sx={{
            width: 24,
            height: 24,
            display: 'flex',
            borderRadius: '50%',
            position: 'relative',
            alignItems: 'center',
            justifyContent: 'center',
            bgcolor: varAlpha(theme.vars.palette.success.mainChannel, 0.16),
            color: 'success.dark',
            ...theme.applyStyles('dark', { color: 'success.light' }),
            ...(percent < 0 && {
              bgcolor: varAlpha(theme.vars.palette.error.mainChannel, 0.16),
              color: 'error.dark',
              ...theme.applyStyles('dark', { color: 'error.light' }),
            }),
          }}
        >
          <Iconify width={16} icon={percent < 0 ? 'eva:trending-down-fill' : 'eva:trending-up-fill'} />
        </Box>
        <Box component="span" sx={{ typography: 'subtitle2' }}>
          {percent > 0 && '+'}
          {fPercent(percent)}
        </Box>
        {caption ? (
          <Box component="span" sx={{ color: 'text.secondary', typography: 'body2' }}>
            {caption}
          </Box>
        ) : null}
      </Box>
    );

  return (
    <Card sx={[{ p: 3, display: 'flex', alignItems: 'center', gap: 2 }, ...(Array.isArray(sx) ? sx : [sx])]} {...other}>
      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        <Box sx={{ typography: 'subtitle2' }}>{title}</Box>
        <Box sx={{ my: 1.5, typography: 'h3' }}>{typeof total === 'number' ? fNumber(total) : total}</Box>
        {renderTrending()}
      </Box>
      {hasWidgetChart(chart) ? (
        <Chart type="line" series={[{ data: chart.series }]} options={chartOptions} sx={{ width: 100, height: 66 }} />
      ) : null}
    </Card>
  );
}
