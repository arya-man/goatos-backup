"use client";

import { useEffect, useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";

const APPLY_DELAY_MS = 250;

type Props = {
  valueG: number;
  maxG: number;
  preserveQuery: [string, string][];
  label: string;
  kgSuffix: string;
};

function thresholdFromTolerance(valueG: number): number {
  return Math.max(0, 35 - valueG / 1000);
}

function thresholdLabel(valueG: number, kgSuffix: string): string {
  return `${thresholdFromTolerance(valueG).toLocaleString("en-IN", {
    maximumFractionDigits: 1,
    minimumFractionDigits: 0,
  })} ${kgSuffix}+`;
}

export function SalesReadyToleranceControl({ valueG, maxG, preserveQuery, label, kgSuffix }: Props) {
  const router = useRouter();
  const [draftG, setDraftG] = useState(valueG);
  const [, startTransition] = useTransition();

  useEffect(() => {
    setDraftG(valueG);
  }, [valueG]);

  const href = useMemo(() => {
    const query = new URLSearchParams(preserveQuery);
    if (draftG > 0) query.set("sale_ready_tolerance_g", String(draftG));
    else query.delete("sale_ready_tolerance_g");
    const qs = query.toString();
    return qs ? `/sales?${qs}` : "/sales";
  }, [draftG, preserveQuery]);

  useEffect(() => {
    if (draftG === valueG) return;
    const timeout = window.setTimeout(() => {
      startTransition(() => router.replace(href, { scroll: false }));
    }, APPLY_DELAY_MS);
    return () => window.clearTimeout(timeout);
  }, [draftG, href, router, valueG]);

  return (
    <div className="sales-ready-tolerance" aria-label={label}>
      <div className="sales-ready-tolerance-head">
        <span>{label}</span>
        <strong>{thresholdLabel(draftG, kgSuffix)}</strong>
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
    </div>
  );
}
