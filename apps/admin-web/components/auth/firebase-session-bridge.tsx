"use client";

import { useEffect } from "react";
import { onIdTokenChanged } from "firebase/auth";
import {
  clearFirebaseSession,
  getFirebaseAuth,
  isFirebaseSessionError,
  syncBridgeSession,
} from "@/lib/auth/firebase-client";

const REFRESH_INTERVAL_MS = 50 * 60 * 1000;

export function FirebaseSessionBridge() {
  useEffect(() => {
    let mounted = true;
    let interval: ReturnType<typeof setInterval> | null = null;
    let unsubscribe: (() => void) | null = null;

    void getFirebaseAuth()
      .then((auth) => {
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
