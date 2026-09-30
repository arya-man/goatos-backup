'use client';

import { useTheme } from '@mui/material/styles';

import { AppThemeProvider } from './app-theme-provider';

// ----------------------------------------------------------------------

// For root-segment special files that Next can mount WITHOUT app/layout.tsx. Next 16 prerenders
// `/_global-error` from a loader tree with no root layout (a global error replaces it) but with
// app/loading.tsx as the Suspense fallback around the built-in error page; React emits that fallback
// only when the page chunk has not resolved yet, so a loading tree that reads `theme.vars` failed
// `next build` intermittently ("Cannot read properties of undefined (reading 'palette')").
// Inside the root layout the app theme (CSS-variables theme, `theme.vars` set) is already there and
// this renders the children as-is; outside it, it mounts the same provider stack the root layout uses.
// guard: global-error-prerender-no-providers (scripts/global-error-prerender.test.mjs)
export function EnsureAppTheme({ children }: { children: React.ReactNode }) {
  const theme = useTheme();
  if (theme.vars) return children;
  return <AppThemeProvider>{children}</AppThemeProvider>;
}
