'use client';

// Template-derived (docs/design/template-derived.json): sections/invoice/invoice-analytic.tsx.
// Demo wiring as props: the "N invoices" + currency lines take the page's own readings — `caption` (the
// muted middle line) and `value` (the subtitle2 line, defaults to the formatted total) — so a queue
// can show "38%" and a count instead of an invoice total. Layout, sizes and colours are the template's.
import type { IconifyName } from '@/components/minimal/iconify';

import { varAlpha } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';

import { fNumber } from '@/components/minimal/_shared/format-number';

import { Iconify } from '@/components/minimal/iconify';

// ----------------------------------------------------------------------

type Props = {
  title: React.ReactNode;
  total: number;
  color?: string;
  percent: number;
  icon: IconifyName;
  caption?: React.ReactNode;
  value?: React.ReactNode;
};

export function InvoiceAnalytic({ title, total, icon, color, percent, caption, value }: Props) {
  return (
    <Box
      // Phone stat strips lay these cells out two to a row (theme/app-baseline.tsx, PR #294 L-C6).
      data-stat-cell=""
      sx={{
        width: 1,
        gap: 2.5,
        minWidth: 200,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <Box
        sx={{
          display: 'flex',
          position: 'relative',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Iconify icon={icon} width={32} sx={{ color, position: 'absolute' }} />

        <CircularProgress
          size={56}
          thickness={2}
          value={percent}
          variant="determinate"
          sx={{ color, opacity: 0.48 }}
        />

        <CircularProgress
          size={56}
          value={100}
          thickness={3}
          variant="determinate"
          sx={[
            (theme) => ({
              top: 0,
              left: 0,
              opacity: 0.48,
              position: 'absolute',
              color: varAlpha(theme.vars.palette.grey['500Channel'], 0.16),
            }),
          ]}
        />
      </Box>

      <div>
        <Typography variant="subtitle1">{title}</Typography>

        {caption != null ? (
          <Box
            component="span"
            sx={{ my: 0.5, display: 'block', typography: 'body2', color: 'text.disabled' }}
          >
            {caption}
          </Box>
        ) : null}

        <Box component="span" sx={{ typography: 'subtitle2' }}>
          {value ?? fNumber(total)}
        </Box>
      </div>
    </Box>
  );
}
