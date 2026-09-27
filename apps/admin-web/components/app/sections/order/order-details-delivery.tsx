'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/order/order-details-delivery.tsx. Anatomy guarded; SLOTS replace the demo delivery:
// `title`, `action` (the template's pen IconButton) and `rows`: each row is the template's label/value
// line (its Ship by / Speedy / Tracking No.), so the right-rail block renders any detail facts.
import type { ReactNode } from 'react';
import type { SxProps, Theme } from '@mui/material/styles';

import Box from '@mui/material/Box';
import Stack from '@mui/material/Stack';
import CardHeader from '@mui/material/CardHeader';

import { mergeSx } from '@/components/app/merge-sx';

// ----------------------------------------------------------------------

export type OrderDetailsRow = { key: string; label: ReactNode; value: ReactNode };

type Props = {
  title: ReactNode;
  action?: ReactNode;
  rows: OrderDetailsRow[];
  /** Declared override (template-derived.json): merged after each row's sx (value wrap). */
  slotProps?: { row?: SxProps<Theme> };
};

export function OrderDetailsDelivery({ title, action, rows, slotProps }: Props) {
  return (
    <>
      <CardHeader
        title={title}
        action={action}
      />
      <Stack spacing={1.5} sx={{ p: 3, typography: 'body2' }}>
        {rows.map((row) => (
        <Box key={row.key} sx={mergeSx({ display: 'flex', alignItems: 'center' }, slotProps?.row)}>
          <Box component="span" sx={{ color: 'text.secondary', width: 120, flexShrink: 0 }}>
            {row.label}
          </Box>

          {row.value}
        </Box>
        ))}
      </Stack>
    </>
  );
}
