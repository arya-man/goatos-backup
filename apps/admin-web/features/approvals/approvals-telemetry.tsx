"use client";

// Faro RUM event for the primary action on /approvals (TELEMETRY GUARDRAIL, AGENTS.md): the
// approve/reject decision result. Route-change/error capture is already covered globally
// (ObservabilityErrorBoundary in app/(admin)/layout.tsx + FaroProvider setView), so this component
// only adds the per-action event the guard's admin_web marker check looks for.
import { useEffect, useRef } from "react";
import { faro } from "@grafana/faro-web-sdk";

export function ApprovalsActionTelemetry({ status, code }: { status?: string; code?: string }) {
  const reported = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!status) return;
    const key = `${status}:${code ?? ""}`;
    if (reported.current === key) return;
    reported.current = key;
    try {
      faro.api?.pushEvent("approvals_decision", {
        status,
        code: code ?? "",
      });
    } catch {
      // Faro must never break the page.
    }
  }, [status, code]);

  return null;
}
