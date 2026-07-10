"use client";

import Script from "next/script";
import { type FormEvent, useCallback, useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import {
  getFirebaseClientRuntimeConfig,
  sendPasswordReset,
  signInWithEmailPassword,
  signInWithGoogleIdToken,
} from "@/lib/auth/firebase-client";

const DEFAULT_NEXT_PATH = "/";

type GoogleCredentialResponse = {
  credential?: string;
  select_by?: string;
};

type GoogleAccountsID = {
  initialize(options: {
    client_id: string;
    callback: (response: GoogleCredentialResponse) => void;
    auto_select?: boolean;
    cancel_on_tap_outside?: boolean;
    hd?: string;
    context?: "signin" | "signup" | "use";
  }): void;
  renderButton(
    parent: HTMLElement,
    options: {
      type?: "standard" | "icon";
      theme?: "outline" | "filled_blue" | "filled_black";
      size?: "large" | "medium" | "small";
      text?: "signin_with" | "signup_with" | "continue_with" | "signin";
      shape?: "rectangular" | "pill" | "circle" | "square";
      logo_alignment?: "left" | "center";
      width?: number;
    },
  ): void;
};

declare global {
  interface Window {
    google?: {
      accounts?: {
        id?: GoogleAccountsID;
      };
    };
  }
}

export function GoogleLogin({ nextPath = DEFAULT_NEXT_PATH }: { nextPath?: string }) {
  const [status, setStatus] = useState<"loading" | "ready" | "signing_in" | "sending_reset" | "redirecting" | "error">("loading");
  const [message, setMessage] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [scriptReady, setScriptReady] = useState(false);
  const [googleClientId, setGoogleClientId] = useState<string | null>(null);
  const buttonContainerRef = useRef<HTMLDivElement | null>(null);
  const renderedButton = useRef(false);
  const mounted = useRef(false);

  const navigateToNext = useCallback(() => {
    setStatus("redirecting");
    window.location.replace(safeNextPath(nextPath));
  }, [nextPath]);

  const handleCredential = useCallback(
    (response: GoogleCredentialResponse) => {
      const googleIdToken = response.credential?.trim();
      if (!googleIdToken) {
        setStatus("ready");
        setMessage("Google sign-in did not return a valid credential. Try again.");
        return;
      }
      setStatus("signing_in");
      setMessage(null);
      setNotice(null);
      void signInWithGoogleIdToken(googleIdToken)
        .then(() => {
          if (!mounted.current) return;
          navigateToNext();
        })
        .catch((error: unknown) => {
          if (!mounted.current) return;
          setStatus("ready");
          setMessage(messageForSignInError(error));
        });
    },
    [navigateToNext],
  );

  const handleEmailPasswordSubmit = useCallback(
    (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      const trimmedEmail = email.trim();
      if (!trimmedEmail || !password) {
        setStatus("ready");
        setNotice(null);
        setMessage("Enter email and password.");
        return;
      }
      setStatus("signing_in");
      setMessage(null);
      setNotice(null);
      void signInWithEmailPassword(trimmedEmail, password)
        .then(() => {
          if (!mounted.current) return;
          navigateToNext();
        })
        .catch((error: unknown) => {
          if (!mounted.current) return;
          setStatus("ready");
          setMessage(messageForSignInError(error));
        });
    },
    [email, navigateToNext, password],
  );

  const handleForgotPassword = useCallback(() => {
    const trimmedEmail = email.trim();
    if (!trimmedEmail) {
      setStatus("ready");
      setNotice(null);
      setMessage("Enter your email first.");
      return;
    }
    setStatus("sending_reset");
    setMessage(null);
    setNotice(null);
    void sendPasswordReset(trimmedEmail)
      .then(() => {
        if (!mounted.current) return;
        setStatus("ready");
        setNotice("Password reset email sent.");
      })
      .catch((error: unknown) => {
        if (!mounted.current) return;
        setStatus("ready");
        setMessage(messageForSignInError(error));
      });
  }, [email]);

  useEffect(() => {
    mounted.current = true;
    void getFirebaseClientRuntimeConfig()
      .then((runtime) => {
        if (!mounted.current) return;
        setGoogleClientId(runtime.googleClientId);
      })
      .catch((error: unknown) => {
        if (!mounted.current) return;
        setStatus("error");
        setMessage(error instanceof Error ? error.message : "Google sign-in is not configured.");
      });
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    if (!scriptReady || !googleClientId || !buttonContainerRef.current || renderedButton.current) {
      return;
    }
    const googleID = window.google?.accounts?.id;
    if (!googleID) {
      window.setTimeout(() => {
        setStatus("error");
        setMessage("Google sign-in could not load in this browser.");
      }, 0);
      return;
    }

    renderedButton.current = true;
    googleID.initialize({
      client_id: googleClientId,
      callback: handleCredential,
      auto_select: false,
      cancel_on_tap_outside: true,
      hd: "mesha.sg",
      context: "signin",
    });
    googleID.renderButton(buttonContainerRef.current, {
      type: "standard",
      theme: "outline",
      size: "large",
      text: "continue_with",
      shape: "rectangular",
      logo_alignment: "left",
      width: 340,
    });
    window.setTimeout(() => setStatus("ready"), 0);
  }, [googleClientId, handleCredential, scriptReady]);

  const isBusy = status === "loading" || status === "signing_in" || status === "sending_reset" || status === "redirecting";
  const statusText =
    status === "loading"
      ? "Loading sign-in"
      : status === "redirecting"
        ? "Opening dashboard"
        : status === "sending_reset"
          ? "Sending reset email"
          : "Signing in";

  return (
    <div className="mt-8">
      <Script
        src="https://accounts.google.com/gsi/client"
        strategy="afterInteractive"
        onLoad={() => setScriptReady(true)}
        onError={() => {
          setStatus("error");
          setMessage("Google sign-in script failed to load. Check the network and reload.");
        }}
      />
      <div className="min-h-11 w-full">
        <div ref={buttonContainerRef} aria-hidden={status !== "ready"} />
        {status === "loading" || status === "error" ? (
          <button
            type="button"
            disabled
            className="flex h-11 w-full items-center justify-center rounded border border-[#dadce0] bg-[#f1f3f4] px-4 text-[14px] font-semibold text-[#5f6368]"
          >
            Continue with Google
          </button>
        ) : null}
      </div>
      <div className="my-5 flex items-center gap-3">
        <span className="h-px flex-1" style={{ background: "var(--line)" }} />
        <span className="text-[11px] font-bold uppercase tracking-[0.18em]" style={{ color: "var(--muted)" }}>
          or
        </span>
        <span className="h-px flex-1" style={{ background: "var(--line)" }} />
      </div>
      <form className="grid gap-3" onSubmit={handleEmailPasswordSubmit}>
        <label className="grid gap-1.5 text-[12px] font-bold uppercase tracking-[0.14em]" style={{ color: "var(--muted)" }}>
          Email
          <input
            autoComplete="email"
            inputMode="email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.currentTarget.value)}
            disabled={isBusy}
            className="h-11 rounded border px-3 text-[14px] font-semibold normal-case tracking-normal outline-none"
            style={{ borderColor: "var(--line)", color: "var(--ink)", background: "var(--card)" }}
          />
        </label>
        <label className="grid gap-1.5 text-[12px] font-bold uppercase tracking-[0.14em]" style={{ color: "var(--muted)" }}>
          Password
          <input
            autoComplete="current-password"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.currentTarget.value)}
            disabled={isBusy}
            className="h-11 rounded border px-3 text-[14px] font-semibold normal-case tracking-normal outline-none"
            style={{ borderColor: "var(--line)", color: "var(--ink)", background: "var(--card)" }}
          />
        </label>
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="submit"
            disabled={isBusy}
            className="btn p"
            style={{ flex: "1 1 180px", justifyContent: "center", height: 46 }}
          >
            Continue
          </button>
          <button
            type="button"
            disabled={isBusy}
            onClick={handleForgotPassword}
            className="btn"
            style={{ flex: "1 1 140px", justifyContent: "center", height: 46 }}
          >
            Forgot password
          </button>
        </div>
      </form>
      <div className="mt-3 min-h-7">
        {message ? (
          <p
            style={{
              borderRadius: 9,
              border: "1px solid color-mix(in srgb, var(--danger) 40%, transparent)",
              background: "var(--dangerx)",
              color: "var(--danger)",
              padding: "8px 12px",
              fontSize: 13,
              lineHeight: 1.6,
            }}
          >
            {message}
          </p>
        ) : notice ? (
          <p
            style={{
              borderRadius: 9,
              border: "1px solid color-mix(in srgb, var(--ok) 36%, transparent)",
              background: "var(--okx)",
              color: "var(--ok)",
              padding: "8px 12px",
              fontSize: 13,
              lineHeight: 1.6,
            }}
          >
            {notice}
          </p>
        ) : isBusy ? (
          <p
            className="inline-flex items-center gap-2 text-xs font-bold uppercase tracking-[0.22em]"
            style={{ color: "var(--muted)" }}
          >
            <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
            {statusText}
          </p>
        ) : null}
      </div>
    </div>
  );
}

