// Local coding-agent backend for the admin-web "Ask Mesha" panel.
// Speaks the same /ceo-ai/* contract as the Go backend, so admin-web only needs
// CEO_AI_AGENT_URL pointed here. Runs the Claude Agent SDK in a per-chat git
// worktree of the live goatos commit, with the repo's own CLAUDE.md/skills and
// read-only access to goatos-stg Postgres.
import http from "node:http";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import { execFileSync, spawn } from "node:child_process";
import { createSdkMcpServer, query, tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
import { createStore } from "./store.mjs";
import { createUploads } from "./uploads.mjs";

const PORT = Number(process.env.PORT || 8787);
// 127.0.0.1 locally; the container sets HOST=0.0.0.0 (Cloud Run fronts it with IAM).
const HOST = process.env.HOST || "127.0.0.1";
const STATE = process.env.ASK_MESHA_STATE_DIR || path.join(process.env.HOME, ".ask-mesha-agent");
const REPO = process.env.GOATOS_REPO || path.join(process.env.HOME, "airnd/goatos-live");
// Default to a fast model; "deep:" prefix on a question switches to the deep model.
const MODEL = process.env.ASK_MESHA_MODEL || "claude-sonnet-5";
const DEEP_MODEL = process.env.ASK_MESHA_DEEP_MODEL || "claude-opus-5-5";
const EFFORT = process.env.ASK_MESHA_EFFORT || "low";
// Read-only mode (default): the agent gets Read/Grep/Glob + a read-only SQL tool and
// NO shell, edit, or write tools. Set ASK_MESHA_READONLY=0 only for local dev.
const READONLY = process.env.ASK_MESHA_READONLY !== "0";
// Spend caps (USD). Monthly: hard stop for new questions once reached (resets on the
// 1st, UTC). Per answer: the SDK aborts a single run that would exceed it.
const MONTHLY_BUDGET_USD = Number(process.env.ASK_MESHA_MONTHLY_BUDGET_USD || 100);
const PER_ANSWER_BUDGET_USD = Number(process.env.ASK_MESHA_PER_ANSWER_BUDGET_USD || 1);
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

const APPEND_PROMPT = `
You are answering inside the Mesha admin web "Ask Mesha" chat. The people asking are Mesha's CEOs:
they want business answers, not engineering. Use the codebase silently to understand how numbers are
defined and calculated, but do NOT mention file paths, function names, code, SQL, views, or tools in
your answer unless the user explicitly asks for them. Explain definitions in plain business language.
You have the full goatos codebase (current working directory, the live commit) and READ-ONLY
access to the goatos-stg Postgres database. ${READONLY
  ? "Read code with Read/Grep/Glob. Query data ONLY with the run_sql tool (one SELECT per call over ceo_ai.* with an explicit tenant_id filter). You cannot edit files or run shell commands."
  : "Query with `psql -c \"...\"` (connection env vars are set). You may read code and run tests."}
Answer style: lead with the direct answer in 1-2 sentences, then at most one compact table
(<= 12 rows) and at most 3 short bullets of context. No preamble, no narration of what you are
about to do, no restating the question. Go straight to the one query the data map points to.
Never merge to main, deploy, or push to main. Code changes stay on this chat's branch.
When a chart would help, add exactly one fenced block at the end of your answer:
\`\`\`chart
{"type":"bar"|"line","title":"...","x":["label1","label2",...],"series":[{"name":"...","data":[1,2,...]}]}
\`\`\`
Use real numbers from queries only. x needs at least 2 labels.
Use the mesha-data-map skill / cheat-sheet below to go straight to the right view; only explore
the schema when the map does not cover the question.`;

