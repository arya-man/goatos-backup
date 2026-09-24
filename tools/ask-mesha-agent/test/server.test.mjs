import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
  makeChartFilter, extractChart, toolLabel, validateReadSql, describeTableSql, kindValuesSql, relInfoSql, shouldSampleKinds, clipSqlOutput, isDeepQuestion,
  answerCapUsd, answerCostUsd, friendlyError, STOPPED_NOTE, historyPreamble, pathAllowed, ttlCache,
  istNowNote, attachmentKind, attachmentPrompt,
} from "../lib.mjs";
import { loadJsonDb, readJsonl, LOCK_MS } from "../store.mjs";

const run = (chunks) => {
  let out = "";
  const f = makeChartFilter((t) => (out += t));
  for (const c of chunks) f(c);
  f("", true);
  return out;
};

test("chart filter hides a fence split across every chunk boundary", () => {
  const text = 'Sold 42 goats.\n```chart\n{"type":"bar","x":["a","b"],"series":[]}\n```\nDone.';
  for (let i = 1; i < text.length; i++) {
    for (const size of [1, 3, 7]) {
      const chunks = [];
      for (let j = 0; j < text.length; j += size) chunks.push(text.slice(j, j + size));
      assert.equal(run(chunks), "Sold 42 goats.\n\nDone.");
    }
    assert.equal(run([text.slice(0, i), text.slice(i)]), "Sold 42 goats.\n\nDone.");
  }
});
test("chart filter passes non-chart code fences and flushes trailing text", () => {
  assert.equal(run(["a ``", "`sql x``", "` b"]), "a ```sql x``` b");
  assert.equal(run(["tail ``"]), "tail ``");
  assert.equal(run(["x ```chart {unterminated"]), "x ");
});

test("extractChart", () => {
  const r = extractChart('Answer\n```chart\n{"type":"line","x":["w1","w2"],"series":[{"name":"a","data":[1,2]}]}\n```');
  assert.equal(r.clean, "Answer");
  assert.equal(r.chart.type, "line");
  assert.equal(extractChart("```chart\n{bad json}\n```\nhi").chart, undefined);
  assert.equal(extractChart('x ```chart {"x":["only"],"series":[]}```').chart, undefined); // < 2 labels
  assert.equal(extractChart('Half answer ```chart {"type":"bar"').clean, "Half answer"); // cut mid-chart
  assert.equal(extractChart("plain").clean, "plain");
});

test("toolLabel is business wording (no SQL/paths)", () => {
  assert.equal(toolLabel("mcp__mesha__run_sql", { sql: "select * from ceo_ai.weighing_daily" }), "Checking weighing records");
  assert.equal(toolLabel("mcp__mesha__run_sql", { sql: "select price from public.feed_purchases" }), "Checking feed records");
  assert.equal(toolLabel("mcp__mesha__run_sql", { sql: "select 1" }), "Checking the records");
  assert.equal(toolLabel("Read", { file_path: "/repo/.agents/skills/mesha-data-map/SKILL.md" }), "Using the Mesha data map");
  assert.equal(toolLabel("Read", { file_path: "/tmp/ask-mesha/c/1-shot.png" }), "Looking at your screenshot");
  assert.equal(toolLabel("Grep", { pattern: "adg" }), "Looking up how daily gain is worked out");
  assert.equal(toolLabel("Skill", {}), "Using the Mesha data map");
  assert.equal(toolLabel("Whatever", null), "Working");
  for (const l of [toolLabel("Read", { file_path: "/repo/x/y.go" }), toolLabel("Glob", { pattern: "**/*.ts" })])
    assert.doesNotMatch(l, /sql|\.go|\.ts|\/|query|code/i);
});

test("validateReadSql only refuses backslash commands", () => {
  assert.equal(validateReadSql("").ok, false);
  assert.equal(validateReadSql("\\! rm -rf /").ok, false);
  assert.equal(validateReadSql("select E'\\n'").ok, false);
  const ok = validateReadSql("  SELECT * FROM public.feed_purchases;  ");
  assert.deepEqual(ok, { ok: true, sql: "SELECT * FROM public.feed_purchases" });
  assert.equal(validateReadSql("delete from x").ok, true); // the DB role/READ ONLY txn refuses, not us
});

test("clipSqlOutput bounds rows and characters", () => {
  const rows = ["h", ...Array.from({ length: 600 }, (_, i) => `r${i}`)].join("\n") + "\n";
  const c = clipSqlOutput(rows);
  assert.match(c, /100 more rows truncated/);
  assert.equal(c.split("\n").length, 502);
  const wide = clipSqlOutput("h\n" + "x".repeat(200_000));
  assert.ok(wide.length < 100_200);
  assert.match(wide, /clipped/);
  assert.equal(clipSqlOutput("a\tb\n1\t2\n"), "a\tb\n1\t2");
});

