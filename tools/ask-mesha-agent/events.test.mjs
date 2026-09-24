// node --test tools/ask-mesha-agent/events.test.mjs
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { classifyFailure, createEvents, percentile, recentAsks, summarizeUsers } from "./events.mjs";

const NOW = new Date("2026-09-24T12:00:00Z");
const ago = (h) => new Date(NOW.getTime() - h * 3600e3).toISOString();

test("percentile is nearest-rank and ignores non-numbers", () => {
  assert.equal(percentile([], 50), null);
  assert.equal(percentile([5], 90), 5);
  assert.equal(percentile([1, 2, 3, 4, 5, 6, 7, 8, 9, 10], 50), 5);
  assert.equal(percentile([10, 1, 9, 2, 8, 3, 7, 4, 6, 5], 90), 9);
  assert.equal(percentile([null, 3, undefined, 1, NaN], 50), 1);
});

test("classifyFailure maps errors to classes", () => {
  const cases = {
    error_max_budget_usd: "per_answer_cap",
    error_during_execution: "sdk_error",
    "Claude Code process exited with code 1": "sdk_error",
    "missing /x/.pgenv; create it": "db_error",
    'relation "ask_mesha.chats" does not exist': "db_error",
    "Request timed out": "timeout",
    leadership_required: "auth",
    "monthly budget reached": "budget_blocked",
    client_aborted: "client_aborted",
    "something odd": "unknown",
    "": "unknown",
  };
  for (const [err, cls] of Object.entries(cases)) assert.equal(classifyFailure(err), cls, err);
  assert.equal(classifyFailure("anything", { aborted: true }), "client_aborted");
});

test("summarizeUsers rolls up per email over today/7d/30d", () => {
  const ev = [
    { event_name: "ask_started", email: "a@m", ts: ago(1) }, // non-terminal: ignored
    { event_name: "ask_completed", email: "a@m", ts: ago(1), total_ms: 1000, first_token_ms: 100, tool_calls: 2, cost_usd: 0.1 },
    { event_name: "ask_completed", email: "a@m", ts: ago(2), total_ms: 3000, first_token_ms: 300, tool_calls: 4, cost_usd: 0.2 },
    { event_name: "ask_failed", email: "a@m", ts: ago(3), error_class: "sdk_error", tool_calls: 0 },
    { event_name: "ask_stopped", email: "a@m", ts: ago(24 * 3), total_ms: 500 },
    { event_name: "ask_failed", email: "a@m", ts: ago(24 * 20), error_class: "budget_blocked" },
    { event_name: "ask_completed", email: "a@m", ts: ago(24 * 40), total_ms: 1 }, // outside 30d
    { event_name: "ask_failed", email: "b@m", ts: ago(1), error_class: "auth" },
  ];
  const { users } = summarizeUsers(ev, NOW);
  assert.deepEqual(users.map((u) => u.email), ["a@m", "b@m"]);
  const a = users[0];
  assert.equal(a.today.asks, 3);
  assert.equal(a.today.success, 2);
  assert.equal(a.today.failed, 1);
  assert.deepEqual(a.today.failed_by_class, { sdk_error: 1 });
  assert.equal(a.today.success_rate, 0.667);
  assert.deepEqual(a.today.total_ms, { p50: 1000, p90: 3000 });
  assert.deepEqual(a.today.first_token_ms, { p50: 100, p90: 300 });
  assert.equal(a.today.avg_tools, 2);
  assert.equal(a.today.cost_usd, 0.3);
  assert.equal(a["7d"].asks, 4);
  assert.equal(a["7d"].stopped, 1);
  assert.equal(a["30d"].asks, 5);
  assert.deepEqual(a["30d"].failed_by_class, { sdk_error: 1, budget_blocked: 1 });
  assert.equal(users[1].today.success_rate, 0);
});

test("recentAsks joins the question preview and orders newest first", () => {
  const ev = [
    { event_name: "ask_started", request_id: "r1", email: "a@m", ts: ago(2), question_preview: "q1", model: "m" },
    { event_name: "ask_completed", request_id: "r1", email: "a@m", ts: ago(2), total_ms: 10 },
    { event_name: "ask_stopped", request_id: "r2", email: "a@m", ts: ago(1), total_ms: 5, question_preview: "q2" },
    { event_name: "ask_failed", request_id: "r3", email: "b@m", ts: ago(0.5), error_class: "auth" },
  ];
  const r = recentAsks(ev, "a@m");
  assert.deepEqual(r.map((x) => [x.request_id, x.status, x.question_preview]), [["r2", "stopped", "q2"], ["r1", "success", "q1"]]);
  assert.equal(r[1].model, "m");
  assert.equal(recentAsks(ev, "", 1)[0].status, "failed");
});

