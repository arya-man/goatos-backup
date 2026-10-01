// Pure helpers for the Ask Mesha agent server (no I/O at import time, so tests can load them).
import fs from "node:fs";
import path from "node:path";

// ---- deep-mode routing -------------------------------------------------------
// Investigations (verify / why / bug / "is this right") get the deep model. Plain
// lookups that merely say "check" or "compare" ("check how many goats we sold",
// "compare sales by park") stay on the fast model. ['’] covers phone keyboards.
export const DEEP_HINT =
  /\b(verify|verif(y|ied|ication of)|double[- ]check|check (if|whether|that|this|these|those|why)|bug|wrong|incorrect|explain|investigate|mismatch|discrepanc\w*|reconcile|doesn['’]?t (match|add up|look right)|does ?not (match|add up)|not right|seems? off|is (this|that|it) (right|correct|accurate)|how (is|was|are|do we calculate) .* calculated|dig deeper|check again|are you sure|look again|root cause|find out|trace|what happened|missing from|left out|only covers?|doesn['’]?t cover|not showing|why (only|not|so (many|few|high|low)))\b/i;
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

// Cost of a failed provider attempt that is about to be retried. No result message (the
// SDK died/was aborted mid-retry): the spend is unknown, so count the attempt's cap
// (conservative, same rule as a run that never returns a result).
export function failedAttemptCostUsd(costUsd, capUsd, started = true) {
  if (costUsd != null && Number.isFinite(Number(costUsd))) return Number(costUsd);
  return started ? capUsd : 0;
}
// Final recorded cost of an answer. `costUsd` is the last attempt's cost (null = no result),
// `failedAttemptCost` what earlier (fallback) attempts spent. Used on every path, including
// a retry that throws, so an earlier attempt's spend is never dropped.
export function finalAnswerCost({ costUsd, started, capUsd, failedAttemptCost = 0 }) {
  let cost = costUsd;
  let estimated = false;
  if (cost == null && started) { cost = capUsd; estimated = true; }
  if (failedAttemptCost) cost = +((cost ?? 0) + failedAttemptCost).toFixed(6);
  return { cost, estimated };
}
// The panel's Stop can land before the chat row exists (new chat): ownership is checked
// against the user who started the request, not the chat.
export function runOwnedBy(run, user) {
  return Boolean(run && user && run.email === user.email && (run.tenantId ?? "") === (user.tenantId ?? ""));
}

// ---- CEO-facing wording ------------------------------------------------------
// Never show raw SDK/infra errors or the words budget/tool/session to CEOs.
export const STOPPED_NOTE =
  "\n\n_I stopped before finishing this one. Ask me to continue, or narrow the question (one park or one month) for a complete answer._";
export function friendlyError(kind) {
  if (kind === "error_max_budget_usd" || kind === "error_max_turns")
    return "That question needed more work than I can do in one go. Try narrowing it (one park, one month) or ask it in parts.";
  if (kind === "chat_gone") return "This chat no longer exists — starting a new one.";
  if (kind === "chat_deleted") return "This chat was deleted, so I stopped answering.";
  if (kind === "busy") return "I'm still answering your previous question in this chat. Please wait for it to finish.";
  return "Sorry, something went wrong while I was working on that. Please try again in a moment.";
}

