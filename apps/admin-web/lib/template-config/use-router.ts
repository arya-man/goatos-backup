'use client';

import { useRouter as useNextRouter } from 'next/navigation';

// ----------------------------------------------------------------------

/**
 * Template useRouter wraps next/navigation with NProgress. Admin-web has its own route progress bar
 * (shell routebar), so the plain Next router is returned.
 */
export function useRouter() {
  return useNextRouter();
}
