'use client';

// Copied from the licensed MUI Minimal template (next-ts src/sections/order/order-details-toolbar.tsx).
// Mesha changes (data plumbing only, anatomy untouched):
//  - the heading, status label colour and the date line are the page's (backend copy), not
//    "Order {orderNumber}" / fDateTime;
//  - the status menu + Print/Edit buttons are the page's `actions` slot (our pages carry their own
//    links and drawers), rendered in the template's right-aligned action row;
//  - the back IconButton is a Next link with an aria-label (44px tap on a phone).

import type { ReactNode } from 'react';
import type { LabelColor } from '@/components/minimal/label';

import Box from '@mui/material/Box';
import Stack from '@mui/material/Stack';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';

import Link from '@/components/no-prefetch-link';

import { Label } from '@/components/minimal/label';
import { Iconify } from '@/components/minimal/iconify';

// ----------------------------------------------------------------------

type Props = {
  title: ReactNode;
  status?: ReactNode;
  statusColor?: LabelColor;
  backHref?: string;
  backLabel?: string;
  /** The template's date line under the heading (already formatted). */
  subtitle?: ReactNode;
  actions?: ReactNode;
};

export function OrderDetailsToolbar({ title, status, statusColor = 'default', backHref, backLabel = 'Back', subtitle, actions }: Props) {
  return (
    <Box
      sx={{
        gap: 3,
        display: 'flex',
        mb: { xs: 3, md: 5 },
        flexDirection: { xs: 'column', md: 'row' },
      }}
    >
      <Box sx={{ gap: 1, display: 'flex', alignItems: 'flex-start', minWidth: 0 }}>
        {backHref ? (
          <IconButton component={Link} href={backHref} aria-label={backLabel}>
            <Iconify icon="eva:arrow-ios-back-fill" />
          </IconButton>
        ) : null}

        <Stack spacing={0.5} sx={{ minWidth: 0 }}>
          <Box sx={{ gap: 1, display: 'flex', alignItems: 'center', flexWrap: 'wrap' }}>
            <Typography variant="h4" component="h1" sx={{ overflowWrap: 'anywhere' }}>
              {title}
            </Typography>
            {status ? (
              <Label variant="soft" color={statusColor}>
                {status}
              </Label>
            ) : null}
          </Box>

          {subtitle ? (
            <Typography variant="body2" sx={{ color: 'text.disabled' }}>
              {subtitle}
            </Typography>
          ) : null}
        </Stack>
      </Box>

      {actions ? (
        <Box
          sx={{
            gap: 1.5,
            flexGrow: 1,
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            justifyContent: { xs: 'flex-start', md: 'flex-end' },
          }}
        >
          {actions}
        </Box>
      ) : null}
    </Box>
  );
}
