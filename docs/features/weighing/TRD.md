# Weighing — Technical Requirements / Design (TRD)

## What weighing IS — read this before proposing anything

Maintainer statement, 2026-08-03. Every session that skipped this has re-derived
rules that do not exist and burned the maintainer's time re-explaining. It is the
whole feature:

```
CEO assigns sheds to an operator or a director (the Growth Director executes too)
individual  → scan RFID, enter weight, record video — per animal
lump-sum    → total weight, animal count, video(s) — per shed
submit
```

**The ONLY business rule: an animal cannot be scanned twice in the same bucket
before submit.**

There is **no** shed↔RFID validation (a scanned tag is stored verbatim and never
checked against a shed), **no** roster or expected count or progress percentage,
**no** herd/goat/clinical lookup, **no** vaccine or protocol or obligation rules,
and **no** "shed is empty" concept — free-flow means the system cannot know what is
in a shed and must not try.

**Do not invent problems that cannot exist in this model.** Two that were raised and
killed: an "empty shed outcome" (impossible — nothing knows the shed is empty), and
the per-animal verifier queue as a grain bug (one video per animal means one review
per animal; the grain follows the evidence — see ban B-5 in
`context/repo-audits/weighing-implementation-do-not-reopen-ledger.md`).

Legitimate weighing work is about **plumbing, never rules**: do writes reach the
server, is evidence reviewable, are failures visible, do screens show honest numbers.


**Status:** Draft v1 · **Date:** 2026-07-27
**Companion:** [PRD.md](./PRD.md)
**Foundation:** shared operational kernel, proof/media engine, Android offline
capture stack, and existing Vaccination execution patterns.
**Source references:** `context/architecture/operational-kernel.md`,
`docs/decisions/vaccination-work-session-bundle.md`,
`docs/decisions/vaccination-shed-ack-not-form.md`,
`docs/decisions/mobile-data-fetch-anti-patterns.md`,
`context/source-findings/goats-and-parks-source-findings.md`,
`context/source-findings/goats-and-parks-source-extract.md`,
`context/source-findings/sheds-db-source-findings.md`.

---

## 1. Scope

This TRD defines the first Weighing implementation slice:

1. Manually authored kids-only weighing campaigns created by CEO/CXO from
   Android, with no inferred recurrence.
2. Explicit shed/partition selection.
3. Bucket grouping guidance that never creates an expected-animal denominator;
   the shared task kernel owns clocks and any roll-forward policy.
4. Shed-level operator execution for v1.
5. Category-aware capture: RFID, weight, and mandatory per-animal video proof
   for `individual_animal`; selected-scope result and mandatory shed/partition
   proof for `per_shed_partition`.
6. Progress/read models for leadership and operator screens.

The design deliberately borrows Vaccination's work-session execution shape, but
not its clinical scheduler. Weighing is not a vaccine obligation. It is a
measurement campaign over selected kid sheds/partitions. Adults are explicitly
out of scope for v1, even though source material mentions monthly adult weighing.
V1 supports two selected-shed categories: individual animal weighing and
per-shed/partition weighing. Individual work captures RFID, weight, and
mandatory per-animal proof video. Per-shed/partition work captures the selected
scope's weighing result and required shed/partition proof video, without
creating individual animal weight observations.

**Weighing is MOBILE ONLY (maintainer decision 2026-08-03).** The admin-web
weighing frontend (`apps/admin-web/app/(admin)/weighing/**`,
`apps/admin-web/features/weighing/**`, and its `lib/api/server.ts` fetchers) was
deleted; it was already unreachable behind a hard redirect. The Android app is
the only weighing client. All backend weighing APIs, read models, and the
`ceo_ai` weighing views stay and are unaffected. Do not rebuild an admin-web
weighing surface without a new maintainer decision.

## 1.0 Authoritative source lifecycle (maintainer decision 2026-07-31; coordination superseded 2026-08-10)

This is the canonical Weighing campaign, bucket, capture, proof, verdict, close,
and reopen lifecycle. It remains authoritative for physical execution. It is no
longer the final app-visible task/owner/clock/contact authority. The shared task
kernel consumes these facts outward-only and owns the cross-module hierarchy,
Today, acknowledgement-gated contact waterfall, separate verifier/sign-off
leaf, and close/reopen rollup. Weighing never reads that generic state and it
never gates free-flow capture. Every section below must be read with that split;
this section governs older conflicting language.

1. CEO/CXO creates a Weighing source campaign by park/date/shed buckets.
2. Each shed bucket (`weighing_campaign_sheds` row) has exactly ONE assigned
   operator (`operator_user_id uuid not null`, migration
   `000056_weighing_shed_operator_assignments.sql`; enforced by
   `tools/agent-hooks/check-weighing-one-operator-per-bucket-guard.mjs`).
3. The operator opens their assigned bucket and scans any number of RFIDs.
4. Per RFID: weight + video (`individual_animal`). Or total weight/count/videos
   (`per_shed_partition`, field-facing "lump-sum").
   **Every weighing video opens on the scale reading 0 kg (maintainer request
   2026-09-04).** Before any weighing clip starts -- each per-animal video and
   each lump-sum group video, replacements and re-uploads included -- the
   in-app recorder holds at the live preview and shows a briefing (title,
   picture of an empty scale reading 0.0 kg, one instruction, in the operator's
   app language: en/hi/kn/te) telling the operator that the FIRST THING IN THE
   VIDEO must be the empty weighing scale reading 0 kg on camera, and only
   then the weighing. Recording starts only when the operator taps OK; cancel
   cancels the capture. This is capture GUIDANCE, not a rule: nothing inspects
   the clip, no scan is gated, and free-flow is untouched. Mechanism:
   `ProofCaptureContext.preRecordBriefing = WEIGHING_SCALE_ZERO`, honoured by
   `InAppVideoRecorderOverlay` (`recorderMayAutoStart`); the two weighing
   `captureVideo` call sites in `WeighingViewModel` are the only ones that set
   it. Pinned by `PreRecordBriefingTest`, the two briefing tests in
   `WeighingViewModelTest`, and the four-locale
   `PreRecordBriefingScreenshotTest` goldens.
5. Operator hits Submit = "I am done for now with this bucket." This flips
   `weighing_campaign_sheds.status` to `completed`, meaning submitted and
   awaiting verification — not that every observation is verified.
6. Submitted observations go to the verifier (generic verification module).
7. Verifier reviews the videos and approves or bounces each observation for
   rework; a bounce puts only that observation back in front of the same
   assigned operator and does not by itself close or reopen the bucket.
8. Only after ALL submitted observations/videos for the bucket are verified can
   CEO/Growth Director close it (`status` → `closed`, with
   `closed_at`/`closed_by`/`close_reason`).
9. CEO/Growth Director can reopen a `closed` or `completed` (submitted,
   pre-close) bucket back to `in_progress`.
10. After reopen, the same assigned operator can add MORE RFIDs and submit
    again.
11. Reopen must NOT allow a duplicate `scanned_identifier` within the same
    `campaign_shed_id`/day (case-insensitive comparison). The same RFID may
    appear in a DIFFERENT bucket if business allows, but never duplicated in
    the same bucket/day.
12. There is NO expected-animal denominator and NO `"N/N"` or `"/100"`-style
    progress. Expected animals are unknown for weighing; progress is reported
    only as counts of scanned/accepted/pending/verified observations, never as
    a fraction of an expected roster.

Backend mapping (already true in code, migration
`000058_weighing_close_and_verification_state.sql`):
`weighing_campaign_sheds.status` is `pending` / `in_progress` / `completed` /
`closed` / `canceled`, where `completed` means "operator submitted, awaiting
verification"; close moves it to `closed`; reopen moves it back to
`in_progress`.

## 1.1 Last-30-commit Vaccination hardening lens

The Weighing implementation must be reviewed against the recent Vaccination
failure patterns before code starts:

| Failure pattern fixed in Vaccination | Weighing design guard |
|---|---|
| Shared parent task hid the real shed submit grain. | No aggregate campaign/task state may be used as per-shed/per-animal completion truth. |
| Over-broad scan items wrote sibling shed completions. | Observation inserts must bind the exact campaign bucket, scan identity, proof subject, and observation grain before counting captured or accepted work. |
| Proof refs disappeared across submit retry/recovery. | Observation, proof artifact, upload state, and idempotency replay must be recoverable from durable rows. |
| Terminal submit state accepted new side effects. | Completed/canceled work groups are immutable except explicit correction/reopen flows. |
| Android routes chose the wrong task/shed after scan. | Route identity must carry campaign, work group, expected shed/partition, and animal scan context end to end. |
| Permission/proof gates ran too late. | Backend must reject observation completion without `weighing.execute` and required proof. |
| Leadership/admin cards used fallback copy/shape. | Backend contracts own progress buckets, disabled reasons, labels, and media URLs. |
| Notification and projection fanout retries were needed after partial failure. | Observation acceptance, progress projection, media review/recovery, notification, and leadership summaries must be durable retryable consumers with visible degraded states. |
| Mobile scan/RFID fixes exposed O(n) lookup, lost identity, and permission timing hazards. | Android must use indexed Room/cache lookup, exact route/outbox identity, and backend-first capability/proof gates. |

