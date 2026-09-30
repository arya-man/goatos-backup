'use client';

import GlobalStyles from '@mui/material/GlobalStyles';

/**
 * Webview rule (apps/admin-web/AGENTS.md): every control is >= 44px at phone width.
 * The template's controls are 30-40px, so this raises hit areas below the `md` breakpoint (phones and the
 * Android WebView up to 900px);
 * desktop keeps the template's exact sizes. It is a sibling GlobalStyles (render once inside the
 * ThemeProvider) rather than theme component overrides, so it never replaces the template's own
 * styleOverrides functions when themes are merged.
 */
const TAP = 44;

export function PhoneTapStyles() {
  return (
    <GlobalStyles
      styles={(theme) => ({
        [theme.breakpoints.down('md')]: {
          // Doubled class = higher specificity than the theme's own single-class overrides.
          [[
            '.MuiButton-root.MuiButton-root',
            '.MuiTab-root.MuiTab-root',
            '.MuiInputBase-root.MuiInputBase-root',
            '.MuiMenuItem-root.MuiMenuItem-root',
            '.MuiSwitch-root.MuiSwitch-root',
            '.MuiTablePagination-select.MuiTablePagination-select',
          ].join(', ')]: { minHeight: TAP },
          '.MuiInputBase-root:not(.MuiInputBase-multiline) .MuiInputBase-input.MuiInputBase-input': {
            minHeight: TAP,
            boxSizing: 'border-box',
          },
          '.MuiTablePagination-select.MuiTablePagination-select': { display: 'inline-flex', alignItems: 'center' },
          [[
            '.MuiIconButton-root.MuiIconButton-root',
            '.MuiCheckbox-root.MuiCheckbox-root',
            '.MuiRadio-root.MuiRadio-root',
            '.MuiPaginationItem-root.MuiPaginationItem-root',
            '.MuiAutocomplete-popupIndicator.MuiAutocomplete-popupIndicator',
            '.MuiAutocomplete-clearIndicator.MuiAutocomplete-clearIndicator',
            // A short header ("Pen") was a 21px-wide sort target.
            '.MuiTableSortLabel-root.MuiTableSortLabel-root',
            // TR1-#8: a one-word chip ("All", "OK") was 41-43px wide; the /tasks Board/List icon
            // toggle was 30x30; the date-range clear "x" was 24px wide.
            '.MuiChip-root.MuiChip-root',
            // Grouped toggles take their size from the group's --size rule (0,2,0), so outrank it.
            '.MuiToggleButton-root.MuiToggleButton-root',
            '.MuiToggleButtonGroup-root .MuiToggleButton-root.MuiToggleButton-root',
          ].join(', ')]: { minWidth: TAP, minHeight: TAP },
          // The switch thumb button is 32px (18px small); padding grows it to 44px and `left` keeps the thumb
          // exactly where the template draws it.
          '.MuiSwitch-switchBase.MuiSwitch-switchBase': { padding: 'calc(calc(2 * var(--spacing)) - 1px)', left: 0 },
          '.MuiSwitch-sizeSmall .MuiSwitch-switchBase.MuiSwitch-switchBase': { padding: 'calc(calc(2 * var(--spacing)) + 1px)', left: 'calc(calc(1 * var(--spacing)) * -1 + 1px)' },
          // The slider thumb is 14px; its ::after is the touch area (MUI's own hit-slop pattern).
          '.MuiSlider-thumb.MuiSlider-thumb::after': { width: TAP, height: TAP },
          '.MuiCheckbox-root input, .MuiRadio-root input': { width: '100%', height: '100%', top: 0, left: 0 },
          '.MuiBreadcrumbs-li > a, a.minimal__breadcrumbs__back': {
            minHeight: TAP,
            minWidth: TAP,
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
          },
        },
      })}
    />
  );
}
