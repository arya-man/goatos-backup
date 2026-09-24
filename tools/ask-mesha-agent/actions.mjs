// Ask Mesha CEO write actions: propose -> preview -> human confirm -> backend executes.
//
// The model NEVER executes anything. It can only call `propose_action`, which validates the
// params against the action's schema, resolves names to ids READ-ONLY, and stores a pending
// action (the exact HTTP request that would be sent) for 10 minutes. The CEO then presses
// Confirm in the panel: POST /ceo-ai/actions/{id}/confirm executes it against the GoatOS
// backend with the CEO's OWN Firebase bearer from that confirm request (never stored), so
// the backend stays the authority for permissions, validation and audit.
//
// Modes (ASK_MESHA_ACTIONS): off (default; propose_action is not offered), dry (proposals
// work, confirm returns the exact request instead of sending it), on (confirm executes).
// ASK_MESHA_ACTIONS_ALLOW=a,b narrows the action set (default: all in action-specs.mjs).
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const PROPOSAL_TTL_MS = 10 * 60_000;
export const CONFIRM_TEXT = "CONFIRM";
export const CLIENT_HEADER = "ask-mesha-action";
const EXEC_TIMEOUT_MS = 20_000;

export function actionsMode(env = process.env) {
  const v = String(env.ASK_MESHA_ACTIONS || "off").trim().toLowerCase();
  return v === "on" || v === "dry" ? v : "off";
}
export function allowedActions(specs, env = process.env) {
  const raw = String(env.ASK_MESHA_ACTIONS_ALLOW || "").trim();
  const names = Object.keys(specs);
  if (!raw) return names;
  const want = new Set(raw.split(",").map((s) => s.trim()).filter(Boolean));
  return names.filter((n) => want.has(n));
}
// Actions are only offered on the admin-web panel. The hosted MCP connector (X-Mesha-Client: mcp)
// and any non-streaming client stay read-only: they have no confirm UI.
export function actionsEnabledFor({ mode, client, streaming }) {
  return mode !== "off" && client !== "mcp" && streaming !== false;
}

// ---- pending store ------------------------------------------------------------
// Never holds the bearer token: only the owner, the resolved request and the preview.
export function memoryPendingStore() {
  const rows = new Map();
  return {
    kind: "memory",
    async put(p) { rows.set(p.id, { ...p }); },
    async get(id) { const r = rows.get(id); return r ? { ...r } : null; },
    // Atomic claim: pending (or transport_failed, same idempotency key) -> executing.
    async claim(id, now) {
      const r = rows.get(id);
      if (!r || !["pending", "transport_failed"].includes(r.status) || new Date(r.expires_at) <= now) return null;
      r.status = "executing";
      r.attempts = (r.attempts || 0) + 1;
      return { ...r };
    },
    async finish(id, fields) { const r = rows.get(id); if (r) Object.assign(r, fields); },
    async cancel(id) {
      const r = rows.get(id);
      if (!r || !["pending", "transport_failed"].includes(r.status)) return false;
      r.status = "cancelled";
      return true;
    },
    _rows: rows,
  };
}

export function pgPendingStore(pool) {
  const q = (t, p) => pool.query(t, p);
  const row = (r) => r && ({
    id: r.id, email: r.email, tenant_id: r.tenant_id, chat_id: r.chat_id, action: r.action, request: r.request,
    title: r.title, summary: r.summary, risk: r.risk, requires_double_confirm: r.requires_double_confirm,
    status: r.status, attempts: r.attempts, result: r.result,
    created_at: new Date(r.created_at).toISOString(), expires_at: new Date(r.expires_at).toISOString(),
  });
  return {
    kind: "postgres",
    async migrate() { await q(fs.readFileSync(path.join(HERE, "sql/003_actions.sql"), "utf8")); },
    async put(p) {
      await q(
        `INSERT INTO ask_mesha.pending_actions (id, email, tenant_id, chat_id, action, request, title, summary, risk, requires_double_confirm, status, created_at, expires_at)
         VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8::jsonb,$9,$10,$11,$12,$13)`,
        [p.id, p.email, p.tenant_id, p.chat_id, p.action, JSON.stringify(p.request), p.title, JSON.stringify(p.summary), p.risk, p.requires_double_confirm, p.status, p.created_at, p.expires_at],
      );
    },
    async get(id) {
      if (!/^[0-9a-f-]{36}$/i.test(String(id))) return null;
      const { rows } = await q(`SELECT * FROM ask_mesha.pending_actions WHERE id=$1`, [id]);
      return row(rows[0]) || null;
    },
    async claim(id, now) {
      const { rows } = await q(
        `UPDATE ask_mesha.pending_actions SET status='executing', attempts=attempts+1
         WHERE id=$1 AND status IN ('pending','transport_failed') AND expires_at > $2 RETURNING *`,
        [id, now.toISOString()],
      );
      return row(rows[0]) || null;
    },
    async finish(id, fields) {
      await q(`UPDATE ask_mesha.pending_actions SET status=$2, result=$3::jsonb WHERE id=$1`, [id, fields.status, JSON.stringify(fields.result ?? null)]);
    },
    async cancel(id) {
      const { rowCount } = await q(`UPDATE ask_mesha.pending_actions SET status='cancelled' WHERE id=$1 AND status IN ('pending','transport_failed')`, [id]);
      return rowCount === 1;
    },
  };
}

