'use client';

// Copied from the licensed MUI Minimal template
// (next-ts src/sections/overview/e-commerce/ecommerce-widget-summary.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - `total` also takes a pre-formatted figure (₹ lakh grouping, "₹422 per kg") from the page;
//  - `percent` is optional and `caption` replaces the template's literal "last week" (copy comes
//    from the backend contract); with no percent the caption sits alone in the trend row;
//  - the sparkline hides below two points (one month is not a trend), and `chart.format` picks
//    the tooltip formatter because a server page cannot pass a function to this client card.

import type { CardProps } from '@mui/material/Card';
import type { ChartOptions } from '@/components/minimal/chart';

import { varAlpha } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import { useTheme } from '@mui/material/styles';

import { fNumber, fPercent } from '@/components/minimal/_shared/format-number';

import { Iconify } from '@/components/minimal/iconify';
import { Chart, useChart } from '@/components/minimal/chart';

// ----------------------------------------------------------------------

type Props = Omit<CardProps, 'title'> & {
  title: React.ReactNode;
  total: number | React.ReactNode;
  percent?: number;
  caption?: React.ReactNode;
  chart: {
    colors?: string[];
    categories: string[];
    series: number[];
    /** Tooltip figure: rupees or a plain count. */
    format?: 'inr' | 'number';
    options?: ChartOptions;
  };
};

const inrFigure = (value: number) => `₹${fNumber(value)}`;

export function EcommerceWidgetSummary({ title, percent, total, caption, chart, sx, ...other }: Props) {
  const theme = useTheme();

  const chartColors = chart.colors ?? [theme.palette.primary.light, theme.palette.primary.main];

  const chartOptions = useChart({
    chart: { sparkline: { enabled: true } },
    colors: [chartColors[1]],
    xaxis: { categories: chart.categories },
    grid: {
      padding: {
        top: 6,
        left: 6,
        right: 6,
        bottom: 6,
      },
    },
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
      y: {
        formatter: (value: number) => (chart.format === 'inr' ? inrFigure(value) : fNumber(value)),
        title: { formatter: () => '' },
      },
    },
    ...chart.options,
  });

  const renderTrending = () => (
    <Box sx={{ gap: 0.5, display: 'flex', alignItems: 'center', flexWrap: 'wrap' }}>
      {percent === undefined ? null : (
        <>
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
              ...theme.applyStyles('dark', {
                color: 'success.light',
              }),
              ...(percent < 0 && {
                bgcolor: varAlpha(theme.vars.palette.error.mainChannel, 0.16),
                color: 'error.dark',
                ...theme.applyStyles('dark', {
                  color: 'error.light',
                }),
              }),
            }}
          >
            <Iconify
              width={16}
              icon={percent < 0 ? 'eva:trending-down-fill' : 'eva:trending-up-fill'}
            />
          </Box>

          <Box component="span" sx={{ typography: 'subtitle2' }}>
            {percent > 0 && '+'}
            {fPercent(percent)}
          </Box>
        </>
      )}

      {caption ? (
        <Box component="span" sx={{ color: 'text.secondary', typography: 'body2' }}>
          {caption}
        </Box>
      ) : null}
    </Box>
  );

  return (
    <Card
      sx={[{ p: 3, display: 'flex', alignItems: 'center' }, ...(Array.isArray(sx) ? sx : [sx])]}
      {...other}
    >
      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        <Box sx={{ typography: 'subtitle2' }}>{title}</Box>

        <Box sx={{ my: 1.5, typography: 'h3' }}>{typeof total === 'number' ? fNumber(total) : total}</Box>

        {renderTrending()}
      </Box>

      {chart.series.length > 1 ? (
        <Chart
          type="line"
          series={[{ data: chart.series }]}
          options={chartOptions}
          sx={{ width: 100, height: 66, flexShrink: 0 }}
        />
      ) : null}
    </Card>
  );
}
