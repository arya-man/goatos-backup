# Feed → Feed Direction — Technical Requirements / Design (TRD)

**Status:** Draft v1 · **Date:** 2026-06-22
**Companion:** [PRD.md](./PRD.md) · **Foundation:** [Generic Protocol & Obligation Engine](../protocol-engine/obligation-engine.md)
**Grounded in:** committed migrations (`000001…000060`) for what exists + wiki goatOS §12 for the feed domain design. Same correction discipline as [PHC TRD v2](../phc-vaccination/TRD.md): committed → target, no `parks`/`sheds` tables, generic engine, generic inventory.

> Feed reuses the **same** engine as vaccination: `obligation_instances` = due work, `sop_*` = execution+proof, generic inventory = stock, `outbox`/idempotency/projection-contract = plumbing, `user_scope_grants` = authority. Only the **ration math + computed plan** tables are feed-specific.

---

## 1. What's committed vs net-new

- **Reuse-as-is:** `goats` (headcount source via `shed_id`/`cohort_id`), `locations`+profiles, `workforce_*` (`feed.report` capability already seeded), `user_scope_grants`, `sop_*`, `outbox_messages`, `idempotency_keys`, projection-contract.
- **Net-new (absent):** all `feed_*` tables, generic `inventory_*`, and the `protocol_*`/`obligation_*` engine (shared with PHC).
- **No `parks`/`sheds` tables** — every feed table's `park`/`shed` FK targets `locations(location_id)` with a `location_type` guard. The wiki's `sheds.transport_shed_name` becomes `shed_profiles.transport_group` (see [PHC TRD §3](../phc-vaccination/TRD.md)).

## 2. Feed config — the ration ruleset (typed, draft→publish)

The ration math is domain-specific, so it stays typed (not flattened into `protocol_rules`), but it is **governed by the same authority + versioning** as the engine: Feed Director drafts (`protocol.draft.feed_direction`), COO/CEO publishes (`protocol.publish.feed_direction`) — **category-specific capabilities** so a Feed Director can't touch PHC rules and vice-versa; `draft → published(immutable, effective-dated)`.

| Table (wiki §12) | Purpose | Notes / corrections |
|---|---|---|
| `feed_master` | feed catalog | `feed_type` ∈ concentrate/pellets/dry/green/misc (text+CHECK, **not** native enum — repo has no enums) |
| `goat_class_energy_requirement` | daily net energy per class | key was `(breed, shed_status)` → **`(tenant_id, breed_id, animal_stage_id)`** referencing **`animal_stage_lookup`** (the animal stage drives ration, not shed lifecycle); validates a pattern's ration meets requirement |
| `feed_pattern` | versioned ration plan header | scope park_wide/shed_specific; type static/step_change; status draft/active/superseded; `prev_feed_pattern_id` chain. `park`/`shed` scope → `locations(location_id)`. This is the publishable config version. |
| `feed_pattern_feeds` | per-feed child rows | static: start=target; step_change: `step_delta=(target−start)/(duration/step_interval)`. UNIQUE `(tenant_id, feed_pattern_id, feed_id)` |
| `feed_session_templates` | sessions per park (replaces hardcoded Template) | UNIQUE `(tenant_id, park_location_id, session_order)` |
| `feed_session_template_items` | feeds per session | UNIQUE `(tenant_id, session_template_id, feed_id)` |
| `feed_change_audit` | immutable config audit | OR fold into committed `audit_log` (partitioned) — prefer reuse |

## 3. Computed plan + directions (the work)

| Table (wiki §12) | Purpose | Engine linkage |
|---|---|---|
| `supply_planning` | daily computed snapshot per `planning_date × shed × breed × age × feed`, from `feed_pattern × feed_pattern_feeds × live headcounts`. **Replaced, not updated** (no `updated_at`; `generated_at` is truth). Regenerated on 2 PM recount. | derived; not an obligation |
| `feed_directions` | operational record per `date × shed × session × breed × age × feed`; **v1=midnight, v2=2 PM after shiftings**; status active/superseded | a shed×session = an **`obligation_batches`** row (session-level fields: estimated qty, reserved stock); per-feed directions = `obligation_instances` (`rule_id` set) under that batch → one `sop_task` (packing) |
| `feed_packing` | worker packing per `date × shed × session × feed`; video; verifier fills packed qty + discrepancy; status pending/packed/flagged/verified | SOP submission; **on `packed`, `reserve`→`consume` via `inventory_stock_movements`** (FEFO lot) in same txn — ledger, not a bare decrement |
| `feed_consumption` | per shed×session; 3 proof videos (quantity/distribution/water); consumed qty by verifier; status pending/submitted/verified/flagged | SOP submission; variance = consumed vs packed (wastage deferred) |
| `feed_transport` | consolidated shed/day transport confirmation | SOP submission; consolidation via `shed_profiles.transport_group` |
| `feed_*_sop_steps` | packing/consumption/transport SOP templates | **reuse `sop_definitions/versions`** (form_dsl) — do not build parallel step tables |

