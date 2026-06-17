"use client";

import { useEffect } from "react";
import { onIdTokenChanged } from "firebase/auth";
import { getFirebaseAuth, syncFirebaseSession } from "@/lib/auth/firebase-client";

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
          void syncFirebaseSession(user).catch(() => {
            // Navigation will recover through /dashboard/login if the cookie goes stale.
          });
        });
        interval = setInterval(() => {
          void syncFirebaseSession(auth.currentUser, true).catch(() => {
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
