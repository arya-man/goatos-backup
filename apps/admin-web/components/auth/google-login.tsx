"use client";

import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Loader2 } from "lucide-react";
import {
  completeGoogleRedirectSignIn,
  startGoogleRedirectSignIn,
} from "@/lib/auth/firebase-client";
import { DASHBOARD_BASE_PATH } from "@/lib/auth/session-cookie";

export function GoogleLogin({ nextPath = DASHBOARD_BASE_PATH }: { nextPath?: string }) {
  const [status, setStatus] = useState<"ready" | "checking" | "signing_in" | "redirecting">("checking");
  const [message, setMessage] = useState<string | null>(null);
  const checkedRedirect = useRef(false);

  const navigateToNext = useCallback(() => {
    setStatus("redirecting");
    window.location.replace(safeNextPath(nextPath));
  }, [nextPath]);

  useEffect(() => {
    if (checkedRedirect.current) return;
    checkedRedirect.current = true;

    let mounted = true;
    void completeGoogleRedirectSignIn()
      .then((user) => {
        if (!mounted) return;
        if (user) {
          navigateToNext();
          return;
        }
        setStatus("ready");
      })
      .catch((error: unknown) => {
        if (!mounted) return;
        setStatus("ready");
        setMessage(messageForGoogleSignInError(error));
      });

    return () => {
      mounted = false;
    };
  }, [navigateToNext]);

  const handleSignIn = useCallback(() => {
    if (status !== "ready") return;
    setStatus("signing_in");
    setMessage(null);

    void startGoogleRedirectSignIn()
      .catch((error: unknown) => {
        setStatus("ready");
        setMessage(messageForGoogleSignInError(error));
      });
  }, [status]);

  const handleKeyDown = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key !== "Enter" && event.key !== " ") return;
      event.preventDefault();
      handleSignIn();
    },
    [handleSignIn],
  );

  const isBusy = status !== "ready";
  const statusText =
    status === "checking" ? "Checking session" : status === "redirecting" ? "Opening dashboard" : "Signing in";

  return (
    <div className="mt-8">
      <button
        type="button"
        onClick={handleSignIn}
        onKeyDown={handleKeyDown}
        disabled={isBusy}
        className="relative flex h-12 w-full max-w-[340px] items-center justify-center rounded border border-[#dadce0] bg-white px-4 text-[14px] font-semibold text-[#3c4043] shadow-sm transition hover:bg-[#f8fafd] focus:outline-none focus:ring-2 focus:ring-[#1a73e8] focus:ring-offset-2 focus:ring-offset-[#171c26] disabled:cursor-wait disabled:bg-[#f1f3f4] disabled:text-[#5f6368]"
      >
        <span className="absolute left-4 flex h-5 w-5 items-center justify-center" aria-hidden="true">
          <GoogleGMark />
        </span>
        Continue with Google
      </button>
      <div className="mt-3 min-h-7">
        {message ? (
          <p className="rounded-lg border border-[#7f1d1d] bg-[#1d1214] px-3 py-2 text-sm leading-6 text-[#fecaca]">
            {message}
          </p>
        ) : isBusy ? (
          <p className="inline-flex items-center gap-2 text-xs font-bold uppercase tracking-[0.22em] text-[#8899AA]">
            <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
            {statusText}
          </p>
        ) : null}
      </div>
    </div>
  );
}

function messageForGoogleSignInError(error: unknown): string {
  if (isFirebaseAuthError(error)) {
    if (
      error.code === "auth/redirect-cancelled-by-user" ||
      error.code === "auth/popup-closed-by-user" ||
      error.code === "auth/cancelled-popup-request"
    ) {
      return "Google sign-in was cancelled.";
    }
    if (error.code === "auth/redirect-operation-pending") {
      return "Google sign-in is already open. Complete it in the Google window, or reload and try again.";
    }
    if (error.code === "auth/popup-blocked") {
      return "Chrome blocked Google sign-in. Reload and try again.";
    }
    if (error.code === "auth/unauthorized-domain") {
      return "This dashboard host is not authorized for Firebase sign-in.";
    }
  }
  return error instanceof Error ? error.message : "Google sign-in failed.";
}

function isFirebaseAuthError(error: unknown): error is { code: string } {
  return Boolean(
    error &&
      typeof error === "object" &&
      "code" in error &&
      typeof (error as { code?: unknown }).code === "string",
  );
}

function GoogleGMark() {
  return (
    <svg aria-hidden="true" viewBox="0 0 18 18" className="h-5 w-5">
      <path
        fill="#4285F4"
        d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.68-3.88 2.68-6.62Z"
      />
      <path
        fill="#34A853"
        d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.33-1.58-5.04-3.72H.94v2.33A9 9 0 0 0 9 18Z"
      />
      <path
        fill="#FBBC05"
        d="M3.96 10.7A5.41 5.41 0 0 1 3.68 9c0-.59.1-1.16.28-1.7V4.97H.94A9 9 0 0 0 0 9c0 1.45.34 2.82.94 4.03l3.02-2.33Z"
      />
      <path
        fill="#EA4335"
        d="M9 3.58c1.32 0 2.5.45 3.44 1.34l2.58-2.58A8.65 8.65 0 0 0 9 0 9 9 0 0 0 .94 4.97L3.96 7.3C4.67 5.16 6.66 3.58 9 3.58Z"
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
