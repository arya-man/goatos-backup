"use client";

import { useEffect } from "react";
import { faro } from "@grafana/faro-web-sdk";

// Telemetry for Counts -> Herd Analytics (docs/observability/TELEMETRY_GUARDRAILS.md §2.2).
//
// Crash reporting is already structural: every (admin) route is wrapped once by
// ObservabilityErrorBoundary. What that cannot report is the primary user action —
// that a leader opened the herd read, for which park, and over how long a window.
// The window length is carried because it is the one control on the page: if every
// session immediately widens to 24 months, the 12-month default is the wrong default.
//
// Renders nothing.
export function HerdAnalyticsTelemetry({
  routeId,
  parkId,
  months,
}: {
  routeId: string;
  parkId?: string;
  months: number;
}) {
  useEffect(() => {
    // faro.api is undefined when Faro was never initialized (no collector configured
    // locally, or during SSR) — the optional call keeps this a no-op rather than a crash.
    faro.api?.pushEvent("counts_page_view", {
      route_id: routeId,
      park_id: parkId ?? "",
      months: String(months),
    });
  }, [routeId, parkId, months]);

  return null;
}
