# Coverage HOW-TO — step-by-step + copy-paste patterns

This is the operator manual the SKILL routes to. Follow the tier that matches
your change. Every step ends by updating `docs/ceo-ai/coverage-matrix.md`.

Run the scaffold first to get filled-in stubs, then edit:

```bash
node tools/ceo-ai/scaffold-coverage.mjs <module-name>          # covered feature
node tools/ceo-ai/scaffold-coverage.mjs <module-name> --kpi    # + Cube metric stub
node tools/ceo-ai/scaffold-coverage.mjs <module-name> --exclude "reason"  # exclusion row
```

---

## Tier 1 — Cube governed metric (official KPI)

Use when the change introduces/changes a number leadership tracks over time or
compares across scope (active animals, vaccination compliance, mortality rate,
feed cost, procurement cost, operator completion rate, …).

1. Add a **`ceo_ai.<module>_base` view** (migration) the metric reads — no
   god-CTE, compute-on-write/read-model backed, tenant-scoped, IST business day.
2. Add the **Cube model** (Cube schema files, outside the browser) defining the
   formula on top of the base view via `mesha_cube_readonly`.
3. Add a **GenAI query-class** entry whose `target_tools` lists `Cube:<metric>`
   FIRST (see Tier 3 for the file).
4. Add an **eval golden Q** asserting the intent resolves through Cube
   (`tiers_any_of: ["cube"]` or `["cube","api"]`), with a SQL oracle.
5. Coverage-matrix row: `path → Cube:<metric> (+ ceo_ai.<module>_base)`.

A GenAI intent that names `Cube:<metric>` with no base view/model behind it is a
**dead route** — the guard's spirit and the eval will flag it.

---

## Tier 2 — Mesha read API mapping

Use when the backend already serves (or should serve) an operational read.

1. Register the route in `backend/internal/permissions/routes.go` gated to
   `ceo_internal` if new.
2. Add a catalog entry (name, path, service, returns, tenant_scoped, paginated,
   tables, `leadership_relevant`, reason) — keep it aligned with the read-API
   inventory the planner consumes.
3. If a stable business-language surface is needed, also add a `ceo_ai.*` view
   (Tier 3a) + Toolbox tool (Tier 3b).
4. GenAI query-class `target_tools` lists `read_api:<path>` at tier 2.
5. Coverage-matrix row: `path → read_api:<name>`.

---

## Tier 3a — `ceo_ai.*` reporting view

Use for a new table/projection with leadership-relevant columns, or a
business-language surface for SQL fallback + Toolbox.

Rules:
- One row-grain, stated in a `_grain` comment.
- Map **every** leadership column to a real source, OR a typed placeholder:
  `NULL::text  -- TODO(no source yet): <reason>`. Never silently drop a column.
- Tenant-scoped; business-named columns (`park_label`, `shed_label`,
  `animal_count`, `due`, `done`, `blocked_reason`, `owner_label`, …).
- Compute-on-write / read-model backed. No compute-on-read god-CTE on hot paths.
  Prove query plans at the ~500k obligation-row envelope (no Seq Scan on big
  tables).

Migration sketch (see scaffold output for the filled version):

```sql
CREATE OR REPLACE VIEW ceo_ai.<module>_current AS
-- _grain: one row per <grain>
SELECT
  t.tenant_id,
  l.name        AS park_label,
  s.name        AS shed_label,
  count(*)      AS animal_count,
  NULL::text    -- TODO(no source yet): <column> has no backing column
FROM <source> t
JOIN locations l ON l.location_id = t.park_id
...
GROUP BY 1,2,3;
GRANT SELECT ON ceo_ai.<module>_current TO mesha_ceo_readonly;
```

---

## Partition label requirement (all tiers)

When a table/API/view carries location (shed, park, partition, herd position),
ensure BOTH columns exist in the leadership response:
- `partition_label` (nullable) — raw stored label ('1', 'Part 3', or NULL/'whole')
- `operational_location_display` (composed by backend) — user-facing rendition

