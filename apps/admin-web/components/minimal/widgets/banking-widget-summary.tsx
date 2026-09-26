// Adapted from the licensed MUI Minimal template (sections/overview/banking/banking-overview.tsx,
// the income/expenses summary tile) as a standalone KPI card. Chart: banking-overview's line options
// (stroke 3) on a sparkline, the template's mini-chart pattern.
import type { PaletteColorKey } from '@/theme/core';
import type { WidgetSummaryBaseProps } from './types';

import { hasWidgetChart } from './types';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Tooltip from '@mui/material/Tooltip';
import { useTheme } from '@mui/material/styles';

import { Label } from '../label';
import { Chart, useChart } from '../chart';
import { Iconify } from '../iconify';
import type { IconifyName } from '../iconify';
import { fNumber, fPercent } from '../_shared/format-number';

export type BankingWidgetSummaryProps = WidgetSummaryBaseProps & {
  color?: PaletteColorKey;
  /** Iconify icon name for the round badge. */
  icon?: IconifyName;
  /** Optional info tooltip next to the title. */
  hint?: string;
};

export function BankingWidgetSummary({
  title,
  total,
  percent,
  chart,
  hint,
  icon = 'eva:diagonal-arrow-left-down-fill',
  color = 'primary',
  caption: _caption,
  sx,
  ...other
}: BankingWidgetSummaryProps) {
  const theme = useTheme();

  const chartOptions = useChart({
    chart: { sparkline: { enabled: true } },
    colors: chart?.colors ?? [theme.palette[color].main],
    xaxis: { categories: chart?.categories },
    stroke: { width: 3 },
    tooltip: {
      y: { formatter: (value: number) => fNumber(value), title: { formatter: () => '' } },
    },
    ...chart?.options,
  });

  return (
    <Card sx={[{ p: 3, position: 'relative' }, ...(Array.isArray(sx) ? sx : [sx])]} {...other}>
      <Box sx={{ display: 'flex', gap: { xs: 1.5, md: 2.5 }, alignItems: 'flex-start' }}>
        <Box
          component="span"
          sx={{
            width: 48,
            height: 48,
            flexShrink: 0,
            borderRadius: '50%',
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: `${color}.lighter`,
            bgcolor: `${color}.darker`,
          }}
        >
          <Iconify width={24} icon={icon} />
        </Box>

        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Box sx={{ mb: 1, gap: 0.5, display: 'flex', alignItems: 'center', typography: 'subtitle2' }}>
            {title}
            {hint ? (
              <Tooltip title={hint} placement="top">
                <Iconify width={16} icon="eva:info-outline" sx={{ color: 'text.disabled' }} />
              </Tooltip>
            ) : null}
          </Box>
          <Box sx={{ typography: 'h4' }}>{typeof total === 'number' ? fNumber(total) : total}</Box>
        </Box>

        {percent === undefined ? null : (
          <Label
            color={percent < 0 ? 'error' : 'success'}
            startIcon={
              <Iconify
                width={24}
                icon={percent < 0 ? 'solar:double-alt-arrow-down-bold-duotone' : 'solar:double-alt-arrow-up-bold-duotone'}
              />
            }
            sx={{ flexShrink: 0 }}
          >
            {percent > 0 && '+'}
            {fPercent(percent)}
          </Label>
        )}
      </Box>
      {hasWidgetChart(chart) ? (
        <Chart type="line" series={[{ data: chart.series }]} options={chartOptions} sx={{ mt: 2, height: 56 }} />
      ) : null}
    </Card>
  );
}
