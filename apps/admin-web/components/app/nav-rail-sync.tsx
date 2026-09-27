'use client';

import { useEffect } from 'react';

import { useSettingsContext } from '@/layouts/template/settings';

/** Mirrors the template settings' navLayout onto html[data-nav-rail], which the pre-bootstrap shell
 * skeleton (layouts/shell-skeleton.css) reads; the server sets it first from the settings cookie. */
export function NavRailSync() {
  const { state } = useSettingsContext();
  useEffect(() => {
    document.documentElement.setAttribute('data-nav-rail', state.navLayout === 'mini' ? 'mini' : 'vertical');
  }, [state.navLayout]);
  return null;
}
