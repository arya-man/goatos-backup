"use client";

import { useRef, useState } from "react";
import { MOVEMENT_LABEL, MAPPING_LABEL, PATTERN_LABEL } from "./format";
import { useHerdSignalsNav } from "./herd-signals-nav-context";
import { HerdSignalsKpiChip } from "./herd-signals-kpis";
import { herdSignalsHref, type HerdSignalsParams } from "./params";

export type ShedOption = { id: string; label: string };

const MOVEMENT_OPTIONS = Object.entries(MOVEMENT_LABEL) as [keyof typeof MOVEMENT_LABEL, string][];
// Filter-dropdown wording is "<state> only" (mock/herd-signals-mock.html #fMap), distinct from the
// bare MAPPING_LABEL used elsewhere (table Status column, mapping-table quick-filter buttons).
const MAPPING_FILTER_LABEL: Record<keyof typeof MAPPING_LABEL, string> = {
  mapped: "Mapped only",
  unmapped: "Unmapped only",
  conflict: "Conflicts only",
};
const MAPPING_OPTIONS = Object.entries(MAPPING_FILTER_LABEL) as [keyof typeof MAPPING_LABEL, string][];
// Only the alert-shaped pattern states are offered here (Section 8) — "normal" is not a useful
// filter choice since it is the majority of every fleet.
const PATTERN_OPTIONS: [string, string][] = (["inactive", "quiet_watch", "spike", "recovered", "missing"] as const).map(
  (key) => [key, PATTERN_LABEL[key]],
);

export function HerdSignalsFilters({
  params,
  sheds,
}: {
  params: HerdSignalsParams;
  sheds: ShedOption[];
}) {
  const { isPending, navigate } = useHerdSignalsNav();
  // Synced from the URL's own hs_q on every navigation, using React's "adjust state during render"
  // pattern (react.dev/learn/you-might-not-need-an-effect) rather than an Effect that calls
  // setState synchronously — the debounced local edits below still take priority between renders.
  const [syncedFromProp, setSyncedFromProp] = useState(params.q);
  const [q, setQ] = useState(params.q ?? "");
  if (params.q !== syncedFromProp) {
    setSyncedFromProp(params.q);
    setQ(params.q ?? "");
  }
  const debounceRef = useRef<number | null>(null);

  function go(href: string) {
    navigate(href);
  }

  function onSearchChange(value: string) {
    setQ(value);
    if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
    debounceRef.current = window.setTimeout(() => {
      go(herdSignalsHref(params, { hs_q: value.trim() || undefined }));
    }, 300);
  }

  return (
    <div className="fbar herd-signals-fbar" aria-busy={isPending}>
      {isPending ? <span className="wfspin" aria-hidden="true" title="Applying filter" /> : null}
      <span className={`fsel search${q ? " has" : ""}`}>
        <svg className="ic sm" viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="11" cy="11" r="7" />
          <path d="m20 20-3.5-3.5" />
        </svg>
        <input
          type="search"
          placeholder="Search animal, tag ID, BLE MAC, shed or gateway"
          value={q}
          onChange={(event) => onSearchChange(event.target.value)}
          autoComplete="off"
        />
        {q ? (
          <button
            type="button"
            className="qclr"
            title="Clear search"
            onClick={() => {
              setQ("");
              go(herdSignalsHref(params, { hs_q: undefined }));
            }}
          >
            ✕
          </button>
        ) : null}
      </span>

      {/* Park scope is owned by the shell top bar (lib/scope.ts), not this page's own filter bar —
          see check-ia-guard.mjs: command/authority screens must not repeat park scope inline. The
          top bar writes the same `park` query param herdSignalsHref/parseHerdSignalsParams already
          read, so scoping by park still filters this page's table and KPI aggregates exactly as
          before; only the duplicate in-page control is gone. */}
      <span className="fsel">
        Shed
        <select
          aria-label="Shed"
          value={params.shedId ?? ""}
          onChange={(event) => go(herdSignalsHref(params, { hs_shed: event.target.value || undefined }))}
        >
          <option value="">All sheds</option>
          {sheds.map((shed) => (
            <option key={shed.id} value={shed.id}>
              {shed.label}
            </option>
          ))}
        </select>
      </span>

      <span className="fsel">
        Movement
        <select
          aria-label="Movement state"
          value={params.movementState ?? ""}
          onChange={(event) => go(herdSignalsHref(params, { hs_move: event.target.value || undefined }))}
        >
          <option value="">Any movement state</option>
          {MOVEMENT_OPTIONS.map(([key, label]) => (
            <option key={key} value={key}>
              {label}
            </option>
          ))}
        </select>
      </span>

      <span className="fsel">
        Mapping
        <select
          aria-label="Mapping state"
          value={params.mappingState ?? ""}
          onChange={(event) => go(herdSignalsHref(params, { hs_map: event.target.value || undefined }))}
        >
          <option value="">Mapped + unmapped</option>
          {MAPPING_OPTIONS.map(([key, label]) => (
            <option key={key} value={key}>
              {label}
            </option>
          ))}
        </select>
      </span>

      <span className="fsel">
        Pattern
        <select
          aria-label="Pattern"
          value={params.pattern ?? ""}
          onChange={(event) => go(herdSignalsHref(params, { hs_pattern: event.target.value || undefined }))}
        >
          <option value="">Any pattern</option>
          {PATTERN_OPTIONS.map(([key, label]) => (
            <option key={key} value={key}>
              {label}
            </option>
          ))}
        </select>
      </span>

      {/* mock/herd-signals-mock.html `#kpiChip` — the ONLY chip the fbar carries is the active KPI
          filter, one per active KPI, independently removable via its own ✕. The mock has no
          generic "Clear filters" control in the filter bar (`clearFilters()` is only wired to the
          empty-state action button) — do not invent one here. */}
      <HerdSignalsKpiChip params={params} />

      <span className="fnote">Window: last 15 min &middot; thresholds provisional</span>
    </div>
  );
}
