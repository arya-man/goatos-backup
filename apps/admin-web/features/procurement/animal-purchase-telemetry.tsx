"use client";

// TELEMETRY GUARDRAIL for /procurement/animal-purchases: one "opened" event per page visit and
// one "decision" event when the Server Action's redirect feedback lands, mirroring
// toxin-review-list.tsx. Renders nothing. Faro must never break the page.
import { useEffect, useRef } from "react";
import { faro } from "@grafana/faro-web-sdk";

export function AnimalPurchaseTelemetry({
  rows,
  pending,
  feedback,
}: {
  rows: number;
  pending: number;
  feedback: { status?: string; code?: string };
}) {
  const openedReported = useRef(false);
  useEffect(() => {
    if (openedReported.current) return;
    openedReported.current = true;
    try {
      faro.api?.pushEvent("animal_purchase_review_opened", { rows: String(rows), pending: String(pending) });
    } catch {
      // Faro must never break the page.
    }
    // Counts at first paint are enough; re-reporting on refresh would double-count the visit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const feedbackReported = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!feedback.status) return;
    const key = `${feedback.status}:${feedback.code ?? ""}`;
    if (feedbackReported.current === key) return;
    feedbackReported.current = key;
    try {
      faro.api?.pushEvent("animal_purchase_decision", { status: feedback.status, code: feedback.code ?? "" });
    } catch {
      // Faro must never break the page.
    }
  }, [feedback.status, feedback.code]);

  return null;
}
