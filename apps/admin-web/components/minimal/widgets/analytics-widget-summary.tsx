// Copied from the licensed MUI Minimal template (sections/overview/analytics/analytics-widget-summary.tsx).
// Change: values accept pre-formatted strings; the chart hides below two points.
import type { PaletteColorKey } from '@/theme/core';
import type { WidgetSummaryBaseProps } from './types';

import { hasWidgetChart } from './types';

import { varAlpha } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import { useTheme } from '@mui/material/styles';

import { Iconify } from '../iconify';
import { Chart, useChart } from '../chart';
import { SvgColor } from '../svg-color';
import { MINIMAL_ASSETS } from '../_shared/config';
import { fNumber, fPercent, fShortenNumber } from '../_shared/format-number';

export type AnalyticsWidgetSummaryProps = WidgetSummaryBaseProps & {
  color?: PaletteColorKey;
  icon: React.ReactNode;
};

export function AnalyticsWidgetSummary({ sx, icon, title, total, chart, percent, color = 'primary', caption: _caption, ...other }: AnalyticsWidgetSummaryProps) {
  const theme = useTheme();

  const chartColors = chart?.colors ?? [theme.palette[color].dark];

  const chartOptions = useChart({
    chart: { sparkline: { enabled: true } },
    colors: chartColors,
    xaxis: { categories: chart?.categories },
    grid: { padding: { top: 6, left: 6, right: 6, bottom: 6 } },
    tooltip: {
      y: { formatter: (value: number) => fNumber(value), title: { formatter: () => '' } },
    },
    markers: { strokeWidth: 0 },
    ...chart?.options,
  });

  return (
    <Card
      sx={[
        () => ({
          p: 3,
          boxShadow: 'none',
          position: 'relative',
          isolation: 'isolate',
          color: `${color}.darker`,
          backgroundColor: 'common.white',
          backgroundImage: `linear-gradient(135deg, ${varAlpha(theme.vars.palette[color].lighterChannel, 0.48)}, ${varAlpha(theme.vars.palette[color].lightChannel, 0.48)})`,
        }),
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      <Box sx={{ width: 48, height: 48, mb: 3 }}>{icon}</Box>

      {percent === undefined ? null : (
        <Box sx={{ top: 16, gap: 0.5, right: 16, display: 'flex', position: 'absolute', alignItems: 'center' }}>
          <Iconify width={20} icon={percent < 0 ? 'eva:trending-down-fill' : 'eva:trending-up-fill'} />
          <Box component="span" sx={{ typography: 'subtitle2' }}>
            {percent > 0 && '+'}
            {fPercent(percent)}
          </Box>
        </Box>
      )}

      <Box sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', justifyContent: 'flex-end' }}>
        <Box sx={{ flexGrow: 1, minWidth: 112 }}>
          <Box sx={{ mb: 1, typography: 'subtitle2' }}>{title}</Box>
          <Box sx={{ typography: 'h4' }}>{typeof total === 'number' ? fShortenNumber(total) : total}</Box>
        </Box>
        {hasWidgetChart(chart) ? (
          <Chart type="line" series={[{ data: chart.series }]} options={chartOptions} sx={{ width: 84, height: 56 }} />
        ) : null}
      </Box>

      <SvgColor
        src={`${MINIMAL_ASSETS}/background/shape-square.svg`}
        sx={{ top: 0, left: -20, width: 240, zIndex: -1, height: 240, opacity: 0.24, position: 'absolute', color: `${color}.main` }}
      />
    </Card>
  );
}