export async function createPendingStore() {
  const url = process.env.ASK_MESHA_DATABASE_URL;
  if (!url) return memoryPendingStore();
  const { default: pg } = await import("pg");
  const s = pgPendingStore(new pg.Pool({ connectionString: url, max: 2 }));
  if (process.env.ASK_MESHA_DB_MIGRATE === "1") await s.migrate();
  return s;
}

// ---- helpers ------------------------------------------------------------------
const sameOwner = (p, user) => p.email === user.email && (p.tenant_id ?? "") === (user.tenantId ?? "");
function zodMessage(err) {
  const issues = err?.issues || [];
  return issues.map((i) => `${(i.path || []).join(".") || "params"}: ${i.message}`).join("; ") || String(err?.message || err);
}
// The preview/result shown in chat never echoes the raw backend body wholesale.
function resultNote(status, body) {
  const msg = body && typeof body === "object" ? body.message || body.error || body.code : typeof body === "string" ? body : "";
  return String(msg || "").slice(0, 300);
}

// ---- service --------------------------------------------------------------------
// deps: specs (action-specs.mjs), pending (store), resolver (read-only lookups), backendBase,
// fetchImpl, emit(event, ctx, fields), addMessage(chatId, msg), now(), env.
export function createActionService(deps) {
  const { specs, pending, resolver, emit = async () => {}, addMessage = async () => {} } = deps;
  const fetchImpl = deps.fetchImpl || fetch;
  const now = deps.now || (() => new Date());
  const env = deps.env || process.env;
  const base = String(deps.backendBase || "").replace(/\/$/, "");

  function catalog() {
    return allowedActions(specs, env).map((n) => `${n}: ${specs[n].describe}`).join("\n");
  }

  // Called from the model's propose_action tool. Returns { ok, text, proposal? }.
  async function propose({ action, params }, ctx) {
    const mode = actionsMode(env);
    if (mode === "off") return { ok: false, text: "Changes from chat are switched off. Tell the CEO to make this change in the app." };
    const spec = specs[action];
    if (!spec || !allowedActions(specs, env).includes(action)) {
      return { ok: false, text: `"${String(action).slice(0, 60)}" is not an action Ask Mesha can prepare. Available:\n${catalog()}` };
    }
    const parsed = spec.schema.safeParse(params ?? {});
    if (!parsed.success) return { ok: false, text: `Invalid params for ${action}: ${zodMessage(parsed.error)}` };
    const id = crypto.randomUUID(); // also the backend Idempotency-Key
    let built;
    try {
      built = await spec.prepare(parsed.data, ctx.resolver || resolver, { proposalId: id, email: ctx.email });
    } catch (e) {
      return { ok: false, text: String(e?.message || e) };
    }
    const t = now();
    const risk = spec.risk === "high" || built.risk === "high" ? "high" : "normal";
    const p = {
      id, email: ctx.email, tenant_id: ctx.tenantId ?? "", chat_id: ctx.chatId, action,
      request: built.request, title: built.title, summary: built.summary, risk,
      requires_double_confirm: risk === "high", status: "pending", attempts: 0,
      created_at: t.toISOString(), expires_at: new Date(t.getTime() + PROPOSAL_TTL_MS).toISOString(),
    };
    await pending.put(p);
    const event = {
      type: "action_proposal", proposal_id: p.id, title: p.title, summary: p.summary, risk: p.risk,
      requires_double_confirm: p.requires_double_confirm, expires_at: p.expires_at, dry_run: mode === "dry",
    };
    ctx.send?.(event);
    emit("action_proposed", { email: ctx.email, tenant_id: ctx.tenantId, chat_id: ctx.chatId }, { action, proposal_id: p.id, risk });
    return {
      ok: true,
      proposal: p,
      event,
      text: `Proposal ready (not executed). The CEO sees a card with Confirm / Cancel${p.requires_double_confirm ? " and must type CONFIRM" : ""}; it expires in 10 minutes. ` +
        `Tell them in one short sentence what will change and to press Confirm. Do not say it is done.\n${p.title}\n- ${p.summary.join("\n- ")}`,
    };
  }

  // user: {email, tenantId}; authz: the confirm request's Authorization header (used once, never stored).
  async function confirm(id, user, authz, body = {}) {
    const mode = actionsMode(env);
    const evCtx = { email: user.email, tenant_id: user.tenantId };
    if (mode === "off") return { status: 403, body: { error: "actions_disabled", message: "Changes from chat are switched off." } };
    const p = await pending.get(id);
    if (!p || !sameOwner(p, user)) return { status: 404, body: { error: "not_found" } };
    evCtx.chat_id = p.chat_id;
    if (!allowedActions(specs, env).includes(p.action)) return { status: 403, body: { error: "action_not_allowed" } };
    if (new Date(p.expires_at) <= now()) return { status: 410, body: { error: "expired", message: "This proposal expired. Ask again to prepare it fresh." } };
    if (!["pending", "transport_failed"].includes(p.status)) return { status: 409, body: { error: "already_used", status: p.status } };
    if (p.requires_double_confirm && body?.confirm_text !== CONFIRM_TEXT) {
      return { status: 428, body: { error: "confirm_text_required", message: `Type ${CONFIRM_TEXT} to run this change.` } };
    }
    if (!/^Bearer\s+\S+/i.test(String(authz || ""))) return { status: 401, body: { error: "bearer_required" } };
    if (mode === "dry") {
      // Nothing is sent; the proposal stays usable so it can be confirmed after the switch flips.
      return { status: 200, body: { ok: true, dry_run: true, proposal_id: p.id, request: { ...p.request, headers: requestHeaders(p, "<redacted>", user) } } };
    }
    const claimed = await pending.claim(id, now());
    if (!claimed) return { status: 409, body: { error: "already_used" } };
    const spec = specs[p.action];
    // Server-side guard hooks (e.g. shifting-only) run with the CEO's bearer right before sending.
    if (spec.guard) {
      const g = await spec.guard(p, (m, pth) => backendFetch(m, pth, undefined, authz, user, p.id)).catch((e) => ({ ok: false, reason: String(e?.message || e) }));
      if (!g.ok) {
        await pending.finish(id, { status: "refused", result: { reason: g.reason } });
        emit("action_failed", evCtx, { action: p.action, proposal_id: p.id, error_class: "guard", error: g.reason, severity: "WARNING" });
        await postResult(p, `Not done: ${g.reason}`);
        return { status: 422, body: { error: "refused", message: g.reason } };
      }
    }
    let res;
    try {
      res = await backendFetch(p.request.method, p.request.path, p.request.body, authz, user, p.id);
    } catch (e) {
      // No response: the backend may or may not have applied it. Re-confirm reuses the same
      // Idempotency-Key, so the backend deduplicates.
      await pending.finish(id, { status: "transport_failed", result: { error: String(e?.message || e) } });
      emit("action_failed", evCtx, { action: p.action, proposal_id: p.id, error_class: "transport", error: String(e?.message || e).slice(0, 200), severity: "WARNING" });
      return { status: 502, body: { error: "backend_unreachable", message: "The app did not answer. Press Confirm again to retry safely (it will not apply twice)." } };
    }
    const ok = res.status >= 200 && res.status < 300;
    await pending.finish(id, { status: ok ? "executed" : "failed", result: { status: res.status } });
    emit(ok ? "action_executed" : "action_failed", evCtx, {
      action: p.action, proposal_id: p.id, backend_status: res.status, attempts: claimed.attempts,
      ...(ok ? {} : { error_class: "backend", error: resultNote(res.status, res.body), severity: "WARNING" }),
    });
    await postResult(p, ok ? `Done: ${p.title}` : `Not done: ${p.title}. The app said: ${resultNote(res.status, res.body) || `status ${res.status}`}`);
    return { status: ok ? 200 : res.status >= 400 && res.status < 600 ? res.status : 502, body: { ok, proposal_id: p.id, backend_status: res.status, backend: res.body } };
  }

  async function cancel(id, user) {
    const p = await pending.get(id);
    if (!p || !sameOwner(p, user)) return { status: 404, body: { error: "not_found" } };
    const done = await pending.cancel(id);
    if (!done) return { status: 409, body: { error: "already_used", status: p.status } };
    emit("action_cancelled", { email: user.email, tenant_id: user.tenantId, chat_id: p.chat_id }, { action: p.action, proposal_id: p.id });
    await postResult(p, `Cancelled: ${p.title}`);
    return { status: 200, body: { ok: true, cancelled: true } };
  }

  function requestHeaders(p, bearer, user) {
    return {
      Authorization: bearer, "Content-Type": "application/json", Accept: "application/json",
      ...(user.tenantId ? { "X-GoatOS-Tenant-ID": user.tenantId } : {}),
      "Idempotency-Key": p.id, "X-Mesha-Client": CLIENT_HEADER,
    };
  }
  async function backendFetch(method, pth, body, authz, user, idem) {
    const r = await fetchImpl(`${base}${pth}`, {
      method,
      headers: requestHeaders({ id: idem }, authz, user),
      ...(body === undefined || method === "GET" ? {} : { body: JSON.stringify(body) }),
      signal: AbortSignal.timeout(EXEC_TIMEOUT_MS),
    });
    const text = await r.text();
    let parsed = text;
    try { parsed = text ? JSON.parse(text) : null; } catch {}
    return { status: r.status, body: parsed };
  }
  async function postResult(p, content) {
    await addMessage(p.chat_id, {
      id: crypto.randomUUID(), role: "assistant", content, source: "ask-mesha-action", mode: "action",
      request_id: p.id, created_at: now().toISOString(),
    }).catch(() => {});
  }

  return { propose, confirm, cancel, catalog };
}
