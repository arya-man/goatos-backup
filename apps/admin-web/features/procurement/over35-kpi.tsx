"use client";

// telemetry:exempt read-only KPI card; its one control re-counts a figure and writes nothing

import { useState, useTransition } from "react";

import Stack from "@mui/material/Stack";

import { replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { KpiWidget } from "@/components/app/kpi-widget";
import { num } from "./sales-format";
import { countOver35Action } from "./over35-actions";
import { SalesReadyToleranceControl } from "./sales-ready-tolerance-control";

/** Backend copy the server page resolves; this card names no label of its own. */
export type Over35Labels = {
  title: string;
  sub: string;
  none: string;
  noneValue: string;
  tolerance: string;
  apply: string;
  failed: string;
};

function fillKg(template: string, kg: number) {
  return template.replace("{kg}", num(kg, 1));
}

/**
 * The Over 35 kg card with its error margin (flicker fix, 2026-09-25): Apply re-counts THIS card
 * in place through a server action and moves nothing else on the page. The URL follows through
 * replaceLocalOverlayUrl so a reload keeps the margin; nothing navigates.
 */
export function Over35Kpi({
  parkId,
  enabled,
  disabledReason,
  initialCount,
  initialToleranceG,
  lineKg,
  maxG,
  labels,
}: {
  parkId: string;
  enabled: boolean;
  disabledReason: string;
  initialCount: number | null;
  initialToleranceG: number;
  lineKg: number;
  maxG: number;
  labels: Over35Labels;
}) {
  const [count, setCount] = useState(initialCount);
  const [toleranceG, setToleranceG] = useState(initialToleranceG);
  const [failed, setFailed] = useState(false);
  const [pending, startTransition] = useTransition();
  const thresholdKg = Math.max(0, lineKg - toleranceG / 1000);

  const apply = (nextG: number) => {
    startTransition(async () => {
      const result = await countOver35Action(parkId, nextG);
      if (!result.ok) {
        setFailed(true);
        return;
      }
      setFailed(false);
      setCount(result.count);
      setToleranceG(nextG);
      const url = new URL(window.location.href);
      if (nextG > 0) url.searchParams.set("sale_ready_tolerance_g", String(nextG));
      else url.searchParams.delete("sale_ready_tolerance_g");
      replaceLocalOverlayUrl(`${url.pathname}${url.search}`);
    });
  };

  return (
    // Template CourseWidgetSummary (KpiWidget) like the valuation cards beside it. The error margin
    // tunes THIS card's figure and nothing else on the page, so it sits right under the card
    // (maintainer request 2026-09-14); the template card itself has no footer slot.
    <Stack spacing={1.5} aria-busy={pending || undefined}>
      <KpiWidget
        color="success"
        icon="completed"
        title={fillKg(labels.title, lineKg)}
        total={count}
        caption={
          !enabled
            ? disabledReason
            : failed
              ? labels.failed
              : count == null
                ? labels.none
                : `${labels.sub} · ${num(thresholdKg, 1)}+`
        }
      />
      {enabled ? (
        <SalesReadyToleranceControl
          lineKg={lineKg}
          valueG={toleranceG}
          maxG={maxG}
          label={labels.tolerance}
          applyLabel={labels.apply}
          onApply={apply}
          pending={pending}
        />
      ) : null}
    </Stack>
  );
}
