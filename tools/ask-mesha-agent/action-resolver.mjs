// Read-only name -> id resolution for CEO action proposals (action-specs.mjs).
// Every lookup is a SELECT through the agent's read-only SQL path (runSql: READ ONLY
// transaction on the mesha_ceo_readonly role). Nothing here writes. Rows come back as one
// JSON object per line (row_to_json) so free text with tabs/newlines parses safely.
// Ambiguous or unknown names throw a plain-business error the model relays to the CEO.

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export const lit = (s) => `'${String(s).replace(/\0/g, "").replace(/'/g, "''")}'`;
const PARK_CODES = { cbe: "coimbatore", cpt: "channapatna" };

export function parseJsonRows(out) {
  return String(out || "").split("\n").map((l) => l.trim()).filter((l) => l.startsWith("{")).map((l) => JSON.parse(l));
}

// pensCtes: the WITH ... CTE block of references/pens.sql (pl, pk, pens), up to its DEMO query.
export function pensCtes(pensSqlText) {
  const t = String(pensSqlText);
  const start = t.indexOf("WITH pl AS");
  const end = t.indexOf("-- DEMO");
  if (start < 0 || end < 0) throw new Error("pen resolver (pens.sql) has an unexpected shape");
  return t.slice(start, end).trim().replace(/,\s*pa AS \([\s\S]*$/, "");
}

export function createSqlResolver({ runSql, tenantId, pensSql }) {
  const tenant = UUID_RE.test(String(tenantId || "")) ? tenantId : null;
  const tf = (alias) => (tenant ? ` AND ${alias}.tenant_id = ${lit(tenant)}` : "");
  async function rows(sql) {
    const r = await runSql(`SELECT row_to_json(t)::text FROM (${sql}) t`);
    if (!r.ok) throw new Error(`Couldn't look that up just now (${String(r.out).slice(0, 160)}).`);
    return parseJsonRows(r.out);
  }
  async function one(sql, what) {
    const rs = await rows(sql);
    if (!rs.length) throw new Error(`No ${what} found.`);
    return rs[0];
  }
  const needId = (id, what) => { if (!UUID_RE.test(String(id))) throw new Error(`${what} must be an id.`); return id; };

  return {
    async park(name) {
      const n = String(name).trim().toLowerCase();
      const full = PARK_CODES[n] || n;
      const rs = await rows(`SELECT l.location_id park_id, l.name FROM locations l WHERE l.location_type='park' AND l.status='active'${tf("l")}
        AND (lower(l.name)=${lit(full)} OR lower(l.name) LIKE ${lit(full + "%")}) ORDER BY lower(l.name)=${lit(full)} DESC LIMIT 3`);
      if (!rs.length) throw new Error(`No park called "${name}".`);
      if (rs.length > 1 && rs[0].name.toLowerCase() !== full) throw new Error(`"${name}" matches several parks: ${rs.map((r) => r.name).join(", ")}.`);
      return rs[0];
    },
    // -> [{shed_id, partition_label, pen_name, pen_code}] in input order; throws on unknown/ambiguous.
    async pens(parkId, names) {
      const wanted = names.map((n) => String(n).trim());
      const conds = wanted.map((n) => `(upper(pen_code)=upper(${lit(n.replace(/\s+/g, ""))}) OR lower(pen_name)=lower(${lit(n)}))`).join(" OR ");
      const rs = await rows(`${pensCtes(pensSql())}
        SELECT DISTINCT ON (pen_key) pen_key, loc shed_id, lbl partition_label, pen_name, pen_code
        FROM pens WHERE act AND NOT grp_only AND park = (SELECT name FROM locations WHERE location_id=${lit(parkId)}) AND (${conds})
        ORDER BY pen_key, o`);
      return wanted.map((n) => {
        const hits = rs.filter((r) => r.pen_code.toUpperCase() === n.replace(/\s+/g, "").toUpperCase() || r.pen_name.toLowerCase() === n.toLowerCase());
        if (!hits.length) throw new Error(`No active pen "${n}" in that park.`);
        if (hits.length > 1) throw new Error(`"${n}" matches several pens: ${hits.map((h) => h.pen_name).join(", ")}.`);
        return hits[0];
      });
    },
    // Active workforce member by display name (exact, else unique prefix); -> {user_id, workforce_member_id, name}.
    async person(name, parkId) {
      const n = String(name).trim();
      const rs = await rows(`SELECT w.user_id, w.workforce_member_id, w.display_name AS name, w.primary_location_id FROM workforce_members w
        WHERE w.status='active'${tf("w")} AND (lower(w.display_name)=lower(${lit(n)}) OR lower(w.display_name) LIKE lower(${lit(n + "%")}))
        ORDER BY lower(w.display_name)=lower(${lit(n)}) DESC, w.display_name LIMIT 6`);
      let hits = rs.filter((r) => r.name.toLowerCase() === n.toLowerCase());
      if (!hits.length) hits = rs;
      if (hits.length > 1 && parkId) {
        const inPark = hits.filter((r) => r.primary_location_id === parkId);
        if (inPark.length) hits = inPark;
      }
      if (!hits.length) throw new Error(`No active person called "${n}".`);
      if (hits.length > 1) throw new Error(`"${n}" matches several people: ${hits.map((h) => h.name).join(", ")}. Which one?`);
      if (!hits[0].user_id) throw new Error(`${hits[0].name} has no app login, so they can't be assigned.`);
      return hits[0];
    },
    async leadershipAssignee(name) { return this.person(name); },
    async pcTask(id) {
      needId(id, "task_id");
      const t = await one(`SELECT t.task_id, t.category, t.status, t.planned_business_date::text, t.shed_id, coalesce(t.partition_label,'') partition_label,
          s.name || CASE WHEN coalesce(t.partition_label,'') <> '' THEN ' Part ' || t.partition_label ELSE '' END pen_name
        FROM pc_care_tasks t LEFT JOIN locations s ON s.location_id=t.shed_id WHERE t.task_id=${lit(id)}${tf("t")}`, "PC-care task with that id");
      return t;
    },
    async pcRound(id) {
      needId(id, "round_id");
      return one(`SELECT r.round_id, r.category, r.planned_business_date::text, p.name park_name,
          (SELECT count(*) FROM pc_care_tasks t WHERE t.round_id=r.round_id) pen_count,
          (SELECT count(*) FROM pc_care_tasks t WHERE t.round_id=r.round_id AND t.status NOT IN ('completed','closed')) open_count
        FROM pc_care_rounds r JOIN locations p ON p.location_id=r.park_id WHERE r.round_id=${lit(id)}${tf("r")}`, "PC-care round with that id");
    },
    async campaign(id) {
      needId(id, "campaign_id");
      const c = await one(`SELECT c.campaign_id, c.park_id, p.name park_name, c.start_business_date::text, c.status, c.planned_cap_per_day,
          c.operator_user_id, ft.operator_user_id fasting_operator_user_id,
          (SELECT coalesce(json_agg(json_build_object('campaign_shed_id', s.campaign_shed_id, 'location_id', s.location_id, 'location_type', s.location_type,
             'display_name', s.display_name, 'partition_label', coalesce(s.partition_label,''), 'weighing_category', s.weighing_category,
             'operator_user_id', to_jsonb(s)->>'operator_user_id', 'status', s.status) ORDER BY s.display_name), '[]')
           FROM weighing_campaign_sheds s WHERE s.campaign_id=c.campaign_id) sheds
        FROM weighing_campaigns c JOIN locations p ON p.location_id=c.park_id
        LEFT JOIN weighing_fasting_tasks ft ON ft.tenant_id=c.tenant_id AND ft.campaign_id=c.campaign_id
        WHERE c.campaign_id=${lit(id)}${tf("c")}`, "weighing plan with that id");
      return c;
    },
    async campaignShed(c, penName) {
      const n = String(penName).trim().toLowerCase();
      let hits = c.sheds.filter((s) => s.display_name.toLowerCase() === n);
      if (!hits.length) {
        const [pn] = await this.pens(c.park_id, [penName]).catch(() => []);
        if (pn) hits = c.sheds.filter((s) => s.location_id === pn.shed_id && String(s.partition_label || "").toLowerCase() === String(pn.partition_label || "").toLowerCase());
      }
      if (hits.length !== 1) throw new Error(`"${penName}" is not a pen in this weighing plan (pens: ${c.sheds.map((s) => s.display_name).join(", ")}).`);
      return hits[0];
    },
    async obligation(id) {
      needId(id, "obligation_id");
      return one(`SELECT o.obligation_id, o.status, o.due_at::text, o.row_version FROM obligation_instances o WHERE o.obligation_id=${lit(id)}${tf("o")}`, "vaccination due item with that id");
    },
    async vaccCapacity() {
      return one(`SELECT c.* FROM vaccination_capacity_config c WHERE true${tf("c")} LIMIT 1`, "vaccination capacity settings");
    },
    async vaccOperatorConfig(parkId) {
      const rs = await rows(`SELECT c.active_operators_per_day, c.default_operator_id, c.selected_operator_ids, c.row_version, w.display_name default_operator_name
        FROM vaccination_operator_assignment_config c LEFT JOIN workforce_members w ON w.workforce_member_id=c.default_operator_id
        WHERE c.park_id=${lit(parkId)}${tf("c")}`);
      return rs[0] || null;
    },
    // task_id or "#12"/"12" (task_no)
    async leadershipTask(ref) {
      const s = String(ref).trim().replace(/^#/, "");
      const where = UUID_RE.test(s) ? `t.task_id=${lit(s)}` : /^\d+$/.test(s) ? `t.task_no=${Number(s)}` : null;
      if (!where) throw new Error("Give the task number (#12) or its id.");
      return one(`SELECT t.task_id, t.task_no, t.title, t.body, t.status, t.row_version,
          (SELECT coalesce(json_agg(json_build_object('proof_id', a.proof_id, 'kind', a.kind, 'file_name', a.file_name) ORDER BY a.position), '[]')
           FROM leadership_task_attachments a WHERE a.task_id=t.task_id) attachments
        FROM leadership_tasks t WHERE ${where}${tf("t")}`, "leadership task");
    },
    async sopTask(id) {
      needId(id, "task_id");
      return one(`SELECT t.task_id, t.title, t.state, t.row_version FROM sop_tasks t WHERE t.task_id=${lit(id)}${tf("t")}`, "SOP task with that id");
    },
    async sop(ref) {
      const s = String(ref).trim();
      const rs = await rows(`SELECT d.sop_id, d.code, d.name, to_jsonb(d)->>'module_key' module_key FROM sop_definitions d
        WHERE (lower(d.code)=lower(${lit(s)}) OR lower(d.name)=lower(${lit(s)}) OR d.sop_id::text=${lit(s)})${tf("d")} LIMIT 3`);
      if (!rs.length) throw new Error(`No SOP called "${s}".`);
      if (rs.length > 1) throw new Error(`"${s}" matches several SOPs: ${rs.map((r) => r.code).join(", ")}.`);
      return rs[0];
    },
    async sopExists(code) {
      return (await rows(`SELECT 1 x FROM sop_definitions d WHERE lower(d.code) IN (lower(${lit(code)}), lower(${lit("general." + code)}))${tf("d")} LIMIT 1`)).length > 0;
    },
    // which: "latest" | "draft" | <sop_version_id>
    async sopVersion(sopId, which) {
      const cond = which === "latest" ? "" : which === "draft" ? " AND v.status='draft'" : UUID_RE.test(which) ? ` AND v.sop_version_id=${lit(which)}` : " AND false";
      const rs = await rows(`SELECT v.sop_version_id, v.version, v.version_label, v.status, v.row_version, v.form_dsl, v.proof_policy, v.compatibility
        FROM sop_versions v WHERE v.sop_id=${lit(sopId)}${cond}${tf("v")} ORDER BY v.version DESC LIMIT 1`);
      return rs[0] || null;
    },
    async approval(id) {
      needId(id, "request_id");
      return one(`SELECT a.approval_request_id, a.request_type, a.status, a.shifting_event_id,
          coalesce(a.payload->>'summary', a.payload->>'summary_line', '') summary FROM counts_approval_requests a
        WHERE a.approval_request_id=${lit(id)}${tf("a")}`, "approval request with that id");
    },
  };
}
