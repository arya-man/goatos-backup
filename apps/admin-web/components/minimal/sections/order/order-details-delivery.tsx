'use client';

// Copied from the licensed MUI Minimal template (next-ts src/sections/order/order-details-delivery.tsx).
// Mesha changes (data plumbing only, anatomy untouched): the header title/action and the
// label/value rows (the template's "Ship by / Speedy / Tracking No.") are the page's, so the same
// right-rail block renders any detail facts; a row may render a node (link, Label) as its value.
// Also serves order-details-shipping.tsx / order-details-payment.tsx, which are the same anatomy.

import type { ReactNode } from 'react';

import Box from '@mui/material/Box';
import Stack from '@mui/material/Stack';
import CardHeader from '@mui/material/CardHeader';

// ----------------------------------------------------------------------

export type OrderDetailsRow = { key: string; label: ReactNode; value: ReactNode };

type Props = {
  title: ReactNode;
  action?: ReactNode;
  rows: OrderDetailsRow[];
  /** Label column width; the template uses 120. */
  labelWidth?: number;
};

export function OrderDetailsDelivery({ title, action, rows, labelWidth = 120 }: Props) {
  return (
    <>
      <CardHeader title={title} action={action} />
      <Stack spacing={1.5} sx={{ p: 3, typography: 'body2' }}>
        {rows.map((row) => (
          <Box key={row.key} sx={{ display: 'flex', alignItems: 'center', minWidth: 0 }}>
            <Box component="span" sx={{ color: 'text.secondary', width: labelWidth, flexShrink: 0 }}>
              {row.label}
            </Box>
            <Box component="span" sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
              {row.value}
            </Box>
          </Box>
        ))}
      </Stack>
    </>
  );
}
