import type { SettingsState } from '@/layouts/template/settings/types';

/** The shell's pre-template rail cookie ('1' = mini), carried into the settings once. */
export const LEGACY_NAV_RAIL_COOKIE = 'mesha.shell.nav-rail';

export type ShellSettings = { settings: SettingsState; clearCookies: string[] };

/**
 * Wraps the verbatim template detectSettings (layouts/template/settings/server.ts), which
 * JSON.parses the settings cookie with no guard: a corrupt cookie falls back to the defaults and is
 * cleared (by the client, see components/app/cookie-cleanup) instead of breaking every page. A reader
 * with only the legacy rail cookie keeps a collapsed rail once, then the legacy cookie is cleared.
 */
export async function resolveShellSettings(
  detect: () => Promise<SettingsState>,
  hasSettingsCookie: boolean,
  legacyRail: string | undefined,
  { defaultSettings, settingsKey }: { defaultSettings: SettingsState; settingsKey: string }
): Promise<ShellSettings> {
  const clearCookies: string[] = [];
  let settings: SettingsState;
  try {
    settings = await detect();
  } catch {
    settings = defaultSettings;
    clearCookies.push(settingsKey);
  }
  if (!hasSettingsCookie && (legacyRail === '1' || legacyRail === '0')) {
    settings = { ...settings, navLayout: legacyRail === '1' ? 'mini' : 'vertical' };
  }
  if (legacyRail !== undefined) clearCookies.push(LEGACY_NAV_RAIL_COOKIE);
  return { settings, clearCookies };
}
