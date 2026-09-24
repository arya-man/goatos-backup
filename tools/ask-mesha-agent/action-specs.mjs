// Ask Mesha CEO write actions: one entry per action the chat may PROPOSE.
// Each spec: describe (for the model), risk (normal|high; high = typed CONFIRM), schema (zod,
// the model's params), prepare(params, resolver, ctx) -> {title, summary[], request:{method,path,body}}.
// prepare only READS (resolver = read-only SQL lookups); nothing here writes. The request bodies
// mirror the backend handlers on origin/main: see docs/agent-rules/ask-mesha.md "CEO actions".
// Scope is Ravi's decision (2026-09-24): pc-care, weighing, vaccination dates/config, leadership
// tasks, SOP tasks + SOP definitions (no delete), shifting approvals only. Nothing else.
import { z } from "zod";

const date = z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "use YYYY-MM-DD");
const rfc3339 = z.string().refine((s) => /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})$/.test(s) && !Number.isNaN(Date.parse(s)), "use RFC3339 with offset, e.g. 2026-09-30T17:00:00+05:30");
const uuid = z.string().uuid();
const reason = z.string().trim().min(3, "give a reason").max(500);
const park = z.string().trim().min(2).describe("park name or code: Coimbatore/CBE, Channapatna/CPT");
const pen = z.string().trim().min(1).max(60).describe("pen code or name, e.g. G1P3 or Godel 1 Part 3 or Castro 1");
const person = z.string().trim().min(2).max(80).describe("person's name as in the app");

const PC_CATEGORIES = ["deworming", "anti_protozoan", "ticks_removal", "hoof_trimming", "hair_trimming"];
const PC_ROUND_CATEGORIES = ["deworming", "ticks_removal", "hoof_trimming", "hair_trimming"];
const label = (c) => c.replace(/_/g, " ");
const penList = (ps) => ps.map((p) => p.pen_name).join(", ");
const today = () => new Date(Date.now() + 5.5 * 3600e3).toISOString().slice(0, 10); // IST business date

function mustBeFuture(d, what) {
  if (d < today()) throw new Error(`${what} ${d} is in the past (today is ${today()} IST).`);
}

