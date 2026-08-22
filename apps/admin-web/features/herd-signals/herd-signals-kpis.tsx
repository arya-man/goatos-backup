import Link from "@/components/no-prefetch-link";
import type { HerdSignalsSummary } from "@/lib/api/herd-signals";
import { herdSignalsHref, type HerdSignalsParams, type KpiFilterKey } from "./params";

type KpiDef = {
  key: KpiFilterKey;
  label: string;
  type: "Direct" | "Derived";
  tone: "mut" | "ok" | "warn" | "dng";
  value: (summary: HerdSignalsSummary) => number;
  detail: string;
};

const KPI_DEFS: KpiDef[] = [
  {
    key: "moving" as KpiFilterKey,
    label: "Tags seen",
    type: "Direct",
    tone: "mut",
    value: (s) => s.tags_seen,
    detail: "distinct mapped tags with a recent packet",
  },
  {
    key: "moving",
    label: "Animals moving",
    type: "Derived",
    tone: "ok",
    value: (s) => s.moving,
    detail: "movement-trend bucket: moving",
  },
  {
    key: "quiet",
    label: "Quiet animals",
    type: "Derived",
    tone: "mut",
    value: (s) => s.quiet + s.not_moving,
    detail: "low or no motion-count delta in the current window",
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
export function HerdSignalsKpis({ summary, params }: { summary: HerdSignalsSummary; params: HerdSignalsParams }) {
  return (
    <div className="kpis herd-signals-kpis">
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
          <Link key={def.label} href={href} aria-pressed={active}>
            {card}
          </Link>
        );
      })}
    </div>
  );
}

export function HerdSignalsKpiChip({ params }: { params: HerdSignalsParams }) {
  if (!params.kpi) return null;
  const def = KPI_DEFS.find((item) => item.key === params.kpi);
  if (!def) return null;
  return (
    <Link href={herdSignalsHref(params, { hs_kpi: undefined })} className="achip" title="Clear this KPI filter">
      {def.label} <b>✕</b>
    </Link>
  );
}
