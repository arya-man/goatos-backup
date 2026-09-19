"use client";

import { useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";

type Props = {
  /** The sale-ready line in kg (the tenant's assumption); the tolerance is taken off this. */
  lineKg: number;
  valueG: number;
  maxG: number;
  /** The saved sale-ready line (Sales Config) the margin is taken off; 35 kg until one is saved. */
  lineKg?: number;
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

// `lineKg` is the sale-ready line itself -- the tenant's sale_ready_threshold_kg assumption
// (maintainer decision 2026-09-19) -- so the label never names a line the count was not taken at.
function thresholdFromTolerance(lineKg: number, valueG: number): number {
  return Math.max(0, lineKg - valueG / 1000);
}

function thresholdLabel(lineKg: number, valueG: number): string {
  return `${thresholdFromTolerance(lineKg, valueG).toLocaleString("en-IN", {
    maximumFractionDigits: 1,
    minimumFractionDigits: 0,
  })}+`;
}

export function SalesReadyToleranceControl({
  lineKg,
  valueG,
  maxG,
  preserveQuery,
  label,
  applyLabel,
  pagePath = "/sales",
  lineKg = 35,
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
        <strong>{thresholdLabel(lineKg, draftG)}</strong>
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
