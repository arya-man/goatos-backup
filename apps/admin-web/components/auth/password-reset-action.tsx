"use client";

import Link from "@/components/no-prefetch-link";
import { type FormEvent, useEffect, useMemo, useState } from "react";
import { ArrowRight } from "lucide-react";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import CircularProgress from "@mui/material/CircularProgress";
import IconButton from "@mui/material/IconButton";
import InputAdornment from "@mui/material/InputAdornment";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { FormHead } from "@/components/auth/form-head";
import { NewPasswordIcon } from "@/components/auth/new-password-icon";
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
  const [showPassword, setShowPassword] = useState(false);

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

  const visibilityToggle = (
    <InputAdornment position="end">
      <IconButton
        onClick={() => setShowPassword((shown) => !shown)}
        edge="end"
        aria-label={showPassword ? "Hide password" : "Show password"}
      >
        <Iconify icon={showPassword ? "solar:eye-bold" : "solar:eye-closed-bold"} />
      </IconButton>
    </InputAdornment>
  );

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
      <>
        <FormHead title="Reset link unavailable" />
        <Alert severity="error" sx={{ mb: 3 }}>
          {linkError || message}
        </Alert>
        <Button
          component={Link}
          href="/login"
          fullWidth
          size="large"
          variant="contained"
          color="primary"
          endIcon={<ArrowRight aria-hidden="true" />}
        >
          Back to sign in
        </Button>
      </>
    );
  }

  if (status === "checking") {
    return (
      <Stack spacing={2} sx={{ alignItems: "center", color: "text.secondary" }}>
        <CircularProgress color="inherit" />
        <Typography variant="body2">Checking reset link</Typography>
      </Stack>
    );
  }

  if (status === "success") {
    return (
      <>
        <FormHead title="Password updated" description="You can sign in with your new password." />
        <Button
          component={Link}
          href={continueHref}
          fullWidth
          size="large"
          variant="contained"
          color="primary"
          endIcon={<ArrowRight aria-hidden="true" />}
        >
          Continue to sign in
        </Button>
      </>
    );
  }

  return (
    <>
      <FormHead icon={<NewPasswordIcon />} title="Choose a new password" description={email} />

      <Stack component="form" spacing={3} onSubmit={handleSubmit}>
        <TextField
          id="reset-password"
          label="New password"
          autoComplete="new-password"
          type={showPassword ? "text" : "password"}
          value={password}
          onChange={(event) => setPassword(event.currentTarget.value)}
          disabled={status === "submitting"}
          slotProps={{ inputLabel: { shrink: true }, input: { endAdornment: visibilityToggle } }}
        />
        <TextField
          id="reset-confirm"
          label="Confirm password"
          autoComplete="new-password"
          type={showPassword ? "text" : "password"}
          value={confirmation}
          onChange={(event) => setConfirmation(event.currentTarget.value)}
          disabled={status === "submitting"}
          slotProps={{ inputLabel: { shrink: true }, input: { endAdornment: visibilityToggle } }}
        />

        {passwordError || message ? <Alert severity="error">{message || passwordError}</Alert> : null}

        <Button
          fullWidth
          size="large"
          variant="contained"
          color="primary"
          type="submit"
          disabled={!canSubmit}
          loading={status === "submitting"}
          loadingPosition="end"
          endIcon={status === "submitting" ? undefined : <ArrowRight aria-hidden="true" />}
        >
          {status === "submitting" ? "Updating password…" : "Update password"}
        </Button>
      </Stack>
    </>
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
