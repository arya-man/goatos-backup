import Link from "next/link";
import { AlertTriangle } from "lucide-react";
import { GoogleLogin } from "@/components/auth/google-login";
import { getAdminRuntimeStatus, searchGoats } from "@/lib/api/server";

export const dynamic = "force-dynamic";

export default async function LoginPage() {
  const runtimeStatus = getAdminRuntimeStatus();
  const shouldCheckLocalDashboard =
    process.env.GOATOS_ENV === "local" &&
    process.env.GOATOS_AUTH_MODE === "bearer" &&
    runtimeStatus.hasLocalBearerFallback;
  const localDashboardCheck = shouldCheckLocalDashboard ? await searchGoats({ limit: 1 }) : null;
  const showLocalDashboardShortcut = localDashboardCheck?.ok === true;
  const showLocalAuthRepair =
    shouldCheckLocalDashboard && localDashboardCheck !== null && localDashboardCheck.ok === false;
  const showGoogleLogin = !shouldCheckLocalDashboard;
  const loginInstruction = showGoogleLogin
    ? "Use Google SSO or email/password to continue."
    : "Use the local dashboard shortcut on this machine.";

  return (
    <main style={{ minHeight: "100vh", display: "grid", placeItems: "center", background: "var(--bg)", color: "var(--ink)", padding: 24 }}>
      <div style={{ width: "100%", maxWidth: 430 }}>
        <div className="brand" style={{ justifyContent: "center", marginBottom: 18 }}>
          <span className="logo">मे</span>
          <b style={{ fontSize: 22, letterSpacing: "-.3px" }}>Mesha</b>
        </div>

        <section className="card">
          <div className="bd" style={{ padding: "26px 28px 30px" }}>
            <div className="crumb">
              Mesha <b>Admin</b>
            </div>
            <h1 style={{ margin: "4px 0 0", fontSize: 26, letterSpacing: "-.4px" }}>Sign in</h1>
            <p className="muted" style={{ margin: "10px 0 0", fontSize: 13.5, lineHeight: 1.6 }}>
              {loginInstruction}
            </p>

            {showGoogleLogin ? <GoogleLogin /> : null}

            {showLocalDashboardShortcut ? (
              <Link
                href="/"
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
                      borderRadius: 8,
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
        </section>

        <p className="muted small" style={{ textAlign: "center", marginTop: 14 }}>
          Access is limited to approved Mesha accounts.
        </p>
      </div>
    </main>
  );
}
