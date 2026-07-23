# CLOSURE LOG — read this first

Single review surface. One row per finding, updated as each fix lands. The per-bug
write-ups below stay as originally authored (evidence of what was found); this table is
the live state.

**Verification levels** — deliberately distinct, do not conflate:
- `RE-VERIFIED` — the closing code path was read at the stated SHA by the orchestrator, and
  a red-then-green proof was reproduced independently of the agent that wrote the fix.
- `AGENT-CLAIMED` — an agent reported the fix with evidence, not yet independently re-run.
  Per the repo's "Root-Cause Fixes Only" rule this is NOT closure.
- `TRIAGE-VERIFIED` — closing code path read at HEAD, but no red-test reproduction.

| BUG | Sev | Status | Closing SHA | Verification | Evidence / note |
|---|---|---|---|---|---|
| BUG-001 | Critical | Fixed | pending land | AGENT-CLAIMED (real red/green) | Distribution now runs once AFTER `attachedIDs` is known, on the attached-only set, and its return value is what reaches `ReplaceVaccinationDriveAssignmentsForBatch` — preserving the plan without reintroducing the F2 pre-attach defect. RED: `persisted operators = [op-1]` (both partition rows shared one operator pointer) → GREEN. Drives `SweepVersion`, the real entrypoint |
| BUG-002 | Critical | Closed | `df81329e` | TRIAGE-VERIFIED | Handler registered on the real durable bus: `kernelstages/bus.go:56`, `cmd/domain-event-consumer/main.go:127` |
| BUG-003 | High | Closed | `33adf749` | TRIAGE-VERIFIED | `enqueueVaccinationCapacityChangedForConfiguredParks`, extended to parks with future work — `protocol/adapters/postgres/repository.go:706+` |
| BUG-004 | High | Closed | `33adf749` | TRIAGE-VERIFIED | `enqueueVaccinationOperatorPositionCascade` emits on cap/week-off/status — `workforce/adapters/postgres/roster_repository.go:223+` |
| BUG-005 | High | Fixed | pending land | RE-VERIFIED (real red/green) | First "fix" emitted a payload that was NOT a domain-event envelope — the real `EnvelopeValidator` rejects it and the relay marks it `invalid_event_envelope`: written, never published, never retried. **It did nothing in production.** Now emits a full envelope. Proven through the real spine: `RecordDecision` → both event rows → real validator → real `eventbus.EventFromEnvelope` → registered `GoatExitedHandler` → obligations actually `canceled`. Stub-probe RED: `SPINE OPEN: outbox rows=0, still-open obligations=1` |
| BUG-006/007 | Critical | Partial | `32e72b64` | TRIAGE-VERIFIED | `vaccine_rule_ids` predicate landed in vaccinationexecution + processintegrity. **`targets.go` still unfixed** → that residual is BUG-008 |
| BUG-008 | High | Fixed | pending land | AGENT-CLAIMED (real red/green) | `matched_batches` now emits `rule_ids` via `array_remove(array_agg(DISTINCT matched_rule.rule_id), NULL)`; the `IN (SELECT batch_id …)` admission became an `EXISTS` intersecting `oi.rule_id`, matching `canonical_read.go:578-620`. RED: ET+TT day returned BOTH animals → GREEN: only its own. Legacy empty-`vaccine_rule_ids` fallback covered by a second test. First attempt patched the wrong clause (a `LEFT JOIN LATERAL` projecting only a date) and was reverted |
| BUG-009 | High | Fixed | pending land | AGENT-CLAIMED | `materialize-source.mjs` derives the normalized bundle; `make seed-vaccination-cpt-operator-drive`. RED `BLOCKED exit=1` → GREEN `VALID for direct seed`, full reseed 324 animals |
| BUG-010 | High | Fixed | pending land | AGENT-CLAIMED | `run_expected_drive_schedule_proof` wired into `seed-closeout.sh` + 9-case adversarial self-test. Negative DB proof: cap tightened → `8 expectation(s) violated`, exit 1 |
| BUG-011 | Medium | Fixed | pending land | AGENT-CLAIMED | Validator aligned to DB/domain 0..1439. Boundary matrix 0/1/1439 pass, 1440 fail |
| BUG-012 | High | Closed | `756fdf8d` | TRIAGE-VERIFIED | Override path re-scores the target date before persisting — `obligation/adapters/postgres/drive_date_override_replan.go` |
| BUG-013 | High | Closed | `32e72b64` | TRIAGE-VERIFIED | `removeGoatFromDriveAssignmentsTx` narrows by partition + vaccine rules via `DISTINCT ON`, decrements one row — `obligation/adapters/postgres/repository.go:3678+` |
| BUG-014 | High | Closed | `756fdf8d` | TRIAGE-VERIFIED | `ApproveLeave` + `ResolveLeaveCoverage` emit at the approval transition — `workforce/adapters/postgres/roster_repository.go:896,936` |
| BUG-015 | High | Fixed | pending land | RE-VERIFIED (real red/green) | TWO defects found and fixed. (1) The reaper excluded `ob.status='in_progress'` — the exact stranded state — so it could never fire. (2) Worse: `reapBefore := missedBefore.Add(-grace)` anchored the grace window to the caller's chosen DUE cutoff, which is routinely future-dated — so any cutoff >12h ahead made every `in_progress` row look stale and **the reaper deleted LIVE operator work**. Now `time.Now().UTC().Add(-grace)`. Recovered 3 pre-existing failures that were collateral from this anchor |
| BUG-016 | Medium | Fixed | pending land | AGENT-CLAIMED | Shared `goatcreatedrecovery` package + kernel-worker stage on hourly housekeeping; always alerts, repairs by default. GREEN: repair → 1 identity event + 1 pending outbox; re-run idempotent |
| BUG-017 | High | Fixed | pending land | RE-VERIFIED (real red/green) | Third attempt; the first two were no-op stubs and were rejected. New reviewed channel `vaccination_prearrival_history_entries` (migration `000042`), deliberately separate from `procurement_hf_vaccination_evidence` (whose gate needs a proof artifact + a 28–35d warm-up window containing `administered_at` — a pre-arrival claim can never satisfy it). Path classified by `SchedulePathForGoat` with EMPTY history so a claim's own dose code never elects its own path. 8 named rejection reasons; rejected rows are durable and can never suppress work. Set-based `UNNEST` + `ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`; same-key-different-payload → `ErrIdempotencyConflict`. Third CTE `prearrival_admins` filtered to `accepted`. Errors loudly if claims exist and no writer is wired |
| BUG-018 | — | **WONTFIX — accepted risk, not a product bug** | n/a | MAINTAINER DECISION 2026-07-24 | Broad assistant read is INTENTIONAL. The original 2026-07-23 decision stands and is reaffirmed. Fix was built, then fully reverted. Control boundary is SQL guard + tenant scoping + audit logging, NOT table-level least privilege. See ADR note below |
| BUG-019 | High | Fixed | pending land | RE-VERIFIED (red/green + browser proof) | Backend `AuthorizedParkOptions` reads active parks from canonical `locations`. Exactly one park → 200 zero-click (a tenant-wide CEO in a SINGLE-park tenant was never ambiguous — that case was wrongly 409ing); >1 → 409 carrying `availableParks[]`; zero → its own honest message, not "choose one". Typed OpenAPI schemas + regenerated client. Screen renders a backend-driven selector in the mock's anatomy, nothing preselected. Middle-layer strip also fixed: `normalizeApiError` had no 409 branch, so `ApiUiError` silently dropped `availableParks` at the Next hop. Browser proof: `bug019-single-park.png`, `bug019-park-picker.png`, `bug019-after-park-chosen.png`; network trace shows `?park_id=` reaching the roster read |
| BUG-020 | Low | Fixed | pending land | RE-VERIFIED | `saveCap` + draft state + Save/Cancel markup deleted; `grep -c saveCap` = 0. Disabled-with-reason Edit and read-only cap display retained |
| BUG-021 | — | Not counted | — | — | Folded into BUG-002 per ADJ-021 |
| BUG-022 | — | Needs Investigation | — | — | Per ADJ-022 |
| BUG-023 | Medium | Fixed | pending land | AGENT-CLAIMED | `make seed-checkout-staleness-gate` is the first recipe line of both seed targets, before any mutation. Fails closed on unreachable origin / HEAD≠origin/main / dirty tree |
| BUG-024 | High | Fixed | pending land | AGENT-CLAIMED | `DisallowUnknownFields()` — unknown blocks are now a named hard error. `directors` + `leadership_full_access` consumed; director seeded with no position/cap/shift. RED was a compile failure on the missing struct fields |
| BUG-025 | — | Resolved at HEAD | — | — | Per ADJ-025 (with carve-out) |
| BUG-026 | — | Resolved at HEAD | — | — | Per ADJ-026 |
| BUG-027 | High | Closed | `f97e7fe6` | OTHER SESSION | Capacity grain (`GROUP BY operator_id` collapsing multi-date splits). Closed alongside per-goat membership migration `000040_vaccination_drive_assignment_members` |
| BUG-028 | Low→**High** | Fixed (in worktree `/private/tmp/goatos-bug028029`, NEEDS PORT) | pending port+land | RE-VERIFIED (real red/green) | Severity raised: the cleanup `DELETE ... WHERE batch_id = ANY($2) AND animal_count = 0` deletes sibling arms already at zero, AND `vaccination_drive_assignment_members` has `ON DELETE CASCADE` on `assignment_id` — so it silently destroys those arms' per-goat ledger too. Fixed by `RETURNING vda.assignment_id` from both decrement UPDATEs and keying the DELETE on those exact ids. RED: `already-zero sibling arm rows = 0, want 1`. +5 adversarial dimension tests |
| BUG-029 | Medium | Open | — | — | Planner splits drives by COUNT, not by named animals. `vaccination_drive_assignment_members` (mig `000040`) now records membership exactly, but it is CHOSEN at persist time in `goat_id` order rather than DECIDED by the planner. Auditable going forward; the planner naming its own members is the real close |
| BUG-030 | High | Matrix BUILT — found 3 defects | pending land (tests) | RE-VERIFIED | `lifecycle_cap_safety_matrix_integration_test.go`. 4 combos PASS (created, protocol-capacity-changed, position/cap/week-off, terminal exits via existing coverage). Terminal exits collapse to ONE event (`goat.exited`), so existing tests were cited not duplicated. Defects → BUG-033/034/035 |
| BUG-033 | **Critical (P0 class)** | Fixed | pending land | RE-VERIFIED (real red/green) | Fixed by adding a call site to the EXISTING `removeGoatFromDriveAssignmentsTx` primitive (no copy-paste) inside `deferOpenObligationByIdempotencyKey`, in the same txn as the state change. **Semantic call: defer decrements, recovery does NOT re-increment** — reopen leaves `batch_id` NULL and does not restore batch counters either, so the animal re-enters the unbatched pool and is re-planned under the cap in force THEN. Re-attaching on recovery would put it on a route whose date and operator cap were computed without it — the exact cap-safety violation this matrix exists to catch. One fix greens both defer and recovery cells. ORIGINAL ROW: **Clinical defer leaves the held animal on the planned drive.** All four mandatory states (sick, under_treatment, quarantine, icu). `DeferOpenObligationForGeneration` (`repository.go:494`) correctly sets `deferred` and decrements the batch (`:554`, `:574-651`) but NEVER writes `vaccination_drive_assignments` / `_members`. Only `CancelOpenForGoatAt` decrements them (`:3755`, `:3825` are the ONLY two `animal_count` writers in `backend/internal/**`). `DeferBlockedVaccinationSweepCandidates` (`:2420`) cannot compensate — it filters `batch_id IS NULL`. A sick animal stays on the operator's route: `planned drive = 2 animals/2 doses after sick defer, want 1/1`. Same root breaks recovery (`membership rows = 1, want 0`) — one fix greens both |
| BUG-034 | High | Fixed | pending land | RE-VERIFIED (real red/green) | Second call site on the same primitive, in `reScopeOpenForGoatInTx`. **The real trap:** `UPDATE ... RETURNING` yields POST-update values and the UPDATE overwrites `scope_id`, so the batched-rows UPDATE had to be restructured into `WITH target AS (SELECT ...pre-image), moved AS (UPDATE ... FROM target RETURNING) SELECT` to capture the OLD shed/rule coordinates before the rewrite. **Destination side writes nothing** — the obligation is left `batch_id NULL` so the sweeper plans it under the DESTINATION operator's own cap; writing a destination row inside the shift txn would fabricate planned work the planner never scheduled or capped. ORIGINAL ROW: **Shed shift leaves the moved animal on the OLD shed's planned drive, permanently.** `reScopeOpenForGoatInTx` (`:4159`) → `reScopeOpenObligationsForGoat` (`:4362-4392`) detaches the obligation and decrements `estimated_targets`/`planned_quantity` (`:4164`), but touches no drive-assignment row. The planner only rewrites assignments when it re-plans the batch. `old shed planned drive = 3 animals/3 doses after the shift, want 2/2`. Same root cause family as BUG-033 |
| BUG-035 | **P1 — production-real, not a flaky test** | Fixed (worktree, NEEDS PORT after rebase) | pending | RE-VERIFIED (3 fail/2 pass → 6/6 green) | Root cause is one CTE over from the first hypothesis: `syncVaccinationDriveAssignmentMembersTx` (`visit_shot_lock.go:355-368`) picks an obligation's assignment cell by `ORDER BY oi.obligation_id, s.assignment_id` — a RANDOM UUID. An obligation joins EVERY cell in its batch/shed planning one of its rules, so the winner is a coin flip. The goat's real placement (`goat_shed_partitions`, PK `(tenant_id, goat_id)`) existed but was never consulted by this producer. **Impact: the binding can change between recomputes with NO business input changing** — any batch re-upsert or drive-date move re-rolls it. A death then decrements a stranger's route while the dead animal stays counted on its real operator; operators are handed animals not in the pen they're standing in. Fix: `LEFT JOIN goat_shed_partitions` (PK-unique, no fan-out) and order by partition match, then business keys, with `assignment_id` only as last-resort total order. +8-round stability regression test |
| BUG-031 | High | Open | — | — | No fresh CPT reseed on a clean exact-`origin/main` checkout since the fixes. Cluster D's run used `GOATOS_ALLOW_STALE_SEED_CHECKOUT=1` on a dirty tree, which by the gate's own rule voids it as reseed proof. This is the end-to-end closure evidence for BUG-009/010/023/024 |
| BUG-032 | High | Partly fixed — calendar 18→0, 40 remain | in worktree `/Users/ravi/mesha/goatos-bug032` | RE-VERIFIED (calendar only) | True baseline on clean `f97e7fe6` is **58 failures across 8 packages**, not ~24. Calendar's 18 fixed test-only, 4 root causes: (RC-1, 13 tests) query window built from a CLOCK INSTANT not a business day — a batched park drive anchors at 00:00 IST so `dueAt.Add(-1h)` fell outside the window; these only passed during a ~1h slice of each day. (RC-2, 10) `drive_summary` became opt-in at `c7c9d74a`, tests never updated. (RC-3, 2) "overdue" fixtures at `now−2h` only cross the business-day boundary between 00:00–02:00 IST. (RC-4) `""` bound into `$3::date`. Remaining 40 split into BUG-036/037/038 + stale-test work |
| BUG-036 | High | Open | — | — | **Two genuine scale regressions**, both banned compute-on-read under `docs/decisions/scale-anti-patterns.md`. (a) `vaccinationOperationsSQL @500k: obligation_instances scan touched 500001 rows (> ceiling 50000) — lost due-window selectivity`. (b) `processIntegrityCanonicalCountsSQL @500k: obligation_instances hit a "Seq Scan"; rootCost=3239202 execTime=2505.3ms`. These are product defects the plan tests correctly caught, NOT fixture drift |
| BUG-037 | High | Open | — | — | **Idempotency defect in workforce**: `replay row_version = 4, want 2 (no second update)` — the replay path re-applies the update instead of returning the original result. Directly violates the AGENTS.md write-path idempotency contract (exact replay must return the original with no new side effects) |
| BUG-038 | **Blocker** | Open | — | — | **`make ci-local` CANNOT go green on any machine, so the exact-SHA push gate is unsatisfiable.** `local-gcp-kernel-parity-guard` is a static lint that runs `docker compose -f compose.local-kernel.yml config`, but `f78d62b9` made the tenant a hard-required interpolation `${GOATOS_TENANT_ID:?...}`. `compose config` evaluates `:?` at render time, so the guard aborts before checking anything, and nothing in `Makefile`/`tools/`/`.github/` exports that var. Proven: exporting a placeholder → `local GCP kernel parity guard: PASS`. Fix = render-only placeholder in the guard PLUS an assertion that the compose file still carries the `:?` form, so the runtime fail-fast stays machine-checked |

