#!/usr/bin/env node
// Local preview seed: fills every admin-web feature with current, date-relative data so a
// reviewer sees non-empty screens inside each page's DEFAULT date window.
//
// Sanctioned path only: every write goes through the running Goat OS API (the same domain write
// paths, audit and transactional outbox the apps use). Nothing here inserts into projection tables.
//
// Safety: refuses anything but a loopback API, and GOATOS_ENV must be local/dev/test.
// Idempotent: every write carries a stable idempotency key or is skipped when a row with the same
// seed marker already exists, so re-running the same day is a no-op and running on a later day
// adds that day's rows (date-relative).
//
// Usage:
//   source ~/mesha/pr294-run/web-env.sh   # GOATOS_API_BASE_URL, GOATOS_AUTH_HS256_SECRET, GOATOS_TENANT_ID
//   node tools/dev/seed-preview-data.mjs [--only=tasks,sales] [--dry-run]
//   make seed-preview-data
import { createHmac, createHash } from "node:crypto";

const API = process.env.GOATOS_API_BASE_URL || "http://127.0.0.1:18250";
const ENV = process.env.GOATOS_ENV || "";
const SECRET = process.env.GOATOS_AUTH_HS256_SECRET || "";
const TENANT = process.env.GOATOS_TENANT_ID || "00000000-0000-4000-8000-000000000001";
const ISSUER = process.env.GOATOS_AUTH_ISSUER || "goatos-local";
const AUDIENCE = process.env.GOATOS_AUTH_AUDIENCE || "goatos-api";
const args = Object.fromEntries(process.argv.slice(2).map((a) => { const [k, v] = a.replace(/^--/, "").split("="); return [k, v ?? "1"]; }));
const ONLY = args.only ? new Set(args.only.split(",")) : null;
const DRY = Boolean(args["dry-run"]);

if (!/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?$/.test(API)) throw new Error(`refusing non-loopback API ${API}`);
if (!["local", "dev", "test"].includes(ENV)) throw new Error(`GOATOS_ENV must be local/dev/test (got "${ENV}")`);
if (SECRET.length < 32) throw new Error("GOATOS_AUTH_HS256_SECRET is required (local dev secret)");

// ---- identities ---------------------------------------------------------------------------------
export const USERS = {
  admin: "90000000-0000-4000-8000-000000000101",
  ceo: "10101010-1010-4101-8101-101010101010",
  director: "20202020-2020-4202-8202-202020202020",
  operator: "30303030-3030-4303-8303-303030303030",
};
export const PARKS = { cbe: "00000000-0000-4000-8000-000000003001", cpt: "00000000-0000-4000-8000-000000003002" };

