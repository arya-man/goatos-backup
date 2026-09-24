"use client";

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
          <table className="mzai-w-table">
            <thead>
              <tr>
                <th>Tag</th>
                <th>Pen</th>
                <th>State</th>
                <th className="num">Motion</th>
                {showOwn ? <th className="num">vs own</th> : null}
                {showPen ? <th className="num">vs pen</th> : null}
                <th>Last seen</th>
                <th>Signal</th>
              </tr>
            </thead>
            <tbody>
              {watch.rows.map((r) => (
                <tr key={r.tag}>
                  <td>
                    <span className="mzai-w-tag">{r.tag}</span>
                    {r.animal ? <span className="mzai-w-dim">{r.animal}</span> : null}
                  </td>
                  <td>
                    {r.pen ?? "Unmapped"}
                    {r.park ? <span className="mzai-w-dim">{r.park}</span> : null}
                  </td>
                  <td>
                    <span className={`mzai-w-pill ${STATE_TONE[r.state] ?? "mut"}`}>{r.state_label}</span>
                    {r.live_state === "moving_now" ? <span className="mzai-w-dim">moving now</span> : null}
                    {r.still_min >= 1 ? <span className="mzai-w-dim">still {r.still_min}m</span> : null}
                  </td>
                  <td className="num">
                    {r.motion_count ?? "—"}
                    <span className="mzai-w-dim">
                      {r.delta_since_start !== null ? `+${r.delta_since_start} since start` : ""}
                    </span>
                  </td>
                  {showOwn ? <td className="num">{pct(r.vs_own_pct) ?? "—"}</td> : null}
                  {showPen ? <td className="num">{pct(r.vs_pen_pct) ?? "—"}</td> : null}
                  <td>{ago(r.last_seen_s)}</td>
                  <td>
                    <span className={`mzai-w-pill ${STATUS_TONE[r.status] ?? "mut"}`}>{r.status}</span>
                    <span className="mzai-w-dim">
                      {r.rssi !== null ? `${r.rssi} dBm` : ""}
                      {r.battery_mv !== null ? ` · ${(r.battery_mv / 1000).toFixed(2)} V` : ""}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
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

// Styles (brand tokens only; light/dark come from the shared theme tokens).
export const WATCH_CSS = `
.mzai-watch{border:1px solid var(--line);border-radius:12px;background:var(--panel);margin:4px 0 10px;overflow:hidden;max-width:100%}
.mzai-watch.live{border-color:color-mix(in srgb,var(--brand) 45%,var(--line))}
.mzai-w-head{display:flex;align-items:center;gap:10px;flex-wrap:wrap;padding:10px 12px;border-bottom:1px solid var(--line);background:var(--panel-2)}
.mzai-w-dot{width:8px;height:8px;border-radius:50%;background:var(--muted);flex:none}
.mzai-w-dot.live{background:var(--brand);box-shadow:0 0 0 0 color-mix(in srgb,var(--brand) 60%,transparent);animation:mzai-w-pulse 1.6s ease-out infinite}
@keyframes mzai-w-pulse{to{box-shadow:0 0 0 8px transparent}}
.mzai-w-title{display:flex;flex-direction:column;min-width:0;flex:1 1 160px}
.mzai-w-title strong{font-size:13px;color:var(--ink)}
.mzai-w-sub{font-size:11.5px;color:var(--muted);overflow-wrap:anywhere}
.mzai-w-count{font-size:12px;color:var(--muted);font-variant-numeric:tabular-nums}
.mzai-w-stop{border:1.5px solid var(--brand);background:var(--panel);color:var(--ink);border-radius:999px;padding:5px 12px;font:inherit;font-size:12px;font-weight:600;cursor:pointer;min-height:32px}
.mzai-w-stop:hover{background:var(--brand-soft)}
.mzai-w-err,.mzai-w-quiet{margin:8px 12px;font-size:12px;color:var(--muted)}
.mzai-w-scroll{overflow-x:auto;-webkit-overflow-scrolling:touch;max-height:320px;overflow-y:auto}
.mzai-w-table{border-collapse:collapse;width:100%;font-size:12px;color:var(--ink)}
.mzai-w-table th{position:sticky;top:0;background:var(--panel);text-align:left;font-weight:600;color:var(--muted);padding:6px 10px;border-bottom:1px solid var(--line);white-space:nowrap}
.mzai-w-table td{padding:6px 10px;border-bottom:1px solid var(--line);vertical-align:top;white-space:nowrap}
.mzai-w-table .num{text-align:right;font-variant-numeric:tabular-nums}
.mzai-w-tag{font-weight:600}
.mzai-w-dim{display:block;font-size:11px;color:var(--muted)}
.mzai-w-pill{display:inline-block;border-radius:999px;padding:1px 8px;font-size:11px;font-weight:600;background:color-mix(in srgb,var(--muted) 14%,transparent);color:var(--ink)}
.mzai-w-pill.ok{background:var(--okx)}
.mzai-w-pill.teal{background:var(--tealx)}
.mzai-w-pill.warn{background:var(--warnx)}
.mzai-w-pill.dng{background:var(--dangerx)}
.mzai-w-pill.pur{background:var(--purplex)}
.mzai-w-pct{font-weight:600}
.mzai-w-pct.dng{color:var(--danger)}
.mzai-w-pct.warn{color:var(--warn)}
.mzai-w-pct.mut{color:var(--muted)}
.mzai-w-feed{list-style:none;margin:0;padding:8px 12px;display:flex;flex-direction:column;gap:4px;max-height:160px;overflow-y:auto;font-size:12px;color:var(--ink)}
.mzai-w-feed li{display:flex;gap:8px;align-items:baseline;border-left:3px solid var(--line);padding-left:8px}
.mzai-w-feed li.ok{border-left-color:var(--brand)}
.mzai-w-feed li.warn{border-left-color:var(--warn)}
.mzai-w-feed li.dng{border-left-color:var(--danger)}
.mzai-w-at{flex:none;min-width:34px;color:var(--muted);font-variant-numeric:tabular-nums}
@media (max-width:620px){.mzai-w-table td,.mzai-w-table th{padding:6px 8px}.mzai-w-head{padding:10px}}
@media (prefers-reduced-motion:reduce){.mzai-w-dot.live{animation:none}}
`;