test("deep routing", () => {
  for (const q of ["Why is ADG down?", "deep: sales", "Is this correct?", "verify the feed cost",
    "The number doesn’t match the dashboard", "check if pen 4 was weighed", "explain the mortality rate",
    "sales seem off this week", "how is ADG calculated"]) assert.ok(isDeepQuestion(q), q);
  for (const q of ["How many animals were weighed this month, by park?", "Compare sales by park this month",
    "Can you check how many goats we sold?", "Which pens are behind on weighing verification?",
    "Show the weekly ADG trend for the last 8 weeks as a chart"]) assert.ok(!isDeepQuestion(q), q);
  assert.ok(isDeepQuestion("how many", [{ name: "a.png" }]));
});

test("budget math", () => {
  const base = { perAnswer: 1, deepAnswer: 5, monthly: 100 };
  assert.equal(answerCapUsd({ ...base, deep: false, spent: 10 }), 1);
  assert.equal(answerCapUsd({ ...base, deep: true, spent: 10 }), 5);
  assert.equal(answerCapUsd({ ...base, deep: true, spent: 98 }), 2);
  assert.equal(answerCapUsd({ ...base, deep: true, spent: Infinity }), 0.01);
  assert.equal(answerCostUsd(0.5, 0), 0.5);
  assert.equal(answerCostUsd(1.25, 1), 0.25); // resumed session: cumulative total
  assert.equal(answerCostUsd(0.3, 1), 0); // earlier aborted-turn estimate already counted
  assert.equal(answerCostUsd(null, 1), null);
});

test("CEO-facing messages never mention internals", () => {
  for (const m of [friendlyError("error_max_budget_usd"), friendlyError("error_max_turns"), friendlyError("Claude Code process exited with code 1"), friendlyError("busy"), STOPPED_NOTE])
    assert.doesNotMatch(m, /budget|token|tool|session|sql|query|code|agent|database/i);
});

test("historyPreamble", () => {
  assert.equal(historyPreamble([]), "");
  const h = historyPreamble([{ role: "user", content: "q1" }, { role: "assistant", content: "a1" }, { role: "system", content: "x" }]);
  assert.match(h, /User: q1\n\nAssistant: a1/);
  assert.doesNotMatch(h, /x\n/);
});

test("pathAllowed", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "am-"));
  fs.mkdirSync(path.join(root, "repo/.agents/skills"), { recursive: true });
  fs.writeFileSync(path.join(root, "secret"), "x");
  fs.symlinkSync(path.join(root, "secret"), path.join(root, "repo/link"));
  const repo = path.join(root, "repo");
  assert.ok(pathAllowed(".agents/skills", repo, [repo]));
  assert.ok(!pathAllowed("../secret", repo, [repo]));
  assert.ok(!pathAllowed("link", repo, [repo])); // symlink out of the repo
  assert.ok(!pathAllowed(repo + "x", repo, [repo])); // sibling prefix
  fs.rmSync(root, { recursive: true, force: true });
});

test("ttlCache prunes and bounds size", () => {
  const c = ttlCache(3);
  c.set("a", 1, -1);
  assert.equal(c.get("a"), undefined);
  for (const k of ["b", "c", "d", "e"]) c.set(k, k, 60_000);
  assert.ok(c.size <= 3);
  assert.equal(c.get("e"), "e");
});

test("describeTableSql accepts only plain schema.table identifiers", () => {
  const d = describeTableSql("public.pc_care_tasks");
  assert.equal(d.ok, true);
  assert.match(d.sql, /nspname = 'public' AND c\.relname = 'pc_care_tasks'/);
  assert.equal(describeTableSql("audit_log").schema, "public");
  for (const bad of ["x'; drop table y; --", "a.b.c", "public.t\\x", "", "public.1abc", "pg_catalog.pg_class;select 1"]) {
    assert.equal(describeTableSql(bad).ok, false, bad);
  }
});

test("kindValuesSql only samples category/status-like identifier columns", () => {
  assert.equal(kindValuesSql("public", "t", ["name", "notes"]), "");
  const sql = kindValuesSql("public", "pc_care_tasks", ["category", "status", "notes", "bad;col_status"]);
  assert.match(sql, /'category'/);
  assert.match(sql, /'status'/);
  assert.doesNotMatch(sql, /notes|bad;/);
  assert.match(sql, /LIMIT 200000/);
});

