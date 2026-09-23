"use client";

import { useCallback, useEffect, useRef, useState, useSyncExternalStore, useTransition } from "react";
import { useRouter } from "next/navigation";
import { fmtClockSeconds } from "./format";

// LIVE / PAUSED control and the SSE refresh bridge. The initial table remains server-rendered; once
// mounted, EventSource snapshots from /api/herd-signals/live/stream trigger router.refresh() so the
// same data path paints the page without a blind browser polling interval.

const STALE_AFTER_MS = 30_000;
const STREAM_REFRESH_MIN_MS = 15_000;

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
// mounted under this hook (the poller AND the tag drawer, both call it) failed the same way.
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

export function HerdSignalsPoller({ generatedAt }: { generatedAt: string }) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  const live = useSyncExternalStore(subscribeLive, readLive, serverLive);
  const tabHidden = useTabHidden();
  const nowMs = useNowMs();
  const pendingRef = useRef(false);
  const lastStreamRefreshAtRef = useRef(0);
  const [streamState, setStreamState] = useState<"connecting" | "open" | "error">("connecting");

  useEffect(() => {
    pendingRef.current = isPending;
  }, [isPending]);

  const refresh = useCallback(() => {
    if (pendingRef.current) return;
    pendingRef.current = true;
    startTransition(() => {
      router.refresh();
    });
  }, [router]);

  // A tag-detail drawer or the full-screen history view open over the board must not be fought by a
  // refresh: both are client-local overlays (never a route navigation, per
  // make admin-web-local-overlay-guard), and their identity lives in the URL hash.
  //
  // Check BOTH the URL parameters AND the history state: the URL params might be transient or lost
  // during router.refresh(), but the history.state LOCAL_OVERLAY_HISTORY_KEY flag survives and is
  // the authoritative marker that an overlay is currently open and being managed by the client.
  const overlayOpen = useCallback(() => {
    // Check history state first -- this is the most reliable indicator that an overlay is open
    const state = window.history.state;
    if (state && typeof state === "object" && state["__meshaLocalOverlay"]) {
      return true;
    }
    // Fall back to URL check for cases where state isn't reliable
    const url = new URL(window.location.href);
    const hashParams = new URLSearchParams(url.hash.replace(/^#/, ""));
    const hs_tag = hashParams.get("hs_tag") ?? url.searchParams.get("hs_tag");
    const hs_history = hashParams.get("hs_history") ?? url.searchParams.get("hs_history");
    return Boolean(hs_tag || hs_history);
  }, []);

  function streamHref(): string {
    const sp = new URLSearchParams(window.location.search);
    const out = new URLSearchParams();
    const map: Record<string, string> = {
      park: "park_id",
      hs_shed: "shed_id",
      hs_move: "movement_state",
      hs_map: "mapping_state",
      hs_pattern: "pattern",
      hs_q: "q",
      hs_cursor: "cursor",
      hs_sort: "sort",
      hs_dir: "dir",
      hs_limit: "limit",
    };
    for (const [from, to] of Object.entries(map)) {
      const value = sp.get(from);
      if (value) out.set(to, value);
    }
    return `/api/herd-signals/live/stream${out.toString() ? `?${out.toString()}` : ""}`;
  }

  useEffect(() => {
    if (!live || tabHidden) return;
    setStreamState("connecting");
    const source = new EventSource(streamHref());
    let first = true;
    source.onopen = () => setStreamState("open");
    source.onerror = () => setStreamState("error");
    source.addEventListener("tick", () => {
      setStreamState("open");
      if (document.visibilityState !== "visible") return;
      if (overlayOpen()) return;
      if (first) {
        first = false;
        return;
      }
      const now = Date.now();
      if (now - lastStreamRefreshAtRef.current < STREAM_REFRESH_MIN_MS) return;
      lastStreamRefreshAtRef.current = now;
      refresh();
    });
    return () => source.close();
  }, [live, tabHidden, refresh, overlayOpen]);

  useEffect(() => {
    function onVisibility() {
      if (document.visibilityState === "visible" && live && !overlayOpen()) refresh();
    }
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, [live, refresh, overlayOpen]);

  function toggleLive() {
    writeLive(!live);
    if (live) return;
    refresh();
  }

  const [exportHref, setExportHref] = useState("/api/herd-signals/export.csv");
  useEffect(() => {
    const sync = () => {
      const sp = new URLSearchParams(window.location.search);
      const out = new URLSearchParams();
      const map: Record<string, string> = { park: "park_id", hs_shed: "shed_id", hs_move: "movement_state", hs_map: "mapping_state", hs_pattern: "pattern", hs_q: "q" };
      for (const [from, to] of Object.entries(map)) { const v = sp.get(from); if (v) out.set(to, v); }
      setExportHref(`/api/herd-signals/export.csv${out.toString() ? `?${out.toString()}` : ""}`);
    };
    sync();
    window.addEventListener("popstate", sync);
    return () => window.removeEventListener("popstate", sync);
  }, []);

  const ageMs = nowMs - new Date(generatedAt).getTime();
  const stale = live && !tabHidden && Number.isFinite(ageMs) && ageMs > STALE_AFTER_MS;
  const staleSeconds = Math.max(0, Math.round(ageMs / 1000));

  let badgeClass = "livebadge";
  let badgeText = "LIVE";
  if (!live) {
    badgeClass = "livebadge paused";
    badgeText = tabHidden ? "PAUSED · tab hidden" : "PAUSED";
  } else if (tabHidden) {
    badgeClass = "livebadge paused";
    badgeText = "PAUSED · tab hidden";
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
        Updated <b>{fmtClockSeconds(generatedAt)}</b> IST · stream {streamState}
      </div>
      <button type="button" className="btn" onClick={refresh} disabled={isPending}>
        Refresh
      </button>
      <a
        className="btn"
        href={exportHref}
        title="Download the current filtered view as CSV"
        // The file must match what is on screen, so the active filters ride along. Not a
        // LocalOverlayLink: this is a real download, not an in-page overlay.
      >
        Export
      </a>
    </div>
  );
}