const b64u = (b) => Buffer.from(b).toString("base64url");
function mint(sub) {
  const now = Math.floor(Date.now() / 1000);
  const h = b64u(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const p = b64u(JSON.stringify({ aud: AUDIENCE, iss: ISSUER, sub, tenant_id: TENANT, nbf: now - 60, exp: now + 3600 }));
  return `${h}.${p}.${createHmac("sha256", SECRET).update(`${h}.${p}`).digest("base64url")}`;
}

// ---- dates (Asia/Kolkata business days) ----------------------------------------------------------
const IST_MS = 330 * 60 * 1000;
export const today = () => new Date(Date.now() + IST_MS).toISOString().slice(0, 10);
export const addDays = (iso, n) => new Date(Date.parse(iso + "T00:00:00Z") + n * 86400000).toISOString().slice(0, 10);
export const istAt = (iso, hh = 10, mm = 0) => new Date(Date.parse(`${iso}T${String(hh).padStart(2, "0")}:${String(mm).padStart(2, "0")}:00+05:30`)).toISOString();
export const key = (...parts) => "seed-preview:" + createHash("sha1").update(parts.join("|")).digest("hex").slice(0, 24);

// ---- http --------------------------------------------------------------------------------------
const stats = {};
export async function api(method, path, body, { as = "admin", ok = [200, 201, 202, 204], headers = {} } = {}) {
  if (DRY && method !== "GET") { console.log(`DRY ${method} ${path}`); return { status: 0, data: null }; }
  const res = await fetch(API + path, {
    method,
    headers: { Authorization: `Bearer ${mint(USERS[as] || as)}`, "Content-Type": "application/json", ...headers },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!ok.includes(res.status)) {
    const err = new Error(`${method} ${path} -> ${res.status} ${typeof data === "string" ? data.slice(0, 300) : JSON.stringify(data).slice(0, 400)}`);
    err.status = res.status; err.data = data;
    throw err;
  }
  return { status: res.status, data };
}
export const get = (path, opts) => api("GET", path, undefined, opts).then((r) => r.data);

function tally(feature, outcome) { stats[feature] ??= { ok: 0, skip: 0, fail: 0 }; stats[feature][outcome]++; }
export async function attempt(feature, label, fn) {
  try { const r = await fn(); tally(feature, r === "skip" ? "skip" : "ok"); return r; }
  catch (e) { tally(feature, "fail"); console.warn(`  ! ${feature}: ${label}: ${e.message}`); return null; }
}

// ---- proof media ---------------------------------------------------------------------------------
// A 3 s test-pattern clip + a still, rendered once with ffmpeg so the review player has a real
// video to play. Without ffmpeg the upload still succeeds with a tiny placeholder body.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
const mediaDir = join(tmpdir(), "goatos-seed-preview-media");
function media(kind) {
  mkdirSync(mediaDir, { recursive: true });
  const f = join(mediaDir, kind === "video" ? "sample.mp4" : "sample.jpg");
  if (!existsSync(f)) {
    try {
      execFileSync("ffmpeg", kind === "video"
        ? ["-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=480x270:rate=15:duration=3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", f]
        : ["-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=640x480:duration=1", "-frames:v", "1", f]);
    } catch { return Buffer.from(`goatos-seed-preview-${kind}`); }
  }
  return readFileSync(f);
}

// Upload one completed proof artifact the way the phone does: create -> PUT bytes -> complete.
export async function proof(as, { scopeType, scopeId, subjectType, subjectId, kind = "video", meta = {} }) {
  const now = Date.now();
  const { data } = await api("POST", "/app/proofs/uploads", {
    proof_type: kind, mime_type: kind === "video" ? "video/mp4" : "image/jpeg",
    scope_type: scopeType, scope_id: scopeId, subject_type: subjectType, subject_id: subjectId ?? null,
    metadata: { capture_source: "in_app_camera", ...(kind === "video" ? { captured_start_ms: now - 3000, captured_end_ms: now } : {}), ...meta },
  }, { as });
  const id = data.proof.proof_id;
  const url = data.upload_url.startsWith("http") ? data.upload_url : API + data.upload_url;
  const body = media(kind);
  const put = await fetch(url, { method: "PUT", headers: { "Content-Type": kind === "video" ? "video/mp4" : "image/jpeg", Authorization: `Bearer ${mint(USERS[as] || as)}` }, body });
  if (!put.ok) throw new Error(`proof PUT ${put.status} ${await put.text()}`);
  await api("POST", `/app/proofs/${id}/complete`, { mime_type: kind === "video" ? "video/mp4" : "image/jpeg", size_bytes: body.length, ...(kind === "video" ? { duration_ms: 3000 } : {}) }, { as });
  return id;
}

// ---- local DB reads (lookup only; never written) ------------------------------------------------
// Picking real ids (sheds, goats, open campaigns) needs a read the public API does not offer in
// one call. Reads go through psql against DATABASE_URL, loopback only.
const DB = process.env.DATABASE_URL || "postgres://postgres@127.0.0.1:15550/goatos?sslmode=disable";
if (!/@(127\.0\.0\.1|localhost):/.test(DB)) throw new Error("refusing non-loopback DATABASE_URL");
export function q(sql) {
  const out = execFileSync("psql", [DB, "-X", "-At", "-F", "\t", "-v", "ON_ERROR_STOP=1", "-c", `BEGIN READ ONLY; ${sql}; COMMIT;`], { encoding: "utf8" });
  return out.split("\n").filter((l) => l && l !== "BEGIN" && l !== "COMMIT").map((l) => l.split("\t"));
}

// ---- features ----------------------------------------------------------------------------------
const features = [];
export const feature = (name, fn) => features.push({ name, fn });

// People access (/people, and the Tasks assignee picker, which lists only leaders whose access
// ticks the Tasks module). Only people with NO module grant yet are touched, so a hand-edited
// access row is never overwritten.
feature("people", async () => {
  const plan = {
    [USERS.ceo]: { designation_code: "ceo_internal", scope_mode: "tenant", levels: ["view", "do", "oversee", "configure"] },
    [USERS.director]: { designation_code: "pc_director", scope_mode: "tenant", levels: ["view", "do", "oversee", "configure"] },
    // Operators work from the phone: mobile ticks only, so no web screen list is needed.
    [USERS.operator]: { designation_code: "operator", scope_mode: "parks", park_ids: [PARKS.cbe, PARKS.cpt], home_park_id: PARKS.cbe, levels: ["view", "do"], mobileOnly: true },
  };
  for (const [personId, p] of Object.entries(plan)) {
    await attempt("people", personId, async () => {
      const cur = await get(`/admin/workforce/people/${personId}/access`);
      if (cur.modules.some((m) => m.granted_web.length || m.granted_mobile.length)) return "skip";
      // "stock" is offered for Feed but the local schema check (person_module_access_capabilities_check) refuses it.
      const pick = (offered) => offered.filter((l) => p.levels.includes(l));
      const modules = cur.modules.map((m) => {
        const web = p.mobileOnly ? [] : pick(m.offered_web);
        return { module_key: m.module_key, web, mobile: pick(m.offered_mobile), pages: web.length ? (MODULE_PAGES[m.module_key] ?? []) : [] };
      });
      await api("PUT", `/admin/workforce/people/${personId}/access`, {
        designation_code: p.designation_code, scope_mode: p.scope_mode,
        ...(p.park_ids ? { park_ids: p.park_ids, home_park_id: p.home_park_id } : {}),
        modules, row_version: cur.row_version,
      });
    });
  }
});

// Web screen keys per module (backend/internal/permissions/capability_pages.go). A web grant on a
// module that has screens must tick at least one; leaders get every screen.
const MODULE_PAGES = {"vaccination": ["control-tower", "action-center", "protocol-adherence", "workflows", "preventive-care-vaccination", "vaccination-live-tracker", "vaccination-care-coverage", "vaccination-plan"], "calendar": ["calendar"], "counts": ["approvals", "counts-herd-analytics", "counts-breakdown", "counts-mortality", "counts-sops"], "leave_approvals": ["leave"], "leadership_tasks": ["leadership-tasks"], "verification": ["verification-actions"], "pc_care": ["pc-care-sops"], "procurement": ["procurement-source-entry", "procurement-sops"], "vendors": ["procurement-vendors"], "sales": ["sales-sold", "sales-farm-value", "sales-sops", "sales-loads", "sales-farm-born", "sales-market-analytics", "sales-buyer-analytics", "sales-vendors", "sales-config"], "feed_purchases": ["procurement-feed-purchases"], "animal_purchases": ["procurement-animal-purchases"], "milk": ["milk-preparation", "milk-sops"], "herd_signals": ["herd-signals"], "weighing": ["weighing-analytics", "weighing-sops"], "feed_direction": ["feed-config", "feed-analytics", "feed-sops"], "aas_health": ["health-analytics", "health-config"], "operations": ["audit-log", "dlq-center"], "people": ["people"], "work_board": ["work-board"], "alerts": ["alerts"], "pen_routines": ["pen-routines"], "configuration": ["configuration-items", "configuration-work-instructions"]};

// Leadership tasks (/tasks, work-board leadership lane): a spread of open / in progress / done /
// overdue tasks across the three leadership users, deadlines relative to today.
feature("tasks", async () => {
  const t = today();
  // The list is per viewer, so read it as every leader and union the titles.
  const existing = new Set();
  for (const as of ["admin", "ceo", "director"]) {
    for (const r of (await get("/app/leadership-tasks?limit=200", { as }).catch(() => ({ rows: [] })))?.rows ?? []) existing.add(r.title);
  }
  // Only CEO/CXO + directors with the Tasks tick can receive a task, and a task is always raised
  // for someone else, with a deadline after it is raised (overdue rows come from time passing).
  const rows = [
    ["Check water troughs in Kid Pen B", "director", 0.3, "open"],
    ["Repair pen 4 curtain", "director", 1, "in_progress"],
    ["Collect mineral mix invoice", "ceo", 1, "open"],
    ["Plan October deworming round", "director", 2, "open"],
    ["Confirm buyer visit for Friday load", "ceo", 2, "in_progress"],
    ["Review last week's mortality notes", "ceo", 0.4, "open"],
    ["Replace broken weighing scale battery", "director", 3, "open"],
    ["Order 40 bags maize", "ceo", 5, "open"],
    ["Share PPR drive photos with vet", "director", 1, "done"],
    ["Update pen capacity sheet", "ceo", 2, "done"],
    ["Call vendor about green fodder delay", "director", 3, "cancelled"],
    ["Audit feed store stock", "ceo", 4, "in_progress"],
    ["Inspect fencing at Channapatna", "director", 6, "open"],
    ["Schedule vet visit for kids", "ceo", 7, "open"],
  ];
  for (const [title, who, dd, status] of rows) {
    if (existing.has(title)) { tally("tasks", "skip"); continue; }
    const raiser = who === "ceo" ? "director" : "ceo";
    await attempt("tasks", title, async () => {
      const { data } = await api("POST", "/app/leadership-tasks", {
        title, body: "Seeded preview task.", assignee_user_id: USERS[who],
        deadline_at: istAt(addDays(t, Math.floor(dd)), dd < 1 ? 23 : 18, dd < 1 ? 30 : 0),
      }, { as: raiser, headers: { "Idempotency-Key": key("lt", t, title) } });
      const task = data?.task ?? data;
      if (status === "open" || !task?.task_id) return;
      let rv = task.row_version;
      for (const st of status === "in_progress" ? ["in_progress"] : status === "done" ? ["in_progress", "done"] : ["cancelled"]) {
        const r = await api("POST", `/app/leadership-tasks/${task.task_id}/status`, { status: st, row_version: rv }, { as: st === "cancelled" ? raiser : who, headers: { "Idempotency-Key": key("lt-status", task.task_id, st) } });
        rv = (r.data?.task ?? r.data)?.row_version ?? rv + 1;
      }
    });
  }
});

// Weighing (this week's campaign per park) + captures -> /weighing/weights and the Verify queue.
// An animal observation recorded by the operator with its proof clip enqueues a weighing
// verification item in the same request (weighing verificationbridge). Scanned tags are the real
// RFIDs of goats in the fattening pens, so the weights line up with the herd register.
const WEIGH_PENS = {
  [PARKS.cbe]: [["Fattening F1", "individual_animal"], ["Fattening F3", "individual_animal"], ["Fattening F2", "per_shed_partition"]],
  [PARKS.cpt]: [["Fattening F4", "individual_animal"], ["Fattening F5", "individual_animal"], ["Fattening F4B", "per_shed_partition"]],
};
feature("weighing", async () => {
  const t = today();
  const tomorrow = addDays(t, 1);
  const perShed = Number(args["per-pen"] ?? 6);
  // A weighing task is ONE park on ONE weigh date, and feed + water come off the evening BEFORE,
  // so a task can only be planned for a later day. Each run plans TOMORROW and captures into the
  // task planned for TODAY by the previous day's run (date-relative: run it daily).
  for (const [parkId, pens] of Object.entries(WEIGH_PENS)) {
    const [planned] = q(`select campaign_id from weighing_campaigns where park_id='${parkId}' and start_business_date='${tomorrow}' and status <> 'cancelled' limit 1`);
    if (!planned) {
      const sheds = pens.map(([name, category]) => {
        const [row] = q(`select l.location_id from locations l where l.name='${name}' and exists (select 1 from goats g where g.shed_id=l.location_id and g.park_id='${parkId}') limit 1`);
        return row && { location_id: row[0], location_type: "shed", display_name: name, weighing_category: category };
      }).filter(Boolean);
      const created = await attempt("weighing", `plan ${parkId} ${tomorrow}`, async () => (await api("POST", "/weighing/campaigns", {
        park_id: parkId, period_start_date: tomorrow, period_end_date: tomorrow, start_business_date: tomorrow,
        planned_cap_per_day: 200, operator_user_id: USERS.operator, fasting_operator_user_id: USERS.operator, sheds,
      }, { headers: { "Idempotency-Key": key("wcamp", parkId, tomorrow) } })).data);
      const id = created?.campaign?.campaign_id ?? created?.campaign_id;
      if (id) await attempt("weighing", `publish ${id}`, () => api("POST", `/weighing/campaigns/${id}/publish`, {}, { headers: { "Idempotency-Key": key("wpub", id) } }));
    }
    const [campaign] = q(`select campaign_id from weighing_campaigns where park_id='${parkId}' and start_business_date='${t}' and status in ('published','in_progress') limit 1`);
    if (!campaign) { console.log(`   no weighing task planned for ${t} at ${parkId} (the previous day's run plans it)`); continue; }
    const campaignId = campaign[0];
    const cs = q(`select campaign_shed_id, location_id, display_name, weighing_category from weighing_campaign_sheds where campaign_id='${campaignId}'`);
    for (const [campaignShedId, locationId, name, category] of cs) {
      if (category !== "individual_animal") continue;
      const tags = q(`select gi.identifier_value from goats g join goat_identifiers gi on gi.goat_id=g.goat_id and gi.status='active'
        where g.shed_id='${locationId}' and g.lifecycle_status='alive'
          and not exists (select 1 from weighing_observations o where o.campaign_shed_id='${campaignShedId}' and o.scanned_identifier=gi.identifier_value)
        order by gi.identifier_value limit ${perShed}`).map((r) => r[0]);
      for (const tag of tags) {
        await attempt("weighing", `${name} ${tag}`, async () => {
          const proofId = await proof("operator", { scopeType: "shed", scopeId: locationId, subjectType: "shed", subjectId: locationId });
          await api("POST", `/app/weighing/campaigns/${campaignId}/animal-observations`, {
            campaign_shed_id: campaignShedId, scanned_identifier: tag, weight_kg: Math.round((24 + Math.random() * 14) * 10) / 10,
            proof_artifact_id: proofId, actual_location_id: locationId,
          }, { as: "operator", headers: { "Idempotency-Key": key("wobs", campaignShedId, tag) } });
        });
      }
    }
  }
});

// Feed packing (today packs tomorrow's issued sheet) -> /feed/packing progress, the Work Board feed
// card and feed_packing items in the Verify queue. One completion per pen x session with a packing
// video, exactly as the phone submits it.
feature("feed-packing", async () => {
  const feedDay = addDays(today(), 1);
  const targets = q(`select distinct i.park_id, s.shed_id, s.partition_key, s.session_no, s.workflow
    from feed_direction_issue_session_items s join feed_direction_issues i using (feed_direction_issue_id)
    where i.feed_day='${feedDay}' and i.state='issued' and s.quantity_kg > 0
      -- the server checks the pen against the shed partition catalog; skip lines whose pen the
      -- local catalog does not hold (they would 400 invalid_partition)
      and (s.partition_key = 'whole' and not exists (select 1 from shed_partitions sp where sp.shed_id=s.shed_id and sp.status='active' and coalesce(nullif(btrim(sp.partition_label),''),'whole')<>'whole')
           or exists (select 1 from shed_partitions sp where sp.shed_id=s.shed_id and sp.status='active' and lower(btrim(sp.partition_label))=s.partition_key))
      and not exists (select 1 from feed_packing_completions c where c.shed_id=s.shed_id and c.session_no=s.session_no
        and c.target_date=i.feed_day and c.workflow=s.workflow and c.partition_key=s.partition_key)
    order by 1, 2, 4`);
  if (!targets.length) console.log(`   no unpacked issued lines for ${feedDay}`);
  for (const [parkId, shedId, partitionKey, sessionNo, workflow] of targets) {
    await attempt("feed-packing", `${shedId}/${partitionKey}/s${sessionNo}`, async () => {
      const proofId = await proof("operator", { scopeType: "shed", scopeId: shedId, subjectType: "shed", subjectId: shedId });
      await api("POST", "/feed-direction/packing/complete", {
        park_id: parkId, shed_id: shedId, partition_label: partitionKey === "whole" ? "" : partitionKey,
        session_no: Number(sessionNo), target_date: feedDay, workflow, packing_proof_ref: proofId,
      }, { as: "operator", headers: { "Idempotency-Key": key("pack", shedId, partitionKey, sessionNo, feedDay, workflow) } });
    });
  }
});

// Herd operations raised from the field -> /approvals (birth, death, shifting requests), the
// Work Board and the herd register. Births use real adult females as dams (by RFID); deaths pick a
// male kid; shifting moves two adults between adult pens of the same park ("normal" category).
// Re-runs: the day's requests raised by the seed operator are counted first, and every write has
// a per-day idempotency key, so a second run on the same day adds nothing.
feature("counts", async () => {
  const t = today();
  const already = Number(q(`select count(*) from counts_approval_requests where (raised_at at time zone 'Asia/Kolkata')::date='${t}'
    and raised_by_user_id='${USERS.operator}'`)[0][0]);
  if (already >= 6) return "skip";
  const tag = t.replaceAll("-", "").slice(4);
  for (const [i, parkId] of [PARKS.cbe, PARKS.cpt, PARKS.cbe, PARKS.cpt, PARKS.cbe, PARKS.cpt].entries()) {
    const [dam] = q(`select gi.identifier_value, g.breed, g.shed_id from goats g join goat_identifiers gi on gi.goat_id=g.goat_id and gi.status='active'
      where g.sex='female' and g.lifecycle_status='alive' and g.age_band='Adult' and g.park_id='${parkId}'
      order by md5(g.goat_id::text || '${t}') offset ${i} limit 1`);
    if (!dam) continue;
    const [rfid, breed, shedId] = dam;
    await attempt("counts", `birth ${rfid}`, async () => {
      const proofId = await proof("operator", { scopeType: "shed", scopeId: shedId, subjectType: "shed", subjectId: shedId });
      await api("POST", "/app/counts/birth-events", {
        species: "goat", breed, sex: i % 2 ? "male" : "female", dob: t, entry_date: t, dam_id: rfid, litter_size: 1,
        park_id: parkId, shed_id: shedId, origin_type: "birth", temporary_identifier: `SEED-${tag}-${i + 1}`,
        evidence_refs: [{ evidence_type: "media", evidence_id: proofId }],
      }, { as: "operator", headers: { "Idempotency-Key": key("birth", t, i) } });
    });
  }
  for (const [i, parkId] of [PARKS.cbe, PARKS.cpt].entries()) {
    const [g] = q(`select goat_id, row_version from goats where lifecycle_status='alive' and park_id='${parkId}' and age_band='Kid' and sex='male'
      order by md5(goat_id::text || '${t}') limit 1`);
    if (!g) continue;
    await attempt("counts", `death ${g[0]}`, async () => {
      const proofId = await proof("operator", { scopeType: "goat", scopeId: g[0], subjectType: "goat", subjectId: g[0] });
      await api("POST", "/app/counts/death-events", {
        goat_id: g[0], lifecycle_status: "dead", exit_reason: "died", reason: "Seed preview: found dead in the morning round",
        occurred_at: new Date().toISOString(), evidence_refs: [{ evidence_type: "media", evidence_id: proofId }], row_version: Number(g[1]),
      }, { as: "operator", headers: { "Idempotency-Key": key("death", t, i) } });
    });
  }
  for (const [i, parkId] of [PARKS.cbe, PARKS.cpt, PARKS.cbe].entries()) {
    const pens = q(`select g.shed_id from goats g join locations l on l.location_id=g.shed_id where g.park_id='${parkId}' and g.lifecycle_status='alive'
      and g.age_band='Adult' and l.name not ilike '%quarantine%' and l.name not ilike 'Q%' group by 1 order by count(*) desc limit 4`).map((r) => r[0]);
    if (pens.length < 2) continue;
    const src = pens[i % pens.length], dst = pens[(i + 1) % pens.length];
    const ids = q(`select goat_id from goats where lifecycle_status='alive' and shed_id='${src}' and age_band='Adult'
      order by md5(goat_id::text || '${t}${i}') limit 2`).map((r) => r[0]);
    await attempt("counts", `shift ${src}->${dst}`, async () => {
      const proofId = await proof("operator", { scopeType: "shed", scopeId: src, subjectType: "shed", subjectId: src });
      await api("POST", "/app/counts/shifting-events", {
        source_park_id: parkId, source_shed_id: src, destination_park_id: parkId, destination_shed_id: dst,
        category: "normal", priority: i ? "low" : "high", stage_mode: "keep_current", comment: "Seed preview: rebalance pens",
        proof_ref: proofId, goat_ids: ids,
      }, { as: "operator", headers: { "Idempotency-Key": key("shift", t, i) } });
    });
  }
});

// Milk preparation (one per park per day, for tomorrow's kid feeding) -> /counts/milk-preparation
// and milk items in the Verify queue. Each step video is bound to the park and its step.
feature("milk", async () => {
  const t = today();
  for (const parkId of [PARKS.cbe, PARKS.cpt]) {
    if (Number(q(`select count(*) from milk_preparation_completions where park_id='${parkId}' and preparation_date='${t}'`)[0]?.[0] ?? 0)) { tally("milk", "skip"); continue; }
    await attempt("milk", parkId, async () => {
      const refs = {};
      for (const step of ["goat_milk_quantity", "boiling_temperature", "cooled_temperature", "uht_milk_quantity", "citric_acid_mixing"]) {
        refs[`${step}_proof_ref`] = await proof("operator", { scopeType: "park", scopeId: parkId, subjectType: "other", subjectId: parkId, meta: { milk_preparation_step: step } });
      }
      const goat = 9 + Math.round(Math.random() * 4);
      await api("POST", "/app/counts/milk-preparation/submit", {
        park_id: parkId, preparation_date: t, goat_milk_used: true,
        answers: { morning_milk_collected_litres: goat - 4, evening_milk_collected_litres: 4, goat_milk_quantity_litres: goat, boiling_temperature_c: 95, cooled_temperature_c: 38, uht_milk_quantity_litres: 12, citric_acid_grams: 6 },
        proofs: refs,
      }, { as: "operator", headers: { "Idempotency-Key": key("milk", parkId, t) } });
    });
  }
});

// Vendor register (/procurement/vendors = buying side, /sales/vendors = selling side; the side is
// decided by record type). Skips any business name already on the register.
feature("vendors", async () => {
  const have = new Set(q(`select lower(business_name) from procurement_vendors`).map((r) => r[0]));
  const buy = [
    ["Goat Vendor", "Sri Murugan Goat Traders", "Murugan K", "Tamil Nadu", "Erode", "active", { breed: "Sojat", filtered_stock: 40, price_per_goat: "9500", eta_after_order_days: 5 }],
    ["Goat Vendor", "Anantapur Livestock Co", "Ramesh Reddy", "Andhra Pradesh", "Anantapur", "active", { breed: "Anantapur sheep", filtered_stock: 60, price_per_goat: "7800", eta_after_order_days: 7 }],
    ["Goat Vendor", "Rajasthan Sojat Farms", "Mahendra Singh", "Rajasthan", "Sojat", "negotiating", { breed: "Sojat", filtered_stock: 120, price_per_goat: "11200", eta_after_order_days: 12 }],
    ["Goat Vendor", "Kolar Beetal Breeders", "Nagaraj", "Karnataka", "Kolar", "active", { breed: "Beetal", filtered_stock: 25, price_per_goat: "10400", eta_after_order_days: 4 }],
    ["Goat Vendor", "Osmanabad Goat Collective", "Suresh Patil", "Maharashtra", "Osmanabad", "inactive", { breed: "Osmanabadi", filtered_stock: 0, price_per_goat: "8900", eta_after_order_days: 9 }],
    ["Feed Supplier", "Green Valley Fodder", "Senthil", "Tamil Nadu", "Coimbatore", "active", { feed: "Green fodder", capacity_quantity: "5", capacity_unit: "tonnes", supply_frequency: "per_week" }],
    ["Feed Supplier", "Channapatna Maize Traders", "Manjunath", "Karnataka", "Channapatna", "active", { feed: "Maize", capacity_quantity: "200", capacity_unit: "bags", supply_frequency: "per_2_weeks" }],
    ["Feed Supplier", "Deccan Mineral Mix", "Farooq", "Telangana", "Hyderabad", "active", { feed: "Mineral mixture", capacity_quantity: "40", capacity_unit: "bags", supply_frequency: "per_month" }],
    ["Feed Supplier", "Pollachi Groundnut Cake", "Kannan", "Tamil Nadu", "Pollachi", "negotiating", { feed: "Groundnut cake", capacity_quantity: "2", capacity_unit: "tonnes", supply_frequency: "per_2_weeks" }],
    ["Medicine Supplier", "VetCare Pharma", "Dr. Anitha", "Karnataka", "Bengaluru", "active", {}],
    ["Medicine Supplier", "Kovai Animal Health", "Prakash", "Tamil Nadu", "Coimbatore", "active", {}],
    ["Transport", "Sai Livestock Transport", "Venkatesh", "Karnataka", "Ramanagara", "active", {}],
    ["Transport", "KPN Goods Carriers", "Balu", "Tamil Nadu", "Salem", "banned", { comments: "Two late deliveries in August." }],
    ["Labour Contractor", "Mandya Farm Labour Services", "Shivanna", "Karnataka", "Mandya", "active", {}],
  ];
  const sell = [
    ["Agent", "Bengaluru Meat Agents", "Imran", "Karnataka", "Bengaluru", "active"],
    ["Agent", "Chennai Livestock Brokers", "Rafiq", "Tamil Nadu", "Chennai", "active"],
    ["Butcher", "Al-Noor Mutton Stall", "Salim", "Karnataka", "Mysuru", "active"],
    ["Butcher", "Kovai Fresh Mutton", "Arumugam", "Tamil Nadu", "Coimbatore", "active"],
    ["Butcher", "Hosur Meat Centre", "Basha", "Tamil Nadu", "Hosur", "negotiating"],
    ["Company", "FreshCuts Foods Pvt Ltd", "Neha Sharma", "Karnataka", "Bengaluru", "active"],
    ["Company", "Licious Procurement Desk", "Arjun", "Karnataka", "Bengaluru", "negotiating"],
    ["Farmer", "Tiruppur Goat Rearers Group", "Palanisamy", "Tamil Nadu", "Tiruppur", "active"],
    ["Farmer", "Kanakapura Farmers FPO", "Gowda", "Karnataka", "Kanakapura", "active"],
    ["Slaughter House", "Deonar Abattoir Buyers", "Iqbal", "Maharashtra", "Mumbai", "inactive"],
    ["Slaughter House", "Tumakuru Municipal Abattoir", "Srinivas", "Karnataka", "Tumakuru", "active"],
    ["Agent", "Madurai Market Agents", "Selvam", "Tamil Nadu", "Madurai", "active"],
  ].map((r) => [...r, {}]);
  for (const [i, [record_type, business_name, contact_person_name, state, city, status, extra]] of [...buy, ...sell].entries()) {
    if (have.has(business_name.toLowerCase())) { tally("vendors", "skip"); continue; }
    await attempt("vendors", business_name, () => api("POST", "/procurement/vendors", {
      record_type, business_name, contact_person_name, phone_number: `98${String(45000000 + i * 7919).padStart(8, "0")}`,
      status, state, city, ...extra,
    }, { as: "ceo", headers: { "Idempotency-Key": key("vendor", business_name) } }));
  }
});

// Alerts: one event rule per event kind the backend offers (death critical, the rest warning), so
// /alerts has rules to evaluate beyond the two built-in feed checks.
feature("alerts", async () => {
  const cfg = await get("/alerts/config");
  const have = new Set((cfg.event_rules ?? []).map((r) => r.kind));
  for (const k of cfg.event_kinds ?? []) {
    if (have.has(k.key)) { tally("alerts", "skip"); continue; }
    await attempt("alerts", k.key, () => api("POST", "/alerts/config/events", {
      label: k.label, kind: k.key, severity: k.key === "death_recorded" ? "critical" : "warning", enabled: true,
    }, { headers: { "Idempotency-Key": key("alert-rule", k.key) } }));
  }
});

// Market survey (/sales/market-analytics): cities to phone, the prices asked, and one survey per
// city per day for the last 7 days (the API refuses a business_date older than 7 days).
feature("market", async () => {
  const cities = ["Bengaluru", "Chennai", "Coimbatore", "Mysuru", "Hyderabad"];
  const questions = [["Live goat price", "₹/kg"], ["Mutton retail price", "₹/kg"], ["Weaned kid price", "₹/head"]];
  const haveC = new Map(q(`select lower(name), id from market_cities`).map((r) => [r[0], r[1]]));
  const haveQ = new Map(q(`select lower(label), id from market_questions`).map((r) => [r[0], r[1]]));
  for (const c of cities) if (!haveC.has(c.toLowerCase())) await attempt("market", c, async () => {
    const { data } = await api("POST", "/market/cities", { name: c, status: "active" }, { headers: { "Idempotency-Key": key("mcity", c) } });
    haveC.set(c.toLowerCase(), data.id ?? data.city_id ?? data.city?.id);
  });
  for (const [label, unit] of questions) if (!haveQ.has(label.toLowerCase())) await attempt("market", label, async () => {
    const { data } = await api("POST", "/market/questions", { label, unit_label: unit, status: "active" }, { headers: { "Idempotency-Key": key("mq", label) } });
    haveQ.set(label.toLowerCase(), data.id ?? data.question_id ?? data.question?.id);
  });
  const base = { "live goat price": 520, "mutton retail price": 860, "weaned kid price": 4200 };
  const done = new Set(q(`select distinct city_id || '|' || business_date from market_price_entries`).map((r) => r[0]));
  for (let d = 7; d >= 0; d--) {
    const day = addDays(today(), -d);
    for (const [ci, c] of cities.entries()) {
      const cityId = haveC.get(c.toLowerCase());
      if (!cityId || done.has(`${cityId}|${day}`)) continue;
      const answers = questions.map(([label]) => ({ question_id: haveQ.get(label.toLowerCase()), price: Math.round(base[label.toLowerCase()] * (1 + ci * 0.03 + Math.sin(d / 2 + ci) * 0.04)) })).filter((a) => a.question_id);
      await attempt("market", `${c} ${day}`, () => api("POST", `/app/market/survey/${cityId}`, { business_date: day, answers }, { as: "operator", headers: { "Idempotency-Key": key("msurvey", cityId, day) } }));
    }
  }
});

// Leave (/leave): requests from the three people who can sign in (a leave cannot start in the
// past), some approved, one rejected, the rest waiting.
feature("leave", async () => {
  const t = today();
  const rows = [["operator", 3, 4, "Family function in native village", "approve"], ["operator", 12, 12, "Medical check-up", null], ["director", 7, 9, "Vet training programme", null], ["ceo", 20, 21, "Personal travel", null], ["director", 1, 1, "Bank work in town", "approve"], ["operator", 2, 2, "Bike repair", "reject"]];
  for (const [who, from, to, reason, decision] of rows) {
    await attempt("leave", `${who} ${reason}`, async () => {
      const exists = q(`select count(*) from workforce_leave_requests where reason='${reason.replaceAll("'", "''")}'`)[0][0];
      if (Number(exists)) return "skip";
      const { data } = await api("POST", "/app/leave/requests", { idempotency_key: key("leave", who, reason), starts_on: addDays(t, from), ends_on: addDays(t, to), reason }, { as: who });
      const id = data?.leave_request_id ?? data?.request?.leave_request_id;
      if (decision && id) await api("POST", `/app/leave/approvals/${id}/${decision}`, decision === "reject" ? { reason: "Harvest week, please pick another day" } : {}, { as: who === "ceo" ? "director" : "ceo", headers: { "Idempotency-Key": key("leave-decide", id) } });
    });
  }
});

// Health: diseases with a published treatment protocol per age band (/health/config), then cases
// opened on real animals over the last three weeks and some closed (/health/analytics).
const DISEASES = [
  // [name, duration days, [[day, session, medicine, dose per kg (or per animal when denominator none), route, denominator]]]
  ["Pneumonia", 5, [[1, "morning", "Oxytetracycline", "0.1", "IM", "kg"], [3, "morning", "Meloxicam", "0.05", "IM", "kg"]]],
  ["Diarrhoea", 3, [[1, "morning", "Sulfadimidine", "5", "Oral", "kg"], [2, "evening", "Probiotic powder", "10", "Oral", "none"]]],
  ["Foot rot", 7, [[1, "morning", "Zinc sulphate", "10", "Topical", "none"], [4, "morning", "Penicillin", "0.1", "IM", "kg"]]],
  ["Bloat", 2, [[1, "unscheduled", "Anti-bloat oil", "100", "Oral", "none"]]],
  ["Tick infestation", 3, [[1, "morning", "Ivermectin", "0.02", "SQ", "kg"]]],
]
feature("health", async () => {
  // Protocol steps may only name medicines on the item register (Configuration > Items).
  const medCat = q(`select category_id from item_categories where name='Medicines' limit 1`)[0]?.[0];
  const haveItems = new Set(q(`select lower(name) from inventory_items where category='medicine'`).map((r) => r[0]));
  const routeKey = { IM: "im", SQ: "sc", Oral: "oral", Topical: "topical" };
  for (const [, , steps] of DISEASES) for (const [, , med, , route] of steps) {
    if (!medCat || haveItems.has(med.toLowerCase())) continue;
    haveItems.add(med.toLowerCase());
    await attempt("health", `item ${med}`, () => api("POST", "/admin/configuration/items", {
      fields: { name: med, category_id: medCat, unit: route === "Oral" || route === "Topical" ? "g" : "ml", route: routeKey[route], withdrawal_days: 7 },
    }, { headers: { "Idempotency-Key": key("item", med) } }));
  }
  for (const [name, days, steps] of DISEASES) {
    await attempt("health", `disease ${name}`, async () => {
      const k = name.toLowerCase().replace(/\W+/g, "_");
      if (Number(q(`select count(*) from health_protocol_versions where disease_key='${k}' and status='published'`)[0][0])) return "skip";
      const { data } = await api("POST", "/health-config/diseases", { display_name: name, duration_days: days }, { headers: { "Idempotency-Key": key("disease", name) } }).catch((e) => {
        if (e.status === 409) return { data: null }; throw e;
      });
      const diseaseKey = data?.disease_key ?? k;
      for (const age of ["adult", "kid"]) {
        const draftBody = {
          disease_key: diseaseKey, age_band: age, display_name: name, duration_days: days,
          // every day of the course needs a step: medication days as authored, check-up actions between
          steps: Array.from({ length: days }, (_, d) => d + 1).flatMap((day) => {
            const meds = steps.filter((st) => st[0] === day);
            return meds.length
              ? meds.map(([day_no, session, medicine_name, dosage_text, medicine_route, dosage_denominator]) => ({ day_no, session, record_type: "medication", medicine_name, dosage_text, dosage_denominator, medicine_route }))
              : [{ day_no: day, session: "morning", record_type: "action", instruction: "Check temperature, appetite and droppings; record in the case." }];
          }),
        };
        const saved = (await api("POST", "/health-config/drafts/save", draftBody, { headers: { "Idempotency-Key": key("draft", JSON.stringify(draftBody)) } })).data;
        const versionId = saved?.protocol_version_id ?? saved?.version_id ?? data?.draft_version_ids?.[age];
        if (versionId) await api("POST", `/health-config/protocols/${versionId}/publish`, {}, { headers: { "Idempotency-Key": key("publish", versionId) } });
      }
    });
  }
  const t = today();
  if (Number(q(`select count(*) from health_cases`)[0][0]) >= 18) return;
  // The case check compares the goat's stored age_band verbatim with "adult"/"kid"; locally most
  // rows carry "Adult"/"Kid", so only exact lowercase rows can open a case.
  const goats = q(`select goat_id, age_band from goats where lifecycle_status='alive' and age_band in ('adult','kid') order by md5(goat_id::text) limit 24`);
  for (const [i, [goatId, band]] of goats.entries()) {
    const disease = DISEASES[i % DISEASES.length][0].toLowerCase().replace(/\W+/g, "_");
    await attempt("health", `case ${goatId}`, () => api("POST", "/app/health/cases", {
      goat_id: goatId, disease_key: disease, age_band: band === "kid" ? "kid" : "adult", start_date: addDays(t, -(i % 21)),
    }, { as: "operator", headers: { "Idempotency-Key": key("hcase", goatId, disease) } }));
  }
});

// Pen routines (/routines): standing pen checks per park, owned by a director / CXO (only park
// heads, directors and CXOs are assignable). The daily task instances are raised by
// the kernel worker's pen-routine stage; this seeds the definitions it reads.
feature("routines", async () => {
  const have = new Set(q(`select lower(name) || '|' || park_id from pen_routine_definitions`).map((r) => r[0]));
  const yesNo = (id, title, proof) => ({ id, kind: "yes_no", title, required: true, ...(proof ? { proof: { kind: proof, count: "single" } } : {}) });
  const defs = [
    ["Water trough check", "daily", {}, "Check every trough is clean and full.", [yesNo("clean", "Trough clean?", "photo"), yesNo("full", "Water full?")], { min: 1, max: 2 }, { min: 0, max: 0 }, "verifier"],
    ["Bedding and floor check", "weekly", { weekdays: [1, 4] }, "Look for wet bedding and broken slats.", [yesNo("dry", "Bedding dry?"), { id: "repairs", kind: "text", title: "Repairs needed", required: false }], { min: 1, max: 3 }, { min: 0, max: 0 }, "verifier"],
    ["Evening headcount", "daily", {}, "Count animals in each pen before lock-up.", [{ id: "count", kind: "number", title: "Animals counted", required: true, min: 0, max: 500, unit: "animals" }], { min: 0, max: 0 }, { min: 1, max: 1 }, "verifier"],
    ["Mineral lick refill", "every_n_days", { interval_days: 7 }, "Refill mineral licks in every occupied pen.", [yesNo("refilled", "Lick refilled?", "photo")], { min: 1, max: 1 }, { min: 0, max: 0 }, "none"],
    ["Fly trap and fogging", "monthly", { month_days: [1, 15] }, "Replace fly traps; fog pen corners.", [yesNo("traps", "Traps replaced?"), yesNo("fogged", "Pen fogged?", "video")], { min: 0, max: 2 }, { min: 1, max: 1 }, "verifier"],
  ];
  for (const parkId of [PARKS.cbe, PARKS.cpt]) {
    for (const [name, cadence_kind, cadence, instruction, questions, photo, video, review_kind] of defs) {
      if (have.has(`${name.toLowerCase()}|${parkId}`)) { tally("routines", "skip"); continue; }
      await attempt("routines", `${name} ${parkId}`, () => api("POST", "/admin/pen-routines", {
        park_id: parkId, name, instruction, scope_kind: "all_pens", occupied_only: true, cadence_kind, ...cadence,
        start_date: addDays(today(), -14), due_offset_days: 0, notify_time: "07:30", review_kind,
        evidence: { questions, photo, video, presence: "required" }, assignee_user_id: parkId === PARKS.cbe ? USERS.director : USERS.ceo,
      }, { headers: { "Idempotency-Key": key("routine", name, parkId) } }));
    }
  }
});

// Sales deals over the last four weeks (/sales/sold, buyer analytics, farm-born sold-in-period),
// to the selling-side vendors seeded above. Skips sale dates already carrying a seed deal.
feature("sales", async () => {
  const buyers = q(`select vendor_id, business_name, coalesce(city,'') from procurement_vendors where record_type in ('Agent','Butcher','Company','Farmer','Slaughter House') and status='active' order by business_name`);
  if (!buyers.length) return "skip";
  const seeded = new Set(q(`select sale_date::text || '|' || buyer_name from sales_deals where comments like 'Seed preview%'`).map((r) => r[0]));
  const lines = [["Goat", "Sojat", 6, 34, 520], ["Sheep", "Anantapur", 10, 27, 470], ["Goat", "Malai", 4, 31, 500], ["Goat", "Beetal", 3, 38, 540], ["Manure", "Manure", null, null, 3]];
  for (let i = 0; i < 14; i++) {
    const day = addDays(today(), -(i * 2));
    const [vendorId, name, city] = buyers[i % buyers.length];
    if (seeded.has(`${day}|${name}`)) { tally("sales", "skip"); continue; }
    const [product_type, breed, count, avg, rate] = lines[i % lines.length];
    const weight = count ? Math.round(count * avg * (0.95 + (i % 4) * 0.03)) : 2000 + i * 150;
    const value = Math.round(weight * rate);
    const status = i % 7 === 3 ? "In Discussion" : i % 7 === 5 ? "Deal Failed" : "Deal Closed";
    await attempt("sales", `${day} ${name}`, () => api("POST", "/sales/deals", {
      sale_date: day, farm: i % 2 ? "CPT" : "CBE", buyer_name: name, buyer_place: city, buyer_vendor_id: vendorId,
      lines: [{ product_type, breed, ...(count ? { animal_count: count, male_count: Math.ceil(count / 2), female_count: Math.floor(count / 2), quantity: count } : { quantity: weight }), total_weight_kg: weight, rate_per_unit: rate, sales_value: value }],
      sales_value: value,
      status, comments: "Seed preview deal", stock_shortfall_acknowledged: true,
    }, { as: "ceo", headers: { "Idempotency-Key": key("deal", day, name) } }));
  }
});

// Sale allocations: tag farm-born animals of the deal's breed to the three most recent seeded goat
// deals (/sales/farm-born "Tagged, sale not closed", the sale workflow on /sales/sops).
feature("sale-allocations", async () => {
  const deals = q(`select d.id, d.farm, d.breed, d.animal_count from sales_deals d where d.comments like 'Seed preview%'
    and d.product_type='Goat' and d.status='Deal Closed' and d.animal_count > 0
    and not exists (select 1 from goat_sale_allocations a where a.sales_deal_id=d.id) order by d.sale_date desc limit 3`);
  for (const [dealId, farm, breed, count] of deals) {
    const parkId = farm === "CPT" ? PARKS.cpt : PARKS.cbe;
    const ids = q(`select g.goat_id from goats g where g.lifecycle_status='alive' and g.origin_type='birth' and g.park_id='${parkId}' and g.breed='${breed}'
      and not exists (select 1 from goat_sale_allocations a where a.goat_id=g.goat_id) order by g.display_id limit ${Math.round(Number(count))}`).map((r) => r[0]);
    if (!ids.length) continue;
    const animal_weights_kg = Object.fromEntries(ids.map((id, i) => [id, String(30 + (i % 5) * 1.5)]));
    await attempt("sale-allocations", dealId, () => api("POST", "/admin/goats/sale-allocations/confirm", { sales_deal_id: dealId, goat_ids: ids, animal_weights_kg, reason: "Seed preview allocation" },
      { as: "ceo", headers: { "Idempotency-Key": key("alloc", dealId) } }));
  }
});

// Feed purchases for the last week (/procurement/feed-purchases, feed stock), from the feed
// suppliers on the register; older loads delivered, the last two still in transit.
feature("feed-purchases", async () => {
  const items = q(`select feed_item_label, max(per_kg_cost) from feed_purchases where feed_item_label is not null group by 1 order by count(*) desc limit 5`);
  const vendors = q(`select business_name from procurement_vendors where record_type='Feed Supplier' and status='active' order by 1`).map((r) => r[0]);
  if (!items.length || !vendors.length) return "skip";
  const seeded = new Set(q(`select purchase_date::text || '|' || feed_item_label || '|' || farm_label from feed_purchases where recorded_by is not null and purchase_date >= current_date - 8`).map((r) => r[0]));
  for (let i = 0; i < 8; i++) {
    const day = addDays(today(), -Math.floor(i * 0.9));
    const [item, perKg] = items[i % items.length];
    const farm = i % 2 ? "CPT" : "CBE";
    if (seeded.has(`${day}|${item}|${farm}`)) { tally("feed-purchases", "skip"); continue; }
    const qty = 1500 + (i % 4) * 900;
    const cost = Math.round(qty * Number(perKg || 12));
    const reached = i >= 2;
    await attempt("feed-purchases", `${day} ${item} ${farm}`, () => api("POST", "/procurement/feed-purchases", {
      purchase_date: day, farm, feed_item: item, quantity_kg: qty, feed_cost: cost, transport_cost: 2500, loading_cost: 400, unloading_cost: 400,
      vendor: vendors[i % vendors.length], payment_status: i % 3 ? "Pending" : "Paid", ...(i % 3 ? {} : { payment_released: cost + 3300 }),
      days_of_stock: 10, ...(reached ? { reached_on: addDays(day, 1) > today() ? today() : addDays(day, 1), reached_weight_kg: qty - 20 } : {}),
    }, { as: "ceo", headers: { "Idempotency-Key": key("feedbuy", day, item, farm) } }));
  }
});

// Decide some pending approvals so /approvals shows every status, leaving at least 8 pending
// (the default view is Pending). Oldest first; one in three rejected.
feature("approvals", async () => {
  const rows = q(`select approval_request_id, request_type from counts_approval_requests where status='pending' order by raised_at`);
  const decide = Math.max(0, rows.length - 8);
  for (const [i, [id, type]] of rows.slice(0, decide).entries()) {
    const reject = i % 3 === 1;
    await attempt("approvals", `${type} ${id}`, () => api("POST", `/app/counts/approvals/${id}/${reject ? "reject" : "approve"}`,
      reject ? { reason: "Seed preview: evidence unclear, raise again with a clearer video" } : {},
      { as: "ceo", headers: { "Idempotency-Key": key("approval", id) } }));
  }
});

// Feed distribution for the latest issued feed day up to today -> /feed/direction execution and
// feed_distribution items in the Verify queue. Feed-weight photo + distribution and water videos.
feature("feed-distribution", async () => {
  const [latest] = q(`select max(feed_day)::text from feed_direction_issues where state='issued' and feed_day <= '${today()}'`);
  const feedDay = latest?.[0];
  if (!feedDay) return "skip";
  const targets = q(`select distinct i.park_id, s.shed_id, s.session_no, s.workflow
    from feed_direction_issue_session_items s join feed_direction_issues i using (feed_direction_issue_id)
    where i.feed_day='${feedDay}' and i.state='issued' and s.quantity_kg > 0 and s.partition_key='whole'
      and not exists (select 1 from shed_partitions sp where sp.shed_id=s.shed_id and sp.status='active' and coalesce(nullif(btrim(sp.partition_label),''),'whole')<>'whole')
      and not exists (select 1 from feed_distribution_completions c where c.shed_id=s.shed_id and c.session_no=s.session_no
        and c.target_date=i.feed_day and c.workflow=s.workflow and c.partition_key='whole')
    order by 1, 2, 3 limit ${Number(args["distribution-limit"] ?? 12)}`);
  for (const [parkId, shedId, sessionNo, workflow] of targets) {
    await attempt("feed-distribution", `${shedId}/s${sessionNo}`, async () => {
      const shot = (kind) => proof("operator", { scopeType: "shed", scopeId: shedId, subjectType: "shed", subjectId: shedId, kind });
      await api("POST", "/feed-direction/distribution/complete", {
        park_id: parkId, shed_id: shedId, partition_label: "", session_no: Number(sessionNo), target_date: feedDay, workflow,
        feed_weight_proof_ref: await shot("photo"), distribution_proof_ref: await shot("video"), water_proof_ref: await shot("video"),
      }, { as: "operator", headers: { "Idempotency-Key": key("dist", shedId, sessionNo, feedDay, workflow) } });
    });
  }
});

// Verdicts: decide part of today's pending queue so Verify shows all three states (to verify,
// accepted, rejected). Roughly 1 in 3 approved, 1 in 5 sent back, the rest left to verify.
feature("verdicts", async () => {
  const pending = q(`select item_id, row_version, category, measurement_fields::text from verification_items
    where status='pending' and (captured_at at time zone 'Asia/Kolkata')::date = '${today()}' order by created_at`);
  const decided = q(`select count(*) from verification_items where status in ('approved','rejected')
    and (verified_at at time zone 'Asia/Kolkata')::date = '${today()}'`)[0][0];
  const want = Math.max(0, Math.floor((pending.length + Number(decided)) * 0.5) - Number(decided));
  const reasons = ["Video does not show the full bag being packed.", "Pen name not visible in the clip.", "Clip too short to see the weight."];
  for (let i = 0; i < want && i < pending.length; i++) {
    const [itemId, rv, , fieldsJson] = pending[i];
    const reject = i % 3 === 2;
    // Items that carry measurement fields (feed packing: kg per feed item) need the verifier's
    // reading before an approval; record a plausible one.
    const fields = JSON.parse(fieldsJson || "[]");
    const entries = fields.map((f, j) => ({ key: f.key, value: Math.round((2 + ((i + j) % 5) * 0.8) * 10) / 10 }));
    const measurement = entries.length ? { value: Math.round(entries.reduce((a, e) => a + e.value, 0) * 10) / 10, entries, variance_acknowledged: true } : undefined;
    await attempt("verdicts", itemId, () => api("POST", `/verification/items/${itemId}/verdict`, {
      decision: reject ? "rejected" : "approved", row_version: Number(rv),
      ...(reject ? { reason: reasons[i % reasons.length] } : measurement ? { measurement } : {}),
    }, { as: "ceo", headers: { "Idempotency-Key": key("verdict", itemId) } }));
  }
});

// ---- main --------------------------------------------------------------------------------------
export async function main() {
  console.log(`seed-preview-data: api=${API} tenant=${TENANT} today=${today()}${DRY ? " (dry run)" : ""}`);
  for (const f of features) {
    if (ONLY && !ONLY.has(f.name)) continue;
    const t0 = Date.now();
    console.log(`== ${f.name}`);
    try { await f.fn(); } catch (e) { tally(f.name, "fail"); console.warn(`  ! ${f.name}: ${e.message}`); }
    const s = stats[f.name] ?? { ok: 0, skip: 0, fail: 0 };
    console.log(`   ${f.name}: ok=${s.ok} skip=${s.skip} fail=${s.fail} (${((Date.now() - t0) / 1000).toFixed(1)}s)`);
  }
  const failed = Object.values(stats).reduce((a, s) => a + s.fail, 0);
  console.log(`seed-preview-data: done, ${failed} failed writes`);
  if (failed && args.strict) process.exit(1);
}

await main();
