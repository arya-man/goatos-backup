'use client';

import type { LinkProps } from '@mui/material/Link';

import { mergeClasses } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Link from '@mui/material/Link';
import { styled, useTheme } from '@mui/material/styles';

import { RouterLink } from '@/layouts/template/routes/components';

import { logoClasses } from '@/layouts/template/logo/classes';

// ----------------------------------------------------------------------

/** The Mesha wordmark tile text, same as the backend bootstrap `top_bar.logo_text`. */
const MESHA_LOGO_TEXT = 'मे';

export type LogoProps = LinkProps & {
  isSingle?: boolean;
  disabled?: boolean;
  /** Mesha tile text (bootstrap contract `top_bar.logo_text`). */
  text?: string;
};

export function Logo({
  sx,
  disabled,
  className,
  href = '/',
  isSingle = true,
  text = MESHA_LOGO_TEXT,
  ...other
}: LogoProps) {
  const theme = useTheme();

  // Template brand point (its "OR using local (public folder)" note): the Mesha tile replaces the
  // Minimal SVG mark. Colours come from the theme palette (locked Mesha brand).
  const singleLogo = (
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

  const fullLogo = singleLogo;

  return (
    <LogoRoot
      component={RouterLink}
      href={href}
      aria-label="Logo"
      underline="none"
      className={mergeClasses([logoClasses.root, className])}
      sx={[
        {
          // Declared override (Mesha WebView rule): 40px template tile, 44px tap target; the -2px
          // margin keeps the layout template-exact.
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
      {isSingle ? singleLogo : fullLogo}
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
