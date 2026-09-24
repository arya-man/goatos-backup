// Per-user lifecycle events for the Ask Mesha agent.
//
// Every event is (1) one structured JSON line on stdout in the goatos Cloud
// Logging shape ({severity, message, event_name, ...}; see admin-web
// lib/api/server.ts) so log-based metrics / the Cloud Logging datasource can
// chart it, and (2) persisted: Postgres ask_mesha.events (sql/002_events.sql)
// when ASK_MESHA_DATABASE_URL is set, else $STATE/events.jsonl.
//
// Terminal events (exactly one per ask): ask_completed | ask_failed | ask_stopped.
// Everything else is a signal: ask_started, ask_first_token, ask_tool,
// budget_warning, budget_blocked, auth_denied, attachment_saved.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const TERMINAL = ["ask_completed", "ask_failed", "ask_stopped"];
export const FAILURE_CLASSES = [
  "auth", "budget_blocked", "per_answer_cap", "sdk_error", "db_error", "timeout", "client_aborted", "unknown",
];
const SEVERITY = {
  ask_failed: "ERROR", auth_denied: "WARNING", budget_warning: "WARNING", budget_blocked: "WARNING",
};

// ---- classification -------------------------------------------------------
// error: SDK result subtype (e.g. "error_max_budget_usd") or a thrown message.
export function classifyFailure(error, { aborted = false } = {}) {
  if (aborted || error === "client_aborted") return "client_aborted";
  const e = String(error || "");
  if (!e) return "unknown";
  if (/^auth|leadership_required|unauthori[sz]ed|forbidden/i.test(e)) return "auth";
  if (/budget_blocked|monthly budget/i.test(e)) return "budget_blocked";
  if (/max_budget|per_answer_cap/i.test(e)) return "per_answer_cap";
  if (/timeout|timed out|ETIMEDOUT|statement_timeout/i.test(e)) return "timeout";
  if (/pgenv|psql|postgres|ECONNREFUSED|ask_mesha\.|relation .* does not exist|database/i.test(e)) return "db_error";
  if (/^error_|claude|anthropic|vertex|sdk|process exited|overloaded|rate.?limit|api error/i.test(e)) return "sdk_error";
  return "unknown";
}

// ---- aggregation ----------------------------------------------------------
export function percentile(xs, p) {
  const v = xs.filter((x) => typeof x === "number" && Number.isFinite(x)).sort((a, b) => a - b);
  if (!v.length) return null;
  // nearest-rank
  return v[Math.min(v.length - 1, Math.max(0, Math.ceil((p / 100) * v.length) - 1))];
}
const round = (x, d = 4) => (x == null ? null : Math.round(x * 10 ** d) / 10 ** d);

function rollup(rows) {
  const done = rows.filter((r) => r.event_name === "ask_completed");
  const failed = rows.filter((r) => r.event_name === "ask_failed");
  const stopped = rows.filter((r) => r.event_name === "ask_stopped");
  const by_class = {};
  for (const r of failed) by_class[r.error_class || "unknown"] = (by_class[r.error_class || "unknown"] || 0) + 1;
  const asks = rows.length;
  const tools = rows.map((r) => r.tool_calls).filter((x) => typeof x === "number");
  return {
    asks,
    success: done.length,
    failed: failed.length,
    failed_by_class: by_class,
    stopped: stopped.length,
    success_rate: asks ? round(done.length / asks, 3) : null,
    total_ms: { p50: percentile(done.map((r) => r.total_ms), 50), p90: percentile(done.map((r) => r.total_ms), 90) },
    first_token_ms: {
      p50: percentile(done.map((r) => r.first_token_ms), 50),
      p90: percentile(done.map((r) => r.first_token_ms), 90),
    },
    avg_tools: tools.length ? round(tools.reduce((a, b) => a + b, 0) / tools.length, 2) : null,
    cost_usd: round(rows.reduce((s, r) => s + (Number(r.cost_usd) || 0), 0)),
  };
}

// Per email: today (UTC day), 7d, 30d rollups over terminal events.
export function summarizeUsers(events, now = new Date()) {
  const t = now.getTime();
  const dayStart = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  const windows = { today: dayStart, "7d": t - 7 * 864e5, "30d": t - 30 * 864e5 };
  const byUser = new Map();
  for (const e of events) {
    if (!TERMINAL.includes(e.event_name)) continue;
    const ts = Date.parse(e.ts);
    if (!(ts >= windows["30d"])) continue;
    const k = e.email || "unknown";
    if (!byUser.has(k)) byUser.set(k, []);
    byUser.get(k).push({ ...e, _t: ts });
  }
  const users = [...byUser.entries()].map(([email, rows]) => {
    const out = { email };
    for (const [name, from] of Object.entries(windows)) out[name] = rollup(rows.filter((r) => r._t >= from));
    out.last_ask_at = new Date(Math.max(...rows.map((r) => r._t))).toISOString();
    return out;
  });
  users.sort((a, b) => b["30d"].asks - a["30d"].asks || a.email.localeCompare(b.email));
  return { generated_at: now.toISOString(), users };
}

