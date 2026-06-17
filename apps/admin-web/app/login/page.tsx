import Image from "next/image";
import Link from "next/link";
import { GoogleLogin } from "@/components/auth/google-login";
import logoImg from "@/lib/logo.png";
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
    ? "Use your Mesha Workspace account to continue."
    : "Use the local dashboard shortcut on this machine.";

  return (
    <main className="min-h-screen overflow-hidden bg-[#0b0e13] text-[#f8fafc]">
      <div className="grid min-h-screen lg:grid-cols-[0.95fr_1.05fr]">
        <section className="relative hidden overflow-hidden border-r border-[#243044] bg-[#10141b] px-10 py-12 lg:flex lg:flex-col lg:justify-between">
          <div className="absolute inset-0 opacity-[0.08] [background-image:linear-gradient(#14f1d9_1px,transparent_1px),linear-gradient(90deg,#14f1d9_1px,transparent_1px)] [background-size:42px_42px]" />
          <div className="relative">
            <div className="flex items-center gap-3">
              <Image src={logoImg} alt="Mesha" width={52} height={52} className="rounded-full" priority />
              <div>
                <div className="text-lg font-bold text-[#14f1d9]">Mesha</div>
                <div className="text-xs font-semibold uppercase tracking-[0.18em] text-[#8899aa]">Admin</div>
              </div>
            </div>

            <div className="mt-24 max-w-xl">
              <div className="inline-flex rounded-full border border-[#14f1d9]/30 bg-[#14f1d9]/10 px-3 py-1 text-xs font-semibold uppercase tracking-[0.16em] text-[#14f1d9]">
                Internal access
              </div>
              <h1 className="mt-6 text-5xl font-black leading-[1.03] text-white">
                Sign in to run herd operations.
              </h1>
              <p className="mt-5 max-w-lg text-base leading-7 text-[#aab7c4]">
                Secure entry for goat passports, import review, data quality queues, and operational dashboards.
              </p>
            </div>
          </div>

          <div className="relative text-sm leading-6 text-[#7f8fa3]">
            Access is limited to Mesha Workspace accounts.
          </div>
        </section>

        <section className="flex min-h-screen items-center justify-center px-5 py-8 sm:px-8">
          <div className="w-full max-w-[420px]">
            <div className="mb-8 flex items-center gap-3 lg:hidden">
              <Image src={logoImg} alt="Mesha" width={48} height={48} className="rounded-full" priority />
              <div>
                <div className="text-lg font-bold text-[#14f1d9]">Mesha</div>
                <div className="text-xs font-semibold uppercase tracking-[0.18em] text-[#8899aa]">Admin</div>
              </div>
            </div>

            <div className="rounded-2xl border border-[#2e3b52] bg-[#171b23] px-6 py-8 shadow-2xl shadow-black/30 sm:px-8">
              <div className="text-xs font-black uppercase tracking-[0.16em] text-[#14f1d9]">
                Mesha Admin
              </div>
              <h2 className="mt-2 text-3xl font-black text-white">Sign in</h2>
              <p className="mt-3 text-sm leading-6 text-[#aab7c4]">
                {loginInstruction}
              </p>

              {showGoogleLogin ? <GoogleLogin /> : null}

              {showLocalDashboardShortcut ? (
                <Link
                  href="/"
                  prefetch={false}
                  className="mt-5 flex h-12 w-full items-center justify-center rounded-xl border border-[#14f1d9]/40 bg-[#14f1d9]/10 px-4 text-sm font-black text-[#14f1d9] transition hover:border-[#14f1d9] hover:bg-[#14f1d9]/15 focus:outline-none focus:ring-2 focus:ring-[#14f1d9]/50"
                >
                  Open local dashboard
                </Link>
              ) : null}

              {showLocalAuthRepair ? (
                <div className="mt-5 rounded-xl border border-[#f59e0b]/35 bg-[#f59e0b]/10 p-4">
                  <div className="text-sm font-black text-[#fbbf24]">Local dashboard is not ready</div>
                  <p className="mt-2 text-sm leading-6 text-[#d6b986]">
                    The admin server has a local fallback token, but the backend is rejecting it. Restart the
                    local stack before opening the dashboard.
                  </p>
                  <div className="mt-3 rounded-lg border border-[#334155] bg-[#0b0e13] px-3 py-2 font-mono text-sm text-[#14f1d9]">
                    make dev-local
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}
