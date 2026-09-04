"use client";

import type { ReactNode } from "react";
import Link from "@/components/no-prefetch-link";
import type { HerdSignalsSummary } from "@/lib/api/herd-signals";
import { useHerdSignalsNav } from "./herd-signals-nav-context";
import { herdSignalsHref, type HerdSignalsParams, type KpiFilterKey } from "./params";

type KpiDef = {
  key: KpiFilterKey;
  label: string;
  type: "Direct" | "Derived";
  tone: "mut" | "ok" | "warn" | "dng" | "info" | "purple";
  // mock/herd-signals-mock.html const IC — one glyph per card, in the mock's own path data.
  icon: ReactNode;
  value: (summary: HerdSignalsSummary) => number;
  detail: (summary: HerdSignalsSummary) => string;
};

// Every summary field here is TAG-grain and counts unmapped tags exactly like mapped ones
// (docs/modules/herd-signals.md "Unmapped tags are the NORMAL state" — on staging today NO tag is
// mapped, so a mapped-only count would read all zeros). Labels say "tags," never "animals," for
// that reason. Tiers follow Section 8 AND the mock's own K array (mock/herd-signals-mock.html
// renderKpis): a recency-thresholded count, a movement-trend bucket, a stale/weak/low-voltage
// threshold check are all labelled "Derived" there — every one of the six cards, with no per-card
// split into Direct/Inferred. Do not invent a split the mock itself does not draw.
//
// Known gap, left honest rather than papered over: the "low" movement-trend bucket (Section 5) has
// no dedicated field on HerdSignalsSummary, so it is not counted by any card here. Adding one would
// mean summing it from the fetched page (banned — see herd-signals-row-filter.ts) rather than from
// a real backend aggregate.
const IC = {
  radio: (
    <>
      <path d="M4.9 19.1a10 10 0 0 1 0-14.2" />
      <path d="M7.8 16.2a6 6 0 0 1 0-8.4" />
      <circle cx="12" cy="12" r="2" />
      <path d="M16.2 7.8a6 6 0 0 1 0 8.4" />
      <path d="M19.1 4.9a10 10 0 0 1 0 14.2" />
    </>
  ),
  activity: <path d="M3 12h4l3 8 4-16 3 8h4" />,
  wifi: (
    <>
      <path d="M5 12.5a7 7 0 0 1 14 0" />
      <path d="M2 9a11 11 0 0 1 20 0" />
      <circle cx="12" cy="17" r="2" />
    </>
  ),
  alert: (
    <>
      <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" />
      <path d="M12 9v4" />
      <path d="M12 17h.01" />
    </>
  ),
  battery: (
    <>
      <rect x="2" y="7" width="16" height="10" rx="2" />
      <path d="M22 11v2" />
    </>
  ),
};

function KpiIcon({ children }: { children: ReactNode }) {
  return (
    <svg className="ic sm" viewBox="0 0 24 24" aria-hidden="true">
      {children}
    </svg>
  );
}

const KPI_DEFS: KpiDef[] = [
  {
    key: "moving" as KpiFilterKey,
    label: "Tags seen",
    type: "Derived",
    tone: "info",
    icon: IC.radio,
    value: (s) => s.tags_seen,
    detail: (s) =>
      `${s.mapped_animals.toLocaleString("en-IN")} mapped animals, ${s.unmapped_tags.toLocaleString("en-IN")} unmapped`,
  },
  {
    key: "moving",
    label: "Tags moving",
    type: "Derived",
    tone: "ok",
    icon: IC.activity,
    value: (s) => s.moving,
    detail: () => "motion delta ≥ 100 in last 15 min",
  },
  {
    key: "quiet",
    label: "Quiet tags",
    type: "Derived",
    tone: "mut",
    icon: IC.activity,
    // Matches the click filter exactly (movement_state=quiet) — summing in not_moving here would
    // make this number disagree with what clicking the card actually filters to.
    value: (s) => s.quiet,
    detail: () => "low or zero delta this window",
  },
  {
    key: "weak_signal",
    label: "Weak signal",
    type: "Derived",
    tone: "warn",
    icon: IC.wifi,
    value: (s) => s.weak_signal,
    detail: () => "RSSI ≤ -75 dBm",
  },
  {
    key: "missing_signal",
    label: "Missing signal",
    type: "Derived",
    tone: "dng",
    icon: IC.alert,
    value: (s) => s.stale,
    detail: () => "not seen for 30+ min — signal, not animal",
  },
  {
    key: "low_battery",
    label: "Low battery",
    type: "Derived",
    tone: "purple",
    icon: IC.battery,
    value: (s) => s.low_battery,
    // Deliberately no "est. ~N left" / life estimate here — that was removed because no vendor
    // discharge curve exists (docs/modules/herd-signals.md), and it must stay removed even though
    // an older mock revision showed one. voltage threshold only, exactly what the current mock has.
    detail: () => "voltage below 2800 mV",
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
            <div className="lab">
              <KpiIcon>{def.icon}</KpiIcon>
              {def.label}
            </div>
            <div className="val">{def.value(summary).toLocaleString("en-IN")}</div>
            <div className="dl">{def.detail(summary)}</div>
            <div className="ty">{def.type}</div>
            {/* mock/herd-signals-mock.html renderKpis: `.act` reads "Filtering table ▾ click to
                clear" and only shows on the active tile (`.kpi.active .act{display:block}`, CSS
                text-transform:uppercase renders it as FILTERING TABLE ▾ CLICK TO CLEAR). */}
            <div className="act">Filtering table ▾ click to clear</div>
            <div className="hint">{filterKey ? "Click to filter the table below" : "Shows every tag"}</div>
          </div>
        );
        if (!href) return <div key={def.label}>{card}</div>;
        return (
          <Link
            key={def.label}
            href={href}
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
