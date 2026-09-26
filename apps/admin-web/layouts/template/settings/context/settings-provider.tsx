'use client';

import type { SettingsState, SettingsProviderProps } from '../types';

import { isEqual } from 'es-toolkit';
import { useMemo, useState, useEffect, useCallback } from 'react';

import { SettingsContext } from './settings-context';
import { NAV_RAIL_STORAGE_KEY } from '../settings-config';

// ----------------------------------------------------------------------

// Mesha adaptation of the template SettingsProvider. Admin-web has no settings drawer, so the only
// persisted field is the nav layout (vertical <-> mini), kept under the shell's existing rail key so
// a reader's collapsed rail survives the move to the template layout. Mode is NOT stored here: it is
// owned by the shell theme toggle (html[data-theme], key mesha.shell.theme) and MUI reads it through
// its data-theme colorSchemeSelector.

// The rail state is also a cookie (template `cookieSettings` path, layouts/template/settings/server.ts)
// so the server renders the mini rail directly instead of 300px-then-88px after hydration, and it
// mirrors onto html[data-nav-rail] for the pre-bootstrap shell skeleton (layouts/shell-skeleton.css).
function persistNavRail(mini: boolean) {
  try {
    window.localStorage.setItem(NAV_RAIL_STORAGE_KEY, mini ? '1' : '0');
  } catch {
    /* storage blocked */
  }
  document.cookie = `${NAV_RAIL_STORAGE_KEY}=${mini ? '1' : '0'}; path=/; max-age=31536000; samesite=lax`;
  document.documentElement.setAttribute('data-nav-rail', mini ? 'mini' : 'vertical');
}

export function SettingsProvider({ children, cookieSettings, defaultSettings }: SettingsProviderProps) {
  const [state, setStateValue] = useState<SettingsState>(cookieSettings ?? defaultSettings);
  const [openDrawer, setOpenDrawer] = useState(false);

  // No cookie yet (a reader from before the cookie existed): migrate the localStorage rail once.
  useEffect(() => {
    if (cookieSettings) return;
    let mini = false;
    try {
      mini = window.localStorage.getItem(NAV_RAIL_STORAGE_KEY) === '1';
    } catch {
      /* storage blocked */
    }
    persistNavRail(mini);
    if (!mini) return;
    const id = window.setTimeout(() => setStateValue((prev) => ({ ...prev, navLayout: 'mini' })), 0);
    return () => window.clearTimeout(id);
  }, [cookieSettings]);

  const setState = useCallback((updateValue: Partial<SettingsState>) => {
    setStateValue((prev) => ({ ...prev, ...updateValue }));
    if (updateValue.navLayout) persistNavRail(updateValue.navLayout === 'mini');
  }, []);

  const setField = useCallback(
    (name: keyof SettingsState, updateValue: SettingsState[keyof SettingsState]) =>
      setState({ [name]: updateValue } as Partial<SettingsState>),
    [setState]
  );

  const onReset = useCallback(() => setState(defaultSettings), [defaultSettings, setState]);
  const onToggleDrawer = useCallback(() => setOpenDrawer((prev) => !prev), []);
  const onCloseDrawer = useCallback(() => setOpenDrawer(false), []);
  const canReset = !isEqual(state, defaultSettings);

  const memoizedValue = useMemo(
    () => ({ canReset, onReset, openDrawer, onCloseDrawer, onToggleDrawer, state, setState, setField }),
    [canReset, onReset, openDrawer, onCloseDrawer, onToggleDrawer, state, setField, setState]
  );

  return <SettingsContext value={memoizedValue}>{children}</SettingsContext>;
}
