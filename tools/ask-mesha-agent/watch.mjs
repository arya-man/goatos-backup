// Live watch of BLE ear-tag data for Ask Mesha (the watch_tags tool).
//
// The SERVER polls herd_signal_tag_latest every interval_s through the same read-only
// runSql path as run_sql and streams `watch` SSE events to the panel. No model call per
// tick: the model sees one compact summary when the watch ends (time up / stop_when met /
// Stop watching / client disconnect / 30-min hard cap). No background continuation.
//
// Semantics are the Herd Signals / Live Monitor screen's, not new ones:
//   backend/internal/herdsignals/domain/types.go DefaultThresholds + motion.go
//     movement_state (15-min window delta): >=100 moving, 10-99 low, 1-9 quiet, 0 not_moving
//     stale / missing signal: no packet for > StalePacketMinutes (30)
//     weak signal: rssi <= -75 dBm; low battery: battery_state low|critical (< 2800 mV)
//   backend/internal/herdsignals/app/service.go applyRiskSignals (the Insights / watchlist
//   risk reasons):
//     vs own pace  = (motion_delta - p75 baseline scaled to the window) / scaled baseline,
//                    p75 over the tag's 24h 300s activity windows since animal_monitoring_since
//                    (repository.go GetBaselineDeltas); <= -70% "far below own baseline",
//                    >= +150% "spike vs own baseline"
//     vs peers     = (motion_delta - pen median motion_delta) / pen median (non-gap rows);
//                    <= -70% "lower than pen group"
//   apps/admin-web/features/herd-signals/format.ts: MOVEMENT_LABEL / herdSignalStatus labels.

export const T = Object.freeze({
  motionActive: 100,
  motionLow: 10,
  motionQuiet: 1,
  staleMinutes: 30,
  weakRssi: -75,
  batteryWatchMv: 2800,
  ownFarBelowPct: -70,
  ownSpikePct: 150,
  penLowerPct: -70,
  baselineBucketSeconds: 300,
});

export const LIMITS = Object.freeze({
  minutesDefault: 5, minutesMax: 30, intervalDefault: 10, intervalMin: 5, intervalMax: 30,
  stillMinutesDefault: 5, baselineRefreshMs: 5 * 60_000, maxRows: 60, maxChanges: 200,
});

export const MOVEMENT_LABEL = { moving: "Moving", low: "Low", quiet: "Quiet", not_moving: "No movement", stale: "Stale" };
export const STOP_WHEN = ["any_stops_moving", "all_stop_moving", "any_starts_moving", "all_start_moving"];
export const COMPARE = ["none", "self", "peers", "both"];

const clamp = (v, lo, hi, d) => {
  const n = Number(v);
  return Number.isFinite(n) ? Math.min(hi, Math.max(lo, n)) : d;
};

