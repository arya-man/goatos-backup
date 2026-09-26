import type { PaletteColorNoChannels } from '../core';

import { primary, secondary } from '../core/palette';

// ----------------------------------------------------------------------

export type ThemeColorPreset =
  | 'default'
  | 'preset1'
  | 'preset2'
  | 'preset3'
  | 'preset4'
  | 'preset5';

// Mesha: the template's alternative brand presets (cyan, purple, blue, orange, red) are not carried.
// Admin-web has one locked brand (app/mesha-theme.css) and no settings drawer, so every preset key
// resolves to the Mesha palette. The type and shape stay template-exact for the settings code.
const meshaPrimary: PaletteColorNoChannels = {
  lighter: primary.lighter,
  light: primary.light,
  main: primary.main,
  dark: primary.dark,
  darker: primary.darker,
  contrastText: primary.contrastText,
};

const meshaSecondary: PaletteColorNoChannels = {
  lighter: secondary.lighter,
  light: secondary.light,
  main: secondary.main,
  dark: secondary.dark,
  darker: secondary.darker,
  contrastText: secondary.contrastText,
};

export const primaryColorPresets: Record<ThemeColorPreset, PaletteColorNoChannels> = {
  default: meshaPrimary,
  preset1: meshaPrimary,
  preset2: meshaPrimary,
  preset3: meshaPrimary,
  preset4: meshaPrimary,
  preset5: meshaPrimary,
};

export const secondaryColorPresets: Record<ThemeColorPreset, PaletteColorNoChannels> = {
  default: meshaSecondary,
  preset1: meshaSecondary,
  preset2: meshaSecondary,
  preset3: meshaSecondary,
  preset4: meshaSecondary,
  preset5: meshaSecondary,
};