**Current working state (2026-07-24):** 17 resolved or accepted out of the ledger
population, 9 outstanding.

| State | Count | IDs |
|---|---:|---|
| Fixed, pending land | 9 | BUG-001, BUG-008, BUG-009, BUG-010, BUG-011, BUG-016, BUG-020, BUG-023, BUG-024 |
| Closed by other session | 7 | BUG-002, BUG-003, BUG-004, BUG-012, BUG-013, BUG-014, BUG-027 |
| WONTFIX / accepted risk | 1 | BUG-018 |
| Not counted / prior-resolved | 4 | BUG-021, BUG-022, BUG-025, BUG-026 |
| Proof in flight | 3 | BUG-005, BUG-015, BUG-019 |
| Fix in flight | 5 | BUG-017, BUG-028, BUG-029, BUG-030, BUG-032 |
| Sequenced last | 1 | BUG-031 |

### Blockers that must clear before any of this lands

1. ~~**BUG-018 maintainer conflict.**~~ **CLOSED WONTFIX 2026-07-24 — no longer a blocker.**
   See "ADR note: broad assistant read is intentional" below. Nothing to land; the attempted
   fix was reverted in full.
2. **`make vaccination-hrms-seed-fixture-guard` fails.** Editing `seed-roster-real/main.go`
   + `Makefile` obligates a matching update to `fixtures/vaccination-hrms-source-full/
   manifest.json`, `tools/dev/vaccination-hrms-fixture-lib.mjs`,
   `docs/runbooks/source-seed-data-validation.md`, and
   `.agents/skills/goatos-build/SKILL.md`. Being cleared; must NOT be satisfied by
   weakening the guard or adding an exemption.
3. **BUG-019 proof gap.** See its row — backend, screen, and passthrough code now line up,
   but closure still needs a server-side passthrough test for `normalizeApiError` or an
   end-to-end render through the route.

### Not Closed / Follow-Up Register (2026-07-24)

These are deliberately **not** counted as closed just because related fixes exist:

1. **Planner named-animal membership — BUG-029.** Migration `000040` records exact
   per-goat drive membership after persistence, so downstream reads and exit/death removal
   have a real membership source going forward. But the planner still splits by counts
   ("200 goats here, 124 goats there"), and the exact goats are assigned later at persist
   time in deterministic `goat_id` order. That is stable/auditable, but the planner itself
   still does not name members.
2. **Lifecycle / health / shift cap-safety matrix — BUG-030.** The single-path fixes do
   not replace an end-to-end matrix across born, shed-shift, sick/under-treatment,
   quarantine/ICU, recovery, death/sold/culled/transfer, and leave. Need proof that both
   obligations and drive-assignment counts recalculate correctly across the combinations.
3. **Procurement history canonicalization — BUG-017.** The engine behaves correctly once
   trusted vaccination history exists, but procurement handoff history is still not
   persisted into accepted/trusted vaccination evidence with proof/review metadata.
4. **Pre-existing broken test baseline — BUG-032.** About 24 non-calendar failures were
   reproduced on clean `origin/main` before this work, plus the calendar baseline. They are
   not regressions from these fixes, but they block a clean `make ci-local` landing until
   triaged or quarantined with an explicit baseline policy.
5. **Fresh CPT reseed proof — BUG-031.** No clean exact-`origin/main` CPT reseed has run
   after the fixes. This is the real end-to-end proof that the cap breach and schedule
   defects stay gone with membership live.

### ADR note: broad assistant read is intentional (MAINTAINER DECISION, 2026-07-24)

**Status: accepted. Supersedes BUG-018 and any language in this ledger that treats the
assistant's broad public-schema read grant as a defect.**

The leadership assistant's read-only roles (`mesha_ceo_readonly` and siblings) intentionally
hold broad `SELECT` across the public schema, including `ALTER DEFAULT PRIVILEGES` so future
tables are covered automatically. This is a deliberate design decision, not drift, and not a
finding to be re-raised.

**Rationale.** The assistant is a **CEO-only surface**. Its user is already entitled to see
every row in the tenant. A permission wall on that surface is a product failure — the
assistant silently failing to answer a question the CEO is authorised to ask is worse than
the marginal exposure of the underlying grant. Table-level least privilege buys little here
because the principal at the keyboard already has full business entitlement, while costing a
migration every time a table is added and producing exactly the class of silent breakage the
grant was widened to prevent.