export const ACTION_SPECS = {
  // ---------------- PC-care (backend/internal/pccare) ----------------
  pc_care_plan_round: {
    describe: "Plan a PC-care round (deworming/ticks_removal/hoof_trimming/hair_trimming) across several pens of one park on one date, assigned to one or more people.",
    risk: "normal",
    schema: z.object({
      category: z.enum(PC_ROUND_CATEGORIES), park, pens: z.array(pen).min(1).max(100),
      date, assignees: z.array(person).min(1).max(10),
      feed_removal_required: z.boolean().optional(), removal_operators: z.array(person).max(10).optional(),
    }).strict(),
    async prepare(p, r) {
      mustBeFuture(p.date, "Planned date");
      const pk = await r.park(p.park);
      const pens = await r.pens(pk.park_id, p.pens);
      const people = await Promise.all(p.assignees.map((n) => r.person(n, pk.park_id)));
      const removal = await Promise.all((p.removal_operators || []).map((n) => r.person(n, pk.park_id)));
      return {
        title: `Plan ${label(p.category)} round: ${pens.length} pen${pens.length > 1 ? "s" : ""}, ${pk.name}, ${p.date}`,
        summary: [`Pens: ${penList(pens)}`, `Assigned to: ${people.map((x) => x.name).join(", ")}`,
          ...(p.feed_removal_required === undefined ? [] : [`Feed & water removal: ${p.feed_removal_required ? "yes" : "no"}${removal.length ? ` (${removal.map((x) => x.name).join(", ")})` : ""}`])],
        request: { method: "POST", path: "/app/pc-care/rounds", body: {
          category: p.category, park_id: pk.park_id, pens: pens.map((x) => ({ shed_id: x.shed_id, partition_label: x.partition_label })),
          planned_business_date: p.date, assignee_user_ids: people.map((x) => x.user_id),
          ...(p.feed_removal_required === undefined ? {} : { feed_removal_required: p.feed_removal_required }),
          ...(removal.length ? { removal_operator_user_ids: removal.map((x) => x.user_id) } : {}),
        } },
      };
    },
  },
  pc_care_plan_task: {
    describe: "Plan a single-pen PC-care task (deworming/anti_protozoan/ticks_removal/hoof_trimming/hair_trimming).",
    risk: "normal",
    schema: z.object({ category: z.enum(PC_CATEGORIES), park, pen, date, assignees: z.array(person).min(1).max(10),
      feed_removal_required: z.boolean().optional(), removal_operators: z.array(person).max(10).optional() }).strict(),
    async prepare(p, r) {
      mustBeFuture(p.date, "Planned date");
      const pk = await r.park(p.park);
      const [pn] = await r.pens(pk.park_id, [p.pen]);
      const people = await Promise.all(p.assignees.map((n) => r.person(n, pk.park_id)));
      const removal = await Promise.all((p.removal_operators || []).map((n) => r.person(n, pk.park_id)));
      return {
        title: `Plan ${label(p.category)}: ${pn.pen_name}, ${pk.name}, ${p.date}`,
        summary: [`Assigned to: ${people.map((x) => x.name).join(", ")}`],
        request: { method: "POST", path: "/app/pc-care/tasks", body: {
          category: p.category, park_id: pk.park_id, shed_id: pn.shed_id, partition_label: pn.partition_label,
          planned_business_date: p.date, assignee_user_ids: people.map((x) => x.user_id),
          ...(p.feed_removal_required === undefined ? {} : { feed_removal_required: p.feed_removal_required }),
          ...(removal.length ? { removal_operator_user_ids: removal.map((x) => x.user_id) } : {}),
        } },
      };
    },
  },
  pc_care_close_task: {
    describe: "Close (cancel) one PC-care pen task with a reason. task_id from public.pc_care_tasks.",
    risk: "normal",
    schema: z.object({ task_id: uuid, reason }).strict(),
    async prepare(p, r) {
      const t = await r.pcTask(p.task_id);
      if (["completed", "closed"].includes(t.status)) throw new Error(`That task is already ${t.status}.`);
      return { title: `Close ${label(t.category)} task: ${t.pen_name}, ${t.planned_business_date}`, summary: [`Reason: ${p.reason}`, `Current status: ${t.status}`],
        request: { method: "POST", path: `/app/pc-care/tasks/${t.task_id}/close`, body: { reason: p.reason } } };
    },
  },
  pc_care_reopen_task: {
    describe: "Reopen a closed PC-care pen task.",
    risk: "normal",
    schema: z.object({ task_id: uuid }).strict(),
    async prepare(p, r) {
      const t = await r.pcTask(p.task_id);
      return { title: `Reopen ${label(t.category)} task: ${t.pen_name}, ${t.planned_business_date}`, summary: [`Current status: ${t.status}`],
        request: { method: "POST", path: `/app/pc-care/tasks/${t.task_id}/reopen`, body: {} } };
    },
  },
  pc_care_close_round: {
    describe: "Close (cancel) a whole PC-care round (all its pens) with a reason. round_id from public.pc_care_rounds.",
    risk: "high",
    schema: z.object({ round_id: uuid, reason }).strict(),
    async prepare(p, r) {
      const rd = await r.pcRound(p.round_id);
      return { title: `Close ${label(rd.category)} round: ${rd.park_name}, ${rd.planned_business_date} (${rd.pen_count} pens)`, summary: [`Reason: ${p.reason}`, `Open pens: ${rd.open_count}`],
        request: { method: "POST", path: `/app/pc-care/rounds/${rd.round_id}/close`, body: { reason: p.reason } } };
    },
  },

  // ---------------- Weighing (backend/internal/weighing) ----------------
  weighing_create_plan: {
    describe: "Create a DRAFT weighing plan for one park on one date: pens, weighing operator, feed & water removal operator. Not published.",
    risk: "normal",
    schema: z.object({ park, date, pens: z.array(pen).min(1).max(100), operator: person, removal_operator: person.optional(),
      feed_water_removal: z.boolean().optional(), per_pen_weighing: z.boolean().optional().describe("true = one weight per pen (per_shed_partition); default individual animals"),
      planned_cap_per_day: z.number().int().min(1).max(5000).optional() }).strict(),
    async prepare(p, r) {
      mustBeFuture(p.date, "Weighing date");
      const pk = await r.park(p.park);
      const pens = await r.pens(pk.park_id, p.pens);
      const op = await r.person(p.operator, pk.park_id);
      const rop = p.removal_operator ? await r.person(p.removal_operator, pk.park_id) : null;
      const cat = p.per_pen_weighing ? "per_shed_partition" : "individual_animal";
      return {
        title: `Draft weighing plan: ${pk.name}, ${p.date}, ${pens.length} pen${pens.length > 1 ? "s" : ""}`,
        summary: [`Pens: ${penList(pens)}`, `Weighing operator: ${op.name}`, ...(rop ? [`Feed & water removal: ${rop.name}`] : []), "Stays a draft until published."],
        request: { method: "POST", path: "/weighing/campaigns", body: {
          park_id: pk.park_id, period_start_date: p.date, period_end_date: p.date, start_business_date: p.date,
          planned_cap_per_day: p.planned_cap_per_day ?? 200, operator_user_id: op.user_id,
          ...(rop ? { fasting_operator_user_id: rop.user_id } : {}),
          ...(p.feed_water_removal === undefined ? {} : { feed_water_removal_requested: p.feed_water_removal }),
          sheds: pens.map((x) => ({ location_id: x.shed_id, location_type: "shed", display_name: x.pen_name, partition_label: x.partition_label, weighing_category: cat })),
        } },
      };
    },
  },
  weighing_edit_plan: {
    describe: "Edit a weighing plan: move its date, change the weighing/removal operator, add or remove pens. Unchanged fields keep their current values.",
    risk: "normal",
    schema: z.object({ campaign_id: uuid, date: date.optional(), operator: person.optional(), removal_operator: person.optional(),
      add_pens: z.array(pen).max(100).optional(), remove_pens: z.array(pen).max(100).optional() }).strict()
      .refine((p) => p.date || p.operator || p.removal_operator || p.add_pens?.length || p.remove_pens?.length, "nothing to change"),
    async prepare(p, r) {
      const c = await r.campaign(p.campaign_id);
      if (["completed", "canceled"].includes(c.status)) throw new Error(`That weighing plan is ${c.status}; it can't be edited.`);
      const d = p.date || c.start_business_date;
      if (p.date) mustBeFuture(p.date, "New weighing date");
      const op = p.operator ? await r.person(p.operator, c.park_id) : null;
      const rop = p.removal_operator ? await r.person(p.removal_operator, c.park_id) : null;
      let sheds = c.sheds.map((s) => ({ location_id: s.location_id, location_type: s.location_type, display_name: s.display_name,
        partition_label: s.partition_label || "", weighing_category: s.weighing_category, ...(s.operator_user_id ? { operator_user_id: s.operator_user_id } : {}) }));
      const changes = [];
      if (p.remove_pens?.length) {
        const gone = await r.pens(c.park_id, p.remove_pens);
        const key = (x) => `${x.location_id || x.shed_id}|${String(x.partition_label || "").toLowerCase()}`;
        const drop = new Set(gone.map(key));
        const before = sheds.length;
        sheds = sheds.filter((s) => !drop.has(key(s)));
        if (sheds.length === before) throw new Error("None of those pens are in this weighing plan.");
        changes.push(`Remove: ${penList(gone)}`);
      }
      if (p.add_pens?.length) {
        const add = await r.pens(c.park_id, p.add_pens);
        const cat = sheds[0]?.weighing_category || "individual_animal";
        for (const x of add) sheds.push({ location_id: x.shed_id, location_type: "shed", display_name: x.pen_name, partition_label: x.partition_label, weighing_category: cat });
        changes.push(`Add: ${penList(add)}`);
      }
      if (!sheds.length) throw new Error("A weighing plan needs at least one pen.");
      if (p.date) changes.push(`Date: ${c.start_business_date} -> ${p.date}`);
      if (op) changes.push(`Weighing operator: ${op.name}`);
      if (rop) changes.push(`Feed & water removal: ${rop.name}`);
      return {
        title: `Edit weighing plan: ${c.park_name}, ${c.start_business_date}`, summary: changes,
        request: { method: "PUT", path: `/weighing/campaigns/${c.campaign_id}`, body: {
          park_id: c.park_id, period_start_date: d, period_end_date: d, start_business_date: d,
          planned_cap_per_day: c.planned_cap_per_day, operator_user_id: op?.user_id || c.operator_user_id,
          ...((rop?.user_id || c.fasting_operator_user_id) ? { fasting_operator_user_id: rop?.user_id || c.fasting_operator_user_id } : {}),
          sheds,
        } },
      };
    },
  },
  weighing_publish_plan: {
    describe: "Publish a draft weighing plan so operators get it on their phones.",
    risk: "high",
    schema: z.object({ campaign_id: uuid }).strict(),
    async prepare(p, r) {
      const c = await r.campaign(p.campaign_id);
      if (c.status !== "draft") throw new Error(`That weighing plan is ${c.status}, not a draft.`);
      return { title: `Publish weighing plan: ${c.park_name}, ${c.start_business_date}`, summary: [`${c.sheds.length} pens: ${c.sheds.map((s) => s.display_name).join(", ")}`, "Operators will see it on their phones."],
        request: { method: "POST", path: `/weighing/campaigns/${c.campaign_id}/publish`, body: {} } };
    },
  },
  weighing_close_pen: {
    describe: "Close one pen in a weighing plan with a reason (animals not accepted stay unweighed).",
    risk: "normal",
    schema: z.object({ campaign_id: uuid, pen, reason }).strict(),
    async prepare(p, r, ctx) {
      const c = await r.campaign(p.campaign_id);
      const s = await r.campaignShed(c, p.pen);
      return { title: `Close weighing for ${s.display_name}: ${c.park_name}, ${c.start_business_date}`, summary: [`Reason: ${p.reason}`, `Pen status: ${s.status}`],
        request: { method: "POST", path: `/app/weighing/campaigns/${c.campaign_id}/sheds/${s.campaign_shed_id}/close`, body: { reason: p.reason, idempotency_key: ctx.proposalId } } };
    },
  },
  weighing_reopen_pen: {
    describe: "Reopen a closed pen in a weighing plan.",
    risk: "normal",
    schema: z.object({ campaign_id: uuid, pen, reason: z.string().trim().max(500).optional() }).strict(),
    async prepare(p, r) {
      const c = await r.campaign(p.campaign_id);
      const s = await r.campaignShed(c, p.pen);
      return { title: `Reopen weighing for ${s.display_name}: ${c.park_name}, ${c.start_business_date}`, summary: p.reason ? [`Reason: ${p.reason}`] : [],
        request: { method: "POST", path: `/app/weighing/campaigns/${c.campaign_id}/sheds/${s.campaign_shed_id}/reopen`, body: { reason: p.reason || "" } } };
    },
  },
  weighing_close_round: {
    describe: "Close a whole weighing plan (round) with a reason.",
    risk: "high",
    schema: z.object({ campaign_id: uuid, reason }).strict(),
    async prepare(p, r, ctx) {
      const c = await r.campaign(p.campaign_id);
      return { title: `Close weighing round: ${c.park_name}, ${c.start_business_date}`, summary: [`Reason: ${p.reason}`, `${c.sheds.length} pens; status ${c.status}`],
        request: { method: "POST", path: `/app/weighing/campaigns/${c.campaign_id}/close`, body: { reason: p.reason, idempotency_key: ctx.proposalId } } };
    },
  },

  // ---------------- Vaccination (backend/internal/vaccinationexecution) ----------------
  vaccination_postpone_drive: {
    describe: "Postpone a park's vaccination drive for one vaccine from its planned date to a later date, with a reason.",
    risk: "normal",
    schema: z.object({ park, vaccine_code: z.string().trim().min(1).max(40), original_date: date, new_date: date, reason }).strict()
      .refine((p) => p.new_date >= p.original_date, "new_date must be on or after original_date"),
    async prepare(p, r) {
      mustBeFuture(p.new_date, "New drive date");
      const pk = await r.park(p.park);
      return { title: `Postpone ${p.vaccine_code} drive: ${pk.name}, ${p.original_date} -> ${p.new_date}`, summary: [`Reason: ${p.reason}`, "The app may shift the date further if it clashes with another vaccine."],
        request: { method: "POST", path: "/vaccination/schedule/drive-date-overrides", body: {
          park_id: pk.park_id, vaccine_code: p.vaccine_code, original_drive_date: p.original_date, override_date: p.new_date, reason: p.reason } } };
    },
  },
  vaccination_reschedule_obligation: {
    describe: "Reschedule one vaccination due item (obligation) to a new due time. obligation_id from public.obligation_instances.",
    risk: "normal",
    schema: z.object({ obligation_id: uuid, due_at: rfc3339, window_end: rfc3339.optional() }).strict(),
    async prepare(p, r) {
      if (Date.parse(p.due_at) <= Date.now()) throw new Error("The new due time must be in the future.");
      if (p.window_end && Date.parse(p.window_end) < Date.parse(p.due_at)) throw new Error("window_end must be after due_at.");
      const o = await r.obligation(p.obligation_id);
      return { title: `Reschedule vaccination due item to ${p.due_at}`, summary: [`Was due: ${o.due_at} (${o.status})`, ...(o.label ? [o.label] : [])],
        request: { method: "POST", path: `/app/vaccination/obligations/${o.obligation_id}/reschedule`, body: { due_at: p.due_at, window_start: p.due_at, ...(p.window_end ? { window_end: p.window_end } : {}) } } };
    },
  },
  vaccination_operator_config: {
    describe: "Set a park's vaccination operator assignment: operators per day (1-3), default operator, selected operators (max 3).",
    risk: "normal",
    schema: z.object({ park, active_operators_per_day: z.number().int().min(1).max(3).optional(), default_operator: person.optional(),
      selected_operators: z.array(person).min(1).max(3).optional() }).strict(),
    async prepare(p, r) {
      const pk = await r.park(p.park);
      const cur = await r.vaccOperatorConfig(pk.park_id); // null = never configured
      const def = p.default_operator ? await r.person(p.default_operator, pk.park_id) : null;
      const sel = p.selected_operators ? await Promise.all(p.selected_operators.map((n) => r.person(n, pk.park_id))) : null;
      const defaultId = def?.workforce_member_id || cur?.default_operator_id;
      if (!defaultId) throw new Error("This park has no vaccination operator set up yet: name the default operator.");
      for (const x of [def, ...(sel || [])].filter(Boolean)) if (!x.workforce_member_id) throw new Error(`${x.name} is not on the workforce roster.`);
      return {
        title: `Vaccination operators: ${pk.name}`,
        summary: [`Operators per day: ${p.active_operators_per_day ?? cur?.active_operators_per_day ?? 1}`, `Default: ${def?.name || cur?.default_operator_name || "unchanged"}`,
          ...(sel ? [`Selected: ${sel.map((x) => x.name).join(", ")}`] : [])],
        request: { method: "PUT", path: "/vaccination/operator-assignment/config", body: {
          parkId: pk.park_id, activeOperatorsPerDay: p.active_operators_per_day ?? cur?.active_operators_per_day ?? 1,
          defaultOperatorId: defaultId, selectedOperatorIds: sel ? sel.map((x) => x.workforce_member_id) : (cur?.selected_operator_ids || []),
          rowVersion: cur ? Number(cur.row_version) : 0,
        } },
      };
    },
  },
  vaccination_capacity_config: {
    describe: "Set vaccination capacity: max animals per day (1-200), buffer days (0-60), scope tenant|center|shed, max shots per animal per drive.",
    risk: "normal",
    schema: z.object({ max_per_day: z.number().int().min(1).max(200).optional(), max_buffer_days: z.number().int().min(0).max(60).optional(),
      capacity_scope: z.enum(["tenant", "center", "shed"]).optional(), max_shots_per_animal_per_drive: z.number().int().min(1).max(20).nullable().optional() }).strict()
      .refine((p) => Object.keys(p).length > 0, "nothing to change"),
    async prepare(p, r) {
      const cur = await r.vaccCapacity();
      const next = {
        maxPerDay: p.max_per_day ?? cur.max_per_day, capacityScope: p.capacity_scope ?? cur.capacity_scope,
        maxBufferDays: p.max_buffer_days ?? cur.max_buffer_days, overflowPolicy: cur.overflow_policy,
        rowVersion: Number(cur.row_version),
        maxShotsPerAnimalPerDrive: p.max_shots_per_animal_per_drive !== undefined ? p.max_shots_per_animal_per_drive : (cur.max_shots_per_animal_per_drive ?? null),
      };
      const diff = [["Max per day", cur.max_per_day, next.maxPerDay], ["Buffer days", cur.max_buffer_days, next.maxBufferDays],
        ["Scope", cur.capacity_scope, next.capacityScope], ["Max shots per animal per drive", cur.max_shots_per_animal_per_drive ?? "none", next.maxShotsPerAnimalPerDrive ?? "none"]]
        .filter(([, a, b]) => String(a) !== String(b)).map(([k, a, b]) => `${k}: ${a} -> ${b}`);
      if (!diff.length) throw new Error("Those are already the current settings.");
      return { title: "Vaccination capacity settings", summary: diff, request: { method: "PUT", path: "/vaccination/capacity-config", body: next } };
    },
  },

  // ---------------- Leadership tasks (backend/internal/leadershiptasks) ----------------
  leadership_task_raise: {
    describe: "Raise a leadership task to one person (not yourself) with a title (<=80 chars), brief and deadline.",
    risk: "normal",
    schema: z.object({ assignee: person, title: z.string().trim().min(1).max(80), body: z.string().trim().max(4000).optional(), deadline_at: rfc3339 }).strict(),
    async prepare(p, r) {
      if (Date.parse(p.deadline_at) <= Date.now()) throw new Error("The deadline must be in the future.");
      const who = await r.leadershipAssignee(p.assignee);
      return { title: `Raise task for ${who.name}: ${p.title}`, summary: [`Deadline: ${p.deadline_at}`, ...(p.body ? [`Brief: ${p.body.slice(0, 200)}`] : [])],
        request: { method: "POST", path: "/app/leadership-tasks", body: { title: p.title, body: p.body || "", assignee_user_id: who.user_id, deadline_at: p.deadline_at, attachments: [] } } };
    },
  },
  leadership_task_edit: {
    describe: "Edit a leadership task you raised: title, brief, deadline. task = task_id or task number (#12). Attachments are kept.",
    risk: "normal",
    schema: z.object({ task: z.string().trim().min(1), title: z.string().trim().min(1).max(80).optional(), body: z.string().trim().max(4000).optional(), deadline_at: rfc3339.optional() }).strict()
      .refine((p) => p.title || p.body !== undefined || p.deadline_at, "nothing to change"),
    async prepare(p, r) {
      const t = await r.leadershipTask(p.task);
      if (t.status === "cancelled") throw new Error(`Task #${t.task_no} is cancelled.`);
      if (p.deadline_at && Date.parse(p.deadline_at) <= Date.now()) throw new Error("The deadline must be in the future.");
      const ch = [p.title && `Title: ${t.title} -> ${p.title}`, p.body !== undefined && "Brief updated", p.deadline_at && `Deadline -> ${p.deadline_at}`].filter(Boolean);
      return { title: `Edit task #${t.task_no}: ${t.title}`, summary: ch,
        request: { method: "POST", path: `/app/leadership-tasks/${t.task_id}/edit`, body: {
          title: p.title || t.title, body: p.body ?? t.body, attachments: t.attachments, row_version: Number(t.row_version), deadline_at: p.deadline_at || "" } } };
    },
  },
  leadership_task_status: {
    describe: "Change a leadership task's status: open | in_progress | done | cancelled (raiser may only cancel).",
    risk: "normal",
    schema: z.object({ task: z.string().trim().min(1), status: z.enum(["open", "in_progress", "done", "cancelled"]) }).strict(),
    async prepare(p, r) {
      const t = await r.leadershipTask(p.task);
      if (t.status === p.status) throw new Error(`Task #${t.task_no} is already ${p.status}.`);
      return { title: `${p.status === "cancelled" ? "Cancel" : "Update"} task #${t.task_no}: ${t.title}`, summary: [`Status: ${t.status} -> ${p.status}`],
        request: { method: "POST", path: `/app/leadership-tasks/${t.task_id}/status`, body: { status: p.status, row_version: Number(t.row_version) } } };
    },
  },
  leadership_task_comment: {
    describe: "Set the comment on a leadership task (optionally @mention people by name).",
    risk: "normal",
    schema: z.object({ task: z.string().trim().min(1), comment: z.string().trim().min(1).max(4000), mentions: z.array(person).max(10).optional() }).strict(),
    async prepare(p, r) {
      const t = await r.leadershipTask(p.task);
      const m = await Promise.all((p.mentions || []).map((n) => r.person(n)));
      return { title: `Comment on task #${t.task_no}: ${t.title}`, summary: [`"${p.comment.slice(0, 200)}"`, ...(m.length ? [`Mentions: ${m.map((x) => x.name).join(", ")}`] : [])],
        request: { method: "POST", path: `/app/leadership-tasks/${t.task_id}/comment`, body: { comment: p.comment, mentions: m.map((x) => ({ user_id: x.user_id })) } } };
    },
  },

  // ---------------- SOP tasks (backend/internal/sop) ----------------
  sop_task_create: {
    describe: "Create an SOP task from an SOP (by code or name) for a pen or park, optionally assigned to a person with a due time.",
    risk: "normal",
    schema: z.object({ sop: z.string().trim().min(2), title: z.string().trim().min(1).max(200), description: z.string().trim().max(4000).optional(),
      park, pen: pen.optional(), assignee: person.optional(), due_at: rfc3339.optional(), priority: z.enum(["low", "normal", "high", "urgent"]).optional() }).strict(),
    async prepare(p, r) {
      const s = await r.sop(p.sop);
      const pk = await r.park(p.park);
      const pn = p.pen ? (await r.pens(pk.park_id, [p.pen]))[0] : null;
      const who = p.assignee ? await r.person(p.assignee, pk.park_id) : null;
      return { title: `New SOP task (${s.name}): ${p.title}`, summary: [`Where: ${pn ? `${pn.pen_name}, ` : ""}${pk.name}`, ...(who ? [`Assigned to: ${who.name}`] : []), ...(p.due_at ? [`Due: ${p.due_at}`] : [])],
        request: { method: "POST", path: "/admin/tasks", body: {
          sop_code: s.code, task_type: s.module_key || s.code, title: p.title, description: p.description || "",
          ...(who ? { assigned_to: who.user_id } : {}), scope_type: pn ? "shed" : "park", scope_id: pn ? pn.shed_id : pk.park_id,
          priority: p.priority || "normal", ...(p.due_at ? { due_at: p.due_at } : {}), context: {} } } };
    },
  },
  sop_task_assign: {
    describe: "Assign or reassign an SOP task (task_id from public.sop_tasks) to a person.",
    risk: "normal",
    schema: z.object({ task_id: uuid, assignee: person, reason: z.string().trim().max(500).optional() }).strict(),
    async prepare(p, r) {
      const t = await r.sopTask(p.task_id);
      const who = await r.person(p.assignee);
      return { title: `Assign SOP task to ${who.name}: ${t.title}`, summary: [`State: ${t.state}`, ...(p.reason ? [`Reason: ${p.reason}`] : [])],
        request: { method: "POST", path: `/admin/tasks/${t.task_id}/assign`, body: { assigned_to: who.user_id, reason: p.reason || "", row_version: Number(t.row_version) } } };
    },
  },
  sop_task_verify: {
    describe: "Verify (accept) a submitted SOP task.",
    risk: "normal",
    schema: z.object({ task_id: uuid, reason: z.string().trim().max(500).optional() }).strict(),
    async prepare(p, r) {
      const t = await r.sopTask(p.task_id);
      return { title: `Verify SOP task: ${t.title}`, summary: [`State: ${t.state} -> accepted`],
        request: { method: "POST", path: `/admin/tasks/${t.task_id}/verify`, body: { reason: p.reason || "", row_version: Number(t.row_version) } } };
    },
  },
  sop_task_rework: {
    describe: "Send a submitted SOP task back for rework with a reason.",
    risk: "normal",
    schema: z.object({ task_id: uuid, reason }).strict(),
    async prepare(p, r) {
      const t = await r.sopTask(p.task_id);
      return { title: `Send back for rework: ${t.title}`, summary: [`Reason: ${p.reason}`, `State: ${t.state} -> rework requested`],
        request: { method: "POST", path: `/admin/tasks/${t.task_id}/rework`, body: { reason: p.reason, row_version: Number(t.row_version) } } };
    },
  },

  // ---------------- SOP definitions (backend/internal/sop; never delete) ----------------
  sop_create: {
    describe: "Create a new SOP definition (code like feed.daily_check, name). Its steps are added as a draft version afterwards.",
    risk: "normal",
    schema: z.object({ code: z.string().trim().regex(/^[a-z][a-z0-9_.]{2,80}$/, "lowercase code like feed.daily_check"), name: z.string().trim().min(2).max(120),
      description: z.string().trim().max(2000).optional(), kind: z.enum(["module", "general"]).optional() }).strict(),
    async prepare(p, r) {
      if (await r.sopExists(p.code)) throw new Error(`An SOP with code ${p.code} already exists.`);
      return { title: `Create SOP: ${p.name} (${p.code})`, summary: [p.kind === "general" ? "General SOP" : "Module SOP", ...(p.description ? [p.description.slice(0, 200)] : [])],
        request: { method: "POST", path: "/admin/sops", body: { code: p.code, name: p.name, description: p.description || "", kind: p.kind || "module", module_key: "" } } };
    },
  },
  sop_draft_version: {
    describe: "Edit an SOP by creating a new DRAFT version (never changes the published one). form_dsl_json / proof_policy_json: full JSON; omit to copy the latest version's.",
    risk: "normal",
    schema: z.object({ sop: z.string().trim().min(2), version_label: z.string().trim().min(1).max(60),
      form_dsl_json: z.string().max(200_000).optional(), proof_policy_json: z.string().max(50_000).optional(), change_note: z.string().trim().max(300).optional() }).strict(),
    async prepare(p, r) {
      const s = await r.sop(p.sop);
      const latest = await r.sopVersion(s.sop_id, "latest");
      const parse = (txt, what) => { try { const v = JSON.parse(txt); if (!v || typeof v !== "object") throw 0; return v; } catch { throw new Error(`${what} is not valid JSON.`); } };
      const form = p.form_dsl_json ? parse(p.form_dsl_json, "form_dsl_json") : latest?.form_dsl;
      const proof = p.proof_policy_json ? parse(p.proof_policy_json, "proof_policy_json") : latest?.proof_policy;
      if (!form || !proof) throw new Error("This SOP has no version to copy from: give form_dsl_json and proof_policy_json.");
      return { title: `New draft of SOP ${s.name}: ${p.version_label}`, summary: [`Based on v${latest?.version ?? "-"} (${latest?.status ?? "none"})`,
        p.form_dsl_json ? "Steps/questions changed" : "Steps copied unchanged", p.proof_policy_json ? "Proof rules changed" : "Proof rules copied unchanged",
        ...(p.change_note ? [p.change_note] : []), "Draft only: publish separately."],
        request: { method: "POST", path: `/admin/sops/${s.sop_id}/versions`, body: { version_label: p.version_label, form_dsl: form, proof_policy: proof, compatibility: latest?.compatibility || {} } } };
    },
  },
  sop_publish_version: {
    describe: "Publish an SOP draft version (the previously published version is retired). sop_version_id from public.sop_versions, or omit for the latest draft.",
    risk: "high",
    schema: z.object({ sop: z.string().trim().min(2), sop_version_id: uuid.optional() }).strict(),
    async prepare(p, r) {
      const s = await r.sop(p.sop);
      const v = await r.sopVersion(s.sop_id, p.sop_version_id || "draft");
      if (!v) throw new Error(`SOP ${s.name} has no draft version to publish.`);
      if (v.status !== "draft") throw new Error(`That version is ${v.status}, not a draft.`);
      return { title: `Publish SOP ${s.name}: ${v.version_label} (v${v.version})`, summary: ["Every new task will follow this version.", "The currently published version is retired."],
        request: { method: "POST", path: `/admin/sops/${s.sop_id}/versions/${v.sop_version_id}/publish`, body: { row_version: Number(v.row_version) } } };
    },
  },

  // ---------------- Shifting approvals ONLY (backend/internal/counts) ----------------
  shifting_approve: {
    describe: "Approve a pending SHIFTING approval request (never birth or death). request_id from public.counts_approval_requests.",
    risk: "high",
    schema: z.object({ request_id: uuid }).strict(),
    prepare: (p, r) => shiftingDecision(p, r, "approve"),
    guard: shiftingGuard,
  },
  shifting_reject: {
    describe: "Reject a pending SHIFTING approval request with a reason (never birth or death).",
    risk: "high",
    schema: z.object({ request_id: uuid, reason }).strict(),
    prepare: (p, r) => shiftingDecision(p, r, "reject"),
    guard: shiftingGuard,
  },
};

