'use client';

import GlobalStyles from '@mui/material/GlobalStyles';

// ----------------------------------------------------------------------

// MUI CssBaseline is emitted by emotion AFTER every Next.js stylesheet, so on equal specificity it
// beats the legacy globals in app/mesha-theme.css. Three of its rules changed legacy kit layout:
//
// 1. `*, *::before, *::after { box-sizing: inherit }` replaced the legacy `* { box-sizing: border-box }`.
//    Inherit breaks through UA shadow trees: a <summary> inherits from the unstyled slot inside
//    <details>, which is content-box, so every <details><summary> trigger (the date-range field, the
//    filter pickers) grew by its padding and ran into the next grid cell.
// 2. `body { font-size: 1rem; line-height: 1.5 }` replaced the legacy 14px/1.45 body text, so every
//    legacy element that inherits body type grew.
// 3. `strong, b { font-weight: 700 }` replaced the UA `bolder`.
//
// This sheet renders after CssBaseline and restates the legacy values. MUI components set their own
// box-sizing-safe dimensions and typography, so the template shell is unaffected.
export function LegacyBaseline() {
  return (
    <GlobalStyles
      styles={{
        '*, *::before, *::after': { boxSizing: 'border-box' },
        'strong, b': { fontWeight: 'bolder' },
        body: {
          font: '14px/1.45 var(--f)',
          color: 'var(--ink)',
          backgroundColor: 'var(--bg)',
        },
      }}
    />
  );
}
