"use client";

import { useCallback, useEffect, useRef, useTransition, type ReactNode } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

export function CalendarMonthPicker({
  label,
  open,
  openHref,
  closeHref,
  children,
}: {
  label: string;
  open: boolean;
  openHref: string;
  closeHref: string;
  children: ReactNode;
}) {
  const router = useRouter();
  const [, startTransition] = useTransition();
  const pathname = usePathname() ?? "";
  const searchParams = useSearchParams();
  const rootRef = useRef<HTMLDetailsElement | null>(null);

  const replaceHref = useCallback((href: string) => {
    startTransition(() => {
      router.replace(href, { scroll: false });
    });
  }, [router]);

  useEffect(() => {
    function onDown(event: MouseEvent) {
      const root = rootRef.current;
      if (!root?.open) return;
      const target = event.target as Node | null;
      if (target && root.contains(target)) return;
      root.open = false;
      const currentHref = `${pathname}${searchParams?.toString() ? `?${searchParams.toString()}` : ""}`;
      if (closeHref !== currentHref) {
        replaceHref(closeHref);
      }
    }
    function onKey(event: KeyboardEvent) {
      if (event.key !== "Escape" || !rootRef.current?.open) return;
      rootRef.current.open = false;
      const currentHref = `${pathname}${searchParams?.toString() ? `?${searchParams.toString()}` : ""}`;
      if (closeHref !== currentHref) {
        replaceHref(closeHref);
      }
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [closeHref, pathname, replaceHref, searchParams]);

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
              replaceHref(closeHref);
            }
            return;
          }
          replaceHref(openHref);
        }}
      >
        {label}
      </summary>
      {children}
    </details>
  );
}
