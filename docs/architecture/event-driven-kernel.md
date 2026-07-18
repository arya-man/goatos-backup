# Event-Driven Kernel: Predefined vs Dynamic Events

Date: 2026-07-10

Purpose: give every Goat OS event a single, named place in the operational
kernel (`context/architecture/operational-kernel.md`), split into the two
shapes that actually exist in the codebase today (`backend/internal/vaccination/app/generation.go`,
`backend/internal/vaccination/app/schedule_policy.go`, `backend/internal/obligation/app/*.go`),
and ground the taxonomy in `docs/preventive-care-vaccination/vaccination-rules.md`.
This document is design/reference only — it does not introduce new tables or
change kernel code. Where behavior is asserted below, it is proven by the E2E
kernel-story suite at `backend/tests/e2e/` (see the story IDs cited per event).

## Why two event classes

Every Preventive Care (PC) vaccination event is one of two shapes, and the
shape decides how the generation engine treats it:

- **PREDEFINED** — the event's future obligation dates are knowable the moment
  the event is recorded, because the schedule is a fixed offset table
  (`docs/preventive-care-vaccination/vaccination-rules.md`'s "V1 Source Matrix
  Presets"). The generation engine computes `due_at` once from the anchor date
  (DOB, farm-entry date, or prior completion date) and materializes every row
  up front. Nothing about the *future* changes unless a dynamic event interrupts it.
- **DYNAMIC** — the event is a state transition on an *existing* goat/schedule
  that the generation engine cannot know in advance: it defers, reopens,
  re-scopes, or permanently cancels obligations that a predefined event already
  created. Dynamic events never invent new dose rows; they only change the
  status/scope/due date of rows a predefined event already materialized.

This maps directly onto `genOneGoat` in `generation.go`: every rule row is
first given a **base due date** (predefined math: `dueAt()`), then a **defer
verdict** is layered on top (`deferredReason` for defer_states, `policyDeferReason`
for pregnancy/milking/post-breeding/warm-up), and finally a **terminal
transition** (cancel on exit) can end the row's life outside the schedule
entirely.

## PREDEFINED events

### Birth (kid registered with DOB)

- **Class:** predefined.
- **Trigger:** a goat row is created/updated with `origin_type='birth'` (or a
  DOB inside the kid window) and a shed/stage that resolves to the kid path
  (`schedulePathForGoat` in `schedule_policy.go`; kid stage codes are
  `K`-prefixed).
- **Chain reaction:** `GenerationService.GenerateForGoat`/`GenerateForVersion`
  materializes the **full kid dose bundle** in one pass — every `birth_age`
  rule row is inserted as `scheduled`, each due at `DOB + rule.OffsetDays`,
  regardless of whether that date is still in the future. Per
  `docs/preventive-care-vaccination/vaccination-rules.md`'s V1 effective days:
  ET+TT #1 (28d/4w), ET+TT #2 (49d/7w), FMD+HS (84d/12w), PPR (112d/16w), Goat
  Pox (140d/20w). No notification/escalation fires at birth itself; the
  Calendar/Action Center pick up each row once the sweeper's due-window logic
  makes it visible. Proven by story-g (`story_g_kid_bundle_test.go`) and the
  buffer-specific story-o (`story_o_birth_tagging_buffer_test.go`).
- **24-hour tagging buffer:** operationally, a newborn kid cannot be RFID-tagged
  and enrolled in the schedule until at least 24 hours after birth (tagging/ID
  window). Because every kid rule's minimum offset is 28 days (ET+TT at 4
  weeks), the 24-hour floor is **structurally satisfied today by the schedule
  itself** — no kid dose is ever due inside 24h of DOB. There is **no explicit
  `due_at >= DOB + 24h` clamp in the generation engine**; it is a coincidence of
  the current rule table, not an enforced invariant. See Open Questions below —
  this is flagged, not silently assumed safe forever.