async function shiftingDecision(p, r, verb) {
  const a = await r.approval(p.request_id);
  if (a.request_type !== "shifting") throw new Error("Ask Mesha can only decide shifting requests. Birth and death approvals must be done in the app.");
  if (a.status !== "pending") throw new Error(`That shifting request is already ${a.status}.`);
  return { title: `${verb === "approve" ? "Approve" : "Reject"} shifting request: ${a.summary || a.approval_request_id}`,
    summary: [...(a.detail ? [a.detail] : []), ...(p.reason ? [`Reason: ${p.reason}`] : [])],
    request: { method: "POST", path: `/admin-web/counts/approvals/${a.approval_request_id}/${verb}`, body: verb === "reject" ? { reason: p.reason } : {} } };
}

// Runs at CONFIRM time with the CEO's bearer: re-reads the pending queue from the backend and
// refuses unless this exact request is still a pending SHIFTING request.
export async function shiftingGuard(proposal, get) {
  const id = proposal.request.path.split("/")[4];
  let cursor = "";
  for (let page = 0; page < 20; page++) {
    const res = await get("GET", `/admin-web/counts/approvals?status=pending${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`);
    if (res.status !== 200) return { ok: false, reason: `could not re-check the request (status ${res.status})` };
    const items = res.body?.items || res.body?.approvals || res.body?.requests || [];
    const hit = items.find((x) => x.approval_request_id === id);
    if (hit) return hit.request_type === "shifting" ? { ok: true } : { ok: false, reason: "only shifting requests can be decided from Ask Mesha" };
    cursor = res.body?.next_cursor || "";
    if (!cursor) break;
  }
  return { ok: false, reason: "that shifting request is no longer pending" };
}
