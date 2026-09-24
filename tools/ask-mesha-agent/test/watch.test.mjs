import { test } from "node:test";
import assert from "node:assert/strict";
import {
  parseWatchArgs, matchFilter, deriveRow, penMedians, makeTracker, stopMet, runWatch, snapshotSql,
  baselineSql, parseTsv, normalizeRow, createWatchRegistry, LIMITS,
} from "../watch.mjs";

const HEAD = "tag_id\tgoat_id\tdisplay_id\tshed_id\tpen\tpark\tmovement_state\tpattern_state\tlive_state\tmotion_count\tmotion_delta\tmotion_window_seconds\tgap_delta\tlast_seen_at\tlast_seen_s\tlast_rssi_dbm\tbattery_mv\tbattery_state\tmapping_state";
const line = (o) => [o.tag, "g-" + o.tag, o.animal || "G-" + o.tag, o.shed || "s1", o.pen || "Yashoda", o.park || "Channapatna",
  o.state || "low", o.pattern || "normal", "", o.count, o.delta ?? 10, 900, "f", "2026-09-24T07:00:00Z", o.age ?? 5, o.rssi ?? -50, o.mv ?? 3000, o.bat || "healthy", "mapped"].join("\t");
const tsv = (rows) => [HEAD, ...rows.map(line)].join("\n");

test("parseWatchArgs clamps minutes/interval and validates enums", () => {
  const s = parseWatchArgs({ filter: "Castro 1, A0002A", minutes: 99, interval_s: 1, stop_when: "nope", compare: "both" });
  assert.deepEqual(s.filter, ["Castro 1", "A0002A"]);
  assert.equal(s.minutes, LIMITS.minutesMax);
  assert.equal(s.interval_s, 5);
  assert.equal(s.stop_when, null);
  assert.equal(s.compare, "both");
  assert.equal(parseWatchArgs({}).minutes, 5);
  assert.equal(parseWatchArgs({ minutes: 0 }).minutes, 0);
  assert.equal(parseWatchArgs({ interval_s: 99 }).interval_s, 30);
});

test("matchFilter: exact ids, pen names either way round, most specific pen", () => {
  const rows = [
    { tag: "A1", animal: "G-1", pen: "Castro 1", park: "CBE" },
    { tag: "A2", animal: "G-2", pen: "Castro 10", park: "CBE" },
    { tag: "A3", animal: "G-3", pen: "Yashoda", park: "Channapatna" },
  ];
  assert.deepEqual(matchFilter(rows, ["Castro 1"]).matched.map((r) => r.tag), ["A1"]);
  assert.deepEqual(matchFilter(rows, ["Yashoda 3"]).matched.map((r) => r.tag), ["A3"]);
  assert.deepEqual(matchFilter(rows, ["g-2"]).matched.map((r) => r.tag), ["A2"]);
  assert.deepEqual(matchFilter(rows, ["all"]).matched.length, 3);
  assert.deepEqual(matchFilter(rows, ["Godel"]).unmatched, ["Godel"]);
});

test("deriveRow: screen status precedence and own/pen pace like applyRiskSignals", () => {
  const base = { tag: "A1", motion_count: 10, motion_delta: 0, window_s: 900, gap_delta: false, rssi: -80, battery_mv: 2700, battery_state: "low", movement_state: "not_moving", last_seen_s: 10 };
  const r = deriveRow(base, { now: 0, start: { motion_count: 5 }, baseline: 10, penMedian: 20, compare: "both", stillSince: 0 });
  assert.equal(r.status, "Weak signal"); // weak outranks low battery
  assert.equal(r.vs_own_pct, -100); // baseline 10 per 300s -> 30 per 900s window
  assert.equal(r.own_baseline_window, 30);
  assert.equal(r.vs_pen_pct, -100);
  assert.deepEqual(r.flags, ["far below own pace", "lower than pen"]);
  assert.equal(r.delta_since_start, 5);
  const stale = deriveRow({ ...base, movement_state: "stale", last_seen_s: 3600 }, { now: 0, compare: "none" });
  assert.equal(stale.status, "Missing signal");
  assert.equal(stale.state_label, "Stale");
  assert.equal(stale.vs_own_pct, undefined);
  const spike = deriveRow({ ...base, motion_delta: 100, rssi: -50, battery_mv: 3000, battery_state: "healthy" }, { now: 0, baseline: 10, compare: "self" });
  assert.deepEqual(spike.flags, ["spike vs own pace"]);
  assert.equal(spike.status, "Good");
});

test("penMedians ignores gap deltas and unmapped tags", () => {
  const m = penMedians([
    { shed_id: "s", motion_delta: 10 }, { shed_id: "s", motion_delta: 30 }, { shed_id: "s", motion_delta: 999, gap_delta: true },
    { shed_id: null, motion_delta: 5 },
  ]);
  assert.equal(m.get("s"), 20);
  assert.equal(m.size, 1);
});

