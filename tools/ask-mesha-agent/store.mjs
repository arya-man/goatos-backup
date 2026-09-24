// Chat/metrics storage for the Ask Mesha agent.
//   ASK_MESHA_DATABASE_URL set -> Postgres (schema ask_mesha), stateless across instances.
//   unset                     -> JSON file under the state dir (local dev, original behaviour).
// Both backends expose the same async API; chats are plain objects
// { id, email, title, session_id, worktree, created_at, updated_at, deleted_at }.
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import os from "node:os";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
// The /ask heartbeat refreshes the lease every 10s, so a live run never loses it. Short on
// purpose: an instance killed mid-answer (deploy, crash) must not leave the chat "still
// answering" for long; the CEO can ask again in the same chat about a minute later.
export const LOCK_MS = 60_000;

function pct(xs, p) {
  if (!xs.length) return null;
  const s = [...xs].sort((a, b) => a - b);
  return s[Math.min(s.length - 1, Math.floor((p / 100) * s.length))];
}
export function summarizeMetrics(rows) {
  const ok = rows.filter((r) => r.ok);
  const stat = (k) => {
    const xs = ok.map((r) => r[k]).filter((v) => typeof v === "number");
    return { p50: pct(xs, 50), p90: pct(xs, 90), max: xs.length ? Math.max(...xs) : null };
  };
  return {
    count: rows.length,
    ok: ok.length,
    total_ms: stat("total_ms"),
    first_token_ms: stat("first_token_ms"),
    first_tool_ms: stat("first_tool_ms"),
    db_queries: stat("db_queries"),
    tool_calls: stat("tool_calls"),
    recent: rows.slice(-25).reverse(),
  };
}

// ---- JSON file backend ------------------------------------------------------
// A corrupt/truncated chats.json (disk full, hand edit, pre-atomic-write crash) must not
// crash-loop the server: fall back to a complete leftover .tmp, else start empty, and keep
// the bad file aside (never overwritten) for recovery.
export function loadJsonDb(file, log = console.error) {
  const parse = (f) => {
    const v = JSON.parse(fs.readFileSync(f, "utf8"));
    if (!v || typeof v !== "object" || !v.chats || typeof v.chats !== "object") throw new Error("no chats map");
    return v;
  };
  if (!fs.existsSync(file)) return { chats: {} };
  try { return parse(file); } catch (e) {
    const aside = `${file}.corrupt-${Date.now()}`;
    try { fs.renameSync(file, aside); } catch {}
    log(`[store] ${path.basename(file)} unreadable (${e.message}); moved to ${path.basename(aside)}`);
    try {
      const tmp = parse(file + ".tmp");
      log("[store] recovered chats from the last complete .tmp write");
      return tmp;
    } catch { return { chats: {} }; }
  }
}
// metrics.jsonl lines, skipping a partial last line from a crash mid-append.
export function readJsonl(file) {
  if (!fs.existsSync(file)) return [];
  const out = [];
  for (const l of fs.readFileSync(file, "utf8").split("\n")) {
    if (!l.trim()) continue;
    try { out.push(JSON.parse(l)); } catch { /* torn line */ }
  }
  return out;
}
function jsonStore(stateDir) {
  const STORE = path.join(stateDir, "chats.json");
  const METRICS = path.join(stateDir, "metrics.jsonl");
  fs.mkdirSync(stateDir, { recursive: true });
  const db = loadJsonDb(STORE);
  // Atomic replace so a crash mid-write can't truncate every CEO's history.
  const save = () => {
    fs.writeFileSync(STORE + ".tmp", JSON.stringify(db));
    fs.renameSync(STORE + ".tmp", STORE);
  };
  const inFlight = new Set();
  const pub = (c) => c && (({ messages, ...rest }) => rest)(c);
  return {
    kind: "json",
    sessionStore: undefined, // SDK transcripts stay on local disk (~/.claude/projects)
    async createChat(email, tenantId = "") {
      const id = crypto.randomUUID();
      const now = new Date().toISOString();
      db.chats[id] = { id, email, tenant_id: tenantId, title: "New chat", session_id: null, worktree: null, created_at: now, updated_at: now, messages: [] };
      save();
      return pub(db.chats[id]);
    },
    async getChat(id) { return pub(db.chats[id]) || null; },
    async listChats(email, tenantId = "") {
      return Object.values(db.chats)
        .filter((c) => c.email === email && (c.tenant_id ?? "") === tenantId && c.messages.length && !c.deleted_at)
        .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
        .map(pub);
    },
    async getMessages(chatId) { return db.chats[chatId]?.messages || []; },
    async addMessage(chatId, msg) { db.chats[chatId].messages.push(msg); save(); },
    async updateChat(chatId, fields) { Object.assign(db.chats[chatId], fields); save(); },
    async findFile(chatId, fileId) {
      return (db.chats[chatId]?.messages || []).flatMap((x) => x.files || []).find((x) => x.id === fileId) || null;
    },
    async tryLock(chatId) { if (inFlight.has(chatId)) return false; inFlight.add(chatId); return true; },
    async refreshLock() {},
    async unlock(chatId) { inFlight.delete(chatId); },
    async recordMetric(m) { fs.appendFileSync(METRICS, JSON.stringify(m) + "\n"); },
    // Sum of per-answer cost since the start of the current UTC month.
    async monthSpendUsd(since) {
      if (!fs.existsSync(METRICS)) return 0;
      return fs.readFileSync(METRICS, "utf8").split("\n").filter(Boolean).reduce((sum, l) => {
        try {
          const r = JSON.parse(l);
          return r.ts >= since ? sum + (Number(r.cost_usd) || 0) : sum;
        } catch { return sum; }
      }, 0);
    },
    async metricsSummary() {
      const rows = readJsonl(METRICS);
      return summarizeMetrics(rows);
    },
    // The SDK keeps local transcripts in <config>/projects/<cwd-key>/<id>.jsonl. If it is gone
    // (wiped ~/.claude, other machine), resuming would fail on every later ask in that chat.
    async hasSession(sessionId) {
      const dir = path.join(process.env.CLAUDE_CONFIG_DIR || path.join(os.homedir(), ".claude"), "projects");
      try {
        return fs.readdirSync(dir).some((d) => fs.existsSync(path.join(dir, d, `${sessionId}.jsonl`)));
      } catch { return false; }
    },
    async close() {},
  };
}

