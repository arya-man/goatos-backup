# Mesha Cube — Metric Registry (human review surface)

Cube is the governed metric layer for the Mesha leadership assistant. It owns
**one formula per official KPI** — the single source of truth for the number.

## Metric governance (maintainer model)

AI/code agents **draft** metric definitions. A human/dev **must review and
approve** a metric before it serves official leadership KPI answers. On any
business-meaning ambiguity, the metric is **surfaced, not guessed** — it stays
`draft` with an explicit AMBIGUITY note listing the exact question for the
business owner. This mirrors the AGENTS.md maintainer-lock rule for business-rule
changes.

- `status: approved` — reviewed and signed off. Serves official figures.
- `status: draft` — usable in dev, but the assistant must label the number
  **"draft metric — pending business sign-off"** and never present it as
  official.

To approve a metric: confirm the formula + dimensions with the business owner,
resolve any AMBIGUITY note, then flip `status` here to `approved` and remove the
"(draft)" title suffix in the corresponding cube YAML. Nothing else in the read
path needs to change — the formula is already the SSOT.

Source note per metric records the canonical table(s) the formula reads today.
Migration path for all metrics: the same measure formulas move onto the
`ceo_ai.*` reporting views (and later BigQuery/dbt marts) with no change to the
governed number.

## Registry

