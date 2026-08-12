"use client";

import { useCallback, useEffect, useRef, useState, useSyncExternalStore, useTransition } from "react";
import { useRouter } from "next/navigation";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtClock, fmtClockSeconds } from "./format";

const STORAGE_KEY = "mesha.live-tracker.interval";
const DEFAULT_INTERVAL_SECONDS = 10;

// The chosen refresh interval is browser state, not URL state: changing it must not push a history
// entry, because a navigation is not a refresh and would reset the board's scroll position every
// time someone switched from 10s to 1m.
//
// It is exposed as an external store rather than as component state seeded from an effect, so the
// server render and the first client render agree on the default and the stored value is adopted
// without a second render pass.
const intervalListeners = new Set<() => void>();

function subscribeInterval(onChange: () => void): () => void {
  intervalListeners.add(onChange);
  return () => {
    intervalListeners.delete(onChange);
  };
}

function readInterval(): number {
  try {
    const stored = Number(window.localStorage.getItem(STORAGE_KEY));
    return stored > 0 ? stored : DEFAULT_INTERVAL_SECONDS;
  } catch {
    // A blocked storage API is not a reason to break the page; the default interval stands.
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

// LIVE / PAUSED control, the "Updated HH:MM:SS IST" readout, and the refresh loop.
//
// The refresh is router.refresh(): it re-runs the SAME server component tree that rendered the page,
// so there is exactly one renderer and one data path. Fetching this page's JSON from the browser
// instead would mean maintaining a second copy of every table in client markup, and this app has no
// sanctioned client data-fetching layer to build that on.
export function LivePoller({
  generatedAt,
  pageContract,
}: {
  generatedAt: string;
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  const [live, setLive] = useState(true);
  const [pausedAt, setPausedAt] = useState<string | null>(null);
  const intervalSeconds = useSyncExternalStore(subscribeInterval, readInterval, serverInterval);
  const pendingRef = useRef(false);

  useEffect(() => {
    pendingRef.current = isPending;
  }, [isPending]);

  const intervals = optionGroup(pageContract, "live_refresh_interval");

  const refresh = useCallback(() => {
    // Backpressure: a refresh already in flight is never stacked behind another. On a slow read at a
    // 10s interval that would otherwise queue requests faster than the server answers them.
    if (pendingRef.current) return;
    pendingRef.current = true;
    startTransition(() => {
      router.refresh();
    });
  }, [router]);

  // A Goat Passport drawer open over the board must not be fought by a refresh: re-rendering the
  // tree underneath an overlay re-mounts it mid-read. The selection lives in the URL, so reading it
  // there is both exact and free of any coupling to the drawer component.
  const passportOpen = useCallback(() => {
    const url = new URL(window.location.href);
    const hashParams = new URLSearchParams(url.hash.replace(/^#/, ""));
    return Boolean(hashParams.get("goat_passport") ?? url.searchParams.get("goat_passport"));
  }, []);

  useEffect(() => {
    if (!live) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState !== "visible") return;
      if (passportOpen()) return;
      refresh();
    }, intervalSeconds * 1000);
    return () => window.clearInterval(timer);
  }, [live, intervalSeconds, refresh, passportOpen]);

  useEffect(() => {
    function onVisibility() {
      if (document.visibilityState === "visible" && live && !passportOpen()) refresh();
    }
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, [live, refresh, passportOpen]);

  function toggleLive() {
    if (live) {
      setPausedAt(generatedAt);
      setLive(false);
      return;
    }
    setPausedAt(null);
    setLive(true);
    refresh();
  }

  return (
    <div className="lt-livebar">
      <button
        type="button"
        className={`lt-livebadge${live ? "" : " paused"}`}
        onClick={toggleLive}
        title={copy(pageContract, "live.toggle_title")}
        aria-pressed={live}
      >
        <span className="lt-livedot" aria-hidden="true" />
        {live ? copy(pageContract, "live.badge_live") : copy(pageContract, "live.badge_paused")}
      </button>
      <div className="lt-refreshmeta">
        {copy(pageContract, "live.updated_prefix")} <b>{fmtClockSeconds(generatedAt)}</b>{" "}
        {copy(pageContract, "live.updated_suffix")}
      </div>
      <div className="lt-intervalpick" role="group" aria-label={copy(pageContract, "live.interval_label")}>
        {intervals.map((option) => {
          const seconds = Number(option.key);
          return (
            <button
              key={option.key}
              type="button"
              className={seconds === intervalSeconds ? "on" : undefined}
              onClick={() => writeInterval(seconds)}
              aria-pressed={seconds === intervalSeconds}
              title={option.title || undefined}
            >
              {option.label}
            </button>
          );
        })}
      </div>
      {!live && pausedAt ? (
        <span className="lt-stale" role="status">
          {copy(pageContract, "live.stale_prefix")} <b>{fmtClock(pausedAt)}</b>{" "}
          {copy(pageContract, "live.stale_suffix")}
        </span>
      ) : null}
    </div>
  );
}