test("tracker emits one structured line per event and a single terminal event", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ask-ev-"));
  const lines = [];
  const ev = await createEvents({ stateDir: dir, log: (l) => lines.push(JSON.parse(l)) });
  const ctx = { request_id: "r", chat_id: "c", email: "a@m", tenant_id: "t" };
  const t = ev.tracker(ctx, { question: "x".repeat(100), model: "claude-sonnet-5", effort: "low" });
  t.firstToken(); t.firstToken();
  t.onMessage({ type: "assistant", message: { content: [{ type: "tool_use", id: "u1", name: "mcp__mesha__run_sql", input: { sql: "select 1" } }] } }, () => "Checking the records");
  t.onMessage({ type: "user", message: { content: [{ type: "tool_result", tool_use_id: "u1", is_error: true, content: [{ type: "text", text: "syntax error" }] }] } });
  t.setAnswer("hello", { type: "bar" });
  await t.finish({ ok: true, error: null, total_ms: 42, tool_calls: 1, db_queries: 1, turns: 2, model: "claude-sonnet-5", cost_usd: 0.01 });
  await t.finish({ ok: true }); // idempotent
  await new Promise((r) => setTimeout(r, 20));
  const names = lines.map((l) => l.event_name);
  assert.deepEqual(names, ["ask_started", "ask_first_token", "ask_tool", "ask_completed"]);
  for (const l of lines) {
    for (const k of ["severity", "message", "event_name", "ts", "request_id", "chat_id", "email", "tenant_id"]) assert.ok(k in l, `${l.event_name} lacks ${k}`);
  }
  assert.equal(lines[0].question_preview, undefined); // never in stdout (Cloud Logging)
  const tool = lines[2];
  assert.equal(tool.ok, false); assert.equal(tool.label, "Checking the records"); assert.equal(typeof tool.duration_ms, "number");
  const done = lines[3];
  assert.equal(done.sql_errors, 1); assert.equal(done.chart, true); assert.equal(done.answer_chars, 5);
  const persisted = fs.readFileSync(path.join(dir, "events.jsonl"), "utf8").trim().split("\n");
  assert.equal(persisted.length, 4);
  const s = await ev.usersSummary();
  assert.equal(s.users[0].email, "a@m");
});

test("tracker finish classifies stops and failures; budget + auth helpers", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ask-ev-"));
  const lines = [];
  const ev = await createEvents({ stateDir: dir, log: (l) => lines.push(JSON.parse(l)) });
  await ev.tracker({ email: "a@m" }, {}).finish({ error: "client_aborted" }, { aborted: true });
  await ev.tracker({ email: "a@m" }, {}).finish({ ok: false, error: "error_max_budget_usd" });
  ev.budgetCheck({ email: "a@m" }, 85, 100);
  ev.budgetCheck({ email: "a@m" }, 90, 100); // once per month
  await ev.budgetBlocked({ email: "a@m" }, 100, 100, "q");
  await ev.authDenied({}, "/ceo-ai/ask");
  await new Promise((r) => setTimeout(r, 20));
  const terminal = lines.filter((l) => ["ask_stopped", "ask_failed"].includes(l.event_name)).map((l) => `${l.event_name}:${l.error_class}`);
  assert.deepEqual(terminal, ["ask_stopped:client_aborted", "ask_failed:per_answer_cap", "ask_failed:budget_blocked", "ask_failed:auth"]);
  assert.equal(lines.filter((l) => l.event_name === "budget_warning").length, 1);
  assert.equal(lines.find((l) => l.event_name === "ask_failed").severity, "ERROR");
});

test("ask_stopped reason distinguishes Stop from a closed tab", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ask-ev-"));
  const lines = [];
  const ev = await createEvents({ stateDir: dir, log: (l) => lines.push(JSON.parse(l)) });
  await ev.tracker({ email: "a@m" }, {}).finish({ error: "client_aborted" }, { aborted: true, stopReason: "stop_pressed" });
  await ev.tracker({ email: "a@m" }, {}).finish({ error: "client_aborted" }, { aborted: true });
  const reasons = lines.filter((l) => l.event_name === "ask_stopped").map((l) => l.reason);
  assert.deepEqual(reasons, ["stop_pressed", "client_closed"]);
});