// ---- Postgres backend -------------------------------------------------------
const iso = (v) => (v instanceof Date ? v.toISOString() : v ?? null);
const chatRow = (r) => r && ({
  id: r.id, email: r.email, tenant_id: r.tenant_id ?? "", title: r.title, session_id: r.session_id, worktree: r.worktree,
  created_at: iso(r.created_at), updated_at: iso(r.updated_at), deleted_at: iso(r.deleted_at),
  session_cost_usd: r.session_cost_usd == null ? null : Number(r.session_cost_usd),
});
const msgRow = (r) => {
  const m = { id: r.id, role: r.role, content: r.content, created_at: iso(r.created_at) };
  if (r.chart != null) m.chart = r.chart;
  if (r.files != null) m.files = r.files;
  if (r.source != null) m.source = r.source;
  if (r.mode != null) m.mode = r.mode;
  if (r.request_id != null) m.request_id = r.request_id;
  return m;
};

export function pgStore(pool) {
  const q = (text, params) => pool.query(text, params);

  // Claude Agent SDK SessionStore adapter (Options.sessionStore): mirrors each
  // session transcript into ask_mesha.session_entries so resume works on any instance.
  const sessionStore = {
    async append(key, entries) {
      if (!entries.length) return;
      const sub = key.subpath || "";
      const vals = [];
      const params = [];
      for (const e of entries) {
        params.push(key.projectKey, key.sessionId, sub, typeof e.uuid === "string" ? e.uuid : null, JSON.stringify(e));
        const b = params.length - 5;
        vals.push(`($${b + 1},$${b + 2},$${b + 3},$${b + 4},$${b + 5}::jsonb)`);
      }
      // uuid is the idempotency key (retries / importSessionToStore replays).
      await q(
        `INSERT INTO ask_mesha.session_entries (project_key, session_id, subpath, uuid, entry)
         VALUES ${vals.join(",")} ON CONFLICT (project_key, session_id, subpath, uuid) WHERE uuid IS NOT NULL DO NOTHING`,
        params,
      );
    },
    async load(key) {
      const { rows } = await q(
        `SELECT entry FROM ask_mesha.session_entries WHERE project_key=$1 AND session_id=$2 AND subpath=$3 ORDER BY seq`,
        [key.projectKey, key.sessionId, key.subpath || ""],
      );
      return rows.length ? rows.map((r) => r.entry) : null;
    },
    async listSessions(projectKey) {
      const { rows } = await q(
        `SELECT session_id, max(created_at) AS mtime FROM ask_mesha.session_entries WHERE project_key=$1 GROUP BY session_id`,
        [projectKey],
      );
      return rows.map((r) => ({ sessionId: r.session_id, mtime: Math.floor(new Date(r.mtime).getTime()) }));
    },
    async listSubkeys(key) {
      const { rows } = await q(
        `SELECT DISTINCT subpath FROM ask_mesha.session_entries WHERE project_key=$1 AND session_id=$2 AND subpath <> ''`,
        [key.projectKey, key.sessionId],
      );
      return rows.map((r) => r.subpath);
    },
    async delete(key) {
      if (key.subpath) {
        await q(`DELETE FROM ask_mesha.session_entries WHERE project_key=$1 AND session_id=$2 AND subpath=$3`, [key.projectKey, key.sessionId, key.subpath]);
      } else {
        await q(`DELETE FROM ask_mesha.session_entries WHERE project_key=$1 AND session_id=$2`, [key.projectKey, key.sessionId]);
      }
    },
  };

  return {
    kind: "postgres",
    sessionStore,
    async migrate() {
      const sql = fs.readFileSync(path.join(HERE, "sql/001_init.sql"), "utf8");
      await q(sql);
    },
    async createChat(email, tenantId = "") {
      const { rows } = await q(
        `INSERT INTO ask_mesha.chats (id, email, tenant_id) VALUES ($1, $2, $3) RETURNING *`,
        [crypto.randomUUID(), email, tenantId],
      );
      return chatRow(rows[0]);
    },
    async getChat(id) {
      if (!/^[0-9a-f-]{36}$/i.test(String(id))) return null; // non-uuid ids never match (and would throw)
      const { rows } = await q(`SELECT * FROM ask_mesha.chats WHERE id=$1`, [id]);
      return chatRow(rows[0]) || null;
    },
    async listChats(email, tenantId = "") {
      const { rows } = await q(
        `SELECT c.* FROM ask_mesha.chats c
         WHERE c.email=$1 AND c.tenant_id=$2 AND c.deleted_at IS NULL
           AND EXISTS (SELECT 1 FROM ask_mesha.messages m WHERE m.chat_id=c.id)
         ORDER BY c.updated_at DESC`,
        [email, tenantId],
      );
      return rows.map(chatRow);
    },
    async getMessages(chatId) {
      const { rows } = await q(`SELECT * FROM ask_mesha.messages WHERE chat_id=$1 ORDER BY created_at, id`, [chatId]);
      return rows.map(msgRow);
    },
    async addMessage(chatId, m) {
      await q(
        `INSERT INTO ask_mesha.messages (id, chat_id, role, content, chart, files, source, mode, request_id, created_at)
         VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9,$10)`,
        [m.id, chatId, m.role, m.content ?? "", m.chart === undefined ? null : JSON.stringify(m.chart),
          m.files === undefined ? null : JSON.stringify(m.files), m.source ?? null, m.mode ?? null,
          m.request_id ?? null, m.created_at],
      );
    },
    async updateChat(chatId, fields) {
      const allowed = ["title", "session_id", "session_cost_usd", "worktree", "updated_at", "deleted_at"];
      const keys = Object.keys(fields).filter((k) => allowed.includes(k));
      if (!keys.length) return;
      await q(
        `UPDATE ask_mesha.chats SET ${keys.map((k, i) => `${k}=$${i + 2}`).join(", ")} WHERE id=$1`,
        [chatId, ...keys.map((k) => fields[k])],
      );
    },
    async findFile(chatId, fileId) {
      const { rows } = await q(
        `SELECT f FROM ask_mesha.messages m, jsonb_array_elements(m.files) f
         WHERE m.chat_id=$1 AND m.files IS NOT NULL AND f->>'id'=$2 LIMIT 1`,
        [chatId, fileId],
      );
      return rows[0]?.f || null;
    },
    // Row-level lease instead of an in-process Set: works across Cloud Run instances
    // and self-heals if an instance dies mid-run.
    async tryLock(chatId) {
      const { rowCount } = await q(
        `UPDATE ask_mesha.chats SET busy_until = now() + ($2 || ' milliseconds')::interval
         WHERE id=$1 AND (busy_until IS NULL OR busy_until < now())`,
        [chatId, String(LOCK_MS)],
      );
      return rowCount === 1;
    },
    async refreshLock(chatId) {
      await q(`UPDATE ask_mesha.chats SET busy_until = now() + ($2 || ' milliseconds')::interval WHERE id=$1`, [chatId, String(LOCK_MS)]);
    },
    async unlock(chatId) { await q(`UPDATE ask_mesha.chats SET busy_until=NULL WHERE id=$1`, [chatId]); },
    async recordMetric(m) { await q(`INSERT INTO ask_mesha.metrics (ts, row) VALUES ($1, $2::jsonb)`, [m.ts, JSON.stringify(m)]); },
    async monthSpendUsd(since) {
      const { rows } = await q(
        `SELECT COALESCE(SUM((row->>'cost_usd')::numeric), 0) AS usd FROM ask_mesha.metrics WHERE ts >= $1`,
        [since],
      );
      return Number(rows[0].usd) || 0;
    },
    async metricsSummary() {
      const { rows } = await q(`SELECT row FROM (SELECT id, row FROM ask_mesha.metrics ORDER BY id DESC LIMIT 5000) t ORDER BY id`);
      return summarizeMetrics(rows.map((r) => r.row));
    },
    // projectKey is derived by the SDK from cwd; session ids are globally unique uuids.
    async hasSession(sessionId) {
      const { rows } = await q(`SELECT 1 FROM ask_mesha.session_entries WHERE session_id=$1 LIMIT 1`, [sessionId]);
      return rows.length > 0;
    },
    async close() { await pool.end(); },
  };
}

export async function createStore({ stateDir }) {
  const url = process.env.ASK_MESHA_DATABASE_URL;
  if (!url) return jsonStore(stateDir);
  const { default: pg } = await import("pg");
  const pool = new pg.Pool({ connectionString: url, max: Number(process.env.ASK_MESHA_DB_POOL || 5) });
  const store = pgStore(pool);
  if (process.env.ASK_MESHA_DB_MIGRATE === "1") await store.migrate();
  return store;
}
