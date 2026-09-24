// CEO write actions: propose -> confirm against a FAKE backend (local http server). Never STG.
import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  actionsMode, allowedActions, actionsEnabledFor, createActionService, memoryPendingStore, PROPOSAL_TTL_MS, CLIENT_HEADER,
} from "../actions.mjs";
import { ACTION_SPECS, shiftingGuard } from "../action-specs.mjs";
import { createSqlResolver, pensCtes, lit, parseJsonRows } from "../action-resolver.mjs";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const REPO = path.resolve(HERE, "../../..");
const U = (n) => `00000000-0000-4000-8000-${String(n).padStart(12, "0")}`;
const PARK = U(1), SHED = U(2), SHED2 = U(3), RAVI = U(10), RAVI_WM = U(11), ASHA = U(12), ASHA_WM = U(13), TENANT = U(99);
const FUTURE = new Date(Date.now() + 5 * 864e5).toISOString().slice(0, 10);
const CEO = { email: "ceo@mesha.sg", tenantId: TENANT };
const BEARER = "Bearer eyJhbGciOi.fake-ceo-token.sig";

// ---- fake resolver (name resolution fakes) ----
function fakeResolver(over = {}) {
  const people = { ravi: { user_id: RAVI, workforce_member_id: RAVI_WM, name: "Ravi" }, asha: { user_id: ASHA, workforce_member_id: ASHA_WM, name: "Asha" } };
  const pens = { G1P3: { shed_id: SHED, partition_label: "3", pen_name: "Godel 1 Part 3", pen_code: "G1P3" }, C1: { shed_id: SHED2, partition_label: "", pen_name: "Castro 1", pen_code: "C1" } };
  return {
    async park(n) { if (!/^(cbe|coimbatore)$/i.test(n)) throw new Error(`No park called "${n}".`); return { park_id: PARK, name: "Coimbatore" }; },
    async pens(_p, names) { return names.map((n) => { const x = pens[n.toUpperCase()]; if (!x) throw new Error(`No active pen "${n}" in that park.`); return x; }); },
    async person(n) { if (/^ra$/i.test(n)) throw new Error(`"r" matches several people: Ravi, Rahul. Which one?`); const x = people[n.toLowerCase()]; if (!x) throw new Error(`No active person called "${n}".`); return x; },
    async leadershipAssignee(n) { return this.person(n); },
    async pcTask(id) { return { task_id: id, category: "deworming", status: "open", planned_business_date: FUTURE, pen_name: "Godel 1 Part 3" }; },
    async pcRound(id) { return { round_id: id, category: "deworming", planned_business_date: FUTURE, park_name: "Coimbatore", pen_count: 3, open_count: 2 }; },
    async campaign(id) { return { campaign_id: id, park_id: PARK, park_name: "Coimbatore", start_business_date: FUTURE, status: "draft", planned_cap_per_day: 150, operator_user_id: RAVI, fasting_operator_user_id: ASHA,
      sheds: [{ campaign_shed_id: U(50), location_id: SHED, location_type: "shed", display_name: "Godel 1 Part 3", partition_label: "3", weighing_category: "individual_animal", status: "pending" }] }; },
    async campaignShed(c, p) { const s = c.sheds.find((x) => x.display_name.toLowerCase() === p.toLowerCase() || p.toUpperCase() === "G1P3"); if (!s) throw new Error("not in plan"); return s; },
    async obligation(id) { return { obligation_id: id, status: "scheduled", due_at: "2026-09-25T00:00:00Z" }; },
    async vaccCapacity() { return { max_per_day: 100, capacity_scope: "tenant", max_buffer_days: 7, overflow_policy: "split_within_safe_window_last_safe_may_exceed_cap", row_version: 4, max_shots_per_animal_per_drive: null }; },
    async vaccOperatorConfig() { return { active_operators_per_day: 1, default_operator_id: RAVI_WM, selected_operator_ids: [RAVI_WM], row_version: 2, default_operator_name: "Ravi" }; },
    async leadershipTask() { return { task_id: U(60), task_no: 12, title: "Fix water line", body: "shed 4", status: "open", row_version: 3, attachments: [{ proof_id: U(61), kind: "photo", file_name: "a.jpg" }] }; },
    async sopTask(id) { return { task_id: id, title: "Weigh shed 4", state: "submitted", row_version: 5 }; },
    async sop(ref) { return { sop_id: U(70), code: "feed.daily_check", name: "Daily feed check", module_key: "feed", ref }; },
    async sopExists(code) { return code === "feed.daily_check"; },
    async sopVersion(_id, which) { return which === "draft" ? { sop_version_id: U(71), version: 3, version_label: "v3", status: "draft", row_version: 1 } : { sop_version_id: U(72), version: 2, version_label: "v2", status: "published", row_version: 2, form_dsl: { steps: [1] }, proof_policy: { p: 1 }, compatibility: {} }; },
    async approval(id) { return over.approval ? over.approval(id) : { approval_request_id: id, request_type: "shifting", status: "pending", summary: "12 goats Castro 1 -> Godel 1 Part 3" }; },
    ...over,
  };
}

