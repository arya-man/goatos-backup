"use client";

import { useEffect, useState } from "react";
import { onAuthStateChanged } from "firebase/auth";
import { Loader2 } from "lucide-react";
import { getFirebaseAuth, signInWithGoogle, syncFirebaseSession } from "@/lib/auth/firebase-client";
import { DASHBOARD_BASE_PATH } from "@/lib/auth/session-cookie";

export function GoogleLogin({ nextPath = DASHBOARD_BASE_PATH }: { nextPath?: string }) {
  const [status, setStatus] = useState<"checking" | "ready" | "signing_in" | "redirecting" | "error">("checking");
  const [message, setMessage] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    let unsubscribe: (() => void) | null = null;
    void getFirebaseAuth()
      .then((auth) =>
        {
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
        },
      )
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
          void signInWithGoogle()
            .then(() => {
              setStatus("redirecting");
              window.location.assign(safeNextPath(nextPath));
            })
            .catch((error) => {
              setStatus("error");
              setMessage(error instanceof Error ? error.message : "Google sign-in failed.");
            });
        }}
        className="flex h-12 w-full items-center justify-center gap-3 rounded-xl border border-[#334155] bg-[#10141b] px-4 text-sm font-black text-[#f8fafc] transition hover:border-[#14f1d9]/70 hover:bg-[#121923] focus:outline-none focus:ring-2 focus:ring-[#14f1d9]/50 disabled:cursor-wait disabled:text-[#7f8fa3]"
      >
        {busy ? (
          <Loader2 className="h-5 w-5 animate-spin" aria-hidden="true" />
        ) : (
          <span className="flex h-5 w-5 items-center justify-center rounded-full bg-white text-[13px] font-black text-[#4285f4]">
            G
          </span>
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

function safeNextPath(value: string): string {
  if (!value.startsWith(DASHBOARD_BASE_PATH) || value.startsWith(`${DASHBOARD_BASE_PATH}//`)) {
    return DASHBOARD_BASE_PATH;
  }
  return value;
}
