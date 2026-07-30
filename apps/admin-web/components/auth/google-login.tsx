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
    itp_support?: boolean;
    use_fedcm_for_prompt?: boolean;
    use_fedcm_for_button?: boolean;
    button_auto_select?: boolean;
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
  const [authMethod, setAuthMethod] = useState<"google" | "password" | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [scriptReady, setScriptReady] = useState(false);
  const [googleClientId, setGoogleClientId] = useState<string | null>(null);
  const [googleAvailable, setGoogleAvailable] = useState(true);
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
        setAuthMethod(null);
        setStatus("ready");
        setMessage("Google sign-in did not return a valid credential. Try again.");
        return;
      }
      setAuthMethod("google");
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
          setAuthMethod(null);
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
        setAuthMethod(null);
        setStatus("ready");
        setNotice(null);
        setMessage("Enter email and password.");
        return;
      }
      setAuthMethod("password");
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
          setAuthMethod(null);
          setStatus("ready");
          setMessage(messageForSignInError(error));
        });
    },
    [email, navigateToNext, password],
  );

  const handleForgotPassword = useCallback(() => {
    const trimmedEmail = email.trim();
    if (!trimmedEmail) {
      setAuthMethod(null);
      setStatus("ready");
      setNotice(null);
      setMessage("Enter your email first.");
      return;
    }
    setAuthMethod(null);
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
      .catch(() => {
        if (!mounted.current) return;
        // Google SSO unavailable (not configured / unreachable). Degrade quietly
        // to the email + password form instead of raising a red error on load.
        setGoogleAvailable(false);
        setStatus("ready");
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
        if (!mounted.current) return;
        setGoogleAvailable(false);
        setStatus("ready");
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
      // Without itp_support, browsers with third-party cookies blocked (Safari
      // ITP, Chrome third-party-cookie phase-out) make GSI fall back to an
      // intermediate storage-access popup at accounts.google.com/gsi/transform.
      // That popup's own postMessage/close handshake with the opener can be left
      // hanging (blank white window) once we resolve the credential. itp_support
      // tells GSI to manage that handshake properly so the popup closes itself.
      // The rendered button flow has its own FedCM switch; without it, clicking
      // Continue with Google can still use the popup path even if prompt FedCM is
      // enabled.
      itp_support: true,
      use_fedcm_for_prompt: true,
      use_fedcm_for_button: true,
      button_auto_select: false,
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
  const showAuthProgress = status === "signing_in" || status === "redirecting";
  const statusText =
    status === "loading"
      ? "Loading sign-in"
      : status === "redirecting"
        ? "Opening dashboard"
        : status === "sending_reset"
          ? "Sending reset email"
          : "Signing in";
  const authProgressTitle =
    status === "redirecting" ? "Opening dashboard" : authMethod === "google" ? "Google verified" : "Signing in";
  const authProgressText =
    status === "redirecting"
      ? "Taking you to Mesha Admin now."
      : authMethod === "google"
        ? "Signing you in. This can take a few seconds."
        : "Checking your account. This can take a few seconds.";

  return (
    <div className="mt-8">
      <Script
        src="https://accounts.google.com/gsi/client"
        strategy="afterInteractive"
        onLoad={() => setScriptReady(true)}
        onError={() => {
          if (!mounted.current) return;
          setGoogleAvailable(false);
          setStatus("ready");
        }}
      />
      {googleAvailable ? (
        <>
          <div className="min-h-[66px] w-full" aria-live="polite">
            <div
              ref={buttonContainerRef}
              aria-hidden={status !== "ready"}
              style={{ display: status === "ready" ? "block" : "none" }}
            />
            {status === "loading" ? (
              <div
                className="flex h-11 w-full items-center justify-center rounded-[10px] border"
                style={{ borderColor: "var(--line)", background: "var(--card)", color: "var(--muted)" }}
              >
                <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
              </div>
            ) : null}
            {showAuthProgress ? (
              <div
                className="flex min-h-[66px] w-full items-center gap-3 rounded-[10px] border px-4"
                role="status"
                style={{
                  borderColor: "color-mix(in srgb, var(--brand) 44%, var(--line))",
                  background: "color-mix(in srgb, var(--brand-soft) 58%, var(--card))",
                  color: "var(--ink)",
                  boxShadow: "0 0 0 3px color-mix(in srgb, var(--brand-soft) 52%, transparent)",
                }}
              >
                <span
                  className="grid h-9 w-9 place-items-center rounded-[9px]"
                  style={{ background: "var(--card)", color: "var(--brand-d)" }}
                >
                  <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
                </span>
                <span className="min-w-0">
                  <span className="block text-[13px] font-extrabold">{authProgressTitle}</span>
                  <span
                    className="mt-0.5 block text-[12px] font-semibold leading-5"
                    style={{ color: "var(--muted)" }}
                  >
                    {authProgressText}
                  </span>
                </span>
              </div>
            ) : null}
          </div>
          <div className="my-5 flex items-center gap-3" style={{ opacity: showAuthProgress ? 0.45 : 1 }}>
            <span className="h-px flex-1" style={{ background: "var(--line)" }} />
            <span className="text-[11px] font-bold uppercase tracking-[0.18em]" style={{ color: "var(--muted)" }}>
              or
            </span>
            <span className="h-px flex-1" style={{ background: "var(--line)" }} />
          </div>
        </>
      ) : null}
      <form className="grid gap-3.5" onSubmit={handleEmailPasswordSubmit}>
        <label
          htmlFor="login-email"
          className="grid gap-1.5 text-[12px] font-bold uppercase tracking-[0.14em]"
          style={{ color: "var(--muted)" }}
        >
          Email
          <input
            id="login-email"
            autoComplete="email"
            inputMode="email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.currentTarget.value)}
            disabled={isBusy}
            className="h-11 rounded-[10px] border px-3 text-[14px] font-semibold normal-case tracking-normal outline-none"
            style={{ borderColor: "var(--line)", color: "var(--ink)", background: "var(--card)" }}
          />
        </label>
        <div className="grid gap-1.5">
          <div className="flex items-center justify-between gap-3">
            <label
              htmlFor="login-password"
              className="text-[12px] font-bold uppercase tracking-[0.14em]"
              style={{ color: "var(--muted)" }}
            >
              Password
            </label>
            <button type="button" onClick={handleForgotPassword} disabled={isBusy} className="login-forgot">
              Forgot password?
            </button>
          </div>
          <input
            id="login-password"
            autoComplete="current-password"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.currentTarget.value)}
            disabled={isBusy}
            className="h-11 rounded-[10px] border px-3 text-[14px] font-semibold normal-case tracking-normal outline-none"
            style={{ borderColor: "var(--line)", color: "var(--ink)", background: "var(--card)" }}
          />
        </div>
        <button
          type="submit"
          disabled={isBusy}
          className="btn p"
          style={{ marginTop: 2, width: "100%", justifyContent: "center", height: 46 }}
        >
          {status === "signing_in" ? "Signing in…" : "Sign in"}
        </button>
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