- **Idempotency + Asia/Kolkata:** the idempotency key is
  `obligationKey(tenant, version, rule, "goat", goatID, keyDue, sequence)`
  where `keyDue = baseDue` (rule-offset date, stable across rechecks — see
  `obligationKeyDue`), so re-running generation for the same kid is a pure
  no-op (`InsertObligation`'s `applied=false` branch). All due dates are
  computed via `biztime.BusinessDayStart`, i.e. Asia/Kolkata business-day
  starts, never raw UTC midnight.
- **Scale @ 1M:** generation runs per-goat (`GenerateForGoat`, single-row event
  path) or chunked per-version (`GenerateForVersion`, cursor-paginated
  `ListEligibleGoatsForGeneration`, bounded page size). No full-herd scan is
  needed to register one birth. Story-n proves the chunked path stays bounded
  at cohort scale.

### Procurement arrival / adult warm-up

- **Class:** predefined (the D0 / D0+21d / D0+28d dates are fixed once farm-entry date is
  known), with a short dynamic-like gate (the warm-up hold) layered on top.
- **Trigger:** goat row has `origin_type IN ('procured','imported')` and an
  entry/warm-up date; `schedulePathForGoat` routes it to `adult_procurement`,
  so only `post_arrival` rule rows fire.
- **Chain reaction:** `post_arrival` rows materialize — D0 (ET+TT + PPR),
  D0+21d ET+TT dose 2, and D0+28d Goat Pox/Sheep Pox (the 4-week pox gap
  honors the live→live spacing rule after PPR). If a `procurement_policy.warmup_no_vaccination_days`
  (7 days) is configured, the D0 row is generated but immediately **deferred**
  (`warmingDeferReason` → `"warming_hold"`) and reopens automatically once the
  7-day cool-off passes and a recovery-repair recheck runs
  (`GenerateRecoveryRepairForGoat`), same reopen path dynamic events use.
- **Idempotency + Asia/Kolkata:** identical key-stability guarantee as birth;
  the anchor is `warmingEntryAt(g)` (farm-entry date), business-day-started.
- **Scale @ 1M:** same chunked/cursor generation path as birth; procurement
  batches are typically small (one truckload), never full-herd.
- Proven by story-h (`story_h_adult_schedule_test.go`, D0/D0+21d/D0+28d course) and
  story-k (`story_k_procurement_warmup_test.go`, warm-up hold + release).

### Adult revaccination / booster cycle (SM-7)

- **Class:** predefined, but the anchor is a *runtime fact* (the previous
  accepted completion date) rather than DOB/entry-date.
- **Trigger:** an adult dose with `trigger_type='after_previous_completion'`
  reaches its next cycle once the prior dose for that rule is `accepted`.
- **Chain reaction:** the next revaccination row is scheduled at
  `prior_completion_date + rule.MinGapDays` (6mo/9mo/1yr/3yr per vaccine,
  `docs/preventive-care-vaccination/vaccination-rules.md`), continuing forever
  as each cycle completes — this is how the source's "adult 6-month, 9-month,
  1-year, 3-year" cadence stays alive without a human re-triggering it.
- **Idempotency + Asia/Kolkata:** the key is derived per-cycle from the
  triggering completion event, so replaying the same completion event cannot
  double-schedule the next cycle.
- **Scale @ 1M:** driven off completion events (one row per accepted dose), not
  a periodic full-herd re-scan.

## DYNAMIC events

### Sick / under-treatment / recovering (health_status)

- **Class:** dynamic.
- **Trigger:** `goats.health_status IN ('sick','under_treatment','recovering')`.
- **Chain reaction:** every rule whose version/rule `eligibility.defer_states`
  includes the goat's current state is deferred
  (`deferredReason` → status `deferred`, a durable `obligation_status_events`
  row with `event_type='deferred'`, `payload={reason, defer_status}`). This
  pauses the goat's **entire remaining open schedule** at once (health status
  is a goat-level fact checked per rule, not a per-dose flag) — a sick goat
  does not selectively skip one dose. On recovery (`health_status` back to
  a non-defer value), the next `GenerateRecoveryRepairForGoat` recheck reopens
  every held row (`ReopenDeferredObligationByIdempotencyKey`) and realigns
  `due_at` onto the recovery-time calendar via `recoveryRescheduleForRule`
  (join a nearby planned drive within `recovery_policy.max_nearby_drive_align_days`,
  default 7, else a same-goat micro-drive).
- **Idempotency + Asia/Kolkata:** defer/reopen both operate against the stable
  `keyDue`-derived idempotency key, so a recheck run twice on the same day is a
  no-op; `recoveryRescheduleForRule` computes align windows in business-day
  terms (Asia/Kolkata).
- **Scale @ 1M:** the recovery-repair job (`ListRecoverableDeferredVaccinationGoatIDs`)
  scans only `status='deferred'` rows whose current health/location state no
  longer requires exclusion, bounded and indexed
  (`obligation_instances_batch_idx`-style filters), never a full herd scan.
- Proven by story-q (`story_q_sick_icu_location_defer_test.go`).

