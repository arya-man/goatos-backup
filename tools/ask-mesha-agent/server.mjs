// Local coding-agent backend for the admin-web "Ask Mesha" panel.
// Speaks the same /ceo-ai/* contract as the Go backend, so admin-web only needs
// CEO_AI_AGENT_URL pointed here. Runs a Gemini agent loop over the live goatos
// commit (read-only code tools), with the repo's own CLAUDE.md/skills and
// read-only access to goatos-stg Postgres. Model: Gemini on Vertex AI (gemini.mjs), ADC auth only.
import http from "node:http";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import { execFileSync, spawn } from "node:child_process";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { createStore } from "./store.mjs";
import { createUploads } from "./uploads.mjs";
import { createEvents } from "./events.mjs";
import {
  isDeepQuestion, answerCapUsd, answerCostUsd, friendlyError, STOPPED_NOTE, validateReadSql, clipSqlOutput,
  finalAnswerCost, runOwnedBy, toolLabel, describeTableSql, describeTableNames, sqlTableRefs, isMissingColumnError, kindValuesSql, relInfoSql, shouldSampleKinds, makeChartFilter, extractChart, lintChart, historyPreamble, pathAllowed, ttlCache, inlineDisposition, makeTurnGate, stripLeadingNarration, istNowNote, attachmentPrompt,
  askClient, jsonAskCollector, NON_STREAM_NOTE, REFERENCE_FILES, referencePath, buildReferenceSql,
} from "./lib.mjs";
import { createWatchRegistry, WATCH_TAGS_DESCRIPTION, watchTagsHandler, watchTagsSchema } from "./watch.mjs";
import { checkAnswer, makeEvidence, needsCheck } from "./checker.mjs";
import { geminiInstructionPack, tableIndexSection, TABLE_INDEX_SQL } from "./instructions.mjs";
import { geminiConfig, createClient, connectMcp, runAgent, generateOnce, historyContents, INLINE_BUDGET_CHARS } from "./gemini.mjs";
import { createCodeTools, CODE_TOOL_DECLARATIONS, inlineMime, INLINE_MAX_BYTES } from "./code-tools.mjs";

const PORT = Number(process.env.PORT || 8787);
// 127.0.0.1 locally; the container sets HOST=0.0.0.0 (Cloud Run fronts it with IAM).
const HOST = process.env.HOST || "127.0.0.1";
const STATE = process.env.ASK_MESHA_STATE_DIR || path.join(process.env.HOME, ".ask-mesha-agent");
const REPO = process.env.GOATOS_REPO || path.join(process.env.HOME, "mesha/goatos");
// Gemini on Vertex (gemini.mjs): newest Pro by default; ASK_MESHA_MODEL / ASK_MESHA_DEEP_MODEL /
// ASK_MESHA_FAST_MODEL / ASK_MESHA_GEMINI_PROJECT / ASK_MESHA_GEMINI_LOCATION override.
const GEMINI = geminiConfig(process.env);
const MODEL = GEMINI.model;
const DEEP_MODEL = GEMINI.deepModel;
const EFFORT = process.env.ASK_MESHA_EFFORT || "low"; // Gemini thinkingLevel for quick lookups; deep = high
const ANSWER_SECONDS = Number(process.env.ASK_MESHA_ANSWER_SECONDS) || 50;
const ANSWER_DEEP_SECONDS = Number(process.env.ASK_MESHA_DEEP_ANSWER_SECONDS) || 240;
const ai = createClient(GEMINI); // ADC; ASK_MESHA_GEMINI_AUTH=gcloud uses the local gcloud user token (dev only)
// Answer checker (checker.mjs): a second, tool-less call that checks the draft's wording against the
// query results before the final answer lands. ASK_MESHA_CHECKER=0 turns it off.
const CHECKER_ON = process.env.ASK_MESHA_CHECKER !== "0";
const CHECK_MODEL = GEMINI.checkModel;
// The check spends inside the answer's own cap: at most this, and never past what the run left.
const CHECK_BUDGET_USD = Number(process.env.ASK_MESHA_CHECK_BUDGET_USD) || 0.25;
// Always read-only: the agent gets read_file/grep/glob/list_dir/get_skill + the read-only SQL
// tools and NO shell, edit, or write tools (there is no other mode).
const READONLY = true;
// Spend caps (USD). Monthly: hard stop for new questions once reached (resets on the
// 1st, UTC). Per answer: the SDK aborts a single run that would exceed it.
const MONTHLY_BUDGET_USD = Number(process.env.ASK_MESHA_MONTHLY_BUDGET_USD || 100);
const PER_ANSWER_BUDGET_USD = Number(process.env.ASK_MESHA_PER_ANSWER_BUDGET_USD || 1);
const DEEP_ANSWER_BUDGET_USD = Number(process.env.ASK_MESHA_DEEP_ANSWER_BUDGET_USD || 5);
const monthStart = () => {
  const d = new Date();
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1)).toISOString();
};
const BASE_SHA = process.env.GOATOS_BASE_SHA || "HEAD";
const STG_API = (process.env.GOATOS_STG_API || "https://api.goatos.mesha.sg").replace(/\/$/, "");
const WORKTREES = path.join(STATE, "worktrees");
fs.mkdirSync(STATE, { recursive: true });
let cachedPgEnv = null;

// Read-only PG* vars, loaded per use so a missing/invalid file fails the request
// (with a clear message) instead of crashing the server at startup.
function loadPgEnv() {
  if (cachedPgEnv) return cachedPgEnv;
  const file = path.join(STATE, ".pgenv");
  if (!fs.existsSync(file)) {
    throw new Error(`missing ${file}; create it with read-only PG* variables before asking data questions`);
  }
  cachedPgEnv = Object.fromEntries(
    fs.readFileSync(file, "utf8").trim().split("\n").filter(Boolean).map((l) => {
      const i = l.indexOf("=");
      if (i <= 0) throw new Error(`invalid ${file} line: ${l.slice(0, 40)}`);
      return [l.slice(0, i), l.slice(i + 1)];
    }),
  );
  return cachedPgEnv;
}

const STARTERS = [
  "How many animals were weighed this month, by park?",
  "Show the weekly ADG trend for the last 8 weeks as a chart",
  "How many animals did we sell this month, by park?",
  "Which pens are behind on weighing verification?",
];

// Instructions (CEO rules, CLAUDE.md/AGENTS.md, data map, table index): instructions.mjs.
// lintedChart: drop a structurally misleading chart (text answer stays) and record why.
function lintedChart(chart, metric) {
  const r = lintChart(chart);
  if (r.reason) metric.chart_dropped = r.reason;
  return r;
}

