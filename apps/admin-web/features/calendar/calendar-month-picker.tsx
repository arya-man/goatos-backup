"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

export function CalendarMonthPicker({
  label,
  open,
  closeHref,
  children,
}: {
  label: string;
  open: boolean;
  closeHref: string;
  children: ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname() ?? "";
  const searchParams = useSearchParams();
  const rootRef = useRef<HTMLDetailsElement | null>(null);

  useEffect(() => {
    function onDown(event: MouseEvent) {
      const root = rootRef.current;
      if (!root?.open) return;
      const target = event.target as Node | null;
      if (target && root.contains(target)) return;
      root.open = false;
      const currentHref = `${pathname}${searchParams?.toString() ? `?${searchParams.toString()}` : ""}`;
      if (closeHref !== currentHref) {
        router.replace(closeHref, { scroll: false });
      }
    }
    function onKey(event: KeyboardEvent) {
      if (event.key !== "Escape" || !rootRef.current?.open) return;
      rootRef.current.open = false;
      const currentHref = `${pathname}${searchParams?.toString() ? `?${searchParams.toString()}` : ""}`;
      if (closeHref !== currentHref) {
        router.replace(closeHref, { scroll: false });
      }
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [closeHref, pathname, router, searchParams]);

  return (
    <details ref={rootRef} className="calpicker" open={open}>
      <summary
        onClick={(event) => {
          event.preventDefault();
          if (!rootRef.current) return;
          if (rootRef.current.open) {
            rootRef.current.open = false;
            const currentHref = `${pathname}${searchParams?.toString() ? `?${searchParams.toString()}` : ""}`;
            if (closeHref !== currentHref) {
              router.replace(closeHref, { scroll: false });
            }
            return;
          }
          rootRef.current.open = true;
        }}
      >
        {label}
      </summary>
      {children}
    </details>
  );
}
