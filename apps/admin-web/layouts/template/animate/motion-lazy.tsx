'use client';

import { LazyMotion } from 'motion/react';

// ----------------------------------------------------------------------

export type MotionLazyProps = {
  children: React.ReactNode;
};

const loadFeaturesAsync = async () => import('./features').then((res) => res.default);

// Mesha deviations from the template (documented, J1 P2-7):
// - `motion/react` instead of `framer-motion`: admin-web already ships `motion` (same library, new
//   package name), so the template's framer-motion dependency is not added twice.
// - no `strict`: the pre-MUI kit (components/kit/overlay, sparkline, radial-stat, theme-toggle and
//   the notification panel) renders full `motion.*` components inside this provider, which strict
//   mode turns into a runtime throw. Restore `strict` once Phase 3 retires those kit components.
export function MotionLazy({ children }: MotionLazyProps) {
  return (
    <LazyMotion features={loadFeaturesAsync}>
      {children}
    </LazyMotion>
  );
}