| Metric (member) | Formula summary | Status | Owner | Source (today) | Ambiguity / open question |
|---|---|---|---|---|---|
| `animals.active_animal_count` | COUNT(goats) WHERE `lifecycle_status='alive'` | **approved** | Counts / Herd | `public.goats` | None. Unambiguous from Goats-and-Parks source semantics. |
| `animals.total_animal_count` | COUNT(goats) all statuses | approved | Counts / Herd | `public.goats` | None (supporting measure). |
| `animals.dead_count` | COUNT(goats) WHERE `lifecycle_status='dead'` | draft | Counts / Herd | `public.goats` | Current dead-flag is point-in-time state, not a period death-event count. |
| `animals.mortality_rate` | `dead_count / total_animal_count` | draft | Counts / Herd | `public.goats` | **Population denominator undefined** (start / end / average of period?) and numerator should be death EVENTS in a window (via `exited_at`/`exit_reason`), not the current flag. Confirm both with business owner. |
| `vaccination.vaccination_due` | COUNT(obligation) WHERE `status='scheduled'` AND IST due-day `<= today` | draft | Preventive Care | `public.obligation_instances` | **Bucket boundary**: does "due" mean strictly today, or today + any past-open (actionable now)? Ships as actionable-now (includes overdue); `due_today` is the strict same-day figure. |
| `vaccination.due_today` | COUNT(obligation) WHERE `status='scheduled'` AND IST due-day `= today` | draft | Preventive Care | `public.obligation_instances` | Same boundary question as above. |
| `vaccination.vaccination_overdue` | COUNT(obligation) WHERE `status='scheduled'` AND IST due-day `< today` | draft | Preventive Care | `public.obligation_instances` | Tolerance (+1 week per vaccination-rules) not yet applied — confirm whether overdue starts at due-day+1 or after tolerance. |
| `vaccination.vaccination_completed` | COUNT(obligation) WHERE `status='completed'` | draft | Preventive Care | `public.obligation_instances` | Confirm terminal-state set (only `completed`?). |
| `vaccination.vaccination_compliance` | `completed / (completed + overdue)` | draft | Preventive Care | `public.obligation_instances` | **Numerator/denominator + period undefined**. Is denominator all-due-in-period or open snapshot? What period window? |
| `feed.feed_quantity_kg` | SUM(`quantity_fed`) | draft | Feed | `public.feed_direction_completions` | Unit (kg vs g) not confirmed by docs. |
| `feed.feed_cost` | **NULL (blocked)** | draft/blocked | Feed | — | **No price/cost column exists on the feed path.** Needs an authored feed-item price catalog before any cost formula. Assistant routes feed-cost questions to "not covered yet". |
| `procurement.load_count` | COUNT(loads) | draft | Procurement | `public.procurement_loads` | Confirm which statuses count as active pipeline. |
| `procurement.procurement_animals` | SUM(`expected_count`) | draft | Procurement | `public.procurement_loads` | "animals" = expected vs received vs accepted — reconcile to one governed definition. |
| `procurement.procurement_cost` | **NULL (blocked)** | draft/blocked | Procurement | — | **No purchase price / cost column** on `procurement_loads`. Needs a captured per-load cost field. |
| `workforce.task_count` | COUNT(sop_tasks) | draft | Workforce | `public.sop_tasks` | — |
| `workforce.task_verified` | COUNT(sop_tasks) WHERE `verified_at IS NOT NULL` | draft | Workforce | `public.sop_tasks` | Confirm whether "verified" is the right completion signal. |
| `workforce.operator_completion_rate` | `task_verified / task_count` | draft | Workforce | `public.sop_tasks` | **Completed-state vocabulary undefined** (observed states include `queued`; terminal set unconfirmed). Confirm numerator/denominator states with business owner. |
| `vaccination_operator.operator_assigned_animals` | SUM(`assigned_animals`) | draft | Preventive Care | `ceo_ai.vaccination_operator_status` → `public.vaccination_drive_assignments` | Operator-grain drive load (operator-based model). "animals" = unique animal slots assigned to an operator per business date, not obligation doses. Group by `operator_label` (+ `planned_business_day`) for a per-operator figure. |
| `vaccination_operator.operator_overdue` | SUM(`overdue`) = assigned animals in still-open drives past their planned IST business day | draft | Preventive Care | `ceo_ai.vaccination_operator_status` → `vaccination_drive_assignments` + `obligation_batches` | "behind" is derived from `obligation_batches.status` (planned/in_progress) + `planned_date < today IST`. Confirm whether a superseded/reopened batch should still count. |
| `vaccination_operator.operator_capacity` | MAX(`daily_capacity`) | draft | Preventive Care | `ceo_ai.vaccination_operator_status` → `public.vaccination_capacity_config` | Per-operator-per-day animal cap (`max_per_day`, `capacity_scope='tenant'`). **MAX not SUM** — the cap is constant within an operator-day; summing across shed rows multiplies it. Only `tenant` scope is honored by the planner today. |
| `vaccination_operator.operator_utilization` | `operator_assigned_animals / operator_capacity` | draft | Preventive Care | `ceo_ai.vaccination_operator_status` | Answers "who is overloaded" (> 1.0). **Meaningful only grouped by `operator_label` + `planned_business_day`** — the ratio is per-operator-per-day. Not an official figure. |

## Dimensions (every metric)

- `park_label`, `shed_label` (facility scope — via `locations`)
- `operator_label`, `operator_id` (operator scope — the operator-grain
  `vaccination_operator` metrics only; `operator_id` is the opaque id,
  `operator_label` the display name)
- `planned_business_day` (IST planned drive date — the `vaccination_operator`
  time member; capacity/utilization must be grouped by it)
- `species` (goat / sheep — where the base row carries it)
- a time member on the IST business day (`*_business_day` / `due_business_day`)
- `tenant_id` — **security context only**, injected server-side by
  `queryRewrite`, never taken from user text.

## Verification (local oracle parity)

`active_animal_count` and `vaccination_overdue` were matched against a direct SQL
oracle over the same canonical tables on `goatos-local-current` (2026-07-22):

- active_animal_count by park → Coimbatore **736**, Channapatna **572**
- vaccination_overdue by park → Coimbatore **108**, Channapatna **60**
- vaccination_due (836) = due_today (668) + overdue (168) ✓

See `docs/runbooks/cube-local.md` → "Verify a metric against the SQL oracle".
