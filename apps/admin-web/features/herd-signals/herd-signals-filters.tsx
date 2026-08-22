"use client";

import { useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { MOVEMENT_LABEL, MAPPING_LABEL, PATTERN_LABEL } from "./format";
import { herdSignalsHref, herdSignalsResetHref, type HerdSignalsParams } from "./params";

export type ShedOption = { id: string; label: string };

const MOVEMENT_OPTIONS = Object.entries(MOVEMENT_LABEL) as [keyof typeof MOVEMENT_LABEL, string][];
const MAPPING_OPTIONS = Object.entries(MAPPING_LABEL) as [keyof typeof MAPPING_LABEL, string][];
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
  const router = useRouter();
  const [, startTransition] = useTransition();
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
    startTransition(() => {
      router.push(href, { scroll: false });
    });
  }

  function onSearchChange(value: string) {
    setQ(value);
    if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
    debounceRef.current = window.setTimeout(() => {
      go(herdSignalsHref(params, { hs_q: value.trim() || undefined }));
    }, 300);
  }

  const hasNarrowing = params.hasFilter || Boolean(params.parkId);

  return (
    <div className="fbar herd-signals-fbar">
      <span className="fsel search has">
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

      {hasNarrowing ? (
        <a
          href={herdSignalsResetHref(params)}
          className="achip clr"
          onClick={(event) => {
            event.preventDefault();
            go(herdSignalsResetHref(params));
          }}
        >
          Clear filters
        </a>
      ) : null}

      <span className="fnote">Thresholds provisional — see Herd Signals module docs</span>
    </div>
  );
}
