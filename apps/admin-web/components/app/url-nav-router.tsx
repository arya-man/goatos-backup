"use client";

// telemetry:exempt navigation plumbing — no user action of its own, no data read

import { useContext, useMemo, type ReactNode } from "react";
import { AppRouterContext } from "next/dist/shared/lib/app-router-context.shared-runtime";

import { announceUrlNav } from "@/components/app/url-tab-nav";

/**
 * Every `router.push` / `router.replace` below the shell announces itself first (guard:
 * url-keyed-panel). The URL-keyed panels (`UrlSuspense`) listen for that announcement and swap to
 * their skeleton in the same frame as the click, so a filter, select, date picker, sort header or
 * pager built on `useRouter()` never holds the old panel on screen while the server answers — even
 * one that forgot to call `announceUrlNav` itself. Mounted once, around the page, in the shell.
 */
export function UrlNavRouter({ children }: { children: ReactNode }) {
  const router = useContext(AppRouterContext);
  const announcing = useMemo(
    () =>
      router
        ? {
            ...router,
            push: (href: string, options?: Parameters<typeof router.push>[1]) => {
              announceUrlNav(href);
              router.push(href, options);
            },
            replace: (href: string, options?: Parameters<typeof router.replace>[1]) => {
              announceUrlNav(href);
              router.replace(href, options);
            },
          }
        : null,
    [router],
  );
  return <AppRouterContext.Provider value={announcing}>{children}</AppRouterContext.Provider>;
}
