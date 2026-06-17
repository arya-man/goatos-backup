"use client";

import { useEffect, useState } from "react";
import { onAuthStateChanged } from "firebase/auth";
import { Loader2 } from "lucide-react";
import { getFirebaseAuth, startGoogleSignIn, syncFirebaseSession } from "@/lib/auth/firebase-client";
import { DASHBOARD_BASE_PATH } from "@/lib/auth/session-cookie";

export function GoogleLogin({ nextPath = DASHBOARD_BASE_PATH }: { nextPath?: string }) {
  const [status, setStatus] = useState<"checking" | "ready" | "signing_in" | "redirecting" | "error">("checking");
  const [message, setMessage] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    let unsubscribe: (() => void) | null = null;
    void getFirebaseAuth()
      .then((auth) => {
        unsubscribe = onAuthStateChanged(auth, (user) => {
          if (!active) return;
          if (!user) {
            setStatus("ready");
            return;
          }
          setStatus("redirecting");
          void syncFirebaseSession(user, true)
            .then(() => {
              window.location.assign(safeNextPath(nextPath));
            })
            .catch((error) => {
              setStatus("error");
              setMessage(error instanceof Error ? error.message : "The admin session could not be refreshed.");
            });
        });
      })
      .catch((error) => {
        setStatus("error");
        setMessage(error instanceof Error ? error.message : "Firebase sign-in is not configured.");
      });
    return () => {
      active = false;
      if (unsubscribe) unsubscribe();
    };
  }, [nextPath]);

  const busy = status === "checking" || status === "signing_in" || status === "redirecting";

  return (
    <div className="mt-8">
      <button
        type="button"
        disabled={busy}
        onClick={() => {
          setStatus("signing_in");
          setMessage(null);
          void startGoogleSignIn().catch((error) => {
            setStatus("error");
            setMessage(error instanceof Error ? error.message : "Google sign-in failed.");
          });
        }}
        className="flex h-12 w-full items-center justify-center gap-3 rounded-md border border-[#dadce0] bg-white px-4 text-sm font-bold text-[#3c4043] shadow-sm transition hover:bg-[#f8fafc] focus:outline-none focus:ring-2 focus:ring-[#14f1d9]/50 disabled:cursor-wait disabled:border-[#3a4352] disabled:bg-[#141a23] disabled:text-[#8b95a5]"
      >
        {busy ? (
          <Loader2 className="h-5 w-5 animate-spin" aria-hidden="true" />
        ) : (
          <GoogleMark />
        )}
        {status === "checking" ? "Checking session" : status === "redirecting" ? "Opening dashboard" : "Continue with Google"}
      </button>
      {message ? (
        <p className="mt-3 rounded-lg border border-[#7f1d1d] bg-[#1d1214] px-3 py-2 text-sm leading-6 text-[#fecaca]">
          {message}
        </p>
      ) : null}
    </div>
  );
}

function GoogleMark() {
  return (
    <svg className="h-5 w-5 shrink-0" viewBox="0 0 18 18" aria-hidden="true">
      <path
        fill="#4285f4"
        d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.68-3.88 2.68-6.62Z"
      />
      <path
        fill="#34a853"
        d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.35 0-4.34-1.58-5.05-3.72H.93v2.33A9 9 0 0 0 9 18Z"
      />
      <path
        fill="#fbbc05"
        d="M3.95 10.7A5.41 5.41 0 0 1 3.67 9c0-.59.1-1.16.28-1.7V4.97H.93A9 9 0 0 0 0 9c0 1.45.34 2.82.93 4.03l3.02-2.33Z"
      />
      <path
        fill="#ea4335"
        d="M9 3.58c1.32 0 2.5.45 3.44 1.35l2.58-2.58C13.46.9 11.43 0 9 0A9 9 0 0 0 .93 4.97L3.95 7.3C4.66 5.16 6.65 3.58 9 3.58Z"
      />
    </svg>
  );
}

function safeNextPath(value: string): string {
  if (!value.startsWith(DASHBOARD_BASE_PATH) || value.startsWith(`${DASHBOARD_BASE_PATH}//`)) {
    return DASHBOARD_BASE_PATH;
  }
  return value;
}
