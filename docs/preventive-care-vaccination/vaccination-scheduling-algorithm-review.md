# Preventive Care Vaccination Scheduling Algorithm — Review Draft

**Purpose:** Vaccination drives cannot be created by “everyone in the shed is due something.” Each goat/sheep must follow species-specific schedules, intake path, approved age windows, cross-vaccine gaps, pregnancy blocks, and defer states. Drives are a **second layer** that groups only **medically compatible, window-valid** obligations.

**Sources:**



---

## 1. Core principle

Two-stage system:

| Stage | Name | Question it answers |
| --- | --- | --- |
| **A** | Per-goat due engine | *Should this animal receive dose X, and when?* |
| **B** | Drive planner / batcher | *Which due doses can safely run together in one park doctor visit on date D, with exact shed/tag/species breakdowns?* |

**Stage A must run first.** Stage B only groups obligations that Stage A already marked eligible and medically safe to combine.

---

## 2. Algorithm type (what we are building)

Not a single cron that marks a shed “vaccination day.” It is a **constraint-satisfaction + event-driven scheduling pipeline**:

1. **Event-driven per-goat generation** (SM-1 / SM-7) — on goat create, import, stage change, health change, pregnancy change, completion of prior dose.
2. **Rule-matrix lookup** — species + intake path + approved kid/adult age window → which dose rows apply.
3. **Temporal constraint propagation** — gaps from last administered vaccine type (live/killed) and series position (primary vs booster).
4. **State guards** — ICU/quarantine/warming hold/pregnancy month 4–5 block or defer.
5. **Conflict-graph partitioning** (drive layer) — only compatible doses batched same day.
6. **Incremental replan** — shed shift, death, proof rejection replan affected goats only, not full herd.

This is closer to **medical protocol orchestration** than calendar blocking.

---

## 3. Animal classification (first decision tree)

Every goat/sheep is routed into exactly one **scheduling path** before any dose is computed:

```
1. Species?
   - Goat  → goat vaccine set (ET+TT, PPR, Goat Pox, FMD, HS)
   - Sheep → sheep vaccine set (+ Blue Tongue, Sheep Pox instead of Goat Pox)

2. Intake / life path?
   A. Farm-born kid           → approved kid schedule
   B. Procured kid ≤16 weeks  → full kid schedule (strict; no adult shortcut)
   C. Procured adult          → procurement intake schedule
   D. Breeding / fattening adult at source → vaccinate at procurement OK

3. Current operational state?
   - ICU / Quarantine         → no vaccination (defer/skip; no drive eligibility)
   - Warming (post-procurement, first 7 days) → hold all vaccination
   - Normal care              → proceed to dose rules

4. Reproductive state?
   - Not pregnant             → normal
   - Pregnant ≤3 months       → may vaccinate
   - Pregnant 4–5 months      → skip/defer doses in that window
   - Post-delivery ≤2 weeks   → catch up missed doses
```

**Output of classification:** `schedule_profile` = `{species, path, age_weeks, reproductive_phase, defer_reason?}`

---

## 4. Vaccine catalog metadata (every dose row must carry)

Each protocol rule / matrix row needs structured metadata—not just a label like “PPR”:

| Field | Why |
| --- | --- |
| `vaccine_code` | ET+TT, PPR, Goat Pox, Sheep Pox, Blue Tongue, FMD, HS |
| `vaccine_class` | bacterial_killed / viral_live / viral_killed |
| `dose_role` | primary / booster / annual / catch_up |
| `dose_number` | 1, 2, … within series |
| `dose_ml` | 1 ml or 2 ml per source table |
| `species` | goat / sheep / both |
| `priority` | medical ordering (ET+TT=1, PPR=2, …) |
| `trigger_type` | birth_age / post_arrival / after_previous_completion / calendar / manual_campaign |
| `offset_days` | e.g. 28d, 49d, 112d |
| `min_gap_days` | minimum from previous dose **or** from any conflicting prior vaccine |
| `due_window_days` | earliest → latest safe window |
| `repeat_policy` | 6mo ET+TT, 3yr PPR, 1yr pox, 9mo FMD, etc. |
| `combo_group_id` | optional: doses allowed same day (e.g. FMD+HS, ET+TT+PPR) |
| `schedule_path` / `age_band` | which approved timing row applies |

Without `vaccine_class` and `combo_group_id`, gap rules and same-day combining cannot be enforced.

### Vaccine priority and type (source)

**Goats:** ET+TT (bacterial killed) · PPR (virus live) · Goat Pox (virus live) · FMD (virus killed) · HS (bacterial killed)

