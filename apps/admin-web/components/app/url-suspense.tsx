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
 * - A page that already awaited its data before rendering may wrap its panel JSX directly: the
 *   click-time swap alone keeps the click instant (tab moves, panel is its skeleton, header/tabs/
 *   filters stay); the content replaces the skeleton the moment the server answers. Splitting the
 *   read into an async child additionally streams the new header/tabs before the panel data.
 */
export function UrlSuspense({
  searchParams,
  watch,
  ignore = [],
  fallback,
  fallbackBy,
  children,
}: {
  searchParams: RouteSearchParams;
  /** The search params this panel's data depends on (`ALL_PARAMS` = every param but `ignore`). */
  watch: readonly string[];
  /** With `ALL_PARAMS`: params that never change this panel (drawer / overlay / export state). */
  ignore?: readonly string[];
  /** The panel's skeleton (components/app skeleton blocks). */
  fallback: ReactNode;
  /** Per-value skeletons for one param (tabs with different shapes); `""` = the param absent (default tab). */
  fallbackBy?: { param: string; shapes: Record<string, ReactNode> };
  children: ReactNode;
}) {
  return (
    <UrlPanel watch={watch} ignore={ignore} fallback={fallback} fallbackBy={fallbackBy}>
      <Suspense key={watchedParamsKey(searchParams, watch, ignore)} fallback={fallback}>
        {children}
      </Suspense>
    </UrlPanel>
  );
}
