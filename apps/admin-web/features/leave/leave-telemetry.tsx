"use client";

// Faro RUM event for the primary action on /leave (TELEMETRY GUARDRAIL, AGENTS.md): the
// approve/reject decision result. Route-change/error capture is covered globally
// (ObservabilityErrorBoundary in app/(admin)/layout.tsx + FaroProvider setView).
import { useEffect, useRef } from "react";
import { faro } from "@grafana/faro-web-sdk";

export function LeaveActionTelemetry({ status, code }: { status?: string; code?: string }) {
  const reported = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!status) return;
    const key = `${status}:${code ?? ""}`;
    if (reported.current === key) return;
    reported.current = key;
    try {
      faro.api?.pushEvent("leave_decision", { status, code: code ?? "" });
    } catch {
      // Faro must never break the page.
    }
  }, [status, code]);

  return null;
}
