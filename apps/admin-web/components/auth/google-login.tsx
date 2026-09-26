"use client";

import Script from "next/script";
import { type FormEvent, useCallback, useEffect, useRef, useState } from "react";
import { ArrowRight } from "lucide-react";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import CircularProgress from "@mui/material/CircularProgress";
import IconButton from "@mui/material/IconButton";
import InputAdornment from "@mui/material/InputAdornment";
import Link from "@mui/material/Link";
import Skeleton from "@mui/material/Skeleton";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { FormDivider } from "@/components/auth/form-divider";
import {
  getFirebaseClientRuntimeConfig,
  sendPasswordReset,
  signInWithEmailPassword,
  signInWithGoogleIdToken,
} from "@/lib/auth/firebase-client";
import { GOOGLE_REDIRECT_ROUTE } from "@/lib/auth/session-cookie";

const DEFAULT_NEXT_PATH = "/";

type GoogleCredentialResponse = {
  credential?: string;
  select_by?: string;
};

type GoogleAccountsID = {
  initialize(options: {
    client_id: string;
    callback: (response: GoogleCredentialResponse) => void;
    login_uri?: string;
    ux_mode?: "popup" | "redirect";
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
      state?: string;
    },
  ): void;
};

type GoogleRedirectCredentialResponse = {
  credential?: string;
  error?: string;
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

export function GoogleLogin({
  nextPath = DEFAULT_NEXT_PATH,
  completingGoogleRedirect = false,
}: {
  nextPath?: string;
  /**
   * True on the hop BACK from Google, when the page already knows a credential is waiting. Google's
   * redirect ux_mode lands the user on a freshly rendered /login, so without this the screen looks
   * exactly like "you are back at the sign-in page" for the seconds Firebase and the session
   * exchange take. It only picks the STARTING state -- a failed exchange restores the form.
   */
  completingGoogleRedirect?: boolean;
}) {
  const [completingRedirect, setCompletingRedirect] = useState(completingGoogleRedirect);
  const [status, setStatus] = useState<"loading" | "ready" | "signing_in" | "sending_reset" | "redirecting" | "error">(
    completingGoogleRedirect ? "signing_in" : "loading",
  );
  const [authMethod, setAuthMethod] = useState<"google" | "password" | null>(
    completingGoogleRedirect ? "google" : null,
  );
  const [message, setMessage] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [scriptReady, setScriptReady] = useState(false);
  const [googleClientId, setGoogleClientId] = useState<string | null>(null);
  const [googleAvailable, setGoogleAvailable] = useState(true);
  const buttonContainerRef = useRef<HTMLDivElement | null>(null);
  const renderedButton = useRef(false);
  const mounted = useRef(false);
  const consumedRedirectCredential = useRef(false);

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

  useEffect(() => {
    if (consumedRedirectCredential.current) return;
    const searchParams = new URLSearchParams(window.location.search);
    if (searchParams.get("google_error")) {
      consumedRedirectCredential.current = true;
      window.setTimeout(() => {
        if (!mounted.current) return;
        setCompletingRedirect(false);
        setStatus("ready");
        setMessage("Google sign-in did not return a valid credential. Try again.");
      }, 0);
      return;
    }
    if (searchParams.get("google_redirect") !== "1") return;

    consumedRedirectCredential.current = true;
    window.setTimeout(() => {
      if (!mounted.current) return;
      setAuthMethod("google");
      setStatus("signing_in");
      setMessage(null);
      setNotice(null);
      void fetch(GOOGLE_REDIRECT_ROUTE, { cache: "no-store" })
        .then(async (response) => {
          const payload = (await response.json().catch(() => ({}))) as GoogleRedirectCredentialResponse;
          if (!response.ok || !payload.credential) {
            throw new Error("Google sign-in did not return a valid credential. Try again.");
          }
          return signInWithGoogleIdToken(payload.credential);
        })
        .then(() => {
          if (!mounted.current) return;
          navigateToNext();
        })
        .catch((error: unknown) => {
          if (!mounted.current) return;
          setCompletingRedirect(false);
          setAuthMethod(null);
          setStatus("ready");
          setMessage(messageForSignInError(error));
        });
    }, 0);
  }, [navigateToNext]);

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
        setCompletingRedirect(false);
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
    const loginUri = new URL(GOOGLE_REDIRECT_ROUTE, window.location.origin).toString();
    googleID.initialize({
      client_id: googleClientId,
      callback: handleCredential,
      login_uri: loginUri,
      ux_mode: "redirect",
      auto_select: false,
      cancel_on_tap_outside: true,
      hd: "mesha.sg",
      context: "signin",
      // Redirect mode keeps Google sign-in in top-level browser navigation,
      // avoiding the accounts.google.com/gsi/transform popup handshake.
      itp_support: true,
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
      state: safeNextPath(nextPath),
    });
    window.setTimeout(() => {
      if (!mounted.current || completingRedirect) return;
      setStatus("ready");
    }, 0);
  }, [completingRedirect, googleClientId, handleCredential, nextPath, scriptReady]);

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
  // While the Google credential is being exchanged there is nothing for the operator to do, so the
  // sign-in controls stay out of the way rather than reading as "sign in again".
  const showSignInControls = !completingRedirect;
  const showSsoSlot = (googleAvailable && showSignInControls) || showAuthProgress;

  return (
    <Box>
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
      {/* The progress card lives OUTSIDE the Google block: a credential exchange in flight must stay
          visible even when the Google button itself is unavailable, or the screen goes blank.
          The Google GSI button renders in an iframe we cannot restyle; while it loads a skeleton of
          the same height holds its place so nothing below jumps when it arrives. */}
      <Box aria-live="polite" sx={showSsoSlot ? { width: 1, display: "flex", alignItems: "center" } : undefined}>
        {googleAvailable && showSignInControls ? (
          <>
            <div
              ref={buttonContainerRef}
              aria-hidden={status !== "ready"}
              style={{ display: status === "ready" ? "block" : "none" }}
            />
            {status === "loading" ? <Skeleton variant="rounded" width="100%" height={44} /> : null}
          </>
        ) : null}
        {showAuthProgress ? (
          <Alert
            severity="success"
            variant="outlined"
            role="status"
            icon={<CircularProgress size={20} color="inherit" />}
            sx={{ width: 1 }}
          >
            <AlertTitle>{authProgressTitle}</AlertTitle>
            {authProgressText}
          </Alert>
        ) : null}
      </Box>
      {googleAvailable && showSignInControls ? (
        <FormDivider label="or" sx={{ opacity: showAuthProgress ? 0.45 : 1 }} />
      ) : null}
      {showSignInControls ? (
        <Stack component="form" spacing={3} onSubmit={handleEmailPasswordSubmit}>
          <TextField
            id="login-email"
            label="Email"
            autoComplete="email"
            type="email"
            placeholder="you@mesha.sg"
            value={email}
            onChange={(event) => setEmail(event.currentTarget.value)}
            disabled={isBusy}
            slotProps={{ inputLabel: { shrink: true }, htmlInput: { inputMode: "email" } }}
          />
          <Stack spacing={1.5}>
            <Link
              component="button"
              type="button"
              variant="body2"
              color="inherit"
              onClick={handleForgotPassword}
              disabled={isBusy}
              sx={(theme) => ({
                alignSelf: "flex-end",
                "&:disabled": { opacity: 0.48, cursor: "default", textDecoration: "none" },
                [theme.breakpoints.down("md")]: { minHeight: theme.spacing(5.5) },
              })}
            >
              Forgot password?
            </Link>
            <TextField
              id="login-password"
              label="Password"
              autoComplete="current-password"
              type={showPassword ? "text" : "password"}
              value={password}
              onChange={(event) => setPassword(event.currentTarget.value)}
              disabled={isBusy}
              slotProps={{
                inputLabel: { shrink: true },
                input: {
                  endAdornment: (
                    <InputAdornment position="end">
                      <IconButton
                        onClick={() => setShowPassword((shown) => !shown)}
                        edge="end"
                        aria-label={showPassword ? "Hide password" : "Show password"}
                      >
                        <Iconify icon={showPassword ? "solar:eye-bold" : "solar:eye-closed-bold"} />
                      </IconButton>
                    </InputAdornment>
                  ),
                },
              }}
            />
          </Stack>
          <Button
            fullWidth
            size="large"
            variant="contained"
            color="primary"
            type="submit"
            disabled={isBusy}
            loading={status === "signing_in"}
            loadingPosition="end"
            endIcon={status === "signing_in" ? undefined : <ArrowRight aria-hidden="true" />}
          >
            {status === "signing_in" ? "Signing in…" : "Sign in"}
          </Button>
        </Stack>
      ) : null}
      <Box sx={(theme) => ({ mt: 2, minHeight: theme.spacing(3.5) })}>
        {message ? (
          <Alert severity="error">{message}</Alert>
        ) : notice ? (
          <Alert severity="success" role="status">
            {notice}
          </Alert>
        ) : isBusy ? (
          <Stack direction="row" spacing={1} sx={{ alignItems: "center", color: "text.secondary" }}>
            <CircularProgress size={14} color="inherit" />
            <Typography variant="body2">{statusText}</Typography>
          </Stack>
        ) : null}
      </Box>
    </Box>
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