// Live index of EVERY readable table (schema, name, approx rows), rebuilt hourly from the
// catalog so nothing depends on the hand-written map: new tables appear automatically.
let tableIndex = { text: "", at: 0 };
async function refreshTableIndex() {
  const r = await runSql(TABLE_INDEX_SQL);
  if (!r.ok) return console.error("[table-index] failed:", r.out.slice(0, 200));
  const rows = r.out.split("\n").filter((l) => l.includes("."));
  tableIndex = { text: rows.join("\n"), at: Date.now() };
  console.log(`[table-index] ${rows.length} readable tables`);
}
function tableIndexPrompt() {
  if (Date.now() - tableIndex.at > 3_600_000) void refreshTableIndex();
  return tableIndexSection(tableIndex.text);
}

function geminiSystemInstruction(cwd) {
  return geminiInstructionPack({ cwd, repo: REPO, readonly: READONLY, tableSection: tableIndexPrompt() });
}

// ---- benchmark events -----------------------------------------------------
async function recordMetric(m) {
  await store.recordMetric(m).catch((e) => console.error("[metric] store failed:", e.message));
  console.log(
    `[metric] total=${m.total_ms}ms turns=${m.turns ?? "-"} cache_read=${m.cache_read_tokens ?? "-"} cache_write=${m.cache_creation_tokens ?? "-"} first_progress=${m.first_progress_ms}ms first_tool=${m.first_tool_ms ?? "-"}ms ` +
      `first_token=${m.first_token_ms ?? "-"}ms tools=${m.tool_calls} db=${m.db_queries} model=${m.model} provider=${m.provider ?? "-"} ok=${m.ok}`,
  );
}

