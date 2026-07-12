"use client";

import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { onIdTokenChanged } from "firebase/auth";
import { getFirebaseAuth, isFirebaseSessionError, syncFirebaseSession } from "@/lib/auth/firebase-client";

const DEFAULT_NEXT_PATH = "/";

// When /login loads, the SSR guard already decided there was no valid session
// cookie. But Firebase browserLocalPersistence may still hold a live sign-in
// (refresh token lives for weeks, while the SSR cookie caps at ~1h). This guard
// re-mints the session cookie from that live Firebase session and forwards the
// user home, so an already-signed-in user is not stranded on the login screen.
export function LoginSessionGuard({ nextPath = DEFAULT_NEXT_PATH }: { nextPath?: string }) {
  const [redirecting, setRedirecting] = useState(false);

  useEffect(() => {
    let active = true;
    let unsubscribe: (() => void) | null = null;

    void getFirebaseAuth()
      .then((auth) => {
        if (!active) return;
        unsubscribe = onIdTokenChanged(auth, (user) => {
          if (!active || !user) return;
          void syncFirebaseSession(user, true)
            .then((ok) => {
              if (!active || !ok) return;
              setRedirecting(true);
              window.location.replace(safeNextPath(nextPath));
            })
            .catch((error: unknown) => {
              // email_not_allowed (or any sync failure) leaves the login form
              // in place so the user can sign in with a permitted account.
              if (isFirebaseSessionError(error, "email_not_allowed")) return;
            });
        });
      })
      .catch(() => {
        // No Firebase config on this deployment (e.g. local bearer mode) —
        // the login page's own shortcuts handle that path.
      });

    return () => {
      active = false;
      if (unsubscribe) unsubscribe();
    };
  }, [nextPath]);

  if (!redirecting) return null;

  return (
    <div className="alert" style={{ marginTop: 18, display: "flex", alignItems: "center", gap: 10 }}>
      <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
      <span className="muted small">Session found. Opening dashboard…</span>
    </div>
  );
}

function safeNextPath(value: string): string {
  if (!value.startsWith("/") || value.startsWith("//")) {
    return DEFAULT_NEXT_PATH;
  }
  return value;
}
