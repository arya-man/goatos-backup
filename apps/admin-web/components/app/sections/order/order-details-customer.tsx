'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/order/order-details-customer.tsx. Anatomy guarded; SLOTS replace the demo customer:
// `title`, `headerAction` (the template's pen IconButton), `name` / `avatarUrl`, `lines` (its email +
// IP-address lines), `action` (its "Add to blacklist" button).
import type { ReactNode } from 'react';

import Box from '@mui/material/Box';
import Stack from '@mui/material/Stack';
import Avatar from '@mui/material/Avatar';
import CardHeader from '@mui/material/CardHeader';
import Typography from '@mui/material/Typography';

// ----------------------------------------------------------------------

type Props = {
  title: ReactNode;
  headerAction?: ReactNode;
  name: string;
  avatarUrl?: string;
  lines?: ReactNode[];
  action?: ReactNode;
};

export function OrderDetailsCustomer({ title, headerAction, name, avatarUrl, lines = [], action }: Props) {
  return (
    <>
      <CardHeader
        title={title}
        action={headerAction}
      />
      <Box sx={{ p: 3, display: 'flex' }}>
        <Avatar
          alt={name}
          src={avatarUrl}
          sx={{ width: 48, height: 48, mr: 2 }}
        >
          {name.slice(0, 1).toUpperCase()}
        </Avatar>

        <Stack spacing={0.5} sx={{ typography: 'body2', alignItems: 'flex-start' }}>
          <Typography variant="subtitle2">{name}</Typography>
          {lines.map((line, index) => (
            <Box key={index} sx={{ color: 'text.secondary' }}>{line}</Box>
          ))}

          {action}
        </Stack>
      </Box>
    </>
  );
}