## 2. Architecture invariants

Go backend remains a modular monolith with strict module boundaries. Domain
logic must depend on ports/interfaces, not vendor SDKs directly. Adapters wrap
auth, storage, media, devices, notifications, analytics, and external APIs.
Adapter selection belongs in bootstrap/factory code.

Web/mobile clients use REST/JSON APIs described by OpenAPI and generated
clients. Forms, events, decisions, DLQ repair payloads, and imports use JSON
Schema where payload compatibility matters. gRPC/protobuf is allowed only behind
the app API boundary for internal high-volume workloads or a future split
service.

Every mutating path must define idempotency, audit, outbox publication, RBAC
scope, observability, replay behavior, and tests. Weighing must plug into the
governed shared operational-kernel chain through an outward-only materializer:

```text
business event
-> Weighing source transaction + idempotency + audit/outbox
-> outward shared materialization receipt + source-version fence
-> real owner + pinned clock
-> bounded task hierarchy
-> acknowledgement-gated contact waterfall
-> proof
-> separately owned verifier/sign-off leaf
-> close/reopen rollup
-> shared Today/My Tasks/leadership reads
```

The kernel chain is not optional because Weighing looks simpler than
Vaccination. Weighing still creates operational work, captures proof, records a
trusted measurement fact, affects leadership answers, and can cross an authored
task clock. The adapter remains strictly outward-only: Weighing execution never
reads shared task state or waits on the shared kernel, while no private
Weighing task list, owner fallback, clock, contact ladder, sign-off queue, or
coordination read may remain authoritative after cutover. Missing/expected
animals are not Weighing concepts.

## 3. Domain grain

Canonical grains:

| Grain | Purpose |
|---|---|
| Weighing campaign | One manually authored instance created by CEO/CXO for a park, date/window, and selected physical buckets. It does not imply recurrence. |
| Selected shed/partition | Atomic source assignment/grouping unit carrying physical scope, category, authored operator, and source lifecycle. It carries no expected membership. |
| Shared task work unit | Outward-materialized campaign/bucket hierarchy with real owner, pinned clock, contacts, and sibling verifier/sign-off work. It never gates Weighing execution. |
| Animal weighing observation | One scanned identifier, measured weight, and proof video; no expected-roster or resident-animal lookup. |
| Per-shed/partition weighing observation | One selected shed/partition's weighing result and required shed/partition proof. It is not animal latest-weight truth. |
| Proof artifact | Mandatory per-animal video linked to an animal observation, or shed/partition proof linked to a per-shed/partition observation. |
| Correction | Audited replacement/voiding of an incorrect weight or proof after sync. |

Shed/partition is atomic for assignment planning. A work group may contain
multiple sheds/partitions when those buckets have the same operator. A
shed/partition must not be split only to satisfy the daily cap. If one
shed/partition exceeds the daily cap, the group remains the whole
shed/partition and may span multiple business dates through execution progress.
For individually marked sheds/partitions, source progress counts accepted
observations and proof/verdict state without a denominator. For
per-shed/partition marked scopes, completion is at the selected-scope grain and
must not invent individual observations.

Campaign-level `operator_user_id` is the backwards-compatible default operator.
`weighing_campaign_sheds.operator_user_id` is the execution owner used for
operator worklist visibility, animal/free-flow scan authorization, lump-sum
authorization, and individual shed submit authorization. CEO/director monitor
reads can see all shed buckets in the campaign; execute-only operators see only
the buckets where `weighing_campaign_sheds.operator_user_id` equals their user
id.

Every query, projection, outbox event, notification, Android route, and proof
lookup must preserve this key set:

```text
tenant_id
campaign_id
campaign_shed_id / shed_location_id / partition key
task source identity where coordination context exists
scanned_identifier where the fact is individual capture grain
proof_artifact_id where the fact is proof/media-grain
```

`weighing_campaigns.status` is aggregate source bookkeeping. It cannot answer
whether a shared task, contact, proof, or sign-off is complete. Source progress
is category-aware: individual capture counts submitted and verified
observations/proofs without a denominator; per-shed/partition capture counts
accepted selected-scope observations/proofs. There is no expected row, extra-
scan classification, missing-animal state, or individual expected numerator.

## 4. Proposed backend module

Add a dedicated `weighing` module rather than encoding weighing as a
vaccination subtype.

Suggested package shape:

```text
backend/internal/weighing/
  domain/
  ports/
  app/
  adapters/postgres/
  adapters/http/
```

The module may reuse shared SOP/proof/media infrastructure through ports, but
it owns weighing-specific tables, planner policy, observations, and read models.
It must not write Vaccination tables or depend on Vaccination-specific protocol
rules. If generic SOP tasks are used for assignment/routing, their state is
aggregate bookkeeping only; Weighing's per-animal observation/proof tables remain
the source of completion truth.

## 5. Data model draft

Names are draft and should be reconciled against current migration conventions
before implementation.

### `weighing_campaigns`

| Column | Notes |
|---|---|
| `campaign_id uuid pk` | Idempotent campaign identity. |
| `tenant_id uuid not null` | Tenant boundary. |
| `farm_id` / `park_id` | Execution scope. Use current location model terminology at implementation time. |
| `period_type text not null` | `week` for v1. Keep the column generic only to avoid later migration churn; month/manual values are not accepted in v1 commands. |
| `period_start_date date not null` | Authored campaign display-window start. |
| `period_end_date date not null` | Authored campaign display-window end. |
| `cadence_type` / `cadence_due_date` | Legacy compatibility fields only. No recurrence is inferred; the shared task clock maps the explicitly authored campaign date/window. |
| `display_week_start_date date not null` | Week-tab anchor. |
| `display_month date` | Reserved for future adult/monthly scope; null in v1. |
| `animal_group_filter text not null` | `kids_k_f` only in v1. |
| `start_business_date date not null` | Day leadership created/scheduled work, e.g. 2026-07-29. |
| `status text not null` | `draft`, `published`, `in_progress`, `delayed`, `completed`, `closed`, `canceled` (per migration `000058_weighing_close_and_verification_state.sql`; `planned` was never a persisted value). |
| `planned_cap_per_day int not null` | Default 100 for v1; authored/configured later. |
| `operator_user_id uuid` | Backwards-compatible default operator for old clients and campaign-level display. Shed rows carry the execution owner. |
| `published_at timestamptz` | Set when operator-visible work is created. |
| `completed_at timestamptz` | Source campaign closure evidence derived from selected bucket/proof/verdict state, never expected animals. |
| `created_by`, `created_at`, `updated_at`, `row_version` | Audit/optimistic lock. |

### `weighing_campaign_sheds`

| Column | Notes |
|---|---|
| `campaign_shed_id uuid pk` | Row identity. |
| `operator_user_id uuid not null` | Field operator who owns this selected shed/partition bucket. |
| `campaign_id uuid not null` | Parent campaign. |
| `location_id uuid not null` | Physical shed or partition/cohort scope. |
| `location_type text not null` | `shed`, `cohort`, or implementation-supported partition grain. |
| `display_name text not null` | Snapshot label for audit/display. |
| `expected_animal_count int not null` | **Always 0 — no expectation.** Weighing is free-flow: there is no expected roster and therefore no denominator. The write path used to store a literal `1`, which every progress figure then read as "this shed expects one animal". Nothing derives business truth from this column; do not start. |
| `weighing_category text not null` | `individual_animal` or `per_shed_partition`. Selected by leadership per shed/partition; `per_shed_partition` may display as "lumpsum" in field-facing copy. |
| `status text not null` | `pending`, `in_progress`, `completed`, `closed`, `canceled` (per migration `000058_weighing_close_and_verification_state.sql`). `completed` means the assigned operator submitted and the bucket is awaiting verification, NOT that every submitted observation is verified. `closed` is a distinct terminal state set only by CEO/Growth Director once every submitted observation in the bucket is verified; a bucket may be reopened from `completed` or `closed` back to `in_progress` by CEO/Growth Director, after which the same assigned operator may add more scanned RFIDs and submit again. |
| `completed_at timestamptz` | Set when the assigned operator submits (awaiting verification), independent of `closed_at`. |
| `closed_at`, `closed_by`, `close_reason`, `closed_not_accepted_count` | Set only on explicit CEO/Growth Director close, after all submitted observations in the bucket are verified. |

### Legacy `weighing_work_groups` design (do not implement as task authority)

| Column | Notes |
|---|---|
| `work_group_id uuid pk` | Suggested execution group. |
| `campaign_id uuid not null` | Parent campaign. |
| `planned_business_date date not null` | Authored/source planning date; shared task policy owns active clock behavior. |
| `sequence_no int not null` | Stable order. |
| `status`, `effective_business_date`, `rolled_from_date` | Legacy coordination fields to shadow/reconcile and retire or demote; not shared task truth. |

Group membership should be a separate join table:

```text
weighing_work_group_sheds(work_group_id, campaign_shed_id)
```

This historical join design is not current task authority. The shared task
adapter maps stable campaign/bucket source identity directly and must not infer
hierarchy from dates, labels, or route parameters.

### Removed: `weighing_expected_animals`

