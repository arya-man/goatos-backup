"use client";

import { useState } from "react";

type Props = {
  /** The sale-ready line in kg (the tenant's assumption); the tolerance is taken off this. */
  lineKg: number;
  /** The margin the card's figure was counted at. */
  valueG: number;
  maxG: number;
  label: string;
  applyLabel: string;
  /**
   * Re-counts the card at the chosen margin. The CARD owns the request (flicker fix, 2026-09-25):
   * Apply used to re-render the whole Farm value page and threw the transition's pending
   * state away, so the figure sat stale with no sign anything was happening, then snapped.
   */
  onApply: (toleranceG: number) => void;
  /** True while the re-count is in flight: the button says so and cannot be pressed twice. */
  pending: boolean;
};

function thresholdFromTolerance(lineKg: number, valueG: number): number {
  return Math.max(0, lineKg - valueG / 1000);
}

function thresholdLabel(lineKg: number, valueG: number): string {
  return `${thresholdFromTolerance(lineKg, valueG).toLocaleString("en-IN", {
    maximumFractionDigits: 1,
    minimumFractionDigits: 0,
  })}+`;
}

export function SalesReadyToleranceControl({ lineKg, valueG, maxG, label, applyLabel, onApply, pending }: Props) {
  const [draftG, setDraftG] = useState(valueG);

  return (
    <div className="sales-ready-tolerance" aria-label={label} aria-busy={pending || undefined}>
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
      <button
        className="btn ghost small"
        type="button"
        disabled={draftG === valueG || pending}
        aria-disabled={draftG === valueG || pending}
        onClick={() => onApply(draftG)}
      >
        {pending ? <span className="wfspin" aria-hidden="true" /> : null}
        {applyLabel}
      </button>
    </div>
  );
}