// ---- read-only SQL tool (replaces Bash/psql in read-only mode) --------------
// Runs one query in a READ ONLY transaction. Output is decoded as UTF-8 per stream (no
// split multibyte chars), bounded in memory, and the process is killed on a hard timeout.
const SQL_KILL_MS = 75_000;
function runSql(sql) {
  return new Promise((resolve) => {
    const checked = validateReadSql(sql);
    if (!checked.ok) return resolve(checked);
    let pgEnv;
    try { pgEnv = loadPgEnv(); } catch (e) { return resolve({ ok: false, out: `Database connection is not configured: ${e.message}` }); }
    let child;
    try {
      child = spawn(
        "psql",
        ["-X", "-q", "-v", "ON_ERROR_STOP=1", "-P", "pager=off", "-P", "footer=off", "-A", "-F", "\t", "-f", "-"],
        {
          env: {
            PATH: process.env.PATH,
            ...pgEnv,
            PGCONNECT_TIMEOUT: "10",
            // Belt and braces on top of the read-only DB role.
            PGOPTIONS: "-c default_transaction_read_only=on -c statement_timeout=60000 -c standard_conforming_strings=on",
          },
        },
      );
    } catch (e) {
      return resolve({ ok: false, out: `psql could not start: ${e.message}` });
    }
    let out = "";
    let err = "";
    let capped = false;
    let done = false;
    const finish = (r) => { if (!done) { done = true; clearTimeout(killer); resolve(r); } };
    const killer = setTimeout(() => {
      child.kill("SIGKILL");
      finish({ ok: false, out: `Query cancelled after ${SQL_KILL_MS / 1000}s (timeout). Narrow it (date range, aggregate) and retry; if it still cannot run, tell the user plainly that this lookup was too big to finish and suggest a narrower question (no SQL or timeout jargon).` });
    }, SQL_KILL_MS);
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (c) => { if (out.length < 400_000) out += c; else capped = true; });
    child.stderr.on("data", (c) => { if (err.length < 8_000) err += c; });
    child.stdin.on("error", () => {}); // EPIPE if psql exits before reading the query
    child.on("error", (e) => finish({ ok: false, out: e.code === "ENOENT" ? "psql is not installed on this server." : `psql failed: ${e.message}` }));
    child.on("close", (code) => {
      if (code === 0) return finish({ ok: true, out: clipSqlOutput(out, { capped }) });
      const msg = err.trim() || `psql exited ${code}`;
      finish({ ok: false, out: /statement timeout/i.test(msg) ? "Query cancelled after 60s (statement timeout). Narrow it (date range, aggregate) and retry; if it still cannot run, tell the user plainly that this lookup was too big to finish and suggest a narrower question (no SQL or timeout jargon)." : msg });
    });
    child.stdin.end(`BEGIN READ ONLY;\n${checked.sql};\nROLLBACK;\n`);
  });
}
// One table's columns (+ FK join targets) and common category/status values.
async function describeOne(name) {
  const d = describeTableSql(name);
  if (!d.ok) return { ok: false, text: d.out };
  const cols = await runSql(d.sql);
  if (!cols.ok) return { ok: false, text: cols.out };
  const rows = cols.out.split("\n").slice(1).filter(Boolean).map((l) => l.split("\t"));
  if (!rows.length) return { ok: false, text: `No table or view named ${d.schema}.${d.table}. Pick one from the table list.` };
  const textCols = rows.filter(([, t]) => !/^(uuid|jsonb?|bool|int|small|big|numeric|real|double|date|time|interval|bytea|tsvector|\w+\[\])/i.test(t)).map(([c]) => c);
  const info = await runSql(relInfoSql(d.schema, d.table));
  const [relkind, relRows] = (info.ok ? info.out.split("\n")[1] || "" : "").split("\t");
  const kindSql = shouldSampleKinds(relkind, relRows) ? kindValuesSql(d.schema, d.table, textCols) : "";
  const kinds = kindSql ? await runSql(kindSql) : null;
  return { ok: true, text: `## ${d.schema}.${d.table}\n` + cols.out + (kinds?.ok && kinds.out.trim() ? `\nCommon values:\n${kinds.out}` : ""), rows, schema: d.schema, table: d.table };
}
// Compact "real columns" hint appended to a failed query's error (saves the describe turn).
async function columnHint(sql) {
  const refs = sqlTableRefs(sql);
  if (!refs.length) return "";
  const parts = await Promise.all(refs.map(async (t) => {
    const d = describeTableSql(t);
    if (!d.ok) return "";
    const r = await runSql(d.sql);
    const cols = r.ok ? r.out.split("\n").slice(1).filter(Boolean).map((l) => l.split("\t")[0]) : [];
    return cols.length ? `${d.schema}.${d.table}: ${cols.join(", ")}` : "";
  }));
  const text = parts.filter(Boolean).join("\n");
  return text ? `\n\nActual columns of the tables in this query (fix the query from these):\n${text}` : "";
}
// readOnlyHint lets the agent's CLI run several calls from one turn concurrently.
const RO = { annotations: { readOnlyHint: true, destructiveHint: false, openWorldHint: false } };
const watches = createWatchRegistry(); // one live tag watch per chat (watch.mjs)
// The "mesha" MCP server (same tools the Claude path had); Gemini reaches it through an MCP client.
function meshaToolsFor(user, watchCtx) {
  const server = new McpServer({ name: "mesha", version: "1.0.0" });
  const tool = (name, description, inputSchema, handler, opts = RO) => server.registerTool(name, { description, inputSchema, annotations: opts.annotations }, handler);
  [
      tool(
        "run_sql",
        "Run read-only SQL against goatos-stg (any table/view in any schema) and return tab-separated rows (max 500). Independent queries: call this several times in the SAME turn (they run in parallel). A 'column does not exist' error comes back with the real column lists of the tables involved.",
        { sql: z.string().describe("A single SELECT/WITH query. No psql backslash commands.") },
        async ({ sql }) => {
          const r = await runSql(sql);
          const hint = !r.ok && isMissingColumnError(r.out) ? await columnHint(sql).catch(() => "") : "";
          return { content: [{ type: "text", text: (r.out || "(no rows)") + hint }], isError: !r.ok };
        },
        RO,
      ),
      tool(
        "run_reference",
        `Run one of the vetted Mesha reference queries BY NAME (read-only), instead of retyping it into run_sql. Wrapped as SELECT * FROM (<file>) q [WHERE where] [ORDER BY order_by] [LIMIT limit]. Files: ${REFERENCE_FILES.join(", ")}. Filter on the file's OUTPUT columns, e.g. pens.sql / pen-weighing-latest.sql where="pen_code='G1P3' AND park_code='CBE'" (or grp='Godel 1'); load-wise-sales.sql where="load_no='126'". Windows via params only: adg-by-park.sql {from_date, to_date} (YYYY-MM-DD), adg-by-breed.sql {from_date, to_date, park_code, sex, origin, weighing} (= the app's Breed-wise ADG tab filters), cost-per-kg-gain.sql {days}. Several calls in one turn run in parallel.`,
        {
          name: z.enum(REFERENCE_FILES).describe("Reference file name (no path)"),
          where: z.string().optional().describe("Optional SQL boolean over the file's output columns"),
          order_by: z.string().optional().describe("Optional ORDER BY list over output columns"),
          limit: z.number().int().min(1).max(500).optional(),
          show_sql: z.boolean().optional().describe("Only when the user explicitly asks for the SQL: also return the exact query that ran"),
          // Explicit keys, not z.record: a record schema makes the SDK drop the whole tool from the model's list.
          params: z.object({
            from_date: z.string().optional().describe("YYYY-MM-DD (adg-by-park.sql, adg-by-breed.sql)"),
            to_date: z.string().optional().describe("YYYY-MM-DD, inclusive (adg-by-park.sql, adg-by-breed.sql)"),
            park_code: z.string().optional().describe("CBE | CPT | PARIGI; omit = all parks (adg-by-breed.sql)"),
            sex: z.enum(["all", "male", "female"]).optional().describe("omit/all = both sexes (adg-by-breed.sql)"),
            origin: z.enum(["all", "farm_born", "purchased"]).optional().describe("omit/all = both (adg-by-breed.sql)"),
            weighing: z.enum(["all", "individual", "whole_pen"]).optional().describe("weighing type; omit/all = both (adg-by-breed.sql)"),
            days: z.number().int().min(1).max(3650).optional().describe("window days back from today (cost-per-kg-gain.sql)"),
          }).optional().describe("Only params the file declares ('-- param:' lines); others are refused"),
        },
        async ({ name, where, order_by, limit, params, show_sql }) => {
          const p = referencePath(REPO, name);
          if (!p.ok) return { content: [{ type: "text", text: p.out }], isError: true };
          let text;
          try { text = fs.readFileSync(p.file, "utf8"); } catch (e) { return { content: [{ type: "text", text: `Reference ${name} is not available: ${e.message}` }], isError: true }; }
          const built = buildReferenceSql(text, { name, where, order_by, limit, params });
          if (!built.ok) return { content: [{ type: "text", text: built.out }], isError: true };
          const r = await runSql(built.sql);
          const sqlNote = show_sql ? `\n\n-- SQL that ran (this question's filters filled in):\n${built.sql}` : "";
          return { content: [{ type: "text", text: (r.out || "(no rows)") + sqlNote }], isError: !r.ok };
        },
        RO,
      ),
      tool(
        "describe_table",
        "List the columns (name, type, note incl. foreign-key join targets) of up to 6 tables/views in ONE call, plus the most common values of their category/status/type-like columns. Pass every table you are about to query at once, before writing SQL against tables you have not queried yet in this chat.",
        {
          table: z.string().optional().describe("schema.table, or several separated by commas, e.g. public.shift_requests, public.workforce_members"),
          tables: z.array(z.string()).optional().describe("Alternative: a list of schema.table names (max 6)"),
        },
        async ({ table, tables }) => {
          const names = describeTableNames([...(tables || []), ...describeTableNames(table)]);
          if (!names.length) return { content: [{ type: "text", text: "Give at least one table as schema.table." }], isError: true };
          const results = await Promise.all(names.map((n) => describeOne(n).catch((e) => ({ ok: false, text: String(e?.message || e) }))));
          const text = results.map((r, i) => (r.ok ? r.text : `## ${names[i]}\n${r.text}`)).join("\n\n");
          return { content: [{ type: "text", text }], isError: results.every((r) => !r.ok) };
        },
        RO,
      ),
      // Live BLE tag watch: the server polls via runSql and streams `watch` SSE events (watch.mjs).
      tool("watch_tags", WATCH_TAGS_DESCRIPTION, watchTagsSchema(z),
        watchTagsHandler({ runSql, emit: (n, c, f) => events.emit(n, c, f), registry: watches, ctx: watchCtx, log: (m) => console.log(m) }), RO),
  ];
  return server;
}

// ---- storage: Postgres (ASK_MESHA_DATABASE_URL) or JSON file (local dev) ----
// Uploads: GCS (ASK_MESHA_UPLOADS_BUCKET) or $STATE/uploads. See store.mjs / uploads.mjs.
fs.mkdirSync(STATE, { recursive: true });
const store = await createStore({ stateDir: STATE });
const uploads = await createUploads({ stateDir: STATE });
const events = await createEvents({ stateDir: STATE }); // per-user lifecycle events (events.mjs)

