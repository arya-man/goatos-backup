"use client";

import { useState } from "react";
import Slider from "@mui/material/Slider";
import Card from "@mui/material/Card";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
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
  const changed = draftG !== valueG || pending;

  return (
    // Template list-toolbar anatomy (TR2-P1-3): its own Card on the section row, label + threshold,
    // the slider and its value, then Apply -- one row from sm, stacked on a phone (44px taps).
    // Never inside the KPI deck. guard: kpi-row-no-stretch
    <Card data-over35-margin="" aria-label={label} aria-busy={pending || undefined}>
      <Stack direction={{ xs: "column", sm: "row" }} spacing={{ xs: 1, sm: 2.5 }} sx={{ p: 2.5, alignItems: { xs: "stretch", sm: "center" } }}>
        <Stack direction="row" spacing={1} sx={{ flexShrink: 0, alignItems: "baseline", justifyContent: "space-between" }}>
          <Typography component="label" htmlFor="sale-ready-tolerance" variant="subtitle2">
            {label}
          </Typography>
          <Typography component="strong" variant="subtitle2" sx={{ color: "text.secondary" }}>
            {thresholdLabel(lineKg, draftG)}
          </Typography>
        </Stack>
        <Stack direction="row" spacing={2} sx={{ flex: "1 1 auto", minWidth: 0, alignItems: "center" }}>
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
            sx={{ flex: "1 1 auto", minWidth: 0 }}
          />
          <Typography component="output" htmlFor="sale-ready-tolerance" variant="body2" sx={{ minWidth: 48, textAlign: "right" }}>
            {draftG} g
          </Typography>
        </Stack>
        {/* DECIDED "no dead controls" (J2B P2-8): no dimmed idle Apply. It keeps its slot (so the
            slider under the reader's thumb does not resize) but is hidden and out of the tab order
            until the margin is moved; while the apply runs it shows its spinner. */}
        <Button
          variant="contained"
          color="inherit"
          type="button"
          disabled={pending}
          aria-hidden={!changed || undefined}
          tabIndex={changed ? undefined : -1}
          onClick={() => onApply(draftG)}
          startIcon={pending ? <CircularProgress size={14} color="inherit" aria-hidden="true" /> : undefined}
          sx={{ flexShrink: 0, visibility: changed ? "visible" : "hidden" }}
        >
          {applyLabel}
        </Button>
      </Stack>
    </Card>
  );
}
