import Image from "next/image";
import logoImg from "@/lib/logo.png";

export const dynamic = "force-dynamic";

export default function LoginPage() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-[#0b0e13] px-4 py-10 text-[#f8fafc]">
      <section className="w-full max-w-[420px] rounded-2xl border border-[#2e3b52] bg-[#171b23] px-6 py-8 shadow-2xl shadow-black/30 sm:px-8">
        <div className="flex flex-col items-center text-center">
          <Image src={logoImg} alt="Mesha" width={64} height={64} className="rounded-full" priority />
          <div className="mt-5 text-xs font-black uppercase tracking-[0.16em] text-[#14f1d9]">
            Mesha Admin
          </div>
          <h1 className="mt-2 text-3xl font-black text-white">Sign in</h1>
          <p className="mt-3 max-w-[300px] text-sm leading-6 text-[#aab7c4]">
            Use your Mesha Workspace account to continue.
          </p>
        </div>

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
      </section>
    </main>
  );
}
