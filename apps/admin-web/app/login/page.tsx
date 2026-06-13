import Image from "next/image";
import logoImg from "@/lib/logo.png";

export const dynamic = "force-dynamic";

export default function LoginPage() {
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
                Use your Mesha Workspace account to continue.
              </p>

              <button
                type="button"
                disabled
                className="mt-8 flex h-12 w-full cursor-not-allowed items-center justify-center gap-3 rounded-xl border border-[#334155] bg-[#10141b] px-4 text-sm font-black text-[#7f8fa3]"
                aria-describedby="sso-setup-note"
              >
                <span className="flex h-5 w-5 items-center justify-center rounded-full bg-white text-[13px] font-black text-[#4285f4]">
                  G
                </span>
                Continue with Google
              </button>
              <p id="sso-setup-note" className="mt-3 text-center text-xs leading-5 text-[#7f8fa3]">
                Google SSO setup is pending for local, staging, and production.
              </p>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}
