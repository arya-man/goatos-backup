'use client';

// Copied from the licensed MUI Minimal template (next-ts src/sections/order/order-details-items.tsx).
// Mesha changes (data plumbing only, anatomy untouched): header title/action, the item rows
// (rounded avatar, name + caption, quantity, right figure) and the totals block take the page's
// already-formatted values instead of the product mock + fCurrency; a row may link somewhere and
// may carry a status node beside the quantity. Phones keep the template's 640px min row width
// inside the Scrollbar (the card scrolls, the page never does).

import type { ReactNode } from 'react';
import type { CardProps } from '@mui/material/Card';

import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Avatar from '@mui/material/Avatar';
import CardHeader from '@mui/material/CardHeader';
import ListItemText from '@mui/material/ListItemText';

import { Scrollbar } from '@/components/minimal/scrollbar';

// ----------------------------------------------------------------------

export type OrderDetailsItem = {
  key: string;
  name: ReactNode;
  caption?: ReactNode;
  /** Avatar initial / icon when there is no cover image. */
  avatar?: ReactNode;
  coverUrl?: string;
  quantity?: ReactNode;
  status?: ReactNode;
  figure?: ReactNode;
};

export type OrderDetailsTotal = { key: string; label: ReactNode; value: ReactNode; emphasis?: boolean; negative?: boolean };

type Props = Omit<CardProps, 'title'> & {
  title: ReactNode;
  action?: ReactNode;
  items: OrderDetailsItem[];
  totals?: OrderDetailsTotal[];
  empty?: ReactNode;
};

export function OrderDetailsItems({ title, action, items, totals = [], empty, sx, ...other }: Props) {
  const renderTotal = () => (
    <Box
      sx={{
        p: 3,
        gap: 2,
        display: 'flex',
        textAlign: 'right',
        typography: 'body2',
        alignItems: 'flex-end',
        flexDirection: 'column',
      }}
    >
      {totals.map((total) => (
        <Box key={total.key} sx={{ display: 'flex', ...(total.emphasis && { typography: 'subtitle1' }) }}>
          <Box sx={total.emphasis ? undefined : { color: 'text.secondary' }}>{total.label}</Box>
          <Box sx={{ width: 160, ...(total.negative && { color: 'error.main' }) }}>{total.value}</Box>
        </Box>
      ))}
    </Box>
  );

  return (
    <Card sx={sx} {...other}>
      <CardHeader title={title} action={action} />

      <Scrollbar>
        {items.map((item) => (
          <Box
            key={item.key}
            sx={[
              (theme) => ({
                p: 3,
                minWidth: 640,
                display: 'flex',
                alignItems: 'center',
                gap: 2,
                borderBottom: `dashed 2px ${theme.vars.palette.background.neutral}`,
              }),
            ]}
          >
            <Avatar src={item.coverUrl} variant="rounded" sx={{ width: 48, height: 48 }}>
              {item.avatar}
            </Avatar>

            <ListItemText
              primary={item.name}
              secondary={item.caption}
              slotProps={{
                primary: { sx: { typography: 'body2' } },
                secondary: {
                  sx: { mt: 0.5, color: 'text.disabled' },
                },
              }}
            />

            {item.status ? <Box sx={{ flexShrink: 0 }}>{item.status}</Box> : null}

            {item.quantity !== undefined ? <Box sx={{ typography: 'body2', flexShrink: 0 }}>{item.quantity}</Box> : null}

            <Box sx={{ width: 110, textAlign: 'right', typography: 'subtitle2', flexShrink: 0 }}>{item.figure}</Box>
          </Box>
        ))}
        {items.length === 0 && empty ? <Box sx={{ p: 3 }}>{empty}</Box> : null}
      </Scrollbar>

      {totals.length ? renderTotal() : null}
    </Card>
  );
}
