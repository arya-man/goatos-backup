"use client";

import Link from "@/components/no-prefetch-link";
import { type FormEvent, useEffect, useMemo, useState } from "react";
import { AlertTriangle, CheckCircle2, KeyRound, Loader2 } from "lucide-react";
import { confirmPasswordResetCode, verifyPasswordReset } from "@/lib/auth/firebase-client";

type ResetStatus = "checking" | "ready" | "submitting" | "success" | "error";

type PasswordResetActionProps = {
  mode: string;
  oobCode: string;
  continueHref: string;
};

export function PasswordResetAction({ mode, oobCode, continueHref }: PasswordResetActionProps) {
  const [status, setStatus] = useState<ResetStatus>("checking");
  const [email, setEmail] = useState("");
  const [message, setMessage] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");

  const linkError =
    mode !== "resetPassword"
      ? "This sign-in link is not a password reset link."
      : !oobCode
        ? "This password reset link is missing its verification code."
        : "";
  const passwordError = useMemo(() => {
    if (!password && !confirmation) return "";
    if (password.length < 8) return "Use at least 8 characters.";
    if (password !== confirmation) return "Passwords do not match.";
    return "";
  }, [confirmation, password]);
  const canSubmit = status === "ready" && password.length >= 8 && password === confirmation;

  useEffect(() => {
    if (linkError) return;
    let cancelled = false;
    void verifyPasswordReset(oobCode)
      .then((verifiedEmail) => {
        if (cancelled) return;
        setEmail(verifiedEmail);
        setStatus("ready");
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setStatus("error");
        setMessage(messageForResetError(error));
      });
    return () => {
      cancelled = true;
    };
  }, [linkError, oobCode]);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSubmit) return;
    setStatus("submitting");
    setMessage("");
    void confirmPasswordResetCode(oobCode, password)
      .then(() => {
        setStatus("success");
        setPassword("");
        setConfirmation("");
      })
      .catch((error: unknown) => {
        setStatus("ready");
        setMessage(messageForResetError(error));
      });
  }

  // A malformed link (wrong mode / missing oobCode) short-circuits the effect, so `status`
  // never leaves "checking". Render the error state FIRST so a broken link always shows the
  // "Reset link unavailable" fallback instead of spinning forever on the checking spinner.
  if (linkError || status === "error") {
    return (
      <div className="grid gap-4 text-center">
        <AlertTriangle className="mx-auto h-8 w-8" style={{ color: "var(--danger)" }} aria-hidden="true" />
        <div>
          <h2 style={{ margin: 0, fontSize: 21 }}>Reset link unavailable</h2>
          <p className="muted" style={{ margin: "8px 0 0", fontSize: 13.5, lineHeight: 1.6 }}>
            {linkError || message}
          </p>
        </div>
        <Link href="/login" className="btn p" style={{ width: "100%", justifyContent: "center", height: 44 }}>
          Back to sign in
        </Link>
      </div>
    );
  }

  if (status === "checking") {
    return (
      <div className="grid gap-4 text-center">
        <Loader2 className="mx-auto h-6 w-6 animate-spin" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <p className="muted" style={{ margin: 0, fontSize: 13 }}>
          Checking reset link
        </p>
      </div>
    );
  }

  if (status === "success") {
    return (
      <div className="grid gap-4 text-center">
        <CheckCircle2 className="mx-auto h-8 w-8" style={{ color: "var(--ok)" }} aria-hidden="true" />
        <div>
          <h2 style={{ margin: 0, fontSize: 21 }}>Password updated</h2>
          <p className="muted" style={{ margin: "8px 0 0", fontSize: 13.5, lineHeight: 1.6 }}>
            You can sign in with your new password.
          </p>
        </div>
        <Link href={continueHref} className="btn p" style={{ width: "100%", justifyContent: "center", height: 44 }}>
          Continue to sign in
        </Link>
      </div>
    );
  }

  return (
    <form className="grid gap-4" onSubmit={handleSubmit}>
      <div className="grid gap-2 text-center">
        <KeyRound className="mx-auto h-8 w-8" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h2 style={{ margin: 0, fontSize: 21 }}>Choose a new password</h2>
        <p className="muted" style={{ margin: 0, fontSize: 13.5, lineHeight: 1.6 }}>
          {email}
        </p>
      </div>

      <label className="grid gap-1.5 text-[12px] font-bold uppercase tracking-[0.14em]" style={{ color: "var(--muted)" }}>
        New password
        <input
          autoComplete="new-password"
          type="password"
          value={password}
          onChange={(event) => setPassword(event.currentTarget.value)}
          disabled={status === "submitting"}
          className="h-11 rounded-[10px] border px-3 text-[14px] font-semibold normal-case tracking-normal outline-none"
          style={{ borderColor: "var(--line)", color: "var(--ink)", background: "var(--card)" }}
        />
      </label>
      <label className="grid gap-1.5 text-[12px] font-bold uppercase tracking-[0.14em]" style={{ color: "var(--muted)" }}>
        Confirm password
        <input
          autoComplete="new-password"
          type="password"
          value={confirmation}
          onChange={(event) => setConfirmation(event.currentTarget.value)}
          disabled={status === "submitting"}
          className="h-11 rounded-[10px] border px-3 text-[14px] font-semibold normal-case tracking-normal outline-none"
          style={{ borderColor: "var(--line)", color: "var(--ink)", background: "var(--card)" }}
        />
      </label>

      {passwordError || message ? (
        <p
          style={{
            borderRadius: 9,
            border: "1px solid color-mix(in srgb, var(--danger) 40%, transparent)",
            background: "var(--dangerx)",
            color: "var(--danger)",
            padding: "8px 12px",
            fontSize: 13,
            lineHeight: 1.6,
            margin: 0,
          }}
        >
          {message || passwordError}
        </p>
      ) : null}

      <button
        type="submit"
        disabled={!canSubmit}
        className="btn p"
        style={{ width: "100%", justifyContent: "center", height: 46 }}
      >
        {status === "submitting" ? "Updating password..." : "Update password"}
      </button>
    </form>
  );
}

function messageForResetError(error: unknown): string {
  if (isFirebaseAuthError(error)) {
    if (error.code === "auth/expired-action-code" || error.code === "auth/invalid-action-code") {
      return "This reset link is expired or has already been used.";
    }
    if (error.code === "auth/weak-password") {
      return "Use a stronger password.";
    }
    if (error.code === "auth/user-disabled" || error.code === "auth/user-not-found") {
      return "This account is no longer available for password reset.";
    }
    if (error.code === "auth/network-request-failed") {
      return "Network connection failed. Try again.";
    }
  }
  return error instanceof Error ? error.message : "Password reset failed.";
}

function isFirebaseAuthError(error: unknown): error is { code: string } {
  return Boolean(
    error &&
      typeof error === "object" &&
      "code" in error &&
      typeof (error as { code?: unknown }).code === "string",
  );
}