### Quarantine (health_status or location flag)

- **Class:** dynamic.
- **Trigger:** `goats.health_status='quarantine'` **or** the goat's current
  location has `location_operational_attributes.is_quarantine=true` — two
  independent signals into the same `deferredReason` function, so a goat
  physically housed in a quarantine shed defers even if its own
  `health_status` field was never flipped.
- **Chain reaction / idempotency / scale:** identical mechanics to
  Sick/under-treatment above (defer → reopen → realign).
- Proven by story-b (`story_b_quarantine_recovery_test.go`, health-status path)
  and story-q (location-flag path).

### ICU (health_status or location flag)

- **Class:** dynamic.
- **Trigger:** `goats.health_status='icu'` **or** the goat's current shed has
  `location_operational_attributes.is_icu=true` (e.g. moved into an ICU shed
  without a matching health-status update — a real gap the location-flag check
  exists specifically to close).
- **Chain reaction / idempotency / scale:** identical defer/reopen mechanics;
  quarantine and ICU animals must never be vaccinated per
  `vaccination-rules.md`'s "Additional Rules".
- Proven by story-q (`story_q_sick_icu_location_defer_test.go`), which drives
  the location-flag path specifically (a goat healthy by `health_status` but
  housed in an ICU-flagged shed) since no existing story exercised it before.

### Pregnant (reproductive_status + breeding_date)

- **Class:** dynamic.
- **Trigger:** `goats.reproductive_status='pregnant'` with a `breeding_date`;
  `pregnancyMonth(breeding_date, asOf)` computed in 30-day months.
- **Chain reaction:** when the version's `pregnancy_policy` is active
  (`skip_from_pregnancy_month`/`skip_through_pregnancy_month` configured — the
  source rule is months 4–5), a dose whose defer check runs while the goat is
  in that window is deferred with reason `late_pregnancy_hold`
  (`pregnancyDeferReason`). Outside months 4–5 (including once the policy's
  `skip_through` month has passed, i.e. post-delivery), no hold applies and any
  previously deferred row reopens/realigns on the next recheck exactly like a
  health recovery — pregnancy uses the **same** defer/reopen machinery, not a
  separate code path. A missing `breeding_date` on a `pregnant` goat defers
  with `pregnancy_month_review` instead of guessing a month.
- **Idempotency + Asia/Kolkata:** `pregnancyMonth` is a pure function of
  `breeding_date`/`asOf` (business-day terms), so the defer verdict is
  deterministic and replay-safe.
- **Scale @ 1M:** same per-goat recheck / bounded recovery-repair scan as every
  other defer state; no new table, no full-herd scan.
- Proven by story-r (`story_r_pregnancy_lactation_defer_test.go`).

### Lactating / milking (reproductive_status or shed stage)

- **Class:** dynamic.
- **Trigger:** `goats.reproductive_status='milking'` **or** the goat's
  management stage contains `MILKING` (and not `WAITING`/`WARMUP`) —
  `milkingDeferReason`. Note: `reproductive_status='lactating'` alone does
  **not** defer anything today (`pregnancyDeferReason`'s lactating branch
  always falls through to no-hold); only the explicit `milking` state/stage
  triggers `milking_window_hold`. This matches the source rule ("do not
  vaccinate milking-department animals during milking"), not a blanket
  lactation exclusion, since `vaccination-rules.md` is specifically about the
  milking window, not the whole lactation period.
- **Chain reaction / idempotency / scale:** same defer/reopen shape; resumes
  once the animal leaves the milking department/stage.
- Proven by story-r (`story_r_pregnancy_lactation_defer_test.go`).

### Post-breeding hold (bred/breeding status)

- **Class:** dynamic.
- **Trigger:** `goats.reproductive_status IN ('bred','breeding')` with
  `breeding_date` less than 30 days before `asOf` (`postBreedingDeferReason`).
- **Chain reaction:** defers with `post_breeding_hold` for the first 30 days
  after breeding, per `vaccination-rules.md`'s "hold bred animals for at least
  one month from breeding date"; reopens automatically once 30 days pass (or
  immediately with `post_breeding_date_review` if `breeding_date` is missing).
- **Idempotency / scale:** identical to pregnancy above.
- Documented here for completeness (same rule family as pregnant/lactating);
  not required as a separate E2E story by this expansion, but any future story
  covering breeding-hold should reuse story-r's pattern.

### Shed-shift (location change)

