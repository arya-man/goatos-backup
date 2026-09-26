'use client';

// Copied from the licensed MUI Minimal template (next-ts src/sections/order/order-details-customer.tsx).
// Mesha changes (data plumbing only, anatomy untouched): the header, the avatar initial, the name
// and the secondary lines are the page's (supplier / vendor / operator instead of the mock
// customer); the "Add to blacklist" button becomes an optional `action` node.

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
      <CardHeader title={title} action={headerAction} />
      <Box sx={{ p: 3, display: 'flex' }}>
        <Avatar alt={name} src={avatarUrl} sx={{ width: 48, height: 48, mr: 2 }}>
          {name.slice(0, 1).toUpperCase()}
        </Avatar>

        <Stack spacing={0.5} sx={{ typography: 'body2', alignItems: 'flex-start', minWidth: 0 }}>
          <Typography variant="subtitle2">{name}</Typography>
          {lines.map((line, index) => (
            <Box key={index} sx={{ color: 'text.secondary', overflowWrap: 'anywhere' }}>
              {line}
            </Box>
          ))}
          {action ? <Box sx={{ mt: 1 }}>{action}</Box> : null}
        </Stack>
      </Box>
    </>
  );
}
