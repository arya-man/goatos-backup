// Template config point (src/components/settings/settings-config.ts): Mesha's storage key and
// defaults (full-width pages; no settings drawer is mounted).
import type { SettingsState } from '@/layouts/template/settings/types';

import { themeConfig } from '@/theme/theme-config';

// ----------------------------------------------------------------------

/** The template's base font size setting (px, a settings value, not a style literal). */
const TEMPLATE_BASE_FONT_SIZE = 16;

export const SETTINGS_STORAGE_KEY: string = 'mesha.shell.settings';

export const defaultSettings: SettingsState = {
  mode: themeConfig.defaultMode,
  direction: themeConfig.direction,
  contrast: 'default',
  navLayout: 'vertical',
  primaryColor: 'default',
  navColor: 'integrate',
  // Mesha: pages keep their full-width content (wide worklists); template default is true (lg cap).
  compactLayout: false,
  fontSize: TEMPLATE_BASE_FONT_SIZE,
  fontFamily: themeConfig.fontFamily.primary,
  version: '1',
};
