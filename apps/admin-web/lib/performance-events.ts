"use client";

import { faro } from "@grafana/faro-web-sdk";

type PerformancePayload = Record<string, string | number | boolean | null | undefined>;

export function reportAdminPerformanceEvent(
  eventName: string,
  surface: string,
  route: string,
  payload: PerformancePayload = {},
): void {
  faro.api?.pushEvent(eventName, {
    surface,
    route,
    ...compactPayload(payload),
  });

  if (typeof navigator === "undefined") return;
  const body = JSON.stringify({
    event_name: eventName,
    surface,
    route,
    occurred_at: new Date().toISOString(),
    payload,
  });
  if (navigator.sendBeacon) {
    const sent = navigator.sendBeacon("/api/admin-web/performance-events", new Blob([body], { type: "application/json" }));
    if (sent) return;
  }
  fetch("/api/admin-web/performance-events", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
    keepalive: true,
  }).catch(() => {
    // Telemetry must never affect navigation.
  });
}

function compactPayload(payload: PerformancePayload): Record<string, string | number | boolean> {
  const out: Record<string, string | number | boolean> = {};
  for (const [key, value] of Object.entries(payload)) {
    if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
      out[key] = value;
    }
  }
  return out;
}
