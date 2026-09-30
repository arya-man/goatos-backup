"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import ButtonBase from "@mui/material/ButtonBase";
import Box from "@mui/material/Box";
import {
  watchAtSx,
  watchCountSx,
  watchDimSx,
  watchDotSx,
  watchFeedItemSx,
  watchFeedSx,
  watchHeadSx,
  watchNoteSx,
  watchPctSx,
  watchPillSx,
  watchScrollSx,
  watchStopSx,
  watchSubSx,
  watchSx,
  watchTableSx,
  watchTagSx,
  watchTitleSx,
} from "./ceo-ai-sx";

// Live BLE ear-tag watch card for Ask Mesha (watch_tags tool). The agent server
// polls the Herd Signals live table and streams frames; this card shows the live
// table, the change feed, a countdown and "Stop watching". Vocabulary and tones
// follow features/herd-signals/format.ts (Live Monitor) so both screens agree.

import { useEffect, useState, type ReactElement } from "react";
import type { CeoAiWatchFrame } from "@/lib/ceo-ai-stream";
import type { WatchState } from "./types";

const MAX_CHANGES = 200;

// Merge one streamed frame into the card state (rows replace, changes append).
export function mergeWatch(prev: WatchState | undefined, f: CeoAiWatchFrame): WatchState {
  const base: WatchState =
    prev && prev.watchId === f.watchId
      ? prev
      : { watchId: f.watchId, polls: 0, rows: [], changes: [], ended: false };
  const changes = f.changes?.length ? [...base.changes, ...f.changes].slice(-MAX_CHANGES) : base.changes;
  return {
    ...base,
    label: f.label ?? base.label,
    startedAt: f.startedAt ?? base.startedAt,
    endsAt: f.endsAt ?? base.endsAt,
    intervalS: f.intervalS ?? base.intervalS,
    compare: f.compare ?? base.compare,
    unmatched: f.unmatched ?? base.unmatched,
    polls: f.polls ?? base.polls,
    rows: f.rows ?? base.rows,
    changes,
    ended: base.ended || f.phase === "end",
    reason: f.phase === "end" ? f.reason : base.reason,
    error: f.phase === "error" ? f.message : f.phase === "tick" || f.phase === "start" ? undefined : base.error,
  };
}

const STATE_TONE: Record<string, string> = { moving: "ok", low: "teal", quiet: "mut", not_moving: "warn", stale: "dng" };
const STATUS_TONE: Record<string, string> = { Good: "ok", "Weak signal": "warn", "Low battery": "pur", "Missing signal": "dng" };
const REASON: Record<string, string> = {
  time_up: "Time up",
  stopped: "Stopped",
  client_disconnected: "Closed",
  snapshot: "Snapshot",
  no_matching_tags: "No matching tags",
  data_error: "Live data unavailable",
};

function reasonLabel(r?: string): string {
  if (!r) return "Ended";
  if (r.startsWith("stop_when_met")) return "Condition met";
  return REASON[r] ?? "Ended";
}

function ago(s: number | null): string {
  if (s === null) return "—";
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  return `${Math.floor(s / 3600)}h ago`;
}

function pct(v?: number): ReactElement | null {
  if (v === undefined) return null;
  const tone = v <= -70 ? "dng" : v >= 150 ? "warn" : "mut";
  return (
    <Box component="span" sx={watchPctSx(tone)}>
      {v > 0 ? `+${v}` : v}%
    </Box>
  );
}

function useCountdown(endsAt: string | undefined, live: boolean): string {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!live) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [live]);
  if (!endsAt) return "";
  const left = Math.max(0, Math.round((Date.parse(endsAt) - now) / 1000));
  return `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")} left`;
}

