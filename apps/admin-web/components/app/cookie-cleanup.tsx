'use client';

import { useEffect } from 'react';

/** Expires the named cookies once on mount (a corrupt settings cookie, the legacy rail cookie). */
export function CookieCleanup({ names }: { names: string[] }) {
  useEffect(() => {
    for (const name of names) document.cookie = `${name}=; path=/; max-age=0; samesite=lax`;
  }, [names]);
  return null;
}
