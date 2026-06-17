"use client";

import { useEffect, useRef, useState } from "react";
import { onAuthStateChanged } from "firebase/auth";
import { Loader2 } from "lucide-react";
import {
  getFirebaseAuth,
  getFirebaseClientRuntimeConfig,
  signInWithGoogleIdToken,
  syncFirebaseSession,
} from "@/lib/auth/firebase-client";
import { DASHBOARD_BASE_PATH } from "@/lib/auth/session-cookie";

type GoogleCredentialResponse = {
  credential?: string;
};

type GoogleIdentityServices = {
  accounts?: {
    id?: {
      disableAutoSelect?: () => void;
      initialize: (options: {
        auto_select?: boolean;
        button_auto_select?: boolean;
        callback: (response: GoogleCredentialResponse) => void;
        client_id: string;
        hd?: string;
        itp_support?: boolean;
        use_fedcm_for_button?: boolean;
      }) => void;
      renderButton: (
        parent: HTMLElement,
        options: {
          shape?: "rectangular";
          size?: "large";
          text?: "continue_with";
          theme?: "outline";
          type?: "standard";
          width?: number;
        },
      ) => void;
    };
  };
};

declare global {
  interface Window {
    google?: GoogleIdentityServices;
  }
}

let googleIdentityScriptPromise: Promise<void> | null = null;

export function GoogleLogin({ nextPath = DASHBOARD_BASE_PATH }: { nextPath?: string }) {
  const [status, setStatus] = useState<"checking" | "ready" | "signing_in" | "redirecting" | "error">("checking");
  const [message, setMessage] = useState<string | null>(null);
  const googleButtonRef = useRef<HTMLDivElement | null>(null);

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

  useEffect(() => {
    if (status !== "ready" || !googleButtonRef.current) return;
    let cancelled = false;
    googleButtonRef.current.replaceChildren();

    void getFirebaseClientRuntimeConfig()
      .then(async ({ googleClientId }) => {
        if (cancelled || !googleButtonRef.current) return;
        if (!googleClientId) {
          throw new Error("Google sign-in client is not configured for this admin deployment.");
        }
        await loadGoogleIdentityScript();
        const google = window.google?.accounts?.id;
        if (!google) {
          throw new Error("Google sign-in could not load.");
        }
        google.disableAutoSelect?.();
        google.initialize({
          client_id: googleClientId,
          hd: "mesha.sg",
          auto_select: false,
          button_auto_select: false,
          itp_support: true,
          use_fedcm_for_button: false,
          callback: (response) => {
            if (!response.credential) {
              setStatus("error");
              setMessage("Google sign-in did not return a credential.");
              return;
            }
            setStatus("signing_in");
            setMessage(null);
            void signInWithGoogleIdToken(response.credential)
              .then(() => {
                setStatus("redirecting");
                window.location.assign(safeNextPath(nextPath));
              })
              .catch((error) => {
                setStatus("error");
                setMessage(error instanceof Error ? error.message : "Google sign-in failed.");
              });
          },
        });
        const buttonWidth = Math.max(220, Math.min(340, Math.floor(googleButtonRef.current.getBoundingClientRect().width)));
        google.renderButton(googleButtonRef.current, {
          type: "standard",
          theme: "outline",
          size: "large",
          text: "continue_with",
          shape: "rectangular",
          width: buttonWidth,
        });
      })
      .catch((error) => {
        if (cancelled) return;
        setStatus("error");
        setMessage(error instanceof Error ? error.message : "Google sign-in is not configured.");
      });

    return () => {
      cancelled = true;
    };
  }, [nextPath, status]);

  return (
    <div className="mt-8">
      {status === "ready" ? (
        <div className="flex min-h-12 w-full justify-center" ref={googleButtonRef} />
      ) : (
        <div className="flex h-12 w-full items-center justify-center gap-3 rounded-md border border-[#3a4352] bg-[#141a23] px-4 text-sm font-bold text-[#8b95a5]">
          <Loader2 className="h-5 w-5 animate-spin" aria-hidden="true" />
          {status === "redirecting" ? "Opening dashboard" : status === "signing_in" ? "Signing in" : "Checking session"}
        </div>
      )}
      {message ? (
        <p className="mt-3 rounded-lg border border-[#7f1d1d] bg-[#1d1214] px-3 py-2 text-sm leading-6 text-[#fecaca]">
          {message}
        </p>
      ) : null}
    </div>
  );
}

function loadGoogleIdentityScript(): Promise<void> {
  if (window.google?.accounts?.id) return Promise.resolve();
  if (googleIdentityScriptPromise) return googleIdentityScriptPromise;

  googleIdentityScriptPromise = new Promise((resolve, reject) => {
    const done = () => {
      window.clearTimeout(timeout);
      if (window.google?.accounts?.id) {
        resolve();
      } else {
        reject(new Error("Google sign-in could not load."));
      }
    };
    const timeout = window.setTimeout(done, 10000);
    const script =
      document.querySelector<HTMLScriptElement>('script[src="https://accounts.google.com/gsi/client"]') ??
      document.createElement("script");
    script.addEventListener("load", done, { once: true });
    script.addEventListener(
      "error",
      () => {
        window.clearTimeout(timeout);
        reject(new Error("Google sign-in could not load."));
      },
      { once: true },
    );
    if (!script.parentNode) {
      script.src = "https://accounts.google.com/gsi/client";
      script.async = true;
      script.defer = true;
      document.head.append(script);
    }
  });
  return googleIdentityScriptPromise;
}

function safeNextPath(value: string): string {
  if (!value.startsWith(DASHBOARD_BASE_PATH) || value.startsWith(`${DASHBOARD_BASE_PATH}//`)) {
    return DASHBOARD_BASE_PATH;
  }
  return value;
}
