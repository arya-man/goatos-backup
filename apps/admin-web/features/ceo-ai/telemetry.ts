import { faro } from "@grafana/faro-web-sdk";

// Faro telemetry for the leadership assistant surface, per
// docs/observability/TELEMETRY_GUARDRAILS.md. Every user-facing interaction on
// this surface (open, ask, answer, error) emits a namespaced event. Guarded on
// `faro.api` so it is a no-op in local/dev where no Faro receiver is configured.
// Attributes carry only non-sensitive metadata — never the raw question text,
// actor identity, tokens, or business rows.

export const CeoAiEvents = {
  Open: "ceo_ai_open",
  Close: "ceo_ai_close",
  Ask: "ceo_ai_ask",
  Answer: "ceo_ai_answer",
  Error: "ceo_ai_error",
  StopGenerating: "ceo_ai_stop_generating",
  NewChat: "ceo_ai_new_chat",
  ResumeChat: "ceo_ai_resume_chat",
  DeleteChat: "ceo_ai_delete_chat",
  StarterClick: "ceo_ai_starter_click",
} as const;

export type CeoAiEventName = (typeof CeoAiEvents)[keyof typeof CeoAiEvents];

export function trackCeoAiEvent(
  name: CeoAiEventName,
  attributes?: Record<string, string>,
): void {
  try {
    if (typeof window === "undefined" || !faro.api) return;
    faro.api.pushEvent(name, attributes ?? {});
  } catch {
    // Telemetry must never break the chat surface.
  }
}

export function trackCeoAiError(context: string, message: string, status?: number): void {
  trackCeoAiEvent(CeoAiEvents.Error, {
    context,
    message: message.slice(0, 200),
    ...(status !== undefined ? { status: String(status) } : {}),
  });
}