// Last N asks for an email: status + duration + question preview.
export function recentAsks(events, email, limit = 50) {
  const started = new Map();
  for (const e of events) if (e.event_name === "ask_started" && e.request_id) started.set(e.request_id, e);
  return events
    .filter((e) => TERMINAL.includes(e.event_name) && (!email || e.email === email))
    .sort((a, b) => String(b.ts).localeCompare(String(a.ts)))
    .slice(0, limit)
    .map((e) => ({
      ts: e.ts,
      request_id: e.request_id,
      chat_id: e.chat_id,
      email: e.email,
      status: e.event_name === "ask_completed" ? "success" : e.event_name === "ask_stopped" ? "stopped" : "failed",
      error_class: e.error_class || null,
      error: e.error || null,
      total_ms: e.total_ms ?? null,
      first_token_ms: e.first_token_ms ?? null,
      tool_calls: e.tool_calls ?? null,
      cost_usd: e.cost_usd ?? null,
      model: e.model || started.get(e.request_id)?.model || null,
      question_preview: e.question_preview ?? started.get(e.request_id)?.question_preview ?? null,
    }));
}

// ---- sinks ------------------------------------------------------------------
function jsonSink(stateDir) {
  const FILE = path.join(stateDir, "events.jsonl");
  fs.mkdirSync(stateDir, { recursive: true });
  const read = () =>
    fs.existsSync(FILE)
      ? fs.readFileSync(FILE, "utf8").split("\n").filter(Boolean).flatMap((l) => { try { return [JSON.parse(l)]; } catch { return []; } })
      : [];
  return {
    kind: "json",
    async write(e) { fs.appendFileSync(FILE, JSON.stringify(e) + "\n"); },
    async since(iso, email) { return read().filter((e) => e.ts >= iso && (!email || e.email === email)); },
  };
}

function pgSink(pool) {
  return {
    kind: "postgres",
    async migrate() { await pool.query(fs.readFileSync(path.join(HERE, "sql/002_events.sql"), "utf8")); },
    async write(e) {
      await pool.query(
        `INSERT INTO ask_mesha.events (ts, event_name, severity, request_id, chat_id, email, tenant_id, row)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`,
        [e.ts, e.event_name, e.severity, e.request_id ?? null, e.chat_id ?? null, e.email ?? null, e.tenant_id ?? null, JSON.stringify(e)],
      );
    },
    async since(iso, email) {
      const { rows } = await pool.query(
        `SELECT row FROM ask_mesha.events WHERE ts >= $1 AND ($2::text IS NULL OR email = $2)
           AND event_name IN ('ask_started','ask_completed','ask_failed','ask_stopped')
         ORDER BY ts LIMIT 100000`,
        [iso, email ?? null],
      );
      return rows.map((r) => r.row);
    },
  };
}

