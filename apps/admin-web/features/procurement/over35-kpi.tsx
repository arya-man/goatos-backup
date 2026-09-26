"use client";

// telemetry:exempt read-only KPI card; its one control re-counts a figure and writes nothing

import { useState, useTransition } from "react";

import Box from "@mui/material/Box";
import { Weight } from "lucide-react";

import { replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { KpiCard } from "@/components/minimal/widgets";
import { KpiValue } from "./kpi-value";
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
    // Template CourseWidgetSummary (plain card, tone icon in the corner) like the valuation
    // cards beside it -- never the pastel AnalyticsWidgetSummary in dark.
    <KpiCard
      tone="success"
      label={fillKg(labels.title, lineKg)}
      // The figure stays readable while it is re-counted -- only this card says it is busy.
      value={
        count == null ? (
          labels.noneValue
        ) : (
          <Box component="span" aria-busy={pending || undefined} sx={pending ? { opacity: 0.6 } : undefined}>
            <KpiValue value={count} />
          </Box>
        )
      }
      icon={<Weight aria-hidden="true" />}
      hint={
        !enabled
          ? disabledReason
          : failed
            ? labels.failed
            : count == null
              ? labels.none
              : `${labels.sub} · ${num(thresholdKg, 1)}+`
      }
      // The error margin tunes THIS card's figure and nothing else on the page, so it sits inside
      // the card (maintainer request 2026-09-14).
      footer={
        enabled ? (
          <SalesReadyToleranceControl
            lineKg={lineKg}
            valueG={toleranceG}
            maxG={maxG}
            label={labels.tolerance}
            applyLabel={labels.apply}
            onApply={apply}
            pending={pending}
          />
        ) : null
      }
    />
  );
}
