import Image from "next/image";
import Link from "next/link";
import { ArrowRight, CheckCircle2, LockKeyhole, ShieldCheck } from "lucide-react";
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

            <div className="mt-20 max-w-xl">
              <div className="inline-flex items-center gap-2 rounded-full border border-[#14f1d9]/30 bg-[#14f1d9]/10 px-3 py-1 text-xs font-semibold uppercase tracking-[0.16em] text-[#14f1d9]">
                <ShieldCheck size={14} />
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

          <div className="relative grid gap-3 text-sm text-[#c7d1dc]">
            {["Role-scoped admin access", "Server-side API credentials", "Local and shared environments"].map((item) => (
              <div key={item} className="flex items-center gap-3 rounded-xl border border-[#263449] bg-[#151b25]/80 px-4 py-3">
                <CheckCircle2 className="h-4 w-4 text-[#14f1d9]" aria-hidden="true" />
                <span>{item}</span>
              </div>
            ))}
          </div>
        </section>

        <section className="flex min-h-screen items-center justify-center px-5 py-8 sm:px-8">
          <div className="w-full max-w-[480px]">
            <div className="mb-8 flex items-center gap-3 lg:hidden">
              <Image src={logoImg} alt="Mesha" width={48} height={48} className="rounded-full" priority />
              <div>
                <div className="text-lg font-bold text-[#14f1d9]">Mesha</div>
                <div className="text-xs font-semibold uppercase tracking-[0.18em] text-[#8899aa]">Admin</div>
              </div>
            </div>

            <div className="rounded-2xl border border-[#2e3b52] bg-[#171b23] p-6 shadow-2xl shadow-black/30 sm:p-8">
              <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-[#14f1d9]/12 text-[#14f1d9]">
                <LockKeyhole className="h-6 w-6" aria-hidden="true" />
              </div>
              <div className="mt-6 text-xs font-black uppercase tracking-[0.16em] text-[#14f1d9]">Mesha admin</div>
              <h2 className="mt-2 text-3xl font-black text-white">Sign in to Mesha Admin</h2>
              <p className="mt-3 text-sm leading-6 text-[#aab7c4]">
                Your session is missing or expired. Use your Mesha admin session to continue.
              </p>

              <div className="mt-7 grid gap-3">
                <Link
                  href="/"
                  className="inline-flex h-12 items-center justify-center gap-2 rounded-xl bg-[#14f1d9] px-4 text-sm font-black text-[#071014] transition hover:bg-[#7ff7ea]"
                >
                  Continue to Mesha Admin
                  <ArrowRight className="h-4 w-4" aria-hidden="true" />
                </Link>
                <button
                  type="button"
                  disabled
                  className="inline-flex h-12 cursor-not-allowed items-center justify-center rounded-xl border border-[#334155] bg-[#10141b] px-4 text-sm font-semibold text-[#64748b]"
                >
                  Continue with Google SSO
                  {" "}
                  <span className="ml-2 rounded-full border border-[#334155] px-2 py-0.5 text-[10px] uppercase tracking-wide">
                    setup pending
                  </span>
                </button>
              </div>

              <div className="mt-6 rounded-xl border border-[#263449] bg-[#10141b] p-4">
                <div className="text-sm font-bold text-white">Local development</div>
                <p className="mt-2 text-sm leading-6 text-[#93a4b8]">
                  If this is your laptop, restart the local stack so it mints a fresh admin token.
                </p>
                <pre className="mt-3 overflow-x-auto rounded-lg border border-[#293241] bg-[#0b0e13] px-3 py-2 font-mono text-xs text-[#14f1d9]">
                  <code>make dev-local</code>
                </pre>
              </div>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}
