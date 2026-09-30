import { faro } from "@grafana/faro-web-sdk";

// Faro RUM for the HRMS discipline writes (TELEMETRY GUARDRAIL): outcome only, never a name.
export type DisciplineEvent = "hrms_violation_record" | "hrms_violation_withdraw" | "hrms_violation_keep" | "hrms_violation_close" | "hrms_enquiry_submit";

export function trackDiscipline(event: DisciplineEvent, status: "success" | "error", code = "") {
  try {
    faro.api?.pushEvent(event, { status, code });
  } catch {
    // Faro must never break the page.
  }
}
