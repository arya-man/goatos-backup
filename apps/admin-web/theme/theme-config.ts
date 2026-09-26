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
    // Template greys (Ravi 2026-09-27): neutrals are exactly the Minimal template's; only the brand
    // and status hues above/below are Mesha. design:guard `template-neutrals` pins these values.
    grey: {
      50: '#FCFDFD',
      100: '#F9FAFB',
      200: '#F4F6F8',
      300: '#DFE3E8',
      400: '#C4CDD5',
      500: '#919EAB',
      600: '#637381',
      700: '#454F5B',
      800: '#1C252E',
      900: '#141A21',
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
  /**
   * Surfaces + ink per scheme: exactly the template's (theme/core/palette.ts text/background, from the
   * grey scale above). The divider is derived like the template (grey-500 at 20%) in core/palette.ts.
   * The nav sits on background.default, as in the template.
   */
  surfaces: {
    light: {
      background: { default: '#FFFFFF', paper: '#FFFFFF', neutral: '#F4F6F8' },
      text: { primary: '#1C252E', secondary: '#637381', disabled: '#919EAB' },
      sidebar: '#FFFFFF',
    },
    dark: {
      background: { default: '#141A21', paper: '#1C252E', neutral: '#28323D' },
      text: { primary: '#FFFFFF', secondary: '#919EAB', disabled: '#637381' },
      sidebar: '#141A21',
    },
  },
};
