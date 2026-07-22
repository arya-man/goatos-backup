import { faro } from "@grafana/faro-web-sdk";

// Faro telemetry for the ADMIN-ONLY leadership-assistant step-trace debug
// surface, per docs/observability/TELEMETRY_GUARDRAILS.md. This surface is an
// internal engineering/admin tool (ceo_internal/superadmin only), but it is
// still a user-facing admin-web route, so it wires analytics + error events.
// Attributes carry only non-sensitive metadata — never the trace body, actor
// identity, question text, or business rows.

export const CeoAiAdminEvents = {
  Open: "ceo_ai_admin_trace_open",
  Lookup: "ceo_ai_admin_trace_lookup",
  Loaded: "ceo_ai_admin_trace_loaded",
  Error: "ceo_ai_admin_trace_error",
} as const;

export type CeoAiAdminEventName =
  (typeof CeoAiAdminEvents)[keyof typeof CeoAiAdminEvents];

export function trackCeoAiAdminEvent(
  name: CeoAiAdminEventName,
  attributes?: Record<string, string>,
): void {
  try {
    if (typeof window === "undefined" || !faro.api) return;
    faro.api.pushEvent(name, attributes ?? {});
  } catch {
    // Telemetry must never break the debug surface.
  }
}

export function trackCeoAiAdminError(context: string, message: string, status?: number): void {
  trackCeoAiAdminEvent(CeoAiAdminEvents.Error, {
    context,
    message: message.slice(0, 200),
    ...(status !== undefined ? { status: String(status) } : {}),
  });
}
