"use client";

import { useEffect } from "react";
import { faro } from "@grafana/faro-web-sdk";

// Telemetry for Counts -> Mortality (docs/observability/TELEMETRY_GUARDRAILS.md §2.2).
//
// Crash reporting is structural: every (admin) route is wrapped once by
// ObservabilityErrorBoundary. What that cannot report is the primary user action — that a
// leader opened the mortality read, for which park, over how long a window, and how many
// deaths it showed. Renders nothing.
export function MortalityTelemetry({
  routeId,
  parkId,
  months,
  deaths,
}: {
  routeId: string;
  parkId?: string;
  months: number;
  deaths: number;
}) {
  useEffect(() => {
    // faro.api is undefined when Faro was never initialized (no collector configured
    // locally, or during SSR) — the optional call keeps this a no-op rather than a crash.
    faro.api?.pushEvent("counts_page_view", {
      route_id: routeId,
      park_id: parkId ?? "",
      months: String(months),
      deaths: String(deaths),
    });
  }, [routeId, parkId, months, deaths]);

  return null;
}