function messageForSignInError(error: unknown): string {
  if (isFirebaseAuthError(error)) {
    if (error.code === "auth/popup-closed-by-user" || error.code === "auth/cancelled-popup-request") {
      return "Google sign-in was cancelled.";
    }
    if (error.code === "auth/unauthorized-domain") {
      return "This dashboard host is not authorized for Google sign-in.";
    }
    if (error.code === "auth/invalid-credential" || error.code === "auth/account-exists-with-different-credential") {
      return "Sign-in did not return a valid Mesha session. Try again.";
    }
    if (error.code === "auth/user-not-found" || error.code === "auth/wrong-password") {
      return "Email or password is incorrect.";
    }
    if (error.code === "auth/operation-not-allowed") {
      return "Email/password sign-in is not enabled for this deployment.";
    }
    if (error.code === "auth/invalid-email") {
      return "Enter a valid email address.";
    }
    if (error.code === "auth/too-many-requests") {
      return "Too many attempts. Try again later.";
    }
  }
  return error instanceof Error ? error.message : "Sign-in failed.";
}

function isFirebaseAuthError(error: unknown): error is { code: string } {
  return Boolean(
    error &&
      typeof error === "object" &&
      "code" in error &&
      typeof (error as { code?: unknown }).code === "string",
  );
}

export function safeNextPath(value: string): string {
  const candidate = value.trim();
  if (!candidate) {
    return DEFAULT_NEXT_PATH;
  }

  let decodedCandidate = candidate;
  try {
    decodedCandidate = decodeURI(candidate);
  } catch {
    decodedCandidate = candidate;
  }

  if (!candidate.startsWith("/") || candidate.startsWith("//") || decodedCandidate.includes("\\")) {
    return DEFAULT_NEXT_PATH;
  }

  try {
    const parsed = new URL(candidate, "https://dev.dashboard.mesha.sg");
    if (parsed.origin !== "https://dev.dashboard.mesha.sg") {
      return DEFAULT_NEXT_PATH;
    }
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return DEFAULT_NEXT_PATH;
  }
}
