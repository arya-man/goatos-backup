// Pure helpers for the Ask Mesha agent server (no I/O at import time, so tests can load them).
import fs from "node:fs";
import path from "node:path";

// ---- deep-mode routing -------------------------------------------------------
// Investigations (verify / why / bug / "is this right") get the deep model. Plain
// lookups that merely say "check" or "compare" ("check how many goats we sold",
// "compare sales by park") stay on the fast model. ['’] covers phone keyboards.
export const DEEP_HINT =
  /\b(verify|verif(y|ied|ication of)|double[- ]check|check (if|whether|that|this|these|those|why)|bug|wrong|incorrect|explain|investigate|mismatch|discrepanc\w*|reconcile|doesn['’]?t (match|add up|look right)|does ?not (match|add up)|not right|seems? off|is (this|that|it) (right|correct|accurate)|how (is|was|are|do we calculate) .* calculated|dig deeper|check again|are you sure|look again)\b/i;
// "why" is an investigation when it questions a number/state ("why is ADG down?"), but a plain
// lookup when it asks for the reasons recorded on records ("who rejected approvals and why",
// "pen visits why delayed"): those reasons are a column away, not a code trace. Seen live: such
// lookups went to the deep model at high effort and took 42-48s instead of ~15s.
const WHY_INVESTIGATE = /\bwhy (is|are|does|do|did|was|were|has|have|would|isn['’]?t|aren['’]?t|doesn['’]?t|didn['’]?t|hasn['’]?t|wasn['’]?t)\b/i;
const RECORD_REASON = /\b(delay(ed|s)?|reject(ed|ions?)?|cancel+(ed|ations?)?|missed|skipped|late|overdue|pending|declined|refused|absent|leave)\b/i;
const POINTS_AT_A_NUMBER = /\b(this|that|these|those|the (number|total|count|dashboard|screen|report)|showing|shows)\b/i;
export function isDeepQuestion(question, attachments) {
  const q = String(question || "");
  if (/^deep:\s*/i.test(q) || (Array.isArray(attachments) && attachments.length > 0) || DEEP_HINT.test(q)) return true;
  if (/^\W*(and |but )?why\W*$/i.test(q)) return true; // bare "why?" follow-up pushes further
  return WHY_INVESTIGATE.test(q) && (!RECORD_REASON.test(q) || POINTS_AT_A_NUMBER.test(q));
}

// ---- spend -------------------------------------------------------------------
// Per-answer SDK cap, never more than what is left of the monthly budget.
// inFlight = sum of the caps of answers already running, so parallel asks can't jointly overshoot.
export function answerCapUsd({ deep, perAnswer, deepAnswer, monthly, spent, inFlight = 0 }) {
  const cap = deep ? deepAnswer : perAnswer;
  const left = Math.max(0, monthly - (Number.isFinite(spent) ? spent : monthly) - (Number.isFinite(inFlight) ? inFlight : 0));
  return Math.max(0.01, Math.min(cap, left));
}
// SDK total_cost_usd is cumulative for a resumed session ("the first result already
// carries the earlier turns"), so a resumed chat's answer costs total - previous total.
export function answerCostUsd(total, prevSessionTotal) {
  if (total == null) return null;
  const t = Number(total);
  if (!Number.isFinite(t)) return null;
  const prev = Number(prevSessionTotal) || 0;
  // prev may include an estimate for an aborted turn (see server.mjs finally); the real
  // spend of that turn shows up in this total, so the pair nets out to the true cost.
  return Math.max(0, +(t - prev).toFixed(6));
}

// ---- CEO-facing wording ------------------------------------------------------
// Never show raw SDK/infra errors or the words budget/tool/session to CEOs.
export const STOPPED_NOTE =
  "\n\n_I stopped before finishing this one. Ask me to continue, or narrow the question (one park or one month) for a complete answer._";
export function friendlyError(kind) {
  if (kind === "error_max_budget_usd" || kind === "error_max_turns")
    return "That question needed more work than I can do in one go. Try narrowing it (one park, one month) or ask it in parts.";
  if (kind === "busy") return "I'm still answering your previous question in this chat. Please wait for it to finish.";
  return "Sorry, something went wrong while I was working on that. Please try again in a moment.";
}

// ---- run_sql -----------------------------------------------------------------
export const SQL_MAX_ROWS = 500;
export const SQL_MAX_CHARS = 100_000;
// No query rules: the DB role (mesha_ceo_readonly) reads every table and writes none,
// and every call runs in a READ ONLY transaction. The only refusal is psql backslash
// commands, which run programs on the server rather than read data.
export function validateReadSql(sql) {
  const text = String(sql || "").trim();
  if (!text) return { ok: false, out: "Empty query." };
  if (text.includes("\\")) return { ok: false, out: "Refused: psql backslash commands are not allowed." };
  const one = text.replace(/;\s*$/, "");
  // One statement only, and never transaction/session control: the query runs inside
  // BEGIN READ ONLY … ROLLBACK, so a COMMIT/SET could otherwise step outside it.
  if (one.includes(";")) return { ok: false, out: "Refused: send one statement at a time (no ';' inside the query)." };
  if (/^\s*(commit|rollback|end|abort|set|reset|begin|start)\b/i.test(one)) {
    return { ok: false, out: "Refused: transaction or session commands are not allowed; send a single SELECT/WITH query." };
  }
  return { ok: true, sql: one };
}
// describe_table: columns (+ type, comment) of one table/view, and the distinct values of
// its short text "kind" columns (category/status/type/…), so the agent can pick the right
// filter without guessing column names. The name is validated to a strict identifier
// pattern and passed only as a quoted literal; the query itself is a fixed catalog read.
const IDENT = /^[a-z_][a-z0-9_]{0,62}$/;
export function describeTableSql(name) {
  const raw = String(name || "").trim().toLowerCase().replace(/"/g, "");
  const parts = raw.includes(".") ? raw.split(".") : ["public", raw];
  if (parts.length !== 2 || !parts.every((p) => IDENT.test(p))) {
    return { ok: false, out: "Give one table as schema.table (letters, digits, underscore), e.g. public.pc_care_tasks." };
  }
  const [schema, table] = parts;
  const sql = `SELECT a.attname AS column, format_type(a.atttypid, a.atttypmod) AS type,
  concat_ws('; ', nullif(col_description(c.oid, a.attnum), ''),
    (SELECT 'joins ' || string_agg(fn.nspname || '.' || fc.relname || '.' || fa.attname, ', ')
       FROM pg_constraint k JOIN pg_class fc ON fc.oid = k.confrelid JOIN pg_namespace fn ON fn.oid = fc.relnamespace
       JOIN pg_attribute fa ON fa.attrelid = k.confrelid AND fa.attnum = k.confkey[1]
      WHERE k.contype = 'f' AND k.conrelid = c.oid AND array_length(k.conkey, 1) = 1 AND k.conkey[1] = a.attnum)) AS note
FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = '${schema}' AND c.relname = '${table}' AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`;
  return { ok: true, sql, schema, table };
}
// describe_table takes one name or several (comma/space separated, max 6) so the agent can
// learn every candidate table in ONE turn instead of one turn per table.
export const DESCRIBE_MAX_TABLES = 6;
export function describeTableNames(input) {
  const list = Array.isArray(input) ? input : String(input || "").split(/[\s,]+/);
  return [...new Set(list.map((t) => String(t || "").trim()).filter(Boolean))].slice(0, DESCRIBE_MAX_TABLES);
}
// Tables a failed query referenced (schema.table or bare names after FROM/JOIN), so a
// "column does not exist" error can come back with the real column lists and the agent
// fixes the query in the next turn instead of spending a turn on describe_table.
export function sqlTableRefs(sql, max = 4) {
  const text = String(sql || "");
  const out = [];
  for (const m of text.matchAll(/\b(?:from|join)\s+("?[a-z_][a-z0-9_]*"?(?:\s*\.\s*"?[a-z_][a-z0-9_]*"?)?)/gi)) {
    const name = m[1].replace(/["\s]/g, "").toLowerCase();
    const full = name.includes(".") ? name : `public.${name}`;
    if (!out.includes(full)) out.push(full);
  }
  return out.slice(0, max);
}
export function isMissingColumnError(msg) {
  return /column .* does not exist|relation .* does not exist|missing FROM-clause entry/i.test(String(msg || ""));
}
// Text columns whose name suggests a small set of values worth listing.
export const KIND_COLUMN = /(^|_)(category|status|type|kind|action|state|stage|reason|outcome|result|source|mode|channel|role|task)$/;
// Only base tables up to ~2M rows are sampled: grouping a big view can take a minute.
export function relInfoSql(schema, table) {
  if (!IDENT.test(schema) || !IDENT.test(table)) return "";
  return `SELECT c.relkind, GREATEST(c.reltuples,0)::bigint FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='${schema}' AND c.relname='${table}'`;
}
export function shouldSampleKinds(relkind, rows) {
  return (relkind === "r" || relkind === "p") && Number(rows) <= 2_000_000;
}
export function kindValuesSql(schema, table, columns) {
  const cols = columns.filter((c) => KIND_COLUMN.test(c) && IDENT.test(c)).slice(0, 6);
  if (!cols.length) return "";
  return cols.map((c) => `(SELECT '${c}' AS column, string_agg(coalesce(v,'(blank)') || ' (' || n || ')', ' | ' ORDER BY n DESC) AS top_values FROM (SELECT ${c}::text AS v, count(*) n FROM (SELECT ${c} FROM ${schema}.${table} LIMIT 200000) t GROUP BY 1 ORDER BY 2 DESC LIMIT 25) s)`).join("\nUNION ALL\n");
}

export function clipSqlOutput(out, { maxRows = SQL_MAX_ROWS, maxChars = SQL_MAX_CHARS, capped = false } = {}) {
  let lines = out.replace(/\n$/, "").split("\n");
  const notes = [];
  if (lines.length > maxRows + 1) {
    notes.push(`${lines.length - maxRows - 1} more rows truncated; aggregate or add LIMIT`);
    lines = lines.slice(0, maxRows + 1);
  }
  let text = lines.join("\n");
  if (text.length > maxChars || capped) {
    text = text.slice(0, maxChars);
    notes.push(`output clipped at ${maxChars} characters; select fewer/narrower columns`);
  }
  return notes.length ? `${text}\n… (${notes.join("; ")})` : text;
}

// ---- activity-step labels (plain English, no paths/SQL) ------------------------
const TOPIC_WORDS = [
  [/weigh/i, "weighing"], [/sale|sold|animals_base|exit/i, "sales and exits"], [/feed/i, "feed"],
  [/vacc/i, "vaccination"], [/mortal|death/i, "mortality"], [/procure|load/i, "procurement"],
  [/verif/i, "verification"], [/pc_care|deworm|trimm|tick/i, "preventive care"], [/workforce|roster|clock/i, "workforce"], [/count|movement|current_scope/i, "headcount"],
  [/growth|adg|gain/i, "daily gain"],
];
export function topicOf(text) {
  const hit = TOPIC_WORDS.find(([re]) => re.test(text));
  return hit ? hit[1] : null;
}
export function toolLabel(name, input = {}) {
  input = input || {};
  if (name === "mcp__mesha__run_sql" || (name === "Bash" && /\bpsql\b/.test(String(input.command || "")))) {
    const sql = String(input.sql || input.command || "");
    const tables = [...sql.matchAll(/\b(?:ceo_ai|public|analytics)\.(\w+)/g)].map((m) => m[1]);
    const topic = topicOf(tables.join(" ") || sql);
    return topic ? `Checking ${topic} records` : "Checking the records";
  }
  if (name === "mcp__mesha__describe_table") {
    const topic = topicOf(String(input.table || ""));
    return topic ? `Checking what the ${topic} records hold` : "Checking what the records hold";
  }
  if (name === "Read") {
    const f = String(input.file_path || "");
    if (/uploads|ask-mesha\//.test(f)) return /\.(png|jpe?g|gif|webp)$/i.test(f) ? "Looking at your screenshot" : "Reading your file";
    if (/mesha-data-map|data-map-core/.test(f)) return "Using the Mesha data map";
    const topic = topicOf(f);
    return topic ? `Looking up how ${topic} is worked out` : "Looking up how it's worked out";
  }
  if (name === "Grep" || name === "Glob") {
    const topic = topicOf(String(input.pattern || "") + " " + String(input.path || ""));
    return topic ? `Looking up how ${topic} is worked out` : "Looking up the definitions";
  }
  if (name === "Skill") return "Using the Mesha data map";
  if (name === "TodoWrite") return "Planning the checks";
  if (name === "Bash") return "Double-checking the numbers";
  return "Working";
}

// ---- streaming chart filter --------------------------------------------------
// Streams text while hiding ```chart ... ``` fences from the visible tokens, even
// when the fence markers are split across chunks.
export function makeChartFilter(emit) {
  let pending = "";
  let inChart = false;
  const OPEN = "```chart";
  const CLOSE = "```";
  return (chunk, flush = false) => {
    pending += chunk;
    for (;;) {
      if (!inChart) {
        const i = pending.indexOf(OPEN);
        if (i >= 0) { emit(pending.slice(0, i)); pending = pending.slice(i + OPEN.length); inChart = true; continue; }
        const keep = flush ? 0 : Math.min(pending.length, OPEN.length - 1);
        emit(pending.slice(0, pending.length - keep));
        pending = pending.slice(pending.length - keep);
        return;
      }
      const j = pending.indexOf(CLOSE);
      if (j < 0) {
        if (flush) pending = "";
        // Hold only a possible partial close marker; the fence body is never shown.
        else pending = pending.slice(-(CLOSE.length - 1));
        return;
      }
      pending = pending.slice(j + CLOSE.length);
      inChart = false;
    }
  };
}

export function extractChart(text) {
  const m = text.match(/```chart\s*([\s\S]*?)```/);
  if (!m) return { clean: text.replace(/```chart[\s\S]*$/, "").trim(), chart: undefined };
  let chart;
  try { chart = JSON.parse(m[1]); } catch {}
  if (chart && !(Array.isArray(chart.x) && chart.x.length >= 2 && Array.isArray(chart.series))) chart = undefined;
  return { clean: text.replace(/```chart\s*[\s\S]*?```/g, "").trim(), chart };
}

// ---- history replay ----------------------------------------------------------
export function historyPreamble(history) {
  const turns = history.filter((m) => m.role === "user" || m.role === "assistant").slice(-20);
  if (!turns.length) return "";
  return (
    "Earlier in this conversation (for context; answer only the new question below):\n" +
    turns.map((m) => `${m.role === "user" ? "User" : "Assistant"}: ${String(m.content).slice(0, 4000)}`).join("\n\n") +
    "\n\nNew question:\n"
  );
}

// ---- read-path restriction -----------------------------------------------------
const real = (p) => { try { return fs.realpathSync(p); } catch { return path.resolve(p); } };
export function pathAllowed(p, base, roots) {
  const r = real(path.resolve(base, String(p)));
  return roots.map(real).some((root) => r === root || r.startsWith(root + path.sep));
}

// ---- tiny TTL cache with pruning (auth) ----------------------------------------
export function ttlCache(max = 1000) {
  const m = new Map();
  return {
    get(k) { const h = m.get(k); if (!h) return undefined; if (h.exp <= Date.now()) { m.delete(k); return undefined; } return h.v; },
    set(k, v, ttlMs) {
      if (m.size >= max) { const now = Date.now(); for (const [kk, h] of m) if (h.exp <= now) m.delete(kk); }
      while (m.size >= max) m.delete(m.keys().next().value);
      m.set(k, { v, exp: Date.now() + ttlMs });
    },
    get size() { return m.size; },
  };
}
