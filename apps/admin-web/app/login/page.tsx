import { AuthRequiredPanel } from "@/components/admin-primitives";

export const dynamic = "force-dynamic";

export default function LoginPage() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-[#0f1115] px-4 py-8 text-[#f8fafc]">
      <div className="w-full max-w-5xl">
        <AuthRequiredPanel />
      </div>
    </main>
  );
}
