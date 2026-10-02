'use client';

import GlobalStyles from '@mui/material/GlobalStyles';

import { layoutClasses } from '@/layouts/core/classes';

import { CHART_CATEGORICAL } from './chart-palette';
import { MESHA_TOKENS_DARK, MESHA_TOKENS_LIGHT } from './mesha-tokens';
import { phoneStickyEdges } from '@/components/app/table/sticky-first-column';

// ----------------------------------------------------------------------

/**
 * App-wide baseline, rendered once inside the ThemeProvider (FIXJ6). It replaces the legacy
 * stylesheets app/mesha-theme.css, app/minimal-theme.css, app/frame.css and layouts/mesha-layout.css,
 * which are deleted (guard `legacy-css-ceiling`: they must stay absent). Only element / attribute
 * level rules live here, never a page class:
 *
 * 1. The Mesha palette + shell tokens (theme/mesha-tokens.ts) as CSS custom properties, dark on
 *    `:root`, light on `html.light` (the theme toggle's class).
 * 2. MUI CssBaseline is emitted after every Next.js stylesheet and changed three things the app is
 *    measured against: `box-sizing: inherit` (breaks through UA shadow trees), `body` 1rem/1.5 type
 *    and `strong, b { 700 }`. They are restated here (border-box, 14px/1.45 body, `bolder`).
 * 3. Bare anchors inherit colour (Next <Link> without MUI Link), placeholders are text.disabled at
 *    full opacity, the selection colour is the brand soft tone, html/body never scroll sideways.
 * 4. WebView contract (apps/admin-web/AGENTS.md "Webview"): inputs are 16px on phones (iOS focus
 *    zoom), every control in the page content keeps a 44px tap floor on coarse pointers / phones,
 *    and a table in the page content keeps a 540px floor below 861px so its columns scroll in their
 *    card instead of crushing (the template tables that set their own min width win).
 * 5. The Dense switch of server-paged tables (DenseToggleAuto, ProcurementTableFooter) marks its
 *    table host `data-dense`; its body cells take MUI's small-table padding.
 * 6. Phones: the first (identity) and last (action) cells of every page-content table stay pinned
 *    while it scrolls sideways (components/app/table/sticky-first-column; was minimal-theme.css
 *    `.tablewrap` :first-child/:last-child, J3B N-P1-1; guard sticky-edges-phone).
 * 7. Route progress affordances the shell toggles on <html> / links (`route-busy`,
 *    `data-route-pending`) and the theme hand-off (`theme-switching`), plus reduced motion.
 */
const CONTENT = `.${layoutClasses.content}`;

/** The categorical chart slots as scheme-following variables (`--chart-series-blue`), which a
 * chart paints from before it has hydrated and knows the colour scheme (components/app/chart-colors). */
const chartSeriesVars = (mode: 'light' | 'dark') =>
  Object.fromEntries(CHART_CATEGORICAL.map((slot) => [slot.cssVar, slot[mode]]));

