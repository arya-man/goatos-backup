'use client';

import type { CardProps } from '@mui/material/Card';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Button from '@mui/material/Button';

// Template-derived (docs/design/template-derived.json): next-ts
// src/sections/overview/e-commerce/ecommerce-current-balance.tsx. Demo wiring as props (anatomy
// guarded): the big fCurrency figure -> `total` (page-formatted: grams a day, a head count…); the three
// fixed demo order/earning/refund rows -> `rows`; the demo Request / Transfer buttons ->
// optional `actions` (the pair renders only when a page has two real actions).

// ----------------------------------------------------------------------

type Props = Omit<CardProps, 'title'> & {
  title: React.ReactNode;
  total: React.ReactNode;
  rows: { label: React.ReactNode; value: React.ReactNode }[];
  actions?: [{ label: React.ReactNode; onClick?: () => void }, { label: React.ReactNode; onClick?: () => void }];
};

export function EcommerceCurrentBalance({ sx, title, total, rows, actions, ...other }: Props) {
  const renderRow = (label: React.ReactNode, value: React.ReactNode, key?: number) => (
    <Box key={key} sx={{ display: 'flex', typography: 'body2', justifyContent: 'space-between' }}>
      <Box component="span" sx={{ color: 'text.secondary' }}>
        {label}
      </Box>

      <Box component="span">{value}</Box>
    </Box>
  );

  return (
    <Card sx={[{ p: 3 }, ...(Array.isArray(sx) ? sx : [sx])]} {...other}>
      <Box sx={{ mb: 1, typography: 'subtitle2' }}>{title}</Box>

      <Box sx={{ gap: 2, display: 'flex', flexDirection: 'column' }}>
        <Box sx={{ typography: 'h3' }}>{total}</Box>

        {rows.map((row, index) => renderRow(row.label, row.value, index))}

        {actions ? (
          <Box sx={{ gap: 2, display: 'flex' }}>
            <Button fullWidth variant="contained" color="warning" onClick={actions[0].onClick}>
              {actions[0].label}
            </Button>

            <Button fullWidth variant="contained" color="primary" onClick={actions[1].onClick}>
              {actions[1].label}
            </Button>
          </Box>
        ) : null}
      </Box>
    </Card>
  );
}
