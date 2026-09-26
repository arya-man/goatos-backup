'use client';

import type { Theme, ThemeProviderProps as MuiThemeProviderProps } from '@mui/material/styles';
import type {} from './extend-theme-types';
import type { ThemeOptions } from './types';

import CssBaseline from '@mui/material/CssBaseline';
import { ThemeProvider as ThemeVarsProvider } from '@mui/material/styles';

import { useSettingsContext } from '@/layouts/template/settings';

import { createTheme } from './create-theme';

// ----------------------------------------------------------------------

export type ThemeProviderProps = Partial<MuiThemeProviderProps<Theme>> & {
  themeOverrides?: ThemeOptions;
};

export function ThemeProvider({ themeOverrides, children, ...other }: ThemeProviderProps) {
  const settings = useSettingsContext();

  const theme = createTheme({
    settingsState: settings.state,
    themeOverrides,
  });

  return (
    <ThemeVarsProvider disableTransitionOnChange theme={theme} {...other}>
      <CssBaseline />
      {/* Mesha: ltr only, so the template Rtl cache wrapper (stylis-plugin-rtl) is not carried. */}
      {children}
    </ThemeVarsProvider>
  );
}