Migration `000079` dropped this table. It is not a compatibility source,
planner hint, denominator, membership snapshot, or future task input. Do not
reintroduce expected, missing, moved, unavailable, ICU/quarantine, lifecycle, or
resident-animal concepts into Weighing. Planning selects physical buckets;
capture records what the scanner/scale/proof observed.

### `weighing_observations`

> **SUPERSEDED (maintainer decision 2026-07-31, migration
> `000007_weighing_free_flow_scanned_identifier.sql`):** `animal_id` is
> **nullable**, not `not null`. Weighing is free-flow: an operator scans a
> real ear-tag RFID and the backend accepts it as `scanned_identifier`
> whether or not it resolves to a known `goats.goat_id`. The
> `weighing_observations_animal_or_identifier_check` CHECK constraint
> requires `animal_id IS NOT NULL OR btrim(scanned_identifier) <> ''` —
> i.e. at least one of the two must be present, never both mandatory. The
> same `scanned_identifier` may legitimately appear in more than one
> `campaign_shed_id` bucket (a goat can be weighed once per bucket); no
> uniqueness constraint may collapse it across buckets. See
> `tools/agent-hooks/check-weighing-free-flow-guard.mjs` for the machine
> guard that enforces this.

> **NO HERD CROSS-CHECK ON THE WRITE PATH (maintainer decision 2026-07-31).**
> This decision SUPERSEDES the "critical-animal-action gate on weighing
> submit/observation" item recorded as blocker 2/9 in
> `context/repo-audits/weighing-phase1-2-do-not-merge-blockers.md`. A clinical
> gate briefly shipped that joined `goats` and refused the write when
> `health_status` was `sick`/`under_treatment`/`recovering`/`quarantine`/`icu`
> or `lifecycle_status` was an exit state. It has been removed.
>
> The weighing observation/submit path:
> - does **not** resolve a scanned RFID to goat identity in order to decide
>   whether the write is allowed;
> - does **not** read `goats.health_status` or `goats.lifecycle_status`;
> - does **not** check sick / ICU / quarantine / recovering / under-treatment;
> - stores the raw `scanned_identifier` in the weighing tables;
> - may carry an `animal_id` when one is already present from non-blocking
>   enrichment, but the write and submit must never *depend* on it.
>
> Why: weighing records what the scale and the scanner saw. Putting an animal on
> a scale administers nothing, so a clinical state is not a safety reason to
> refuse the measurement — and refusing it destroys exactly the weight trend a
> vet needs for an animal under treatment. Health state belongs to the clinical
> workflows that own it.
>
> **Vaccination remains strict and must not be loosened by anything here.**
>
> **SUPERSEDED — there is no roster at all.** `weighing_expected_animals` was
> DROPPED (migration `000079`) and `weighing_observations.mismatch_status` with
> it (migration `000081`). Nothing is classified as `expected_shed`,
> `wrong_shed` or `extra_scan`, because those verdicts need an expected set that
> no longer exists; the write path stores no verdict. The paragraph below is
> kept only to record why the roster could never have been a gate. The one
> surviving precondition is
> bucket category (`weighing_category='individual_animal'`), which is a
> weighing-owned check about the bucket, not about the animal.
>
> **STRICT FORM (maintainer decision 2026-07-31, later the same day).** There is no
> longer a "known animal" write path at all. `RecordAnimalObservation` always takes
> the free-flow route:
> - the request's `animal_id` is IGNORED for the write decision and is cleared by the
>   service layer; stored `weighing_observations.animal_id` is always `NULL`;
> - `scanned_identifier` is REQUIRED — a capture with no scanned tag is rejected;
> - the write never resolves an RFID to a goat, never joins `goats`, and is never
>   refused because a `goat_id` does not exist;
> - **proof is BUCKET-scoped, not goat-scoped**: a completed video whose
>   `scope_type='shed'` matches the campaign shed's `location_id`. Goat-scoped proof
>   is no longer accepted, because requiring it was itself herd coupling;
> - the remaining gates are all weighing-owned: live campaign, the bucket belongs to
>   this campaign and this operator and is not canceled, the bucket is
>   `weighing_category='individual_animal'`, positive weight, idempotency key.
>
> The `animal_id` column and its FK to `goats` REMAIN in the schema for future
> enrichment/backfill — they are simply never written by the operator path. Nothing
> in the weighing write may depend on them.
>
> Machine enforcement: `make weighing-free-flow-guard`. Seven failure modes, the
> decisive one being mode 7 (`write-path-table-not-allowlisted`): the write path may
> touch ONLY weighing-owned tables plus proof/idempotency/audit/outbox, so `goats`,
> `weighing_expected_animals` and vaccination tables are banned by default rather
> than one incident at a time. The guard follows same-file helper calls, so
> extracting a gate into a private helper does not hide it.
> Behavioural proof:
> `backend/internal/weighing/adapters/postgres/repository_free_flow_no_herd_crosscheck_integration_test.go`.

| Column | Notes |
|---|---|
| `observation_id uuid pk` | Idempotent observation identity. |
| `campaign_id uuid not null` | Parent campaign. |
| `work_group_id uuid` | Work group being executed. |
| `animal_id uuid` | Legacy nullable enrichment only; the operator path stores NULL and never resolves it to authorize capture. |
| `scanned_identifier text not null` | Raw scanned value for audit; the free-flow contract's primary identity when `animal_id` cannot be resolved. |
| `weight_kg numeric not null` | Positive measured weight. |
| `observed_at timestamptz not null` | Device/business timestamp. |
| `operator_user_id uuid not null` | Field operator. |
| `campaign_shed_id uuid not null` | Weighing-owned selected physical bucket. |
| `proof_artifact_id uuid not null` | Mandatory bucket-scoped video proof for the captured row. |
| `status text not null` | `local_pending`, `submitted`, `accepted`, `rejected`, `voided`, `replaced`. |
| `correction_of_observation_id uuid` | Nullable pointer for audited replacement. |
| `sync_state/audit columns` | Follow existing mobile capture conventions. |

Uniqueness should prevent duplicate same-campaign animal observations unless an
explicit correction/replacement flow is added. V1 can use one accepted
observation per `(campaign_id, animal_id)`.

### `weighing_shed_observations`

Per-shed/partition observations are separate from individual animal observations
so they cannot accidentally update animal latest-weight truth.

| Column | Notes |
|---|---|
| `shed_observation_id uuid pk` | Idempotent row identity. |
| `campaign_id uuid not null` | Parent campaign. |
| `campaign_shed_id uuid not null` | Selected shed/partition row with `weighing_category=per_shed_partition`. |
| `work_group_id uuid` | Work group being executed. |
| `weighing_result text/jsonb not null` | Implementation-defined result payload for the selected scope. Must be validated by backend policy. |
| `observed_at timestamptz not null` | Device/business timestamp. |
| `operator_user_id uuid not null` | Field operator. |
| `proof_artifact_id uuid not null` | Required shed/partition proof video. |
| `status text not null` | `local_pending`, `submitted`, `accepted`, `rejected`, `voided`, `replaced`. |

Accepted `weighing_shed_observations` complete only the selected
shed/partition category row. They do not create individual observations or
animal latest-weight truth. Individual observations have no expected-versus-
extra classification.

### `weighing_observation_corrections`

Corrections are explicit audit records, not destructive updates.

| Column | Notes |
|---|---|
| `correction_id uuid pk` | Row identity. |
| `observation_id uuid not null` | Observation being voided/replaced. |
| `replacement_observation_id uuid` | Nullable until replacement is accepted. |
| `reason text not null` | Stable reason code plus optional note. |
| `requested_by uuid not null` | Actor. |
| `created_at timestamptz not null` | Audit instant. |

Shed/partition observation corrections follow the same audit pattern with a
separate `weighing_shed_observation_corrections` table or a shared correction
table that carries `correction_subject_type`. They must preserve old proof/result
lineage and must not route through animal observation correction code paths.

### Durable media linkage

The proof/media link must be queryable in both directions:

- from observation to proof artifact for leadership/review display;
- from proof artifact to observation for upload retry/recovery;
- from idempotency key to existing accepted observation for replay.

Do not rely on transient Android form state or a generic SOP submission blob as
the only proof reference.

### Status and constraint requirements

Implementation must define persisted status values in migrations with `CHECK`
constraints and mirror them in domain value objects. At minimum, the design must
separate:

- Weighing source campaign/bucket lifecycle from current migrations;
- observation status: pending_upload/submitted/accepted/rejected/voided/replaced.

Exact names follow current migrations, but terminal source states change only
through explicit correction/reopen commands. Today, delayed, contact pending,
and sign-off pending belong to the shared task kernel.

## 5.1 Count and projection grain

The first implementation must write the projection proof into the SQL/repository
next to the query marker `projection-review: weighing-progress`.

Required written proof:

