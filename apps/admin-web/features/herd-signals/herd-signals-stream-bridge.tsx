"use client";

import { useCallback, useMemo, useEffect, useState, useSyncExternalStore } from "react";
import { useSearchParams } from "next/navigation";
import type { HerdSignalsLiveResponse } from "@/lib/api/herd-signals";
import { fmtClockSeconds } from "./format";
import { useHerdSignalsLiveSnapshot, writeHerdSignalsLiveSnapshot } from "./herd-signals-live-store";

// LIVE / PAUSED control and the SSE connection bridge. The initial table remains server-rendered;
// stream ticks update connection/freshness state only. Route refreshes here would be polling
// disguised as SSE and cause the full-page flicker operators reported.

const STALE_AFTER_MS = 30_000;
const DEFAULT_SORT = "smart_tag";
const DEFAULT_SORT_DIR = "asc";
const DEFAULT_LIMIT = "25";

function liveStateFromKpi(kpi: string | null): "moving_now" | "active_1m" | null {
  return kpi === "moving_now" || kpi === "active_1m" ? kpi : null;
}

function movementStateFromKpi(kpi: string | null): "moving" | "quiet" | null {
  if (kpi === "moving_15m") return "moving";
  if (kpi === "quiet") return "quiet";
  return null;
}

function currentLiveQuery(search: string): URLSearchParams {
	const sp = new URLSearchParams(search);
	const tab = sp.get("hs_tab") || "live";
	const out = new URLSearchParams();
	const parkId = sp.get("park");
	const shedId = sp.get("hs_shed");
	const q = sp.get("hs_q");
	const cursor = sp.get("hs_cursor");
	const sort = sp.get("hs_sort") || DEFAULT_SORT;
	const dir = sp.get("hs_dir") || DEFAULT_SORT_DIR;
	const limit = sp.get("hs_limit") || DEFAULT_LIMIT;
	let movementState = sp.get("hs_move");
	let liveState: string | null = null;
	let mappingState = sp.get("hs_map");
	let pattern = sp.get("hs_pattern");
	let riskState = sp.get("hs_risk");
	if (tab === "live") {
		const kpi = sp.get("hs_kpi");
		liveState = liveStateFromKpi(kpi);
		movementState = liveState ? null : (movementStateFromKpi(kpi) ?? movementState);
	} else if (tab === "animals") {
		liveState = null;
		mappingState = "mapped";
	} else if (tab === "mapping") {
		liveState = null;
		movementState = null;
		pattern = null;
		riskState = null;
	} else if (tab === "alerts") {
		liveState = null;
		movementState = null;
		mappingState = null;
		pattern = null;
		riskState = "attention";
	} else if (tab !== "mapping" && tab !== "live") {
		liveState = null;
		mappingState = null;
	}
	if (parkId) out.set("park_id", parkId);
	if (shedId) out.set("shed_id", shedId);
	if (q) out.set("q", q);
	if (cursor) out.set("cursor", cursor);
	out.set("sort", sort);
	out.set("dir", dir);
	out.set("limit", limit);
	if (movementState) out.set("movement_state", movementState);
	if (liveState) out.set("live_state", liveState);
	if (mappingState) out.set("mapping_state", mappingState);
	if (pattern) out.set("pattern", pattern);
	if (riskState) out.set("risk_state", riskState);
	return out;
}

// Live/paused state is session-only — no localStorage persistence. Fresh page loads are always
// live. Users can pause within the session using the toggle button, but the pause state is lost
// on reload (no sticky pause across page reloads). Tab-hidden automatic pause is still an efficiency
// measure but is not presented as the data's true state (the badge says "PAUSED · tab hidden").
const liveListeners = new Set<() => void>();
let liveState = true;
function subscribeLive(onChange: () => void): () => void {
  liveListeners.add(onChange);
  return () => liveListeners.delete(onChange);
}
function readLive(): boolean {
  return liveState;
}
function serverLive(): boolean {
  return true;
}
function writeLive(nextLive: boolean): void {
  // No localStorage persistence — state is session-only.
  liveState = nextLive;
  liveListeners.forEach((listener) => listener());
}