A subdivided shed must be named with its partition (`Godel 1 - Part 3` or `Castro - 2`,
space-dash-space); an undivided shed shows the bare name (`Yashoda`,
`Ho Chi Minh 1`). Never render `whole` to users — it is a storage key only.
Group and key by `shed_id + park`, never by shed name alone (names repeat).

See `docs/decisions/operational-location-convention.md` for the worked examples,
storage vs. display rules, and the canonical composition helpers per language.
Add `partition_label` and `operational_location_display` to new location-bearing
tiers and ensure the assistant's tooling contract states the convention.

---

## Tier 3b — MCP Toolbox tool

Add a curated tool over the view in `docs/ceo-ai/mcp-toolbox-tools.yaml`
(kind, description, statement selecting from `ceo_ai.<module>_current`, bound
tenant param, LIMIT). Register it in the toolset. Browser never calls Toolbox.

---

## Tier 4 — SQL fallback

No new file — the fallback executes model-drafted SELECTs against the `ceo_ai.*`
allowlist through the SQL guard (`backend/internal/ceoai/sqlguard`). Ensure the
view exists (Tier 3a). Never widen the allowlist to base tables.

---

## GenAI query-class entry (Tiers 1–4)

Add a class to the GenAI query-space the planner consumes (see
`context/agents/ceo-bot-analytics-context.md` and the assistant's query-space
config). Shape:

```json
{
  "intent": "<module>_summary",
  "description": "…",
  "examples": ["…"],
  "target_tools": ["Cube:<metric>", "read_api:<path>", "mcp:<tool>", "sql_fallback:ceo_ai.<module>_current"],
  "params": {"park_label": "optional", "period": "IST"},
  "grounding": "aggregate-first; blocked != 0; human labels; per-obligation vs per-goat where relevant"
}
```

## Eval golden Q

Add to `tools/ceo-ai/eval/golden/<domain>.json`:

```json
{
  "id": "<module>-summary",
  "class": "<module>_summary",
  "question": "…",
  "expect": { "grounded": true, "tiers_any_of": ["cube","api"], "chart_type_any_of": ["bar"] },
  "oracle": { "kind": "scalar_int", "sql": "SELECT … WHERE tenant_id = :'tenant_id'::uuid …" }
}
```

Expectation keys (see `docs/ceo-ai/eval.md`): `grounded`, `species_split`,
`refusal`, `injection_safe`(+`injection_forbid_leak`), `tiers_any_of`,
`forbid_tools_any_of` (the answer must NOT cite these views/tools),
`chart_type_any_of` (`bar|grouped_bar|stacked_bar|line|kpi|table`), `series_min`,
`aggregate_first` (optional — per-animal answers are allowed within the tenant),
and `pending_view: true` when the view is still planned (shape-validated, live
run skips it loudly; drop the flag in the PR that lands the view).

## Schema card (every `ceo_ai.*` view)

A new or changed view ships its schema card in the same change
(`ceo-ai-schema-card-guard`): `tenant_id` column present, tenant-scoped columns
marked, the page `route` for the drill link, and the SQL fact contract
`label, scope, value[, unit][, series_<name>…]` for anything the composer will
chart. No card, no view.

## Page KPI / chart (admin-web page contract)

A `kpi.*` / `chart.*` copy key added to a page in
`backend/internal/adminui/app/service.go` trips
`make ceo-ai-page-contract-drift-guard` until the page's data-source route has a
row in `docs/ceo-ai/coverage-matrix.md` → "Automatic coverage": covered,
`PLANNED:P<n> (<view>)`, or a typed exclusion. Pair it with a golden question.

## Always finish with the matrix

Add/adjust the row in `docs/ceo-ai/coverage-matrix.md` mapping the
table/API/feature → its coverage path (or exclusion + reason). This is the
baseline the guard's future enforcement builds on.