```text
Producer unique key:
  individual observation = tenant_id + campaign_shed_id + normalized scanned_identifier + accepted generation
  shed observation = tenant_id + campaign_id + campaign_shed_id + accepted generation
Consumer group key:
  overview = tenant_id + campaign_id
  shed progress = tenant_id + campaign_id + campaign_shed_id
  captured row = tenant_id + campaign_shed_id + observation_id
  per-shed/partition row = tenant_id + campaign_id + campaign_shed_id
Joined side multiplicity:
  observation -> proof artifact is 1:1 for accepted rows
  selected per-shed/partition scope -> active accepted shed observation is 0:1
  shed observation -> proof artifact is 1:1 for accepted rows
No expected denominator or resident-animal join exists.
```

Projection buckets are disjoint:

```text
individual_captured_total
= individual_proof_verified
+ individual_proof_pending
+ individual_rework
+ individual_voided_or_replaced

per_shed_partition_selected_total
= per_shed_partition_completed
+ per_shed_partition_pending
+ per_shed_partition_proof_blocked
+ per_shed_partition_closed_by_leadership
```

No counter may add Vaccination-style wrong-shed, not-in-campaign, missing,
unavailable, or roster concepts; the selected shed/partition is a Weighing
evidence bucket, not a Herd Register membership assertion.

Never use `ORDER BY ... LIMIT 1` to bind an observation to a work group, shed, or
proof when multiple rows can legitimately exist for the same campaign. Use the
exact membership/proof key or reject the ambiguity.

## 6. Planning algorithm

Inputs are explicitly selected shed/partition rows, authored campaign date/
window, optional capacity guidance, and authored bucket operator assignment.
No expected roster, resident-animal filter, recurring cadence, or generic duty
lookup runs inside Weighing.

Algorithm:

1. Sort selected sheds/partitions by stable operational order.
2. Treat each selected physical shed/partition bucket as an atomic item without
   inspecting resident animals.
3. Build source bucket groups greedily:
   - add the next item if it does not exceed cap;
   - if the current group is empty, add the item even when it exceeds cap;
   - otherwise close the current group and start the next.
4. Optionally fit a small later item into remaining capacity if it avoids a
   tiny group and does not reorder across parks/farms.
5. Assign suggested source planning dates starting at `start_business_date`;
   shared task policy owns active clock behavior.
6. Do not create hard errors for under-cap or over-cap groups when caused by
   atomic shed/partition rules.

The planner must be deterministic and idempotent for the same campaign snapshot.
Planner output is advisory until published. Once published, re-running the
planner must preserve completed observations, accepted proof, explicit
corrections, and in-progress operator work. A replan can only move pending
work-group dates or create new groups for newly added sheds.

Planner stability requirements:

- Sort by canonical farm/park order, then shed/partition operational order, then
  stable id. Do not rely on display labels alone.
- Store `planner_policy_version`, `input_snapshot_hash`, and `output_hash` on the
  plan/publish response.
- Same snapshot + same policy version must produce byte-identical work-group
  membership and sequence numbers.
- Capacity math uses operator-business-date grain. Multiple operators can work
  the same campaign only by owning different selected shed/partition buckets;
  the query/model must not collapse multi-operator or multi-date capacity into a
  single campaign-level number.
- Replan only pending groups. Accepted observations, proof, corrections, and
  terminal groups are immutable unless an explicit correction/reopen command
  creates auditable new facts.

Vaccination comparison:

- Do not evaluate vaccine due windows, medical safe windows, compatibility,
  protocol rules, vial lots, or dose cells.
- Do reuse the concept that published/in-flight work is not silently destroyed.
  Replanning should preserve completed observations and explicit operator
  progress.

## 6.1 IMPLEMENTED COMPATIBILITY SOURCE (Phase 2): local time-driven lane

Phase 1 shipped planning, execution, proof, verification, and explicit close, but
weighing had **no time-driven kernel at all**: publishing a campaign produced a
plan nothing swept, `kernel-worker` had zero weighing awareness, and Calendar /
Control Tower had no weighing process state. Phase 2 closes that. This section
describes what is currently built, not the final coordination authority.

Under the 2026-08-10 non-deviation decision, `weighing_work_items`,
`WeighingKernelStage`, direct cadence notifications, `/app/weighing/alerts`, and
`GET /weighing/process-state` are legacy compatibility/source lanes to
shadow-compare and suppress or demote at task-kernel cutover. Weighing-owned
campaign/bucket identity, assigned operator, authored planned date, capture,
proof, verdict, close, and reopen remain source facts. Generic owner/clock,
Today, delay/contact policy, verifier/sign-off task, hierarchy, and rollup move
to shared task truth. The cutover must prove parity, event receipt/version
fencing, replay, reconciliation, and zero duplicate tasks or contacts, and must
not add an inbound Weighing dependency.

### Work items on publish

`weighing_work_items` (migration `000059_weighing_kernel_work_items.sql`) is the
currently deployed Weighing source/compatibility work ledger. It is not the
post-cutover generic task authority.

- **Grain: ONE ROW PER `weighing_campaign_sheds` BUCKET.** Never per animal, never
  per campaign. One bucket has exactly one operator, so a work item has exactly one
  owner. Free-flow is untouched: the table references no goat, no herd roster, and
  no vaccination row, and `weighing_observations.animal_id` stays nullable.
- Columns carry tenant, campaign, `campaign_shed_id`, park, `shed_location_id`,
  assigned `operator_user_id`, weighing category, shed label, and:
  - `planned_business_date` — the ORIGINAL planned date. **Immutable.**
  - `due_business_date` — the CURRENT executable date. Rolls forward only
    (`CHECK (due_business_date >= planned_business_date)`).
  - `work_state` — the single **disjoint** read bucket dimension:
    `scheduled | delayed | completed | closed | canceled`. Open/executable =
    `scheduled | delayed`.
  - `day_start_surfaced_on`, `rolled_forward_count`, `last_rolled_forward_on`,
    `delayed_since_business_date`, `escalated_on`, `terminal_at`.
- `Repository.createWorkItemsForPublishTx` runs **inside the publish
  transaction** (`PublishCampaign`). It is a required recorder, not a side effect:
  if work items cannot be written the publish fails and the campaign stays draft,
  so a published campaign can never exist without the work the kernel sweeps.
