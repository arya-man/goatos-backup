import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
  makeChartFilter, extractChart, toolLabel, validateReadSql, describeTableSql, kindValuesSql, relInfoSql, shouldSampleKinds, clipSqlOutput, isDeepQuestion,
  answerCapUsd, answerCostUsd, friendlyError, STOPPED_NOTE, historyPreamble, pathAllowed, ttlCache,
} from "../lib.mjs";

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
  assert.match(src, /cost_usd === null && started/);
});
