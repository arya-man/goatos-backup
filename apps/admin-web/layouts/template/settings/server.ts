import type { SettingsState } from './types';

import { cookies } from 'next/headers';

import { defaultSettings, NAV_RAIL_STORAGE_KEY } from './settings-config';

// ----------------------------------------------------------------------

// Mesha adaptation of the template detectSettings: the only persisted setting is the nav rail,
// stored as `mesha.shell.nav-rail=1|0` (see context/settings-provider.tsx). Returns undefined when
// the cookie is absent so the provider falls back to defaultSettings + the localStorage migration.
export async function detectSettings(): Promise<SettingsState | undefined> {
  const cookieStore = await cookies();
  const rail = cookieStore.get(NAV_RAIL_STORAGE_KEY)?.value;
  if (rail !== '1' && rail !== '0') return undefined;
  return { ...defaultSettings, navLayout: rail === '1' ? 'mini' : 'vertical' };
}
