"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import type { ReactNode } from "react";

/**
 * DialogModal overlays server-rendered content in a centered popup when `open`.
 * Backdrop click and Escape navigate to `closeHref` (server nav drops the
 * selection param and the modal disappears). Body scroll is locked while open.
 */
export function DialogModal({
  open,
  closeHref,
  label,
  children,
}: {
  open: boolean;
  closeHref: string;
  label: string;
  children: ReactNode;
}) {
  const router = useRouter();

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") router.push(closeHref);
    };
    document.addEventListener("keydown", onKey);
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = previousOverflow;
    };
  }, [open, closeHref, router]);

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={label}
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/70 p-4 sm:p-8"
      onClick={() => router.push(closeHref)}
    >
      <div
        className="relative my-auto w-full max-w-5xl rounded-xl border border-[#293241] bg-[#0b0f15] shadow-2xl"
        onClick={(event) => event.stopPropagation()}
      >
        {children}
      </div>
    </div>
  );
}
