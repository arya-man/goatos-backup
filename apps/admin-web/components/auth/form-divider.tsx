'use client';

import type { Theme, SxProps } from '@mui/material/styles';

import Divider from '@mui/material/Divider';

// Template-exact: Minimal_TypeScript_v7.7.0 next-ts src/auth/components/form-divider.tsx.

type FormDividerProps = {
  sx?: SxProps<Theme>;
  label?: React.ReactNode;
};

export function FormDivider({ sx, label = 'OR' }: FormDividerProps) {
  return (
    <Divider
      sx={[
        () => ({
          my: 3,
          typography: 'overline',
          color: 'text.disabled',
          '&::before, :after': { borderTopStyle: 'dashed' },
        }),
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      {label}
    </Divider>
  );
}
