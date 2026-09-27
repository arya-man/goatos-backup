// The Live Monitor KPI cards: one definition list, read by the deck (herd-signals-kpis.tsx) AND its
// loading twin (herd-signals-skeletons.tsx), so the skeleton's card count cannot drift. No "use client":
// the values are plain functions and the icons plain elements, safe on either side.
import type { ReactNode } from "react";
import { Activity, BatteryLow, Radio, TriangleAlert, Wifi } from "lucide-react";
import type { KitTone } from "@/lib/tone";
import type { HerdSignalsSummary } from "@/lib/api/herd-signals";
import type { KpiFilterKey } from "./params";

export type KpiDef = {
  key: KpiFilterKey;
  label: string;
  type: "Direct" | "Derived";
  tone: KitTone;
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
  radio: <Radio />,
  activity: <Activity />,
  wifi: <Wifi />,
  alert: <TriangleAlert />,
  battery: <BatteryLow />,
};

export const KPI_DEFS: KpiDef[] = [
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
    key: "moving_now",
    label: "Moving now",
    type: "Derived",
    tone: "success",
    icon: IC.activity,
    value: (s) => s.moving_now,
    detail: () => "fresh packet with movement in last packet/30 sec",
  },
  {
    key: "active_1m",
    label: "Active 1m",
    type: "Derived",
    tone: "primary",
    icon: IC.activity,
    value: (s) => s.active_1m,
    detail: () => "motion-count delta above 0 in last 1 min",
  },
  {
    key: "moving_15m",
    label: "Moving last 15m",
    type: "Derived",
    tone: "info",
    icon: IC.activity,
    value: (s) => s.moving,
    detail: () => "sustained activity: delta ≥ 100 in last 15 min",
  },
  {
    key: "quiet",
    label: "Quiet tags",
    type: "Derived",
    tone: "neutral",
    icon: IC.activity,
    // Matches the click filter exactly (movement_state=quiet) — summing in not_moving here would
    // make this number disagree with what clicking the card actually filters to.
    value: (s) => s.quiet,
    detail: () => "motion-count delta 1 to 9 in last 15 min",
  },
  {
    key: "weak_signal",
    label: "Weak signal",
    type: "Derived",
    tone: "warning",
    icon: IC.wifi,
    value: (s) => s.weak_signal,
    detail: () => "RSSI ≤ -75 dBm",
  },
  {
    key: "missing_signal",
    label: "Missing signal",
    type: "Derived",
    tone: "error",
    icon: IC.alert,
    value: (s) => s.stale,
    detail: () => "not seen for 30+ min — signal, not animal",
  },
  {
    key: "low_battery",
    label: "Low battery",
    type: "Derived",
    tone: "violet",
    icon: IC.battery,
    value: (s) => s.low_battery,
    // Deliberately no "est. ~N left" / life estimate here — that was removed because no vendor
    // discharge curve exists (docs/modules/herd-signals.md), and it must stay removed even though
    // an older mock revision showed one. voltage threshold only, exactly what the current mock has.
    detail: () => "voltage below 2800 mV",
  },
];