test("tracker: hasn't-moved and started-moving lines, stop_when", () => {
  const tr = makeTracker({ start: 0, still_minutes: 5 });
  const r = (count) => [{ tag: "A1", animal: "G-1", pen: "Yashoda", motion_count: count }];
  assert.deepEqual(tr.observe(r(100), 0), []);
  assert.deepEqual(tr.observe(r(100), 4 * 60_000), []);
  const c1 = tr.observe(r(100), 5 * 60_000);
  assert.match(c1[0].text, /A1 \(G-1\) in Yashoda hasn't moved for 5 min/);
  assert.ok(stopMet("any_stops_moving", tr));
  assert.ok(!stopMet("any_starts_moving", tr));
  const c2 = tr.observe(r(112), 6 * 60_000);
  assert.match(c2[0].text, /started moving \(\+12\) after 6 min still/);
  assert.ok(stopMet("any_starts_moving", tr));
  assert.ok(!stopMet("all_stop_moving", tr));
});

test("sql builders only inline validated values", () => {
  assert.match(snapshotSql("5d4c7b3a-1111-2222-3333-444455556666"), /tl\.tenant_id = '5d4c7b3a-/);
  assert.doesNotMatch(snapshotSql("x' OR 1=1 --"), /OR 1=1/);
  assert.match(snapshotSql("", { realtime: true }), /moving_now/);
  assert.equal(baselineSql(["A1'; drop"]), "");
  assert.match(baselineSql(["A0002A"]), /percentile_disc\(0\.75\)/);
  assert.equal(parseTsv(tsv([{ tag: "A1", count: 1 }])).length, 1);
  assert.equal(normalizeRow(parseTsv(tsv([{ tag: "A1", count: 7 }]))[0]).motion_count, 7);
});

// Fake clock + DB: sleep advances time instantly.
function harness(counts, { failAll = false } = {}) {
  let t = 1_000_000;
  let poll = 0;
  const sent = [];
  const sqls = [];
  const runSql = async (sql) => {
    sqls.push(sql);
    if (sql.includes("information_schema")) return { ok: true, out: "column_name" };
    if (sql.includes("percentile_disc")) return { ok: true, out: "tag_id\tbaseline\nA1\t10" };
    if (failAll) return { ok: false, out: "boom" };
    const c = counts[Math.min(poll, counts.length - 1)];
    poll += 1;
    return { ok: true, out: tsv([{ tag: "A1", count: c }, { tag: "B1", count: 50, pen: "Other", shed: "s2" }]) };
  };
  const sleep = async (ms, signal) => { if (!signal.aborted) t += ms; };
  return { runSql, sleep, now: () => t, sent, send: (e) => sent.push(e), sqls, advance: (ms) => { t += ms; } };
}

test("runWatch: streams start/tick/end, ends at time_up, summary for the model", async () => {
  const h = harness([100, 100, 105]);
  const res = await runWatch({ args: { filter: ["Yashoda"], minutes: 1, interval_s: 30 }, ...h });
  assert.equal(res.reason, "time_up");
  assert.equal(res.polls, 3);
  assert.deepEqual(h.sent.map((e) => e.phase), ["start", "tick", "tick", "end"]);
  assert.ok(h.sent.every((e) => e.type === "watch" && e.watch_id === res.watch_id));
  assert.equal(h.sent[0].rows.length, 1); // B1 is in another pen
  assert.equal(h.sent[2].rows[0].delta_since_start, 5);
  assert.match(res.text, /Watch ended: time_up/);
  assert.match(res.text, /A1 \| G-A1 \| Yashoda, Channapatna/);
});

test("runWatch: minutes=0 is a one-shot snapshot", async () => {
  const h = harness([1]);
  const res = await runWatch({ args: { filter: "all", minutes: 0, compare: "both" }, ...h });
  assert.equal(res.reason, "snapshot");
  assert.equal(res.polls, 1);
  assert.equal(h.sent[0].rows.length, 2);
  assert.ok(h.sqls.some((s) => s.includes("percentile_disc")));
});

test("runWatch: stop_when met ends early", async () => {
  const h = harness([100, 100, 100, 100, 100, 100, 100, 100]);
  const res = await runWatch({ args: { filter: "A1", minutes: 30, interval_s: 30, stop_when: "all_stop_moving", still_minutes: 1 }, ...h });
  assert.equal(res.reason, "stop_when_met:all_stop_moving");
  assert.ok(res.polls <= 4);
});

test("runWatch: client disconnect stops polling immediately", async () => {
  const h = harness([1]);
  const ac = new AbortController();
  let polls = 0;
  const runSql = async (sql) => { const r = await h.runSql(sql); if (sql.includes("herd_signal_tag_latest tl")) { polls += 1; if (polls === 2) ac.abort(); } return r; };
  const res = await runWatch({ args: { filter: "A1", minutes: 30, interval_s: 5 }, ...h, runSql, signal: ac.signal });
  assert.equal(res.reason, "client_disconnected");
  assert.equal(polls, 2);
  assert.ok(!h.sent.some((e) => e.phase === "end")); // nothing written to a closed stream
});

test("runWatch: Stop watching ends only the watch; stop_pressed abort is 'stopped'", async () => {
  const h = harness([1]);
  let handle;
  const realSleep = (ms, signal) => new Promise((r) => { const t = setTimeout(r, 50); signal.addEventListener("abort", () => { clearTimeout(t); r(); }, { once: true }); });
  const p = runWatch({ args: { filter: "A1", minutes: 30 }, ...h, sleep: realSleep, onHandle: (x) => { handle = x; } });
  await new Promise((r) => setTimeout(r, 10));
  handle.stop("stopped");
  const res = await p;
  assert.equal(res.reason, "stopped");
  assert.equal(h.sent.at(-1).phase, "end");

  const ac = new AbortController();
  const p2 = runWatch({ args: { filter: "A1", minutes: 30 }, ...harness([1]), sleep: realSleep, signal: ac.signal, stopReason: () => "stop_pressed" });
  await new Promise((r) => setTimeout(r, 10));
  ac.abort();
  assert.equal((await p2).reason, "stopped");
});

test("runWatch: no matching tags and repeated data errors", async () => {
  const none = await runWatch({ args: { filter: "Godel 9" }, ...harness([1]) });
  assert.equal(none.reason, "no_matching_tags");
  assert.match(none.text, /No live tag matched: Godel 9/);
  const bad = await runWatch({ args: { filter: "A1" }, ...harness([1], { failAll: true }) });
  assert.equal(bad.reason, "data_error");
});

test("registry: one watch per chat", () => {
  const reg = createWatchRegistry();
  let stopped = null;
  reg.set("c1", { stop: (r) => { stopped = r; } });
  assert.ok(reg.has("c1"));
  assert.ok(reg.stop("c1", "stopped"));
  assert.equal(stopped, "stopped");
  assert.ok(!reg.stop("c2"));
});

test("watchTagsHandler: events, one watch per chat, Stop watching via run.stopWatch", async () => {
  const { watchTagsHandler } = await import("../watch.mjs");
  const reg = createWatchRegistry();
  const evs = [];
  const h = harness([1]);
  const run = {};
  const ctx = { send: h.send, chatId: "c1", evCtx: { chat_id: "c1" }, run, stopReason: () => null };
  const handler = watchTagsHandler({ runSql: h.runSql, emit: async (n, c, f) => evs.push([n, f]), registry: reg, ctx });
  const snap = await handler({ filter: "A1", minutes: 0 });
  assert.match(snap.content[0].text, /Watch ended: snapshot/);
  assert.deepEqual(evs.map((e) => e[0]), ["watch_started", "watch_ended"]);
  assert.equal(evs[1][1].reason, "snapshot");
  assert.ok(!reg.has("c1"));

  const p = handler({ filter: "A1", minutes: 30, interval_s: 5 });
  await new Promise((r) => setTimeout(r, 20));
  const busy = await handler({ filter: "A1" });
  assert.equal(busy.isError, true);
  assert.equal(typeof run.stopWatch, "function");
  run.stopWatch("stopped");
  const done = await p;
  assert.match(done.content[0].text, /Watch ended: stopped/);
  assert.equal(evs.at(-1)[1].reason, "stopped");
});

test("per-user summary counts watches", async () => {
  const { summarizeUsers } = await import("../events.mjs");
  const ts = new Date().toISOString();
  const s = summarizeUsers([
    { event_name: "ask_completed", email: "ceo@x", ts, total_ms: 1 },
    { event_name: "watch_started", email: "ceo@x", ts },
    { event_name: "watch_ended", email: "ceo@x", ts, reason: "time_up" },
  ]);
  assert.equal(s.users[0].today.watches, 1);
  assert.equal(s.users[0].today.asks, 1);
});

test("diffRows: pen comparison going away is not reported as recovery", () => {
  const tr = makeTracker({ start: 0 });
  tr.observe([{ tag: "A1", motion_count: 1 }], 0);
  const row = (extra) => ({ tag: "A1", state: "low", state_label: "Low", status: "Good", flags: [], ...extra });
  tr.diffRows([row({ vs_pen_pct: -90, flags: ["lower than pen"] })]);
  assert.deepEqual(tr.diffRows([row({})]), []); // pen median fell to 0: no comparison
  tr.diffRows([row({ vs_pen_pct: -90, flags: ["lower than pen"] })]);
  assert.match(tr.diffRows([row({ vs_pen_pct: -10 })])[0].text, /no longer lower than pen/);
});
