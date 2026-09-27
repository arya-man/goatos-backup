'use client';

import { useEffect } from 'react';

import type { SettingsState } from '@/layouts/template/settings';

import { useColorScheme } from '@mui/material/styles';
import { AppRouterCacheProvider } from '@mui/material-nextjs/v16-appRouter';
import { AdapterDayjs } from '@mui/x-date-pickers/AdapterDayjs';
import { LocalizationProvider } from '@mui/x-date-pickers/LocalizationProvider';

import '@/components/app/apex-globals';
import { PhoneTapStyles } from '@/components/minimal/_shared/phone-tap-styles';

import { MotionLazy } from '@/layouts/template/animate';

import { defaultSettings, SettingsProvider } from '@/layouts/template/settings';

import { themeConfig } from './theme-config';
import { ThemeProvider } from './theme-provider';
import { LegacyBaseline } from './legacy-baseline';

// ----------------------------------------------------------------------

// Template app/layout.tsx provider stack (AppRouterCacheProvider > SettingsProvider > ThemeProvider),
// trimmed to what admin-web uses: no i18n, no auth provider, no settings drawer. The template's
// LocalizationProvider (from its i18n provider) is mounted directly so MUI X pickers work on every
// page, and PhoneTapStyles raises MUI controls to the 44px WebView tap floor at phone/tablet width.
// Storybook's Minimal decorator (stories/minimal/_minimal.tsx) renders AppThemeStack: the same stack
// without AppRouterCacheProvider, which logs "not compatible with the Pages Router" outside Next.
export function AppThemeProvider({ children, cookieSettings }: { children: React.ReactNode; cookieSettings?: SettingsState }) {
  return (
    <AppRouterCacheProvider options={{ key: 'css' }}>
      <AppThemeStack cookieSettings={cookieSettings}>{children}</AppThemeStack>
    </AppRouterCacheProvider>
  );
}

export function AppThemeStack({ children, cookieSettings }: { children: React.ReactNode; cookieSettings?: SettingsState }) {
  return (
    <SettingsProvider defaultSettings={defaultSettings} cookieSettings={cookieSettings}>
      {/* forceThemeRerender: with CSS variables MUI otherwise keeps `theme.palette` on the DEFAULT
          (light) scheme forever. Mesha's dark hues differ from light (the template's do not), so every
          chart / template section reading theme.palette.* painted light-mode colours in dark
          (#54A02C bars on /sales/sold). guard: theme-palette-follows-mode */}
      <ThemeProvider modeStorageKey={themeConfig.modeStorageKey} defaultMode={themeConfig.defaultMode} forceThemeRerender>
        <LegacyBaseline />
        <PhoneTapStyles />
        <ModeSync />
        <LocalizationProvider dateAdapter={AdapterDayjs}>
          <MotionLazy>{children}</MotionLazy>
        </LocalizationProvider>
      </ThemeProvider>
    </SettingsProvider>
  );
}

// The shell theme toggle (components/kit/theme) writes html[data-theme] + the same storage key MUI
// uses (modeStorageKey). CSS variables already follow the attribute; this keeps MUI's own mode state
// (useColorScheme) in step so MUI never writes a stale attribute back.
function ModeSync() {
  const { mode, setMode } = useColorScheme();
  useEffect(() => {
    const root = document.documentElement;
    const sync = () => {
      const attr = root.getAttribute('data-theme');
      if ((attr === 'light' || attr === 'dark') && attr !== mode) setMode(attr);
    };
    sync();
    const observer = new MutationObserver(sync);
    observer.observe(root, { attributes: true, attributeFilter: ['data-theme'] });
    return () => observer.disconnect();
  }, [mode, setMode]);
  return null;
}