// Tool args -> a bounded, validated watch spec. minutes=0 is a one-shot snapshot.
export function parseWatchArgs(a = {}) {
  const raw = Array.isArray(a.filter) ? a.filter : String(a.filter ?? "").split(/[,;\n]+/);
  const filter = raw.map((s) => String(s).trim()).filter(Boolean).slice(0, 40);
  const minutes = a.minutes === undefined || a.minutes === null ? LIMITS.minutesDefault : clamp(a.minutes, 0, LIMITS.minutesMax, LIMITS.minutesDefault);
  const interval_s = Math.round(clamp(a.interval_s, LIMITS.intervalMin, LIMITS.intervalMax, LIMITS.intervalDefault));
  const stop_when = STOP_WHEN.includes(a.stop_when) ? a.stop_when : null;
  const compare = COMPARE.includes(a.compare) ? a.compare : "none";
  const still_minutes = clamp(a.still_minutes, 1, 30, LIMITS.stillMinutesDefault);
  return { filter, minutes, interval_s, stop_when, compare, still_minutes };
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const TAG_ID = /^[A-Za-z0-9:_-]{1,64}$/;

// origin/main (1d630e9b3 / repository.go herdSignalsLiveFilter) adds per-packet motion columns
// and the live_state cohorts; stg may not have them yet, so they are probed once per watch.
export const REALTIME_COLUMNS_SQL = `SELECT column_name FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = 'herd_signal_tag_latest'
  AND column_name IN ('last_packet_motion_delta', 'motion_delta_60s')`;

// Same tag -> goat -> pen join as the live table (repository.go tagLocationJoin), and the same
// read-time stale rule (effectiveMovementStateExpr: now() - last_seen_at > 30 min => stale,
// pattern => missing) evaluated on the DB clock.
// Fail closed: without a valid tenant UUID the watch would read every tenant's tags. Only the
// local bench user (allowAllTenants) may watch unscoped.
function tenantClause(col, tenantId, allowAllTenants) {
  if (UUID.test(String(tenantId || ""))) return `${col} = '${tenantId}'`;
  if (allowAllTenants) return "";
  throw new Error("tenant_required");
}
export const validTenant = (tenantId) => UUID.test(String(tenantId || ""));

export function snapshotSql(tenantId, { realtime = false, allowAllTenants = false } = {}) {
  const tc = tenantClause("tl.tenant_id", tenantId, allowAllTenants);
  const tenant = tc ? `WHERE ${tc}` : "";
  const live = realtime
    ? `CASE WHEN now() - tl.last_seen_at <= interval '30 seconds' AND COALESCE(tl.last_packet_motion_delta, 0) > 0 THEN 'moving_now'
       WHEN now() - tl.last_seen_at <= interval '90 seconds' AND COALESCE(tl.motion_delta_60s, 0) > 0 THEN 'active_1m' ELSE '' END`
    : "''";
  return `SELECT tl.tag_id, g.goat_id, g.display_id, g.shed_id, shed_loc.name AS pen, park_loc.name AS park,
  CASE WHEN now() - tl.last_seen_at > interval '30 minutes' THEN 'stale' ELSE tl.movement_state END AS movement_state,
  CASE WHEN now() - tl.last_seen_at > interval '30 minutes' THEN 'missing' ELSE tl.pattern_state END AS pattern_state,
  ${live} AS live_state,
  tl.motion_count, tl.motion_delta, tl.motion_window_seconds, tl.gap_delta,
  to_char(tl.last_seen_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS last_seen_at,
  round(extract(epoch FROM now() - tl.last_seen_at))::int AS last_seen_s,
  tl.last_rssi_dbm, tl.battery_mv, tl.battery_state, tl.mapping_state
FROM public.herd_signal_tag_latest tl
LEFT JOIN LATERAL (
  SELECT gi.goat_id FROM public.goat_identifiers gi
  WHERE gi.tenant_id = tl.tenant_id AND gi.status = 'active' AND gi.smart_tag_capable IS TRUE
    AND gi.normalized_value IN (UPPER(BTRIM(tl.tag_id)), UPPER(BTRIM(COALESCE(tl.tag_mac, ''))))
  LIMIT 1
) mg ON true
LEFT JOIN public.goats g ON g.tenant_id = tl.tenant_id AND g.goat_id = mg.goat_id
LEFT JOIN public.locations shed_loc ON shed_loc.tenant_id = tl.tenant_id AND shed_loc.location_id = g.shed_id
LEFT JOIN public.locations park_loc ON park_loc.tenant_id = tl.tenant_id AND park_loc.location_id = g.park_id
${tenant}
ORDER BY shed_loc.name NULLS LAST, tl.tag_id
LIMIT 500`; // run_sql's row cap (lib.mjs SQL_MAX_ROWS)
}

// p75 of the tag's own 24h 300s windows since monitoring began (GetBaselineDeltas).
export function baselineSql(tagIds, tenantId, { allowAllTenants = false } = {}) {
  const tc = tenantClause("w.tenant_id", tenantId, allowAllTenants);
  const ids = tagIds.filter((t) => TAG_ID.test(t)).map((t) => `'${t}'`);
  if (!ids.length) return "";
  return `SELECT w.tag_id, percentile_disc(0.75) WITHIN GROUP (ORDER BY w.motion_delta) AS baseline
FROM public.herd_signal_activity_windows w
JOIN public.herd_signal_tag_latest tl ON tl.tenant_id = w.tenant_id AND tl.tag_id = w.tag_id
WHERE w.tag_id IN (${ids.join(",")})${tc ? `
  AND ${tc}` : ""}
  AND w.bucket_seconds = 300 AND w.packet_count > 0 AND w.gap_delta = false
  AND w.bucket_start >= now() - interval '24 hours'
  AND tl.animal_monitoring_since IS NOT NULL AND w.bucket_start >= tl.animal_monitoring_since
GROUP BY w.tag_id`;
}

// psql -A -F\t output (header line first) -> objects.
export function parseTsv(out) {
  const lines = String(out || "").split("\n").filter((l) => l.length);
  if (lines.length < 1) return [];
  const head = lines[0].split("\t");
  return lines.slice(1).filter((l) => !/^\(\d+ rows?\)$/.test(l) && !l.startsWith("… (")).map((l) => {
    const cells = l.split("\t");
    return Object.fromEntries(head.map((h, i) => [h, cells[i] ?? ""]));
  });
}

const num = (v) => (v === "" || v === null || v === undefined || !Number.isFinite(Number(v)) ? null : Number(v));
const norm = (s) => String(s || "").toLowerCase().replace(/[^a-z0-9]+/g, " ").trim();

export function normalizeRow(r) {
  return {
    tag: r.tag_id, goat_id: r.goat_id || null, animal: r.display_id || null, shed_id: r.shed_id || null,
    pen: r.pen || null, park: r.park || null,
    movement_state: r.movement_state || null, pattern_state: r.pattern_state || null,
    motion_count: num(r.motion_count), motion_delta: num(r.motion_delta),
    window_s: num(r.motion_window_seconds) || 900, gap_delta: r.gap_delta === "t" || r.gap_delta === "true",
    live_state: r.live_state || null, last_seen_s: num(r.last_seen_s),
    last_seen_at: r.last_seen_at || null, rssi: num(r.last_rssi_dbm), battery_mv: num(r.battery_mv),
    battery_state: r.battery_state || null, mapping_state: r.mapping_state || null,
  };
}

// Filter terms: tag id / animal display id / goat uuid match exactly; pen and park names
// match either way round ("Yashoda 3" -> pen "Yashoda", "castro" -> "Castro 1").
// "all" / "*" / empty = every tag.
export function matchFilter(rows, terms) {
  if (!terms.length || terms.some((t) => /^(all|\*|everything|all tags|all animals)$/i.test(t.trim()))) {
    return { matched: rows, unmatched: [] };
  }
  const hit = new Set();
  const unmatched = [];
  for (const term of terms) {
    const t = norm(term);
    const exact = rows.filter((r) => [r.tag, r.animal, r.goat_id].some((v) => v && norm(v) === t));
    const byPlace = exact.length ? [] : rows.filter((r) => [r.pen, r.park].some((v) => {
      const n = norm(v);
      return n.length >= 3 && t.length >= 3 && (n === t || t.includes(n) || n.includes(t));
    }));
    // Prefer the most specific place match: "Castro 1" should not pull in "Castro 10".
    const exactPlace = byPlace.filter((r) => [r.pen, r.park].some((v) => norm(v) === t));
    const found = exact.length ? exact : exactPlace.length ? exactPlace : byPlace;
    if (!found.length) unmatched.push(term);
    for (const r of found) hit.add(r.tag);
  }
  return { matched: rows.filter((r) => hit.has(r.tag)), unmatched };
}

function median(xs) {
  if (!xs.length) return null;
  const s = [...xs].sort((a, b) => a - b);
  const m = Math.floor(s.length / 2);
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
}

// Pen median motion_delta over ALL tags in the pen (non-gap), as riskGroupStatsFromItems.
export function penMedians(allRows) {
  const by = new Map();
  for (const r of allRows) {
    if (!r.shed_id || r.motion_delta === null || r.gap_delta) continue;
    if (!by.has(r.shed_id)) by.set(r.shed_id, []);
    by.get(r.shed_id).push(r.motion_delta);
  }
  return new Map([...by].map(([k, v]) => [k, median(v)]));
}

const minutesSince = (iso, now) => (iso ? (now - Date.parse(iso)) / 60_000 : Infinity);

// One derived table row, with the screen's labels and the requested comparisons.
export function deriveRow(r, { now, start, baseline, penMedian, compare, stillSince }) {
  const stale = r.movement_state === "stale" || (r.last_seen_s !== null && r.last_seen_s !== undefined ? r.last_seen_s > T.staleMinutes * 60 : minutesSince(r.last_seen_at, now) > T.staleMinutes);
  const state = stale ? "stale" : r.movement_state || "not_moving";
  const weak = r.rssi !== null && r.rssi <= T.weakRssi;
  const lowBattery = r.battery_state === "low" || r.battery_state === "critical" || (r.battery_mv !== null && r.battery_mv < T.batteryWatchMv);
  // herdSignalStatus precedence: missing > weak > low battery > good.
  const status = stale ? "Missing signal" : weak ? "Weak signal" : lowBattery ? "Low battery" : "Good";
  const row = {
    tag: r.tag, animal: r.animal, pen: r.pen, park: r.park, state, state_label: MOVEMENT_LABEL[state] || state,
    pattern_state: r.pattern_state, motion_count: r.motion_count, motion_delta_15m: r.motion_delta,
    delta_since_start: r.motion_count !== null && start?.motion_count !== null && start?.motion_count !== undefined
      ? Math.max(0, r.motion_count - start.motion_count) : null,
    still_min: stillSince ? Math.floor((now - stillSince) / 60_000) : 0,
    live_state: r.live_state || null,
    last_seen_at: r.last_seen_at, last_seen_s: r.last_seen_s ?? (r.last_seen_at ? Math.max(0, Math.round((now - Date.parse(r.last_seen_at)) / 1000)) : null),
    rssi: r.rssi, battery_mv: r.battery_mv, status,
  };
  if ((compare === "self" || compare === "both") && baseline > 0 && r.motion_delta !== null && !r.gap_delta) {
    const scaled = baseline * (r.window_s / T.baselineBucketSeconds);
    row.vs_own_pct = Math.round(((r.motion_delta - scaled) / scaled) * 100);
    row.own_baseline_window = Math.round(scaled);
  }
  if ((compare === "peers" || compare === "both") && penMedian > 0 && r.motion_delta !== null && !r.gap_delta) {
    row.vs_pen_pct = Math.round(((r.motion_delta - penMedian) / penMedian) * 100);
    row.pen_median = penMedian;
  }
  const flags = [];
  if (row.vs_own_pct !== undefined && row.vs_own_pct <= T.ownFarBelowPct) flags.push("far below own pace");
  if (row.vs_own_pct !== undefined && row.vs_own_pct >= T.ownSpikePct) flags.push("spike vs own pace");
  if (row.vs_pen_pct !== undefined && row.vs_pen_pct <= T.penLowerPct) flags.push("lower than pen");
  row.flags = flags;
  return row;
}

const who = (r) => `${r.tag}${r.animal ? ` (${r.animal})` : ""}${r.pen ? ` in ${r.pen}` : ""}`;

// Per-tag tracker across ticks -> change lines. Pure: state in, changes out.
export function makeTracker({ start, still_minutes = LIMITS.stillMinutesDefault }) {
  const tags = new Map(); // tag -> {first, lastCount, stillSince, lastRow, wasStill, nextStillMark, moved}
  return {
    tags,
    // rawRows: normalized rows for the watched set. Returns change lines for this tick.
    observe(rawRows, now) {
      const changes = [];
      for (const r of rawRows) {
        let s = tags.get(r.tag);
        if (!s) {
          s = { first: r, lastCount: r.motion_count, stillSince: start, lastRow: null, nextStillMark: still_minutes, movedSinceStart: false, startedMoving: false, stoppedMoving: false };
          tags.set(r.tag, s);
          continue;
        }
        if (r.motion_count !== null && s.lastCount !== null && r.motion_count > s.lastCount) {
          const stillFor = (now - s.stillSince) / 60_000;
          // Tags tick in small bursts; only a real bout after >= 2 still minutes is "started moving".
          if (stillFor >= 2 && r.motion_count - s.lastCount >= T.motionLow) {
            changes.push({ tag: r.tag, tone: "ok", text: `${who(r)} started moving (+${r.motion_count - s.lastCount}) after ${Math.floor(stillFor)} min still` });
            s.startedMoving = true;
          }
          s.stillSince = now;
          s.nextStillMark = still_minutes;
          s.movedSinceStart = true;
          s.stoppedMoving = false;
        } else if (r.motion_count !== null && s.lastCount !== null && r.motion_count < s.lastCount) {
          s.stillSince = now; // counter reset (domain.MotionDelta floors at 0)
        } else {
          const stillFor = (now - s.stillSince) / 60_000;
          if (stillFor >= s.nextStillMark) {
            changes.push({ tag: r.tag, tone: "warn", text: `${who(r)} hasn't moved for ${Math.floor(stillFor)} min` });
            s.nextStillMark += still_minutes;
          }
          if (stillFor >= still_minutes) s.stoppedMoving = true;
        }
        if (r.motion_count !== null) s.lastCount = r.motion_count;
      }
      return changes;
    },
    // Diff derived rows (labels/status/flags) against the previous tick.
    diffRows(rows) {
      const changes = [];
      for (const row of rows) {
        const s = tags.get(row.tag);
        if (!s) continue;
        const p = s.lastRow;
        if (p) {
          if (p.state !== row.state) changes.push({ tag: row.tag, tone: row.state === "stale" ? "dng" : "mut", text: `${who(row)} now ${row.state_label} (was ${p.state_label})` });
          if (p.status !== row.status) changes.push({ tag: row.tag, tone: row.status === "Good" ? "ok" : "dng", text: `${who(row)}: ${row.status === "Good" ? "signal back to Good" : row.status}` });
          for (const f of row.flags) if (!p.flags.includes(f)) changes.push({ tag: row.tag, tone: "warn", text: `${who(row)} ${f}${f.includes("own") ? ` (${row.vs_own_pct}%)` : ` (${row.vs_pen_pct}%)`}` });
          // Only when still comparable: a pen median of 0 (everyone resting) is "no comparison", not "recovered".
          const comparable = (f) => (f.includes("own") ? row.vs_own_pct !== undefined : row.vs_pen_pct !== undefined);
          for (const f of p.flags) if (!row.flags.includes(f) && comparable(f)) changes.push({ tag: row.tag, tone: "ok", text: `${who(row)} no longer ${f}` });
        }
        s.lastRow = row;
      }
      return changes;
    },
    stillSince(tag) { return tags.get(tag)?.stillSince ?? start; },
  };
}

// stop_when over the watched set, using the tracker's per-tag flags.
export function stopMet(stopWhen, tracker) {
  if (!stopWhen) return false;
  const ss = [...tracker.tags.values()];
  if (!ss.length) return false;
  switch (stopWhen) {
    case "any_stops_moving": return ss.some((s) => s.stoppedMoving);
    case "all_stop_moving": return ss.every((s) => s.stoppedMoving);
    case "any_starts_moving": return ss.some((s) => s.startedMoving);
    case "all_start_moving": return ss.every((s) => s.startedMoving || s.movedSinceStart);
    default: return false;
  }
}

export function scopeLabel(spec, matched) {
  const pens = [...new Set(matched.map((r) => [r.pen, r.park].filter(Boolean).join(", ")).filter(Boolean))];
  const what = spec.filter.length ? spec.filter.join(", ") : "all tags";
  return `${what} · ${matched.length} tag${matched.length === 1 ? "" : "s"}${pens.length && pens.length <= 3 ? ` · ${pens.join("; ")}` : ""}`;
}

const REASON_TEXT = {
  time_up: "the requested time ran out",
  stop_when_met: "the stop_when condition was met",
  stopped: "the user pressed Stop watching; no condition was met",
  client_disconnected: "the user closed the chat",
  snapshot: "one snapshot was requested",
  data_error: "live tag data could not be read",
};

// Compact end-of-watch summary for the model (it writes the CEO answer from this).
export function summaryText({ spec, reason, polls, durationMs, rows, changes, unmatched }) {
  const lines = [
    `Watch ended: ${reason} (${REASON_TEXT[reason.split(":")[0]] || "ended"}). Watched ${rows.length} tag(s) for ${Math.round(durationMs / 1000)}s, ${polls} poll(s) every ${spec.interval_s}s.` +
      (spec.stop_when ? ` stop_when=${spec.stop_when}.` : "") + (spec.compare !== "none" ? ` compare=${spec.compare}.` : ""),
  ];
  if (unmatched?.length) lines.push(`No tag matched: ${unmatched.join(", ")}.`);
  if ((spec.compare === "peers" || spec.compare === "both") && rows.length && rows.every((r) => r.vs_pen_pct === undefined)) {
    lines.push("vs pen: not comparable (pen median 15-min motion is 0, the whole pen is resting); same rule as the Insights risk score.");
  }
  lines.push("tag | animal | pen, park | state | +motion since start | still min | 15m delta | vs own % | vs pen % | status | flags");
  for (const r of rows.slice(0, LIMITS.maxRows)) {
    lines.push([r.tag, r.animal || "-", [r.pen, r.park].filter(Boolean).join(", ") || "unmapped", r.state_label, r.delta_since_start ?? "-", r.still_min,
      r.motion_delta_15m ?? "-", r.vs_own_pct ?? "-", r.vs_pen_pct ?? "-", r.status, r.flags.join("; ") || "-"].join(" | "));
  }
  if (changes.length) {
    lines.push(`Changes (${changes.length}, latest last):`);
    for (const c of changes.slice(-25)) lines.push(`- ${c.at_min} min: ${c.text}`);
  } else lines.push("No changes during the watch.");
  lines.push("The CEO already saw the live table; answer in 2-4 short sentences: what happened, who needs attention, and why the watch ended.");
  return lines.join("\n");
}

// One active watch per chat, in-process.
export function createWatchRegistry() {
  const active = new Map(); // chatId -> {stop(reason)}
  return {
    has: (chatId) => active.has(chatId),
    set: (chatId, h) => active.set(chatId, h),
    delete: (chatId) => active.delete(chatId),
    stop(chatId, reason = "stopped") { const h = active.get(chatId); if (h) h.stop(reason); return Boolean(h); },
    size: () => active.size,
  };
}


const defaultSleep = (ms, signal) => new Promise((resolve) => {
  if (signal.aborted) return resolve();
  const done = () => { clearTimeout(t); resolve(); };
  const t = setTimeout(() => { signal.removeEventListener("abort", done); resolve(); }, ms);
  signal.addEventListener("abort", done, { once: true });
});

// Runs one watch to completion. Never throws for data errors: a failed poll is reported and
// retried next tick; 3 in a row end the watch (reason data_error).
//   signal      the request's abort (client disconnect, or Stop which aborts the whole answer)
//   stopReason  () => "stop_pressed" | null, to tell Stop from a closed tab
//   onHandle    receives {stop(reason)} before the first poll ("Stop watching": ends only
//               the watch; the model still writes the summary answer)
export async function runWatch({
  args, runSql, send, signal, tenantId, log = () => {}, now = Date.now, sleep = defaultSleep,
  stopReason = () => null, onHandle = () => {}, allowAllTenants = false,
}) {
  const spec = parseWatchArgs(args);
  const watchId = `w_${Math.random().toString(36).slice(2, 10)}`;
  const t0 = now();
  const endAt = t0 + spec.minutes * 60_000; // minutes is clamped to <= 30: the hard cap
  const local = new AbortController();
  let localReason = null;
  const onAbort = () => local.abort();
  if (signal) { if (signal.aborted) local.abort(); else signal.addEventListener("abort", onAbort, { once: true }); }
  onHandle({ id: watchId, stop: (r) => { localReason = r || "stopped"; local.abort(); } });
  const sse = (obj) => { if (signal?.aborted) return; try { send({ type: "watch", watch_id: watchId, ...obj }); } catch {} };

  let polls = 0;
  let failures = 0;
  let tracker = null;
  let startRows = new Map();
  let baselines = new Map();
  let baselineAt = -Infinity;
  let realtime = false;
  let unmatched = [];
  let rows = [];
  let label = "";
  const changes = [];
  let reason = null;

  try {
    const rt = await runSql(REALTIME_COLUMNS_SQL);
    realtime = rt.ok && parseTsv(rt.out).length === 2;
    for (;;) {
      if (local.signal.aborted) break;
      const tick = now();
      const r = await runSql(snapshotSql(tenantId, { realtime, allowAllTenants }));
      if (local.signal.aborted) break;
      if (!r.ok) {
        failures += 1;
        log(`[watch] ${watchId} poll failed (${failures}): ${String(r.out || "").slice(0, 160)}`);
        sse({ phase: "error", message: "Live tag data did not load this time; retrying." });
        if (failures >= 3) { reason = "data_error"; break; }
        await sleep(spec.interval_s * 1000, local.signal);
        continue;
      }
      failures = 0;
      polls += 1;
      const all = parseTsv(r.out).map(normalizeRow);
      if (!tracker) {
        let matched;
        ({ matched, unmatched } = matchFilter(all, spec.filter));
        startRows = new Map(matched.map((x) => [x.tag, x]));
        tracker = makeTracker({ start: tick, still_minutes: spec.still_minutes });
        label = scopeLabel(spec, matched);
        if (!matched.length) { reason = "no_matching_tags"; break; }
      }
      const watched = all.filter((x) => startRows.has(x.tag));
      if ((spec.compare === "self" || spec.compare === "both") && tick - baselineAt >= LIMITS.baselineRefreshMs) {
        const sql = baselineSql([...startRows.keys()], tenantId, { allowAllTenants });
        const b = sql ? await runSql(sql) : { ok: false };
        if (b.ok) baselines = new Map(parseTsv(b.out).map((x) => [x.tag_id, Number(x.baseline) || 0]));
        baselineAt = tick;
      }
      const pens = penMedians(all);
      const tickChanges = tracker.observe(watched, tick);
      rows = watched.map((x) => deriveRow(x, {
        now: tick, start: startRows.get(x.tag), baseline: baselines.get(x.tag) || 0,
        penMedian: x.shed_id ? pens.get(x.shed_id) : null, compare: spec.compare, stillSince: tracker.stillSince(x.tag),
      }));
      tickChanges.push(...tracker.diffRows(rows));
      const at_min = Math.round((tick - t0) / 6000) / 10;
      for (const c of tickChanges) changes.push({ ...c, at_min });
      if (changes.length > LIMITS.maxChanges) changes.splice(0, changes.length - LIMITS.maxChanges);
      sse({
        phase: polls === 1 ? "start" : "tick", label, started_at: new Date(t0).toISOString(), ends_at: new Date(endAt).toISOString(),
        interval_s: spec.interval_s, compare: spec.compare, stop_when: spec.stop_when, polls, unmatched, realtime,
        rows: rows.slice(0, LIMITS.maxRows), changes: tickChanges.map((c) => ({ ...c, at_min })),
      });
      log(`[watch] ${watchId} poll ${polls} tags=${rows.length} changes=${tickChanges.length}`);
      if (spec.minutes === 0) { reason = "snapshot"; break; }
      if (stopMet(spec.stop_when, tracker)) { reason = `stop_when_met:${spec.stop_when}`; break; }
      const left = endAt - now();
      if (left <= 1000) { reason = "time_up"; break; }
      await sleep(Math.min(spec.interval_s * 1000, left), local.signal); // the last poll lands on the deadline
    }
  } catch (e) {
    reason = reason || "data_error";
    log(`[watch] ${watchId} failed: ${e?.message || e}`);
  } finally {
    signal?.removeEventListener("abort", onAbort);
  }
  if (!reason) reason = signal?.aborted ? (stopReason() === "stop_pressed" ? "stopped" : "client_disconnected") : localReason || "stopped";
  const duration_ms = now() - t0;
  sse({ phase: "end", reason, polls, duration_ms, label, rows: rows.slice(0, LIMITS.maxRows), changes: [] });
  log(`[watch] ${watchId} ended reason=${reason} polls=${polls} duration_ms=${duration_ms}`);
  return {
    watch_id: watchId, reason, polls, duration_ms, tags: rows.length,
    text: reason === "no_matching_tags"
      ? `No live tag matched: ${spec.filter.join(", ") || "(empty filter)"}. Query public.herd_signal_tag_latest joined to goats/locations to list tagged pens, then ask which one.`
      : summaryText({ spec, reason, polls, durationMs: duration_ms, rows, changes, unmatched }),
  };
}

// ---- MCP tool wiring (server.mjs registers it next to run_sql) ----------------------------
export const WATCH_TAGS_DESCRIPTION =
  "Live-watch BLE ear-tag movement for a few minutes: the server polls the live tag table (herd_signal_tag_latest, " +
  "same data and labels as the Herd Signals Live Monitor) and streams a live table + change feed to the user's panel; " +
  "you get ONE compact summary when it ends (time up, stop_when met, user pressed Stop watching, or closed the chat). " +
  "Use for 'watch Castro 1 tags for 10 minutes', 'is A0002A moving? keep watching', 'tell me when Yashoda goats stop moving'. " +
  "minutes=0 = one snapshot now (use it for one-shot 'which goats are slower than their pen / own pace right now'). " +
  "Answer from the summary in 2-4 short sentences; do not re-query what it already says.";

export function watchTagsSchema(z) {
  return {
    filter: z.union([z.string(), z.array(z.string())]).optional()
      .describe("What to watch: pen names (e.g. 'Castro 1'), park names, tag ids (A0002A) or animal ids (G-003659); several allowed; 'all' or empty = every tagged animal"),
    minutes: z.number().optional().describe("How long to watch, default 5, max 30. 0 = single snapshot now."),
    interval_s: z.number().optional().describe("Seconds between polls, 5-30, default 10"),
    stop_when: z.enum(STOP_WHEN).optional()
      .describe("End early when: any_stops_moving / all_stop_moving (no motion for still_minutes) or any_starts_moving / all_start_moving"),
    still_minutes: z.number().optional().describe("Minutes without motion that count as 'stopped moving', default 5"),
    compare: z.enum(COMPARE).optional()
      .describe("self = vs the animal's own pace (p75 of its last 24h, as the Insights risk score); peers = vs its pen's median right now; both; none (default)"),
  };
}

// ctx (per /ask request): { send, signal, stopReason, chatId, tenantId, evCtx, run }
export const MAX_WATCHES = 4; // server-wide: each watch polls the DB every few seconds
export function watchTagsHandler({ runSql, emit = async () => {}, registry, ctx, log = () => {}, maxWatches = MAX_WATCHES }) {
  return async (args) => {
    const fail = (text) => ({ content: [{ type: "text", text }], isError: true });
    if (!ctx?.send) return fail("Live watch needs the chat stream; answer from run_sql instead.");
    if (ctx.signal?.aborted) return fail("This answer was stopped; no watch started.");
    if (registry.has(ctx.chatId)) {
      return fail("Another live watch is already running in this chat, so this one did not start. Tell the user in plain words that one live watch runs at a time per chat, and offer to start this one when the current watch ends (or they can press Stop watching). Do not mention tools.");
    }
    if (!ctx.allowAllTenants && !validTenant(ctx.tenantId)) return fail("Live watch is unavailable here (no farm selected). Answer from run_sql instead.");
    if (registry.size() >= maxWatches) {
      return fail("Too many live watches are running right now. Tell the user live watching is busy and to try again in a few minutes; answer from a one-time run_sql snapshot instead.");
    }
    const spec = parseWatchArgs(args);
    registry.set(ctx.chatId, { stop: () => {} });
    await emit("watch_started", ctx.evCtx, {
      filter: spec.filter.join(", ").slice(0, 200), minutes: spec.minutes, interval_s: spec.interval_s,
      compare: spec.compare, stop_when: spec.stop_when,
    }).catch(() => {});
    let res = null;
    try {
      res = await runWatch({
        args, runSql, send: ctx.send, signal: ctx.signal, tenantId: ctx.tenantId, allowAllTenants: Boolean(ctx.allowAllTenants), stopReason: ctx.stopReason, log,
        onHandle: (h) => { registry.set(ctx.chatId, h); if (ctx.run) ctx.run.stopWatch = h.stop; },
      });
      return { content: [{ type: "text", text: res.text }] };
    } finally {
      registry.delete(ctx.chatId);
      if (ctx.run) ctx.run.stopWatch = null;
      await emit("watch_ended", ctx.evCtx, {
        severity: res?.reason === "data_error" ? "WARNING" : "INFO",
        reason: res?.reason || "error", duration_ms: res?.duration_ms ?? null, polls: res?.polls ?? 0, tags: res?.tags ?? 0,
      }).catch(() => {});
    }
  };
}
