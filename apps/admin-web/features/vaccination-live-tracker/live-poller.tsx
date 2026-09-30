"use client";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { TAP_MIN } from "@/components/app/tap";
import { useCallback, useEffect, useRef, useSyncExternalStore, useTransition } from "react";
import { useRouter } from "next/navigation";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtClock } from "./format";

const STORAGE_KEY = "mesha.live-tracker.interval";
const LIVE_STORAGE_KEY = "mesha.live-tracker.live";
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

// PAUSED must survive a remount. The page wraps the board in <Suspense key={JSON.stringify(sp)}>,
// so applying or clearing ANY filter remounts this component — and a board the reader deliberately
// paused to study a row would silently resume polling and refresh out from under them. The refresh
// interval already survived because it lives in storage; the pause state did not.
const liveListeners = new Set<() => void>();

function subscribeLive(onChange: () => void): () => void {
  liveListeners.add(onChange);
  return () => {
    liveListeners.delete(onChange);
  };
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
  const live = useSyncExternalStore(subscribeLive, readLive, serverLive);
  const intervalSeconds = useSyncExternalStore(subscribeInterval, readInterval, serverInterval);
  const pendingRef = useRef(false);

  useEffect(() => {
    pendingRef.current = isPending;
  }, [isPending]);


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
    writeLive(!live);
    if (live) return;
    refresh();
  }

  // TR-2 P1-10 (template App overview): the poller is the AppWelcome's ONE contained action (the
  // template's "Go now" slot): LIVE (pulsing dot) / PAUSED. The updated time is the welcome text and
  // the refresh interval is a select in the filter card (LiveIntervalField), not a segmented control.
  return (
    <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", justifyContent: { xs: "center", md: "flex-start" }, gap: 1.5 }}>
      <Button
        variant="contained"
        color={live ? "primary" : "inherit"}
        onClick={toggleLive}
        title={copy(pageContract, "live.toggle_title")}
        aria-pressed={live}
        startIcon={
          <Box
            component="span"
            aria-hidden="true"
            sx={{
              width: "calc(1 * var(--spacing))",
              height: "calc(1 * var(--spacing))",
              borderRadius: "50%",
              bgcolor: "currentColor",
              animation: live ? "lt-pulse 1.6s ease-in-out infinite" : "none",
              "@keyframes lt-pulse": { "0%, 100%": { opacity: 1 }, "50%": { opacity: 0.35 } },
            }}
          />
        }
        sx={{ minHeight: { xs: TAP_MIN, md: 36 } }}
      >
        {live ? copy(pageContract, "live.badge_live") : copy(pageContract, "live.badge_paused")}
      </Button>
      {/* "Data shown as of" is the timestamp of the data actually on screen. A paused board does not
          refresh, so generatedAt cannot move underneath it — deriving this instead of holding it in
          state is what lets PAUSED survive the Suspense remount that every filter change triggers. */}
      {!live ? (
        <Typography variant="caption" role="status" sx={{ color: "warning.main" }}>
          {copy(pageContract, "live.stale_prefix")} <b>{fmtClock(generatedAt)}</b> {copy(pageContract, "live.stale_suffix")}
        </Typography>
      ) : null}
    </Box>
  );
}

/** The refresh interval as a filter-card select (template toolbar TextField), on the poller's own store. */
export function LiveIntervalField({ pageContract }: { pageContract: AdminUiPageContract }) {
  const intervalSeconds = useSyncExternalStore(subscribeInterval, readInterval, serverInterval);
  const intervals = optionGroup(pageContract, "live_refresh_interval");
  return (
    <TextField
      select
      fullWidth
      label={copy(pageContract, "live.interval_label")}
      value={String(intervalSeconds)}
      onChange={({ target: { value } }) => writeInterval(Number(value))}
      slotProps={{ inputLabel: { shrink: true } }}
    >
      {intervals.map((option) => (
        <MenuItem key={option.key} value={option.key} title={option.title || undefined}>
          {option.label}
        </MenuItem>
      ))}
    </TextField>
  );
}
