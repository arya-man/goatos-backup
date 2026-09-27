"use client";

import Link from "@/components/no-prefetch-link";
import { KpiGrid } from "@/components/app/kpi-grid";
import { KpiWidget, kpiColor } from "@/components/app/kpi-widget";
import type { HerdSignalsSummary } from "@/lib/api/herd-signals";
import { useHerdSignalsNav } from "./herd-signals-nav-context";
import { useHerdSignalsLiveSnapshot } from "./herd-signals-live-store";
import { herdSignalsHref, type HerdSignalsParams } from "./params";
import { KPI_DEFS } from "./herd-signals-kpi-defs";

// The tenant-scoped summary drives every KPI number here — never a count of the fetched page's
// rows. "Tags seen" duplicates the first slot deliberately (Section 8, insight #1) but keeps a
// distinct filter key so a click on it does not collide with "Animals moving".
//
// Clicking a card is a filter change, so it goes through the SAME shared transition as the filter
// bar (useHerdSignalsNav) rather than a plain <Link> navigation — a plain Link here was the
// "clicking a KPI reloads the whole page" defect: no pending affordance, table just blanked and
// reappeared.
export function HerdSignalsKpis({ summary, params, liveKey }: { summary: HerdSignalsSummary; params: HerdSignalsParams; liveKey: string }) {
  const { navigate } = useHerdSignalsNav();
  const liveSnapshot = useHerdSignalsLiveSnapshot(liveKey);
  const displayedSummary = liveSnapshot?.data.summary ?? summary;
  const serverMovementKpis = new Set(["moving_now", "active_1m", "moving_15m", "quiet"]);
  return (
    // A KPI click swaps this deck to the panel skeleton (UrlSuspense ALL_PARAMS in herd-signals-board).
    <div className="herd-signals-kpis">
      <KpiGrid min={220}>
        {KPI_DEFS.map((def, index) => {
          const filterKey = index === 0 ? undefined : def.key;
          const active = filterKey ? params.kpi === filterKey : false;
          const href = filterKey
            ? herdSignalsHref(params, {
                hs_kpi: active ? undefined : filterKey,
                hs_move: serverMovementKpis.has(filterKey) ? undefined : params.movementState,
              })
            : undefined;
          const card = (
            <KpiWidget
              title={def.label}
              total={def.value(displayedSummary)}
              color={kpiColor(def.tone)}
              caption={active ? `${def.detail(displayedSummary)} \u00b7 filtering` : def.detail(displayedSummary)}
              sx={{ height: 1 }}
            />
          );
          if (!href) return <div key={def.label}>{card}</div>;
          return (
            <Link
              key={def.label}
              href={href}
              className="kit-kpi-link"
              aria-current={active ? "true" : undefined}
              onClick={(event) => {
                if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                event.preventDefault();
                navigate(href);
              }}
            >
              {card}
            </Link>
          );
        })}
      </KpiGrid>
    </div>
  );
}

export function HerdSignalsKpiChip({ params }: { params: HerdSignalsParams }) {
  const { navigate } = useHerdSignalsNav();
  if (!params.kpi) return null;
  const def = KPI_DEFS.find((item) => item.key === params.kpi);
  if (!def) return null;
  const href = herdSignalsHref(params, { hs_kpi: undefined });
  return (
    <Link
      href={href}
      className="achip"
      title="Clear this KPI filter"
      onClick={(event) => {
        if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        navigate(href);
      }}
    >
      {def.label} <b>✕</b>
    </Link>
  );
}
