"use client";

import { useEffect } from "react";
import { faro } from "@grafana/faro-web-sdk";

// Telemetry for Health -> Health Analytics (docs/observability/TELEMETRY_GUARDRAILS.md §2.2).
//
// Crash reporting is already structural: every (admin) route is wrapped once by
// ObservabilityErrorBoundary. What that cannot report is the primary user action — that a
// director opened the health read, for which park, over how long a window, and WHICH TAB they
// landed on. The tab matters here more than on most screens: five views ship together, and if
// nobody ever leaves the overview then four of them are carrying weight they do not earn.
//
// Renders nothing.
export function HealthAnalyticsTelemetry({
  routeId,
  parkId,
  tab,
  months,
}: {
  routeId: string;
  parkId?: string;
  tab: string;
  months: number;
}) {
  useEffect(() => {
    // faro.api is undefined when Faro was never initialized (no collector configured locally,
    // or during SSR) — the optional call keeps this a no-op rather than a crash.
    faro.api?.pushEvent("health_page_view", {
      route_id: routeId,
      park_id: parkId ?? "",
      tab,
      months: String(months),
    });
  }, [routeId, parkId, tab, months]);

  return null;
}