**Sheep:** ET+TT (bacterial killed) · PPR (virus live) · Blue Tongue (virus killed) · Sheep Pox (virus live) · FMD (virus killed) · HS (bacterial killed)

### Dosage (source)

| Vaccine | Type | Dosage | Vial |
| --- | --- | --- | --- |
| PPR | Single | 1 ml | 100 |
| ET+TT | Booster | 2 ml | 100 |
| Blue Tongue | Booster | 2 ml | 100 |
| Goat Pox | Single | 1 ml | 25 |
| Sheep Pox | Single | 1 ml | 100 |
| FMD | Single | 1 ml | 30 |
| HS | Single | 2 ml | 100 |

---

## 5. Schedule paths (source-backed)

### 5A. Farm-born / procured kid (≤16 weeks) — strict schedule

V1 uses the approved standard kid timing path because vaccination operations
ensure mothers stay vaccinated. The source/wiki mother-not-vaccinated or
unknown-mother branch is non-executable in GoatOS: do not expose, seed, ask,
import, or match on mother vaccination status.

| Vaccine | Approved kid schedule | Revaccination |
| --- | --- | --- |
| ET+TT | 4 wk + 7 wk | 6 months |
| PPR | 16 wk | 3 years |
| Blue Tongue (sheep) | 16 wk + 20 wk | 1 year |
| Goat Pox | 16 wk | 1 year |
| Sheep Pox | 12 wk | 1 year |
| FMD | 12 wk | 9 months |
| HS | 12 wk | 1 year |

**Kid combo order (PDF bundles):**

**Goats — kids**

1. ET+TT (primary)
2. ET+TT booster — **3 weeks** after #1
3. PPR
4. Goat Pox
5. FMD + HS (same day allowed)

**Sheep — kids**

1. ET+TT
2. ET+TT booster — **3 weeks**
3. PPR + Blue Tongue (same day)
4. Sheep Pox + Blue Tongue booster
5. FMD + HS

Algorithm: generate dose obligations in sequence; booster doses use `after_previous_completion` with `min_gap_days = 21` for ET+TT booster.

---

### 5B. Procured adult — intake schedule

For newly procured goats/sheep (breeding, fattening, general adult):

**Step 1 (day 0 / procurement):** ET+TT + PPR (same day OK — bacterial + live viral)

**Step 2 (after 4 weeks):** Goat Pox (goats) or Sheep Pox (sheep) + ET+TT booster

**Step 3 (later per revaccination calendar):** FMD + HS as annual/9-month cycles

**Adult combo (PDF):**

**Goats — adults**

1. ET+TT + PPR
2. ET+TT booster + Goat Pox — **4 weeks** after step 1
3. FMD + HS

**Sheep — adults**

1. ET+TT + PPR
2. ET+TT booster + Sheep Pox — **4 weeks**
3. Blue Tongue + Blue Tongue booster
4. FMD + HS

**Important:** Adults do **not** replay the full kid age matrix. They enter via `post_arrival` triggers off `entry_date` (or trusted source vaccination date if recorded at procurement).

---

## 6. Cross-vaccine gap rules (hard constraints)

These apply **between any two administered vaccines**, not only within one series:

| Prior → Next | Minimum gap |
| --- | --- |
| Live → Killed | **2 weeks** |
| Killed → Killed | **2 weeks** |
| Live → Live | **4 weeks** |
| ET+TT primary → ET+TT booster (kids) | **3 weeks** |
| Procurement ET+TT+PPR → Pox + ET booster (adults) | **4 weeks** |

**Same-day allowed combinations:**

- Bacterial + viral (any mix)
- Live viral + killed viral
- Explicit combo groups from PDF (FMD+HS, PPR+Blue Tongue, etc.)

**Same-day shot cap:** even if more rows are medically compatible, GoatOS
plans at most **2 shots per animal per drive/doctor visit**. If 3+ vaccines are
due, choose the highest-priority compatible pair and schedule the remaining
vaccines on the next safe date.

**Same-day forbidden (unless in approved combo group):**

- Two live vaccines with &lt;4 week gap since last live dose
- Any dose that violates `min_gap_days` from last completion of conflicting vaccine family

**Enforcement point:** When SM-7 schedules the next dose after completion, due date = `max(offset_days, min_gap_days, cross_vaccine_gap_days)` from `administered_at` of the **relevant prior dose** (same series or conflicting type).

---

## 7. Per-goat due engine algorithm (Stage A)

**Trigger events:** `goat.created`, `goat.imported`, `goat.stage_changed`, `goat.health.changed`, `goat.reproductive_status.changed`, `vaccination.completed`, `manual_campaign.approved`

**Steps per goat, per published protocol version:**

