'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/order/order-details-delivery.tsx. Anatomy guarded; SLOTS replace the demo delivery:
// `title`, `action` (the template's pen IconButton) and `rows`: each row is the template's label/value
// line (its Ship by / Speedy / Tracking No.), so the right-rail block renders any detail facts.
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
};

export function OrderDetailsDelivery({ title, action, rows }: Props) {
  return (
    <>
      <CardHeader
        title={title}
        action={action}
      />
      <Stack spacing={1.5} sx={{ p: 3, typography: 'body2' }}>
        {rows.map((row) => (
        <Box key={row.key} sx={{ display: 'flex', alignItems: 'center' }}>
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