// ---- auth: reuse the live backend's leadership check ----------------------
// Bounded (expired entries pruned) so a stream of distinct tokens can't grow memory forever.
const authCache = ttlCache(2000);
const AUTH_TIMEOUT_MS = Number(process.env.ASK_MESHA_AUTH_TIMEOUT_MS || 8000);
async function authenticate(req) {
  const authz = req.headers["authorization"] || "";
  const token = authz.replace(/^Bearer\s+/i, "");
  if (!token) return null;
  const tenantId = typeof req.headers["x-goatos-tenant-id"] === "string" ? req.headers["x-goatos-tenant-id"] : "";
  // Local benchmark runs: a shared secret from the environment, never set in deployed envs.
  // K_SERVICE is set by Cloud Run: the bench bypass can never be live there.
  if (process.env.ASK_MESHA_BENCH_TOKEN && !process.env.K_SERVICE && token === process.env.ASK_MESHA_BENCH_TOKEN) return { email: "bench@local", tenantId };
  const cacheKey = `${tenantId}\0${token}`;
  const hit = authCache.get(cacheKey);
  if (hit) return hit;
  const headers = { Authorization: authz, Accept: "application/json" };
  if (tenantId) headers["X-GoatOS-Tenant-ID"] = tenantId;
  const res = await fetch(`${STG_API}/ceo-ai/starters`, { headers, signal: AbortSignal.timeout(AUTH_TIMEOUT_MS) }).catch((e) => {
    console.warn(`[auth] stg leadership check failed: ${e?.message || e}`);
    return null;
  });
  if (!res || res.status !== 200) {
    if (res) console.warn(`[auth] stg leadership check returned ${res.status} (tenant=${tenantId || "none"})`);
    return null;
  }
  // The payload is only trusted because the stg API just accepted this token's
  // signature. No identifiable claim => refuse: a shared fallback id would put
  // every such user's chats in one bucket.
  let email = null;
  try {
    const payload = JSON.parse(Buffer.from(token.split(".")[1], "base64url").toString());
    email = payload.email || payload.sub || null;
  } catch {}
  if (typeof email !== "string" || !email) return null;
  const user = { email, tenantId };
  authCache.set(cacheKey, user, 5 * 60_000);
  return user;
}

// ---- helpers --------------------------------------------------------------
const json = (res, status, body) => {
  res.writeHead(status, { "Content-Type": "application/json", "Cache-Control": "no-store" });
  res.end(JSON.stringify(body));
};
// 15 MB covers 10 MB of attachments after base64 inflation; larger bodies are dropped.
const MAX_BODY = 15 * 1024 * 1024;
const readBody = (req) =>
  new Promise((resolve) => {
    // Collect Buffers: string += chunk splits multibyte UTF-8 (Tamil/Chinese/emoji) at chunk edges.
    const parts = [];
    let size = 0;
    req.on("data", (c) => {
      size += c.length;
      // Too large: keep draining (discarding) so the caller can still send a 413;
      // destroying the socket here left the client with a bare connection reset.
      if (size > MAX_BODY) { parts.length = 0; return; }
      parts.push(c);
    });
    req.on("error", () => resolve({}));
    req.on("end", () => {
      if (size > MAX_BODY) return resolve({ __too_large: true });
      const s = Buffer.concat(parts).toString("utf8");
      try { resolve(s ? JSON.parse(s) : {}); } catch { resolve({}); }
    });
  });
// A chat belongs to the email AND the tenant it was created under.
function sameOwner(chat, user) {
  return chat?.email === user.email && (chat.tenant_id ?? "") === user.tenantId;
}
const summary = (c) => ({ id: c.id, title: c.title, updated_at: c.updated_at });


// Optional overlay (local testing before the PR lands): copy the latest data map
// into each chat worktree on every ask so map updates apply immediately.
const OVERLAY = process.env.ASK_MESHA_OVERLAY_DIR;
const OVERLAY_PATHS = [".agents/skills/mesha-data-map", "tools/ask-mesha-agent/data-map-core.md"];
function applyOverlay(dir) {
  if (!OVERLAY) return;
  for (const rel of OVERLAY_PATHS) {
    const src = path.join(OVERLAY, rel);
    if (fs.existsSync(src)) fs.cpSync(src, path.join(dir, rel), { recursive: true, force: true, dereference: true });
  }
}

// All chats share ONE read-only checkout so the (large) system prompt prefix is
// byte-identical across chats and served from the prompt cache. Per-chat
// worktrees are only for code-changing sessions (ASK_MESHA_WORKTREE_PER_CHAT=1).
const PER_CHAT = process.env.ASK_MESHA_WORKTREE_PER_CHAT === "1";
async function ensureWorktree(chat) {
  if (!PER_CHAT) {
    applyOverlay(REPO);
    return REPO;
  }
  if (chat.worktree && fs.existsSync(chat.worktree)) {
    applyOverlay(chat.worktree);
    return chat.worktree;
  }
  const dir = path.join(WORKTREES, chat.id.slice(0, 8));
  fs.mkdirSync(WORKTREES, { recursive: true });
  execFileSync("git", ["-C", REPO, "worktree", "add", "-q", "-B", `agent/${chat.id.slice(0, 8)}`, dir, BASE_SHA]);
  applyOverlay(dir);
  chat.worktree = dir;
  await store.updateChat(chat.id, { worktree: dir });
  return dir;
}

// Answer checker: one tool-less Gemini call (fast model); stops on timeout or when the user stops.
async function runCheck(prompt, signal, userSignal) {
  const ac = new AbortController();
  const stop = () => ac.abort();
  signal.addEventListener("abort", stop, { once: true });
  userSignal.addEventListener("abort", stop, { once: true });
  try {
    return await generateOnce({ ai, model: CHECK_MODEL, systemInstruction: "You check answers against query results. Reply with JSON only.", prompt, signal: ac.signal });
  } finally {
    signal.removeEventListener("abort", stop);
    userSignal.removeEventListener("abort", stop);
  }
}

// Gemini tool name -> the name toolLabel()/checker/events already understand.
const LEGACY_TOOL_NAME = {
  run_sql: "mcp__mesha__run_sql", run_reference: "mcp__mesha__run_reference", describe_table: "mcp__mesha__describe_table",
  watch_tags: "mcp__mesha__watch_tags", read_file: "Read", grep: "Grep", glob: "Glob", list_dir: "Glob", get_skill: "Skill",
};
const legacyInput = (name, a = {}) => (name === "read_file" ? { ...a, file_path: a.path } : name === "list_dir" ? { ...a, pattern: a.path || "" } : a);
const EVIDENCE_NAMES = new Set(["run_sql", "run_reference", "describe_table"]);

// ---- stop signals -----------------------------------------------------------
// request_id -> live run. The panel POSTs /ceo-ai/events {kind:"stop_pressed"}
// just before aborting, so ask_stopped can tell Stop from a closed tab.
const activeRuns = new Map();
const STOP_SIGNAL_GRACE_MS = 1500;

