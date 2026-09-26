import { Suspense, type ReactNode } from "react";

import { UrlPanel } from "@/components/app/url-panel";
import { watchedParamsKey } from "@/components/app/url-tab-nav";
import type { RouteSearchParams } from "@/lib/search-params";

/**
 * THE way a tabbed / filtered page renders its data (guard: url-keyed-panel). The page renders its
 * header, tabs and filters from the search params alone; each data-dependent panel is an async
 * server component inside `UrlSuspense`, keyed by the params it reads (`watch`).
 *
 * - On a click that changes a watched param the panel shows `fallback` at once (client, `UrlPanel`).
 * - The server answers with the shell right away; the new Suspense key shows `fallback` until the
 *   panel's data streams in. A param the panel does not watch (a drawer id, another panel's pager)
 *   leaves it untouched.
 */
export function UrlSuspense({
  searchParams,
  watch,
  fallback,
  children,
}: {
  searchParams: RouteSearchParams;
  /** The search params this panel's data depends on. */
  watch: readonly string[];
  /** The panel's skeleton (components/app skeleton blocks). */
  fallback: ReactNode;
  children: ReactNode;
}) {
  return (
    <UrlPanel watch={watch} fallback={fallback}>
      <Suspense key={watchedParamsKey(searchParams, watch)} fallback={fallback}>
        {children}
      </Suspense>
    </UrlPanel>
  );
}
