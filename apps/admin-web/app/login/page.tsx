import Link from "@/components/no-prefetch-link";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { ArrowRight } from "lucide-react";

import { AuthSplitLayout } from "@/layouts/auth-split";
import { FormHead } from "@/components/auth/form-head";
import { GoogleLogin } from "@/components/auth/google-login";
import { LoginSessionGuard } from "@/components/auth/login-session-guard";
import { getAdminRuntimeStatus, searchGoats } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function LoginPage({ searchParams }: { searchParams?: Promise<RouteSearchParams> }) {
  const sp = (await searchParams) ?? {};
  const nextPath = safeNextPath(firstSearchParam(sp.next));
  const runtimeStatus = getAdminRuntimeStatus();
  // Local-only preview: `/login?preview=signin` forces the SSO + email/password
  // form instead of the one-click local-dashboard shortcut, so the real sign-in
  // UI can be inspected on this machine. Guarded to GOATOS_ENV=local; no effect
  // on staging/production, where the form already shows.
  const previewSignIn =
    process.env.GOATOS_ENV === "local" && firstSearchParam(sp.preview) === "signin";
  const shouldCheckLocalDashboard =
    process.env.GOATOS_ENV === "local" &&
    process.env.GOATOS_AUTH_MODE === "bearer" &&
    runtimeStatus.hasLocalBearerFallback &&
    !previewSignIn;
  const localDashboardCheck = shouldCheckLocalDashboard ? await searchGoats({ limit: 1 }) : null;
  const showLocalDashboardShortcut = localDashboardCheck?.ok === true;
  const showLocalAuthRepair =
    shouldCheckLocalDashboard && localDashboardCheck !== null && localDashboardCheck.ok === false;
  const showGoogleLogin = !shouldCheckLocalDashboard;
  // Google's redirect sign-in lands back HERE with the credential waiting in a cookie, so this
  // render is the tail of a sign-in, not a fresh one. Say so instead of showing the sign-in form
  // again while Firebase and the session exchange finish.
  const completingGoogleRedirect =
    showGoogleLogin &&
    firstSearchParam(sp.google_redirect) === "1" &&
    firstSearchParam(sp.google_error) === undefined;
  const loginInstruction = completingGoogleRedirect
    ? "Google confirmed your account. Finishing sign-in — this takes a few seconds."
    : showGoogleLogin
      ? "Use Google SSO or email/password to continue."
      : "Use the local dashboard shortcut on this machine.";

  return (
    <AuthSplitLayout
      slotProps={{
        section: {
          title: "Sign in to run herd operations.",
          subtitle: "Secure entry for goat passports, import review, data quality queues, and operational dashboards.",
        },
      }}
    >
      <FormHead
        title={completingGoogleRedirect ? "Signing you in" : "Sign in"}
        description={loginInstruction}
        sx={{ textAlign: { xs: "center", md: "left" } }}
      />

      {showGoogleLogin ? (
        <Stack sx={{ gap: 3 }}>
          <LoginSessionGuard nextPath={nextPath} />
          <GoogleLogin nextPath={nextPath} completingGoogleRedirect={completingGoogleRedirect} />
        </Stack>
      ) : null}

      {showLocalDashboardShortcut ? (
        <Button
          component={Link}
          href={nextPath}
          prefetch={false}
          fullWidth
          size="large"
          color="inherit"
          variant="contained"
          endIcon={<ArrowRight aria-hidden="true" />}
        >
          Open local dashboard
        </Button>
      ) : null}

      {showLocalAuthRepair ? (
        <Alert severity="warning" sx={{ mt: 2.5 }}>
          <AlertTitle>Local dashboard is not ready</AlertTitle>
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            The admin server has a local fallback token, but the backend is rejecting it. Restart
            the local stack before opening the dashboard.
          </Typography>
          <Box
            component="code"
            sx={{
              mt: 1.5,
              px: 1.25,
              py: 0.75,
              display: "inline-block",
              borderRadius: "var(--r-sm)",
              fontFamily: "monospace",
              fontSize: "var(--fs-caption)",
              bgcolor: "action.hover",
            }}
          >
            make dev-local
          </Box>
        </Alert>
      ) : null}
    </AuthSplitLayout>
  );
}

function firstSearchParam(value: RouteSearchParams[string]): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

function safeNextPath(value: string | undefined): string {
  if (!value?.startsWith("/") || value.startsWith("//")) {
    return "/";
  }
  return value;
}
