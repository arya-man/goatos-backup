"use client";

// telemetry:exempt presentational pending mark inside a link; no action of its own

import { useLinkStatus } from "@/components/no-prefetch-link";

/**
 * A small busy ring shown INSIDE the link that was just pressed, while its page is on its way
 * (flicker fix, 2026-09-25). Server-rendered filter chips and pagers are soft navigations that
 * keep the current page on screen until the next one is ready; without a mark on the pressed
 * control the old numbers sat there with no sign anything was happening, then snapped. Must be
 * rendered as a descendant of a next/link.
 */
export function LinkPending() {
  const { pending } = useLinkStatus();
  return pending ? <span className="wfspin" aria-hidden="true" style={{ marginLeft: 6 }} /> : null;
}