1. **Load goat facts:** species, approx_dob, entry_date, origin_type (born/procured), current shed → animal_stage, sex, lifecycle, health, reproductive_status, warming_entry_date.

2. **Select schedule path** (Section 3).

3. **Filter matrix rows** where eligibility matches:
   - species
   - path (kid vs adult procurement)
   - approved kid schedule path
   - animal_stage + age band
   - sex/breed if row-specific
   - exclude rows blocked by pregnancy month 4–5

4. **For each matching dose row:**
   - Compute anchor date:
     - `birth_age` → approx_dob + offset
     - `post_arrival` → entry_date + offset (adults)
     - `after_previous_completion` → defer to SM-7
   - If trusted prior completion exists → next due from last completion, not DOB
   - Apply warming hold: if `days_since_warming_entry < 7` → status = `deferred`, reason = `warming_hold`
   - Apply ICU/quarantine: → `deferred`, reason = `icu_quarantine`
   - Apply pregnancy block months 4–5: → `deferred`, reason = `late_pregnancy_hold`
   - Post-delivery catch-up window (≤2 weeks): reopen deferred doses from pregnancy skip

5. **Upsert `obligation_instances`** per goat per dose with deterministic idempotency key.

6. **On dose completion (SM-7):** schedule next `after_previous_completion` dose respecting `min_gap_days` + cross-vaccine gap from last administered vaccine class.

**Invariant:** No obligation is created without a resolvable anchor date and matching eligibility. Missing DOB on kid path → visible `missing_dob` defer, not silent skip.

---

## 8. Drive planner algorithm (Stage B) — cannot be blind

**Input:** open obligations for a park where `status IN (scheduled, due)` and
`due_at` falls inside the planning window. Sheds/tags remain breakdown
dimensions; they are not the maximum grouping boundary.

**Steps:**

1. **Revalidate each obligation** against live goat facts (death, shift, new pregnancy, ICU, warming).

2. **Bucket obligations** by:
   `(park, shed/tag breakdown, due_window_bucket, vaccine_code, dose_role, eligibility_state, species_grouping_key)`
   where kids can use `kid_mixed` and adults use `species:<species_code>`.

3. **Build vaccine conflict graph** for the bucket:
   - Node = vaccine dose family (e.g. PPR primary, Goat Pox primary)
   - Edge = cannot same-day execute (live-live violation, gap not satisfied, unknown compatibility → fail closed)

4. **Graph coloring / partitioning** → safe same-day groups.
   - Example safe group: `{FMD, HS}`
   - Example unsafe: `{PPR, Goat Pox}` if last live dose &lt; 4 weeks ago

5. **Propose drive date** only inside medical window:
   `earliest_safe_date ≤ planned_date ≤ last_safe_date`

6. **Apply one-time batching hold** before scoring. A compatible small shed/tag
   group can wait up to 7 calendar days to merge with another same-park group
   only when every animal remains inside its medical `last_safe_date`. The hold
   can happen once per obligation/dose cycle. After that, no rolling
   postponement: execute, micro-drive, defer for a real blocker, or mark a
   process exception.

7. **Apply the max-shots rule.** At most 2 shots per animal per visit. If more
   due rows exist, keep the highest-priority compatible pair in this drive and
   schedule the rest by live/killed and row-specific gap rules.

8. **Score candidate drives** (urgency, animals covered, priority, stock expiry,
   doctor/route efficiency, fairness)—hard constraints are not scored;
   incompatible groups are rejected before scoring. The goal is maximum safe
   doctor coverage for the park visit, not a separate small drive for every
   shed.

9. **Create `obligation_batches`** as park-level drive plans with per-shed/tag
   breakdowns. Kid groups can combine goat+sheep kids when compatible; adult
   groups remain species-specific inside the same park visit.

10. **Attach only compatible obligations**; leave incompatible ones for a later drive.

**What we must NOT do:**

- Batch “all due animals in shed X” into one drive without park-level
  optimization, shed/tag counts, and species/stage safety
- Keep postponing the same due item beyond the one permitted batching hold
- Ignore last administered vaccine type when planning date
- Schedule live vaccines on same day when 4-week rule not met
- Vaccinate ICU/quarantine/warming animals because their shed has a drive

---

## 9. Operational guardrails (business rules as engine guards)

| Rule | Engine behavior |
| --- | --- |
| Warming hold (7 days post-arrival) | Defer all doses; re-evaluate on day 8 |
| ICU / Quarantine | No vaccination; defer with visible reason |
| Pregnancy ≤3 months | Allow scheduled doses |
| Pregnancy 4–5 months | Defer; do not mark missed |
| Post-delivery +2 weeks | Catch-up deferred doses |
| Procured adult | `post_arrival` path, not kid matrix |
| Kid ≤16 weeks | Full kid matrix mandatory |
| Source vaccination at procurement | Trusted history suppresses duplicate primaries; still respect warming hold after farm arrival |
| Trusted vs untrusted history | Trusted → advance schedule; untrusted → catch-up review / manual campaign |