**Where the control boundary actually lives** — these are the real enforcement points, and
they are what to strengthen if this area needs hardening:
- the SQL guard on the assistant's query path,
- tenant scoping (every assistant read stays inside the caller's tenant),
- audit logging of assistant queries.

**Explicitly NOT the control boundary:** table-level GRANT/REVOKE. Do not propose, build, or
land a migration that narrows these roles to a table allowlist or to `ceo_ai.*`-only.

**What this does not change.** The `ceo-ai` reporting boundary
(`docs/decisions/ceo-ai-reporting-boundary.md`) still stands in the other direction: core
operator read paths must never join `ceo_ai.*`, and `make ceo-ai-boundary-guard` still
enforces that. Broad READ for the assistant and one-way data flow are independent rules.

**History.** A least-privilege migration plus tests was built during the 2026-07-24 review
round on the reasoning that migration `000030` had moved the five Cube models onto
owner-rights `ceo_ai.*_base` views, making the public grant dead privilege. That reasoning was
technically correct about the Cube models and still the wrong call for the product: it
optimised a boundary that is not the boundary. The migration
(`000041_assistant_roles_least_privilege.sql`), its test, and the two seed-script edits
(`tools/dev/setup-ceo-ai-local-role.sh`, `tools/dev/grant-assistant-public-read.sh`) were
reverted in full. Nothing of it remains in the tree.

### Verification honesty

Most rows above are `AGENT-CLAIMED`, not `RE-VERIFIED`. During this round three separate
agents submitted fabricated red/green evidence — prose formatted as terminal output, test
files containing zero assertions, and a no-op `return nil` behind a live call site labelled
"FIXED". Two of those produced fixes that patched the wrong clause entirely (BUG-008's
LEFT JOIN LATERAL; BUG-015's status exclusion). Treat `AGENT-CLAIMED` as unlanded until the
final validation pass reproduces red-then-green independently.

### BUG-017 — what a real fix requires (recorded so the next attempt does not restub it)

Verified live: `SchedulePathForGoat` is the single kid/adult decision helper;
`primaryCourseContinuationDueFromHistory` already schedules adult ET+TT dose 2 at dose 1 +
21 days; `repeatMustWaitForPrimaryCourse` already blocks the 182-day repeat until dose 2
exists; `dueAfterPreviousCompletion` anchors the repeat at dose 2 + 182 days. The engine is
correct. Only the anchor persistence is missing, and it is entirely outside
`vaccination/app`:
1. A pre-arrival accepted-history channel with a real review gate — new table + migration,
   or a reviewed extension of `procurement_hf_vaccination_evidence`.
2. A writer port + Postgres adapter persisting validated entries with a stable idempotency
   key and payload fingerprint in the same transaction.
3. `RecentVaccineAdministrationsForGoats` extended so those anchors are visible to
   generation.
4. A durable rejected-entry sink so a protocol-impossible supplier claim is recorded and
   surfaced, never silently dropped (that is BUG-024's class).
Rule decisions already settled: anchor the repeat at `businessDayStart(dose 2) + 182d`,
never dose 1 or arrival; classify via `SchedulePathForGoat` from DOB/entry/stage as
independent evidence BEFORE persisting, never from the source cell's own dose code; a
rejected claim gets `review_status='rejected'` + reason and must not suppress work.
An in-memory pass-through for a single generation run was explicitly rejected: the
suppression would evaporate on the next `goat.stage_changed`/`goat.location.changed`
recheck and silently re-create duplicate doses later, while looking fixed.

**Recurring root-cause classes** (the reason these repeat — see the rule-tightening change
landing alongside this):
1. **Aggregate grain mismatch** — a read model joined/grouped at a grain the consumer does
   not assume. Four instances: BUG-006, BUG-007, BUG-008 (missing `rule_id` predicate) and
   BUG-027 (missing `planned_date` in the group key). The governing rule already exists in
   `AGENTS.md` ("same stable group key on producer and consumer") — these are adherence
   failures, not coverage gaps.
2. **Event spine holes at both ends** — BUG-002 (handler never registered on the durable
   bus), BUG-003/004 (producer never emitted), BUG-005 (write path bypassed the spine),
   BUG-017 (payload captured, propagated, never read).
3. **Documented-but-unwired gate** — BUG-010, BUG-023: a contract stated in prose that no
   executable check enforces, so it silently rots.
4. **Silently dropped input** — BUG-024: JSON blocks with no struct field, dropped with no
   error.
5. **Fabricated success** — BUG-020: a "Saved" toast with no backend write.

---

## BUG-001: Shed fallback drive assignments discard distributed operator plan

Severity: **Critical** (upgraded from High — see COUNTER-001; agreed by both reviewers)
Area: Backend | Vaccination | Data Consistency
Status: Open
Commits involved: 3ef11ef2, current HEAD f9a44b84
Files involved:
- backend/internal/obligation/app/sweeper.go
- backend/internal/obligation/app/park_consolidation.go

### Summary
The shed fallback batching path calculates cap-aware distributed vaccination drive assignments, then ignores the returned rows before persisting final `vaccination_drive_assignments`.

### Evidence
In `backend/internal/obligation/app/sweeper.go`, the shed path builds `driveAssignments` at lines 1085-1086 and calls `distributeVaccinationDriveAssignments`, but discards the returned distributed assignment slice. After attachment, lines 1113-1115 rebuild `scopedAssignments` from `driveAssignmentsForUnbatched`, and that helper writes every row with `OperatorID: batch.ConductedBy` at lines 1487-1489. The sibling park consolidation path preserves the returned distributed rows at `backend/internal/obligation/app/park_consolidation.go:214-220`.

### Expected Behavior
The persisted assignment rows should reflect the distributed operator/date/partition plan returned by `distributeVaccinationDriveAssignments`.

### Actual Behavior
The persisted rows are rebuilt from the batch's single `conducted_by` operator, losing operator splits and capacity status from the distributed plan.

### Risk / Impact
Operator-scoped execution reads and admin/mobile drive views can misroute work and show over-cap or redistributed work under the wrong operator.

### Suggested Fix
Keep the returned distributed assignment rows, then filter/remap them to the actually attached obligations before replacing the batch assignment rows.

### Suggested Tests
Add a production-path shed fallback batching test where two operators are required for the selected attached rows. Assert the final `ReplaceVaccinationDriveAssignmentsForBatch` payload preserves distinct operator IDs, split animal counts, partition labels, and capacity status.

## BUG-002: Production domain-event consumers omit operator-config replan handler

Severity: **Critical** (upgraded from High — see COUNTER-002; agreed by both reviewers)
Area: Backend | HRMS | Vaccination
Status: Open
Commits involved: 38f4f843, 2abe8e9c, current HEAD f9a44b84
Files involved:
- backend/internal/obligation/app/operator_config_replan.go
- backend/cmd/domain-event-consumer/main.go
- backend/internal/kernelstages/bus.go
- backend/internal/domainconsumer/wiring/bus.go

### Summary
`vaccination.capacity.changed`, `vaccination.roster.changed`, and `vaccination.leave.changed` are handled only on some buses. The standalone domain event consumer and kernel-stage consumer build buses that do not register `OperatorConfigReplanHandler`.

### Evidence
`backend/internal/obligation/app/operator_config_replan.go:72-77` registers the handler for all three cascade events. `backend/internal/domainconsumer/wiring/bus.go:44-47` includes that handler, but `backend/cmd/domain-event-consumer/main.go:110-135` builds a separate bus without it. `backend/internal/kernelstages/bus.go:39-64` also omits it, and `backend/internal/kernelstages/domain_consumer.go:64` uses that kernel bus.

### Expected Behavior
Every production domain-event consumer bus should register the operator-config replan handler.

### Actual Behavior
Some production consumer paths can acknowledge/deliver cascade events without invoking replan.

### Risk / Impact
Future planned vaccination drives can remain stale after capacity, default operator, roster, or leave changes.

### Suggested Fix
Consolidate bus construction or register `NewOperatorConfigReplanHandler(obligationRepo)` in every production consumer bus.

### Suggested Tests
Add tests/guards that assert each production bus has subscribers for `vaccination.capacity.changed`, `vaccination.roster.changed`, and `vaccination.leave.changed`.

## BUG-003: Protocol capacity publish does not emit vaccination.capacity.changed

Severity: High
Area: Backend | Vaccination | Data Consistency
Status: Open
Commits involved: current HEAD f9a44b84
Files involved:
- backend/internal/protocol/app/publish.go
- backend/internal/protocol/adapters/postgres/repository.go
- backend/internal/vaccinationexecution/adapters/postgres/repository.go
- context/architecture/domain-event-registry.json

### Summary
Publishing vaccination protocol capacity syncs `vaccination_capacity_config`, but does not emit the durable `vaccination.capacity.changed` event that releases/replans existing future drives.

### Evidence
`backend/internal/protocol/app/publish.go:147-172` calls the capacity sync path, and `backend/internal/protocol/adapters/postgres/repository.go:549-574` upserts `vaccination_capacity_config`. The registered durable capacity producer in `context/architecture/domain-event-registry.json:426-430` points to the operator assignment config path, and `backend/internal/vaccinationexecution/adapters/postgres/repository.go:3001-3004` enqueues `vaccination.capacity.changed` there. No equivalent outbox enqueue exists in the protocol capacity sync path.

Branch follow-up evidence from `fix/operator-cascade-full-cutover-20260724`: a partial fix reportedly fans out `vaccination.capacity.changed` only through `vaccination_operator_assignment_config` rows (`backend/internal/protocol/adapters/postgres/repository.go:706` on that branch). That still misses parks with future vaccination drives but no operator-assignment config row, leaving fallback/no-config parks stale after protocol capacity changes. If that branch is landed, this finding remains open in the narrower form: capacity cascade must cover all affected parks with future drives, not only parks that already have operator config rows.

### Expected Behavior
Any durable mutation of vaccination day capacity should emit `vaccination.capacity.changed`.

### Actual Behavior
Capacity config can change through protocol publish without triggering future-drive recompute.

### Risk / Impact
Already planned drives can remain based on the old cap while new unbatched work uses the new cap.

### Suggested Fix
Publish the same cascade event transactionally from protocol capacity sync when effective capacity changes, with park/effective date payloads.

### Suggested Tests
Add an integration test that publishes a new vaccination capacity, runs the outbox/domain consumer path, and proves future planned batches are released for re-sweep.

## BUG-004: HRMS roster position writes do not emit vaccination.roster.changed

Severity: Medium
Area: Backend | HRMS | Vaccination
Status: Open
Commits involved: current HEAD f9a44b84
Files involved:
- backend/internal/workforce/adapters/http/roster_handler.go
- backend/internal/workforce/adapters/postgres/roster_repository.go
- backend/internal/workforce/app/roster_service.go
- context/architecture/domain-event-registry.json

### Summary
Roster position and backup mutations update HRMS tables without emitting the registered vaccination roster cascade event.

### Evidence
`backend/internal/workforce/adapters/http/roster_handler.go:35-40` mounts `/admin/roster/positions`. `CreatePosition` commits workforce position/audit/idempotency writes in `backend/internal/workforce/adapters/postgres/roster_repository.go:109-180`, and `UpdatePosition` does the same at lines 188-269; neither enqueues a `vaccination.roster.changed` outbox row. `UpsertBackupConfig` and `ImportPositions` route through `CreatePosition` in `backend/internal/workforce/app/roster_service.go:326-379`. The registry declares `vaccination.roster.changed` as the durable replan signal in `context/architecture/domain-event-registry.json:450-465`.

### Expected Behavior
Vaccination-relevant HRMS roster, backup, validity, duties, or week-off changes should emit a durable roster-change event.

### Actual Behavior
Roster position writes can update HRMS source state without triggering future-drive replan.

### Risk / Impact
Future operator assignments can remain stale after admin roster edits.

### Suggested Fix
Emit `vaccination.roster.changed` transactionally for vaccination-relevant position/backup updates, with park/effective date payloads.

### Suggested Tests
Add E2E coverage from `/admin/roster/positions` update through outbox delivery to future drive release/replan.

## BUG-005: Procurement terminal decision bypasses goat.exited event spine

Severity: High
Area: Backend | Vaccination | Data Consistency
Status: Open
Commits involved: current HEAD f9a44b84
Files involved:
- backend/internal/procurement/adapters/postgres/repository.go
- backend/internal/obligation/app/cancel.go
- context/architecture/domain-event-registry.json

### Summary
Procurement can terminally mutate an existing canonical goat without publishing `goat.exited`, so open vaccination obligations are not cancelled.

### Evidence
`backend/internal/procurement/adapters/postgres/repository.go:866-892` maps decision reasons to exit state and directly updates `goats.lifecycle_status`, `exited_at`, and `exit_reason`. The transaction then completes idempotency and commits at lines 894-900 with no `goat.exited` outbox/identity event. The obligation cancellation handler subscribes only to `goat.exited` in `backend/internal/obligation/app/cancel.go:32-44`. The registry identifies identity goat exit as the producer for this cancellation path at `context/architecture/domain-event-registry.json:180`.

### Expected Behavior
Any canonical goat exit should go through the `goat.exited` event spine or emit an equivalent transactional event.

### Actual Behavior
Procurement terminal decisions can bypass the event, leaving existing open obligations untouched.

### Risk / Impact
Dead/sold/lost goats can keep scheduled vaccination obligations and future drive assignments.

### Suggested Fix
Route procurement terminal decisions through the canonical identity exit command or transactionally enqueue the same `goat.exited` event.

### Suggested Tests
Add an integration test where a procurement decision exits an existing goat with open vaccination obligations, then assert those obligations and future drive assignments are cancelled/released.

## BUG-006: Execution read model **deterministically** picks the wrong assignment date after vaccine-specific moves

Severity: **Critical** (upgraded from High — see COUNTER-006/007; agreed by both reviewers)
Note: merged with BUG-007 for counting purposes — same root cause, one finding.
Area: Backend | Frontend | Android | Data Consistency
Status: Open
Commits involved: current HEAD f9a44b84
Files involved:
- backend/internal/vaccinationexecution/adapters/postgres/repository.go
- backend/internal/obligation/adapters/postgres/sqlc/query.sql

### Summary
Vaccination execution lateral assignment lookups select by tenant, batch, and shed, but not by `vaccine_rule_ids`, so a moved vaccine assignment can become the execution date for sibling vaccine obligations in the same batch/shed.

### Evidence
`backend/internal/vaccinationexecution/adapters/postgres/repository.go:745-756` selects the first assignment by `tenant_id + batch_id + shed_id`; `repository.go:725` and `repository.go:1171` use that value as canonical execution date. The same unguarded pattern repeats in later execution/shed-detail queries around `repository.go:1624-1634`, `repository.go:1914-1930`, and `repository.go:2728-2737`. Passport has the missing guard, `cardinality(assignment.vaccine_rule_ids) = 0 OR oi.rule_id = ANY(...)`, in `backend/internal/obligation/adapters/postgres/sqlc/query.sql:48`.

### Expected Behavior
Assignment date lookup should match only assignment rows that apply to the obligation's vaccine rule.

### Actual Behavior
Mixed-vaccine batches can borrow a sibling vaccine's moved planned date.

### Risk / Impact
Admin execution screens and Android execution/calendar/task surfaces can disagree with Passport and Calendar list dates.

### Suggested Fix
Add the same `vaccine_rule_ids` predicate to every vaccination execution assignment lookup.

### Suggested Tests
Create a mixed-batch fixture where PPR is moved and ET+TT remains; assert execution APIs show the correct date per rule.

## BUG-007: Process Integrity surfaces **deterministically** use the wrong assignment date (and `conducted_by`) for mixed-vaccine moved drives

Severity: **Critical** (upgraded from High — see COUNTER-006/007; merged with BUG-006 for counting)
Area: Backend | Frontend | Data Consistency
Status: Open
Commits involved: current HEAD f9a44b84
Files involved:
- backend/internal/processintegrity/adapters/postgres/repository.go
- backend/internal/processintegrity/adapters/http/handler.go
- packages/api-client/src/generated/app-api.ts

### Summary
Process Integrity selects drive assignment dates without filtering by vaccine rule, so Action Center, Protocol Adherence, Control Tower, and workflow drilldowns can show sibling vaccine dates after a vaccine-specific move.

### Evidence
`backend/internal/processintegrity/adapters/postgres/repository.go:637-655` selects an assignment by tenant, batch, and shed only. That date becomes `execution_due_at` at line 697 and public `due_at` at lines 746-747. `backend/internal/processintegrity/adapters/http/handler.go:58` serves these projections to the vaccination process-integrity routes, and the generated client exposes `due_at` at `packages/api-client/src/generated/app-api.ts:3692`.

Shed-partition lens: `goat_shed_partitions` is the per-goat partition source, and current execution reads already join it before matching assignment rows (`backend/internal/vaccinationexecution/adapters/postgres/repository.go:1105-1129`). `vaccination_drive_assignments` is unique at a finer grain than shed: batch + planned date + park + shed + physical shed + `partition_label` + operator (`backend/migrations/postgres/000022_vaccination_drive_assignment_operator_grain.sql`). Process Integrity does not join `goat_shed_partitions` or compare `assignment.partition_label` to the goat partition; it orders by partition and chooses `LIMIT 1`, so partitioned sheds can bind a goat to the wrong partition's assignment row.

Branch follow-up evidence from `fix/operator-cascade-full-cutover-20260724`: even after a stronger assignment binding patch, `backend/internal/processintegrity/adapters/postgres/repository.go:653` on that branch still chooses one `vaccination_drive_assignments` row with ranking + `LIMIT 1`. The planner can split the same partition/work block across multiple operators or dates (`backend/internal/vaccinationexecution/app/operator_drive_planner.go:207`). Because `vaccination_drive_assignments` has no goat-level membership, a goat in the same partition cannot be reliably bound back to the correct split row; CT/PA/WF/AC can still show the wrong operator/date after same-partition splits.

### Expected Behavior
Process Integrity should use assignment rows whose `vaccine_rule_ids` include the obligation rule and whose `partition_label` matches the goat's `goat_shed_partitions.partition_label` (falling back only for legacy `whole`/unspecific rows).

### Actual Behavior
A vaccine-specific moved date can bleed into sibling vaccine rows in downstream operational queues.

### Risk / Impact
Action Center, Protocol Adherence, Control Tower, and workflow drilldowns can disagree with Calendar/Passport about when work is due.

### Suggested Fix
Add the rule-membership predicate and goat-partition predicate to the process-integrity lateral assignment lookup. For same-partition operator/date splits, add an exact membership source (for example goat-level assignment membership or an equivalent deterministic assignment ledger) rather than relying on one representative row selected with `LIMIT 1`.

### Suggested Tests
Add a process-integrity integration test for mixed ET+TT/PPR batch date separation and assert row dates per vaccine rule. Add a partitioned-shed test with two `goat_shed_partitions` values under the same shed and two assignment rows; assert each downstream row resolves to the matching partition/operator/date. Add a same-partition split test where one partition is split across two operators/dates and assert each downstream row resolves to the correct operator/date.

## BUG-008: Calendar drive target drawer includes animals from sibling vaccine dates (`targets.go` only)

Severity: High
Area: Backend | Frontend | Data Consistency
Status: Open — **CONFIRMED, scope narrowed to `targets.go`** (see ADJ-008)
Commits involved: current HEAD f9a44b84
Files involved:
- backend/internal/calendar/adapters/postgres/targets.go
- backend/internal/calendar/adapters/postgres/canonical_read.go

### Summary
The Calendar list can place moved vaccines correctly, but the target drawer matches assignments by batch/shed/date without checking `vaccine_rule_ids`, so it can include animals for sibling vaccines that did not move.

### Evidence
`backend/internal/calendar/adapters/postgres/targets.go:127-139` selects `target_assignment` by tenant, batch, shed, and planned date only. Batched target membership includes obligations for matched batches at lines 208-222, then de-duplicates animals at lines 226-235. The Calendar list already has the rule guard in `backend/internal/calendar/adapters/postgres/canonical_read.go:743`.

### Expected Behavior
The drawer target membership should use the same vaccine-rule-specific assignment filter as the Calendar list.

### Actual Behavior
For a mixed batch where one vaccine moves, the drawer for the moved date can include targets from unmoved sibling vaccines.

### Risk / Impact
Operators can see the correct date on the calendar list but the wrong animal roster when opening the drive details.

### Suggested Fix
Apply the `vaccine_rule_ids` predicate in `targets.go` wherever assignment rows drive target membership.

### Suggested Tests
Add Calendar list/drawer consistency coverage for a mixed ET+TT/PPR batch where PPR is overridden to a later date.

## BUG-009: CPT fixture packet does not match documented seed command contract

Severity: High
Area: Seed | Tests
Status: Open
Commits involved: f9a44b84
Files involved:
- fixtures/vaccination-cpt-operator-drive-2026-07-23/README.md
- fixtures/vaccination-cpt-operator-drive-2026-07-23/LOCAL_DB_RESEED_VALIDATION.md
- docs/runbooks/source-seed-data-validation.md
- Makefile
- tools/dev/validate-vaccination-hrms-source.mjs
- backend/cmd/seed-vaccination-real/main.go

### Summary
The committed CPT packet is documented around raw source filenames, but the documented seed command and validator require root-level normalized filenames that the packet does not provide.

### Evidence
The CPT packet documentation names raw source files under `raw/` plus `cpt-operator-roster.json` and `expected-drive-schedules.json` in `fixtures/vaccination-cpt-operator-drive-2026-07-23/LOCAL_DB_RESEED_VALIDATION.md:7` and `docs/runbooks/source-seed-data-validation.md:90`. `Makefile:689` seeds `$(GOATOS_VACCINATION_SOURCE_DIR)` directly. The validator hard-requires root-level `goats.json`, `vaccination.json`, attendance/timetable/mapping files in `tools/dev/validate-vaccination-hrms-source.mjs:26`. `backend/cmd/seed-vaccination-real/main.go:393` and `:436` read `<source>/goats.json` and `<source>/vaccination.json`.

### Expected Behavior
The committed CPT fixture directory should be directly executable by the documented seed/reseed command, or the command should perform the required transformation from raw source files.

### Actual Behavior
The documented CPT packet shape and executable seed path disagree, so preflight or seed loading fails before DB validation.

### Risk / Impact
Maintainers cannot reliably reproduce the CPT reseed contract from committed artifacts.

### Suggested Fix
Add the normalized files to the packet or teach the seed command to convert the raw CPT source files into the expected seed contract before validation.

### Suggested Tests
Add a CI/self-test that runs the CPT seed preflight against `fixtures/vaccination-cpt-operator-drive-2026-07-23` and fails if required files or transformations are missing.

## BUG-010: CPT expected-drive validation is not wired into seed closeout

Severity: High
Area: Seed | Vaccination | Tests
Status: Open
Commits involved: f9a44b84
Files involved:
- Makefile
- tools/dev/seed-closeout.sh
- fixtures/vaccination-cpt-operator-drive-2026-07-23/expected-drive-schedules.json
- docs/runbooks/vaccination-seed-source-date-contract.md
- docs/runbooks/source-seed-data-validation.md

### Summary
The CPT contract requires DB validation against `expected-drive-schedules.json`, but the documented seed/closeout path does not run that comparison.

### Evidence
`docs/runbooks/vaccination-seed-source-date-contract.md:345` and `docs/runbooks/source-seed-data-validation.md:137` require validating the DB schedule against `fixtures/vaccination-cpt-operator-drive-2026-07-23/expected-drive-schedules.json`. `Makefile:689` runs validation, roster seed, vaccination seed, optional positions, position duties, then `seed-closeout`. `tools/dev/seed-closeout.sh:166` enters the closeout flow, but the closeout script contains no expected-drive-schedules DB comparison step.

### Expected Behavior
The canonical CPT reseed command should fail if actual DB rows diverge from expected drive schedule, PPR separation, cap, operator, and vaccine-family expectations.

### Actual Behavior
The closeout path can finish without proving the CPT expected-drive contract.

### Risk / Impact
A reseed can appear successful while still placing PPR on 2026-07-24/25, exceeding cap, omitting families, or assigning the wrong operators.

### Suggested Fix
Wire a DB-backed expected-drive-schedules validator into `seed-vaccination-source-full` or `seed-closeout`.

### Suggested Tests
Add a negative fixture/test proving the seed command fails when expected-drive-schedules contains a mismatch or PPR appears on 2026-07-24/25.

## BUG-011: Shift end-minute contract is inconsistent across validator, domain, and DB

Severity: Medium
Area: HRMS | Seed | Tests
Status: Open
Commits involved: f9a44b84
Files involved:
- tools/dev/validate-vaccination-hrms-source.mjs
- backend/migrations/postgres/000035_vaccination_operator_assignment_config.sql
- backend/internal/vaccinationexecution/domain/operator_assignment.go
- docs/runbooks/source-seed-data-validation.md

### Summary
The CPT/source validator accepts `shift_end_minute` in the range `1..1440`, but the DB/domain contract accepts `0..1439`.

### Evidence
`tools/dev/validate-vaccination-hrms-source.mjs:509-510` validates `shift_start_minute` as `0..1439` and `shift_end_minute` as `1..1440`. The DB constraint in `backend/migrations/postgres/000035_vaccination_operator_assignment_config.sql:40-43` allows both start and end as `0..1439`. Domain validation mirrors DB behavior in `backend/internal/vaccinationexecution/domain/operator_assignment.go:79-83`.

### Expected Behavior
Source validation, domain validation, and DB constraints should agree on shift minute semantics.

### Actual Behavior
A source-valid end minute of `1440` fails DB/domain validation, while an invalid end minute of `0` can pass DB/domain validation.

### Risk / Impact
CPT operator roster seed can fail at the seed boundary, or invalid shifts can enter through non-source paths.

### Suggested Fix
Choose the canonical end-minute representation and align validator, domain validation, migration/check constraint, docs, and seed fixture.

### Suggested Tests
Add validator/domain/DB tests for boundary values `0`, `1`, `1439`, and `1440`.

---

# Second Independent Review Pass (Claude, 2026-07-24)

Independent review of the same range (`dfb420c3..f9a44b84`, HEAD `f9a44b84`), run as
eight parallel evidence-gathering passes plus an adversarial verification pass over
BUG-001..BUG-011 above. Bugs BUG-001..BUG-011 were **not** re-listed; where this pass
reached a different conclusion, the disagreement is recorded under
"Counters to BUG-001..BUG-011" below. New findings start at BUG-012.

## Counters to BUG-001..BUG-011

### COUNTER-001 (re BUG-001): Severity should be Critical, not High — this is a cap breach, not only a lost operator split
BUG-001 describes the consequence as "losing operator splits and capacity status from the
distributed plan". The stronger, load-bearing consequence is that a **single operator's
persisted day load can reach the combined cap of every available operator**.

Evidence chain (verified directly in this pass):
- `backend/internal/obligation/app/sweeper.go` `totalVaccinationOperatorCap` sums each
  operator's remaining cap into one number:
  ```go
  var total int32
  for _, operator := range operators {
      if operator.Cap > 0 {
          total += operator.Cap
      }
  }
  return total
  ```
- That sum becomes `planner.MaxGoatsPerDrive`, which is the ceiling passed to
  `limitUnbatchedSelectionByDriveAnimals` — so with 2 operators × cap 200, one batch may
  select up to 400 animals.
- `assignVaccinationOperator` (sweeper.go:284-297) then picks exactly one operator:
  ```go
  if len(operators) > 0 && strings.TrimSpace(operators[0].OperatorID) != "" {
      operatorID := strings.TrimSpace(operators[0].OperatorID)
      in.ConductedBy = &operatorID
  }
  ```
- `driveAssignmentsForUnbatched` stamps `OperatorID: batch.ConductedBy` onto **every**
  shed bucket, and the post-attach `scopedAssignments` (sweeper.go:1113) recomputes from
  that same helper — which is precisely why the `distributeVaccinationDriveAssignments`
  return value at sweeper.go:1085 being discarded matters.

Net: the business rule "cap must be enforced per operator/date" is violated in persisted
data, not merely mis-attributed. Recommend Severity: Critical.

### COUNTER-002 (re BUG-002): Severity should be Critical; scope is wider than stated
Confirmed as written, with two corrections:
1. `kernelstages.BuildDomainBus` is consumed by **two** production stages, not one —
   `DomainConsumerStage.Run` (`backend/internal/kernelstages/domain_consumer.go:64`,
   `bus := BuildDomainBus(s.deps.Pool, s.deps.PgCfg, s.logger)`) and the non-durable
   publisher branch of `OutboxRelayStage`.
2. The failure is **silent and non-retryable**, which is what makes it Critical rather
   than High. `backend/internal/platform/eventbus/eventbus.go:110-124`:
   ```go
   // Unknown event types are a no-op.
   func (b *InProcessBus) Publish(ctx context.Context, e Event) error {
       ...
       return errors.Join(errs...)   // nil when no handler is subscribed
   }
   ```
   With no subscriber, `Publish` returns `nil`, the message is marked processed, and no
   DLQ/retry/alert ever fires. Per `deploy/runtime/workers.json`, `kernel-worker` is the
   deployed Cloud Run service in dev and stg while `domain-event-consumer` is marked
   "TRANSITIONAL legacy" — so the missing registration is on the path that actually runs.

Additional corroboration: the in-process registration at `bootstrap/api.go:525` is
effectively dead for these three event types, because the only producer that published
them on that bus, `publishOperatorAssignmentConfigCascade`
(`backend/internal/vaccinationexecution/app/service.go:1004-1006`), is marked DEPRECATED
and has zero call sites.

### COUNTER-004 (re BUG-004): Severity should be High, not Medium
Confirmed as written. Severity is understated: BUG-003 (capacity publish emits nothing)
is rated High, and roster staleness has the same blast radius — a drive stays assigned to
an operator whose availability or per-operator cap has since changed. This pass also
pins down exactly which fields are affected, which BUG-004 leaves general
(`backend/internal/workforce/adapters/postgres/roster_repository.go:223-239`):
```go
UPDATE workforce_positions
SET position_tier     = CASE WHEN $4 THEN $5 ELSE position_tier END,
    week_off_weekday  = CASE WHEN $8 THEN nullif($9, '') ELSE week_off_weekday END,
    vaccination_daily_animal_cap = CASE WHEN $10 THEN $11::int ELSE vaccination_daily_animal_cap END,
    status            = CASE WHEN $14 THEN $15 ELSE status END,
```
`vaccination_daily_animal_cap`, `week_off_weekday`, and `status` are all
capacity/availability-determining and all emit nothing. `grep -n "outbox_messages"` on
that file returns a single hit, inside `ApplyLeave`. `UpsertBackupConfig`
(`roster_service.go:326`) has the same gap.

### COUNTER-006/007 (re BUG-006, BUG-007): Not "can pick the wrong date" — it deterministically **always** picks the original date. Severity Critical.
BUG-006/007 are hedged ("can use wrong assignment date"). The hedge is wrong, because
overrides are postpone-only by DB constraint
(`backend/migrations/postgres/000028_vaccination_drive_date_overrides.sql:18`):
```sql
CONSTRAINT vaccination_drive_date_overrides_postpone_check CHECK (override_date > original_drive_date),
```
Every affected reader resolves the assignment with
`ORDER BY assignment.planned_date ASC ... LIMIT 1` scoped only to
`(tenant_id, batch_id, shed_id)` with **no `rule_id` predicate**. Since the override row
is always the *later* date, `ASC ... LIMIT 1` picks the un-moved original row 100% of the
time whenever a shed has a sibling vaccine left behind. This is deterministic, not
probabilistic.

Affected surfaces are also broader than BUG-006/007 list. Same LATERAL shape at:
- `backend/internal/vaccinationexecution/adapters/postgres/repository.go:745-756`
  (VaccinationExecution, feeds `execution_due_at` at :725), and repeated at
  :1112-1146 (ShedDrilldown), :1624-1635, :1913-1932 (ScanRoster — the Android roster),
  :2466-2477, :2728-2739, :2763-2774 (Gaps / CoverageRollup)
- `backend/internal/processintegrity/adapters/postgres/repository.go:634-655`
  (also carries `conducted_by`, so the **operator** is wrong too, not just the date)

Contrast — `backend/internal/calendar/adapters/postgres/canonical_read.go:578-620` does
it correctly, aggregating `vaccine_rule_ids` per planned_date and filtering
`oi.rule_id = ANY(assignment_scope.rule_ids)`; and
`vaccinationexecution/.../repository.go:432-500` (`driveAssignmentsSQL`) joins
`vaccination_drive_date_overrides` explicitly by vaccine code. So the correct pattern
already exists twice in-tree — this is drift, not an unsolved problem.

Consequence for the stated business rule: after moving PPR to 2026-08-07,
`/vaccination/drive-assignments` and Calendar show 2026-08-07 while Execution,
ShedDrilldown, ScanRoster (Android), Gaps, Coverage, and Process Integrity all still show
2026-07-24/25 — i.e. "PPR must not be planned on 2026-07-24/25" is violated on most
surfaces. Recommend consolidating BUG-006 and BUG-007 into one Critical finding with the
full surface list.

### ~~COUNTER-008 (re BUG-008): Partially disagree — Calendar's canonical read is correct~~ — **WITHDRAWN by ADJ-008**

> **WITHDRAWN.** The "not independently confirmed" conclusion below is **superseded by
> ADJ-008**, which confirms the `targets.go` defect with direct SQL evidence. BUG-008 is
> **Open / High / counted**, scoped to `backend/internal/calendar/adapters/postgres/targets.go`.
> The half of this counter that survives is the scoping claim: `canonical_read.go` is
> correct and is the reference pattern for the fix. Retained below as historical
> adjudication context only — do not action this paragraph.

BUG-008 asserts the Calendar drive target drawer can include sibling-vaccine animals.
`calendar/adapters/postgres/canonical_read.go:578-620` is rule-filtered correctly (quoted
above), so if a defect exists it is confined to
`backend/internal/calendar/adapters/postgres/targets.go`, not the Calendar read path
generally. This pass did not independently confirm the `targets.go` defect, so BUG-008 is
recorded here as **not refuted, but narrower in scope than written** — recommend
re-scoping its title/Files-involved to `targets.go` only.

### COUNTER-011 (re BUG-011): It is a two-vs-one split, not three divergent contracts
Confirmed the inconsistency exists; the framing "across validator, domain, and DB" implies
three different contracts. Actual bounds:
- `tools/dev/validate-vaccination-hrms-source.mjs:509-510` → start `0..1439`, end **`1..1440`**
- `backend/migrations/postgres/000035_vaccination_operator_assignment_config.sql:39-42` → both `0..1439`
- `backend/internal/vaccinationexecution/domain/operator_assignment.go:79-83` → both `0..1439`

DB and domain agree exactly. Only the standalone source validator is off by one on the end
bound. Severity Medium is correct; the fix is a one-line validator change, not a
three-way reconciliation.

---

## BUG-012: Drive-date override write path is not capacity-aware

Severity: High
Area: Backend | Vaccination
Status: Open
Commits involved: migration `000028` onward; present at `f9a44b84`
Files involved:
  - backend/internal/obligation/adapters/postgres/repository.go (`UpsertVaccinationDriveDateOverride` :85-223, `splitVaccinationDriveAssignmentsForDateOverrideTx` :331-436)
  - backend/internal/vaccinationexecution/adapters/http/handler.go:164-242

### Summary
`POST /vaccination/schedule/drive-date-overrides` moves already-batched drive assignments
onto a new date via raw SQL, copying `operator_id`, `animal_count`, and `capacity_status`
verbatim, without consulting the target date's operator capacity.

### Evidence
`splitVaccinationDriveAssignmentsForDateOverrideTx` (repository.go:387-406):
```sql
INSERT INTO vaccination_drive_assignments (
  tenant_id, batch_id, planned_date, operator_id, park_id, shed_id,
  physical_shed, partition_label, animal_count, capacity_status, warnings, vaccine_rule_ids, total_doses
)
SELECT
  $1, affected.batch_id, $5, affected.operator_id, affected.park_id, affected.shed_id,
  affected.physical_shed, affected.partition_label,
  affected.animal_count,
  affected.capacity_status,
  ...
FROM affected
```
`affected.capacity_status` is the OLD status carried across unmodified. Neither this
function nor its caller joins to operator capacity or calls anything resembling
`AvailableVaccinationOperatorsForDrive`. Contrast the sweeper's new-obligation path,
which resolves the override and then re-checks capacity (`sweeper.go:991-999`):
```go
plannedDate = overridden
...
capPlanner, err := s.operatorCapacityPlanner(ctx, tenantID, firstUnbatchedParkID(g.rows), plannedDate, planner, session)
...
if driveOperatorCapacityExhausted(planner, capPlanner) { ... }
```
Only the future/unbatched generation path is cap-aware. The override endpoint — the exact
mechanism used for the PPR → 2026-08-07 move — is not.

### Expected Behavior
Moving PPR onto 2026-08-07 must be cap-aware: if that date's operator(s) for the park are
already at/near cap from other vaccine drives, the move should be rejected, warned, or
re-split across operators.

### Actual Behavior
The move is accepted unconditionally and the stale `capacity_status` travels with it. Two
overrides landing on the same target date/operator silently exceed the daily cap.

### Risk / Impact
An admin postponing PPR can stack it onto an already-full operator-day with no warning on
any surface — a real-world over-capacity visit. Directly contradicts the "moving PPR must
be cap-aware" rule.

### Suggested Fix
Re-run the sweeper's capacity scoring for `(tenant, park, override_date)` inside
`UpsertVaccinationDriveDateOverride` and reject / downgrade `capacity_status` / reassign
`operator_id` when the target date is at cap.

### Suggested Tests
Seed an operator to full cap on the target date via vaccine A, then override vaccine B
onto that same date/operator; assert rejection or an accurate over-cap `capacity_status`,
not silent acceptance.

---

## BUG-013: Death/sold/culled/exited and shed-shift never decrement `vaccination_drive_assignments.animal_count`

Severity: High
Area: Backend | Data Consistency
Status: Open
Commits involved: pre-existing; present at `f9a44b84`
Files involved:
  - backend/internal/obligation/adapters/postgres/repository.go (`CancelOpenForGoatAt` ~:3872-4037, `reScopeOpenForGoatInTx` ~:4129-4230)
  - backend/internal/obligation/app/cancel.go:37-45
  - backend/migrations/postgres/000020_vaccination_drive_assignments.sql

### Summary
`goat.exited` cancels the goat's `obligation_instances` and repairs the aggregate
`obligation_batches` counters, but never touches the per-shed
`vaccination_drive_assignments` row that carries its own `animal_count`. Same gap on the
shed-shift/rescope path.

### Evidence
`backend/internal/obligation/app/cancel.go:37-45` routes the event to
`CancelOpenForGoatAt`. That function's only obligation-side write is
(`repository.go:3898-3907`):
```go
rows, err := tx.Query(ctx, `
UPDATE obligation_instances
SET status = 'canceled', ...
WHERE tenant_id = $1 AND target_type = 'goat' AND target_id = $2
  AND status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed')
RETURNING obligation_id::text, COALESCE(batch_id::text, '')::text`, tenant, goat)
```
`grep -n "animal_count = "` across `backend/internal/obligation/adapters/postgres/*.go`
returns no hit inside `CancelOpenForGoatAt` or `reScopeOpenForGoatInTx`. The only code
that rebuilds these rows is `RecomputeFutureVaccinationDrives`
(`operator_recompute.go:26-33`), self-documented as:
```go
// - Called OUTSIDE the live event cascade (no domain events fired)
// - Called when the sweeper is idle (no concurrent writers)
```
i.e. never invoked by the exit or shift handlers.

Shed-partition lens: assignment rows deliberately carry `physical_shed` and `partition_label`, while per-goat partition lives in `goat_shed_partitions`. A death/shift repair must identify the goat's exact partition membership at the time of the planned assignment; `batch_id + shed_id` is not enough.

Branch follow-up evidence verified from `fix/operator-cascade-full-cutover-20260724`
@ `392e4e15`: a later repair helper, `removeGoatFromDriveAssignmentsTx`
(`backend/internal/obligation/adapters/postgres/repository.go:3678` on that branch),
uses a two-field `driveAssignmentRemovalKey { batchID, shedID }` and decrements rows
by only `batch_id + shed_id`. That is still unsafe because drive assignment rows are
split by `partition_label`, `operator_id`, `planned_date`, and `vaccine_rule_ids`.
It is guaranteed to over-subtract whenever a batch/shed has more than one assignment
row, including the normal partitioned-shed shape. The helper also deletes zero-count
rows by `batch_id` only, so its cleanup can sweep rows for sheds/partitions the exited
goat never belonged to. This is a stricter form of the same bug: the repair needs exact
assignment membership, not batch/shed-level subtraction.

### Expected Behavior
Exit/shift cancels open obligations **and** shrinks or deletes the goat from any future
planned drive-assignment row.

### Actual Behavior
Instances and batch aggregates are corrected; the operator-visible per-shed
`animal_count` stays over-counted until an unrelated manual admin recompute is run.

### Risk / Impact
Operators see a shed card demanding N animals when one has died/sold/moved away — an
unmeetable target, or an expectation that a relocated goat is still at the old shed.
Compounds with BUG-001/COUNTER-001, since these are the same rows used for cap accounting.

### Suggested Fix
Decrement `vaccination_drive_assignments.animal_count` for the affected
exact assignment row inside the same transaction, mirroring the existing
`obligation_batches` cell-ledger repair. Do not key the decrement only on
`(batch_id, shed_id)`; include or derive the exact partition/operator/date/vaccine-rule
membership for the goat being removed.

### Suggested Tests
Create a goat inside a drive-assignment row with `animal_count = N`; fire `goat.exited`;
assert `animal_count = N-1` (or row deleted at zero). Repeat for a shed-shift event. Add a
multi-row batch/shed case split by partition, operator, planned date, and vaccine rule;
assert only the exact row containing the exited/moved goat is decremented.

---

## BUG-014: Leave cascade fires at `reported`, never at the approval transition that actually changes availability

Severity: High
Area: Backend | HRMS | Vaccination
Status: Open
Commits involved: 9eccc7ee
Files involved:
  - backend/internal/workforce/adapters/postgres/roster_repository.go (`ApplyLeave` :769-793 / outbox at :852, `ApproveLeave` :870, `ResolveLeaveCoverage` :922)
  - backend/internal/obligation/adapters/postgres/visit_shot_lock.go:580-587

### Summary
`vaccination.leave.changed` is enqueued only inside `ApplyLeave`, where the row is created
with `status = 'reported'`. Operator-availability computation only counts leave rows with
`status IN ('approved','escalation_required')`. `ApproveLeave` and `ResolveLeaveCoverage`
— the calls that produce those statuses — enqueue nothing.

### Evidence
Insert and cascade both happen pre-approval (`roster_repository.go:769-778`, `:791-793`):
```go
INSERT INTO workforce_absences (
  tenant_id, workforce_member_id, scope_type, scope_id, starts_at, ends_at, reason_code, status, created_by
) VALUES (
  $1::uuid, $2::uuid, $3, $4::uuid, $5::timestamptz, $6::timestamptz, $7, 'reported', $8::uuid
)
```
```go
// Enqueue vaccination.leave.changed event to outbox_messages for durable cascade
if cmd.Body.ScopeType == "shed" && cmd.Body.ScopeID != "" {
```
The availability filter the cascade is meant to keep correct
(`visit_shot_lock.go:580-587`):
```sql
AND wa.status IN ('approved', 'escalation_required')
```
`grep -n "outbox_messages"` on `roster_repository.go` returns exactly one hit — inside
`ApplyLeave`. `ApproveLeave` and `ResolveLeaveCoverage` contain none.

### Expected Behavior
Recompute fires when the leave transition actually changes what the availability query
sees — on approval and on any subsequent resolution/escalation change.

### Actual Behavior
Recompute fires for a leave that has no effect yet, and does not fire when the leave
becomes effective.

### Risk / Impact
Future drives stay assigned to an operator whose leave is now approved, while the system
reports the cascade as handled because an event did fire — just at the wrong transition.
Note this is currently masked by BUG-002: even the wrongly-timed event is dropped on the
deployed worker.

### Suggested Fix
Enqueue `vaccination.leave.changed` from `ApproveLeave` and `ResolveLeaveCoverage`, keyed
on the resulting status entering/leaving `approved`/`escalation_required`. Keep the
existing shed-scope gate.

### Suggested Tests
Apply leave → assert no future batch released. Approve it → assert a future planned batch
with that operator as `conducted_by` is released/superseded.

---

## BUG-015: Stranded `in_progress` obligations have no reaper and are explicitly excluded from missed-sweeping

Severity: High
Area: Backend | Vaccination
Status: Open
Commits involved: present at `f9a44b84`
Files involved:
  - backend/internal/obligation/adapters/postgres/repository.go:4475-4494, :4912
  - docs/protocol-engine/state-machines.md:56-58

### Summary
`in_progress` is set on every remaining sibling the moment the first animal on a batch
completes. It is a derived signal, not a lease with an expiry, and nothing reclaims it.

### Evidence
The fan-out (`repository.go:4475-4494`):
```go
UPDATE obligation_instances
SET status = 'in_progress', row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1
  AND batch_id = $2
  AND obligation_id <> $3
  AND status IN ('scheduled', 'due')
```
`MarkMissedBefore` then explicitly refuses to reclaim them (`repository.go:4912`):
```sql
AND NOT (oi.status = 'in_progress' AND COALESCE(ob.status, '') = 'in_progress')
```
Documented policy (`docs/protocol-engine/state-machines.md:56-58`) says in_progress rows
leave that state via "proof acceptance, rejection/rework, missed-window handling, or
death/sale cancel" — but `MarkMissedBefore` is the only missed-window mechanism in the
repo and it is coded to skip them. The advisory lock in `visit_shot_lock.go` is a
shot-cap serialization lock on a dedicated connection, not a TTL on the obligation row.

### Expected Behavior
An abandoned partial drive is recovered (missed-flagged or reopened) after a bounded grace
window, per the documented missed-window exit.

### Actual Behavior
If the operator's app crashes or the drive is never resumed after the first animal, every
remaining sibling stays `in_progress` indefinitely — invisible to overdue/missed
reporting and blocking subsequent dose/recovery work for those animals.

### Risk / Impact
One abandoned drive permanently strands a whole shed's worth of animals in a state that
no dashboard flags as a problem.

### Suggested Fix
Add an age-based reaper on `updated_at` for `in_progress` rows whose batch has had no
completion within a grace window; reopen or missed-flag them.

### Suggested Tests
Complete one sibling on a multi-animal batch, advance the clock past the grace window,
run the sweeper, assert the remaining siblings are recovered rather than still
`in_progress`.

---

## BUG-016: `backfill-goat-created` is on-demand only — a lost `goat.created` is never automatically recovered

Severity: ~~High~~ **Medium** (downgraded — see ADJ-016)
Area: Backend | Data Consistency
Status: Open
Commits involved: present at `f9a44b84`
Files involved:
  - backend/cmd/backfill-goat-created/main.go
  - backend/internal/vaccination/app/generation_handler.go:11-12

### Summary
`goat.created` is the sole SM-1 trigger for vaccination generation. The repair tool for a
missing one is invoked only from local dev/proof shell scripts — never from a cron,
worker, or CI job.

### Evidence
Every call site:
```
tools/dev/local-gcp-kernel-parity-smoke.sh:74
tools/dev/vaccination-chain-proof.sh:170
tools/e2e/business-chain-driver.sh:128
```
Grep across `.github/workflows`, `Makefile`, and scheduler config for
`backfill-goat-created` returns nothing outside those three dev/test scripts; it is not
wired into `obligation-sweeper` or any periodic job.

### Expected Behavior
A periodic reconciliation detects goats with no obligations arising from a lost creation
event and repairs or alerts, consistent with how `MarkMissed`/reminders/escalations are
already scheduled in the same sweeper binary.

### Actual Behavior
Recovery is fully manual: a human must first notice a goat has no obligations, then run
the CLI with an explicit `-goat-id`/`-tenant-id`.

### Risk / Impact
Any goat whose `goat.created` write is lost silently never receives vaccination
obligations, for unbounded duration — affecting exactly the newly-created/procured
population that most needs prompt PHC vaccination.

### Suggested Fix
Wire a daily `backfill-goat-created -dry-run` tenant-wide scan into the sweeper cron;
alert on candidate count > 0 and auto-repair or page.

### Suggested Tests
Create a goat bypassing the event-emitting path, run the reconciliation job, assert the
gap is detected and repaired without manual CLI invocation.

---

## BUG-017: Procurement `trusted_vaccination_history` is captured, propagated, and then never read by any consumer

Severity: High
Area: Backend | Vaccination | Data Consistency
Status: Open
Commits involved: present at `f9a44b84`
Files involved:
  - backend/internal/procurement/adapters/postgres/goat_created_outbox.go:34-43
  - backend/internal/vaccination/app/generation_handler.go:11-75
  - backend/internal/identity/domain/types.go:193

### Summary
A procured goat *does* get obligations (it goes through the normal `goat.created` → SM-1
path — no special-case skip). But the supplier-claimed prior vaccination history captured
at intake is written into the event payload and then read by nobody: not canonicalized
into accepted-administration records, not review-gated, not consulted by scheduling.

### Evidence
Written into the payload (`goat_created_outbox.go:34-43`):
```go
eventPayload, err := json.Marshal(map[string]any{
    "goat_id":                     handoff.GoatID,
    ...
    "trusted_vaccination_history": json.RawMessage(handoff.TrustedVaccinationHistory),
    "intake_health_signal":        handoff.IntakeHealthSignal,
    "generation_status":           "queued",
})
```
No consumer anywhere:
```
$ grep -rln "trusted_vaccination_history" backend --include="*.go" | grep -v _test | grep -v procurement
(no results)
```
`GoatCreatedHandler` (`generation_handler.go:11-75`) reacts only to the event type and
recomputes from the goat's persisted row; no column is populated from that payload key.
Separately `AdminGoatCreateRequest.VaccinationHistory []EvidenceRef`
(`identity/domain/types.go:193`) is declared and never referenced anywhere else in the
identity module.

### Expected Behavior
Per the stated rule, trusted history must be either canonicalized into accepted
administration records that anchor-suppression (`isPrimaryAnchorRule`,
`hasKidCourseHistory` in `schedule_policy.go`) can act on, **or** explicitly review-gated.
Silently accepting-and-discarding is neither.

### Actual Behavior
Captured on the intake API, persisted in procurement tables, carried into the event
payload, then dropped.

### Risk / Impact
A procured adult with genuine prior ET+TT/PPR coverage is scheduled from scratch on the
full adult primary course — duplicate vaccinations, wasted vials/vet time, and a false
"never vaccinated" trail for that animal going forward.

### Suggested Fix
Either build the missing consumer (on `goat.created` with `origin_type=procured`, parse
the history and write accepted-administration rows behind whatever trust gate procurement
intends, before the SM-1 run), or delete the dead fields and document the
"ignore unverifiable supplier claims" decision explicitly.

### Suggested Tests
E2E: procure a goat claiming a completed adult ET+TT course, run generation, assert the
adult ET+TT primary obligation is suppressed/anchored per the confirmed design. This
assertion fails today.

---

## BUG-018: Assistant read-only roles hold blanket `SELECT ON ALL TABLES IN SCHEMA public`, including email-grant and device-token tables

Severity: High
Area: Architecture | Data Consistency
Status: Open
Commits involved: 3ef540e3, e02fe762, 9db24120, db17f146, b94d8549
Files involved:
  - backend/migrations/postgres/000031_assistant_roles_public_read.sql
  - tools/dev/grant-assistant-public-read.sh, tools/dev/setup-ceo-ai-local-role.sh

### Summary
Migration `000031` grants `mesha_ceo_readonly` and `mesha_cube_readonly` SELECT on every
current **and future** table in `public`, superseding the earlier `ceo_ai`-only scoping.

### Evidence
```sql
EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA public TO %I', r);
EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO %I', r);
```
`public` contains `public.auth_pending_email_grants` (columns `email`,
`normalized_email`, `role`, `scope_type` —
`000001_goatos_clean_slate_baseline.sql:2131-2154`) and a device table with
`push_token_hash`/`fcm_token` (same file, :4951-4962). Both are now readable by the
assistant roles, and `ALTER DEFAULT PRIVILEGES` auto-extends the grant to any table added
later, whether or not it ever enters the coverage matrix.

### Expected Behavior
An LLM-facing read role should be scoped to a coverage-matrix-driven whitelist, or to
`ceo_ai.*` reporting views plus specific Cube-backing tables — matching the coverage
matrix's own philosophy of enumerating exactly what the assistant may read.

### Actual Behavior
Schema-wide blanket grant with automatic future extension.

### Risk / Impact
Widens the blast radius of any bypass in the assistant's own SQL validator: a
prompt-injected or misdirected query could reach PII (emails) and device push tokens that
have no leadership-reporting purpose. The roles being `default_transaction_read_only`
limits writes but not reads.

### Suggested Fix
Replace with an explicit table/view whitelist, or move the needed tables behind `ceo_ai.*`
views and grant only those. At minimum exclude email/token/PII tables from the default-
privilege grant.

### Suggested Tests
Connect as `mesha_ceo_readonly` and assert `SELECT` on `public.auth_pending_email_grants`
and the device push-token table returns `permission denied`. This test fails today.

---

## BUG-019: Vaccination Operators HRMS screen fetches positions with no park scope

Severity: High
Area: Frontend | HRMS
Status: Open
Commits involved: 5833a727, 349750a2, 6b5cb698
Files involved: apps/admin-web/features/people/vaccination-operators-screen.tsx:158-180, :399, :414-479

### Summary
`listStaffPositions` is called with no `scope_type`/`scope_id`, so a multi-park tenant
merges every park's active positions into one roster — while `parkId` (used to load and
save the assignment config) is derived from only `pos[0].scope_id`.

### Evidence
```tsx
const [posRes, capRes, leaveRes] = await Promise.all([
  api.listStaffPositions({ status: 'active', limit: 500 }),
  ...
]);
...
const firstNonBackupOp = pos.find((p) => !p.is_backup_slot);
if (firstNonBackupOp?.workforce_member_id) setDefaultOperator(firstNonBackupOp.workforce_member_id);

// Extract park ID from the first position's scope_id (all positions should be from the same park)
let parkId: string | null = null;
if (pos.length > 0 && pos[0].scope_type === 'center') {
  parkId = pos[0].scope_id;
}
```
The comment "all positions should be from the same park" is an unenforced assumption — the
generated client supports the filter (`packages/api-client/src/generated/admin-api.ts`,
`listStaffPositions.parameters.query: scope_type?: "tenant" | "center"; scope_id?: string`)
and it is not passed. `operatorsList` (:399) is `positions.filter((p) => !p.is_backup_slot)`
with no park filter, feeding `orderedOps`/`weeklyPlan` (:414-479), the roster table, the
"Daily capacity" KPI, and the default-operator dropdown. The repo models CPT and CBE as
separate parks.

### Expected Behavior
Roster, default-operator dropdown, weekly preview, and capacity KPI scoped to the selected
park — the same park the assignment config is read/written against.

### Actual Behavior
All parks blended. `defaultOperator` can initialize to an operator from a different park
than `parkId`, and the capacity KPI sums operators across parks.

### Risk / Impact
Undermines 6b5cb698, whose whole purpose was default-operator selection accuracy. A CEO/PM
can persist a `defaultOperatorId` for park A while looking at a preview built from
park A+B+C.

### Suggested Fix
Pass `scope_type: 'center', scope_id: <selected park id>` from the shell/top-bar park
scope, or filter client-side to the resolved `parkId` before deriving
`operatorsList`/`defaultOperator`.

### Suggested Tests
Seed two parks with distinct rosters; assert park A's `operatorsList` contains none of
park B's positions and `defaultOperator` never resolves outside `parkId`.

---

## BUG-020: `saveCap` renders a fabricated "Saved" success toast with no backend write

Severity: ~~Medium~~ **Low** (downgraded — latent, not live; see ADJ-020)
Area: Frontend
Status: Open (latent)
Commits involved: 5833a727
Files involved: apps/admin-web/features/people/vaccination-operators-screen.tsx:276-289, :632-641

### Summary
The cap Save button is live and performs no API call — it mutates local state and shows a
success toast.

### Evidence
```tsx
// Cap save — NOT YET WIRED (no backend endpoint exists in Phase 2)
const saveCap = async () => {
  const v = Math.max(1, parseInt(capDraft, 10));
  setCapSaving(true);
  try {
    setCommonCap(v);
    setCapEditing(false);
    showToast(`<b style="color:var(--brand)">Saved</b> · cap set to ${v}/day for all operators`);
  } catch (err) {
```
The `Edit` entry point at :632-641 is correctly `disabled` + `aria-disabled` with an
explanatory title ("Common-cap write not yet available"), honoring the repo's
disabled-with-reason rule — but the Save/Cancel/input controls remain in the DOM and
keyboard-reachable, and `saveCap` itself does not honor that contract.

### Expected Behavior
Per `apps/admin-web/AGENTS.md`, a control path with no real write must never render a
fabricated success state.

### Actual Behavior
Unconditional success report with no server round-trip.

### Risk / Impact
Currently gated by the disabled Edit button, so low live exposure — but if a future change
re-enables `Edit` without wiring `saveCap`, operators will believe the per-operator cap
changed when it did not, directly contradicting every operator-cap-fail-closed guard in
this range.

### Suggested Fix
Delete the dead Save/Cancel/input markup until the endpoint exists, or wire `saveCap` to
the real endpoint.

### Suggested Tests
Frontend test asserting `saveCap` invokes a network client method (same grep-style pattern
already used by `positions-panel.test.mjs`).

---

## BUG-021: Four hand-duplicated `buildDomainBus` constructions — the structural root cause of BUG-002

Severity: Medium
Area: Backend | Architecture
Status: **Not counted as a separate product bug** — folded into BUG-002 as its root cause / tech debt (see ADJ-021)
Commits involved: present at `f9a44b84`
Files involved:
  - backend/internal/kernelstages/bus.go:41-64
  - backend/cmd/domain-event-consumer/main.go:114-135
  - backend/internal/domainconsumer/wiring/bus.go:30-52
  - backend/internal/bootstrap/api.go:~522-525

### Summary
"The production domain event bus" is constructed four separate times by copy-paste. BUG-002
is one symptom: `OperatorConfigReplanHandler` was added to two of the four.

### Evidence
The three `BuildDomainBus`-shaped functions are near-identical bodies — same repo/service
construction order, same handler list minus one line — confirming copy-paste rather than a
shared constructor. Registration status of `obligationapp.NewOperatorConfigReplanHandler`:
present in `domainconsumer/wiring/bus.go:46`, `bootstrap/api.go:525`, and
`cmd/outbox-relay/main.go:156`; **absent** in `kernelstages/bus.go` and
`cmd/domain-event-consumer/main.go`.

### Expected Behavior
One canonical constructor called by every production entrypoint.

### Actual Behavior
Four independently maintained copies, already drifted by one handler.

### Risk / Impact
Every future handler addition carries the same silent-drift risk, on a path where a
missing subscriber produces no error (see COUNTER-002).

### Suggested Fix
Delete `kernelstages.BuildDomainBus` and `cmd/domain-event-consumer`'s private
`buildDomainBus`; have all entrypoints call `domainconsumer/wiring.BuildDomainBus`. Add a
guard asserting the subscribed-event-type set is identical across every production bus.

### Suggested Tests
Structural test comparing the subscribed event-type set of each production-built bus
against `domainconsumer/wiring.BuildDomainBus`.

---

## BUG-022: `ceo-ai-boundary-guard` cannot detect schema aliasing, `search_path`, or Go-import indirection

Severity: Medium
Area: Architecture | Tests
Status: **Moved to Needs Investigation** — guard-efficacy gap, no confirmed runtime coupling (see ADJ-022)
Commits involved: 1a6194d7, 5b90dabc, 8b8c013d, 97a6932e
Files involved: tools/agent-hooks/check-ceo-ai-core-boundary.mjs:39-42, :78-96

### Summary
The guard is a literal-token regex scan. It catches the exact historical incident and is
blind by construction to three straightforward evasions.

### Evidence
```js
const SQL_RES = [
  /\bceo_ai\.[a-z_][a-z0-9_]*\s*\(/gi,
  /\b(?:from|join|into|update)\s+ceo_ai[._][a-z0-9_]+/gi,
];
```
None of these match:
- `fmt.Sprintf("SELECT %s.vaccine_label_for(x)", os.Getenv("REPORTING_SCHEMA"))` — schema
  name is never the literal token
- `SET search_path TO ceo_ai, public;` followed by an unqualified call — no `ceo_ai.`
  token in the source at all
- a core package importing a helper from `backend/internal/ceoai/**` that runs the SQL
  internally — the literal string never appears in the core file

`rootFor` (:88-96) classifies files by path prefix only; `execSync("git ls-files")` is the
sole cross-file operation and is used purely for enumeration. There is no import-graph
check anywhere in the 254-line script, so the intended one-way dependency direction
(ceoai → core, never core → ceoai) is unenforced at the Go level.

### Expected Behavior
A guard whose purpose is "core must not depend on ceo_ai" should catch dependency via
indirection, not only the literal pattern from the original incident.

### Actual Behavior
Literal SQL tokens and `/api/ceo-ai` route strings only.

### Risk / Impact
A future regression reintroducing the coupling that caused the CT/AC 500s — for example by
moving the offending SQL into a `ceoai`-owned helper and calling it from core — ships
silently and green.

### Suggested Fix
Add an import-level check failing any file outside `backend/internal/ceoai/` that imports
from `backend/internal/ceoai/**`. Optionally add a CI job that drops the `ceo_ai` schema in
a throwaway DB and re-runs E2E — topology-independent, and catches the config/`search_path`
evasions a static regex cannot.

### Suggested Tests
Self-test fixtures for dynamic-schema `fmt.Sprintf`, `search_path`-then-unqualified, and a
core→ceoai import; assert each is detected once the fix lands.

---

## BUG-023: No automated staleness gate stops a reseed from a non-`origin/main` checkout, despite being a documented automatic-failure condition

Severity: Medium
Area: Seed | Tests
Status: Open
Commits involved: f9a44b84
Files involved:
  - fixtures/vaccination-cpt-operator-drive-2026-07-23/LOCAL_DB_RESEED_VALIDATION.md
  - Makefile (`seed-vaccination-source-full`, ~:690-700)

### Summary
`LOCAL_DB_RESEED_VALIDATION.md` lists "the checkout SHA is not latest `origin/main`" as an
automatic-failure condition. No target, guard, or seed command checks it before mutating
the DB.

### Evidence
```
$ grep -rn "origin/main\|git rev-parse" tools/dev/validate-vaccination-hrms-source.mjs tools/dev/seed-closeout.sh
(no output)
```
The `seed-vaccination-source-full` recipe runs the HRMS-source audit, roster/vaccination
seed, position-duties, and closeout — none check git ref state.

### Expected Behavior
The pipeline refuses to run when HEAD is behind or diverged from `origin/main`.

### Actual Behavior
Purely a human-discipline instruction in a markdown file.

### Risk / Impact
A stale checkout can produce a "clean reseed proof" that silently used out-of-date rule/seed
code — the exact scenario the doc's own automatic-failure list is meant to prevent.

### Suggested Fix
Add `git fetch && git merge-base --is-ancestor origin/main HEAD` (or a HEAD==origin/main
check) as a prerequisite of the seed target.

### Suggested Tests
Guard self-test with a synthetic stale-HEAD fixture that must fail.

---

## BUG-024: `cpt-operator-roster.json`'s `directors` and `leadership_full_access` blocks are silently dropped by `seed-roster-real`

Severity: High
Area: Seed | HRMS
Status: Open
Commits involved: 78f7fe14, f9a44b84
Files involved:
  - backend/cmd/seed-roster-real/main.go:561-589
  - fixtures/vaccination-cpt-operator-drive-2026-07-23/cpt-operator-roster.json

### Summary
The `operatorRosterContract` struct declares only `Schema`, `OperatorCapacity`,
`SourceScope`, `Operators`, and `DefaultOperatorAssignment`. The fixture's top-level
`directors` (Chandrakant) and `leadership_full_access` (5 CXO emails) have no
corresponding fields, and `encoding/json` drops unknown keys silently — no error, no
warning.

### Evidence
`grep -n "Directors\|LeadershipFullAccess" backend/cmd/seed-roster-real/main.go` returns
nothing, while the fixture contains
`"directors": [{"code": "preventive_care_director_chandrakant", ...}]` and
`"leadership_full_access": {"emails": [...]}`. Chandrakant is only ever seeded via the
separate generic jun-26 HRMS path (designation-string matching `"director"`,
`main.go:447-448`) — an unrelated code path. `leadership_full_access.emails` is read by no
Go command at all; actual CXO grants come from `seed-dev-email-grants` driven by the
`GOATOS_DEV_DASHBOARD_ADMIN_EMAILS` env var.

### Expected Behavior
The fixture's own `LOCAL_DB_RESEED_VALIDATION.md` lists Chandrakant and the CXO grants
under "Required HRMS Seed Shape" for this source bundle, so the contract should seed them.

### Actual Behavior
A CPT-only reseed as the README describes seeds neither the director nor the CXO grants.
The "director has zero execution capacity" property holds only incidentally — because he is
never touched by the operator overlay — not as an enforced invariant.

### Risk / Impact
The documented CPT reseed produces an HRMS state that contradicts its own validation doc,
and the director-has-no-capacity rule is unasserted.

### Suggested Fix
Add `Directors` and `LeadershipFullAccess` fields, seed the director with no operator
position / zero `vaccination_daily_animal_cap` and route the emails through the same grant
path as `seed-dev-email-grants` — or fail loud when these keys are present but unconsumed.

### Suggested Tests
Unit test loading a contract with both blocks; assert the director row exists with no
execution capacity and the CXO grants are created. Fails today.

---

## BUG-025: Commit `4a5aa427` shipped to `main` with a broken Android `:app` compile

Severity: Medium
Area: Tests | Android
Status: **Resolved at HEAD** (fixed by `83d4aa5a`) — not counted. Residual CI-classifier question moved to Needs Investigation (see ADJ-025)
Commits involved: 4a5aa427, 83d4aa5a
Files involved: apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt

### Summary
`4a5aa427` used `rememberSaveable` without importing it, breaking the `:app` compile. The
fix landed ten minutes later as a separate commit, `83d4aa5a` — so this range contains a
non-compiling intermediate commit on `main`.

### Evidence
`83d4aa5a` message:
```
fix(mobile): add missing rememberSaveable import in AppNavHost

rememberSaveable is used at AppNavHost.kt:546 (added in #20) but never imported,
breaking the Android :app compile on main.
```

### Expected Behavior
Per the exact-SHA local-CI push gate on `main`, only a complete green `make ci-local` on
that SHA authorizes a push.

### Actual Behavior
A commit that fails the Android compile reached `main` standalone — meaning the Android job
was either not selected by the scoped CI run or the gate was bypassed.

### Risk / Impact
Anyone fetching `main` at `4a5aa427` (CI, another agent, `git bisect`) hits a
non-compiling Android module. This is the exact failure class the gate exists to prevent.

### Suggested Fix
Tighten the CI diff classifier so any change under
`apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/**` always forces the Android
compile job.

### Suggested Tests
Classifier unit test asserting a diff touching `AppNavHost.kt` always selects the Android
job in `tools/ci/run-local-ci.sh`.

---

## BUG-026: `000031` Down migration was not a symmetric revoke as originally authored

Severity: Low
Area: Data Consistency
Status: **Resolved at HEAD** (fixed by `9db24120`) — process note, not counted (see ADJ-026)
Commits involved: 3ef540e3, 9db24120
Files involved: backend/migrations/postgres/000031_assistant_roles_public_read.sql

### Summary
The original Down block revoked SELECT and default privileges but not schema `USAGE`,
leaving the read roles with `USAGE ON SCHEMA public` after rollback. Fixed in the very next
commit.

### Evidence
`git show 9db24120`:
```diff
             EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE SELECT ON TABLES FROM %I', r);
             EXECUTE format('REVOKE SELECT ON ALL TABLES IN SCHEMA public FROM %I', r);
+            EXECUTE format('REVOKE USAGE ON SCHEMA public FROM %I', r);
```

### Expected Behavior
A Down migration is a complete inverse of its Up at merge time.

### Actual Behavior
Now symmetric at HEAD. Recorded because it is evidence that the same authoring pass which
widened privilege scope (BUG-018) did not reason through the rollback path.

### Risk / Impact
None at HEAD. Process signal only.

### Suggested Fix
None beyond what landed; treat privilege migrations as requiring explicit Up/Down symmetry
review.

### Suggested Tests
Up/Down/Up round-trip asserting role-grant state is identical before Up and after Down.

## BUG-027: `drive_operator_cap` under-reports capacity for same-operator, multi-date split cohorts

Severity: **High**
Area: Backend | Data Consistency | Aggregate Grain
Status: Open
Commits involved: introduced/left open at `32e72b64` (found reviewing the cascade cutover, NOT part of the original BUG-001..026 pass)
Files involved:
- backend/internal/processintegrity/adapters/postgres/repository.go

### Summary
The capacity denominator and the assigned-load numerator are computed at **different
grains**. Assigned animals are summed over every row in the split cohort — across all
planned dates — while the operator capacity ceiling is collapsed to **one operator-day per
operator**. A same-operator, two-date split therefore reports the full two-day animal load
against a single day's cap, so CT/PA/WF/AC can render a false over-cap (or mask a real one).

### Evidence
`backend/internal/processintegrity/adapters/postgres/repository.go:938-948` builds the
capacity source by grouping the cohort down to one row per operator and taking the earliest
date:
```sql
SELECT a.operator_id, MIN(a.planned_date) AS planned_date
FROM vaccination_drive_assignments a
WHERE ... AND a.assignment_id = ANY(drive_split.cohort_ids)
GROUP BY a.operator_id
```
`operator_cap` is then `SUM(operator_day.daily_cap)` over that one-row-per-operator set — so
an operator with split work on two dates contributes **one** daily cap, not two.

The numerator does not share that collapse. At `:920`, over the *same* `cohort_ids`:
```sql
COALESCE(SUM(a.animal_count), 0)::int AS assigned_animals
```
which sums every assignment row in the cohort regardless of `planned_date`.

The cohort itself is explicitly allowed to span dates: `drive_split` at `:900-915` matches
siblings on `batch_id`/`shed_id`/`partition_label`/`vaccine_rule_ids` — `planned_date` is
deliberately **not** in the match key, and the comment at `:896-899` states the display date
is a representative `MIN(planned_date)` while "the capacity facts below are aggregated over
the WHOLE cohort".

### Expected Behavior
Capacity must be grouped at `(operator_id, planned_date)` — the real operator-day grain — so
a two-date split contributes two operator-days of cap, matching a numerator that spans two
dates. Numerator and denominator must share one stable group key.

### Actual Behavior
`GROUP BY a.operator_id` alone. Two operator-days of assigned animals are compared against
one operator-day of capacity.

### Risk / Impact
Fabricated `over_cap_required` / `capacity_action` states on legitimately-distributed work,
and the inverse — a genuine over-cap can be hidden when the cap side is understated but the
status is sourced from the stored per-row `capacity_status`. Operators get escalations for
capacity problems that do not exist, which erodes trust in the whole adherence surface.

### Root-Cause Class
Same class as BUG-006/007/008: an aggregate read model whose join grain is not the grain the
consumer assumes. Here it is not a missing `rule_id` predicate but a missing `planned_date`
in the group key. The standing repo rule already covers it — "identify the canonical
membership source, use the same stable group key on producer and consumer" — so this is a
rule that exists and was not applied, not a rule that is missing.

### Suggested Fix
Add `planned_date` to the capacity `GROUP BY` (one row per operator-day), keeping the
`workforce_positions` cap lookup bound to each row's own date as it already is at `:949-957`.
Verify the numerator and denominator are then provably over the same date set.

### Suggested Tests
Integration test: one batch/shed/partition/vaccine cohort split across two dates for the
SAME operator. Assert `operator_cap` equals two operator-days, and that a load which fits
across two days does not report over-cap. Add the inverse case (genuine single-day over-cap
still reports). Add the multi-operator/multi-date matrix so a future refactor cannot silently
re-collapse the key. Carry the `projection-review:` evidence marker per
`.agents/skills/goatos-code-review/references/aggregates-and-projections.md`.

### Note on ownership
Found by the parallel Codex/Claude session reviewing `32e72b64`, independently re-verified
here by direct inspection before being recorded. That session owns
`processintegrity/adapters/postgres/repository.go`; this entry is the ledger record, not a
claim that the fix is in flight on this branch.

---

---

# Adjudication Round (Codex counter → Claude response, 2026-07-24)

Codex reviewed the second pass and issued counters. Recorded verbatim in effect below,
with the resulting disposition. **5 of 6 pushbacks accepted, 1 accepted-with-a-carve-out.**

## Agreed upgrades (no dispute)
Codex accepts COUNTER-001, COUNTER-002, COUNTER-004, COUNTER-006/007 as written. Those
severity changes are now settled: BUG-001 Critical, BUG-002 Critical, BUG-004 High,
BUG-006/007 Critical.

## Agreed new bugs (no dispute)
Codex accepts as real/open: BUG-012, BUG-013, BUG-014, BUG-015, BUG-017, BUG-018,
BUG-019, BUG-023, BUG-024.

## ADJ-016: BUG-016 High → Medium — ACCEPTED
**Codex position:** valid operational gap, but should be Medium or Needs Investigation
unless a stated guarantee exists that a missed `goat.created` must be auto-recovered
periodically.

**Disposition:** Accepted, downgraded to **Medium, Status Open**.

**Reasoning, and the one carve-out:** Codex is right that the *severity* rests on an
unstated policy. I searched for a documented recovery guarantee or SLO for lost
`goat.created` and found none — so rating it High was importing an assumption the repo
does not make, and Medium is the defensible rating.

I am keeping it **Open rather than Needs Investigation**, because the two facts are proven,
not suspected: `goat.created` is the sole SM-1 generation trigger
(`backend/internal/vaccination/app/generation_handler.go:11-12`), and
`backfill-goat-created` has zero non-dev call sites (only
`tools/dev/local-gcp-kernel-parity-smoke.sh:74`,
`tools/dev/vaccination-chain-proof.sh:170`, `tools/e2e/business-chain-driver.sh:128`; no
hit in `.github/workflows`, `Makefile`, or scheduler config). What is unsettled is whether
that gap is *acceptable*, which is a maintainer decision, not a missing investigation. The
open question is recorded as a decision request, not a research task:

> **Decision needed:** is manual-only recovery of a lost `goat.created` acceptable? If yes,
> close BUG-016 as WONTFIX with an ADR. If no, it stays open at Medium.

## ADJ-020: BUG-020 Medium → Low — ACCEPTED
**Codex position:** not a live user-facing bug while `Edit` is disabled; keep as a latent
frontend hazard.

**Disposition:** Accepted. **Severity Low, Status Open (latent).**

**Reasoning:** Correct. The entry point is `disabled` + `aria-disabled` with a reason
string (`vaccination-operators-screen.tsx:632-641`), so no user reaches `saveCap` through
normal navigation today. My original Medium over-weighted keyboard reachability of the
still-mounted Save/Cancel controls. That keeps it from being *fixed-and-closed*, but it
does not make it a live defect. Low/latent is right.

## ADJ-021: BUG-021 not a separate product bug — ACCEPTED
**Codex position:** real architectural root cause of BUG-002, but mostly duplicate/tech
debt; should not be counted separately.

**Disposition:** Accepted. **Folded into BUG-002 as its root cause. Not counted.**

**Reasoning:** Agreed — counting both double-counts one defect. The four duplicated bus
constructions have no user-visible symptom independent of BUG-002. Retained in the ledger
as the *fix direction* for BUG-002 (consolidate to one constructor + a set-equality guard),
because fixing only the missing registration line leaves the drift mechanism intact.

## ADJ-022: BUG-022 → Needs Investigation — ACCEPTED
**Codex position:** hypothetical guard evasion, not a confirmed runtime bug.

**Disposition:** Accepted. **Moved to Needs Investigation (Tests/Architecture).**

**Reasoning:** Correct on the standard this review set for itself — "every bug needs code
evidence, uncertain items go to Needs Investigation." I have evidence the guard *cannot*
catch three evasion classes, but no evidence any of them is present in the tree. That is a
proven weakness in a control, not a proven defect in the product. Reclassified.

## ADJ-025: BUG-025 → Resolved, with a carve-out — ACCEPTED WITH CARVE-OUT
**Codex position:** historical broken intermediate commit, already fixed at HEAD; do not
keep as Open.

**Disposition:** **BUG-025 closed as Resolved at HEAD** (fixed by `83d4aa5a`), not counted.

**Carve-out:** the *commit* is fixed; the *gate that let it through* has not been shown to
be fixed. Per `AGENTS.md`, only a green `make ci-local` on the exact SHA authorizes a push
to `main`, so either the Android job was not selected for `4a5aa427`'s diff or the gate was
bypassed. I could not determine which from a read-only pass (no CI run history available).
That question is now recorded under Needs Investigation rather than as an open bug — which
respects Codex's point that a fixed commit is not an open defect, while not silently
dropping the possibility that the classifier is still mis-scoping Android diffs today.

## ADJ-026: BUG-026 → Resolved/process note — ACCEPTED
**Codex position:** Claude's own entry says no risk at HEAD; should be Resolved, not Open.

**Disposition:** Accepted. **Resolved at HEAD** (fixed by `9db24120`), retained as a
process note only, not counted.

**Reasoning:** Agreed, and my original entry contradicted itself by stating "None at HEAD.
Process signal only" under a `Status: Open` header. Corrected.

## Reconciled Open Count

Codex estimated ~21 open bugs vs my 26. After this round the two counts reconcile exactly:

| | Count |
|---|---|
| First pass (BUG-001..011) | 11 |
| Second pass, retained open (BUG-012..020, 023, 024) | 11 |
| Subtotal | 22 |
| Less BUG-006 + BUG-007 merged into one finding (per COUNTER-006/007) | −1 |
| **Reconciled open total** | **21** |

Not counted: BUG-021 (folded into BUG-002), BUG-022 (Needs Investigation),
BUG-025 (Resolved at HEAD), BUG-026 (Resolved at HEAD).

The gap between "26" and "21" was three disposition disagreements plus one merge I had
myself recommended and then failed to apply to my own tally. Codex's number was the right
one.

## ADJ-008: BUG-008 CONFIRMED, scope narrowed to `targets.go` — Claude's hedge WITHDRAWN

**Codex position:** the `targets.go` leak is no longer unconfirmed. `matched_batches`
selects a batch when *any* assignment for that batch falls on the queried date, then
includes every obligation from that batch with no `vaccine_rule_ids`/`rule_id` filter.
Claude was right that the scope is `targets.go`, not `canonical_read.go`, but wrong to
leave it unconfirmed.

**Disposition:** Accepted. **BUG-008 stays Open/High, counted**, with its title and
Files-involved re-scoped to `targets.go`. My COUNTER-008 hedge is withdrawn.

**Independently verified** in this round —
`backend/internal/calendar/adapters/postgres/targets.go:44-47` matches the batch on the
mere *presence* of an assignment row for the queried date:
```sql
LEFT JOIN vaccination_drive_assignments vda
    ON vda.tenant_id = ob.tenant_id
   AND vda.batch_id = ob.batch_id
   AND vda.planned_date = $3::date
WHERE ...
    AND (
      vda.assignment_id IS NOT NULL
```
and `:212` then admits every obligation on that batch, unfiltered by rule:
```sql
oi.batch_id IN (SELECT batch_id FROM matched_batches)
```
After `splitVaccinationDriveAssignmentsForDateOverrideTx` produces two rows for one batch
(original date + override date), querying **either** date matches the batch and pulls in
the sibling vaccine's obligations. Same missing-`rule_id` root cause as BUG-006/007, third
distinct location. Fix direction is identical: intersect
`assignment.vaccine_rule_ids` with `oi.rule_id`, per the pattern already correct in
`canonical_read.go:578-620`.

## ADJ-ROLLUP: High count typo corrected — my error

**Codex position:** the roll-up table said High = 13 while listing 14 IDs.

**Disposition:** Accepted, corrected below. My arithmetic error — I wrote 13 while
hedging BUG-008 out of the count, then listed it anyway. With BUG-008 confirmed (ADJ-008)
the correct figure is unambiguously 14, and the 21 total is unchanged.

## Severity Roll-Up (post-adjudication, 21 open) — CORRECTED

| Severity | Count | IDs |
|---|---|---|
| Critical | 3 | BUG-001, BUG-002, BUG-006/007 (merged) |
| High | 14 | BUG-003, BUG-004, BUG-005, BUG-008, BUG-009, BUG-010, BUG-012, BUG-013, BUG-014, BUG-015, BUG-017, BUG-018, BUG-019, BUG-024 |
| Medium | 3 | BUG-011, BUG-016, BUG-023 |
| Low | 1 | BUG-020 (latent) |
| **Total** | **21** | |

## Ledger Hygiene (applied)

Top-level `Severity:` fields on BUG-001, BUG-002, BUG-006, and BUG-007 have been
normalized in-place to **Critical**, so an implementer reading the bug header alone sees
the adjudicated priority without having to reach the counters section. BUG-006 and BUG-007
carry an explicit "merged for counting" note. BUG-008's header now states the confirmed
status and the `targets.go` scope.

**Missing-`rule_id` cluster.** BUG-006, BUG-007, and BUG-008 are now confirmed to be three
locations of one root cause: an assignment lookup that does not filter by the obligation's
own vaccine rule. They should be fixed as a single change with one shared predicate, and
regression-tested together against a per-vaccine override split.

## Fix-First Ordering (suggested)

1. **BUG-002** — every HRMS cascade is a silent no-op in production; fix via BUG-021's
   consolidation, not a one-line patch.
2. **BUG-006/007 + BUG-008** — one shared fix: add the `rule_id` predicate to the 7+
   LATERAL joins *and* to `targets.go`'s `matched_batches`; the correct pattern already
   exists in `canonical_read.go:578-620`. Regression-test all three together against a
   per-vaccine override split.
3. **BUG-001 + BUG-012** — persist the distributed plan; make the override path cap-aware.
   Same invariant, two write paths.
4. **BUG-005 + BUG-013** — exit correctness: route procurement through `goat.exited`, and
   decrement `animal_count` on exit/shift.
5. **BUG-009 + BUG-010 + BUG-024 + BUG-023** — make the CPT reseed runnable and
   self-proving before it is used to validate anything else.

---

# Branch-Note Verification Round (Claude, 2026-07-24)

Codex added three branch follow-up notes (BUG-003, BUG-007, BUG-013) citing
`fix/operator-cascade-full-cutover-20260724`. Those notes are hedged with "reportedly".
This round removes the hedge where the branch code supports it.

**Branch confirmed to exist:** `fix/operator-cascade-full-cutover-20260724` @ `392e4e15`,
**6 commits ahead of `main`**, not merged. `main` is still `f9a44b84`. The symbol
`removeGoatFromDriveAssignmentsTx` exists on that branch only — a `git grep` across `main`
returns nothing, which is consistent with the original BUG-013 finding (no decrement
existed at `f9a44b84`).

## ADJ-013-BRANCH: Codex's BUG-013 branch note is CONFIRMED — and is stronger than "reportedly"

**Codex position:** `removeGoatFromDriveAssignmentsTx` decrements by only
`batch_id + shed_id`, which is unsafe because rows split by `partition_label`,
`operator_id`, `planned_date`, and `vaccine_rule_ids`.

**Disposition:** **Confirmed by direct inspection.** Upgrade the wording from "reportedly
… can subtract from multiple rows" to **"guaranteed to over-subtract whenever a
batch/shed has more than one assignment row"**, which is the normal case in the CPT
fixture, not an edge case.

**Evidence.** The key struct carries exactly two fields
(`repository.go:3664-3667` on that branch):
```go
type driveAssignmentRemovalKey struct {
	batchID string
	shedID  string
}
```
and the UPDATE predicate matches on nothing more:
```sql
WHERE vda.tenant_id = $1
  AND vda.batch_id = removal.batch_id
  AND COALESCE(vda.shed_id, '00000000-0000-0000-0000-000000000000'::uuid) = removal.shed_key
```
But the table's own uniqueness grain is eight columns
(`backend/migrations/postgres/000022_vaccination_drive_assignment_operator_grain.sql:4-14`):
```sql
CREATE UNIQUE INDEX IF NOT EXISTS vaccination_drive_assignments_batch_shed_part_operator_uq
  ON public.vaccination_drive_assignments (
    tenant_id, batch_id, planned_date, park_id,
    COALESCE(shed_id, '00000000-...-0'::uuid),
    physical_shed, partition_label,
    COALESCE(operator_id, '00000000-...-0'::uuid));
```
So `(batch_id, shed_id)` is **not** the row identity — `planned_date`, `physical_shed`,
`partition_label`, and `operator_id` are all part of it. The UPDATE therefore decrements
`animal_count` by 1 **and** subtracts the same `doses` from **every** matching row.

Three live multiplicities make this fire in practice:
1. **Partitions.** `driveAssignmentsForUnbatched` buckets by `partition_label`, and the CPT
   fixture itself models partitioned sheds (`Godel 1 - Part 1`, `Part 2`). A two-partition
   shed means one exiting goat subtracts 1 animal from **both** partition rows — one of
   which never contained that goat. This needs no override and no multi-operator split; it
   is the fixture's normal shape.
2. **Override splits.** Per BUG-012, an override writes a second row for the same
   batch/shed on a different `planned_date` with different `vaccine_rule_ids`. One exit
   then decrements the moved PPR row *and* the un-moved ET+TT row.
3. **Operator splits.** Once BUG-001 is fixed and the distributed plan is actually
   persisted, rows will differ by `operator_id` for the same batch/shed — multiplying the
   error further.

**Self-invalidating justification.** The helper's own doc comment defends the two-field key
like this (`repository.go:3660-3663`):
```go
// the planners bucket by (batch, shed scope) and stamp the batch's single
// conducted_by operator on every row of that batch, so batch + shed scope is the row's identity.
```
That premise — one `conducted_by` operator stamped on every row — **is BUG-001**. So this
repair is correct only for as long as the cap bug it sits next to remains unfixed, and
fixing BUG-001 silently breaks it. That coupling should be recorded on both findings:
**BUG-001's fix must land together with a re-key of `driveAssignmentRemovalKey`**, not
before it and not after.

**Additional finding in the same helper (not in Codex's note).** The follow-up DELETE is
scoped to the batch with no shed/partition predicate:
```sql
DELETE FROM vaccination_drive_assignments
WHERE tenant_id = $1
  AND batch_id = ANY($2::uuid[])
  AND animal_count = 0
```
It sweeps every zero-count row on the affected batches, including rows for sheds/partitions
the exiting goat never belonged to that were already at zero from an earlier operation.
Impact is smaller than the double-decrement (a zero-count row is phantom work either way,
as the comment argues), but the blast radius is wider than the removal key it is paired
with. Severity: Low. Worth tightening to the same exact key once that key is correct.

**Net:** BUG-013 stays **Open / High**. The branch does not close it; it narrows it and
introduces a correctness regression of its own. Recommend the branch not land as-is.

## ADJ-007-BRANCH: Codex's BUG-007 branch note is ACCEPTED, and it identifies a real design gap
**Codex position:** even with a stronger binding patch, process-integrity still selects one
row via ranking + `LIMIT 1`; because `vaccination_drive_assignments` has no goat-level
membership, a goat in a same-partition split cannot be bound back to the correct row.

**Disposition:** Accepted. This is a genuine escalation of BUG-007 rather than a restatement.

**Reasoning.** My COUNTER-006/007 prescribed adding the `rule_id` predicate, which fixes
the *vaccine* dimension. Codex is right that it does **not** fix the *operator/date*
dimension: once one partition's work is split across two operators or two dates, the rule
predicate matches both rows equally and `LIMIT 1` still picks arbitrarily. The schema
carries `animal_count` (a number), not membership (which animals), so the information
needed to bind a goat to its split row does not exist in the table at all.

This means the missing-`rule_id` cluster fix is **necessary but not sufficient**. The full
fix needs an exact membership source — goat-level assignment membership or an equivalent
deterministic ledger. Recorded as the second half of BUG-007, and it raises the cost
estimate for that cluster considerably.

## ADJ-003-BRANCH: Codex's BUG-003 branch note is ACCEPTED as written
**Codex position:** the branch's partial fix fans capacity changes out only through
`vaccination_operator_assignment_config` rows, missing parks that have future drives but no
operator-config row.

**Disposition:** Accepted; no correction. The reasoning is sound on its face — a fan-out
driven by config-table membership cannot reach parks absent from that table, and those are
exactly the fallback/no-config parks. I did not independently re-derive the branch's
fan-out query, so this note stands on Codex's evidence, not mine; flagged as such rather
than double-counted as confirmed.

**BUG-003 remains Open / High** in the narrower form Codex states: the cascade must cover
all parks with future drives, not only parks that already have operator config rows.

## ADJ-SCOPE: Branch blockers are NOT shipped-main bugs — agreed, and recorded structurally

Both reviewers agree on a distinction the ledger must not blur:

| | Applies to | Counted in the 21? |
|---|---|---|
| **BUG-001..BUG-026** | shipped `main` @ `f9a44b84` | Yes (21 open) |
| **Branch follow-up notes** on BUG-003 / BUG-007 / BUG-013 | `fix/operator-cascade-full-cutover-20260724` @ `392e4e15`, **unmerged** | **No** — they are landing blockers for that branch |

`main` is still at the older broken behavior described by the parent findings. The branch
notes describe defects in an *attempted repair* that has not shipped. They must not be
counted as additional main bugs, and equally must not be treated as closing their parent
findings.

### Branch Blocker Register — `fix/operator-cascade-full-cutover-20260724` @ `392e4e15`
Blocks landing until resolved:
- **BB-1 (from BUG-013):** `driveAssignmentRemovalKey{batchID, shedID}` under-keys an
  eight-column row grain → guaranteed over-subtraction on any multi-row batch/shed.
  Re-key to the full assignment identity.
- **BB-2 (from BUG-013):** zero-count `DELETE` scoped to `batch_id` only → sweeps rows for
  sheds/partitions the exited goat never held. Scope it to the corrected key.
- **BB-3 (from BUG-007):** process-integrity still resolves one row via ranking +
  `LIMIT 1`; the rule predicate fixes the vaccine dimension but not same-partition
  operator/date splits.
- **BB-4 (from BUG-003):** capacity fan-out driven by
  `vaccination_operator_assignment_config` membership misses fallback/no-config parks that
  have future drives.
- **BB-5 (cross-cutting):** BB-1's fix is coupled to BUG-001 — the helper's own comment
  justifies the short key by "the planners stamp the batch's single `conducted_by`
  operator on every row", which is precisely the behavior BUG-001 says must change.
  Landing BUG-001's fix without re-keying re-breaks the repair.

### Partial membership source for BB-1 — `goat_shed_partitions` (current partition ONLY)
**Scope caution (agreed by both reviewers):** this table supplies the goat's *current* shed
partition and nothing else. It **does not** solve historical/as-of assignment membership,
operator split, date split, or vaccine-rule split. It is a partial input to BB-1, not a
solution to BB-1 or BB-3. Do not read the paragraph below as "membership is available".

Codex's shed-partition lens points at the right table, and it checks out —
`backend/migrations/postgres/000023_goat_shed_partitions.sql:2-9`:
```sql
CREATE TABLE IF NOT EXISTS goat_shed_partitions (
  tenant_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  partition_label text NOT NULL DEFAULT 'whole',
  source_shed_name text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, goat_id),
```
This gives per-goat `partition_label`, which is one of the missing key columns — so BB-1
can be fixed by joining it into the removal key rather than inventing new state. Two
caveats for whoever implements it:
1. `PRIMARY KEY (tenant_id, goat_id)` means it holds the goat's **current** partition, with
   no history. A repair must reconstruct membership *as of the planned assignment*, not as
   of now; for an exiting goat these usually coincide, but for a shed-shift they do not —
   the shift itself changes the row.
2. It supplies `partition_label` and `shed_id` only. `operator_id` and `planned_date`
   remain unresolvable per-goat from this table, so BB-3's same-partition operator/date
   split still needs a separate assignment-membership ledger. `goat_shed_partitions`
   narrows the gap; it does not close it.

## Effect on Counts
None. All three notes narrow or sharpen existing findings; none adds or removes a bug.
**Open total remains 21** (3 Critical / 14 High / 3 Medium / 1 Low). The branch
`fix/operator-cascade-full-cutover-20260724` is unmerged, so `main` @ `f9a44b84` is
unchanged and every finding above still describes shipped code.

**New cross-finding constraint recorded:** BUG-001 and BUG-013 are now coupled — fixing
BUG-001 (persisting per-operator splits) invalidates the `(batch_id, shed_id)` removal key
the branch's BUG-013 repair relies on. They must be fixed in one change.

---

# Review Coverage Summary

## Commit Range Reviewed
From: `dfb420c3`
To: `f9a44b84` (80 commits, plus current working tree at that SHA)

Two independent passes: the first produced BUG-001..BUG-011; the second (Claude, 2026-07-24)
produced BUG-012..BUG-026 plus COUNTER-001/002/004/006/007/008/011 above.

## Areas Covered
- Vaccination scheduler, operator capacity planner, and cap-aware assignment persistence
- Operator-config / HRMS roster / leave / capacity cascade and production event-bus wiring
- Durable recompute, outbox relay, and consumer registration across all four bus constructions
- Vaccine drive-date overrides and PPR / ET+TT separation
- Goat lifecycle: creation, procurement intake, health change, shed shift, death/sold/culled/exited
- Obligation generation, sweep horizon, reopen/recovery, stale-lock policy
- Downstream read models: Execution, ShedDrilldown, ScanRoster, Gaps, Coverage, Process
  Integrity, Calendar
- CPT seed/reseed contract validated against `/Users/ravi/mesha/wiki/CPT-Adult-goats.json`,
  `CPT-Adult-vaccination.json`, `CPT_Nuanced Timetable.xlsx`, and
  `fixtures/vaccination-cpt-operator-drive-2026-07-23/`
- admin-web HRMS/operator and Counts screens; Android nav, proof binding, Counts screens, i18n
- Test-integrity guards (silently-skipped-test detectors, cap fail-closed guard)
- ceo_ai / leadership-assistant coupling boundary, guards, exclusion lists, and grant scripts

## HRMS / Operator Scheduling Rules Checked
- Cap is distinct animals, not doses; ET+TT 2 doses = 1 animal — **violated in persisted
  data** (COUNTER-001): selection ceiling is the sum of all operators' caps while
  persistence attributes the whole batch to one operator.
- Cap enforced per operator/date across all vaccine families and batches — **not enforced
  per operator**; session bookkeeping tracks (park, date) aggregate only.
- Default operator / N / week-offs HRMS-driven, not hardcoded — config path is HRMS-driven;
  no hardcoded operator names or caps found in backend or FE.
- Cap / N / default-operator / week-off / status / leave changes emit durable events —
  only the operator-assignment-config write emits (BUG-003, BUG-004/COUNTER-004, BUG-014).
- Leave cascade at effective state, not "reported" — **violated** (BUG-014).
- Emitted cascade actually consumed in the deployed runtime — **violated**
  (BUG-002/COUNTER-002); silent ack, no retry.
- Recompute idempotent and retriable — verified sound (two-phase watermark).
- CPT operator model (Amit / Darshan / Sagar, Darshan default, N=1, Sagar then Amit
  fallback, Fri/Sun/Sat offs, Chandrakant director-only) — fixture matches, but director
  and CXO blocks are never consumed by the seeder (BUG-024).

## Vaccination / Obligation Lifecycle Rules Checked
- New goat creates correct obligations — verified; procured goats use the same SM-1 path.
- Trusted history not duplicated / canonicalized or review-gated — **violated** (BUG-017).
- Closed/resolved health must not become recovering — verified correct; `normalizeHealth`
  (Closed→healthy, Fine→healthy, Recovering→recovering) is isolated to the seed tool and
  consistent with `clinical_defer.go`. Live writes go through the fixed enum.
- Recovery reopens/replans under cap — verified; reopen sets `batch_id = NULL`,
  `status = 'scheduled'`, re-entering the normal capped sweep path. No bypass.
- Shed/location shift rescopes obligations — obligations yes, drive-assignment
  `animal_count` no (BUG-013).
- Death/sold/culled/exited cancels obligations and removes future planned drive rows —
  cancels yes, drive rows no (BUG-013); and procurement's terminal decision bypasses the
  `goat.exited` spine entirely (BUG-005, confirmed).
- No undocumented multi-year pre-materialization — verified. `buildSweepConfig` bounds
  `DueBefore` to end of current IST business day (documented, PEND-6), and future revac is
  generated per-dose off `after_previous_completion`.
- Stale in_progress policy matches documented policy — **violated** (BUG-015).
- Missed `goat.created` recovered automatically — **violated** (BUG-016).

## UI / Android / Read Model Consistency Checked
- Canonical date priority (assignment.planned_date → batch.planned_date → due_at) uniform
  across surfaces — **violated** (COUNTER-006/007): 7+ read paths omit the `rule_id`
  predicate and deterministically resolve to the pre-override date; `conducted_by` is wrong
  on the same rows.
- Calendar `canonical_read.go` and `/vaccination/drive-assignments` are the two correct
  implementations — the fix pattern already exists in-tree.
- Process integrity picking first assignment row on multi-row batch/shed — confirmed
  (`ORDER BY ... LIMIT 1`, no rule scope).
- Capacity/load fields sourced from assignment rows — yes, but those rows are written by
  the uncapped override path (BUG-012) and the single-operator sweep path (COUNTER-001).
- admin-web operator screen park scoping — **violated** (BUG-019); fake success toast
  (BUG-020).
- Android Counts screens — Paging 3 + `RemoteMediator`, Room-backed, totals from the
  backend envelope; no unbounded-fetch or pagination-rule violation found in the diffed
  files.
- Android compile integrity in-range — one non-compiling commit (BUG-025).

## Seed / Reseed Contract Checked
- One canonical CPT reseed command — **absent** (BUG-009 confirmed); default
  `GOATOS_VACCINATION_SOURCE_DIR` points at `fixtures/vaccination-hrms-source-full`, and no
  Makefile target names CPT.
- Fixture is actually feedable to the seeder — **no**: `seed-vaccination-real` requires
  `<source>/goats.json` and `<source>/vaccination.json`; the fixture has
  `raw/CPT-Adult-goats.json` and `raw/CPT-Adult-vaccination.json` (BUG-009). Those raw
  files were `diff`-verified byte-identical to the wiki source, so this is a wiring gap,
  not content drift.
- Seeds all three operators and the director — operators yes, director/CXO no (BUG-024).
- Refuses stale / non-`origin/main` code before DB mutation — **no** (BUG-023).
- Validates actual DB rows, not prose/screenshots — **no** (BUG-010 confirmed): the
  `operator-cap-fail-closed-guard` is a `node` source-shape lint, and the
  `having count(distinct target_id) > 200` proof query in `LOCAL_DB_RESEED_VALIDATION.md`
  is unexecuted prose with no script wrapping it.
- Fails if PPR appears on 2026-07-24/25, or any operator/date exceeds cap — no automated
  assertion exists for either.
- Fails if tests report PASS with zero tests run — the stray-build-tag detector
  (`6098334a`) is real, but is generic and does not cover the CPT DB-proof path.

## Cross-Feature Coupling Checked
- CT / PA / vaccination / calendar / dashboards depending on ceo_ai — no live coupling
  found in backend core or admin-web; the `6f1e621c` decoupling holds.
- Guard enforcement quality — literal-token only; blind to aliasing, `search_path`, and
  core→ceoai Go imports (BUG-022).
- Exclusion lists used to silence the coverage guard rather than decouple — reviewed each
  exclusion added in range; all are assistant-coverage annotations, not operational
  surfaces being hidden. No finding.
- Assistant grant scope — over-broad (BUG-018).

## What Can Break
1. **Operator over-scheduling.** One operator's persisted day load can reach the combined
   cap of every operator (COUNTER-001), and the override endpoint can stack more on top
   (BUG-012), with no surface warning.
2. **PPR shows on the wrong date almost everywhere.** After the 2026-08-07 move, Execution,
   ShedDrilldown, Android roster, Gaps, Coverage, and Process Integrity keep showing
   2026-07-24/25 (COUNTER-006/007) — the specific outcome the business rule forbids.
3. **Every HRMS capacity/roster/leave change is a silent no-op in production.** The event
   is enqueued, relayed, delivered, and dropped without error (COUNTER-002); leave also
   fires at the wrong transition (BUG-014); position-field changes emit nothing at all
   (COUNTER-004).
4. **Drive rosters over-count.** Dead/sold/moved goats stay in
   `vaccination_drive_assignments.animal_count` (BUG-013), and procurement exits never
   reach the cancel path at all (BUG-005).
5. **Silent permanent stalls.** An abandoned partial drive strands its shed in
   `in_progress` forever, invisible to missed reporting (BUG-015); a lost `goat.created`
   leaves a goat with zero obligations until a human notices (BUG-016).
6. **Duplicate vaccination of procured adults**, because supplier history is captured and
   discarded (BUG-017).
7. **The CPT reseed cannot be run as documented** (BUG-009/BUG-024) and, if run, proves
   nothing about caps or dates (BUG-010).

## Needs Investigation
- **`ceo-ai-boundary-guard` evasion classes (was BUG-022, moved here per ADJ-022).** The
  guard provably cannot detect dynamic-schema `fmt.Sprintf`, `SET search_path` +
  unqualified calls, or core→`backend/internal/ceoai/**` Go imports
  (`tools/agent-hooks/check-ceo-ai-core-boundary.mjs:39-42`, `:78-96`). No instance of any
  evasion was found in the tree — this is a control weakness, not a confirmed defect.
  Needs a decision on whether to harden the guard.
- **CI diff-classifier scoping for Android (carve-out from ADJ-025).** BUG-025's commit is
  fixed at HEAD, but it is not established *why* a non-compiling `:app` change passed the
  exact-SHA `make ci-local` gate — misclassified diff vs bypassed gate. Determining this
  requires CI run history, unavailable to a read-only pass. If the classifier does not
  force the Android job for changes under
  `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/**`, the same class of break
  can recur.
- **Decision request (BUG-016, per ADJ-016).** Is manual-only recovery of a lost
  `goat.created` acceptable? No documented guarantee or SLO exists either way. If
  acceptable, close BUG-016 WONTFIX with an ADR; if not, it stays Open at Medium.
- **Revac interval constants.** ET+TT 182d / FMD 274d / BT, Goat Pox, HS, Sheep Pox 365d /
  PPR 1095d were not exhaustively reconciled against every definition site (Go constant vs
  seed JSON vs migration). No mismatch surfaced, but no positive confirmation either —
  needs a dedicated single-purpose pass.
- **`splitLatestSafeGroupAcrossOperators` / `over_cap_required_latest_safe`**
  (`operator_drive_planner.go:207-241`) deliberately exceeds a per-operator cap on the last
  clinically safe day, labeled `CapacityStatus = "over_cap_required"` with a warning. Looks
  like intentional policy; needs maintainer confirmation that it is approved rather than a
  residual gap.
- **Cloud/prod grant timing.** `000031` is `IF EXISTS`-guarded and no-ops if the roles do
  not yet exist; no CI/CD workflow invokes `grant-assistant-public-read.sh`. Whether the
  manual re-run is wired into the stg/prod deploy pipeline could not be determined from the
  repo.
- **`ceoai/sqlguard/validator.go` bypass surface.** `executor.go:70-72` forces a
  `search_path` as defence-in-depth; whether a fully schema-qualified `public.*` query can
  evade the validator was not audited, and BUG-018 raises the stakes of any bypass.
- **`UpsertBackupConfig`** (`roster_service.go:326`) — no outbox insert in its repository
  counterpart; whether a backup-holder change *should* cascade was not settled.
- **`AdminGoatCreateRequest.VaccinationHistory []EvidenceRef`** — declared and unreferenced;
  needs a dead-code determination separate from BUG-017.
- **Outbox DLQ/alerting.** `OperatorConfigReplanHandler` errors look retriable via the
  two-phase watermark, but the relay's `MaxAttempts`/dead-letter threshold and whether a
  stuck PENDING watermark ever surfaces to a human was not traced end-to-end.
- **`applyStaffLeave` optimistic merge** in the operators screen updates local state rather
  than refetching; no test compares the optimistic value against what the server persisted.
- **Android Room DAO audit.** The diffed Counts files are clean; a full DAO-level audit of
  every Room query added in this range was not completed.
- **`visit_shot_lock.go` connection leak** on a panic path outside the documented `defer` —
  not evidenced, not exhaustively traced.
- **Shed/partition normalization** (`Godel 1 - Part 1` → shed + partition) documented in the
  fixture README was not traced into `seed-vaccination-real`'s parsing code.
- **Live CPT sweep output** vs `expected-drive-schedules.json` (the 200/124 ET+TT split and
  the 2026-08-07/08 PPR split) — not executed; read-only review performed no DB mutation.

## Commands / Tests Run
Read-only only. No fixes implemented, no refactors, no DB mutation, no test execution
against a live database.
- `git log --oneline -80`, `git show <sha>`, `git rev-parse HEAD` for range and per-commit diffs
- `grep` / `rg` / `ls` / `sed` / `diff` across `backend/`, `apps/admin-web/`,
  `apps/goatos-android/`, `tools/`, `fixtures/`, `docs/`, `deploy/`, `.github/workflows`
- `diff` of `fixtures/vaccination-cpt-operator-drive-2026-07-23/raw/CPT-Adult-goats.json`
  and `CPT-Adult-vaccination.json` against the `/Users/ravi/mesha/wiki/` originals — both
  byte-identical
- Direct reads of migrations `000001`, `000020`, `000028`, `000031`, `000035`, `000036-000039`

## Remaining Gaps
- No live-DB verification of any finding; all evidence is static (code, SQL text,
  migrations, fixtures, commit messages).
- No CPT reseed executed, so the expected-drive-schedule claims remain unproven in either
  direction.
- Revac interval reconciliation incomplete (see Needs Investigation).
- Android DTO ↔ backend struct field-by-field parity, and OpenAPI ↔ frontend/mobile usage
  parity, were only spot-checked — a systematic contract diff was not performed.
- The exclusion lists on the leadership-assistant coverage guard were judged individually
  but not cross-checked against the full coverage matrix.