async function stopEvent(req, res, user) {
  const b = await readBody(req);
  if ((b.kind !== "stop_pressed" && b.kind !== "watch_stop") || typeof b.request_id !== "string") return json(res, 400, { error: "invalid_event" });
  const run = activeRuns.get(b.request_id);
  if (!run) { res.writeHead(204); return res.end(); }
  // Recorded against the request (its starter), not the chat: Stop on a brand-new chat can
  // arrive before the chat row exists, and a 404 there left the run spending until disconnect.
  if (!runOwnedBy(run, user)) return json(res, 404, { error: "not_found" });
  if (b.kind === "watch_stop") run.stopWatch?.("stopped"); // ends only the live watch; the answer still comes
  else run.stopPressed();
  res.writeHead(204);
  res.end();
}

// ---- ask ------------------------------------------------------------------
async function ask(req, res, user) {
  const body = await readBody(req);
  if (body.__too_large) {
    return json(res, 413, { error: "too_large", message: "Those attachments are too large to send together (10 MB in total). Please attach fewer or smaller files." });
  }
  const question = String(body.question || "").trim();
  if (!question) return json(res, 400, { error: "question_required" });
  // stream:false (the hosted MCP's ask_goatos): same pipeline, one JSON response at the end.
  const streaming = body.stream !== false;
  const client = askClient(req.headers);
  // A pasted wall of text costs real money on every resumed turn; 20k chars is ~10 pages.
  if (question.length > 20_000) {
    return json(res, 413, { error: "question_too_long", message: "That question is too long. Please shorten it or attach the text as a file." });
  }
  let chat = null;
  if (body.conversation_id != null && body.conversation_id !== "") {
    chat = await store.getChat(String(body.conversation_id));
    // Unknown, deleted or someone else's chat: never silently start a new one (the
    // question would land in a chat the panel isn't showing). The panel starts fresh.
    if (!chat || !sameOwner(chat, user) || chat.deleted_at) {
      return json(res, 404, { error: "conversation_not_found", message: friendlyError("chat_gone") });
    }
  }
  // Hard monthly cap: answer with a plain message instead of calling Claude.
  // Fail closed: if spend can't be read, don't risk running past the cap.
  const spent = await store.monthSpendUsd(monthStart()).catch(() => Infinity);
  const provider = "gemini";
  const evCtx = { request_id: crypto.randomUUID(), chat_id: chat?.id ?? null, email: user.email, tenant_id: user.tenantId, provider, ...(client ? { source: client } : {}) };
  events.budgetCheck(evCtx, spent, MONTHLY_BUDGET_USD);
  // Answers still running may each spend up to their cap; count them against the month too.
  const inFlight = [...activeRuns.values()].reduce((a, r) => a + (r.capUsd || 0), 0);
  if (spent + inFlight >= MONTHLY_BUDGET_USD) {
    await events.budgetBlocked(evCtx, spent, MONTHLY_BUDGET_USD, question);
    const answer = Number.isFinite(spent)
      ? "Ask Mesha is paused for this month. Please contact the Mesha team."
      : "Ask Mesha is unavailable for a moment. Please try again shortly.";
    if (!streaming) return json(res, 200, { answer, chart: null, conversation_id: chat?.id ?? null, message_id: null, timing: null, mode: "agent", source: "budget" });
    res.writeHead(200, { "Content-Type": "text/event-stream; charset=utf-8", "Cache-Control": "no-store" });
    res.end(`data: ${JSON.stringify({ type: "final", answer, mode: "agent", source: "budget" })}\n\n`);
    return;
  }
  // Reserve this answer's cap NOW (synchronously after the check): the awaits below
  // (createChat/tryLock) would otherwise let concurrent asks on other chats all pass
  // the same budget check. Every early exit below releases the reservation.
  const requestId = evCtx.request_id; // same id as budget_warning, so the events correlate
  // Investigations (screenshots, "verify/why/bug…") get the deep model, high effort and
  // a larger per-answer budget; quick lookups stay fast and cheap.
  const deep = isDeepQuestion(question, body.attachments);
  // Per-answer cap, clipped to what is left this month so one run can't overshoot the hard cap.
  const capUsd = answerCapUsd({ deep, perAnswer: PER_ANSWER_BUDGET_USD, deepAnswer: DEEP_ANSWER_BUDGET_USD, monthly: MONTHLY_BUDGET_USD, spent, inFlight });
  let stopReason = null;
  let onStopSignal = () => {};
  let onDeleted = () => {};
  const run = {
    chatId: chat?.id ?? null, capUsd, email: user.email, tenantId: user.tenantId,
    stopPressed: () => { stopReason = "stop_pressed"; onStopSignal(); },
    // The chat was deleted mid-answer: abort the whole run (spend stops, nothing is saved).
    chatDeleted: () => { stopReason = "chat_deleted"; onDeleted(); },
  };
  activeRuns.set(requestId, run);
  try {
    if (!chat) chat = await store.createChat(user.email, user.tenantId);
    run.chatId = chat.id;
  } catch (e) {
    activeRuns.delete(requestId);
    throw e;
  }
  // One run per chat: two concurrent resumes of the same session fork it and
  // race on session_id / message order. DB-backed lease in Postgres mode.
  const locked = await store.tryLock(chat.id).catch((e) => { activeRuns.delete(requestId); throw e; });
  if (!locked) {
    activeRuns.delete(requestId);
    // Visible in /metrics/recent: a CEO double-submitting or a stuck lease shows up here.
    await events.emit("chat_busy", { ...evCtx, chat_id: chat.id }, { severity: "WARNING", error_class: "chat_busy", question_preview: String(question || "").slice(0, 80) });
    return json(res, 409, { error: "chat_busy", message: friendlyError("busy") });
  }

  const collector = streaming ? null : jsonAskCollector();
  if (streaming) {
    res.writeHead(200, {
      "Content-Type": "text/event-stream; charset=utf-8",
      "Cache-Control": "no-store, no-transform",
      Connection: "keep-alive",
    });
  }
  const send = streaming ? (obj) => res.write(`data: ${JSON.stringify(obj)}\n\n`) : (obj) => collector.send(obj);
  const heartbeat = setInterval(() => {
    if (streaming) res.write(": ping\n\n");
    store.refreshLock(chat.id).catch(() => {});
  }, 10_000);
  const abort = new AbortController();
  // 'close' also fires after a normal res.end(); only a real disconnect aborts the SDK run.
  res.on("close", () => { if (!res.writableFinished) abort.abort(); });
  onDeleted = () => abort.abort();
  if (stopReason === "chat_deleted") abort.abort(); // deleted between lock and here

  const userMsg = { id: crypto.randomUUID(), role: "user", content: question, created_at: new Date().toISOString() };
  const t0 = Date.now();
  let started = false; // model call started: from here on spend is real even if no result arrives
  let prompt = istNowNote() + (streaming ? "" : NON_STREAM_NOTE) + "\n\n" + question.replace(/^deep:\s*/i, "");
  // Resumed chats can carry stale conclusions from when access was narrower
  // ("prices aren't readable"). A turn-level note beats the system prompt there.
  if (chat.title !== "New chat") {
    prompt =
      "[Context note, not from the user: you can read EVERY table now, including raw public.* tables " +
      "(e.g. feed purchase prices in public.feed_purchases, per-weigh rows in public.weighing_observations). " +
      "If earlier in this chat you said something wasn't readable, query the raw tables before answering.]\n\n" +
      prompt;
  }
  const scope = body.page_scope && typeof body.page_scope === "object" ? body.page_scope : {};
  const pageScope = {
    park_id: typeof scope.park_id === "string" && scope.park_id ? scope.park_id : "",
    shed_id: typeof scope.shed_id === "string" && scope.shed_id ? scope.shed_id : "",
  };
  if (pageScope.park_id || pageScope.shed_id) {
    prompt += `\nCurrent page scope: ${pageScope.park_id ? `park_id=${pageScope.park_id}` : ""}${pageScope.park_id && pageScope.shed_id ? ", " : ""}${pageScope.shed_id ? `shed_id=${pageScope.shed_id}` : ""}. If the user's wording does not ask for all parks or another scope, apply this page scope in the SQL.`;
  }
  let history = [];
  let cleanupUploads = async () => {};
  const metric = {
    ts: new Date(t0).toISOString(), request_id: requestId, chat_id: chat.id, email: user.email,
    resumed: Boolean(chat.session_id), model: deep ? DEEP_MODEL : MODEL, effort: deep ? "high" : EFFORT,
    question_chars: prompt.length, first_progress_ms: null, first_tool_ms: null, first_token_ms: null,
    total_ms: null, cache_read_tokens: null, cache_creation_tokens: null, tool_calls: 0, db_queries: 0, turns: 0, input_tokens: null, output_tokens: null,
    cost_usd: null, cost_estimated: false, session_cost_usd: null, cap_usd: capUsd, ok: false, error: null,
    provider,
  };
  const since = () => Date.now() - t0;
  const trackCtx = { request_id: requestId, chat_id: chat.id, email: user.email, tenant_id: user.tenantId, provider, ...(client ? { source: client } : {}) };
  const track = events.tracker(trackCtx,
    { t0, question, deep, model: metric.model, effort: metric.effort, resumed: metric.resumed });
  let full = "";
  let evidence = makeEvidence(); // query results the checker verifies the draft against
  // Only the answer should stay on screen: text streamed before a tool call is
  // narration ("let me check…"), so the client is told to clear it ("reset").
  let turnVisible = false;
  const emitVisible = (t) => {
    if (!t) return;
    if (metric.first_token_ms === null) metric.first_token_ms = since();
    track.firstToken();
    turnVisible = true;
    send({ type: "token", text: t });
  };
  let filter = makeChartFilter(emitVisible);
  // Narration before a tool call is held back, not streamed-then-cleared (no flicker).
  let gate = makeTurnGate((t) => filter(t));
  let lastTurnText = "";

  try {
    // conversation_id up front: a stream cut short (server restart, network) still leaves the
    // panel on this chat, so asking again continues it instead of starting another.
    send({ type: "progress", phase: "planning", label: "Starting agent", request_id: requestId, conversation_id: chat.id });
    metric.first_progress_ms = since();
    // Deleted between lock and here: nothing (not even the question) is written into it.
    if (abort.signal.aborted) throw new Error("client_aborted");
    // Inside try so a store failure still releases the chat lease (finally).
    if (chat.title === "New chat") {
      chat.title = question.slice(0, 60);
      await store.updateChat(chat.id, { title: chat.title });
    }
    // History before this turn, for the resume-miss fallback below.
    history = await store.getMessages(chat.id);
    const up = await uploads.save(chat.id, body.attachments);
    cleanupUploads = up.cleanup;
    track.attachments(up.files);
    if (up.files.length) {
      // Persist file refs on the user turn so a reloaded chat can show them again.
      userMsg.files = up.files.map(({ id, name, type }) => ({ id, name, type }));
      prompt += attachmentPrompt(up.files, Array.isArray(body.attachments) ? body.attachments.length : up.files.length);
    }
    await store.addMessage(chat.id, userMsg);
    metric.question_chars = prompt.length;
    const cwd = await ensureWorktree(chat);
    metric.resumed = history.length > 0;
    // Stateless across turns: the stored chat history is replayed as real user/model turns.
    const userParts = [{ text: prompt }];
    // Screenshots/PDFs reach the model directly as inline parts (and stay readable via read_file).
    let inlineChars = 0;
    for (const f of up.files) {
      const mime = inlineMime(f.name) || (/^image\/(png|jpe?g|gif|webp)$|^application\/pdf$/.test(f.type) ? f.type : null);
      if (!mime) continue;
      try {
        const st = fs.statSync(f.path);
        if (st.size > INLINE_MAX_BYTES || inlineChars + Math.ceil(st.size / 3) * 4 > INLINE_BUDGET_CHARS) continue;
        const data = fs.readFileSync(f.path).toString("base64");
        inlineChars += data.length;
        userParts.push({ inlineData: { mimeType: mime, data } });
      } catch {}
    }
    const contents = [...historyContents(history), { role: "user", parts: userParts }];
    if (abort.signal.aborted) throw new Error("client_aborted"); // deleted/closed before the model started
    const code = createCodeTools({ repo: cwd, signal: abort.signal, roots: [WORKTREES, path.join(os.tmpdir(), "ask-mesha", chat.id), path.join(STATE, "uploads", chat.id)] });
    const mcp = await connectMcp(meshaToolsFor(user, { send, signal: abort.signal, stopReason: () => stopReason, chatId: chat.id, tenantId: user.tenantId, allowAllTenants: user.email === "bench@local", evCtx: trackCtx, run, snapshotOnly: !streaming }), { signal: abort.signal });
    const codeNames = new Set(CODE_TOOL_DECLARATIONS.map((d) => d.name));
    const tools = {
      declarations: [...mcp.declarations, ...CODE_TOOL_DECLARATIONS],
      call: (name, args) => (codeNames.has(name) ? code[name](args) : mcp.call(name, args)),
    };
    let firstCallInTurn = true;
    started = true;
    metric.cost_usd = 0; // updated after every model call ("usage" events)
    try {
      const r = await runAgent({
        ai, model: metric.model, signal: abort.signal, budgetUsd: capUsd, maxSteps: GEMINI.maxSteps, fallbackModel: GEMINI.fastModel,
        // Time guard: past this the next turn must answer (quick lookups ~1 min; investigations longer).
        deadlineMs: (deep ? ANSWER_DEEP_SECONDS : ANSWER_SECONDS) * 1000,
        thinkingLevel: metric.effort === "high" ? "high" : "low",
        systemInstruction: geminiSystemInstruction(cwd),
        contents, tools,
        onEvent: (ev) => {
          if (ev.type === "step") {
            if (lastTurnText) full += "\n\n";
            lastTurnText = "";
            firstCallInTurn = true;
          } else if (ev.type === "text") {
            full += ev.text;
            lastTurnText += ev.text;
            gate.text(ev.text);
          } else if (ev.type === "turn_end" && ev.final) {
            gate.end();
          } else if (ev.type === "tool_call") {
            if (firstCallInTurn) {
              firstCallInTurn = false;
              gate.toolStart();
              // Only a long pre-tool preamble ever reached the screen; clear just that case.
              if (turnVisible) send({ type: "reset" });
              turnVisible = false;
              filter = makeChartFilter(emitVisible);
            }
            const legacy = LEGACY_TOOL_NAME[ev.name] || ev.name;
            const label = toolLabel(legacy, legacyInput(ev.name, ev.args));
            metric.tool_calls += 1;
            if (metric.first_tool_ms === null) metric.first_tool_ms = since();
            if (ev.name === "run_sql" || ev.name === "run_reference") metric.db_queries += 1;
            track.toolStart(ev.id, legacy, label);
            send({ type: "progress", phase: "querying", label });
          } else if (ev.type === "usage") {
            // Spend so far: a run that fails or is stopped later is charged what it really used.
            metric.cost_usd = ev.costUsd;
          } else if (ev.type === "turn_retry") {
            // The model call failed mid-turn and is retried: withdraw that turn's text.
            full = full.slice(0, full.length - lastTurnText.length);
            lastTurnText = "";
            if (turnVisible) send({ type: "reset" });
            turnVisible = false;
            filter = makeChartFilter(emitVisible);
            gate = makeTurnGate((t) => filter(t));
          } else if (ev.type === "model_fallback") {
            console.warn(`[model] ${requestId} ${ev.from} rate-limited after retries; continuing on ${ev.to}`);
            metric.model_fallback = ev.to;
          } else if (ev.type === "tool_result") {
            track.toolEnd(ev.id, ev.isError, ev.text);
            if (EVIDENCE_NAMES.has(ev.name) && !ev.isError) evidence.add(ev.text);
          }
        },
      });
      metric.turns = r.steps;
      metric.model = r.model || metric.model;
      metric.input_tokens = r.usage.input;
      metric.output_tokens = r.usage.output + r.usage.thoughts;
      metric.cache_read_tokens = r.usage.cached;
      metric.cost_usd = r.costUsd;
      if (r.error) metric.error = r.error;
    } finally {
      await mcp.close();
    }
    // Aborted (chat deleted mid-answer, tab closed) but the agent loop ended without throwing:
    // never save a summary into a deleted chat or present a cut-off run as an answer.
    if (abort.signal.aborted) throw new Error("client_aborted");
    gate.end();
    filter("", true); // flush
    // Live tokens showed the whole run; the stored/final answer is only the
    // last assistant turn (drops "now querying…" narration between tool calls).
    let { clean, chart } = extractChart(lastTurnText.trim() ? lastTurnText : full);
    ({ chart } = lintedChart(chart, metric));
    clean = stripLeadingNarration(clean);
    track.setAnswer(clean, chart);
    // Failed run with nothing to show: send an error, not an empty final (the
    // panel lets a later final overwrite an error). Partial answers still land.
    // Error text is CEO-facing: no "budget"/"agent_error_max_budget_usd".
    if (metric.error && !clean) {
      send({ type: "error", message: friendlyError(metric.error) });
      return;
    }
    // Cut short (per-answer cap / turn limit) after some text: say so instead of
    // presenting a half answer as complete. The note is also streamed.
    if (metric.error) {
      clean += STOPPED_NOTE;
      emitVisible(STOPPED_NOTE);
    }
    // Check the wording against the rows the agent actually got; a flagged draft is replaced
    // on screen by the corrected one before the final lands. Fail-open: errors keep the draft.
    // Budget left in this answer's cap (null cost = unknown spend: already charged the full cap, skip).
    const checkBudget = metric.cost_usd == null ? 0 : Math.min(CHECK_BUDGET_USD, capUsd - metric.cost_usd);
    if (CHECKER_ON && !metric.error && clean && !evidence.empty() && !abort.signal.aborted &&
        checkBudget >= 0.02 && needsCheck(clean, chart)) {
      send({ type: "progress", phase: "checking", label: "Checking the answer" });
      const checkT0 = Date.now();
      const draft = chart ? `${clean}\n\n\`\`\`chart\n${JSON.stringify(chart)}\n\`\`\`` : clean;
      const chk = await checkAnswer({
        question, answer: draft, evidence: evidence.text(),
        run: (p, signal) => runCheck(p, signal, abort.signal),
      });
      metric.check_ms = Date.now() - checkT0;
      metric.check = chk.skipped ? "skipped" : chk.error ? `error:${chk.error}` : chk.rejected ? "rejected" : chk.ok ? "ok" : "revised";
      metric.check_issues = chk.issues || [];
      // No result (timeout/abort): the spend is unknown but real; count the check's budget (fail-closed).
      metric.cost_usd += chk.costUsd ?? checkBudget;
      // Stopped, tab closed or chat deleted while checking: never save or send the answer.
      if (abort.signal.aborted) throw new Error("client_aborted");
      if (!chk.ok) {
        const fixed = extractChart(chk.revised);
        const fixedClean = stripLeadingNarration(fixed.clean);
        if (fixedClean.trim()) {
          clean = fixedClean;
          chart = lintedChart(fixed.chart, metric).chart;
          send({ type: "replace", text: clean });
          track.setAnswer(clean, chart);
        } else metric.check = "rejected";
      }
    }
    const assistantMsg = {
      id: crypto.randomUUID(), role: "assistant", content: clean, chart,
      source: "coding-agent", mode: "agent", provider, model: metric.model, request_id: requestId, created_at: new Date().toISOString(),
    };
    await store.addMessage(chat.id, assistantMsg);
    await store.updateChat(chat.id, { updated_at: assistantMsg.created_at });
    metric.total_ms = since();
    metric.ok = !metric.error;
    send({
      type: "final",
      timing: { total_ms: metric.total_ms, first_token_ms: metric.first_token_ms, db_queries: metric.db_queries }, answer: clean, source: "coding-agent", mode: "agent", request_id: requestId,
      conversation_id: chat.id, message_id: assistantMsg.id, chart,
    });
  } catch (err) {
    metric.error = abort.signal.aborted ? "client_aborted" : String(err?.message || err).slice(0, 300);
    console.error(`[ask] ${requestId} failed: ${metric.error}`);
    // Raw SDK/process errors stay in logs/metrics; the CEO sees plain wording.
    if (stopReason === "chat_deleted") send({ type: "error", status: 410, message: friendlyError("chat_deleted") });
    else if (!abort.signal.aborted) send({ type: "error", message: friendlyError(metric.error) });
  } finally {
    if (metric.total_ms === null) metric.total_ms = since();
    // No result message (client closed the tab, crash): the spend is unknown but real.
    // Count the answer's cap so the monthly hard cap stays fail-closed.
    {
      const { cost, estimated } = finalAnswerCost({ costUsd: metric.cost_usd, started, capUsd });
      metric.cost_usd = cost;
      if (estimated) {
        metric.cost_estimated = true;
        // The resumed session's next total will include this turn's real spend; pre-credit the
        // estimate so that answer's delta nets it out instead of counting it twice.
      }
    }
    clearInterval(heartbeat);
    await cleanupUploads().catch(() => {});
    await store.unlock(chat.id).catch((e) => console.error("[lock] unlock failed:", e.message));
    await recordMetric(metric);
    // The Stop signal races the disconnect; give it a moment to land.
    if (abort.signal.aborted && !stopReason) {
      await new Promise((r) => { const t = setTimeout(r, STOP_SIGNAL_GRACE_MS); onStopSignal = () => { clearTimeout(t); r(); }; });
    }
    activeRuns.delete(requestId);
    await track.finish(metric, { aborted: abort.signal.aborted, stopReason });
    if (streaming) res.end();
    else if (!res.destroyed) {
      const { status, body: out } = collector.result();
      json(res, status, out);
    }
  }
}