// ---- run_sql -----------------------------------------------------------------
export const SQL_MAX_ROWS = 500;
export const SQL_MAX_CHARS = 100_000;
// No query rules: the DB role (mesha_ceo_readonly) reads every table and writes none,
// and every call runs in a READ ONLY transaction. The only refusal is psql backslash
// commands, which run programs on the server rather than read data.
// A backslash is only a psql meta-command outside string literals. Plain '...' strings (with ''
// escapes, standard_conforming_strings=on, forced in runSql) may hold regex backslashes such
// as '\\1' (reference queries use them); anything else with a backslash is refused, including
// E'...' / U&'...' strings, whose escape rules differ, and dollar-quoted bodies.
export function hasPsqlBackslash(text) {
  if (!text.includes("\\")) return false;
  if (/(^|[^a-z0-9_])(e|u&)'/i.test(text)) return true;
  return text.replace(/'(?:[^']|'')*'/g, "''").includes("\\");
}
// True when ';' appears outside plain '...' literals and quoted identifiers. Comments are code
// here (a quote inside "-- it's" must not hide a later ';'), and any ';' in a query that uses
// dollar quoting counts, since $tag$ bodies are not scanned.
export function hasStatementBreak(sql) {
  const t = String(sql);
  if (t.includes("$") && /\$[a-z_]*\$/i.test(t)) return t.includes(";");
  let i = 0;
  while (i < t.length) {
    const c = t[i];
    if (c === "-" && t[i + 1] === "-") { const n = t.indexOf("\n", i); if (n < 0) return false; i = n + 1; continue; }
    if (c === "/" && t[i + 1] === "*") { const n = t.indexOf("*/", i + 2); if (n < 0) return false; i = n + 2; continue; }
    if (c === "'" || c === '"') {
      let j = i + 1;
      for (;;) {
        const k = t.indexOf(c, j);
        if (k < 0) return t.slice(i).includes(";"); // unterminated: be strict
        if (t[k + 1] === c) { j = k + 2; continue; }
        i = k + 1; break;
      }
      continue;
    }
    if (c === ";") return true;
    i++;
  }
  return false;
}
export function validateReadSql(sql) {
  const text = String(sql || "").trim();
  if (!text) return { ok: false, out: "Empty query." };
  if (hasPsqlBackslash(text)) return { ok: false, out: "Refused: psql backslash commands are not allowed." };
  const one = text.replace(/;\s*$/, "");
  // One statement only, and never transaction/session control: the query runs inside
  // BEGIN READ ONLY … ROLLBACK, so a COMMIT/SET could otherwise step outside it.
  // A ';' inside a plain '...' string literal (e.g. concat_ws('; ', ...)) is data, not a statement break.
  if (hasStatementBreak(one)) return { ok: false, out: "Refused: send one statement at a time (no ';' inside the query)." };
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
    notes.push(`only the first ${maxRows} of ${lines.length - 1} rows are shown; ${lines.length - maxRows - 1} more rows truncated. Aggregate or add LIMIT and re-run before answering; never present these ${maxRows} rows as the full list, and if the user asked for every row say plainly that the list is long and offer a summary or a narrower list`);
    lines = lines.slice(0, maxRows + 1);
  }
  let text = lines.join("\n");
  if (text.length > maxChars || capped) {
    text = text.slice(0, maxChars);
    notes.push(`output clipped at ${maxChars} characters; select fewer/narrower columns`);
  }
  return notes.length ? `${text}\n… (${notes.join("; ")})` : text;
}

