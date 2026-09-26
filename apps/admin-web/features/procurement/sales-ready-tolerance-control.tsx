"use client";

import { useState } from "react";
import Slider from "@mui/material/Slider";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import CircularProgress from "@mui/material/CircularProgress";

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
    // Footer slot of the Over-35 KPI widget: head across, slider row left, Apply right — it wraps
    // inside the card rather than running past its edge. `&&` outranks the legacy class rules.
    <Box
      className="sales-ready-tolerance"
      aria-label={label}
      aria-busy={pending || undefined}
      sx={{
        "&&": {
          width: "100%",
          mt: 1.5,
          position: "relative",
          zIndex: 1,
          display: "grid",
          gridTemplateColumns: "minmax(0,1fr) auto",
          columnGap: 1.5,
          rowGap: 1,
          alignItems: "center",
        },
        "&& > .MuiButton-root": { gridColumn: 2, justifySelf: "end" },
      }}
    >
      <Box
        className="sales-ready-tolerance-head"
        sx={{ "&&": { gridColumn: "1 / -1", display: "flex", justifyContent: "space-between", alignItems: "baseline", gap: 1 } }}
      >
        <label htmlFor="sale-ready-tolerance">{label}</label>
        <strong>{thresholdLabel(lineKg, draftG)}</strong>
      </Box>
      <Box
        className="sales-ready-tolerance-row"
        sx={{ "&&": { gridColumn: 1, minWidth: 0, display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 } }}
      >
        <Slider
          id="sale-ready-tolerance"
          name="sale_ready_tolerance_g"
          size="small"
          min={0}
          max={maxG}
          step={50}
          value={draftG}
          onChange={(_e, v) => setDraftG(Array.isArray(v) ? v[0] : v)}
          aria-label={label}
          sx={{ flex: "1 1 120px", minWidth: 0 }}
        />
        <output htmlFor="sale-ready-tolerance">{draftG} g</output>
      </Box>
      <Button
        variant="soft"
        color="primary"
        size="small"
        type="button"
        disabled={draftG === valueG || pending}
        aria-disabled={draftG === valueG || pending}
        onClick={() => onApply(draftG)}
        startIcon={pending ? <CircularProgress size={14} color="inherit" aria-hidden="true" /> : undefined}
      >
        {applyLabel}
      </Button>
    </Box>
  );
}
