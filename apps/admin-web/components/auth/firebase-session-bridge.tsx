"use client";

import { useEffect } from "react";

// The Firebase SDK is loaded on demand after hydration: this bridge is mounted by the admin
// shell on every page, and static imports put firebase/app + firebase/auth into every page's
// first-load JS even though nothing here is needed for the first paint.
const loadFirebase = () =>
  Promise.all([import("firebase/auth"), import("@/lib/auth/firebase-client")]);

const REFRESH_INTERVAL_MS = 50 * 60 * 1000;

export function FirebaseSessionBridge() {
  useEffect(() => {
    let mounted = true;
    let interval: ReturnType<typeof setInterval> | null = null;
    let unsubscribe: (() => void) | null = null;

    void loadFirebase()
      .then(async ([{ onIdTokenChanged }, { clearFirebaseSession, getFirebaseAuth, isFirebaseSessionError, syncBridgeSession }]) => {
        const auth = await getFirebaseAuth();
        if (!mounted) return;
        unsubscribe = onIdTokenChanged(auth, (user) => {
          if (!user) return;
          void syncBridgeSession(user).catch((error: unknown) => {
            if (isFirebaseSessionError(error, "email_not_allowed")) {
              void clearFirebaseSession().catch(() => undefined);
            }
            // Navigation will recover through /login if the cookie goes stale.
          });
        });
        interval = setInterval(() => {
          if (!auth.currentUser) return;
          // The forced refresh also fires onIdTokenChanged for the same new token; both calls go
          // through the deduper, so the interval posts once.
          void syncBridgeSession(auth.currentUser, true).catch((error: unknown) => {
            if (isFirebaseSessionError(error, "email_not_allowed")) {
              void clearFirebaseSession().catch(() => undefined);
            }
            // A later SSR request will redirect to login if refresh cannot recover.
          });
        }, REFRESH_INTERVAL_MS);
      })
      .catch(() => {
        // Local legacy mode may not provide Firebase config; server-side fallback handles that path.
      });

    return () => {
      mounted = false;
      if (unsubscribe) unsubscribe();
      if (interval) clearInterval(interval);
    };
  }, []);

  return null;
}
