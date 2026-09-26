"use client";

import { useEffect } from "react";
import { faro } from "@grafana/faro-web-sdk";

// Reports the shell's contract failure to Faro: the kind, code and trace id the screen no longer
// prints (2026-09-26). Renders nothing.
export function ContractUnavailableTelemetry({
  kind,
  code,
  status,
  traceId,
}: {
  kind: string;
  code?: string;
  status?: number;
  traceId?: string;
}) {
  useEffect(() => {
    faro.api?.pushEvent("admin_shell_contract_unavailable", {
      kind,
      code: code ?? "",
      status: status == null ? "" : String(status),
      trace_id: traceId ?? "",
    });
  }, [kind, code, status, traceId]);
  return null;
}
