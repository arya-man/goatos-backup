"use client";

import { useEffect } from "react";
import { faro } from "@grafana/faro-web-sdk";

// Telemetry for the Feed routes (docs/observability/TELEMETRY_GUARDRAILS.md §2.2).
//
// Crash/error reporting is already structural: every route in the (admin) group is wrapped once by
// ObservabilityErrorBoundary in app/(admin)/layout.tsx, which pushes an error to Faro. What that
// boundary cannot report is the primary user action — that a Feed surface was actually opened, for
// which park and which feed day, and whether the day it opened on carried authored gaps.
//
// That last part is the reason this is not just a page-view counter. A blocked cell means a shed
// goes unfed, and the operator has to notice it. `blockedCells` here lets the number of unclosed
// ration gaps be tracked over time rather than being visible only to whoever happened to open the
// page that morning.
//
// No visible output — this component renders nothing.

export type FeedFaroViewProps = {
  /** The page contract's route_id: feed-direction | feed-packing | feed-config | toxin-reports. */
  routeId: string;
  parkId?: string;
  targetDate?: string;
  /** Authored ration gaps visible on this page, when the surface reports them. */
  blockedCells?: number;
  /**
   * Feed loads whose aflatoxin strip came back positive or unusable, on the Toxin report.
   * Tracked for the same reason as blockedCells: a flagged load is a bag of feed nobody should
   * issue, and its count must be visible over time rather than only to whoever opened the page.
   */
  flaggedLoads?: number;
};

export function FeedFaroView({
  routeId,
  parkId,
  targetDate,
  blockedCells,
  flaggedLoads,
}: FeedFaroViewProps) {
  useEffect(() => {
    // faro.api is undefined when Faro was never initialized (no collector configured locally, or
    // during SSR) — the optional call keeps this a no-op there rather than a crash.
    faro.api?.pushEvent("feed_page_view", {
      route_id: routeId,
      park_id: parkId ?? "",
      target_date: targetDate ?? "",
      blocked_cells: blockedCells === undefined ? "" : String(blockedCells),
      flagged_loads: flaggedLoads === undefined ? "" : String(flaggedLoads),
    });
  }, [routeId, parkId, targetDate, blockedCells, flaggedLoads]);

  return null;
}
