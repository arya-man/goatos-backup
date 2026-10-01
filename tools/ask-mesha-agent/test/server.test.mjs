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
    "COMMIT", "rollback", "end", "abort", "SET default_transaction_read_only = off", "reset all", "begin", "start transaction",
    "select 1 -- it's\n; commit; select 'x'", "select 1 /* ' */; commit; select '", "select $$;$$", "select 'a''; commit; --'' ; x'; select 2",
    "select \"a;\" from t; select 1", "select 'unterminated; commit"]) {
    assert.equal(validateReadSql(bad).ok, false, bad);
  }
  // ';' inside a string literal or quoted identifier is data (describe_table uses concat_ws('; ', ...)).
  for (const ok of ["select concat_ws('; ', 'a', 'b')", "select 'it''s; fine'", "select 1 as \"a;b\"", "select 1 -- trailing; comment"]) {
    assert.equal(validateReadSql(ok).ok, true, ok);
  }
  const d = describeTableSql("public.goats");
  assert.equal(validateReadSql(d.sql).ok, true, "describe_table's own catalog query passes the validator");
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
  // The abort check sits between the agent loop and the assistant message save.
  const save = askSrc.indexOf("await store.addMessage(chat.id, assistantMsg)");
  const guard = askSrc.lastIndexOf('if (abort.signal.aborted) throw new Error("client_aborted")', save);
  assert.ok(guard > askSrc.indexOf("await runAgent(") && guard < save);
  // And before the model is started at all.
  assert.ok(askSrc.indexOf('if (abort.signal.aborted) throw new Error("client_aborted")') < askSrc.indexOf("await runAgent("));
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
  // The finally block (every exit path, incl. throw) charges the cap when no cost arrived.
  const fin = askSrc.slice(askSrc.lastIndexOf("} finally {"));
  assert.match(fin, /finalAnswerCost\(\{ costUsd: metric\.cost_usd, started, capUsd \}\)/);
});

test("failedAttemptCostUsd: no result message counts the cap, not 0", () => {
  assert.equal(failedAttemptCostUsd(null, 1.5), 1.5);
  assert.equal(failedAttemptCostUsd(undefined, 1.5), 1.5);
  assert.equal(failedAttemptCostUsd(0, 1.5), 0, "a real $0 result is kept");
  assert.equal(failedAttemptCostUsd(0.03, 1.5), 0.03);
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

test("chat deleted between lock and stream start: aborted before anything is written or the model runs", () => {
  assert.ok(askSrc.indexOf('if (stopReason === "chat_deleted") abort.abort()') > askSrc.indexOf("store.tryLock"));
  const tryStart = askSrc.indexOf('label: "Starting agent"');
  const guard = askSrc.indexOf('if (abort.signal.aborted) throw new Error("client_aborted")', tryStart);
  assert.ok(guard > tryStart);
  for (const write of ["store.updateChat(chat.id, { title", "store.addMessage(chat.id, userMsg)", "uploads.save(", "await runAgent("])
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

test("lintChart drops misleading charts, keeps good ones", async () => {
  const { lintChart } = await import("../lib.mjs");
  const ok = { type: "bar", title: "Gain, Coimbatore vs Channapatna", x: ["P1", "P2", "P3"],
    series: [{ name: "Coimbatore", data: [1, 2, 3] }, { name: "Channapatna", data: [2, null, 4] }] };
  assert.deepEqual(lintChart(ok).chart.series[1].data, [2, null, 4]);
  assert.equal(lintChart(undefined).reason, null);
  const r = (patch) => lintChart({ ...ok, ...patch }).reason;
  assert.equal(r({ series: [ok.series[0]] }), "title_vs_single_series");
  assert.equal(r({ series: [{ name: "a", data: [1, 2] }, ok.series[1]] }), "length_mismatch");
  assert.equal(r({ series: [ok.series[0], { name: "b", data: [null, null, null] }] }), "all_null_series");
  assert.equal(r({ series: [ok.series[0], { name: "b", data: [1, NaN, 2] }] }), "non_finite");
  assert.equal(r({ series: [ok.series[0], { name: "b", data: [1, "2", 2] }] }), "non_finite");
  assert.equal(r({ series: Array.from({ length: 8 }, (_, i) => ({ name: `s${i}`, data: [1, 2, 3] })) }), "too_many_series");
  assert.equal(r({ x: ["P1", "P1", "P3"] }), "x_duplicate");
  assert.equal(r({ type: "pie" }), "type");
  assert.equal(r({ series: [ok.series[0], { name: "Coimbatore", data: [1, 2, 3] }] }), "series_name");
  assert.equal(r({ title: "Gain", series: [{ name: "a", data: [1, null, null] }] }), "too_few_values");
  const many = Array.from({ length: 30 }, (_, i) => `c${i}`);
  assert.equal(r({ title: "t", x: many, series: [{ name: "a", data: many.map(() => 1) }] }), "too_many_bars");
  assert.equal(r({ title: "t", type: "line", x: many, series: [{ name: "a", data: many.map(() => 1) }] }), null);
});

test("stripLeadingNarration drops a self-instruction first line, keeps real answers", async () => {
  const { stripLeadingNarration } = await import("../lib.mjs");
  assert.equal(stripLeadingNarration("Do not invent any numbers. Do not apologize.\n\nNo, Castro 1 has 49 sheep."), "No, Castro 1 has 49 sheep.");
  assert.equal(stripLeadingNarration("Do not invent or guess information.\n\nMahendran owes Rs 4,29,875."), "Mahendran owes Rs 4,29,875.");
  assert.equal(stripLeadingNarration("Never weighed: 12 pens.\n\nThey are listed below."), "Never weighed: 12 pens.\n\nThey are listed below.");
  assert.equal(stripLeadingNarration("Always 3 deaths in Coimbatore.\nmore"), "Always 3 deaths in Coimbatore.\nmore");
});

test("stripLeadingNarration drops restated rules glued onto the answer (live WG-PARK output)", async () => {
  const { stripLeadingNarration } = await import("../lib.mjs");
  const live = fs.readFileSync(new URL("./fixtures/echoed-rules.txt", import.meta.url), "utf8");
  const out = stripLeadingNarration(live);
  assert.match(out, /^The ADG \(Average Daily Gain\) dashboard calculates daily gain/);
  assert.doesNotMatch(out, /system prompt|no narration|Follow the exact form/);
  assert.equal(stripLeadingNarration("The herd has 1,562 animals. No SQL was harmed."), "The herd has 1,562 animals. No SQL was harmed.");
});