// ---- fake backend (httptest-like) ----
async function fakeBackend(handler) {
  const calls = [];
  const srv = http.createServer((req, res) => {
    let b = "";
    req.on("data", (c) => (b += c));
    req.on("end", () => {
      const call = { method: req.method, url: req.url, headers: req.headers, body: b ? JSON.parse(b) : undefined };
      calls.push(call);
      const [status, body] = handler(call, calls) || [200, { ok: true }];
      res.writeHead(status, { "Content-Type": "application/json" });
      res.end(JSON.stringify(body));
    });
  });
  await new Promise((r) => srv.listen(0, "127.0.0.1", r));
  return { base: `http://127.0.0.1:${srv.address().port}`, calls, close: () => new Promise((r) => srv.close(r)) };
}

function service({ mode = "on", allow = "", base = "http://127.0.0.1:1", now, resolver = fakeResolver(), fetchImpl } = {}) {
  const events = [], messages = [], sse = [];
  const pending = memoryPendingStore();
  const svc = createActionService({
    specs: ACTION_SPECS, pending, resolver, backendBase: base, fetchImpl, now,
    env: { ASK_MESHA_ACTIONS: mode, ASK_MESHA_ACTIONS_ALLOW: allow },
    emit: async (name, ctx, fields) => events.push({ name, ctx, fields }),
    addMessage: async (chatId, m) => messages.push({ chatId, ...m }),
  });
  const ctx = { email: CEO.email, tenantId: CEO.tenantId, chatId: "chat-1", send: (e) => sse.push(e) };
  return { svc, pending, events, messages, sse, ctx };
}

// ---------------- kill switch / modes ----------------
test("kill switch: off by default, only on|dry enable; MCP and non-streaming stay read-only", async () => {
  assert.equal(actionsMode({}), "off");
  assert.equal(actionsMode({ ASK_MESHA_ACTIONS: "ON" }), "on");
  assert.equal(actionsMode({ ASK_MESHA_ACTIONS: "dry" }), "dry");
  assert.equal(actionsMode({ ASK_MESHA_ACTIONS: "yes" }), "off");
  assert.equal(actionsEnabledFor({ mode: "on", client: "mcp", streaming: false }), false);
  assert.equal(actionsEnabledFor({ mode: "on", client: "mcp", streaming: true }), false);
  assert.equal(actionsEnabledFor({ mode: "on", client: null, streaming: false }), false);
  assert.equal(actionsEnabledFor({ mode: "off", client: null, streaming: true }), false);
  assert.equal(actionsEnabledFor({ mode: "dry", client: null, streaming: true }), true);
  const { svc, ctx, pending } = service({ mode: "off" });
  const r = await svc.propose({ action: "pc_care_close_task", params: { task_id: U(5), reason: "pen sold" } }, ctx);
  assert.equal(r.ok, false);
  assert.equal(pending._rows.size, 0);
  const c = await svc.confirm(U(5), CEO, BEARER);
  assert.equal(c.status, 403);
});
test("allow-list narrows actions", async () => {
  assert.deepEqual(allowedActions(ACTION_SPECS, { ASK_MESHA_ACTIONS_ALLOW: "pc_care_plan_task, nope" }), ["pc_care_plan_task"]);
  const { svc, ctx } = service({ allow: "pc_care_plan_task" });
  const r = await svc.propose({ action: "shifting_approve", params: { request_id: U(5) } }, ctx);
  assert.equal(r.ok, false);
  assert.match(r.text, /not an action/);
});
test("scope: no herd/money/people/delete/birth/death actions exist", () => {
  for (const n of Object.keys(ACTION_SPECS)) assert.doesNotMatch(n, /delete|birth|death|goat|herd|sale|price|money|user|access|role|shift_create/);
  assert.equal(Object.keys(ACTION_SPECS).length, 28);
  for (const [n, s] of Object.entries(ACTION_SPECS)) assert.notEqual(s.prepare, undefined, n);
});