- It is **one set-based `INSERT ... SELECT`**, never a per-shed loop of queries.
  Suggested business dates come from the greedy planner expressed as a window
  function: buckets are ordered per OPERATOR (capacity is operator-business-date
  grain, so two operators do not consume each other's cap) and the day offset is
  the running EXCLUSIVE bucket size divided by `planned_cap_per_day`.
- Idempotency is `UNIQUE (tenant_id, campaign_shed_id)` +
  `ON CONFLICT DO NOTHING`: republish, an exact idempotency-key replay, and a
  retried transaction all converge on exactly one work item per bucket.

### Cadence: registered in the existing kernel worker

`kernelstages.WeighingKernelStage` (`Name() == "weighing-kernel"`) is registered
in `backend/cmd/kernel-worker/main.go` on the existing **operational** cadence
class (5-minute lane), next to the feed-transport day-task cadence. There is **no
new worker binary, no Cloud Scheduler cron, and no scheduled Cloud Run Job** — the
5k-50k envelope topology is unchanged, so `deploy/` needs no job or scheduler
entry. Tunables: `GOATOS_WEIGHING_KERNEL_CHUNK_SIZE` (default 200),
`GOATOS_WEIGHING_KERNEL_MAX_CHUNKS` (default 50).

`Repository.SweepWorkItems` is one bounded, resumable, forward-progressing tick
with four passes, in this order:

1. **Terminal reconciliation** — a bucket that reached `completed` / `closed` /
   `canceled` stops being open work, so it can never roll forward or escalate
   forever. The terminal status is carried straight across; no state is invented.
2. **Roll-forward** — open work whose `due_business_date` has passed stays
   EXECUTABLE and moves to today. `planned_business_date` is never touched, so the
   original plan survives for audit, and `rolled_forward_count` increments. Work is
   **never auto-canceled because a date passed.**
3. **Delayed / escalation** — open work past its ORIGINAL planned business date
   becomes `work_state='delayed'` with `delayed_since_business_date` and
   `escalated_on` set, and escalates UPWARD. Escalation fires once per transition,
   not once per tick.
4. **Day-start** — today's open work is surfaced per assigned operator, once per
   business date (`day_start_surfaced_on`).

Pass 2 runs before pass 4 deliberately: work that rolled forward becomes due today
and is included in today's day-start surface in the same tick.

Every pass is **keyset-chunked with `FOR UPDATE SKIP LOCKED` and a `LIMIT`**, over
the partial index `weighing_work_items_open_keyset_idx (tenant_id, work_item_id)
WHERE work_state IN ('scheduled','delayed')`. Forward progress is doubly
guaranteed: a monotonically increasing `work_item_id` cursor per pass, AND every
pass's `UPDATE` makes the claimed row stop matching its own predicate. A tick that
spends its chunk budget stops and reports `Truncated`, resuming on the next tick.
No full scan, no `OFFSET`, no unbounded tick.

### Time grain

Every comparison is the **Asia/Kolkata business DAY**. The stage hands
`SweepWorkItems` an instant; `biztime.BusinessDate(asOf)` resolves it once and
every SQL predicate is `::date` against that value. There is no `now()` business
comparison, no `now ± N hours`, and no hour/minute arithmetic anywhere in the
kernel path — the read model rejects anything finer than a `YYYY-MM-DD` business
date outright.

### Events (registered both ends)

| Event | Direction | Recipients |
|---|---|---|
| `weighing.work_item.day_start` | DOWNWARD only | the assigned bucket operator, their own buckets only |
| `weighing.work_item.rolled_forward` | DOWNWARD + UPWARD | assigned operator + `growth_director` + `ceo_internal` |
| `weighing.work_item.delayed` | UPWARD only (escalation) | `growth_director` + `ceo_internal` |

These rows describe the legacy pre-cutover cadence, not disciplinary policy.
Under the accepted 2026-08-10 timing decision, D+1 and D+2 are normal
carry-forward. The shared-kernel adapter must suppress incident/page/voice and
must not create a breach or employee violation for ordinary Weighing aging.
After D+2 it may create a Director-owned capacity/unblock follow-up while
keeping execution open. See
`docs/decisions/task-timing-alerting-violations-and-appeals.md`.

One event per `(campaign, operator)` group per pass — never one per work item.
Each is enqueued in the SAME transaction as the state change it describes, with a
deterministic idempotency key of `(event type, tenant, campaign, operator,
business date)`, so an at-least-once redelivery collapses instead of pushing an
operator twice for the same business day.

All three are consumed by the **existing** Phase 1 consumer
`notificationbridge.WeighingLifecycleEventConsumer.handleWorkItemCadence` — not a
parallel consumer — and all three producer/consumer pairs are registered in
`context/architecture/domain-event-registry.json`. Direction is fixed by the event
TYPE, not by a payload flag. Recipients resolve only from
`ResolveMemberRecipients` (the assignment-row `operator_user_id`) and
`ResolvePositionRecipients` (active leadership role grants); no name, phone, token,
or seed-time route exists in any payload or handler.

### Calendar + Control Tower binding

This subsection describes current compatibility behavior. After shared-kernel
cutover, Calendar, Today, Control Tower, Action Center, and alerts read shared
task truth. `GET /weighing/process-state` remains only for source reconciliation
or is retired after zero-use proof; it cannot remain a competing coordination
authority.

`Repository.WeighingProcessState` / `GET /weighing/process-state`
(`getWeighingProcessState`, permission `weighing.monitor`) is the shared-surface
read, per `docs/architecture/operational-read-model-contract.md`:

- **Declared grain** on the wire: `grain: "weighing_work_item"` (not animal, not
  campaign).
- **Disjoint buckets**: the five `work_state` counts sum exactly to `total`, so a
  UI may add them without double counting. The union is explicitly named
  `open_total` (= `scheduled + delayed`).
- **Whole-filter summary**: computed by the database over every matching work item.
  The endpoint has no page or cursor parameter at all, so the summary is page-size
  independent by construction; narrowing the date window changes day-marker ROWS
  only and leaves the summary identical.
- **Day markers are dot-grain**: one row per business day carrying open/delayed
  counts, never the day's work items, so a Calendar grid never fetches a day's rows
  to draw itself.
- Served from canonical indexed SQL (`weighing_work_items_campaign_state_idx`)
  with **zero projection tables**, per the 5k-50k envelope.

Not built in this slice, and honestly out of scope here: the weighing rows are
**not** merged into `backend/internal/calendar` or
`backend/internal/processintegrity` SQL. Those modules remain
vaccination-shaped; weighing exposes its own backend-owned process-state contract
for those surfaces to render, and folding it into the shared calendar query is
follow-up work.

### Proof

| Invariant | Test |
|---|---|
| publish creates work items exactly once, replay-safe | `TestWeighingPublishCreatesWorkItemsExactlyOnceOnReplay` |
| day-start selects only today's open work, per operator, no cross-operator leak | `TestWeighingKernelDayStartSelectsOnlyTodaysOpenWorkForTheAssignedOperator` |
| roll-forward preserves the original planned date, never cancels | `TestWeighingKernelRollForwardPreservesOriginalPlannedDateAndNeverCancels` |
| delayed detection + one-shot escalation past the planned business date | `TestWeighingKernelDelayedDetectionEscalatesPastPlannedBusinessDate` |
| terminal buckets stop being swept | `TestWeighingKernelStopsSweepingTerminalBuckets` |
| the claim is chunked, bounded, and terminates | `TestWeighingKernelSweepClaimIsChunkedAndTerminates` |
| summary is disjoint, whole-filter, page-size independent | `TestWeighingProcessStateSummaryIsDisjointWholeFilterAndPageSizeIndependent` |
| FCM direction/recipients per cadence | `backend/internal/notificationbridge/weighing_work_item_cadence_notify_test.go` |
| cadence registered on the existing worker's operational lane | `TestKernelWorkerSchedulesWeighingKernelOnOperationalCadence` |

All dates in these tests are FIXED Asia/Kolkata business dates; there is no
wall-clock offset and no hour arithmetic.

Compatibility machine gate: `make weighing-kernel-phase2-guard`
(`tools/agent-hooks/check-weighing-kernel-phase2-guard.mjs`, registered in
`tools/ci/guardrail-manifest.json`, `Makefile:guardrails`, and
`tools/ci/run-local-ci.sh`) enforces five failure modes: publish without work
items, an unbounded/non-keyset sweeper claim, hour arithmetic in the kernel path, a
cadence not registered inside the existing kernel worker, and hardcoded cadence
recipients. F0 must reclassify this guard as pre-cutover continuity protection;
the task-kernel cutover change replaces its private-coordination requirements
with materialization parity, receipt/version fencing, no duplicate task/contact,
source reconciliation, and no inbound Weighing dependency.

## 7. Open execution across business days

Weighing source buckets remain executable across day boundaries until their
category policy completes or leadership cancels/closes the campaign. Before
cutover the legacy sweeper records local delay/roll-forward compatibility facts.
After cutover the shared task clock is the only app-visible day/delay authority;
the source bucket never reads that state or blocks capture.

Allowed execution cases:

- Operator completes only part of a suggested group.
- Operator completes one shed but not another shed in the same group.
- Operator weighs an animal from another shed during the same session.
- Task extends beyond the week end.
- Operator removes/replaces an unsynced proof locally before submission.
- Synced weight/proof needs correction through an audited replacement flow.

Forbidden behavior:

- Auto-cancel because week end passed.
- Auto-split shed/partition rows to force daily cap.
- Auto-move animal location because the animal was scanned in another shed.
- Accept a completed observation without the mandatory proof for its selected
  category.
- Change completion counts by reading only the visible/paginated rows.

## 8. No animal availability reconciliation

Migration `000079` removed the expected roster. Weighing does not consume
movement, ICU/quarantine, death/culling, sale/transfer, identity, resident-
animal, or lifecycle events to classify pending work. It records the selected
physical bucket and what the scanner/scale/proof observed. The shared task
materializer reconciles only Weighing campaign/bucket/event facts; it never
introduces animal availability into capture or completion.

## 9. API draft

Leadership:

```text
GET  /api/v1/weighing/weeks?from=&to=
POST /api/v1/weighing/campaigns
GET  /api/v1/weighing/campaigns/{campaign_id}
GET  /api/v1/weighing/campaigns/{campaign_id}/leadership-contract
PATCH /api/v1/weighing/campaigns/{campaign_id}
POST /api/v1/weighing/campaigns/{campaign_id}/plan
POST /api/v1/weighing/campaigns/{campaign_id}/publish
POST /api/v1/weighing/campaigns/{campaign_id}/close-pending
POST /api/v1/weighing/campaigns/{campaign_id}/cancel
```

Shared operator coordination after cutover:

```text
GET  /api/v1/app/tasks?scope=mine&business_date=&state=&cursor=&limit=
GET  /api/v1/app/tasks/{task_node_id}
```

These shared routes own L1 Today/open work, owner/clock/state filters, day/week
markers, and hierarchy. A shared task launches Weighing source detail through
its stable `(source_module, source_type, source_id, source_part)` identity.

Weighing source detail/capture:

```text
GET  /api/v1/app/weighing/bootstrap
GET  /api/v1/app/weighing/work-groups/{work_group_id}
GET  /api/v1/app/weighing/work-groups/{work_group_id}/progress-contract
GET  /api/v1/app/weighing/work-groups/{work_group_id}/animals?status=&cursor=&limit=
POST /api/v1/app/weighing/observations
POST /api/v1/app/weighing/shed-observations
POST /api/v1/app/weighing/observations/{observation_id}/corrections
POST /api/v1/app/weighing/shed-observations/{shed_observation_id}/corrections
POST /api/v1/app/weighing/work-groups/{work_group_id}/submit-progress
```

The current `/app/weighing/work-groups?date=&status=...` and week-list routes
are pre-cutover compatibility worklists. Shadow them against shared task reads,
suppress their navigation/date/status authority when parity is proven, and
retain only bounded source-detail access or retire them after zero-use proof.

All mutating routes require idempotency keys and semantic request fingerprints.
List endpoints must return `items`, `next_cursor`, `total`, and backend-owned
summary buckets. Android must not infer campaign totals from the current page.

API response contracts must include backend-owned:

- row IDs and row versions for campaign, work group, selected shed/partition,
  captured observation, and proof artifact;
- disjoint progress buckets;
- capture, proof, verdict, correction, and recovery reason codes plus display
  labels;
- disabled reasons for publish, submit progress, replace proof, correct
  observation, close remaining, and cancel;
- media upload/recovery states and signed playback/download URLs where allowed;
- deep-link/tap targets for Android and admin-web leadership surfaces.

No client should derive these from local enum guesses or from the current page
of animals.

Backend-owned contracts must include labels, tones, disabled reasons, empty
states, route targets, proof media URLs, summary buckets, and
page-size-independent totals. Android and admin-web render these fields; they do
not hardcode bucket meanings.

API read contracts:

- Shared `GET /api/v1/app/tasks` returns the keyset-paged L1 task list plus
  canonical Today/date/state markers. It suppresses duplicate legacy Weighing
  worklist rows during shadow/cutover.
- `GET /api/v1/app/weighing/work-groups/{work_group_id}` returns only header,
  summary, selected shed memberships, and cursors for animal lists. It must not
  embed all animals for large groups.
- `GET /api/v1/app/weighing/work-groups/{work_group_id}/animals` is keyset
  paged by stable captured-observation/work-row identity and accepts
  status/proof/verdict/shed filters.
- Leadership campaign detail returns summary buckets and paged drilldowns
  separately. A leadership card must not depend on fetching every animal row.
- Every list route must support stable `as_of` or revision semantics so counts
  and pages do not visibly fight each other during sync/reconciliation.

## 10. RBAC

Capabilities should map to existing role grants rather than inventing an admin
role:

| Capability | Actors |
|---|---|
| `weighing.plan` | CEO/CXO only |
| `weighing.monitor` | CEO/CXO and Growth Director |
| `weighing.execute` | Operator and `growth_director` field execution users |
| `verification.review` | Shared evidence read/lens capability; not authority to decide |
| `verification.verdict` | Shared verifier-only approve/rework authority for the separately owned sign-off leaf |

Dinakar uses `growth_director` for monitoring/review plus execution capability.
He is not a planner and does not add field-operator capacity.

RBAC must be enforced in backend query predicates, not only by sidebar
visibility. Every route that accepts `campaign_id`, `work_group_id`,
`animal_id`, `location_id`, or `proof_artifact_id` must re-check tenant and
authorized park/farm/shed scope in SQL or the owning service before returning or
mutating the row. Android role gating is UX only.

## 11. Android UX contract

Sidebar:

- Add Weighing for CEO/CXO, preventive director, and operator personas.
- Show create/edit/publish actions only to CEO/CXO with `weighing.plan`.
- Show review/monitoring surfaces to preventive director/Dinakar with
  `weighing.monitor`, and field execution surfaces when `weighing.execute` is
  present, without create/edit/publish controls.

Leadership screen:

- Week tabs.
- Campaign status for each week.
- Shed/partition selector.
- Captured/accepted/pending counts and the authored date/window.
- Operator assignment display.
- Progress summary: captured, accepted, proof/sync pending, correction-needed,
  selected-scope complete, and shared-task delayed state.

Operator screen:

- Shared Today/My Tasks list; selecting a Weighing task opens exact source
  detail by stable task/source identity.
- Work group detail grouped by selected shed/partition.
- Scan-first flow.
- Weight input.
- Mandatory per-animal video capture for individual animal rows.
- Required shed/partition proof capture for per-shed/partition rows.
- Same table for all captured RFID/tag rows in the selected Weighing bucket.
- Pending state is bucket-local proof/weight/sync readiness only. V1 must not
  compute missing, unavailable, moved, or expected herd animals during submit.
- Offline queue and sync status consistent with current Android capture patterns.

Android data contract:

- Screen reads are network to Room to Flow to UI. Weighing must not ship a
  network-only read repository.
- Do not mutate `ScanViewModel` or `feature-scan` in place with Weighing-only
  assumptions. Extract reusable scan/proof renderer pieces behind neutral models
  if needed, keep the Vaccination adapter and tests intact, and add a separate
  Weighing adapter/route/viewmodel contract.
- Shared L1 task rows plus Weighing source-detail, captured rows, proof upload
  rows, and sync attempts are principal-scoped Room rows and are wiped on
  sign-out. Weighing does not maintain a second L1 task cache after cutover.
- Shared L1 tasks and Weighing captured/source-detail rows use keyset paging
  with a phone-sized page.
- RFID lookup is O(1) against indexed Room/cache state, not a linear scan of a
  large in-memory list.
- Physical RFID attempts are append-only audited separately from accepted
  completion rows. Duplicate RFID/tag values across different selected buckets
  are valid Weighing evidence in V1, not wrong-shed or not-in-campaign failures.
- Accepted scan/result completion uses Room/outbox rows backed by database
  unique constraints and idempotency keys. In-memory guards may improve UX, but
  they are not correctness authority.
- RFID keyboard-wedge key events, including Enter/Tab terminators, are consumed
  only while the Weighing scan route is active. They must never fall through to
  Back, focused buttons, submit actions, navigation, text fields, or unrelated
  form controls.
- Scanning an RFID/tag appends or updates the selected bucket's visible scan
  feed and local observation state from indexed Room/cache state. The app must
  not fetch or render an expected animal roster to decide whether submit is
  allowed.
- UI states distinguish loading, cached/offline, empty, forbidden, backend
  error, sync pending, sync failed, and conflict.
- Route identity includes campaign id, work group id, selected shed/partition
  id, animal id, and proof id where relevant; never "pick first open group" after
  a scan or process restart.
- Per-animal proof capture must survive process death, retry, and offline
  re-entry until synced or explicitly removed by the operator before submit.
- Weight/proof capture writes Room first, queues an outbox command with a stable
  device event id, and syncs through the shared retry engine. The UI renders the
  Room row while upload/acceptance is pending.
- Uploaded-but-unsubmitted proof can be removed by the operator before final
  observation acceptance; this must call a real backend/media endpoint when the
  proof is already uploaded and then update Room.
- The sign-out wipe inventory must include weighing Room tables, proof/video
  cache files, upload work, outbox rows, and saved route/draft state.
- The scan screen must not fetch a Herd Register roster into memory. It should
  page captured rows and resolve RFID through an indexed lookup/cache path.

## 12. Proof/media

Use the shared proof artifact/media system through weighing-specific policies.
Individual animal observations require animal-scoped proof:

- `subject_scope=animal`
- `proof_mode=per_animal_video`
- exactly one mandatory video for each accepted observation
- capture source should prefer in-app camera where existing mobile policy
  requires it

Per-shed/partition observations require shed/partition-scoped proof:

- `subject_scope=shed_partition`
- `proof_mode=shed_partition_video`
- exactly one mandatory video for each accepted per-shed/partition observation
- the proof must be linked to `campaign_shed_id` and the weighing session

Shed/partition proof is not sufficient for individual animal rows. Per-animal
proof is not required for selected per-shed/partition rows.

Backend completion rule:

> **SUPERSEDED (maintainer decision 2026-07-31, migration
> `000007_weighing_free_flow_scanned_identifier.sql`):** the original rule
> below required a **resolved `animal_id`**. Weighing is free-flow: a
> resolved `animal_id` is no longer required to accept an observation. The
> free-flow rule is `weight_kg > 0`, `scanned_identifier` present (a
> resolved `animal_id` is accepted opportunistically when the scan matches a
> known goat, but its absence must never reject the scan), mandatory proof,
> proof upload accepted/recoverable, and idempotent replay. See
> `tools/agent-hooks/check-weighing-free-flow-guard.mjs`.

```text
accepted_weighing_observation  (SUPERSEDED — see decision note above)
requires weight_kg > 0
and resolved animal_id
and proof_artifact_id with subject_scope=animal
and proof upload accepted/recoverable
and idempotent command accepted for the same semantic payload
```

```text
accepted_weighing_observation  (CURRENT — free-flow, 2026-07-31)
requires weight_kg > 0
and (resolved animal_id OR non-empty scanned_identifier)
and proof_artifact_id with subject_scope=animal
and proof upload accepted/recoverable
and idempotent command accepted for the same semantic payload
```

```text
accepted_shed_partition_weighing_observation
requires campaign_shed_id with weighing_category=per_shed_partition
and backend-valid weighing_result
and proof_artifact_id with subject_scope=shed_partition
and proof upload accepted/recoverable
and idempotent command accepted for the same semantic payload
```

An uploaded proof may be removed/replaced before final observation acceptance.
After acceptance, replacement is a correction event with preserved old proof
lineage.

Media implementation requirements:

- Video capture source, max duration/size, transcoding, upload retry, and signed
  URL generation must reuse the shared proof/media policy infrastructure.
- The backend must be able to recover from all partial states: proof uploaded
  without observation, observation submitted with proof still processing,
  idempotency replay after app reinstall/process death, and accepted observation
  whose playback URL needs regeneration.
- Proof lineage must preserve original and replacement artifacts for audits.
- Media URLs exposed to admin-web, Android, and leadership assistant surfaces
  must come from backend contract fields, not hand-built client paths.

## 13. Events and projections

The outward source stream includes campaign create/publish/update, shed
submission/reopen/close/verified-close, observation acceptance/rework/verified,
shed-observation acceptance, and campaign close/verified-close. It must carry
stable campaign/bucket identity, assigned operator, authored business date,
source version, proof/verdict state, and close/reopen facts needed by the shared
task materializer. Current work-item day-start/rolled-forward/delayed/carry-over
events are legacy coordination events: suppress their direct contacts per
tenant/module when shared task contact policy activates, then retire/demote them
after reconciliation and zero-use proof.

Weighing consumes no animal-location, animal-lifecycle, ICU/quarantine,
identity/RFID, vaccination, SOP, obligation, roster, or generic-task event to
decide whether capture may proceed. Proof-engine events may complete evidence
plumbing, but never introduce a herd or clinical gate.

Every event added or consumed must be registered in
`context/architecture/domain-event-registry.json` with durable producer,
consumer, replay, DLQ, and E2E proof. A handler registered only in test wiring or
an unused bus is not accepted.

Read models:

- Weighing campaign/bucket capture and proof/verdict detail.
- Captured identifier/weight history and per-shed/partition result history.
- Proof recovery/review exceptions.
- Shared task-kernel Today, owner, clock, hierarchy, contact, sign-off, and
  close/reopen state. Operator/leadership coordination must not be rebuilt from
  Weighing pages or notification delivery rows.

The implementation must register producer/consumer relationships in the domain
event registry and update leadership assistant coverage or document a deliberate
exclusion.

Projection grain requirements:

- individual captured/proof/verdict source: `weighing_observations`;
- per-shed/partition accepted observation source:
  `weighing_shed_observations`;
- media state source: proof/media tables through the proof port;
- physical-bucket source: `weighing_campaign_sheds` and partition identity;
- shared coordination source: `task_nodes` and its task/contact/sign-off history
  after cutover;
- totals are computed or projected over the full filtered set, never from one
  page of rows;
- every projection row must carry tenant, campaign, farm/park, work group and,
  where relevant, shed/partition grain.
- The Weights dashboard selected date range is binding for gain. Whole-shed
  daily gain and procurement-load gain compare the first accepted weigh date in
  the selected range with the latest accepted weigh date in that same range. If
  the selected range contains only one accepted date for a shed, gain is unknown;
  do not borrow a 28-day/four-week baseline from outside the visible period.
- Weights dashboard shed rows and gain charts show only sheds with accepted
  weigh data inside the selected range. Planned-but-unweighed sheds may still be
  counted in scope summaries, but they are not row/chart items.
- Admin-web row composition chips may show the real breed+sex mix for a weighed
  shed through the recorded `weight_demographics.go` read-only exception. Render
  actual chips such as `F2 / female` and `Anantapur Sheep / male`, not generic
  `mixed breed / mixed gender`. Mixed whole-shed averages are labelled only; one
  shed average must never be split into breed or sex buckets.

Projection/update rules:

- Observation acceptance updates captured/proof/verdict facts, audit, and outbox
  without expected-animal or current-herd state.
- Per-shed/partition observation acceptance updates selected-scope progress,
  proof state, audit, and outbox. It must not update expected-animal `weighed`
  status or animal latest-weight projections.
- Source progress is incrementally updated by campaign + shed bucket. A fallback
  full recompute is allowed only as a bounded repair job for a
  named campaign, never as a hot read path.
- Projection consumers must dedupe by event id and aggregate version. Replay
  must be safe after partial failure between animal observation or
  per-shed/partition observation acceptance, proof linkage, notification enqueue,
  and progress update.
- If no projection table is used for v1, canonical SQL reads must still satisfy
  the same grain proof, page-boundary tests, and 5k-to-50k latency envelope.

## 14. Idempotency and replay

Campaign creation:

- Key source: client idempotency key plus a semantic fingerprint containing
  tenant, park, explicitly authored date/window, selected physical bucket/
  partition identities, categories, authored operators, optional capacity
  guidance, and requested publish mode. It contains no recurrence or animal
  membership snapshot.
- Same key + same payload returns the existing campaign.
- Same key + different payload fails.

Observation submission:

- Key source: device idempotency key per animal scan/proof submission.
- Semantic fingerprint includes campaign bucket, normalized scanned identifier,
  weight, observed timestamp or exact device event ID, and proof reference.
- Same key replay returns existing observation.
- Same campaign + same animal duplicate without correction intent fails or
  returns the accepted observation depending on chosen API behavior.

Shed/partition observation submission:

- Key source: device idempotency key per selected-scope proof/result submission.
- Semantic fingerprint includes campaign, campaign shed, work group, selected
  weighing category, weighing result payload, observed timestamp bucket or exact
  device event ID, and proof artifact reference.
- Same key replay returns existing `weighing_shed_observations` row.
- Same campaign shed duplicate without correction/replacement intent fails or
  returns the accepted shed observation depending on chosen API behavior.
- A shed observation command must fail if the selected campaign shed is not
  `weighing_category=per_shed_partition`.

Outbox consumers dedupe by event id and aggregate version.

Every idempotent write must have a matching database unique constraint or
reservation row. The TRD implementation is incomplete until the migration,
repository SQL, and tests prove:

- exact replay returns the original response and writes no new audit/outbox/media
  side effect;
- same key with different semantic fingerprint fails before mutation;
- duplicate same-bucket/scanned-identifier observation follows the current
  correction/replacement rule; the same identifier may occur in another bucket;
- duplicate same-campaign-shed per-shed/partition observation fails unless it is
  an explicit correction/replacement;
- retry after partial proof upload recovers the same proof/observation lineage;
- retry after partial shed/partition proof upload recovers the same shed
  observation/proof lineage;
- terminal work-group/campaign states reject fresh side effects while allowing
  exact replay.

Submit-progress command:

- Key source: bucket submission idempotency key.
- Semantic fingerprint includes campaign, bucket, operator, captured
  observation/proof ids, shed-observation ids, and source bucket revisions.
- Same key replay returns the same progress response.
- Same key with a different observation/proof set fails with no side effects.
- Terminal `completed`/`canceled` work groups reject new side effects except
  explicit correction/reopen commands.

## 15. Scale and query shape

Current release target follows the 5k-to-50k animal envelope. Avoid full-tenant
hot reads from Android or leadership dashboards.

Requirements:

- Campaign reads page by authored period/date and park scope.
- Source bucket/observation lists page by campaign and physical bucket.
- Shared operator worklists page by canonical task owner/clock/status.
- Source progress is maintained incrementally by campaign/bucket.
- Summary buckets are computed over the full filtered result, not the current
  page.
- Source progress is category-aware: captured observations/proofs for individual
  buckets and selected bucket plus shed observations/proofs for lump-sum work.
  There is no expected membership.
- Indexed predicates keep typed columns bare; do not cast indexed UUID/text
  columns in predicates.
- Task materialization reconciliation is keyset-bounded by stable Weighing
  source identity and version; no herd/lifecycle event participates.
- Read models serving Android and admin-web should have query-plan proof at the
  50k-animal release envelope and keep p90/p95 latency inside the API latency
  policy.

Required indexes should cover:

- `(tenant_id, period_type, period_start_date, status)`
- `(tenant_id, display_week_start_date, status)` for week-tab campaign lookup
- `(tenant_id, operator_user_id, planned_business_date, status)`
- `(campaign_id, location_id)`
- `(campaign_shed_id, normalized_scanned_identifier)`
- `(campaign_id, weighing_category, status)` on selected shed/partition rows
- shared task materialization receipt/source-version lookup by stable campaign
  and bucket identity
- proof/media lookup by subject and aggregate id using existing proof patterns

Expected cardinality envelope for validation:

| Shape | Validation target |
|---|---:|
| Tenant animals | 5k, 25k, 50k |
| One large campaign | thousands of captured observations across selected buckets |
| Many retained campaigns | one year of authored campaign history |
| Observations/history | Multiple raw-identifier observations across retained campaigns |
| Android page size | Around 20 detail rows; no bulk page of 100/1000 |

Hot query contracts:

- Overview queries filter by `tenant_id + period_type + period_start_date/status`
  or by the week-tab display anchor (`display_week_start_date`) and prebuilt or
  bounded progress buckets.
- Shared operator worklist filters by canonical task owner/clock/state and a
  keyset cursor; Weighing detail filters by exact campaign/bucket.
- Captured bucket rows filter by `tenant_id + campaign_id + work_group_id +
  campaign_shed_id` and keyset cursor.
- Observation submit performs indexed lookups for captured bucket rows and proof
  state. It must not load or require expected Herd Register membership.
- Media/proof display preloads proof metadata in one batched query for the page.
  No per-row signed URL/proof lookup loop.

Performance proof expected in implementation:

- EXPLAIN for campaign overview, shared operator task list, captured bucket row
  list, observation insert lookup, and source reconciliation at the
  50k-animal envelope.
- Query-count tests proving no per-animal N+1 proof/media or current-location
  lookups.
- Page-boundary tests proving totals do not change when page size changes.
- API latency proof against the repo policy budget for operator worklist, work
  group detail, animal page, and leadership overview.

## 16. Notifications and reminders

Pre-cutover compatibility notification rules:

| Trigger | Audience | Route | Dedupe key |
|---|---|---|---|
| Campaign published | Assigned shed operator | Open weighing work group | campaign + shed + operator |
| Day-start open work | Assigned shed operator | Today/open weighing work | shed + operator + business date |
| Proof failed or missing after submit | Assigned shed operator | Proof repair screen | observation/proof artifact |
| Authored task clock breached | Shared policy recipients | Shared task/contact lens | task + run + policy version |

Avoid noisy per-capture pushes. Leadership task/contact summaries come from the
shared kernel; Weighing source summaries contain no missing/unavailable animals.

Notification rows must be durable. Delivery failures belong in the shared
notification retry/DLQ path.

Notification contracts must name trigger, audience source, cadence/SLA, summary
copy fields, and tap route. Audience resolution must come from active role
grants/profile truth and assigned operator rows, not hardcoded names. V1
specifics:

- Shared task policy contacts each assigned shed operator for assignment,
  time-bounded open work, and separately classified proof/sync repair only for
  their buckets.
- Shared task policy contacts CEO/CXO and preventive director for policy-defined
  delayed/open summaries or unresolved proof/verdict work.
- Dinakar receives reviewer/supervisor notifications and execution assignments
  when explicitly assigned, not task creation or publish/edit notifications.

Notification durability contract:

At task-kernel cutover, the shared contact engine becomes the sole owner of
assignment/day-start/delay/rework/sign-off contacts and acknowledgement. The
legacy Weighing notification consumer is shadowed, deduplicated by stable source
identity, then suppressed per tenant/module before shared sends activate. It is
retired only after retained-event replay and zero-use proof. Weighing continues
to emit source facts; it does not resolve shared duty/absence/contact policy.

- Assignment, day-start reminder, rolled-forward reminder, delayed-beyond-week,
  proof-upload-failed, and leadership summary notifications are created as
  durable notification requests from backend/domain events.
- Recipient resolution reads active role grants/profile truth. Dinakar receives
  monitoring/review and explicit execution pushes, not task creation, planning,
  publish, or edit pushes.
- A failed FCM/send does not change campaign/work-group state. It retries through
  the shared notification dispatcher and becomes visible in DLQ/repair tooling if
  exhausted.
- Notification tap routes include enough identity to land on the exact weighing
  campaign/work group/shed/proof review context, not "first open weighing task".

## 16.1 Observability and operations

Weighing must emit structured logs, metrics, and traces with low-cardinality
labels plus request/event ids:

- Planner: campaign id, source revision, selected shed count, generated bucket
  count, optional grouping guidance, duration, and edit reason.
- Observation submit: campaign id, work group id, animal id hash/id, proof id,
  idempotency replay/conflict, location match status, and latency.
- Proof upload/recovery: proof id, observation id, upload state, retry count, and
  recovery outcome.
- Shared-task materialization: source event/version, task identity, outcome,
  lag, replay status, source-versus-task reconciliation result, and duration.
- Projection refresh: rows touched, previous/current aggregate version, retry
  count, and DLQ reason on failure.
- Notification: notification request id, event id, recipient role, delivery
  state, retry count, and tap route type.

Operational runbooks must cover: replaying a stuck observation/proof link,
repairing source-to-task materialization for one campaign, regenerating captured
progress for one campaign, inspecting shared-task delayed work, and diagnosing
why a contact did not reach Amit or leadership.

## 17. Tests and validation

Minimum tests before implementation is considered done:

- Planner preserves shed/partition atomicity under cap.
- V1 accepts manually authored kids/K/F campaigns and rejects adult shed
  selection; it does not infer weekly or monthly recurrence.
- Leadership can select `individual_animal` or `per_shed_partition` category per
  selected shed/partition.
- Individually marked sheds/partitions require animal observations with RFID,
  weight, and mandatory per-animal proof.
- Per-shed/partition marked rows require one accepted selected-scope observation
  with required shed/partition proof and must not update individual animal latest
  trusted weight.
- Per-shed/partition completion changes only the selected-scope category; it
  creates no individual observations or implied individually weighed animals.
- `weighing_shed_observations` emit their own recorded/corrected events and
  update per-shed/partition progress projections.
- Shed observation idempotency rejects category mismatches, duplicate selected
  scope submissions without correction intent, and same-key/different-payload
  replay.
- Planner allows over-cap single shed/partition.
- Planning may group whole physical buckets owned by the same operator for field
  routing without creating an expected count. The Weighing source bucket stays
  open/executable until submit/close; the shared task clock alone carries
  unfinished visibility into later business days.
- Campaign created on 2026-07-29 inside week 2026-07-26..2026-08-01 can finish
  after 2026-08-01.
- Dinakar can review/monitor and execute assigned weighing work, but cannot
  create, publish, or edit weighing tasks and is not counted as operator
  capacity.
- The assigned shed operator can execute only their own shed buckets.
- A scanned RFID/tag remains bucket-local Weighing evidence even when current
  Herd Register location differs; capture does not read or mutate herd state.
- Observation without proof video is rejected.
- Duplicate observation/idempotency replay is safe.
- Android offline capture sync preserves proof and weight.
- Progress read models do not multiply rows across sheds/work groups.
- Summary totals remain correct beyond page one.
- Proof upload recovery restores observation proof refs after retry/process
  death.
- Completed/canceled work group rejects fresh submit side effects, while exact
  idempotency replay is allowed.
- Backend-owned contract supplies labels/disabled reasons for leadership and
  Android; clients do not hardcode bucket semantics.
- Leadership close/adjust command requires reason code, actor, timestamp,
  affected count, and affected-grain snapshot: animal list snapshot for
  `individual_animal`, or selected `campaign_shed_id`/scope snapshot for
  `per_shed_partition`. Closed rows leave operator workload but remain in
  audit/reporting.
- Domain event registry covers every weighing producer and consumer.
- Leadership assistant coverage is updated or an explicit exclusion is
  documented.
- Backend migration constraints reject illegal statuses, impossible foreign
  grains, duplicate accepted observations, and orphan proof links.
- API integration tests prove tenant/scope enforcement for campaign, work group,
  animal, location, and proof ids.
- Projection tests include a work group with multiple sheds where only one shed
  submits today and the sibling shed remains pending.
- Fanout/retry tests prove observation acceptance still reaches progress,
  latest-weight projection, notification, and media/recovery queues after a
  transient consumer failure.
- Android tests cover process death/reopen, sign-out wipe, page-2 animals,
  O(1) RFID lookup, proof removal after upload but before acceptance, and route
  identity after scan.
- Android RFID tests prove Enter/Tab terminators are consumed only on the active
  Weighing scan route and cannot trigger Back, focused buttons, submit,
  navigation, text fields, or unrelated form controls.
- Android off-visible-page scan tests prove an RFID for an animal outside the
  rendered page appends/updates the scan feed and local observation state without
  fetching/rendering all animals, while totals stay page-independent.
- Admin-web tests, if leadership screens are added there, prove backend contract
  labels/media URLs are rendered without local fallback copy.
- 5k/25k/50k seed fixtures prove hot query plans remain indexed and bounded.
- Query-count tests prove proof/media and current-location lookups are batched.
- Multi-page campaign tests prove whole-task summaries are stable across page
  size and cursor boundaries.
- Planner hash tests prove deterministic output for identical inputs and safe
  pending-only replan for changed inputs.
- Notification tests prove durable request creation, exact replay, retry/DLQ
  behavior, and role-correct recipients.
- Observability tests or golden log/metric assertions cover key failure paths:
  idempotency conflict, proof missing, stale source-version materialization,
  projection retry, and notification failure. Wrong-shed scan is not a V1
  Weighing failure.

Implementation guard targets to add:

- `weighing-planner-atomicity-guard`
- `weighing-observation-proof-guard`
- `weighing-progress-grain-guard`
- `weighing-mobile-contract-guard`
- `operational-task-kernel-non-deviation-guard`
- `weighing-query-plan-guard`
- `weighing-notification-durability-guard`
- `weighing-observability-contract-guard`

## 18. Open decisions

Already settled and not open: every submitted proof-backed observation creates
a separately owned shared sign-off leaf. `verification.review` may read it;
only a principal with `verification.verdict` may approve or return it for
rework. An individual observation becomes trusted latest-weight truth only after
that shared verdict is applied. Weighing must not invent a private verifier
permission or optional supervisor bypass.

- Exact canonical naming: `animal_id`/`herd_animals` target versus current
  `goat_id`/`goats` implementation names during this slice.
- Whether leadership can manually close remaining pending animals/scopes as
  `closed_by_override` in v1 or only cancel the whole campaign.
- Whether a future reconciliation layer should compare captured RFID/tag values
  to Herd Register location and suggest movement review. That future analytics
  layer must not become a V1 submit gate.
- Whether the daily cap should be tenant-wide, farm-specific, or operator
  configuration in v1. Product default is 100.
- Whether active campaign membership is immutable after publish or supports
  versioned add/remove edits.
- Whether v1 stores progress projections or serves canonical indexed SQL only
  under the 5k-to-50k envelope.
- Whether/when adult monthly weighing re-enters scope, and whether it appears as
  month tabs, a dated 15th card inside the week containing the 15th, or both.
