import { PasswordResetAction } from "@/components/auth/password-reset-action";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function FirebaseAuthActionPage({ searchParams }: { searchParams?: Promise<RouteSearchParams> }) {
  const sp = (await searchParams) ?? {};
  const mode = firstSearchParam(sp.mode) ?? "";
  const oobCode = firstSearchParam(sp.oobCode) ?? "";
  const continueHref = safeContinueHref(firstSearchParam(sp.continueUrl));

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
          <span className="login-badge">Account recovery</span>
          <h1 className="login-hero">Reset access for herd operations.</h1>
          <p className="login-sub muted">Protected recovery for approved Mesha dashboard accounts.</p>
        </div>

        <p className="login-brand-foot small muted">Access is limited to approved Mesha accounts.</p>
      </aside>

      <section className="login-panel">
        <div className="login-card card">
          <div className="bd" style={{ padding: "26px 28px 30px" }}>
            <div className="crumb">
              Mesha <b>Admin</b>
            </div>
            <PasswordResetAction mode={mode} oobCode={oobCode} continueHref={continueHref} />
          </div>
        </div>
      </section>
    </main>
  );
}

function firstSearchParam(value: RouteSearchParams[string]): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

function safeContinueHref(value: string | undefined): string {
  if (!value) return "/login";
  try {
    const url = new URL(value);
    if (isAllowedDashboardHost(url.hostname)) {
      return `${url.pathname}${url.search}${url.hash}` || "/login";
    }
  } catch {
    if (value.startsWith("/") && !value.startsWith("//") && !value.includes("\\")) {
      return value;
    }
  }
  return "/login";
}

function isAllowedDashboardHost(hostname: string): boolean {
  return (
    hostname === "dashboard.mesha.sg" ||
    hostname === "localhost" ||
    hostname === "127.0.0.1"
  );
}