// Repo instructions (CLAUDE.md + its @imports, i.e. AGENTS.md) go into the SYSTEM
// prompt instead of Claude Code's per-session context message. The system prompt is
// byte-identical across chats, so its ~140k tokens are served from the prompt cache
// rather than re-written for every new chat (was ~18s + ~$0.60 per question).
function repoInstructions(cwd) {
  const main = path.join(cwd, "CLAUDE.md");
  if (!fs.existsSync(main)) return "";
  const text = fs.readFileSync(main, "utf8").replace(/^@(\S+)\s*$/gm, (_, rel) => {
    const f = path.join(cwd, rel);
    return fs.existsSync(f) ? fs.readFileSync(f, "utf8") : "";
  });
  return "\n\n# Repository instructions (CLAUDE.md)\n" + text;
}

// Always-loaded routing cheat-sheet, read per request so map updates apply without restart.
function dataMapCore(cwd) {
  for (const dir of [cwd, REPO]) {
    const f = path.join(dir, "tools/ask-mesha-agent/data-map-core.md");
    if (fs.existsSync(f)) return "\n\n# Mesha data map (cheat-sheet)\n" + fs.readFileSync(f, "utf8");
  }
  return "";
}

// ---- benchmark events -----------------------------------------------------
async function recordMetric(m) {
  await store.recordMetric(m).catch((e) => console.error("[metric] store failed:", e.message));
  console.log(
    `[metric] total=${m.total_ms}ms first_progress=${m.first_progress_ms}ms first_tool=${m.first_tool_ms ?? "-"}ms ` +
      `first_token=${m.first_token_ms ?? "-"}ms tools=${m.tool_calls} db=${m.db_queries} model=${m.model} ok=${m.ok}`,
  );
}

