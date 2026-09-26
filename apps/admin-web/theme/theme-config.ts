import type { Theme, Direction, CommonColors, ThemeProviderProps } from '@mui/material/styles';
import type { ThemeCssVariables } from './types';
import type { PaletteColorKey, PaletteColorNoChannels } from './core/palette';

// ----------------------------------------------------------------------

export type ThemeConfig = {
  direction: Direction;
  classesPrefix: string;
  cssVariables: ThemeCssVariables;
  defaultMode: ThemeProviderProps<Theme>['defaultMode'];
  modeStorageKey: ThemeProviderProps<Theme>['modeStorageKey'];
  fontFamily: Record<'primary' | 'secondary', string>;
  palette: Record<PaletteColorKey, PaletteColorNoChannels> & {
    common: Pick<CommonColors, 'black' | 'white'>;
    grey: {
      [K in 50 | 100 | 200 | 300 | 400 | 500 | 600 | 700 | 800 | 900 as `${K}`]: string;
    };
  };
  paletteDark: Record<PaletteColorKey, PaletteColorNoChannels>;
  surfaces: Record<'light' | 'dark', MeshaSurfaces>;
};

export type MeshaSurfaces = {
  background: { default: string; paper: string; neutral: string };
  text: { primary: string; secondary: string; disabled: string };
  divider: string;
  sidebar: string;
};

export const themeConfig: ThemeConfig = {
  /** **************************************
   * Base
   *************************************** */
  defaultMode: 'dark',
  modeStorageKey: 'mesha.shell.theme',
  direction: 'ltr',
  classesPrefix: 'minimal',
  /** **************************************
   * Css variables
   *************************************** */
  cssVariables: {
    cssVarPrefix: '',
    colorSchemeSelector: 'data-theme',
  },
  /** **************************************
   * Typography
   *************************************** */
  fontFamily: {
    primary: 'Public Sans Variable',
    secondary: 'Barlow',
  },
  /** **************************************
   * Palette
   *************************************** */
  palette: {
    // LOCKED MESHA PALETTE (light scheme). Canonical source: app/mesha-theme.css (:root.light).
    // Only colours differ from the Minimal template; the shape of this object is template-exact.
    // Every hex here exists in app/mesha-theme.css. Where Mesha has fewer tonal steps than Minimal
    // (no deeper purple/blue/amber), the step repeats the nearest locked value instead of inventing one.
    primary: {
      lighter: '#ECF7E0',
      light: '#7BC13F',
      main: '#54A02C',
      dark: '#44831F',
      darker: '#20470E',
      contrastText: '#FFFFFF',
    },
    secondary: {
      lighter: '#EDE7FB',
      light: '#A78BF5',
      main: '#7A5BD1',
      dark: '#7A5BD1',
      darker: '#7A5BD1',
      contrastText: '#FFFFFF',
    },
    info: {
      lighter: '#E5EFFB',
      light: '#5B9BE8',
      main: '#2B66B8',
      dark: '#2B66B8',
      darker: '#2B66B8',
      contrastText: '#FFFFFF',
    },
    success: {
      lighter: '#ECF7E0',
      light: '#7CCB45',
      main: '#3F9A28',
      dark: '#44831F',
      darker: '#20470E',
      contrastText: '#FFFFFF',
    },
    warning: {
      lighter: '#FBF1DE',
      light: '#E0A53A',
      main: '#B5791A',
      dark: '#B5791A',
      darker: '#B5791A',
      contrastText: '#FFFFFF',
    },
    error: {
      lighter: '#FBE6E6',
      light: '#F0635F',
      main: '#CC3D3D',
      dark: '#B83232',
      darker: '#8F1D1D',
      contrastText: '#FFFFFF',
    },
    // Mesha has no cool-grey scale: its neutrals are green-tinted. Each Minimal grey step maps to the
    // nearest locked Mesha neutral (app/mesha-theme.css), light to dark, so action/hover/selected,
    // outlines and the backdrop (all derived from grey here) stay on the Mesha palette.
    grey: {
      50: '#F4F7F2', // --bg (light)
      100: '#F1F5EF', // --panel-2 (light)
      200: '#ECF1E8', // --sidebar (light)
      300: '#E2E8E1', // --line (light)
      400: '#9FB6A6', // --sidebar-ink (dark)
      500: '#94A89A', // --muted (dark)
      600: '#6E8377', // --faint (dark)
      700: '#46564B', // --sidebar-ink (light)
      800: '#1D2820', // --panel-2 (dark)
      900: '#0E1512', // --bg (dark)
    },
    common: {
      black: '#000000',
      white: '#FFFFFF',
    },
  },
  paletteDark: {
    // LOCKED MESHA PALETTE (dark scheme). Canonical source: app/mesha-theme.css (:root).
    primary: {
      lighter: '#C6ECA3',
      light: '#A6E07A',
      main: '#7CCB45',
      dark: '#69BA37',
      darker: '#4E8F27',
      contrastText: '#08130B',
    },
    secondary: {
      lighter: '#EDE7FB',
      light: '#A78BF5',
      main: '#A78BF5',
      dark: '#7A5BD1',
      darker: '#7A5BD1',
      contrastText: '#08130B',
    },
    info: {
      lighter: '#E5EFFB',
      light: '#5B9BE8',
      main: '#5B9BE8',
      dark: '#2B66B8',
      darker: '#2B66B8',
      contrastText: '#08130B',
    },
    success: {
      lighter: '#C6ECA3',
      light: '#A6E07A',
      main: '#7CCB45',
      dark: '#69BA37',
      darker: '#4E8F27',
      contrastText: '#08130B',
    },
    warning: {
      lighter: '#FBF1DE',
      light: '#E0A53A',
      main: '#E0A53A',
      dark: '#B5791A',
      darker: '#B5791A',
      contrastText: '#08130B',
    },
    error: {
      lighter: '#FFC9C6',
      light: '#F0A19E',
      main: '#F0635F',
      dark: '#CC3D3D',
      darker: '#8F1D1D',
      contrastText: '#08130B',
    },
  },
  /** Mesha surfaces + ink per scheme (app/mesha-theme.css). */
  surfaces: {
    light: {
      background: { default: '#F4F7F2', paper: '#FFFFFF', neutral: '#F1F5EF' },
      text: { primary: '#16201B', secondary: '#5E6E64', disabled: '#8A998F' },
      divider: '#E2E8E1',
      sidebar: '#ECF1E8',
    },
    dark: {
      background: { default: '#0E1512', paper: '#161F1A', neutral: '#1D2820' },
      text: { primary: '#E9F1EA', secondary: '#94A89A', disabled: '#6E8377' },
      divider: '#26332B',
      sidebar: '#0A0F0C',
    },
  },
};