// ---------------- schema validation ----------------
test("schema validation rejects bad params before any lookup", async () => {
  let looked = false;
  const { svc, ctx } = service({ resolver: fakeResolver({ park: async () => { looked = true; throw new Error("x"); } }) });
  const bad = [
    ["pc_care_plan_round", { category: "anti_protozoan", park: "CBE", pens: ["G1P3"], date: FUTURE, assignees: ["Ravi"] }], // not a round category
    ["pc_care_plan_round", { category: "deworming", park: "CBE", pens: [], date: FUTURE, assignees: ["Ravi"] }],
    ["pc_care_plan_task", { category: "deworming", park: "CBE", pen: "G1P3", date: "30/09/2026", assignees: ["Ravi"] }],
    ["pc_care_close_task", { task_id: "not-a-uuid", reason: "x y z" }],
    ["pc_care_close_task", { task_id: U(1) }], // reason required
    ["weighing_edit_plan", { campaign_id: U(1) }], // nothing to change
    ["vaccination_capacity_config", { max_per_day: 500 }],
    ["vaccination_operator_config", { park: "CBE", active_operators_per_day: 4 }],
    ["leadership_task_raise", { assignee: "Asha", title: "x".repeat(81), deadline_at: "2026-10-01T10:00:00+05:30" }],
    ["leadership_task_raise", { assignee: "Asha", title: "ok", deadline_at: "tomorrow" }],
    ["leadership_task_status", { task: "12", status: "deleted" }],
    ["sop_create", { code: "Bad Code", name: "x" }],
    ["shifting_reject", { request_id: U(1) }], // reason required
    ["pc_care_plan_task", { category: "deworming", park: "CBE", pen: "G1P3", date: FUTURE, assignees: ["Ravi"], goat_ids: ["x"] }], // unknown key (strict)
  ];
  for (const [action, params] of bad) {
    const r = await svc.propose({ action, params }, ctx);
    assert.equal(r.ok, false, `${action} ${JSON.stringify(params)}`);
    assert.match(r.text, /Invalid params/);
  }
  assert.equal(looked, false);
});
test("past dates are refused in prepare", async () => {
  const { svc, ctx } = service();
  const r = await svc.propose({ action: "pc_care_plan_task", params: { category: "deworming", park: "CBE", pen: "G1P3", date: "2020-01-01", assignees: ["Ravi"] } }, ctx);
  assert.equal(r.ok, false);
  assert.match(r.text, /in the past/);
});

