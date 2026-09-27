"use client";

import { createContext, useContext } from "react";

/**
 * The search params of a route navigation the shell has started but the router has not committed
 * (components/mesha-shell.tsx paints the TARGET route's skeleton at once). A loading shape that
 * branches on the URL (the SOP library vs builder) reads these first, since `useSearchParams()`
 * still answers for the page being left.
 */
export const PendingRouteSearchContext = createContext<URLSearchParams | null>(null);

export function usePendingRouteSearch(): URLSearchParams | null {
  return useContext(PendingRouteSearchContext);
}