---

## 10. Mother vaccination assumption

V1 exposes exactly one approved kid schedule. Mothers are kept vaccinated for
the vaccination slice. Any source/wiki evidence for mother-not-vaccinated or
unknown-mother timing is ignored for GoatOS scheduling and must not become a
selector, fallback, API field, config field, import prompt, seed row, or UI
option.

---

## 11. Revaccination / annual cycles

After primary course completes, repeat doses fire on calendar policy:

| Vaccine | Interval |
| --- | --- |
| ET+TT | 6 months |
| PPR | 3 years |
| Blue Tongue | 1 year |
| Goat/Sheep Pox | 1 year |
| FMD | 9 months |
| HS | 1 year |

Algorithm: when primary series complete → spawn `calendar` or `every_n_days` repeat obligations from last accepted completion.

---

## 12. What exists in Goat OS today vs what we still need

| Capability | Status |
| --- | --- |
| Per-goat obligation generation (SM-1) | **Built** — birth_age, post_arrival, eligibility, defer states |
| Booster chaining (SM-7) | **Built** — `after_previous_completion` + `min_gap_days` |
| Shed batching (SM-4) | **Partial** — groups due obligations by shed; not yet full conflict graph |
| Source-approved vaccine matrix in config | **Gap** — labels exist; full matrix rows not all approved/published |
| Cross-vaccine live/killed gap enforcement | **Gap** — needs vaccine_class metadata + cross-series gap checker |
| Combo same-day grouping | **Gap** — needs `combo_group_id` + drive conflict graph |
| Approved kid schedule | **Built** — standard path only; mother-not-vaccinated / unknown-mother branch ignored |
| Warming 7-day hold | **Gap** — needs warming entry date + defer rule |
| Pregnancy month 4–5 block + post-delivery catch-up | **Gap** — needs reproductive phase rules beyond generic defer |
| Procurement intake path (ET+TT+PPR → 4wk → Pox) | **Gap** — needs adult `post_arrival` protocol version |
| V2 optimized drive planner | **Spec only** (TRD §6) — not production-complete |

---

## 13. Data inputs required before implementation

1. **Goat facts:** species, approx_dob, entry_date, origin_type, reproductive_status, lifecycle, health_status, current shed → stage
2. **Warming entry timestamp** (for 7-day hold)
3. **Trusted vaccination history:** vaccine, dose, administered_at, source, approval status
4. **Published protocol versions** per species/path with full matrix rows
5. **Vaccine metadata:** live/killed class, dose_ml, combo groups, priority
6. **Inventory/stock** (for drive feasibility, not eligibility)

---

## 14. Outputs the system produces

| Output | Consumer |
| --- | --- |
| `obligation_instances` (per goat, per dose) | Calendar, Protocol Adherence |
| `obligation_batches` (shed drives) | Vaccination execution, SOP tasks |
| Deferred obligations with reason | Control Tower gaps |
| Next dose after completion | SM-7 auto-chain |
| Drive conflict rejections | Ops review / planner logs |

---

## 15. Review decisions needed (please confirm)

1. **Kid cutoff:** strict through 16 weeks only, or also 14 weeks as separate stage gate?
2. **Live→live gap:** confirm **4 weeks** (source doc) vs any ops practice of 2 weeks.
3. **Source vaccination at procurement + warming hold:** if vaccinated at source Monday and arrives warming Tuesday, is next farm dose earliest day 8 after warming entry, or 7 days after source dose?
4. **Post-delivery catch-up:** all skipped pregnancy doses in 2 weeks, or priority order if multiple due?
5. **Sheep Blue Tongue on adult path:** booster timing relative to step 2 pox bundle.
6. **Untrusted procurement vaccination mentions:** catch-up full intake protocol or trust with review queue?

---

## 16. One-line summary for reviewers

> We are building a **two-layer vaccination orchestrator**: first compute **per-goat medically correct dose obligations** using species, intake path, the approved kid schedule, age matrix, gaps, pregnancy, and defer rules; then **only batch compatible obligations into shed drives** using a conflict graph—never schedule a shed drive from headcount alone.

---

## 17. Suggested next step after review

Turn approved rules into a **matrix config spec** (one row per dose per path) loadable into `protocol_versions.rule_dsl`, with goat-by-goat fixture tests before drive planner work.