// ---- read-only SQL tool (replaces Bash/psql in read-only mode) --------------
const SQL_MAX_ROWS = 500;
const SQL_SOURCE_RE = /\b(?:from|join)\s+([a-z_][\w$]*)(?:\s*\.\s*([a-z_][\w$]*))?/gi;
const SQL_BANNED_RE = /\b(?:insert|update|delete|merge|alter|create|drop|truncate|grant|revoke|copy|vacuum|analyze|call|do|execute|prepare|set|reset|listen|notify|lock)\b/i;
function sqlLiteral(s) {
  return String(s).replace(/'/g, "''");
}
function validateReadSql(sql, user) {
  const text = String(sql || "").trim();
  if (!text || text.includes("\\")) {
    return { ok: false, out: "Refused: only plain SQL (no psql backslash commands)." };
  }
  if (text.includes('"')) return { ok: false, out: "Refused: double-quoted identifiers are not allowed; use ceo_ai.<view>." };
  const one = text.replace(/;\s*$/, "");
  if (one.includes(";")) return { ok: false, out: "Refused: run exactly one SQL statement." };
  if (!/^select\b/i.test(one)) return { ok: false, out: "Refused: only flat SELECT queries are allowed." };
  if (SQL_BANNED_RE.test(one)) return { ok: false, out: "Refused: mutating or session-control SQL is not allowed." };
  const sourceClause = one.match(/\bfrom\b([\s\S]*?)(?:\bwhere\b|\bgroup\s+by\b|\border\s+by\b|\blimit\b|$)/i)?.[1] || "";
  if (/,\s*[a-z_]/i.test(sourceClause)) return { ok: false, out: "Refused: comma-joined sources are not allowed; use explicit ceo_ai.* JOINs." };
  if (/\(\s*select\b/i.test(one)) return { ok: false, out: "Refused: subqueries are not allowed in this read-only tool." };
  let sawSource = false;
  for (const m of one.matchAll(SQL_SOURCE_RE)) {
    sawSource = true;
    if (!m[2] || m[1].toLowerCase() !== "ceo_ai") {
      return { ok: false, out: "Refused: queries may read only explicitly-qualified ceo_ai.* views." };
    }
  }
  if (!sawSource) return { ok: false, out: "Refused: query must read from a ceo_ai.* view." };
  if (!user?.tenantId) return { ok: false, out: "Refused: missing authenticated tenant scope." };
  const tenant = sqlLiteral(user.tenantId);
  const tenantRe = new RegExp(`\\bwhere\\b[\\s\\S]*\\btenant_id\\s*=\\s*'${tenant}'`, "i");
  if (!tenantRe.test(one)) {
    return { ok: false, out: `Refused: include tenant_id = '${tenant}' in the top-level WHERE clause.` };
  }
  return { ok: true, sql: one };
}
function runSql(sql, user) {
  return new Promise((resolve) => {
    const checked = validateReadSql(sql, user);
    if (!checked.ok) return resolve(checked);
    const child = spawn(
      "psql",
      ["-X", "-q", "-v", "ON_ERROR_STOP=1", "-P", "pager=off", "-P", "footer=off", "-A", "-F", "\t", "-f", "-"],
      {
        env: {
          PATH: process.env.PATH,
          ...loadPgEnv(),
          // Belt and braces on top of the read-only DB role.
          PGOPTIONS: "-c default_transaction_read_only=on -c statement_timeout=30000",
        },
      },
    );
    let out = "";
    let err = "";
    child.stdout.on("data", (c) => (out.length < 200_000 ? (out += c) : null));
    child.stderr.on("data", (c) => (err += c));
    child.on("close", (code) => {
      const lines = out.split("\n");
      const clipped = lines.length > SQL_MAX_ROWS + 1 ? lines.slice(0, SQL_MAX_ROWS + 1).join("\n") + `\n… (${lines.length - SQL_MAX_ROWS - 1} more rows truncated)` : out;
      resolve({ ok: code === 0, out: code === 0 ? clipped : err.trim() || `psql exited ${code}` });
    });
    child.stdin.end(`BEGIN READ ONLY;\n${checked.sql};\nROLLBACK;\n`);
  });
}
function meshaToolsFor(user) {
  return createSdkMcpServer({
    name: "mesha",
    version: "1.0.0",
    tools: [
      tool(
        "run_sql",
        "Run ONE read-only SQL query against goatos-stg (ceo_ai.* views) and return tab-separated rows (max 500). Include tenant_id = '<authenticated tenant>' in the WHERE clause. Use the mesha data map to pick the view.",
        { sql: z.string().describe("A single SELECT/WITH query. No psql backslash commands.") },
        async ({ sql }) => {
          const r = await runSql(sql, user);
          return { content: [{ type: "text", text: r.out || "(no rows)" }], isError: !r.ok };
        },
      ),
    ],
  });
}

// ---- storage: Postgres (ASK_MESHA_DATABASE_URL) or JSON file (local dev) ----
// Uploads: GCS (ASK_MESHA_UPLOADS_BUCKET) or $STATE/uploads. See store.mjs / uploads.mjs.
fs.mkdirSync(STATE, { recursive: true });
const store = await createStore({ stateDir: STATE });
const uploads = await createUploads({ stateDir: STATE });

// ---- auth: reuse the live backend's leadership check ----------------------
const authCache = new Map();
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
  if (hit && hit.exp > Date.now()) return hit.user;
  const headers = { Authorization: authz, Accept: "application/json" };
  if (tenantId) headers["X-GoatOS-Tenant-ID"] = tenantId;
  const res = await fetch(`${STG_API}/ceo-ai/starters`, { headers }).catch((e) => {
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
  authCache.set(cacheKey, { user, exp: Date.now() + 5 * 60_000 });
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
    let s = "";
    req.on("data", (c) => {
      s += c;
      if (s.length > MAX_BODY) {
        s = "";
        req.destroy();
        resolve({});
      }
    });
    req.on("end", () => {
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

// Deny list for the auto-approver (the existing panel has no approval UI yet).
const DENY = [
  /\bgit\s+push\b[^\n]*\b(main|master)\b/i,
  /\bgh\s+pr\s+merge\b/i,
  /\bmake\s+land-main\b/i,
  /\bgcloud\b[^\n]*\b(deploy|builds\s+submit|delete|update|create|set-iam|add-iam)\b/i,
  /goatos-stg-deploy/i,
  /\brm\s+-rf\s+(\/|~)(\s|$)/,
];
async function canUseTool(toolName, input) {
  if (toolName === "Bash" && DENY.some((re) => re.test(String(input.command || "")))) {
    return { behavior: "deny", message: "Blocked by Ask Mesha policy (no deploys/merges to main)." };
  }
  // Read-only mode: file tools stay inside the repo snapshot and this run's uploads.
  // Outside paths (/proc/*/environ, the state dir's .pgenv, $HOME) hold secrets.
  if (READONLY && ["Read", "Grep", "Glob"].includes(toolName)) {
    const p = input.file_path || input.path;
    if (p) {
      let real;
      try { real = fs.realpathSync(path.resolve(REPO, String(p))); } catch { real = path.resolve(REPO, String(p)); }
      const roots = [REPO, path.join(os.tmpdir(), "ask-mesha"), path.join(STATE, "uploads")].map((r) => {
        try { return fs.realpathSync(r); } catch { return path.resolve(r); }
      });
      if (!roots.some((r) => real === r || real.startsWith(r + path.sep))) {
        return { behavior: "deny", message: "Ask Mesha can only read the goatos repo and this chat's attachments." };
      }
    }
  }
  return { behavior: "allow", updatedInput: input };
}

function toolLabel(name, input) {
  if (name === "mcp__mesha__run_sql") return "Querying goatos-stg database";
  if (name === "Bash") {
    const cmd = String(input.command || "");
    return /psql/.test(cmd) ? "Querying goatos-stg database" : `Running: ${cmd.slice(0, 60)}`;
  }
  if (["Read", "Grep", "Glob"].includes(name)) return "Reading code";
  if (["Edit", "Write"].includes(name)) return "Editing code";
  if (name === "Task" || name === "Agent") return "Delegating to a sub-agent";
  if (name === "Skill") return `Using skill ${input.skill || input.command || ""}`.trim();
  return `Using ${name}`;
}

// Streams text while hiding ```chart ... ``` fences from the visible tokens.
function makeChartFilter(emit) {
  let pending = "";
  let inChart = false;
  const OPEN = "```chart";
  const CLOSE = "```";
  return (chunk, flush = false) => {
    pending += chunk;
    if (flush) {
      if (!inChart) emit(pending);
      pending = "";
      return;
    }
    for (;;) {
      if (!inChart) {
        const i = pending.indexOf(OPEN);
        if (i >= 0) { emit(pending.slice(0, i)); pending = pending.slice(i + OPEN.length); inChart = true; continue; }
        const keep = Math.min(pending.length, OPEN.length - 1);
        emit(pending.slice(0, pending.length - keep));
        pending = pending.slice(pending.length - keep);
        return;
      }
      const j = pending.indexOf(CLOSE);
      if (j < 0) return;
      pending = pending.slice(j + CLOSE.length);
      inChart = false;
    }
  };
}

function extractChart(text) {
  const m = text.match(/```chart\s*([\s\S]*?)```/);
  if (!m) return { clean: text.trim(), chart: undefined };
  let chart;
  try { chart = JSON.parse(m[1]); } catch {}
  return { clean: text.replace(m[0], "").trim(), chart };
}

// Only these variables reach the agent (and therefore its Bash tool). The server's
// own environment (cloud credentials, tokens, keys) is never inherited wholesale.
const AGENT_ENV_ALLOW = ["PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "TMPDIR", "TERM", "ANTHROPIC_API_KEY",
  // deploy-stg.sh auth modes: vertex (runtime SA via metadata server) and oauth.
  "CLAUDE_CODE_USE_VERTEX", "ANTHROPIC_VERTEX_PROJECT_ID", "CLOUD_ML_REGION", "CLAUDE_CODE_OAUTH_TOKEN"];
function agentEnv() {
  const env = {};
  for (const k of AGENT_ENV_ALLOW) if (process.env[k] !== undefined) env[k] = process.env[k];
  return env;
}

// ---- ask ------------------------------------------------------------------
// Replayed as context when the SDK session cannot be resumed.
function historyPreamble(history) {
  const turns = history.filter((m) => m.role === "user" || m.role === "assistant").slice(-20);
  if (!turns.length) return "";
  return (
    "Earlier in this conversation (for context; answer only the new question below):\n" +
    turns.map((m) => `${m.role === "user" ? "User" : "Assistant"}: ${String(m.content).slice(0, 4000)}`).join("\n\n") +
    "\n\nNew question:\n"
  );
}
async function ask(req, res, user) {
  const body = await readBody(req);
  const question = String(body.question || "").trim();
  if (!question) return json(res, 400, { error: "question_required" });
  let chat = body.conversation_id ? await store.getChat(body.conversation_id) : null;
  if (chat && !sameOwner(chat, user)) return json(res, 404, { error: "not_found" });
  // Hard monthly cap: answer with a plain message instead of calling Claude.
  // Fail closed: if spend can't be read, don't risk running past the cap.
  const spent = await store.monthSpendUsd(monthStart()).catch(() => Infinity);
  if (spent >= MONTHLY_BUDGET_USD) {
    res.writeHead(200, { "Content-Type": "text/event-stream; charset=utf-8", "Cache-Control": "no-store" });
    const answer = Number.isFinite(spent)
      ? `This month's Ask Mesha budget ($${MONTHLY_BUDGET_USD}) has been used ($${spent.toFixed(2)}). It resets on the 1st; ask Ravi to raise the cap if needed.`
      : "Ask Mesha can't check this month's spend right now, so it isn't answering to stay under the budget. Please try again shortly.";
    res.end(`data: ${JSON.stringify({ type: "final", answer, mode: "agent", source: "budget" })}\n\n`);
    return;
  }
  if (!chat) chat = await store.createChat(user.email, user.tenantId);
  // One run per chat: two concurrent resumes of the same session fork it and
  // race on session_id / message order. DB-backed lease in Postgres mode.
  if (!(await store.tryLock(chat.id))) return json(res, 409, { error: "chat_busy" });
  if (chat.title === "New chat") {
    chat.title = question.slice(0, 60);
    await store.updateChat(chat.id, { title: chat.title });
  }

  res.writeHead(200, {
    "Content-Type": "text/event-stream; charset=utf-8",
    "Cache-Control": "no-store, no-transform",
    Connection: "keep-alive",
  });
  const send = (obj) => res.write(`data: ${JSON.stringify(obj)}\n\n`);
  const heartbeat = setInterval(() => {
    res.write(": ping\n\n");
    store.refreshLock(chat.id).catch(() => {});
  }, 10_000);
  const abort = new AbortController();
  res.on("close", () => abort.abort());

  const requestId = crypto.randomUUID();
  const userMsg = { id: crypto.randomUUID(), role: "user", content: question, created_at: new Date().toISOString() };
  const t0 = Date.now();
  const deep = /^deep:\s*/i.test(question);
  let prompt = question.replace(/^deep:\s*/i, "");
  const scope = body.page_scope && typeof body.page_scope === "object" ? body.page_scope : {};
  const pageScope = {
    park_id: typeof scope.park_id === "string" && scope.park_id ? scope.park_id : "",
    shed_id: typeof scope.shed_id === "string" && scope.shed_id ? scope.shed_id : "",
  };
  prompt += `\n\nAuthenticated tenant scope: tenant_id = '${user.tenantId}'. Every SQL query must include that tenant_id filter.`;
  if (pageScope.park_id || pageScope.shed_id) {
    prompt += `\nCurrent page scope: ${pageScope.park_id ? `park_id=${pageScope.park_id}` : ""}${pageScope.park_id && pageScope.shed_id ? ", " : ""}${pageScope.shed_id ? `shed_id=${pageScope.shed_id}` : ""}. If the user's wording does not ask for all parks or another scope, apply this page scope in the SQL.`;
  }
  let history = [];
  let cleanupUploads = async () => {};
  const metric = {
    ts: new Date(t0).toISOString(), request_id: requestId, chat_id: chat.id, email: user.email,
    resumed: Boolean(chat.session_id), model: deep ? DEEP_MODEL : MODEL, effort: deep ? "high" : EFFORT,
    question_chars: prompt.length, first_progress_ms: null, first_tool_ms: null, first_token_ms: null,
    total_ms: null, tool_calls: 0, db_queries: 0, turns: 0, input_tokens: null, output_tokens: null,
    cost_usd: null, ok: false, error: null,
  };
  const since = () => Date.now() - t0;
  let full = "";
  const emitVisible = (t) => {
    if (!t) return;
    if (metric.first_token_ms === null) metric.first_token_ms = since();
    send({ type: "token", text: t });
  };
  const filter = makeChartFilter(emitVisible);
  let lastTurnText = "";

  try {
    send({ type: "progress", phase: "planning", label: "Starting agent" });
    metric.first_progress_ms = since();
    // History before this turn, for the resume-miss fallback below.
    history = await store.getMessages(chat.id);
    const up = await uploads.save(chat.id, body.attachments);
    cleanupUploads = up.cleanup;
    if (up.files.length) {
      // Persist file refs on the user turn so a reloaded chat can show them again.
      userMsg.files = up.files.map(({ id, name, type }) => ({ id, name, type }));
      prompt +=
        "\n\nThe user attached these files (open them with the Read tool; images and PDFs are supported):\n" +
        up.files.map((f) => `- ${f.path} (${f.name}${f.type ? `, ${f.type}` : ""})`).join("\n");
    }
    await store.addMessage(chat.id, userMsg);
    metric.question_chars = prompt.length;
    const cwd = await ensureWorktree(chat);
    // Session resume. Postgres mode mirrors SDK transcripts via Options.sessionStore,
    // so any instance can resume. If the transcript is missing (pre-migration chat,
    // dropped mirror batch), start a fresh session and replay the stored history.
    let resume = chat.session_id || undefined;
    if (resume && !(await store.hasSession(resume))) {
      console.warn(`[resume] session ${resume} not in store; replaying ${history.length} messages`);
      resume = undefined;
      metric.resumed = false;
      prompt = historyPreamble(history) + prompt;
    }
    const stream = query({
      prompt,
      options: {
        cwd,
        model: metric.model,
        maxBudgetUsd: PER_ANSWER_BUDGET_USD,
        effort: metric.effort,
        resume,
        ...(store.sessionStore ? { sessionStore: store.sessionStore } : {}),
        systemPrompt: { type: "preset", preset: "claude_code", append: repoInstructions(cwd) + APPEND_PROMPT + dataMapCore(cwd) },
        settingSources: ["project", "local"],
        includePartialMessages: true,
        canUseTool,
        permissionMode: "default",
        ...(READONLY
          ? {
              // Only read tools + the read-only SQL tool exist for the agent.
              tools: ["Read", "Grep", "Glob", "Skill", "TodoWrite"],
              mcpServers: { mesha: meshaToolsFor(user) },
              allowedTools: ["mcp__mesha__run_sql"],
              disallowedTools: ["Bash", "Edit", "Write", "NotebookEdit", "WebFetch", "WebSearch", "Task", "Agent"],
            }
          : {}),
        // GOATOS_AI_SETUP_GUARD=0: the repo's documented opt-out for its "install code-graph
        // tooling" nag, which otherwise blocks every tool call in fresh chat worktrees.
        env: {
          ...agentEnv(),
          ...loadPgEnv(),
          GOATOS_AI_SETUP_GUARD: "0",
          CLAUDE_CODE_DISABLE_CLAUDE_MDS: "1", // loaded via repoInstructions() instead
          ENABLE_PROMPT_CACHING_1H: "1", // CEOs ask sporadically; keep the prefix warm for an hour
        },
        abortController: abort,
      },
    });
    for await (const msg of stream) {
      if (msg.type === "system" && msg.subtype === "init" && msg.session_id) {
        if (chat.session_id !== msg.session_id) {
          chat.session_id = msg.session_id;
          await store.updateChat(chat.id, { session_id: msg.session_id });
        }
      } else if (msg.type === "stream_event") {
        const ev = msg.event;
        if (ev.type === "message_start") {
          // A new assistant turn: only the final turn's text is "the answer",
          // but intermediate narration still streams so the user sees progress.
          if (lastTurnText) { filter("\n\n"); full += "\n\n"; }
          lastTurnText = "";
        } else if (ev.type === "content_block_delta" && ev.delta?.type === "text_delta") {
          full += ev.delta.text;
          lastTurnText += ev.delta.text;
          filter(ev.delta.text);
        }
      } else if (msg.type === "assistant") {
        for (const block of msg.message.content || []) {
          if (block.type === "tool_use") {
            metric.tool_calls += 1;
            if (metric.first_tool_ms === null) metric.first_tool_ms = since();
            if (
              block.name === "mcp__mesha__run_sql" ||
              (block.name === "Bash" && /\bpsql\b/.test(String(block.input?.command || "")))
            )
              metric.db_queries += 1;
            send({ type: "progress", phase: "querying", label: toolLabel(block.name, block.input || {}) });
          }
        }
      } else if (msg.type === "result") {
        metric.turns = msg.num_turns ?? null;
        metric.cost_usd = msg.total_cost_usd ?? null;
        metric.input_tokens = msg.usage?.input_tokens ?? null;
        metric.output_tokens = msg.usage?.output_tokens ?? null;
        if (msg.subtype !== "success") metric.error = msg.subtype;
      }
    }
    filter("", true); // flush
    // Live tokens showed the whole run; the stored/final answer is only the
    // last assistant turn (drops "now querying…" narration between tool calls).
    const { clean, chart } = extractChart(lastTurnText.trim() ? lastTurnText : full);
    // Failed run with nothing to show: send an error, not an empty final (the
    // panel lets a later final overwrite an error). Partial answers still land.
    if (metric.error && !clean) {
      send({ type: "error", message: `agent_${metric.error}` });
      return;
    }
    const assistantMsg = {
      id: crypto.randomUUID(), role: "assistant", content: clean, chart,
      source: "coding-agent", mode: "agent", request_id: requestId, created_at: new Date().toISOString(),
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
    if (!abort.signal.aborted) send({ type: "error", message: metric.error });
  } finally {
    if (metric.total_ms === null) metric.total_ms = since();
    clearInterval(heartbeat);
    await cleanupUploads().catch(() => {});
    await store.unlock(chat.id).catch((e) => console.error("[lock] unlock failed:", e.message));
    await recordMetric(metric);
    res.end();
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
  .listen(PORT, HOST, () => console.log(`ask-mesha agent on http://127.0.0.1:${PORT} repo=${REPO} store=${store.kind} uploads=${uploads.kind}`));

async function route(req, res) {
    const url = new URL(req.url, "http://x");
    const p = url.pathname;
    console.log(new Date().toISOString(), req.method, p);
    if (p === "/healthz") return json(res, 200, { ok: true });
    // Benchmark: local-only (server binds 127.0.0.1).
    // Benchmark summary: open on a loopback bind; otherwise requires the bench token.
    if (p === "/metrics") {
      const bench = process.env.ASK_MESHA_BENCH_TOKEN;
      const authz = String(req.headers["authorization"] || "");
      if (HOST !== "127.0.0.1" && !(bench && authz === `Bearer ${bench}`)) return json(res, 403, { error: "forbidden" });
      return json(res, 200, await store.metricsSummary());
    }
    const user = await authenticate(req);
    if (!user) return json(res, 403, { error: "leadership_required" });

    if (p === "/ceo-ai/starters") return json(res, 200, { starters: STARTERS });
    if (p === "/ceo-ai/ask" && req.method === "POST") return ask(req, res, user);
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
        "Content-Disposition": `inline; filename="${encodeURIComponent(ref.name)}"`,
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
        return json(res, 200, { ok: true });
      }
    }
    json(res, 404, { error: "not_found" });
}
