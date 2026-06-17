"use client";

import { useState } from "react";
import { LogOut } from "lucide-react";
import { LOGIN_PATH } from "@/lib/auth/session-cookie";
import { clearFirebaseSession } from "@/lib/auth/firebase-client";

export function SignOutButton() {
  const [pending, setPending] = useState(false);

  return (
    <button
      type="button"
      title="Sign out"
      aria-label="Sign out"
      disabled={pending}
      onClick={() => {
        setPending(true);
        void clearFirebaseSession().finally(() => {
          window.location.assign(LOGIN_PATH);
        });
      }}
      className="inline-flex h-9 items-center gap-2 rounded-lg border border-[#334155] px-3 text-xs font-bold text-[#c7d1dc] transition hover:border-[#14f1d9]/70 hover:text-[#14f1d9] disabled:cursor-wait disabled:opacity-70"
    >
      <LogOut className="h-4 w-4" aria-hidden="true" />
      <span className="hidden sm:inline">{pending ? "Signing out" : "Sign out"}</span>
    </button>
  );
}
