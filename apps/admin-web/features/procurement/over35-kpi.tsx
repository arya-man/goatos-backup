"use client";

// telemetry:exempt read-only KPI card; its one control re-counts a figure and writes nothing

import { createContext, useContext, useState, useTransition, type ReactNode } from "react";

import Box from "@mui/material/Box";

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

type Over35Props = {
  parkId: string;
  enabled: boolean;
  disabledReason: string;
  initialCount: number | null;
  initialToleranceG: number;
  lineKg: number;
  maxG: number;
  labels: Over35Labels;
};

type Over35State = Over35Props & {
  count: number | null;
  toleranceG: number;
  failed: boolean;
  pending: boolean;
  apply: (nextG: number) => void;
};

const Over35Context = createContext<Over35State | null>(null);

function useOver35(): Over35State {
  const state = useContext(Over35Context);
  if (!state) throw new Error("Over35Kpi / Over35MarginForm render inside Over35Scope");
  return state;
}

/**
 * The Over 35 kg figure and its error margin share one state (flicker fix, 2026-09-25): Apply
 * re-counts THE CARD in place through a server action and moves nothing else on the page. The URL
 * follows through replaceLocalOverlayUrl so a reload keeps the margin; nothing navigates. The
 * margin is a form of its own (TR2-P1-3: a slider + Apply inside the KPI deck is not template),
 * so the scope wraps the section and the tile and the form sit where the template puts each.
 */
export function Over35Scope({ children, ...props }: Over35Props & { children: ReactNode }) {
  const { parkId, initialCount, initialToleranceG } = props;
  const [count, setCount] = useState(initialCount);
  const [toleranceG, setToleranceG] = useState(initialToleranceG);
  const [failed, setFailed] = useState(false);
  const [pending, startTransition] = useTransition();

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
    <Over35Context.Provider value={{ ...props, count, toleranceG, failed, pending, apply }}>{children}</Over35Context.Provider>
  );
}

/** The Over 35 kg tile: template CourseWidgetSummary (KpiWidget) like the valuation cards beside it. */
export function Over35Kpi() {
  const { enabled, disabledReason, failed, count, labels, lineKg, toleranceG, pending } = useOver35();
  const thresholdKg = Math.max(0, lineKg - toleranceG / 1000);
  return (
    <Box aria-busy={pending || undefined} sx={{ height: 1 }}>
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
    </Box>
  );
}

/**
 * The error margin as a template toolbar form (label, slider, value, Apply) on the section's own
 * row, not inside the KPI deck. Renders nothing when the card is gated off.
 */
export function Over35MarginForm() {
  const { enabled, lineKg, toleranceG, maxG, labels, apply, pending } = useOver35();
  if (!enabled) return null;
  return (
    <SalesReadyToleranceControl
      lineKg={lineKg}
      valueG={toleranceG}
      maxG={maxG}
      label={labels.tolerance}
      applyLabel={labels.apply}
      onApply={apply}
      pending={pending}
    />
  );
}