export function CeoAiWatchCard(props: { watch: WatchState; onStop?: () => void }): ReactElement {
  const { watch, onStop } = props;
  const live = !watch.ended;
  const countdown = useCountdown(watch.endsAt, live);
  const showOwn = watch.compare === "self" || watch.compare === "both";
  const showPen = watch.compare === "peers" || watch.compare === "both";
  const feed = [...watch.changes].reverse().slice(0, 30);
  return (
    <Box component="section" sx={watchSx(live)} aria-label="Live tag watch">
      <Box component="header" sx={watchHeadSx}>
        <Box component="span" sx={watchDotSx(live)} aria-hidden="true" />
        <Box sx={watchTitleSx}>
          <strong>{live ? "Watching live tags" : `Watch ended · ${reasonLabel(watch.reason)}`}</strong>
          <Box component="span" sx={watchSubSx}>
            {watch.label}
            {watch.intervalS ? ` · every ${watch.intervalS}s` : ""}
            {watch.polls ? ` · ${watch.polls} update${watch.polls === 1 ? "" : "s"}` : ""}
          </Box>
        </Box>
        {live ? (
          <>
            <Box component="span" sx={watchCountSx} aria-live="off">
              {countdown}
            </Box>
            {onStop ? (
              <ButtonBase sx={watchStopSx} onClick={onStop}>
                Stop watching
              </ButtonBase>
            ) : null}
          </>
        ) : null}
      </Box>
      {watch.error ? <Box component="p" sx={watchNoteSx} role="status">
          {watch.error}
        </Box> : null}
      {watch.unmatched?.length ? <Box component="p" sx={watchNoteSx}>
          No tag matched: {watch.unmatched.join(", ")}
        </Box> : null}
      {watch.rows.length ? (
        <Box sx={watchScrollSx}>
          <Table sx={watchTableSx}>
            <TableHead>
              <TableRow>
                <TableCell component="th">Tag</TableCell>
                <TableCell component="th">Pen</TableCell>
                <TableCell component="th">State</TableCell>
                <TableCell component="th" align="right">Motion</TableCell>
                {showOwn ? <TableCell component="th" align="right">vs own</TableCell> : null}
                {showPen ? <TableCell component="th" align="right">vs pen</TableCell> : null}
                <TableCell component="th">Last seen</TableCell>
                <TableCell component="th">Signal</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {watch.rows.map((r) => (
                <TableRow key={r.tag}>
                  <TableCell>
                    <Box component="span" sx={watchTagSx}>
                      {r.tag}
                    </Box>
                    {r.animal ? <Box component="span" sx={watchDimSx}>{r.animal}</Box> : null}
                  </TableCell>
                  <TableCell>
                    {r.pen ?? "Unmapped"}
                    {r.park ? <Box component="span" sx={watchDimSx}>{r.park}</Box> : null}
                  </TableCell>
                  <TableCell>
                    <Box component="span" sx={watchPillSx(STATE_TONE[r.state] ?? "mut")}>
                      {r.state_label}
                    </Box>
                    {r.live_state === "moving_now" ? <Box component="span" sx={watchDimSx}>moving now</Box> : null}
                    {r.still_min >= 1 ? <Box component="span" sx={watchDimSx}>still {r.still_min}m</Box> : null}
                  </TableCell>
                  <TableCell align="right">
                    {r.motion_count ?? "—"}
                    <Box component="span" sx={watchDimSx}>
                      {r.delta_since_start !== null ? `+${r.delta_since_start} since start` : ""}
                    </Box>
                  </TableCell>
                  {showOwn ? <TableCell align="right">{pct(r.vs_own_pct) ?? "—"}</TableCell> : null}
                  {showPen ? <TableCell align="right">{pct(r.vs_pen_pct) ?? "—"}</TableCell> : null}
                  <TableCell>{ago(r.last_seen_s)}</TableCell>
                  <TableCell>
                    <Box component="span" sx={watchPillSx(STATUS_TONE[r.status] ?? "mut")}>
                      {r.status}
                    </Box>
                    <Box component="span" sx={watchDimSx}>
                      {r.rssi !== null ? `${r.rssi} dBm` : ""}
                      {r.battery_mv !== null ? ` · ${(r.battery_mv / 1000).toFixed(2)} V` : ""}
                    </Box>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      ) : null}
      {feed.length ? (
        <Box component="ol" sx={watchFeedSx} aria-live="polite">
          {feed.map((c, i) => (
            <Box component="li" key={`${watch.changes.length - i}`} sx={watchFeedItemSx(c.tone ?? "mut")}>
              <Box component="span" sx={watchAtSx}>
                {c.at_min !== undefined ? `${c.at_min}m` : ""}
              </Box>
              <span>{c.text}</span>
            </Box>
          ))}
        </Box>
      ) : live && watch.polls > 0 ? (
        <Box component="p" sx={watchNoteSx}>
          No changes yet.
        </Box>
      ) : null}
    </Box>
  );
}