- **Class:** dynamic.
- **Trigger:** `goat.location.changed` / `goat.shifted` event (goat's
  `shed_id`/`current_location_id` changes).
- **Chain reaction:** the real `oblapp.GoatShiftedHandler` (SM-2) calls
  `ReScopeOpenForGoat`: every still-open, unbatched obligation is re-scoped
  from the old shed to the new shed (`scope_type='shed'`) so the destination
  shed's drive counts it and the origin shed's drive no longer does. Rows
  already attached to an in-progress/completed batch are left alone (field
  work may already have started) — only unbatched open rows move.
- **Idempotency + Asia/Kolkata:** the handler is **ordered**: it tracks the
  latest accepted shift time per goat, so a stale, out-of-order redelivery
  (an older `occurred_at` than the last accepted shift) is rejected as a
  durable no-op — it can never rewind scope back to a shed the goat already
  left. Proven by story-j (`story_j_shed_shift_rescope_test.go`).
- **Scale @ 1M:** re-scoping touches only that one goat's open rows (indexed
  by `target_id`), never a shed-wide or herd-wide rewrite.

### Death / sale / exit (goat.exited)

- **Class:** dynamic, terminal.
- **Trigger:** `goat.exited` event (death, sale, or permanent transfer out of
  the tenant's herd); `goats.lifecycle_status` moves to a terminal state.
- **Chain reaction:** the real `oblapp.GoatExitedHandler` (SM-3) calls
  `CancelOpenForGoatAt`, which cancels **every** open obligation regardless of
  which open status it is in —
  `status IN ('scheduled','due','in_progress','deferred','missed')` — not just
  plainly-scheduled rows. This matters: a goat that dies while mid-quarantine
  (some rows `deferred`) or mid-drive (some rows `missed`) must still have
  **all** of them canceled forever, not just the ones that happened to still
  read `scheduled`. Completed/accepted history is never touched (death does not
  rewrite the past). Batches that lose obligations to this cancellation have
  their `estimated_targets` reduced and a `cancel_repair` stock-reconcile
  marker recorded so drive-level reservation counts stay honest. A
  `canceled` `obligation_status_events` row and an `outbox_messages` row
  (`goat.obligations_canceled`) are written per canceled obligation in the same
  transaction — the audit + outbox half of the kernel's golden rule.
- **Idempotency + Asia/Kolkata:** the cancel `UPDATE ... WHERE status IN (...)`
  is naturally idempotent — a replay finds zero matching rows (they are all
  already `canceled`) and writes nothing new; `occurred_at` is business-day
  business-clock time, not wall-clock now().
- **Scale @ 1M:** the cancellation is scoped to one `target_id` (the exited
  goat), indexed, and bounded — it never scans other goats' obligations or the
  whole herd.
- Proven by story-i (`story_i_death_autocancel_test.go`, the `scheduled`-status
  case) and story-p (`story_p_death_open_deferred_missed_test.go`, filling the
  gap: cancellation of `deferred`/`missed`/batched rows, batch `estimated_targets`
  repair, and the outbox row).

### Missed dose / window crossed (sweeper)

- **Class:** dynamic (a scheduling *consequence*, not a business event on the
  goat itself).
- **Trigger:** the obligation sweeper's `MarkMissed` pass finds a `scheduled`
  row whose `window_end` (falling back to `due_at`) is before "now".
- **Chain reaction:** the row flips to `missed` (a visible process exception
  for Control Tower / Protocol Adherence — not silently dropped). A missed row
  can later be put back on the calendar via `RescheduleObligationByID` (the
  mobile reschedule write path), which records a `scheduled` status event with
  `payload.reason='mobile_reschedule'`.
- **Idempotency / scale:** the sweeper's missed-pass and the reschedule call
  are both keyed/targeted at a specific obligation id; safe to rerun.
- Proven by story-a (`story_a_missed_buffer_reschedule_test.go`) and story-f
  (`story_f_escalation_alert_test.go`).

## Event taxonomy table

| Event | Class | Trigger | Effect | Scale note |
| --- | --- | --- | --- | --- |
| Birth (kid) | predefined | goat created, DOB + kid schedule path | Full 5-row kid bundle materialized (28/49/84/112/140d); 24h tagging buffer structurally satisfied (not enforced — see Open Questions) | Per-goat or chunked cursor generation, no full-herd scan |
| Procurement arrival | predefined (+ warm-up gate) | `origin_type` procured/imported + entry date | D0 + D0+21d ET+TT dose 2 + D0+28d pox rows; D0 deferred during 7-day warm-up, auto-releases | Chunked generation; small per-truckload batches |
| Adult revaccination cycle | predefined (runtime-anchored) | prior dose accepted | Next cycle row at completion + interval (6mo/9mo/1yr/3yr) | Event-driven per completion, not periodic full scan |
| Sick / under-treatment / recovering | dynamic | `health_status` sick/under_treatment/recovering | Defer ALL open rows; reopen + realign when healthy | Bounded recovery-repair scan, indexed by status/health |
| Quarantine | dynamic | `health_status` or shed `is_quarantine` | Defer ALL open rows; reopen + realign on recovery | Same as above |
| ICU | dynamic | `health_status` or shed `is_icu` | Defer ALL open rows; reopen + realign on recovery | Same as above |
| Pregnant | dynamic | `reproductive_status='pregnant'` + month 4–5 | Defer `late_pregnancy_hold`; resume after window/delivery | Per-goat recheck, no full-herd scan |
| Milking | dynamic | `reproductive_status='milking'` or milking stage | Defer `milking_window_hold`; resume post-milking | Same as above |
| Post-breeding hold | dynamic | `bred`/`breeding` + <30d since breeding_date | Defer `post_breeding_hold`; resume after 30d | Same as above |
| Shed-shift | dynamic | `goat.location.changed` | Re-scope open, unbatched rows to new shed; ordered, rejects stale rewind | Single-goat indexed update |
| Death / sale / exit | dynamic, terminal | `goat.exited` | Cancel ALL open rows (scheduled/due/in_progress/deferred/missed) forever; history retained; batch repair + outbox | Single-goat indexed cancel |
| Missed dose | dynamic (scheduling consequence) | sweeper window-crossed | Row flips to `missed`; visible exception; reschedulable | Sweeper scans by window/status, indexed |

## Cross-cutting rules that hold at 1M-goat scale

- **Tenant/shed/cohort/date/status scoping is first-class** on every query in
  this document's chain reactions: generation pages by goat-id cursor
  (`ListEligibleGoatsForGeneration`), recovery-repair scans by
  `status='deferred'` + indexed health/location join, sweepers/cancel/re-scope
  all key off `target_id` or `scope_id`, never an unscoped table scan.
- **No full-herd scans.** Every dynamic-event handler above is a single-goat
  operation (`CancelOpenForGoatAt`, `ReScopeOpenForGoat`,
  `DeferOpenObligationByIdempotencyKey`) or a bounded/paginated batch pass
  (`GenerateForVersion`, `MarkMissed`, the recovery-repair job).
- **Idempotency is structural, not incidental.** Every obligation's identity
  key is derived from tenant + version + rule + goat + the rule's *base* due
  date (stable across rechecks), so replaying the same event twice is provably
  a no-op rather than "probably fine." Terminal transitions (cancel) are
  idempotent because the `WHERE status IN (...)` predicate naturally excludes
  already-terminal rows on replay.
- **Asia/Kolkata is the only business calendar.** `biztime.BusinessDayStart`
  is used for every due-date, window, and recovery-alignment computation in
  this document; UTC never defines a Goat OS business day.
- **Audit + outbox are part of the transition, not bolted on.** Defer,
  reopen, re-scope, and cancel each write a durable `obligation_status_events`
  row inside the same transaction as the state change; cancel additionally
  writes an `outbox_messages` row so downstream consumers (batch repair,
  future notification adapters) see the same event the audit trail recorded.

## Open questions (flagged, not decided here)

1. **24-hour tagging buffer enforcement.** Today the buffer holds only because
   every kid rule offset is ≥28 days. There is no explicit
   `due_at >= max(computed_due, DOB + 24h)` floor in `dueAt()`/`genOneGoat`. If
   a future predefined event ever needs a same-day or next-day dose, this floor
   would need to be added explicitly. Decide: should this floor be added now as
   defensive hardening, or deferred until a rule actually needs it?
2. **Milking vs. lactating semantics.** The kernel only defers on the explicit
   `milking` reproductive-status/stage value, not a general `lactating` value
   (see "Lactating / milking" above). Confirm this matches current field
   operations (i.e., "lactating" without "milking" is a valid non-deferred
   state) before this becomes load-bearing for a new module.
3. **Post-breeding hold E2E coverage.** Documented in this taxonomy and covered
   incidentally by the same code path pregnancy/milking use, but not yet given
   its own dedicated E2E story. Confirm whether it needs one before the next
   E2E expansion pass.
