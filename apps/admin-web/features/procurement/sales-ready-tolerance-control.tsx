"use client";

import { useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";

type Props = {
  valueG: number;
  maxG: number;
  preserveQuery: [string, string][];
  label: string;
  applyLabel: string;
  /**
   * The page the applied margin re-renders: the Sales board by default, or Farm value, which
   * has carried the card since the 2026-09-11 split. A control that always replaced to /sales
   * would bounce a Farm value reader onto the ledger on Apply.
   */
  pagePath?: string;
};

function thresholdFromTolerance(valueG: number): number {
  return Math.max(0, 35 - valueG / 1000);
}

function thresholdLabel(valueG: number): string {
  return `${thresholdFromTolerance(valueG).toLocaleString("en-IN", {
    maximumFractionDigits: 1,
    minimumFractionDigits: 0,
  })}+`;
}

export function SalesReadyToleranceControl({
  valueG,
  maxG,
  preserveQuery,
  label,
  applyLabel,
  pagePath = "/sales",
}: Props) {
  const router = useRouter();
  const [draftG, setDraftG] = useState(valueG);
  const [, startTransition] = useTransition();

  const href = useMemo(() => {
    const query = new URLSearchParams(preserveQuery);
    if (draftG > 0) query.set("sale_ready_tolerance_g", String(draftG));
    else query.delete("sale_ready_tolerance_g");
    const qs = query.toString();
    return qs ? `${pagePath}?${qs}` : pagePath;
  }, [draftG, preserveQuery, pagePath]);

  const apply = () => {
    if (draftG === valueG) return;
    startTransition(() => router.replace(href, { scroll: false }));
  };

  return (
    <div className="sales-ready-tolerance" aria-label={label}>
      <div className="sales-ready-tolerance-head">
        <label htmlFor="sale-ready-tolerance">{label}</label>
        <strong>{thresholdLabel(draftG)}</strong>
      </div>
      <div className="sales-ready-tolerance-row">
        <input
          id="sale-ready-tolerance"
          name="sale_ready_tolerance_g"
          type="range"
          min="0"
          max={maxG}
          step="50"
          value={draftG}
          onChange={(event) => setDraftG(Number(event.currentTarget.value))}
        />
        <output htmlFor="sale-ready-tolerance">{draftG} g</output>
      </div>
      <button className="btn ghost small" type="button" disabled={draftG === valueG} onClick={apply}>
        {applyLabel}
      </button>
    </div>
  );
}