export function AppBaseline() {
  return (
    <GlobalStyles
      styles={(theme) => ({
        ':root': { ...MESHA_TOKENS_DARK, ...chartSeriesVars('dark'), colorScheme: 'dark' },
        // The attribute too: MUI writes `data-theme` itself (colorSchemeSelector), so the Mesha tokens
        // follow the same switch as MUI's own variables even if the class lags.
        ':root.light': { ...MESHA_TOKENS_LIGHT, ...chartSeriesVars('light'), colorScheme: 'light' },
        ':root[data-theme="light"]': { ...MESHA_TOKENS_LIGHT, ...chartSeriesVars('light'), colorScheme: 'light' },
        '*, *::before, *::after': { boxSizing: 'border-box' },
        'strong, b': { fontWeight: 'bolder' },
        'html, body': { maxWidth: '100%', overflowX: 'hidden' },
        body: {
          margin: 0,
          font: '14px/1.45 var(--f)',
          color: 'var(--ink)',
          backgroundColor: 'var(--bg)',
          WebkitFontSmoothing: 'antialiased',
        },
        a: { color: 'inherit', textDecoration: 'none' },
        '::selection': { background: 'var(--primary-soft)' },
        ':where(input, textarea)::placeholder': { color: 'var(--fg-faint)', opacity: 1 },
        'input[type="radio"], input[type="checkbox"]': { accentColor: 'var(--primary)' },
        ':where(a, [tabindex="0"]):focus-visible': {
          outline: '2px solid color-mix(in srgb, var(--primary) 80%, transparent)',
          outlineOffset: 2,
        },
        '@media (max-width: 640px)': {
          'input:not([type="checkbox"]):not([type="radio"]):not([type="hidden"]), select, textarea': { fontSize: 16 },
          '[role="dialog"] input:not([type="checkbox"]):not([type="radio"]):not([type="hidden"])': { minHeight: 'var(--tap-min)' },
        },
        '@media (pointer: coarse), (max-width: 640px)': {
          [`:root ${CONTENT} :is(button, summary, textarea, input:not([type="checkbox"]):not([type="radio"]):not([type="hidden"]))`]: {
            minHeight: 'var(--tap-min)',
          },
        },
        // Webview rule: below the desktop nav (lg) the header icon buttons and the phone / tablet
        // nav drawer items stay >= 44px taps (template layout classes; was layouts/mesha-layout.css).
        '@media (max-width: 1199.95px)': {
          [`.${layoutClasses.header} .MuiIconButton-root`]: { minWidth: 'var(--tap-min)', minHeight: 'var(--tap-min)' },
          [`.${layoutClasses.nav.root} .MuiButtonBase-root`]: { minHeight: 'var(--tap-min)' },
        },
        '@media (max-width: 860px)': {
          [`${CONTENT} table`]: { minWidth: 540 },
        },
        // Light top bar: the header icons are duotone (a solid layer plus a ~40% layer) on
        // action.active grey, so on white the dominant layer measured ~1.7:1 (PR #294 L2). Ink one
        // step darker and the soft layer lifted keep both layers >= 3:1; the white assistant goat
        // (a fixed-colour mascot) gets a hairline outline so it does not vanish on white.
        [`:root[data-theme="light"] .${layoutClasses.header} .MuiIconButton-root`]: { color: theme.vars.palette.grey[700] },
        [`:root[data-theme="light"] .${layoutClasses.header} .MuiIconButton-root svg [opacity]`]: { opacity: 0.64 },
        [`:root[data-theme="light"] #topbar-ai-slot :is(img, svg)`]: { filter: `drop-shadow(0 0 0.5px ${theme.vars.palette.grey[700]}) drop-shadow(0 0 0.5px ${theme.vars.palette.grey[700]})` },
        ...phoneStickyEdges(`${CONTENT} table`, theme),
        '[data-dense] .MuiTableCell-body.MuiTableCell-body': { paddingTop: 6, paddingBottom: 6 },
        'html.route-busy, html.route-busy body': { cursor: 'progress' },
        'a[data-route-pending="true"]': { position: 'relative', isolation: 'isolate', cursor: 'progress' },
        'a[data-route-pending="true"]::after': {
          content: '""',
          position: 'absolute',
          inset: 2,
          borderRadius: 'inherit',
          pointerEvents: 'none',
          background: 'linear-gradient(100deg, transparent, color-mix(in srgb, var(--brand) 24%, transparent), transparent)',
          opacity: 0.85,
          animation: 'route-pending-sheen .9s ease-in-out infinite',
          zIndex: -1,
        },
        '@keyframes route-pending-sheen': {
          '0%, 100%': { opacity: 0.38, transform: 'scaleX(.94)' },
          '50%': { opacity: 0.9, transform: 'scaleX(1)' },
        },
        'html.theme-switching *, html.theme-switching *::before, html.theme-switching *::after': {
          transition:
            'background-color 300ms var(--ease), border-color 300ms var(--ease), color 200ms var(--ease), box-shadow 300ms var(--ease) !important',
        },
        '@media (prefers-reduced-motion: reduce)': {
          '*, *::before, *::after': {
            animationDuration: '1ms !important',
            animationDelay: '0ms !important',
            animationIterationCount: '1 !important',
            transitionDuration: '1ms !important',
            transitionDelay: '0ms !important',
            scrollBehavior: 'auto !important',
          },
        },
      })}
    />
  );
}
