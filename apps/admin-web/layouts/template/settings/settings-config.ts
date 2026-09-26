import type { SettingsState } from './types';

import { themeConfig } from '@/theme/theme-config';

// ----------------------------------------------------------------------

export const SETTINGS_STORAGE_KEY: string = 'mesha.shell.settings';

/** Mesha: the persisted nav rail (localStorage + cookie), see context/settings-provider.tsx. */
export const NAV_RAIL_STORAGE_KEY = 'mesha.shell.nav-rail';

export const defaultSettings: SettingsState = {
  mode: themeConfig.defaultMode,
  direction: themeConfig.direction,
  contrast: 'default',
  navLayout: 'vertical',
  primaryColor: 'default',
  navColor: 'integrate',
  // Mesha: pages keep their full-width content (wide worklists); template default is true (lg cap).
  compactLayout: false,
  fontSize: 16,
  fontFamily: themeConfig.fontFamily.primary,
  version: '1',
};