test("describe_table has a plain-English step label", () => {
  assert.equal(toolLabel("mcp__mesha__describe_table", { table: "public.pc_care_tasks" }), "Checking what the preventive care records hold");
});

test("validateReadSql refuses multi-statement and transaction/session control", () => {
  assert.equal(validateReadSql("select 1;").ok, true);
  assert.equal(validateReadSql("select 1;").sql, "select 1");
  assert.equal(validateReadSql("with a as (select 1) select * from a").ok, true);
  for (const bad of ["commit; set default_transaction_read_only=off; delete from x", "select 1; select 2",
    "COMMIT", "rollback", "end", "abort", "SET default_transaction_read_only = off", "reset all", "begin", "start transaction"]) {
    assert.equal(validateReadSql(bad).ok, false, bad);
  }
});

test("answerCapUsd subtracts caps of answers already in flight", () => {
  const base = { perAnswer: 1, deepAnswer: 5, monthly: 100 };
  assert.equal(answerCapUsd({ ...base, deep: true, spent: 90, inFlight: 0 }), 5);
  assert.equal(answerCapUsd({ ...base, deep: true, spent: 90, inFlight: 7 }), 3);
  assert.equal(answerCapUsd({ ...base, deep: true, spent: 90, inFlight: 20 }), 0.01);
});

test("describe_table samples values only on base tables of sane size", () => {
  assert.equal(shouldSampleKinds("r", "440634"), true);
  assert.equal(shouldSampleKinds("v", "10"), false);
  assert.equal(shouldSampleKinds("r", "5000000"), false);
  assert.equal(relInfoSql("public", "x;y"), "");
});

