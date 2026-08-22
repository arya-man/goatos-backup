"use client";

import { useCallback, useEffect, useRef, useSyncExternalStore, useTransition } from "react";
import { useRouter } from "next/navigation";
import { fmtClockSeconds } from "./format";

// LIVE / "LIVE - Ns stale" / PAUSED / "PAUSED - tab hidden" control, the interval picker, and the
// refresh loop. Ported from features/vaccination-live-tracker/live-poller.tsx with two differences
// required by docs/modules/herd-signals.md Section 7: the default interval is 5s (not 10s) and the
// option set is fixed at 5s/15s/60s (not a backend-driven option group) since this module owns its
// own polling contract end to end.
//
// The refresh is router.refresh(): it re-runs the same server component tree that produced the
// page, so there is exactly one renderer and one data path for the live table + KPIs.

const STORAGE_KEY = "mesha.herd-signals.interval";
const LIVE_STORAGE_KEY = "mesha.herd-signals.live";
const DEFAULT_INTERVAL_SECONDS = 5;
const INTERVAL_OPTIONS = [5, 15, 60];
const STALE_AFTER_MS = 30_000;

const intervalListeners = new Set<() => void>();
function subscribeInterval(onChange: () => void): () => void {
  intervalListeners.add(onChange);
  return () => intervalListeners.delete(onChange);
}
function readInterval(): number {
  try {
    const stored = Number(window.localStorage.getItem(STORAGE_KEY));
    return INTERVAL_OPTIONS.includes(stored) ? stored : DEFAULT_INTERVAL_SECONDS;
  } catch {
    return DEFAULT_INTERVAL_SECONDS;
  }
}
function serverInterval(): number {
  return DEFAULT_INTERVAL_SECONDS;
}
function writeInterval(seconds: number): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, String(seconds));
  } catch {
    // Persistence is a convenience; the chosen interval still applies for this session.
  }
  intervalListeners.forEach((listener) => listener());
}

// PAUSED must survive a remount — the page wraps the board in <Suspense key={JSON.stringify(sp)}>,
// so applying any filter remounts this component. See live-poller.tsx for the full rationale.
const liveListeners = new Set<() => void>();
function subscribeLive(onChange: () => void): () => void {
  liveListeners.add(onChange);
  return () => liveListeners.delete(onChange);
}
function readLive(): boolean {
  try {
    return window.localStorage.getItem(LIVE_STORAGE_KEY) !== "paused";
  } catch {
    return true;
  }
}
function serverLive(): boolean {
  return true;
}
function writeLive(live: boolean): void {
  try {
    window.localStorage.setItem(LIVE_STORAGE_KEY, live ? "live" : "paused");
  } catch {
    // Persistence is a convenience; the chosen state still applies for this mount.
  }
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
// subscribed side-effect (the interval tick, i.e. the actual external mutation), and have
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
  const intervalSeconds = useSyncExternalStore(subscribeInterval, readInterval, serverInterval);
  const tabHidden = useTabHidden();
  const nowMs = useNowMs();
  const pendingRef = useRef(false);

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
  const overlayOpen = useCallback(() => {
    const url = new URL(window.location.href);
    const hashParams = new URLSearchParams(url.hash.replace(/^#/, ""));
    return Boolean(
      (hashParams.get("hs_tag") ?? url.searchParams.get("hs_tag")) ||
        (hashParams.get("hs_history") ?? url.searchParams.get("hs_history")),
    );
  }, []);

  useEffect(() => {
    if (!live) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState !== "visible") return;
      if (overlayOpen()) return;
      refresh();
    }, intervalSeconds * 1000);
    return () => window.clearInterval(timer);
  }, [live, intervalSeconds, refresh, overlayOpen]);

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
        title={live ? "Pause polling" : "Resume polling"}
        aria-pressed={live}
      >
        <span className="livedot" aria-hidden="true" />
        {badgeText}
      </button>
      <div className="refreshmeta">
        Updated <b>{fmtClockSeconds(generatedAt)}</b> IST
      </div>
      <div className="intervalpick" role="group" aria-label="Poll interval">
        {INTERVAL_OPTIONS.map((seconds) => (
          <button
            key={seconds}
            type="button"
            className={seconds === intervalSeconds ? "on" : undefined}
            onClick={() => writeInterval(seconds)}
            aria-pressed={seconds === intervalSeconds}
          >
            {seconds}s
          </button>
        ))}
      </div>
      <button type="button" className="btn" onClick={refresh} disabled={isPending}>
        Refresh
      </button>
      <button type="button" className="btn" disabled title="Export endpoint not built yet">
        Export
      </button>
    </div>
  );
}
