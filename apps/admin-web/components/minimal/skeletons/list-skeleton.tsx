// Copied from the licensed MUI Minimal template (src/sections/mail/mail-skeleton.tsx);
// exports renamed from Mail* to generic list names. Markup unchanged.
import type { BoxProps } from '@mui/material/Box';

import { varAlpha } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Skeleton from '@mui/material/Skeleton';

// ----------------------------------------------------------------------

type NavItemSkeletonProps = BoxProps & {
  itemCount?: number;
};

export function NavItemSkeleton({ itemCount = 6, sx, ...other }: NavItemSkeletonProps) {
  return Array.from({ length: itemCount }, (_, index) => (
    <Box
      key={index}
      sx={[
        (theme) => ({
          py: 1,
          gap: 2,
          display: 'flex',
          alignItems: 'center',
          color: varAlpha(theme.vars.palette.grey['500Channel'], 0.24),
        }),
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      <Skeleton variant="circular" sx={{ width: 32, height: 32, bgcolor: 'currentColor' }} />

      <Skeleton sx={{ width: 0.5, height: 10, bgcolor: 'currentColor' }} />
    </Box>
  ));
}

// ----------------------------------------------------------------------

type ListItemSkeletonProps = BoxProps & {
  itemCount?: number;
};

export function ListItemSkeleton({ sx, itemCount = 6, ...other }: ListItemSkeletonProps) {
  return Array.from({ length: itemCount }, (_, index) => (
    <Box
      key={index}
      sx={[
        {
          py: 1,
          gap: 2,
          display: 'flex',
          alignItems: 'center',
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      <Skeleton variant="circular" sx={{ width: 40, height: 40 }} />

      <Box sx={{ flex: '1 1 auto' }}>
        <Skeleton sx={{ mb: 1, width: 0.75, height: 10 }} />
        <Skeleton sx={{ width: 0.5, height: 10 }} />
      </Box>
    </Box>
  ));
}