function useTabHidden(): boolean {
  const subscribe = useCallback((onChange: () => void) => {
    document.addEventListener("visibilitychange", onChange);
    return () => document.removeEventListener("visibilitychange", onChange);
  }, []);
  const getSnapshot = useCallback(() => document.visibilityState !== "visible", []);
  const getServerSnapshot = useCallback(() => false, []);
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

// The current wall-clock time, read through useSyncExternalStore.
//
// getSnapshot must return the SAME value across repeated calls until the external store actually
// changes -- React calls it more than once per commit to detect tearing, and if two calls in the
// same pass disagree (as `Date.now()` called directly here would, since real time moves between
// those two calls), React concludes the store changed mid-render and schedules another render,
// which calls getSnapshot again, disagrees again, and so on: "Maximum update depth exceeded" from
// inside useSyncExternalStore's own re-render machinery. This crashed the drawer on its very first
// tag click, before the timeline fetch or its from/to params ever mattered — every render tree
// mounted under this hook (the stream bridge AND the tag drawer, both call it) failed the same way.
//
// The fix is the standard one: cache the value in a module-level box that is written ONLY by the
// subscribed side-effect (the timer tick, i.e. the actual external mutation), and have
// getSnapshot do nothing but read that box. Between ticks, repeated getSnapshot calls return the
// identical cached number.
let cachedNowMs = 0;
const nowMsListeners = new Set<() => void>();

function tickNowMs(): void {
  cachedNowMs = Date.now();
  nowMsListeners.forEach((listener) => listener());
}

export function useNowMs(everyMs = 1000): number {
  const subscribe = useCallback(
    (onChange: () => void) => {
      // Prime the cache synchronously on subscribe (React calls subscribe before the next render),
      // so the first client render already reflects "now" instead of waiting a full tick.
      tickNowMs();
      nowMsListeners.add(onChange);
      const timer = window.setInterval(tickNowMs, everyMs);
      return () => {
        nowMsListeners.delete(onChange);
        window.clearInterval(timer);
      };
    },
    [everyMs],
  );
  const getSnapshot = useCallback(() => cachedNowMs, []);
  const getServerSnapshot = useCallback(() => 0, []);
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

export function HerdSignalsStreamBridge({ generatedAt }: { generatedAt: string }) {
  const searchParams = useSearchParams();
  const searchKey = searchParams.toString();
  const liveQuery = useMemo(() => currentLiveQuery(searchKey), [searchKey]);
  const liveKey = liveQuery.toString();
  const residualKpi = searchParams.get("hs_kpi");
  const tab = searchParams.get("hs_tab") || "live";
  const streamConsumesLiveSnapshot = tab === "live" || tab === "animals";
  const unsupportedExportTab = tab === "gateways" || tab === "insights";
  const pageOnlyKpiExportDisabled = tab === "live" && (residualKpi === "weak_signal" || residualKpi === "missing_signal" || residualKpi === "low_battery");
  const exportDisabled = unsupportedExportTab || pageOnlyKpiExportDisabled;
  const exportHref = useMemo(() => {
    const out = new URLSearchParams(liveQuery);
    out.delete("cursor");
    out.delete("limit");
    return `/api/herd-signals/export.csv${out.toString() ? `?${out.toString()}` : ""}`;
  }, [liveQuery]);
  const live = useSyncExternalStore(subscribeLive, readLive, serverLive);
  const liveSnapshot = useHerdSignalsLiveSnapshot(liveKey);
  const tabHidden = useTabHidden();
  const nowMs = useNowMs();
  const [streamState, setStreamState] = useState<"connecting" | "open" | "error" | "snapshot_error">("connecting");
  const [lastStreamEventAtMs, setLastStreamEventAtMs] = useState(() => new Date(generatedAt).getTime());

  function streamHref(): string {
    const out = new URLSearchParams(liveQuery);
    return `/api/herd-signals/live/stream${out.toString() ? `?${out.toString()}` : ""}`;
  }

  useEffect(() => {
    if (!live || tabHidden || !streamConsumesLiveSnapshot) return;
    setStreamState("connecting");
    setLastStreamEventAtMs(Date.now());
    const source = new EventSource(streamHref());
    source.onopen = () => {
      setLastStreamEventAtMs(Date.now());
      setStreamState("open");
    };
    source.onerror = () => setStreamState("error");
    source.addEventListener("snapshot", (event) => {
      setLastStreamEventAtMs(Date.now());
      setStreamState("open");
      try {
        writeHerdSignalsLiveSnapshot(liveKey, JSON.parse(event.data) as HerdSignalsLiveResponse);
      } catch {
        setStreamState("error");
      }
    });
    source.addEventListener("snapshot_error", () => setStreamState("snapshot_error"));
    source.addEventListener("tick", () => {
      setLastStreamEventAtMs(Date.now());
      setStreamState((current) => (current === "snapshot_error" ? current : "open"));
    });
    return () => source.close();
  }, [live, tabHidden, streamConsumesLiveSnapshot, liveKey, liveQuery]);

  function toggleLive() {
    writeLive(!live);
  }

  const updatedAtMs = liveSnapshot?.receivedAt ?? new Date(generatedAt).getTime();
  const ageMs = nowMs - lastStreamEventAtMs;
  const stale = live && streamConsumesLiveSnapshot && !tabHidden && Number.isFinite(ageMs) && ageMs > STALE_AFTER_MS;
  const staleSeconds = Math.max(0, Math.round(ageMs / 1000));

  let badgeClass = "livebadge";
  let badgeText = "LIVE";
  if (!live) {
    badgeClass = "livebadge paused";
    badgeText = tabHidden ? "PAUSED · tab hidden" : "PAUSED";
  } else if (tabHidden) {
    badgeClass = "livebadge paused";
    badgeText = "PAUSED · tab hidden";
  } else if (!streamConsumesLiveSnapshot) {
    badgeClass = "livebadge paused";
    badgeText = "LIVE · not used on this tab";
  } else if (streamState === "snapshot_error") {
    badgeClass = "livebadge stale";
    badgeText = "LIVE · snapshot error";
  } else if (stale) {
    badgeClass = "livebadge stale";
    badgeText = `LIVE · ${staleSeconds}s stale`;
  }

  return (
    <div className="herd-signals-livebar">
      <button
        type="button"
        className={badgeClass}
        onClick={toggleLive}
        title={live ? "Pause live stream" : "Resume live stream"}
        aria-pressed={live}
      >
        <span className="livedot" aria-hidden="true" />
        {badgeText}
      </button>
      <div className="refreshmeta">
        Updated <b>{fmtClockSeconds(new Date(updatedAtMs).toISOString())}</b> IST · stream {streamState}
      </div>
      {exportDisabled ? (
        <button type="button" className="btn" disabled title={unsupportedExportTab ? "Export is available on table tabs" : "Clear this page-only KPI filter before exporting"}>
          Export
        </button>
      ) : (
        <a
          className="btn"
          href={exportHref}
          title="Download the current filtered view as CSV"
          // The file must match what is on screen, so the active filters ride along. Not a
          // LocalOverlayLink: this is a real download, not an in-page overlay.
        >
          Export
        </a>
      )}
    </div>
  );
}
