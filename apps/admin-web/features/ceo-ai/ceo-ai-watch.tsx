"use client";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import Table from "@mui/material/Table";
import Button from "@mui/material/Button";
import TableRow from "@mui/material/TableRow";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import Typography from "@mui/material/Typography";
import TableContainer from "@mui/material/TableContainer";
import { Label, type LabelColor } from "@/components/minimal/label";

// Live BLE ear-tag watch card for Ask Mesha (watch_tags tool). The agent server
// polls the Herd Signals live table and streams frames; this card shows the live
// table, the change feed, a countdown and "Stop watching". Vocabulary and tones
// follow features/herd-signals/format.ts (Live Monitor) so both screens agree. Template parts only:
// an outlined card box, a small MUI Table that scrolls inside it, soft Labels for states and signal.

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

const STATE_TONE: Record<string, LabelColor> = { moving: "success", low: "info", quiet: "default", not_moving: "warning", stale: "error" };
const STATUS_TONE: Record<string, LabelColor> = { Good: "success", "Weak signal": "warning", "Low battery": "secondary", "Missing signal": "error" };
const FEED_TONE: Record<string, string> = { ok: "success.main", teal: "info.main", warn: "warning.main", dng: "error.main", pur: "secondary.main", mut: "text.secondary" };
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
  const color = v <= -70 ? "error.main" : v >= 150 ? "warning.main" : "text.secondary";
  return (
    <Box component="span" sx={{ color, fontWeight: "fontWeightSemiBold" }}>
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
  const dim = (text: string | number | null | undefined) =>
    text ? (
      <Typography component="span" variant="caption" sx={{ display: "block", color: "text.secondary" }}>
        {text}
      </Typography>
    ) : null;
  return (
    <Box
      component="section"
      aria-label="Live tag watch"
      sx={{ mb: 1.5, border: 1, borderColor: live ? "success.main" : "divider", borderRadius: "var(--r-md)", overflow: "hidden", bgcolor: "background.paper" }}
    >
      <Stack direction="row" spacing={1.5} sx={{ alignItems: "center", px: 2, py: 1.5 }}>
        <Label variant="soft" color={live ? "success" : "default"}>
          {live ? "Live" : "Ended"}
        </Label>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography variant="subtitle2">{live ? "Watching live tags" : `Watch ended · ${reasonLabel(watch.reason)}`}</Typography>
          <Typography variant="caption" sx={{ color: "text.secondary", overflowWrap: "anywhere" }}>
            {watch.label}
            {watch.intervalS ? ` · every ${watch.intervalS}s` : ""}
            {watch.polls ? ` · ${watch.polls} update${watch.polls === 1 ? "" : "s"}` : ""}
          </Typography>
        </Box>
        {live ? (
          <>
            <Typography variant="caption" sx={{ color: "text.secondary", fontVariantNumeric: "tabular-nums" }} aria-live="off">
              {countdown}
            </Typography>
            {onStop ? (
              <Button size="small" variant="outlined" color="error" onClick={onStop}>
                Stop watching
              </Button>
            ) : null}
          </>
        ) : null}
      </Stack>
      {watch.error ? (
        <Typography variant="body2" role="status" sx={{ px: 2, pb: 1.5, color: "text.secondary" }}>
          {watch.error}
        </Typography>
      ) : null}
      {watch.unmatched?.length ? (
        <Typography variant="body2" sx={{ px: 2, pb: 1.5, color: "text.secondary" }}>
          No tag matched: {watch.unmatched.join(", ")}
        </Typography>
      ) : null}
      {watch.rows.length ? (
        <TableContainer sx={{ maxHeight: 320 }}>
          <Table size="small" stickyHeader sx={{ minWidth: 640 }}>
            <TableHead>
              <TableRow>
                <TableCell>Tag</TableCell>
                <TableCell>Pen</TableCell>
                <TableCell>State</TableCell>
                <TableCell align="right">Motion</TableCell>
                {showOwn ? <TableCell align="right">vs own</TableCell> : null}
                {showPen ? <TableCell align="right">vs pen</TableCell> : null}
                <TableCell>Last seen</TableCell>
                <TableCell>Signal</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {watch.rows.map((r) => (
                <TableRow key={r.tag}>
                  <TableCell>
                    <Typography variant="subtitle2" component="span">
                      {r.tag}
                    </Typography>
                    {dim(r.animal)}
                  </TableCell>
                  <TableCell>
                    {r.pen ?? "Unmapped"}
                    {dim(r.park)}
                  </TableCell>
                  <TableCell>
                    <Label variant="soft" color={STATE_TONE[r.state] ?? "default"}>
                      {r.state_label}
                    </Label>
                    {r.live_state === "moving_now" ? dim("moving now") : null}
                    {r.still_min >= 1 ? dim(`still ${r.still_min}m`) : null}
                  </TableCell>
                  <TableCell align="right">
                    {r.motion_count ?? "—"}
                    {dim(r.delta_since_start !== null ? `+${r.delta_since_start} since start` : "")}
                  </TableCell>
                  {showOwn ? <TableCell align="right">{pct(r.vs_own_pct) ?? "—"}</TableCell> : null}
                  {showPen ? <TableCell align="right">{pct(r.vs_pen_pct) ?? "—"}</TableCell> : null}
                  <TableCell>{ago(r.last_seen_s)}</TableCell>
                  <TableCell>
                    <Label variant="soft" color={STATUS_TONE[r.status] ?? "default"}>
                      {r.status}
                    </Label>
                    {dim(
                      `${r.rssi !== null ? `${r.rssi} dBm` : ""}${r.battery_mv !== null ? ` · ${(r.battery_mv / 1000).toFixed(2)} V` : ""}`,
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      ) : null}
      {feed.length ? (
        <Stack component="ol" spacing={0.5} aria-live="polite" sx={{ m: 0, px: 2, py: 1.5, listStyle: "none", borderTop: 1, borderColor: "divider", maxHeight: 180, overflowY: "auto" }}>
          {feed.map((c, i) => (
            <Stack component="li" key={`${watch.changes.length - i}`} direction="row" spacing={1.5} sx={{ typography: "caption", color: FEED_TONE[c.tone ?? "mut"] ?? "text.secondary" }}>
              <Box component="span" sx={{ minWidth: 32, color: "text.secondary", fontVariantNumeric: "tabular-nums" }}>
                {c.at_min !== undefined ? `${c.at_min}m` : ""}
              </Box>
              <span>{c.text}</span>
            </Stack>
          ))}
        </Stack>
      ) : live && watch.polls > 0 ? (
        <Typography variant="body2" sx={{ px: 2, pb: 1.5, color: "text.secondary" }}>
          No changes yet.
        </Typography>
      ) : null}
    </Box>
  );
}
