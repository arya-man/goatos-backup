"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

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
  return <span className={`mzai-w-pct ${tone}`}>{v > 0 ? `+${v}` : v}%</span>;
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
    <section className={`mzai-watch${live ? " live" : ""}`} aria-label="Live tag watch">
      <header className="mzai-w-head">
        <span className={`mzai-w-dot${live ? " live" : ""}`} aria-hidden="true" />
        <div className="mzai-w-title">
          <strong>{live ? "Watching live tags" : `Watch ended · ${reasonLabel(watch.reason)}`}</strong>
          <span className="mzai-w-sub">
            {watch.label}
            {watch.intervalS ? ` · every ${watch.intervalS}s` : ""}
            {watch.polls ? ` · ${watch.polls} update${watch.polls === 1 ? "" : "s"}` : ""}
          </span>
        </div>
        {live ? (
          <>
            <span className="mzai-w-count" aria-live="off">{countdown}</span>
            {onStop ? (
              <button type="button" className="mzai-w-stop" onClick={onStop}>
                Stop watching
              </button>
            ) : null}
          </>
        ) : null}
      </header>
      {watch.error ? <p className="mzai-w-err" role="status">{watch.error}</p> : null}
      {watch.unmatched?.length ? <p className="mzai-w-err">No tag matched: {watch.unmatched.join(", ")}</p> : null}
      {watch.rows.length ? (
        <div className="mzai-w-scroll">
          <Table className="mzai-w-table">
            <TableHead>
              <TableRow>
                <TableCell component="th">Tag</TableCell>
                <TableCell component="th">Pen</TableCell>
                <TableCell component="th">State</TableCell>
                <TableCell component="th" className="num">Motion</TableCell>
                {showOwn ? <TableCell component="th" className="num">vs own</TableCell> : null}
                {showPen ? <TableCell component="th" className="num">vs pen</TableCell> : null}
                <TableCell component="th">Last seen</TableCell>
                <TableCell component="th">Signal</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {watch.rows.map((r) => (
                <TableRow key={r.tag}>
                  <TableCell>
                    <span className="mzai-w-tag">{r.tag}</span>
                    {r.animal ? <span className="mzai-w-dim">{r.animal}</span> : null}
                  </TableCell>
                  <TableCell>
                    {r.pen ?? "Unmapped"}
                    {r.park ? <span className="mzai-w-dim">{r.park}</span> : null}
                  </TableCell>
                  <TableCell>
                    <span className={`mzai-w-pill ${STATE_TONE[r.state] ?? "mut"}`}>{r.state_label}</span>
                    {r.live_state === "moving_now" ? <span className="mzai-w-dim">moving now</span> : null}
                    {r.still_min >= 1 ? <span className="mzai-w-dim">still {r.still_min}m</span> : null}
                  </TableCell>
                  <TableCell className="num">
                    {r.motion_count ?? "—"}
                    <span className="mzai-w-dim">
                      {r.delta_since_start !== null ? `+${r.delta_since_start} since start` : ""}
                    </span>
                  </TableCell>
                  {showOwn ? <TableCell className="num">{pct(r.vs_own_pct) ?? "—"}</TableCell> : null}
                  {showPen ? <TableCell className="num">{pct(r.vs_pen_pct) ?? "—"}</TableCell> : null}
                  <TableCell>{ago(r.last_seen_s)}</TableCell>
                  <TableCell>
                    <span className={`mzai-w-pill ${STATUS_TONE[r.status] ?? "mut"}`}>{r.status}</span>
                    <span className="mzai-w-dim">
                      {r.rssi !== null ? `${r.rssi} dBm` : ""}
                      {r.battery_mv !== null ? ` · ${(r.battery_mv / 1000).toFixed(2)} V` : ""}
                    </span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}
      {feed.length ? (
        <ol className="mzai-w-feed" aria-live="polite">
          {feed.map((c, i) => (
            <li key={`${watch.changes.length - i}`} className={c.tone ?? "mut"}>
              <span className="mzai-w-at">{c.at_min !== undefined ? `${c.at_min}m` : ""}</span>
              <span>{c.text}</span>
            </li>
          ))}
        </ol>
      ) : live && watch.polls > 0 ? (
        <p className="mzai-w-quiet">No changes yet.</p>
      ) : null}
    </section>
  );
}