// ---- router ---------------------------------------------------------------
http
  .createServer((req, res) =>
    route(req, res).catch((err) => {
      console.error("[http] unhandled:", err);
      if (!res.headersSent) json(res, 500, { error: "internal" });
      else res.end();
    }),
  )
  .listen(PORT, HOST, () => (void refreshTableIndex(), console.log(`ask-mesha agent on http://127.0.0.1:${PORT} repo=${REPO} store=${store.kind} uploads=${uploads.kind}`)));

async function route(req, res) {
    const url = new URL(req.url, "http://x");
    const p = url.pathname;
    console.log(new Date().toISOString(), req.method, p);
    if (p === "/healthz") return json(res, 200, { ok: true, provider: "gemini", model: MODEL });
    // Benchmark: local-only (server binds 127.0.0.1).
    // Benchmark summary: open on a loopback bind; otherwise requires the bench token.
    if (p === "/metrics" || p === "/metrics/users" || p === "/metrics/recent") {
      const bench = process.env.ASK_MESHA_BENCH_TOKEN;
      const authz = String(req.headers["authorization"] || "");
      if (HOST !== "127.0.0.1" && !(bench && authz === `Bearer ${bench}`)) return json(res, 403, { error: "forbidden" });
      if (p === "/metrics/users") return json(res, 200, await events.usersSummary());
      if (p === "/metrics/recent") return json(res, 200, await events.recent(url.searchParams.get("email") || "", 50));
      // accuracy: eval/run.mjs regression runs over time (eval_run events).
      const accuracy = await events.evalHistory(20).catch(() => null);
      return json(res, 200, { ...(await store.metricsSummary()), model: { provider: "gemini", project: GEMINI.project, location: GEMINI.location, model: MODEL, deep_model: DEEP_MODEL, check_model: CHECK_MODEL }, accuracy });
    }
    const user = await authenticate(req);
    if (!user) {
      await events.authDenied({ email: null, tenant_id: String(req.headers["x-goatos-tenant-id"] || "") || null }, p);
      return json(res, 403, { error: "leadership_required" });
    }

    if (p === "/ceo-ai/starters") return json(res, 200, { starters: STARTERS });
    if (p === "/ceo-ai/ask" && req.method === "POST") return ask(req, res, user);
    if (p === "/ceo-ai/events" && req.method === "POST") return stopEvent(req, res, user);
    if (p === "/ceo-ai/conversations") {
      if (req.method === "POST") return json(res, 200, summary(await store.createChat(user.email, user.tenantId)));
      const mine = await store.listChats(user.email, user.tenantId);
      return json(res, 200, { conversations: mine.map(summary) });
    }
    const f = p.match(/^\/ceo-ai\/conversations\/([^/]+)\/files\/([\w-]+)$/);
    if (f && req.method === "GET") {
      const chat = await store.getChat(decodeURIComponent(f[1]));
      if (!chat || !sameOwner(chat, user) || chat.deleted_at) return json(res, 404, { error: "not_found" });
      const ref = await store.findFile(chat.id, f[2]);
      const stream = ref ? await uploads.open(chat.id, ref) : null;
      if (!ref || !stream) return json(res, 404, { error: "not_found" });
      res.writeHead(200, {
        "Content-Type": ref.type || "application/octet-stream",
        "Cache-Control": "private, max-age=86400",
        // Only types the panel previews render inline (matches admin-web _forward.ts); the rest download.
        "Content-Disposition": `${inlineDisposition(ref.type) ? "inline" : "attachment"}; filename*=UTF-8''${encodeURIComponent(ref.name)}`,
        "X-Content-Type-Options": "nosniff",
      });
      stream.on("error", (e) => { console.error("[uploads] read failed:", e.message); res.destroy(); });
      return stream.pipe(res);
    }
    const m = p.match(/^\/ceo-ai\/conversations\/([^/]+)(\/messages)?$/);
    if (m) {
      const chat = await store.getChat(decodeURIComponent(m[1]));
      if (!chat || !sameOwner(chat, user) || chat.deleted_at) return json(res, 404, { error: "not_found" });
      if (req.method === "GET") {
        const messages = await store.getMessages(chat.id);
        return json(res, 200, { messages: messages.map((x) => ({ ...x, message_id: x.id })) });
      }
      if (req.method === "PATCH") {
        const b = await readBody(req);
        if (typeof b.title === "string" && b.title.trim()) {
          chat.title = b.title.trim().slice(0, 120);
          await store.updateChat(chat.id, { title: chat.title });
        }
        return json(res, 200, summary(chat));
      }
      if (req.method === "DELETE") {
        // Soft delete: hidden from the list, kept in storage for recovery.
        await store.updateChat(chat.id, { deleted_at: new Date().toISOString() });
        watches.stop(chat.id, "stopped"); // a deleted chat has no one to stream a watch to
        // An answer still running in it (this or another tab) stops now: no more spend,
        // and no answer saved into a chat the CEO just deleted.
        for (const r of activeRuns.values()) if (r.chatId === chat.id) r.chatDeleted?.();
        return json(res, 200, { ok: true });
      }
    }
    json(res, 404, { error: "not_found" });
}