// ---- emitter ----------------------------------------------------------------
export async function createEvents({ stateDir, sink, log = (line) => console.log(line), now = () => new Date() } = {}) {
  if (!sink) {
    const url = process.env.ASK_MESHA_DATABASE_URL;
    if (url) {
      const { default: pg } = await import("pg");
      sink = pgSink(new pg.Pool({ connectionString: url, max: 2 }));
      if (process.env.ASK_MESHA_DB_MIGRATE === "1") await sink.migrate();
    } else sink = jsonSink(stateDir);
  }
  const warnedMonths = new Set();

  async function emit(event_name, ctx = {}, fields = {}) {
    const e = {
      severity: fields.severity || SEVERITY[event_name] || "INFO",
      message: event_name,
      event_name,
      component: "ask-mesha-agent",
      ts: now().toISOString(),
      request_id: ctx.request_id ?? null,
      chat_id: ctx.chat_id ?? null,
      email: ctx.email ?? null,
      tenant_id: ctx.tenant_id ?? null,
      ...fields,
    };
    try { log(JSON.stringify(e)); } catch {}
    await sink.write(e).catch((err) => console.error(`[events] store failed: ${err.message}`));
    return e;
  }

  // Tracks one /ask run. Feed it SDK messages; call finish() once in finally.
  function tracker(ctx, info = {}) {
    const t0 = info.t0 ?? Date.now();
    const since = () => Date.now() - t0;
    const open = new Map(); // tool_use_id -> {name, label, start}
    const s = { tool_errors: 0, sql_errors: 0, first_token_ms: null, answer_chars: null, chart: false, finished: false };
    emit("ask_started", ctx, {
      model: info.model, effort: info.effort, deep: Boolean(info.deep), resumed: Boolean(info.resumed),
      question_chars: info.question?.length ?? null, question_preview: String(info.question || "").slice(0, 80),
    });
    return {
      firstToken() {
        if (s.first_token_ms !== null) return;
        s.first_token_ms = since();
        emit("ask_first_token", ctx, { first_token_ms: s.first_token_ms });
      },
      attachments(files = []) {
        for (const f of files) emit("attachment_saved", ctx, { file_id: f.id, file_type: f.type || null, file_name_chars: String(f.name || "").length, bytes: f.size ?? null });
      },
      toolStart(id, name, label) { if (id) open.set(id, { name, label, start: Date.now() }); },
      toolEnd(id, isError, text) {
        const t = open.get(id);
        if (!t) return;
        open.delete(id);
        const ok = !isError;
        if (!ok) { s.tool_errors += 1; if (t.name === "mcp__mesha__run_sql") s.sql_errors += 1; }
        emit("ask_tool", ctx, {
          tool: t.name, label: t.label, duration_ms: Date.now() - t.start, ok,
          ...(ok ? {} : { error: String(text || "").slice(0, 200), severity: "WARNING" }),
        });
      },
      // SDK message hook: tool_use (assistant) -> tool_result (user).
      onMessage(msg, labelFor = () => null) {
        if (msg?.type === "assistant") {
          for (const b of msg.message?.content || []) if (b.type === "tool_use") this.toolStart(b.id, b.name, labelFor(b.name, b.input || {}));
        } else if (msg?.type === "user") {
          const content = msg.message?.content;
          if (!Array.isArray(content)) return;
          for (const b of content) {
            if (b.type !== "tool_result") continue;
            const text = typeof b.content === "string" ? b.content : Array.isArray(b.content) ? b.content.map((c) => c.text || "").join(" ") : "";
            this.toolEnd(b.tool_use_id, b.is_error, text);
          }
        }
      },
      setAnswer(text, chart) { s.answer_chars = String(text || "").length; s.chart = Boolean(chart); },
      // metric: the server's metric object. stopped: the client closed mid-run.
      async finish(metric, { aborted = false } = {}) {
        if (s.finished) return;
        s.finished = true;
        for (const [id] of open) this.toolEnd(id, true, "unfinished");
        const base = {
          total_ms: metric.total_ms ?? since(), first_token_ms: metric.first_token_ms ?? s.first_token_ms,
          tool_calls: metric.tool_calls, db_queries: metric.db_queries, tool_errors: s.tool_errors, sql_errors: s.sql_errors,
          turns: metric.turns, model: metric.model, effort: metric.effort, deep: Boolean(info.deep),
          cost_usd: metric.cost_usd, input_tokens: metric.input_tokens, output_tokens: metric.output_tokens,
          question_preview: String(info.question || "").slice(0, 80),
        };
        if (aborted || metric.error === "client_aborted") {
          return emit("ask_stopped", ctx, { ...base, error_class: "client_aborted", reason: "client_closed" });
        }
        if (metric.ok && !metric.error) {
          return emit("ask_completed", ctx, { ...base, answer_chars: s.answer_chars, chart: s.chart });
        }
        const cls = classifyFailure(metric.error);
        return emit("ask_failed", ctx, { ...base, error_class: cls, error: String(metric.error || "").slice(0, 300), answer_chars: s.answer_chars });
      },
    };
  }

  return {
    kind: sink.kind,
    emit,
    tracker,
    // Once per process per month at >= 80 %.
    budgetCheck(ctx, spent, budget) {
      const month = now().toISOString().slice(0, 7);
      if (Number.isFinite(spent) && spent >= 0.8 * budget && spent < budget && !warnedMonths.has(month)) {
        warnedMonths.add(month);
        emit("budget_warning", ctx, { spent_usd: round(spent), budget_usd: budget, pct: round(spent / budget, 3) });
      }
    },
    async budgetBlocked(ctx, spent, budget, question) {
      const f = { spent_usd: Number.isFinite(spent) ? round(spent) : null, budget_usd: budget, spend_unreadable: !Number.isFinite(spent) };
      await emit("budget_blocked", ctx, f);
      await emit("ask_failed", ctx, { ...f, error_class: "budget_blocked", error: "monthly budget reached", total_ms: 0, question_preview: String(question || "").slice(0, 80) });
    },
    async authDenied(ctx, pathName) {
      await emit("auth_denied", ctx, { path: pathName });
      if (pathName === "/ceo-ai/ask") await emit("ask_failed", ctx, { error_class: "auth", error: "leadership_required", total_ms: 0 });
    },
    async usersSummary() {
      const from = new Date(now().getTime() - 30 * 864e5).toISOString();
      return summarizeUsers(await sink.since(from), now());
    },
    async recent(email, limit = 50) {
      const from = new Date(now().getTime() - 90 * 864e5).toISOString();
      return { email: email || null, asks: recentAsks(await sink.since(from, email || null), email, limit) };
    },
  };
}
