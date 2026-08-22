"use client";

import Link from "@/components/no-prefetch-link";
import type { HerdSignalsSummary } from "@/lib/api/herd-signals";
import { useHerdSignalsNav } from "./herd-signals-nav-context";
import { herdSignalsHref, type HerdSignalsParams, type KpiFilterKey } from "./params";

type KpiDef = {
  key: KpiFilterKey;
  label: string;
  type: "Direct" | "Derived";
  tone: "mut" | "ok" | "warn" | "dng";
  value: (summary: HerdSignalsSummary) => number;
  detail: string;
};

// Every summary field here is TAG-grain and counts unmapped tags exactly like mapped ones
// (docs/modules/herd-signals.md "Unmapped tags are the NORMAL state" — on staging today NO tag is
// mapped, so a mapped-only count would read all zeros). Labels say "tags," never "animals," for
// that reason. Tiers follow Section 8: a recency-thresholded count, a movement-trend bucket, a
// stale/weak/low-voltage threshold check are all DERIVED, not Direct — only the tag's own reported
// fields (RSSI, battery mV, tag temperature, motion_count) are Direct.
//
// Known gap, left honest rather than papered over: the "low" movement-trend bucket (Section 5) has
// no dedicated field on HerdSignalsSummary, so it is not counted by any card here. Adding one would
// mean summing it from the fetched page (banned — see herd-signals-row-filter.ts) rather than from
// a real backend aggregate.
const KPI_DEFS: KpiDef[] = [
  {
    key: "moving" as KpiFilterKey,
    label: "Tags seen",
    type: "Derived",
    tone: "mut",
    value: (s) => s.tags_seen,
    detail: "every tag with a packet within the stale threshold — mapped and unmapped both count",
  },
  {
    key: "moving",
    label: "Tags moving",
    type: "Derived",
    tone: "ok",
    value: (s) => s.moving,
    detail: "movement-trend bucket: moving — mapped and unmapped both count",
  },
  {
    key: "quiet",
    label: "Quiet tags",
    type: "Derived",
    tone: "mut",
    // Matches the click filter exactly (movement_state=quiet) — summing in not_moving here would
    // make this number disagree with what clicking the card actually filters to.
    value: (s) => s.quiet,
    detail: "quiet movement-trend bucket (delta 1-9 in the window)",
  },
  {
    key: "weak_signal",
    label: "Weak signal",
    type: "Derived",
    tone: "warn",
    value: (s) => s.weak_signal,
    detail: "RSSI at or below the weak-signal threshold",
  },
  {
    key: "missing_signal",
    label: "Missing signal",
    type: "Derived",
    tone: "dng",
    value: (s) => s.stale,
    detail: "no packet received within the stale threshold",
  },
  {
    key: "low_battery",
    label: "Low battery",
    type: "Derived",
    tone: "warn",
    value: (s) => s.low_battery,
    detail: "battery voltage below the low-battery threshold",
  },
];

// The tenant-scoped summary drives every KPI number here — never a count of the fetched page's
// rows. "Tags seen" duplicates the first slot deliberately (Section 8, insight #1) but keeps a
// distinct filter key so a click on it does not collide with "Animals moving".
//
// Clicking a card is a filter change, so it goes through the SAME shared transition as the filter
// bar (useHerdSignalsNav) rather than a plain <Link> navigation — a plain Link here was the
// "clicking a KPI reloads the whole page" defect: no pending affordance, table just blanked and
// reappeared.
export function HerdSignalsKpis({ summary, params }: { summary: HerdSignalsSummary; params: HerdSignalsParams }) {
  const { isPending, navigate } = useHerdSignalsNav();
  return (
    <div className={`kpis herd-signals-kpis${isPending ? " wfbusy" : ""}`} aria-busy={isPending}>
      {KPI_DEFS.map((def, index) => {
        const filterKey = index === 0 ? undefined : def.key;
        const active = filterKey ? params.kpi === filterKey : false;
        const href = filterKey
          ? herdSignalsHref(params, { hs_kpi: active ? undefined : filterKey })
          : undefined;
        const card = (
          <div className={`kpi k-${def.tone}${active ? " active" : ""}${filterKey ? " kpi-clickable" : ""}`}>
            <div className="lab">{def.label}</div>
            <div className="val">{def.value(summary).toLocaleString("en-IN")}</div>
            <div className="dl">{def.detail}</div>
            <div className="ty">{def.type}</div>
          </div>
        );
        if (!href) return <div key={def.label}>{card}</div>;
        return (
          <Link
            key={def.label}
            href={href}
            aria-pressed={active}
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
