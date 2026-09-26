'use client';

import type { LinkProps } from '@mui/material/Link';

import { mergeClasses } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Link from '@mui/material/Link';
import { styled, useTheme } from '@mui/material/styles';

import { RouterLink } from '@/layouts/template/routes/components';

import { logoClasses } from './classes';

// ----------------------------------------------------------------------

export type LogoProps = LinkProps & {
  isSingle?: boolean;
  disabled?: boolean;
  /** Mesha tile text (bootstrap contract `top_bar.logo_text`). */
  text?: string;
};

// Mesha adaptation: the template logo box (40x40 single, RouterLink root) with the Mesha tile in place
// of the Minimal SVG mark. Colours come from the theme palette (locked Mesha brand).
export function Logo({
  sx,
  disabled,
  className,
  href = '/',
  isSingle = true,
  text = 'M',
  ...other
}: LogoProps) {
  const theme = useTheme();

  const tile = (
    <Box
      component="span"
      aria-hidden="true"
      sx={{
        width: 40,
        height: 40,
        display: 'grid',
        placeItems: 'center',
        borderRadius: '50%',
        bgcolor: theme.vars.palette.primary.main,
        color: theme.vars.palette.primary.contrastText,
        fontWeight: theme.typography.fontWeightExtraBold,
        fontSize: theme.typography.pxToRem(18),
        lineHeight: 1,
      }}
    >
      {text}
    </Box>
  );

  return (
    <LogoRoot
      component={RouterLink}
      href={href}
      aria-label="Logo"
      underline="none"
      className={mergeClasses([logoClasses.root, className])}
      sx={[
        {
          // 40px template tile, 44px tap target (Mesha WebView rule); -2px margin keeps the layout template-exact.
          width: 44,
          height: 44,
          m: '-2px',
          display: 'grid',
          placeItems: 'center',
          ...(!isSingle && { width: 102, height: 36 }),
          ...(disabled && { pointerEvents: 'none' }),
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      {tile}
    </LogoRoot>
  );
}

// ----------------------------------------------------------------------

const LogoRoot = styled(Link)(() => ({
  flexShrink: 0,
  color: 'transparent',
  display: 'inline-flex',
  verticalAlign: 'middle',
}));