// ---- attachments ---------------------------------------------------------------
// What the agent's Read tool can actually open. Anything else (Excel, Word, HEIC, zip…)
// is still stored, but the model is told it can't open it so it asks for a PDF/PNG/CSV
// instead of failing a tool call or guessing at the contents.
export const MAX_ATTACHMENTS = 5;
const TEXT_EXT = /\.(csv|tsv|txt|md|json)$/i;
export function attachmentKind(name = "", type = "") {
  const t = String(type).toLowerCase();
  if (/^image\/(png|jpe?g|gif|webp)$/.test(t) || (!t && /\.(png|jpe?g|gif|webp)$/i.test(name))) return "image";
  if (t === "application/pdf" || (!t && /\.pdf$/i.test(name))) return "pdf";
  if (/^text\//.test(t) || t === "application/json" || TEXT_EXT.test(name)) return "text";
  return "unsupported";
}
export function attachmentPrompt(files, received = files.length) {
  const lines = files.map((f) => attachmentKind(f.name, f.type) === "unsupported"
    ? `- ${f.path} (${f.name}${f.type ? `, ${f.type}` : ""}) — this file type can't be opened here; do not try to Read it. Tell the user plainly and ask them to send it as a PDF, a PNG/JPEG screenshot or a CSV.`
    : `- ${f.path} (${f.name}${f.type ? `, ${f.type}` : ""})`);
  let text = "\n\nThe user attached these files (open them with the Read tool; images, PDFs and text/CSV are supported):\n" + lines.join("\n");
  if (received > files.length) text += `\n(The user attached ${received} files; only the first ${files.length} were kept. Mention that you looked at the first ${files.length} only.)`;
  return text;
}

// ---- dates ---------------------------------------------------------------------
// The server runs in UTC (Cloud Run) but Mesha's business day is IST. Between 00:00 and
// 05:30 IST the machine's date is still "yesterday", so each turn states the IST date.
export function istNowNote(now = new Date()) {
  const ist = new Date(now.getTime() + 330 * 60_000);
  const day = ist.toISOString().slice(0, 10);
  const hm = ist.toISOString().slice(11, 16);
  const wd = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"][ist.getUTCDay()];
  return `[Context note, not from the user: it is now ${wd} ${day} ${hm} IST (Asia/Kolkata). "Today", "yesterday" and "this week/month" mean IST calendar days; use (now() AT TIME ZONE 'Asia/Kolkata')::date in SQL, not current_date.]`;
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
// Plain progress labels for run_reference (no file names in the CEO-facing UI).
const REFERENCE_LABELS = {
  "pens.sql": "Checking the pen records",
  "pen-weighing-latest.sql": "Checking the pen weighing records",
  "adg-by-park.sql": "Checking the weight gain records",
  "adg-by-breed.sql": "Checking the weight gain records by breed",
  "cost-per-kg-gain.sql": "Checking the feed cost and weight gain records",
  "load-wise-sales.sql": "Checking the load-wise sales records",
  "herd-avg-weight.sql": "Checking the herd weight records",
  "feed-stock-days-left.sql": "Checking the feed stock records",
};
export function toolLabel(name, input = {}) {
  input = input || {};
  if (name === "mcp__mesha__run_sql" || (name === "Bash" && /\bpsql\b/.test(String(input.command || "")))) {
    const sql = String(input.sql || input.command || "");
    const tables = [...sql.matchAll(/\b(?:ceo_ai|public|analytics)\.(\w+)/g)].map((m) => m[1]);
    const topic = topicOf(tables.join(" ") || sql);
    return topic ? `Checking ${topic} records` : "Checking the records";
  }
  if (name === "mcp__mesha__run_reference") return REFERENCE_LABELS[String(input.name || "")] || "Checking the records";
  if (name === "mcp__mesha__watch_tags") return Number(input.minutes) === 0 ? "Reading live ear-tag data" : "Watching live ear-tag data";
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

// ---- chart lint ---------------------------------------------------------------
// Deterministic structural check before a chart reaches the CEO. A chart that could mislead is
// dropped (the text answer stays): a wrong picture is worse than none. Returns the chart (with
// missing readings normalised to null) or undefined, plus the reason it was dropped.
export const CHART_MAX_SERIES = 7;
export const CHART_MAX_BARS = 25;
export function lintChart(chart) {
  if (!chart) return { chart: undefined, reason: null };
  const drop = (reason) => ({ chart: undefined, reason });
  if (chart.type !== "bar" && chart.type !== "line") return drop("type");
  const { x, series } = chart;
  if (!Array.isArray(x) || x.length < 2) return drop("x_short");
  if (!x.every((l) => typeof l === "string" || (typeof l === "number" && Number.isFinite(l)))) return drop("x_label");
  const labels = x.map((l) => String(l).trim());
  if (labels.some((l) => !l)) return drop("x_label");
  if (new Set(labels).size !== labels.length) return drop("x_duplicate");
  if (chart.type === "bar" && labels.length > CHART_MAX_BARS) return drop("too_many_bars");
  if (!Array.isArray(series) || !series.length) return drop("no_series");
  if (series.length > CHART_MAX_SERIES) return drop("too_many_series");
  const names = new Set();
  const out = [];
  for (const s of series) {
    if (!s || !Array.isArray(s.data)) return drop("series_shape");
    if (s.data.length !== labels.length) return drop("length_mismatch");
    if (!s.data.every((v) => v === null || (typeof v === "number" && Number.isFinite(v)))) return drop("non_finite");
    const real = s.data.filter((v) => v !== null).length;
    if (real === 0) return drop("all_null_series");
    const name = String(s.name ?? "").trim();
    if (series.length > 1 && (!name || names.has(name))) return drop("series_name");
    names.add(name);
    out.push({ name: name || String(chart.title ?? ""), data: s.data });
  }
  if (!out.some((s) => s.data.filter((v) => v !== null).length >= 2)) return drop("too_few_values");
  // "A vs B" promises two things compared; one series cannot show a comparison.
  const title = String(chart.title ?? "").trim();
  if (/\b(vs\.?|versus|compared (to|with))\b/i.test(title) && out.length < 2) return drop("title_vs_single_series");
  return { chart: { type: chart.type, title, x: labels, series: out }, reason: null };
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

// File route: only these types are shown inline (same allow-list as admin-web _forward.ts);
// anything else (html, svg, text…) is served as a download so it can't render in our origin.
export const INLINE_FILE_TYPES = new Set(["image/png", "image/jpeg", "image/jpg", "image/gif", "image/webp", "application/pdf"]);
export function inlineDisposition(type) {
  return INLINE_FILE_TYPES.has(String(type || "").split(";")[0].trim().toLowerCase());
}

// ---- narration gate ------------------------------------------------------------
// Text the model writes before a tool call is narration ("Let me check…"). Streaming it and then
// clearing it on the tool call made the answer area flash. Hold each turn's text until it is
// clearly the answer (the turn ended without a tool call, or it grew past a narration-sized
// prefix); a turn that turns into a tool call is dropped without ever being shown.
export const NARRATION_HOLD_CHARS = 280;
// A working line the model sometimes writes before the answer in the SAME turn
// ("Confirming there's genuinely no weighing activity…", "Let me pull the pen list."). Only the
// FIRST paragraph, only when it opens with a working verb, is short, and more text follows it.
const NARRATION_START = /^(?:(?:now|next|first|then),? )?(?:wait[,.]|hmm|if i am forced|if i'm forced|i can still|i ran out|since i (?:can't|cannot) (?:run|query)|let me|let's|i'll|i will|i'm going to|i am going to|i need to|i'm now|i now have|i have everything|now i have|okay[,.]|ok[,.]|alright[,.]|good[,.—-]|great[,.—-]|got it|perfect[,.—-]|(?:re-?|double-)?(?:confirming|checking|verifying|looking|querying|pulling|fetching|searching|reading|running|gathering|digging|cross-checking)\b)/i;
// A first line that is the model restating a rule to itself ("Do not invent any numbers. Do not apologize.").
// Only whole lines of short imperative sentences, no figures, so a real answer ("Never weighed: 12 pens") survives.
const SELF_INSTRUCTION = /^(?:(?:do not|don't|never|always|remember|make sure|avoid|be sure to|keep it)\b[^.!\d]{0,120}[.!]\s*)+$/i;
// A leading run of sentences that restate the model's own rules ("Follow the exact form and rules from the system
// prompt: no narration, no file names, no SQL ... Provide a chart only if instructed.") is dropped up to where the real
// answer starts (often glued on: "...if there is data.The ADG ...").
const RULE_ECHO = /system prompt|the rules|no narration|no file names|no raw column|no sql|no code talk|only if instructed|do not answer questions that|do not invent|do not apologi[sz]e|never mention|my instructions/i;
export function stripEchoedRules(text) {
  let t = String(text || "");
  for (let guard = 0; guard < 12; guard++) {
    const m = t.match(/^\s*([^\n]*?[.!?])(?=\s|[A-Z]|$)/);
    if (!m || !RULE_ECHO.test(m[1])) break;
    t = t.slice(m[0].length);
  }
  return t.replace(/^\s+/, "");
}
export function stripLeadingNarration(text) {
  const t = stripEchoedRules(text);
  const m = t.match(/^\s*([^\n]*)\n+/);
  if (!m) return t;
  const first = m[1].trim();
  const rest = t.slice(m[0].length);
  if (!rest.trim() || first.length > 220 || first.startsWith("|") || first.startsWith("#") || first.includes("**")) return t;
  return NARRATION_START.test(first) || SELF_INSTRUCTION.test(first) ? stripLeadingNarration(rest) : t;
}

export function makeTurnGate(emit, hold = NARRATION_HOLD_CHARS) {
  let held = "";
  let released = false;
  // Releasing held text for the first time: drop a leading working line if it is complete.
  const release = (h) => emit(stripLeadingNarration(h));
  return {
    text(t) {
      if (released) return emit(t);
      held += t;
      if (held.length >= hold) { released = true; const h = held; held = ""; release(h); }
    },
    // A tool call started: drop held narration. Returns true if text was already on screen.
    toolStart() { const shown = released; held = ""; released = false; return shown; },
    // The turn ended without a tool call (or the run ended): release what is held.
    end() { if (held) { const h = held; held = ""; release(h); } released = false; },
  };
}

// ---- non-streaming /ceo-ai/ask (MCP ask_goatos) -------------------------------
// X-Mesha-Client: mcp tags events with source "mcp"; anything else is the admin-web panel.
export function askClient(headers) {
  return String(headers?.["x-mesha-client"] || "").trim().toLowerCase() === "mcp" ? "mcp" : null;
}

// Prompt note for stream:false callers: nobody watches a live table, so watch_tags is snapshot-only.
export const NON_STREAM_NOTE =
  "\n[Context note, not from the user: this question came through the Mesha MCP connector, which shows only your " +
  "final answer (no live panel). watch_tags can only take a one-time snapshot here (minutes=0); if the user asks to " +
  "watch over time, take the snapshot and say live watching is available in the Ask Mesha panel.]";

// Collects the SSE events ask() would stream and turns them into one JSON response.
// Tokens, progress labels and watch frames are dropped; only final/error matter.
export function jsonAskCollector() {
  let conversationId = null;
  let final = null;
  let error = null;
  return {
    send(obj) {
      if (!obj || typeof obj !== "object") return;
      if (obj.conversation_id) conversationId = obj.conversation_id;
      if (obj.type === "final") final = obj;
      else if (obj.type === "error" && !final) error = obj;
    },
    result() {
      if (final) {
        return {
          status: 200,
          body: {
            answer: final.answer ?? "", chart: final.chart ?? null,
            conversation_id: final.conversation_id ?? conversationId, message_id: final.message_id ?? null,
            timing: final.timing ?? null, request_id: final.request_id ?? null, source: final.source ?? "coding-agent", mode: "agent",
          },
        };
      }
      if (error) {
        const status = Number.isInteger(error.status) ? error.status : 502;
        return { status, body: { error: status === 410 ? "chat_deleted" : "agent_error", message: error.message || friendlyError(""), conversation_id: conversationId } };
      }
      return { status: 500, body: { error: "internal", message: friendlyError(""), conversation_id: conversationId } };
    },
  };
}

// ---- run_reference: vetted reference queries by NAME -------------------------
// The model used to retype ~5KB of .agents/skills/mesha-data-map/references/*.sql per
// question (50-150s of output tokens). run_reference loads the file by allow-listed
// name, fills documented params, wraps it as SELECT * FROM (<file>) q [WHERE] [ORDER BY]
// [LIMIT] and sends it through the same validateReadSql/runSql READ ONLY path.
// Param convention inside a reference file:
//   -- param: <name> <date|uuid|int|number|text>  <description>   (text: [A-Za-z0-9 _.-], <=64 chars)        (declaration, header comment)
//   /*param:<name>*/<default SQL expression>/*end*/               (inline; psql runs the default)
export const REFERENCE_DIR = ".agents/skills/mesha-data-map/references";
export const REFERENCE_FILES = [
  "pens.sql", "pen-weighing-latest.sql", "adg-by-park.sql", "cost-per-kg-gain.sql",
  "load-wise-sales.sql", "herd-avg-weight.sql", "feed-stock-days-left.sql", "adg-by-breed.sql",
];
export const REFERENCE_MAX_LIMIT = SQL_MAX_ROWS;

export function referencePath(root, name) {
  const n = String(name || "").trim();
  if (!REFERENCE_FILES.includes(n)) {
    return { ok: false, out: `Unknown reference "${n.slice(0, 60)}". Use one of: ${REFERENCE_FILES.join(", ")}.` };
  }
  return { ok: true, file: path.join(root, REFERENCE_DIR, n) };
}

export function referenceParams(text) {
  const out = {};
  for (const m of String(text).matchAll(/^--\s*param:\s*([a-z_][a-z0-9_]*)\s+(date|uuid|int|number|text)\b\s*(.*)$/gm)) {
    out[m[1]] = { type: m[2], doc: m[3].trim() };
  }
  return out;
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export function paramLiteral(type, value) {
  const v = typeof value === "number" ? String(value) : String(value ?? "").trim();
  if (type === "date") {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(v);
    const d = m && new Date(Date.UTC(+m[1], +m[2] - 1, +m[3]));
    if (!d || d.getUTCFullYear() !== +m[1] || d.getUTCMonth() !== +m[2] - 1 || d.getUTCDate() !== +m[3]) return null;
    return `'${v}'::date`;
  }
  if (type === "uuid") return UUID_RE.test(v) ? `'${v.toLowerCase()}'::uuid` : null;
  if (type === "int") return /^-?\d{1,9}$/.test(v) ? String(Number(v)) : null;
  if (type === "number") return /^-?\d{1,12}(\.\d{1,6})?$/.test(v) ? v : null;
  if (type === "text") return /^[A-Za-z0-9 _.-]{0,64}$/.test(v) ? `'${v}'` : null;
  return null;
}

// Removes -- and /* */ comments outside single-quoted strings (reference files carry
// long comment headers, some with ';', which validateReadSql would refuse).
export function stripSqlComments(sql) {
  let out = "";
  for (let i = 0; i < sql.length;) {
    const c = sql[i];
    if (c === "'") {
      let j = i + 1;
      while (j < sql.length && !(sql[j] === "'" && sql[j + 1] !== "'")) j += sql[j] === "'" ? 2 : 1;
      out += sql.slice(i, j + 1); i = j + 1;
    } else if (c === "-" && sql[i + 1] === "-") {
      while (i < sql.length && sql[i] !== "\n") i++;
    } else if (c === "/" && sql[i + 1] === "*") {
      const e = sql.indexOf("*/", i + 2); i = e < 0 ? sql.length : e + 2; out += " ";
    } else { out += c; i++; }
  }
  return out;
}

// Free-text pieces (where / order_by) may not end the statement or comment out the tail.
function clauseOk(s) { return !/;|--|\/\*|\*\/|\\/.test(s); }

export function buildReferenceSql(text, { name = "reference", params, where, order_by, limit } = {}) {
  const declared = referenceParams(text);
  const given = params && typeof params === "object" ? params : {};
  for (const k of Object.keys(given)) {
    if (!declared[k]) {
      const names = Object.keys(declared);
      return { ok: false, out: `${name} has no param "${k}". ${names.length ? `Params: ${names.map((n) => `${n} (${declared[n].type})`).join(", ")}.` : "It takes no params; use where instead."}` };
    }
  }
  let bad = null;
  const filled = String(text).replace(/\/\*param:([a-z_][a-z0-9_]*)\*\/([\s\S]*?)\/\*end\*\//g, (all, k, dflt) => {
    if (!declared[k]) { bad ??= `${name}: inline param "${k}" is not declared with "-- param:".`; return all; }
    if (given[k] === undefined || given[k] === null || given[k] === "") return dflt;
    const lit = paramLiteral(declared[k].type, given[k]);
    if (lit === null) { bad ??= `Param ${k} must be a ${declared[k].type}${declared[k].type === "date" ? " as YYYY-MM-DD" : ""}; got ${JSON.stringify(given[k]).slice(0, 40)}.`; return all; }
    return lit;
  });
  if (bad) return { ok: false, out: bad };
  const inner = stripSqlComments(filled).trim().replace(/;\s*$/, "").trim();
  if (!/^(with|select)\b/i.test(inner)) return { ok: false, out: `${name} is not a single SELECT/WITH query.` };
  let sql = `SELECT * FROM (\n${inner}\n) q`;
  for (const [key, val, kw] of [["where", where, "WHERE"], ["order_by", order_by, "ORDER BY"]]) {
    const s = String(val ?? "").trim();
    if (!s) continue;
    if (!clauseOk(s)) return { ok: false, out: `Refused: ${key} may not contain ';', comments or backslashes.` };
    sql += kw === "WHERE" ? `\nWHERE (${s})` : `\nORDER BY ${s}`;
  }
  if (limit !== undefined && limit !== null && limit !== "") {
    const n = Number(limit);
    if (!Number.isInteger(n) || n < 1 || n > REFERENCE_MAX_LIMIT) return { ok: false, out: `limit must be an integer 1-${REFERENCE_MAX_LIMIT}.` };
    sql += `\nLIMIT ${n}`;
  }
  return validateReadSql(sql);
}