// ---------------- name resolution + exact request bodies ----------------
test("pc_care_plan_round resolves names to ids and builds the handler's body", async () => {
  const { svc, ctx, sse, pending } = service();
  const r = await svc.propose({ action: "pc_care_plan_round", params: { category: "deworming", park: "CBE", pens: ["G1P3", "C1"], date: FUTURE, assignees: ["Ravi", "Asha"] } }, ctx);
  assert.equal(r.ok, true, r.text);
  assert.deepEqual(r.proposal.request, { method: "POST", path: "/app/pc-care/rounds", body: {
    category: "deworming", park_id: PARK, pens: [{ shed_id: SHED, partition_label: "3" }, { shed_id: SHED2, partition_label: "" }],
    planned_business_date: FUTURE, assignee_user_ids: [RAVI, ASHA] } });
  assert.equal(sse.length, 1);
  assert.equal(sse[0].type, "action_proposal");
  assert.equal(sse[0].risk, "normal");
  assert.equal(sse[0].requires_double_confirm, false);
  assert.match(sse[0].title, /2 pens, Coimbatore/);
  assert.equal(pending._rows.get(r.proposal.id).email, CEO.email);
  assert.equal(new Date(r.proposal.expires_at) - new Date(r.proposal.created_at), PROPOSAL_TTL_MS);
});
test("unknown / ambiguous names come back as questions, nothing stored", async () => {
  const { svc, ctx, pending, sse } = service();
  let r = await svc.propose({ action: "pc_care_plan_task", params: { category: "deworming", park: "CBE", pen: "Z9", date: FUTURE, assignees: ["Ravi"] } }, ctx);
  assert.match(r.text, /No active pen "Z9"/);
  r = await svc.propose({ action: "pc_care_plan_task", params: { category: "deworming", park: "CBE", pen: "G1P3", date: FUTURE, assignees: ["Ra"] } }, ctx);
  assert.match(r.text, /several people/);
  assert.equal(pending._rows.size, 0);
  assert.equal(sse.length, 0);
});
test("request bodies for every action family", async () => {
  const { svc, ctx } = service();
  const P = async (action, params) => { const r = await svc.propose({ action, params }, ctx); assert.equal(r.ok, true, `${action}: ${r.text}`); return r.proposal; };
  let p = await P("pc_care_close_task", { task_id: U(5), reason: "pen emptied" });
  assert.deepEqual(p.request, { method: "POST", path: `/app/pc-care/tasks/${U(5)}/close`, body: { reason: "pen emptied" } });
  p = await P("pc_care_reopen_task", { task_id: U(5) });
  assert.equal(p.request.path, `/app/pc-care/tasks/${U(5)}/reopen`);
  p = await P("pc_care_close_round", { round_id: U(6), reason: "rain all week" });
  assert.equal(p.risk, "high");
  assert.equal(p.request.path, `/app/pc-care/rounds/${U(6)}/close`);

  p = await P("weighing_create_plan", { park: "CBE", date: FUTURE, pens: ["G1P3"], operator: "Ravi", removal_operator: "Asha" });
  assert.deepEqual(p.request.body, { park_id: PARK, period_start_date: FUTURE, period_end_date: FUTURE, start_business_date: FUTURE, planned_cap_per_day: 200,
    operator_user_id: RAVI, fasting_operator_user_id: ASHA, sheds: [{ location_id: SHED, location_type: "shed", display_name: "Godel 1 Part 3", partition_label: "3", weighing_category: "individual_animal" }] });
  p = await P("weighing_edit_plan", { campaign_id: U(7), operator: "Asha", add_pens: ["C1"] });
  assert.equal(p.request.method, "PUT");
  assert.equal(p.request.body.operator_user_id, ASHA);
  assert.equal(p.request.body.fasting_operator_user_id, ASHA); // kept from current plan
  assert.equal(p.request.body.planned_cap_per_day, 150);
  assert.deepEqual(p.request.body.sheds.map((s) => s.location_id), [SHED, SHED2]);
  p = await P("weighing_edit_plan", { campaign_id: U(7), remove_pens: ["G1P3"], add_pens: ["C1"] });
  assert.deepEqual(p.request.body.sheds.map((s) => s.location_id), [SHED2]);
  const none = await svc.propose({ action: "weighing_edit_plan", params: { campaign_id: U(7), remove_pens: ["C1"] } }, ctx);
  assert.match(none.text, /None of those pens/);
  p = await P("weighing_publish_plan", { campaign_id: U(7) });
  assert.equal(p.requires_double_confirm, true);
  p = await P("weighing_close_pen", { campaign_id: U(7), pen: "G1P3", reason: "animals sold" });
  assert.deepEqual(p.request, { method: "POST", path: `/app/weighing/campaigns/${U(7)}/sheds/${U(50)}/close`, body: { reason: "animals sold", idempotency_key: p.id } });
  p = await P("weighing_reopen_pen", { campaign_id: U(7), pen: "G1P3" });
  assert.deepEqual(p.request.body, { reason: "" }); // weighing decode refuses an empty body
  p = await P("weighing_close_round", { campaign_id: U(7), reason: "open_buckets_closed" });
  assert.equal(p.request.body.idempotency_key, p.id);
  assert.equal(p.risk, "high");

  p = await P("vaccination_postpone_drive", { park: "CBE", vaccine_code: "PPR", original_date: FUTURE, new_date: FUTURE, reason: "rain" });
  assert.deepEqual(p.request.body, { park_id: PARK, vaccine_code: "PPR", original_drive_date: FUTURE, override_date: FUTURE, reason: "rain" });
  const due = new Date(Date.now() + 3 * 864e5).toISOString().replace(/\.\d+Z$/, "Z");
  p = await P("vaccination_reschedule_obligation", { obligation_id: U(8), due_at: due });
  assert.deepEqual(p.request.body, { due_at: due, window_start: due });
  p = await P("vaccination_operator_config", { park: "CBE", selected_operators: ["Ravi", "Asha"] });
  assert.deepEqual(p.request.body, { parkId: PARK, activeOperatorsPerDay: 1, defaultOperatorId: RAVI_WM, selectedOperatorIds: [RAVI_WM, ASHA_WM], rowVersion: 2 });
  p = await P("vaccination_capacity_config", { max_per_day: 150 });
  assert.deepEqual(p.request.body, { maxPerDay: 150, capacityScope: "tenant", maxBufferDays: 7, overflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap", rowVersion: 4, maxShotsPerAnimalPerDrive: null });

  const dl = new Date(Date.now() + 864e5).toISOString().replace(/\.\d+Z$/, "+00:00");
  p = await P("leadership_task_raise", { assignee: "Asha", title: "Fix water line", deadline_at: dl });
  assert.deepEqual(p.request.body, { title: "Fix water line", body: "", assignee_user_id: ASHA, deadline_at: dl, attachments: [] });
  p = await P("leadership_task_edit", { task: "#12", title: "Fix water line today" });
  assert.deepEqual(p.request.body, { title: "Fix water line today", body: "shed 4", attachments: [{ proof_id: U(61), kind: "photo", file_name: "a.jpg" }], row_version: 3, deadline_at: "" });
  p = await P("leadership_task_status", { task: "12", status: "cancelled" });
  assert.deepEqual(p.request.body, { status: "cancelled", row_version: 3 });
  p = await P("leadership_task_comment", { task: "12", comment: "by Friday", mentions: ["Ravi"] });
  assert.deepEqual(p.request.body, { comment: "by Friday", mentions: [{ user_id: RAVI }] });

  p = await P("sop_task_create", { sop: "feed.daily_check", title: "Check feed", park: "CBE", pen: "G1P3", assignee: "Ravi" });
  assert.equal(p.request.path, "/admin/tasks");
  assert.equal(p.request.body.scope_type, "shed");
  assert.equal(p.request.body.scope_id, SHED);
  p = await P("sop_task_assign", { task_id: U(9), assignee: "Asha" });
  assert.deepEqual(p.request.body, { assigned_to: ASHA, reason: "", row_version: 5 });
  p = await P("sop_task_verify", { task_id: U(9) });
  assert.equal(p.request.path, `/admin/tasks/${U(9)}/verify`);
  p = await P("sop_task_rework", { task_id: U(9), reason: "photo blurry" });
  assert.deepEqual(p.request.body, { reason: "photo blurry", row_version: 5 });

  p = await P("sop_create", { code: "health.hoof_check", name: "Hoof check" });
  assert.deepEqual(p.request.body, { code: "health.hoof_check", name: "Hoof check", description: "", kind: "module", module_key: "" });
  p = await P("sop_draft_version", { sop: "feed.daily_check", version_label: "v3" });
  assert.deepEqual(p.request.body, { version_label: "v3", form_dsl: { steps: [1] }, proof_policy: { p: 1 }, compatibility: {} });
  p = await P("sop_publish_version", { sop: "feed.daily_check" });
  assert.deepEqual(p.request, { method: "POST", path: `/admin/sops/${U(70)}/versions/${U(71)}/publish`, body: { row_version: 1 } });
  assert.equal(p.risk, "high");
  const dup = await svc.propose({ action: "sop_create", params: { code: "feed.daily_check", name: "Dup" } }, ctx);
  assert.match(dup.text, /already exists/);

  p = await P("shifting_approve", { request_id: U(20) });
  assert.deepEqual(p.request, { method: "POST", path: `/admin-web/counts/approvals/${U(20)}/approve`, body: {} });
  p = await P("shifting_reject", { request_id: U(20), reason: "wrong pen" });
  assert.deepEqual(p.request.body, { reason: "wrong pen" });
});

// ---------------- shifting-only guard ----------------
test("shifting-only: birth/death approvals refused at propose", async () => {
  for (const t of ["birth", "death"]) {
    const { svc, ctx, pending } = service({ resolver: fakeResolver({ approval: async (id) => ({ approval_request_id: id, request_type: t, status: "pending" }) }) });
    const r = await svc.propose({ action: "shifting_approve", params: { request_id: U(21) } }, ctx);
    assert.equal(r.ok, false);
    assert.match(r.text, /only decide shifting/);
    assert.equal(pending._rows.size, 0);
  }
});
test("shifting-only: server re-checks type with the backend at confirm, refuses non-shifting", async () => {
  const be = await fakeBackend((c) => (c.method === "GET"
    ? [200, { items: [{ approval_request_id: U(22), request_type: "death", status: "pending" }] }]
    : [200, { status: "approved" }]));
  try {
    const { svc, ctx, events, messages } = service({ base: be.base });
    const r = await svc.propose({ action: "shifting_approve", params: { request_id: U(22) } }, ctx); // DB says shifting
    const c = await svc.confirm(r.proposal.id, CEO, BEARER, { confirm_text: "CONFIRM" });
    assert.equal(c.status, 422);
    assert.equal(be.calls.filter((x) => x.method === "POST").length, 0, "never POSTs a non-shifting decision");
    assert.ok(events.some((e) => e.name === "action_failed" && e.fields.error_class === "guard"));
    assert.match(messages.at(-1).content, /^Not done/);
  } finally { await be.close(); }
});
test("shiftingGuard follows pagination and requires still-pending", async () => {
  const p = { request: { path: `/admin-web/counts/approvals/${U(23)}/approve` } };
  const pages = { "": { items: [], next_cursor: "c2" }, c2: { items: [{ approval_request_id: U(23), request_type: "shifting" }] } };
  const ok = await shiftingGuard(p, async (_m, pth) => ({ status: 200, body: pages[new URL(pth, "http://x").searchParams.get("cursor") || ""] }));
  assert.deepEqual(ok, { ok: true });
  const gone = await shiftingGuard(p, async () => ({ status: 200, body: { items: [] } }));
  assert.equal(gone.ok, false);
});

// ---------------- owner / expiry / single-use / double confirm ----------------
test("owner check: other email or other tenant gets 404", async () => {
  const { svc, ctx } = service();
  const r = await svc.propose({ action: "pc_care_close_task", params: { task_id: U(5), reason: "pen emptied" } }, ctx);
  assert.equal((await svc.confirm(r.proposal.id, { email: "other@mesha.sg", tenantId: TENANT }, BEARER)).status, 404);
  assert.equal((await svc.confirm(r.proposal.id, { email: CEO.email, tenantId: U(98) }, BEARER)).status, 404);
  assert.equal((await svc.cancel(r.proposal.id, { email: "other@mesha.sg", tenantId: TENANT })).status, 404);
  assert.equal((await svc.confirm(U(404), CEO, BEARER)).status, 404);
});
test("expiry: confirm after 10 minutes is 410 and nothing is sent", async () => {
  let t = Date.now();
  const be = await fakeBackend(() => [200, {}]);
  try {
    const { svc, ctx } = service({ base: be.base, now: () => new Date(t) });
    const r = await svc.propose({ action: "pc_care_close_task", params: { task_id: U(5), reason: "pen emptied" } }, ctx);
    t += PROPOSAL_TTL_MS + 1;
    const c = await svc.confirm(r.proposal.id, CEO, BEARER);
    assert.equal(c.status, 410);
    assert.equal(be.calls.length, 0);
  } finally { await be.close(); }
});
test("end to end against a fake backend: headers, single use, result posted to chat, events", async () => {
  const be = await fakeBackend(() => [200, { status: "closed" }]);
  try {
    const { svc, ctx, events, messages, pending } = service({ base: be.base });
    const r = await svc.propose({ action: "pc_care_close_task", params: { task_id: U(5), reason: "pen emptied" } }, ctx);
    const c = await svc.confirm(r.proposal.id, CEO, BEARER, {});
    assert.equal(c.status, 200);
    assert.equal(c.body.ok, true);
    assert.equal(be.calls.length, 1);
    const call = be.calls[0];
    assert.equal(call.method, "POST");
    assert.equal(call.url, `/app/pc-care/tasks/${U(5)}/close`);
    assert.deepEqual(call.body, { reason: "pen emptied" });
    assert.equal(call.headers.authorization, BEARER);
    assert.equal(call.headers["x-goatos-tenant-id"], TENANT);
    assert.equal(call.headers["idempotency-key"], r.proposal.id);
    assert.equal(call.headers["x-mesha-client"], CLIENT_HEADER);
    // single use
    const again = await svc.confirm(r.proposal.id, CEO, BEARER, {});
    assert.equal(again.status, 409);
    assert.equal(be.calls.length, 1);
    assert.equal((await svc.cancel(r.proposal.id, CEO)).status, 409);
    assert.equal(pending._rows.get(r.proposal.id).status, "executed");
    assert.ok(events.some((e) => e.name === "action_proposed"));
    assert.ok(events.some((e) => e.name === "action_executed" && e.fields.backend_status === 200 && e.ctx.email === CEO.email));
    assert.equal(messages.length, 1);
    assert.equal(messages[0].chatId, "chat-1");
    assert.match(messages[0].content, /^Done: Close deworming task/);
  } finally { await be.close(); }
});
test("backend refusal is relayed (403/409) and marks the proposal failed", async () => {
  const be = await fakeBackend(() => [409, { error: "row_version_conflict", message: "Someone changed this first." }]);
  try {
    const { svc, ctx, events, messages } = service({ base: be.base });
    const r = await svc.propose({ action: "leadership_task_status", params: { task: "12", status: "cancelled" } }, ctx);
    const c = await svc.confirm(r.proposal.id, CEO, BEARER);
    assert.equal(c.status, 409);
    assert.equal(c.body.ok, false);
    assert.ok(events.some((e) => e.name === "action_failed" && e.fields.error_class === "backend"));
    assert.match(messages.at(-1).content, /Someone changed this first/);
    assert.equal((await svc.confirm(r.proposal.id, CEO, BEARER)).status, 409); // no silent retry of a refusal
  } finally { await be.close(); }
});
test("double confirm: high-risk needs confirm_text CONFIRM", async () => {
  const be = await fakeBackend(() => [200, { campaign: {} }]);
  try {
    const { svc, ctx } = service({ base: be.base });
    const r = await svc.propose({ action: "weighing_publish_plan", params: { campaign_id: U(7) } }, ctx);
    assert.equal(r.event.requires_double_confirm, true);
    assert.equal((await svc.confirm(r.proposal.id, CEO, BEARER, {})).status, 428);
    assert.equal((await svc.confirm(r.proposal.id, CEO, BEARER, { confirm_text: "confirm" })).status, 428);
    assert.equal(be.calls.length, 0);
    const ok = await svc.confirm(r.proposal.id, CEO, BEARER, { confirm_text: "CONFIRM" });
    assert.equal(ok.status, 200);
    assert.equal(be.calls.length, 1);
  } finally { await be.close(); }
});
test("idempotency key is reused across a transport-failure retry", async () => {
  let n = 0;
  const be = await fakeBackend(() => [200, {}]);
  const flaky = async (url, init) => { n += 1; if (n === 1) throw new Error("socket hang up"); return fetch(url, init); };
  try {
    const { svc, ctx, pending } = service({ base: be.base, fetchImpl: flaky });
    const r = await svc.propose({ action: "vaccination_postpone_drive", params: { park: "CBE", vaccine_code: "PPR", original_date: FUTURE, new_date: FUTURE, reason: "rain" } }, ctx);
    const first = await svc.confirm(r.proposal.id, CEO, BEARER);
    assert.equal(first.status, 502);
    assert.equal(pending._rows.get(r.proposal.id).status, "transport_failed");
    const second = await svc.confirm(r.proposal.id, CEO, BEARER);
    assert.equal(second.status, 200);
    assert.equal(be.calls.length, 1);
    assert.equal(be.calls[0].headers["idempotency-key"], r.proposal.id);
    assert.equal(pending._rows.get(r.proposal.id).attempts, 2);
    assert.equal((await svc.confirm(r.proposal.id, CEO, BEARER)).status, 409);
  } finally { await be.close(); }
});
test("concurrent confirms: exactly one reaches the backend", async () => {
  const be = await fakeBackend(() => [200, {}]);
  try {
    const { svc, ctx } = service({ base: be.base });
    const r = await svc.propose({ action: "pc_care_reopen_task", params: { task_id: U(5) } }, ctx);
    const rs = await Promise.all([1, 2, 3].map(() => svc.confirm(r.proposal.id, CEO, BEARER)));
    assert.deepEqual(rs.map((x) => x.status).sort(), [200, 409, 409]);
    assert.equal(be.calls.length, 1);
  } finally { await be.close(); }
});
test("bearer is never persisted: not in the pending row, events, messages or SSE", async () => {
  const be = await fakeBackend(() => [200, {}]);
  try {
    const { svc, ctx, pending, events, messages, sse } = service({ base: be.base });
    const r = await svc.propose({ action: "pc_care_reopen_task", params: { task_id: U(5) } }, ctx);
    await svc.confirm(r.proposal.id, CEO, BEARER);
    const dump = JSON.stringify([[...pending._rows.values()], events, messages, sse]);
    assert.doesNotMatch(dump, /fake-ceo-token/);
    assert.doesNotMatch(dump, /Bearer/);
  } finally { await be.close(); }
});
test("confirm without a bearer is refused before anything is claimed", async () => {
  const { svc, ctx, pending } = service();
  const r = await svc.propose({ action: "pc_care_reopen_task", params: { task_id: U(5) } }, ctx);
  assert.equal((await svc.confirm(r.proposal.id, CEO, "")).status, 401);
  assert.equal(pending._rows.get(r.proposal.id).status, "pending");
});
test("cancel: owner cancels once; confirm afterwards is 409", async () => {
  const { svc, ctx, messages } = service();
  const r = await svc.propose({ action: "pc_care_reopen_task", params: { task_id: U(5) } }, ctx);
  assert.equal((await svc.cancel(r.proposal.id, CEO)).status, 200);
  assert.match(messages.at(-1).content, /^Cancelled/);
  assert.equal((await svc.confirm(r.proposal.id, CEO, BEARER)).status, 409);
});
test("dry mode: returns the exact request with redacted bearer and sends nothing", async () => {
  const be = await fakeBackend(() => [200, {}]);
  try {
    const { svc, ctx, pending, sse } = service({ mode: "dry", base: be.base });
    const r = await svc.propose({ action: "weighing_close_round", params: { campaign_id: U(7), reason: "open_buckets_closed" } }, ctx);
    assert.equal(sse[0].dry_run, true);
    const c = await svc.confirm(r.proposal.id, CEO, BEARER, { confirm_text: "CONFIRM" });
    assert.equal(c.status, 200);
    assert.equal(c.body.dry_run, true);
    assert.deepEqual(c.body.request.body, { reason: "open_buckets_closed", idempotency_key: r.proposal.id });
    assert.equal(c.body.request.path, `/app/weighing/campaigns/${U(7)}/close`);
    assert.equal(c.body.request.headers.Authorization, "<redacted>");
    assert.equal(c.body.request.headers["Idempotency-Key"], r.proposal.id);
    assert.equal(be.calls.length, 0);
    assert.equal(pending._rows.get(r.proposal.id).status, "pending");
  } finally { await be.close(); }
});

// ---------------- SQL resolver (read-only, fake runSql) ----------------
test("resolver: quotes input, tenant-scopes, parses row_to_json lines, only SELECTs", async () => {
  const seen = [];
  const runSql = async (sql) => {
    seen.push(sql);
    if (/FROM workforce_members/.test(sql)) return { ok: true, out: `row_to_json\n${JSON.stringify({ user_id: RAVI, workforce_member_id: RAVI_WM, name: "O'Neil", primary_location_id: PARK })}` };
    if (/location_type='park'/.test(sql)) return { ok: true, out: JSON.stringify({ park_id: PARK, name: "Coimbatore" }) };
    return { ok: true, out: "" };
  };
  const r = createSqlResolver({ runSql, tenantId: TENANT, pensSql: () => fs.readFileSync(path.join(REPO, ".agents/skills/mesha-data-map/references/pens.sql"), "utf8") });
  assert.equal((await r.person("O'Neil")).user_id, RAVI);
  assert.equal((await r.park("CBE")).park_id, PARK);
  await assert.rejects(r.pcTask("1; drop table x"), /must be an id/);
  await assert.rejects(r.pens(PARK, ["G1P3"]), /No active pen/);
  for (const s of seen) {
    assert.match(s, /^SELECT row_to_json\(t\)::text FROM \(/);
    assert.doesNotMatch(s, /\b(insert|update|delete|drop|alter|create)\s/i);
  }
  assert.ok(seen.some((s) => s.includes(`'O''Neil'`)));
  assert.ok(seen.filter((s) => /workforce_members|location_type='park'/.test(s)).every((s) => s.includes(`tenant_id = '${TENANT}'`)));
  assert.equal(lit("a'b"), "'a''b'");
  assert.deepEqual(parseJsonRows('row_to_json\n{"a":"x\\ny"}\n'), [{ a: "x\ny" }]);
});
test("pensCtes extracts pl/pk/pens from the shipped pens.sql", () => {
  const t = fs.readFileSync(path.join(REPO, ".agents/skills/mesha-data-map/references/pens.sql"), "utf8");
  const c = pensCtes(t);
  assert.match(c, /^WITH pl AS/);
  assert.match(c, /pens AS \(/);
  assert.doesNotMatch(c, /pa AS \(|-- DEMO/);
});
