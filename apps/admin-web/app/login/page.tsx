import Link from "@/components/no-prefetch-link";
import { AlertTriangle } from "lucide-react";
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
    <main className="login-shell">
      <aside className="login-brand">
        <div className="brand">
          <span className="logo">मे</span>
          <div style={{ display: "flex", flexDirection: "column", lineHeight: 1 }}>
            <b>Mesha</b>
            <small>Admin</small>
          </div>
        </div>

        <div className="login-brand-mid">
          <span className="login-badge">Internal access</span>
          <h1 className="login-hero">Sign in to run herd operations.</h1>
          <p className="login-sub muted">
            Secure entry for goat passports, import review, data quality queues, and operational dashboards.
          </p>
        </div>

        <p className="login-brand-foot small muted">Access is limited to approved Mesha accounts.</p>
      </aside>

      <section className="login-panel">
        <div className="login-card card">
          <div className="bd" style={{ padding: "26px 28px 30px" }}>
            <div className="crumb">
              Mesha <b>Admin</b>
            </div>
            <h1 style={{ margin: "4px 0 0", fontSize: 26, letterSpacing: "-.4px" }}>
              {completingGoogleRedirect ? "Signing you in" : "Sign in"}
            </h1>
            <p className="muted" style={{ margin: "10px 0 0", fontSize: 13.5, lineHeight: 1.6 }}>
              {loginInstruction}
            </p>

            {showGoogleLogin ? <LoginSessionGuard nextPath={nextPath} /> : null}
            {showGoogleLogin ? (
              <GoogleLogin nextPath={nextPath} completingGoogleRedirect={completingGoogleRedirect} />
            ) : null}

            {showLocalDashboardShortcut ? (
              <Link
                href={nextPath}
                prefetch={false}
                className="btn p"
                style={{ marginTop: 18, width: "100%", justifyContent: "center", height: 46 }}
              >
                Open local dashboard
              </Link>
            ) : null}

            {showLocalAuthRepair ? (
              <div className="alert warn" style={{ marginTop: 18 }}>
                <AlertTriangle className="ic" aria-hidden="true" />
                <div>
                  <b style={{ display: "block" }}>Local dashboard is not ready</b>
                  <span className="muted small" style={{ display: "block", marginTop: 4, lineHeight: 1.6 }}>
                    The admin server has a local fallback token, but the backend is rejecting it. Restart the local
                    stack before opening the dashboard.
                  </span>
                  <code
                    className="mono"
                    style={{
                      display: "inline-block",
                      marginTop: 8,
                      background: "var(--bg)",
                      border: "1px solid var(--line)",
                      borderRadius: "var(--r)",
                      padding: "6px 10px",
                      color: "var(--brand-d)",
                    }}
                  >
                    make dev-local
                  </code>
                </div>
              </div>
            ) : null}
          </div>
        </div>
      </section>
    </main>
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