## 4. Generation pipeline on GCP (reuses engine §7)

```
[Cloud Scheduler] 00:00 cron → [feed-generation Job]
   → recompute supply_planning from feed_pattern × live goats headcount (per shed)
   → write feed_directions v1 (active)
   → materialize obligation_instances (target=shed, due=session window) → sop_task (packing)
[Cloud Scheduler] 14:00 cron  (or goat.shifted location_event trigger)
   → regenerate supply_planning → feed_directions v2 (supersede v1; status active→superseded)
   → reconcile obligations (cancel/replace superseded; new ones for changed sheds)
[api] packing submit → sop_submission → feed_packing(packed) → inventory_stock_movements consume/release (FEFO, txn) → outbox
[api] consumption submit → sop_submission → feed_consumption → variance → outbox
```

- **Directions live in Postgres** (`feed_directions` + `obligation_instances`); Cloud Tasks only carries near-term packing reminders/escalations. Control Tower reads Postgres/projection.
- **2 PM recompute** = the same generation, triggered by shiftings (`goat.shifted` → `location_event`) or the 14:00 cron, producing v2 that supersedes v1 — the wiki's explicit model, expressed once via the engine.
- **Idempotent generation** keyed on `(planning_date, shed, session, feed, direction_version)` so a re-run never duplicates.

## 5. Stock = generic inventory + ledger (same as PHC)
Feed is `inventory_items.category='feed'`; `feed_master` rows FK → `inventory_items`. Stock moves through the **`inventory_stock_movements`** ledger (reserve/consume/release/adjust), `inventory_stock` balances are its projection, `CHECK(quantity_in_stock>=0)`. **Group session:** `reserve` planned qty at session start, `consume`+`release` at packing verification. No feed-only stock island, no bare decrement — same ledger as vaccines.

## 6. Edge cases
| Case | Handling |
|---|---|
| Goats shifted before 2 PM | `goat.shifted` triggers v2 regeneration; v1 directions for affected sheds → superseded; obligations reconciled |
| Headcount changed (birth/death) intra-day | folded into 2 PM recompute (or on-demand regenerate) |
| Packing flagged (discrepancy) | `feed_packing.status='flagged'` → `obligation_escalations` |
| Stock-out at packing | reserve-at-pack surfaces shortfall; `CHECK(>=0)` blocks negative |
| Missed session | obligation `missed`; escalation |
| Config change in flight | new `feed_pattern` version (superseded chain) + effective date; today's already-generated directions keep their version |

## 7. Migration plan (after the PHC `000070-075` engine/inventory land — Feed reuses them)
| Migration | Contents |
|---|---|
| `000076_feed_config.sql` | `feed_master` (→inventory_items), `goat_class_energy_requirement`, `feed_pattern`, `feed_pattern_feeds`, `feed_session_templates`, `feed_session_template_items` (all tenant-scoped, location FKs, text+CHECK enums) |
| `000077_feed_plan_directions.sql` | `supply_planning` (replace-not-update), `feed_directions` (v1/v2 + supersede), UNIQUEs per wiki §12 |
| `000078_feed_execution.sql` | `feed_packing`, `feed_consumption`, `feed_transport`; ALTER `feature_coverage_registry` CHECK add `'feed_direction'`; feed SOP definitions/versions seed; **capability seeds (explicit, no wildcard): COO/CEO `protocol.publish.feed_direction` + Feed Director `protocol.draft.feed_direction`** |

**Depends on:** the engine (`protocol_*`/`obligation_*`), generic inventory, and `shed_profiles` (incl. `transport_group`) from the PHC migrations.

## 8. Keep / enhance / ditch — identical to [PHC TRD §8](../phc-vaccination/TRD.md)
KEEP committed identity/locations/workforce/sop/outbox/RBAC; ENHANCE locations(+profiles), feature_coverage_registry(+feed_direction); DITCH legacy/BQ-reconcile from runtime.

## 9. Blocked on input
Ration values (feed × class × quantity) to seed `feed_pattern`/`feed_pattern_feeds`; session count + feed assignment per park (`feed_session_templates`); wastage scope confirmation.
