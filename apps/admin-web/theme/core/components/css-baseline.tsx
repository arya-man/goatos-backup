import type { CSSObject } from '@mui/material/styles';
import type { Theme, Components } from '@mui/material/styles';

import { varAlpha } from 'minimal-shared/utils';

// ----------------------------------------------------------------------

/**
 * App-wide CssBaseline additions (FIXJ7, J1B P1-1), in the MUI theme instead of app/globals.css,
 * which loaded Tailwind (and its preflight, a second reset on top of CssBaseline) for them:
 *
 * 1. The template's own global baseline (Minimal next-ts src/global.css "Baseline"): `ul` without
 *    margin / padding / bullets and `img` capped at its box. Tailwind preflight used to cover both.
 * 2. The native scrollbar: thin, invisible track, a grey thumb that darkens on hover, in both
 *    schemes from the palette. The template's SimpleBar `Scrollbar` draws its own bar; this only styles the browser's
 * native one (page body, plain overflow boxes). Read by the MUI CssBaseline overrides in
 * `components` below AND by theme/with-settings/update-components.ts, whose MuiCssBaseline
 * styleOverrides replace these when settings are applied (MUI deep-merge replaces a function).
 */
export function appCssBaseline(theme: Theme): CSSObject {
  const thumb = varAlpha(theme.vars.palette.grey['500Channel'], 0.36);
  return {
    img: { maxWidth: '100%', verticalAlign: 'middle' },
    ul: { margin: 0, padding: 0, listStyleType: 'none' },
    '*': {
      scrollbarWidth: 'thin',
      scrollbarColor: `${thumb} transparent`,
    },
    '::-webkit-scrollbar': { width: 5, height: 5 },
    '::-webkit-scrollbar-track': { background: 'transparent' },
    '::-webkit-scrollbar-thumb': { background: thumb, borderRadius: 999 },
    '::-webkit-scrollbar-thumb:hover': { background: theme.vars.palette.text.secondary },
    '::-webkit-scrollbar-corner': { background: 'transparent' },
  };
}

const MuiCssBaseline: Components<Theme>['MuiCssBaseline'] = {
  styleOverrides: (theme) => appCssBaseline(theme),
};

export const cssBaseline: Components<Theme> = {
  MuiCssBaseline,
};