test("server source: budget pause wording and file headers", () => {
  const src = fs.readFileSync(new URL("../server.mjs", import.meta.url), "utf8");
  assert.doesNotMatch(src, /ask Ravi|Ask Mesha budget/);
  assert.match(src, /Ask Mesha is paused for this month\. Please contact the Mesha team\./);
  assert.match(src, /filename\*=UTF-8''\$\{encodeURIComponent/);
  assert.match(src, /"X-Content-Type-Options": "nosniff"/);
  assert.match(src, /finalAnswerCost\(\{ costUsd: metric\.cost_usd, started, capUsd/); // no-result runs count the cap
});

test("deep routing: recorded-reason lookups stay fast", async () => {
  const { isDeepQuestion: deep } = await import("../lib.mjs");
  for (const q of ["who rejected shifting approvals and why", "pen visits why delayed, how many",
    "pen visits delayed, with reasons", "which RFID goats are not moving"]) assert.ok(!deep(q), q);
  for (const q of ["why is this failing we have done deworming", "Why are sales down this month?",
    "why is the number of pending visits showing 40", "why?", "dig deeper", "check again please"]) assert.ok(deep(q), q);
});

test("describe_table batching and missing-column hints", async () => {
  const { describeTableNames, sqlTableRefs, isMissingColumnError, describeTableSql: d } = await import("../lib.mjs");
  assert.deepEqual(describeTableNames("public.a, public.b public.a"), ["public.a", "public.b"]);
  assert.equal(describeTableNames(["a", "b", "c", "d", "e", "f", "g"]).length, 6);
  assert.deepEqual(sqlTableRefs("select * from public.shift_requests s left join workforce_members m on m.id=s.x join ceo_ai.v x on true"),
    ["public.shift_requests", "public.workforce_members", "ceo_ai.v"]);
  assert.ok(isMissingColumnError('ERROR:  column m.member_id does not exist'));
  assert.ok(!isMissingColumnError("syntax error at or near filter"));
  assert.match(d("public.x").sql, /contype = 'f'/);
});

test("narration gate: pre-tool text never shown, answers released", async () => {
  const { makeTurnGate } = await import("../lib.mjs");
  let out = "";
  const g = makeTurnGate((t) => (out += t), 40);
  g.text("Let me check ");
  g.text("the table.");
  assert.equal(g.toolStart(), false); // nothing reached the screen, so no reset needed
  assert.equal(out, "");
  g.text("Short answer.");
  g.end();
  assert.equal(out, "Short answer.");
  out = "";
  g.text("A long answer that passes the forty char hold ");
  assert.ok(out.length > 0); // streams once past the hold
  g.text("and keeps streaming.");
  g.end();
  assert.equal(out, "A long answer that passes the forty char hold and keeps streaming.");
});

test("file route: only previewable types are inline", async () => {
  const { inlineDisposition } = await import("../lib.mjs");
  for (const t of ["image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "IMAGE/PNG; x=1"]) assert.ok(inlineDisposition(t), t);
  for (const t of ["text/html", "image/svg+xml", "text/plain", "", undefined, "application/octet-stream"]) assert.ok(!inlineDisposition(t), String(t));
  const src = fs.readFileSync(new URL("../server.mjs", import.meta.url), "utf8");
  assert.match(src, /inlineDisposition\(ref\.type\) \? "inline" : "attachment"/);
});

test("monthly cap: in-flight reservation is taken before any await after the budget check", () => {
  const src = fs.readFileSync(new URL("../server.mjs", import.meta.url), "utf8");
  const body = src.slice(src.indexOf("if (spent + inFlight >= MONTHLY_BUDGET_USD)"));
  const reserve = body.indexOf("activeRuns.set(requestId, run)");
  const afterBlock = body.indexOf("return;\n  }\n") + 1;
  assert.ok(reserve > 0 && body.indexOf("store.createChat") > reserve && body.indexOf("store.tryLock") > reserve);
  // No await between the end of the budget-blocked branch and the reservation.
  assert.doesNotMatch(body.slice(afterBlock, reserve), /await /);
  // Every early exit after the reservation releases it.
  const locked = body.slice(body.indexOf("if (!locked)"), body.indexOf("chat_busy"));
  assert.match(locked, /activeRuns\.delete\(requestId\)/);
  assert.equal((body.slice(reserve, body.indexOf("if (!locked)")).match(/activeRuns\.delete\(requestId\)/g) || []).length, 2);
});

// ---- Claude provider switch (provider.mjs) ----------------------------------
import {
  authMode, selectProvider, envForProvider, combinedCost, parseProbeResponse, isVertexUnavailable, shouldFallback, probeVertex, createProviderSwitch, vertexUrl,
} from "../provider.mjs";

test("provider: auth mode + per-request selection", () => {
  assert.equal(authMode({ ASK_MESHA_CLAUDE_AUTH: "auto" }), "auto");
  assert.equal(authMode({ CLAUDE_CODE_USE_VERTEX: "1" }), "vertex");
  assert.equal(authMode({}), "api-key");
  assert.equal(selectProvider("auto", { vertexOk: false }), "anthropic");
  assert.equal(selectProvider("auto", { vertexOk: true }), "vertex");
  assert.equal(selectProvider("vertex", { vertexOk: false }), "vertex");
  assert.equal(selectProvider("api-key", { vertexOk: true }), "anthropic");
  assert.equal(selectProvider("oauth", { vertexOk: true }), "anthropic");
});

test("provider: agent env carries only the chosen backend's credentials", () => {
  const env = { PATH: "/bin", ANTHROPIC_API_KEY: "k", CLAUDE_CODE_USE_VERTEX: "1", ANTHROPIC_VERTEX_PROJECT_ID: "p", CLOUD_ML_REGION: "global" };
  const a = envForProvider("anthropic", env);
  assert.equal(a.ANTHROPIC_API_KEY, "k");
  assert.equal(a.CLAUDE_CODE_USE_VERTEX, undefined);
  assert.equal(a.ANTHROPIC_VERTEX_PROJECT_ID, undefined);
  const v = envForProvider("vertex", env);
  assert.equal(v.ANTHROPIC_API_KEY, undefined);
  assert.equal(v.CLAUDE_CODE_USE_VERTEX, "1");
  assert.equal(v.ANTHROPIC_VERTEX_PROJECT_ID, "p");
  assert.equal(env.ANTHROPIC_API_KEY, "k", "input env not mutated");
});

test("provider: probe response parsing", () => {
  assert.deepEqual(parseProbeResponse(200, JSON.stringify({ type: "message", content: [{ type: "text", text: "o" }] })), { ok: true, reason: "ok" });
  assert.equal(parseProbeResponse(200, "<html>").ok, false);
  assert.equal(parseProbeResponse(429, '{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Quota exceeded"}}').reason, "quota_429");
  assert.equal(parseProbeResponse(429, "slow down").reason, "rate_limited_429");
  assert.equal(parseProbeResponse(403, "").reason, "forbidden_403");
  assert.equal(parseProbeResponse(404, "").reason, "not_found_404");
  assert.equal(parseProbeResponse(500, "").reason, "http_500");
  assert.equal(vertexUrl({ project: "p", region: "global", model: "m" }), "https://aiplatform.googleapis.com/v1/projects/p/locations/global/publishers/anthropic/models/m:rawPredict");
  assert.match(vertexUrl({ project: "p", region: "us-east5", model: "m" }), /^https:\/\/us-east5-aiplatform/);
});

test("provider: probeVertex sends max_tokens 1 and never throws", async () => {
  let sent;
  const ok = await probeVertex({ project: "p", region: "global", model: "m", tokenFn: async () => "t",
    fetchImpl: async (url, init) => { sent = { url, init }; return { status: 200, text: async () => '{"type":"message","content":[]}' }; } });
  assert.deepEqual(ok, { ok: true, reason: "ok" });
  assert.equal(JSON.parse(sent.init.body).max_tokens, 1);
  assert.equal(sent.init.headers.Authorization, "Bearer t");
  assert.equal((await probeVertex({ project: "p", region: "global", model: "m", tokenFn: async () => null })).reason, "no_token");
  assert.equal((await probeVertex({ project: "p", region: "global", model: "m", tokenFn: async () => "t", fetchImpl: async () => { throw new Error("x"); } })).reason, "network");
});

test("provider: mid-flight fallback decision", () => {
  const base = { mode: "auto", provider: "vertex", attempt: 0, streamed: false };
  assert.equal(shouldFallback({ ...base, status: 429 }), true);
  assert.equal(shouldFallback({ ...base, status: 403 }), true);
  assert.equal(shouldFallback({ ...base, status: 404 }), true);
  assert.equal(shouldFallback({ ...base, error: "API Error: 429 RESOURCE_EXHAUSTED Quota exceeded" }), true);
  assert.equal(shouldFallback({ ...base, error: "rate_limit" }), true);
  assert.equal(shouldFallback({ ...base, status: 529, error: "overloaded" }), false, "overload isn't a provider problem");
  assert.equal(shouldFallback({ ...base, status: 429, streamed: true }), false, "tokens already on screen");
  assert.equal(shouldFallback({ ...base, status: 429, attempt: 1 }), false, "only one retry");
  assert.equal(shouldFallback({ ...base, provider: "anthropic", status: 429 }), false);
  assert.equal(shouldFallback({ ...base, mode: "vertex", status: 429 }), false, "forced vertex never falls back");
  assert.equal(shouldFallback({ ...base, status: 429, toolCalls: 2 }), false, "queries/watch already ran: no rerun");
  assert.equal(shouldFallback({ ...base, status: 429, toolCalls: 0 }), true);
  assert.equal(isVertexUnavailable({ error: "PERMISSION_DENIED on aiplatform.endpoints.predict" }), true);
});

test("provider: switch probes, flips to vertex, re-probes on schedule, and marks vertex down", async () => {
  const logs = [];
  const timers = [];
  let probeResult = { ok: false, reason: "quota_429" };
  const sw = createProviderSwitch({
    mode: "auto", probe: async () => probeResult, log: (l) => logs.push(l),
    setTimer: (fn, ms) => { timers.push(ms); return { fn }; }, clearTimer: () => {},
  });
  assert.equal(sw.current(), "anthropic");
  await sw.probeNow();
  assert.equal(sw.current(), "anthropic");
  assert.equal(timers.at(-1), 15 * 60_000);
  probeResult = { ok: true, reason: "ok" };
  await sw.probeNow();
  assert.equal(sw.current(), "vertex");
  assert.equal(timers.at(-1), 60 * 60_000);
  assert.ok(logs.includes("[provider] switched to vertex (probe ok)"));
  sw.markVertexDown("api_retry 429 rate_limit");
  assert.equal(sw.current(), "anthropic");
  assert.equal(sw.status().vertex_ok, false);
  assert.equal(timers.at(-1), 15 * 60_000);
  const fixed = createProviderSwitch({ mode: "api-key", probe: async () => ({ ok: true }) });
  await fixed.probeNow();
  assert.equal(fixed.current(), "anthropic");
});

test("provider: failed attempt's cost is counted once on top of the retry", () => {
  assert.equal(combinedCost(0.2, 0), 0.2);
  assert.equal(combinedCost(0.2, 0.05), 0.25);
  assert.equal(combinedCost(null, 0.05), 0.05, "retry without a result still carries the first attempt");
  assert.equal(combinedCost(null, 0), null, "no result stays null so the cap estimate kicks in");
});

test("stripLeadingNarration drops a working line, keeps real answers", async () => {
  const { stripLeadingNarration, makeTurnGate } = await import("../lib.mjs");
  assert.equal(stripLeadingNarration("Confirming there's genuinely no weighing activity…\n\n**No goats were weighed today.**"), "**No goats were weighed today.**");
  assert.equal(stripLeadingNarration("Let me pull the pen list.\nNow checking sales.\n\nCastro 1, Coimbatore has 49."), "Castro 1, Coimbatore has 49.");
  assert.equal(stripLeadingNarration("Checking the records, Castro 1 has 49 sheep."), "Checking the records, Castro 1 has 49 sheep.", "single paragraph kept");
  assert.equal(stripLeadingNarration("**49 sheep** are in Castro 1.\n\n| a | b |"), "**49 sheep** are in Castro 1.\n\n| a | b |");
  assert.equal(stripLeadingNarration("Castro 1 has 49.\n\nLet me know if you need more."), "Castro 1 has 49.\n\nLet me know if you need more.");
  let out = "";
  const g = makeTurnGate((t) => (out += t), 40);
  g.text("Now I have everything I need.\n\nThe answer is 49 sheep in Castro 1, Coimbatore.");
  g.end();
  assert.equal(out, "The answer is 49 sheep in Castro 1, Coimbatore.");
});

const SERVER_SRC = fs.readFileSync(new URL("../server.mjs", import.meta.url), "utf8");
const askSrc = SERVER_SRC.slice(SERVER_SRC.indexOf("async function ask("), SERVER_SRC.indexOf("// ---- router"));

test("unknown / deleted / foreign conversation_id on /ask is a 404, never a silent new chat", () => {
  const i = askSrc.indexOf("body.conversation_id");
  const guard = askSrc.slice(i, askSrc.indexOf("}", askSrc.indexOf("return json(res, 404", i)) + 1);
  assert.match(guard, /!chat \|\| !sameOwner\(chat, user\) \|\| chat\.deleted_at/);
  assert.match(guard, /conversation_not_found/);
  // createChat only happens when no id was sent at all.
  assert.ok(askSrc.indexOf("conversation_not_found") < askSrc.indexOf("store.createChat"));
  assert.doesNotMatch(friendlyError("chat_gone"), /budget|token|tool|session|sql|query|code|agent|database/i);
  assert.doesNotMatch(friendlyError("chat_deleted"), /budget|token|tool|session|sql|query|code|agent|database/i);
});

test("deleting a chat mid-answer aborts the run and nothing is saved into it", () => {
  const del = SERVER_SRC.slice(SERVER_SRC.indexOf('if (req.method === "DELETE")'));
  assert.match(del.slice(0, 800), /activeRuns\.values\(\)\) if \(r\.chatId === chat\.id\) r\.chatDeleted/);
  assert.match(askSrc, /chatDeleted: \(\) => \{ stopReason = "chat_deleted"; onDeleted\(\); \}/);
  assert.match(askSrc, /onDeleted = \(\) => abort\.abort\(\)/);
  // The abort check sits between the SDK loop and the assistant message save.
  const save = askSrc.indexOf("await store.addMessage(chat.id, assistantMsg)");
  const guard = askSrc.lastIndexOf('if (abort.signal.aborted) throw new Error("client_aborted")', save);
  assert.ok(guard > askSrc.indexOf("for await (const msg of stream)") && guard < save);
  // And before Claude is started at all.
  assert.ok(askSrc.indexOf('if (abort.signal.aborted) throw new Error("client_aborted")') < askSrc.indexOf("const stream = query("));
});

test("the first progress frame carries the chat id (a cut-off stream stays resumable)", () => {
  assert.match(askSrc, /label: "Starting agent", request_id: requestId, conversation_id: chat\.id/);
});

test("chat lease is short so a killed instance doesn't block the chat for long", () => {
  assert.ok(LOCK_MS <= 120_000 && LOCK_MS >= 30_000); // heartbeat refreshes every 10s
});

test("istNowNote gives the IST date around IST midnight", () => {
  assert.match(istNowNote(new Date("2026-09-23T18:45:00Z")), /Thursday 2026-09-24 00:15 IST/);
  assert.match(istNowNote(new Date("2026-09-23T18:29:00Z")), /Wednesday 2026-09-23 23:59 IST/);
  assert.match(istNowNote(new Date("2026-12-31T18:30:00Z")), /Friday 2027-01-01 00:00 IST/);
  assert.match(istNowNote(), /Asia\/Kolkata/);
  assert.match(askSrc, /let prompt = istNowNote\(\)/);
});

test("clipSqlOutput: 500-row cap tells the model not to pass it off as the full list", () => {
  const c = clipSqlOutput(["h", ...Array.from({ length: 1234 }, (_, i) => `r${i}`)].join("\n"));
  assert.match(c, /only the first 500 of 1234 rows/);
  assert.match(c, /never present these 500 rows as the full list/);
});

test("attachments: kinds, unsupported types, and more than 5 files", () => {
  assert.equal(attachmentKind("a.png", "image/png"), "image");
  assert.equal(attachmentKind("a.JPG", ""), "image");
  assert.equal(attachmentKind("a.pdf", "application/pdf"), "pdf");
  assert.equal(attachmentKind("a.csv", "text/csv"), "text");
  assert.equal(attachmentKind("a.csv", "application/vnd.ms-excel"), "text"); // Windows labels CSVs as Excel
  assert.equal(attachmentKind("a.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"), "unsupported");
  assert.equal(attachmentKind("IMG_1.HEIC", "image/heic"), "unsupported");
  assert.equal(attachmentKind("a.svg", "image/svg+xml"), "unsupported");
  const files = [
    { path: "/t/1-a.png", name: "a.png", type: "image/png" },
    { path: "/t/2-b.xlsx", name: "b.xlsx", type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" },
  ];
  const p = attachmentPrompt(files, 7);
  assert.match(p, /a\.png, image\/png\)$/m);
  assert.match(p, /b\.xlsx.*can't be opened here; do not try to Read it/);
  assert.match(p, /attached 7 files; only the first 2 were kept/);
  assert.doesNotMatch(attachmentPrompt(files), /only the first/);
});

test("JSON store: corrupt chats.json doesn't crash-loop; .tmp recovery; torn metrics line skipped", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ask-store-"));
  const f = path.join(dir, "chats.json");
  const logs = [];
  fs.writeFileSync(f, '{"chats":{"a":{"id":"a"');
  assert.deepEqual(loadJsonDb(f, (l) => logs.push(l)), { chats: {} });
  assert.ok(fs.readdirSync(dir).some((n) => n.startsWith("chats.json.corrupt-")), "bad file kept aside");
  fs.writeFileSync(f, "garbage");
  fs.writeFileSync(f + ".tmp", JSON.stringify({ chats: { x: { id: "x" } } }));
  assert.deepEqual(Object.keys(loadJsonDb(f, (l) => logs.push(l)).chats), ["x"]);
  fs.writeFileSync(f, "null");
  fs.rmSync(f + ".tmp");
  assert.deepEqual(loadJsonDb(f, () => {}), { chats: {} });
  assert.ok(logs.some((l) => /unreadable/.test(l)));
  const m = path.join(dir, "metrics.jsonl");
  fs.writeFileSync(m, '{"ts":"a","ok":true}\n{"ts":"b","o');
  assert.deepEqual(readJsonl(m), [{ ts: "a", ok: true }]);
});

// ---- fallback cost / stop / delete races ------------------------------------
import { failedAttemptCostUsd, finalAnswerCost, runOwnedBy } from "../lib.mjs";

test("fallback then crash: the failed attempt's cost is still counted when the retry throws", () => {
  // Attempt 0 (Vertex) reported $0.04, retry crashed with no result: cap estimate + attempt 0.
  assert.deepEqual(finalAnswerCost({ costUsd: null, started: true, capUsd: 1, failedAttemptCost: 0.04 }), { cost: 1.04, estimated: true });
  // Retry got a result then threw later: its real cost + attempt 0.
  assert.deepEqual(finalAnswerCost({ costUsd: 0.2, started: true, capUsd: 1, failedAttemptCost: 0.04 }), { cost: 0.24, estimated: false });
  // Never started: nothing spent.
  assert.deepEqual(finalAnswerCost({ costUsd: null, started: false, capUsd: 1 }), { cost: null, estimated: false });
  // The finally block (every exit path, incl. throw) is where failed attempts are added.
  const fin = askSrc.slice(askSrc.lastIndexOf("} finally {"));
  assert.match(fin, /finalAnswerCost\(\{ costUsd: metric\.cost_usd, started, capUsd, failedAttemptCost \}\)/);
  assert.ok(askSrc.indexOf("let failedAttemptCost = 0") < askSrc.indexOf("\n  try {\n    // conversation_id up front"), "declared outside the try");
});

test("fallback: a failed attempt with no result message counts its cap, not 0", () => {
  assert.equal(failedAttemptCostUsd(null, 1.5), 1.5);
  assert.equal(failedAttemptCostUsd(undefined, 1.5), 1.5);
  assert.equal(failedAttemptCostUsd(0, 1.5), 0, "a real $0 result is kept");
  assert.equal(failedAttemptCostUsd(0.03, 1.5), 0.03);
  assert.match(askSrc, /failedAttemptCost \+= failedAttemptCostUsd\(metric\.cost_usd, capUsd\)/);
});

test("fallback: the failed attempt's session_cost_usd is rolled back", () => {
  const rb = askSrc.slice(askSrc.indexOf("providerSwitch.markVertexDown"), askSrc.indexOf("// metric.cost_usd is this attempt's cost only"));
  assert.match(rb, /chat\.session_cost_usd = origSessionCostUsd/);
  assert.match(rb, /updateChat\(chat\.id, \{ session_cost_usd: origSessionCostUsd \}\)/);
  assert.match(rb, /metric\.session_cost_usd = null/);
});

test("Stop pressed before the chat exists is recorded against the request's owner", () => {
  const run = { chatId: null, email: "ceo@x", tenantId: "t1" };
  assert.equal(runOwnedBy(run, { email: "ceo@x", tenantId: "t1" }), true);
  assert.equal(runOwnedBy(run, { email: "ceo@x", tenantId: "t2" }), false);
  assert.equal(runOwnedBy(run, { email: "other@x", tenantId: "t1" }), false);
  assert.equal(runOwnedBy(null, { email: "ceo@x", tenantId: "t1" }), false);
  const stop = SERVER_SRC.slice(SERVER_SRC.indexOf("async function stopEvent("), SERVER_SRC.indexOf("// ---- ask"));
  assert.doesNotMatch(stop, /store\.getChat/, "no chat lookup: the chat may not exist yet");
  assert.match(stop, /runOwnedBy\(run, user\)/);
  assert.match(askSrc, /chatId: chat\?\.id \?\? null, capUsd, email: user\.email, tenantId: user\.tenantId/);
});

test("chat deleted between lock and stream start: aborted before anything is written or Claude runs", () => {
  assert.ok(askSrc.indexOf('if (stopReason === "chat_deleted") abort.abort()') > askSrc.indexOf("store.tryLock"));
  const tryStart = askSrc.indexOf('label: "Starting agent"');
  const guard = askSrc.indexOf('if (abort.signal.aborted) throw new Error("client_aborted")', tryStart);
  assert.ok(guard > tryStart);
  for (const write of ["store.updateChat(chat.id, { title", "store.addMessage(chat.id, userMsg)", "uploads.save(", "const stream = query("])
    assert.ok(guard < askSrc.indexOf(write), write);
});

test("askClient: only X-Mesha-Client: mcp tags the source", async () => {
  const { askClient } = await import("../lib.mjs");
  assert.equal(askClient({ "x-mesha-client": "mcp" }), "mcp");
  assert.equal(askClient({ "x-mesha-client": " MCP " }), "mcp");
  assert.equal(askClient({ "x-mesha-client": "admin-web" }), null);
  assert.equal(askClient({}), null);
  assert.equal(askClient(undefined), null);
});

test("jsonAskCollector: final -> 200 JSON answer, tokens/progress/watch dropped", async () => {
  const { jsonAskCollector } = await import("../lib.mjs");
  const c = jsonAskCollector();
  c.send({ type: "progress", phase: "planning", conversation_id: "chat-1" });
  c.send({ type: "token", text: "Sold" });
  c.send({ type: "watch", rows: [] });
  c.send({ type: "final", answer: "Sold 42 goats.", chart: { type: "bar" }, conversation_id: "chat-1", message_id: "m1", timing: { total_ms: 5 }, request_id: "r1", source: "coding-agent" });
  const r = c.result();
  assert.equal(r.status, 200);
  assert.deepEqual(r.body, { answer: "Sold 42 goats.", chart: { type: "bar" }, conversation_id: "chat-1", message_id: "m1", timing: { total_ms: 5 }, request_id: "r1", source: "coding-agent", mode: "agent" });
});

test("jsonAskCollector: errors keep status and conversation_id; nothing -> 500", async () => {
  const { jsonAskCollector, friendlyError } = await import("../lib.mjs");
  const c = jsonAskCollector();
  c.send({ type: "progress", conversation_id: "chat-2" });
  c.send({ type: "error", message: friendlyError("error_max_budget_usd") });
  assert.deepEqual(c.result(), { status: 502, body: { error: "agent_error", message: friendlyError("error_max_budget_usd"), conversation_id: "chat-2" } });
  const d = jsonAskCollector();
  d.send({ type: "error", status: 410, message: friendlyError("chat_deleted") });
  assert.equal(d.result().status, 410);
  assert.equal(d.result().body.error, "chat_deleted");
  assert.equal(jsonAskCollector().result().status, 500);
});

test("NON_STREAM_NOTE tells the model watches are snapshot-only", async () => {
  const { NON_STREAM_NOTE } = await import("../lib.mjs");
  assert.match(NON_STREAM_NOTE, /minutes=0/);
});
