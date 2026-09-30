# Business and Medical Rule Changes

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## Business and medical rule changes (maintainer lock)

When the maintainer states a **new working rule, condition, timing, or workflow**
(in chat, WhatsApp screenshots, Preventive Care sign-off, or ad-hoc instructions) that may
**contradict or supersede** existing docs, seeded config, implemented kernel
behavior, or a prior decision in the same thread:

1. **Stop and surface the conflict first** — quote the old rule/source and the new
   instruction side by side. Do **not** silently pick one, blend them, or change
   code/docs on assumption.
2. **Ask explicitly** which rule wins, whether the old rule is retired, or
   whether both apply in different scopes (species, stage, procurement path, etc.).
3. **Implement only after confirmation** — then update the canonical source in the
   same change as the code (`docs/preventive-care-vaccination/vaccination-rules.md`,
   published `rule_dsl`, TRD/ADR, or this file when appropriate).

Ambiguity is not approval. Informal agreement in a screenshot or chat applies to
**that** scenario until it is written into the source contract.

Confirmed Preventive Care (PC) vaccination override: never ask about, model, seed,
import, expose, or schedule from mother-not-vaccinated / unknown-mother status.
The private source/wiki may contain that branch, but GoatOS ignores it. Mothers
are kept vaccinated operationally, and every kid uses the approved standard
schedule in `docs/preventive-care-vaccination/vaccination-rules.md`.

Confirmed Preventive Care (PC) Z1+Z3 course rule: Z1+Z3 is a two-dose course
before the 182-day repeat. Dose 2 is due 21 days after dose 1 for both kid and
adult courses. Imported/seeded Z1+Z3 dose 1 must create the dose 2 obligation
first; it must not jump straight to the 182-day repeat. The 182-day repeat
starts only after accepted Z1+Z3 dose 2/course completion. Blue Tongue kid dose
2 is due 21 days after dose 1, at 19 weeks/133 days; pox vaccines still obey
the 28-day live-to-live spacing after PPR.

Hard seed/generation guard: after real vaccination seeding, any accepted
legacy `et_tt_adult_w1` completion without a same-goat legacy `et_tt_adult_w2` obligation or
completion is a broken database, not a warning. Do not report future drives from
`vaccination_drive_assignments` alone; first audit missing required obligations
against `protocol_rules` and accepted history, especially adult Z1+Z3 dose 2.

Confirmed module ownership and weighing planning authority (maintainer decision
2026-08-01): each operational module has ONE accountable director, and a module's
verification notification must reach that director in that module's own wording -- never
another module's recipients or copy. Vaccination -> `pc_director`. Weighing ->
`growth_director`. Feed -> `feed_director`. Counts -> `health_director`, which is a DISTINCT
role from `pc_director` (preventive care) and must not be merged with it. The verifier is
TENANT-level: one verifier reviews proof videos across all parks and sheds. Feed ownership is
documented (`wiki/Handbooks/Feed_Director.pdf`, Role Purpose + M1 daily video double
verification). Counts ownership is a maintainer decision rather than a documented one: no
counting department or counting handbook exists in any source (the live `Counting DB` records
only a "Staff (Counted)" person with no role, no verifier and no approver, and
`Health_Director.pdf` never mentions count, census, headcount or shifting). The routing SHAPE
is well supported either way -- both handbooks carry the same "Meet verifier every day" /
Video Verification Team duty the other directors have.

COUNTS IS AN OFF FEATURE and stays that way. `health_director` is created and recorded as
the counts owner so the module has a declared owner when it is switched on; the role exists
ahead of the feature deliberately.

Note precisely HOW counts is off, because it is easy to switch on by accident: the module is
registered `moduleStatusAvailable` in `workforce/app/bootstrap_copy.go` and is held back only
by `counts.read` / `counts.write`, which today only `ceo_internal` holds. Granting those to
`health_director` would light up the Counts nav for him and thereby ENABLE the feature. So
`health_director` gets counts OWNERSHIP (it is the leadership recipient for a counts/shifting
proof, replacing the silent vaccination default) but NOT `counts.read`/`counts.write` until
the feature is deliberately turned on. Ownership and access are separate decisions here.

Confirmed Health-protocol authoring rule (maintainer decision 2026-08-06): `health_director` DOES
hold `health.config.read` / `health.config.write` -- the authored treatment rulebook behind
`/health/config`. This is a SECOND grant to that role and it is a different kind from the counts
one above: counts ownership is an extension of the handbook, whereas this is squarely inside it
(`Health_Director.pdf` Responsibilities 1-4 put observation, diagnosis, treatment and treatment
tracking on that desk, and the protocol IS the standard those are carried out against). The role
already held `goat.write_health` to record a clinical fact about ONE animal; this lets it author
the standing course EVERY animal with that disease is treated under.

Read the two together and the pattern is: `health_director` gets Health authority in full and
Counts ownership without Counts access. PREVENTIVE CARE ACCESS (maintainer instruction 2026-09-30,
REPLACING "no Preventive Care permission"): it reads the PC Care board (`pc_care.monitor`) and plans
FUMIGATION (`pc_care.plan_fumigation`) -- and nothing more. It is still not the PC Director: no
`pc_care.plan` (deworming, ticks, trimming), no execute, no stock approval, and vaccination protocol
authoring stays on `/config` behind `ProtocolWrite`, which `health_director` does not hold. See
`docs/decisions/pc-care-fumigation.md`.
`health.config.write` is also deliberately withheld from `operator` (executes a course, does not
author it), `park_head` (runs a park's execution) and `verifier` (separation of duty: the verifier
must not rewrite the standard the work is judged against). Only `ceo_internal` and `health_director`
hold it.

TREATMENT PROTOCOLS ARE VERSIONED, NEVER EDITED IN PLACE, and that is a medical-safety property
rather than an implementation preference. An edit builds a DRAFT; publishing promotes it and RETIRES
the version it replaces. `health_cases` pins `health_protocol_version_id` at diagnosis, so a goat
mid-treatment finishes on the dosages it started on and the version it was actually treated from
stays readable forever. Do not "simplify" this into an in-place update: that changes the dose an
animal currently being treated receives.

The Google Sheet (`Adults SOP` / `Kids SOP`) is now a ONE-TIME BOOTSTRAP, not an ongoing sync.
`ReplacePublishedProtocols` retires every published protocol and republishes the set, so running it
after an app edit would silently discard that edit; it therefore fails closed with
`ports.ErrImportAfterAuthoring` once any version carries the `health-config:app` source ref.
`ReplacePublishedProtocolsOverwritingAuthored` is the reviewed break-glass. Canonical prose:
`docs/decisions/health-config-authoring.md`.

Separately: PLANNING a weighing task is CEO-only. `growth_director` monitors weighing across
both parks, oversees the operators and may execute, but does not raise the task; the two
planner reads that feed the create wizard (`/app/weighing/planner/catalog` and
`.../parks/{park_id}/buckets`) are planning surfaces and carry `weighing.plan` despite being
GETs.

Enforcement note: a module that enqueues a verification item MUST have an entry in
`pendingModuleProfiles` (`backend/internal/notificationbridge/verification_notify_consumer.go`)
and at least one `verify` duty holder in `position_module_duties`. There is deliberately no
fallback profile -- an unclaimed module notifies nobody loudly rather than the wrong people
quietly, which is how weighing proofs reached the vaccination verifier and PC Director in
vaccination wording. Both conditions are asserted by tests; neither is a comment.

Confirmed movement rule (maintainer decision 2026-07-19): goats never move
between parks — shed moves exist only within one park; leaving a park is a
terminal transferred/sold exit, never a move. Initial placement is exempt. See
`context/source-findings/goats-and-parks-source-findings.md` → Movement
Semantics.

Weighing vocabulary (maintainer decision 2026-08-03): the weighing workflow has
exactly two verbs — CLOSE a task, or REOPEN it if it is already closed. There is
no third verb, and no force, override or skip variant of close.

THE CLOSE GATE IS UNCONDITIONAL. A bucket cannot close while verification is
pending, and there is no caller-supplied way past that. If a bucket will not
close, the answer is to RESOLVE the verification — get the verdict — never to add
a path around the gate. Machine-enforced by
`tools/agent-hooks/check-weighing-close-gate-guard.mjs`; see
`context/repo-audits/weighing-implementation-do-not-reopen-ledger.md` → D-5.

Confirmed "You" / profile nav placement rule (maintainer decision 2026-08-03,
stated THREE times and implemented wrong twice before this — read it exactly):

> **"You" belongs in the NAVIGATION DRAWER for any principal with 2 or more
> features/modules — CEO, leadership, and a verifier who verifies more than one
> feature. It must NOT be sent as a bottom-bar tab in every feature's bar.**

- **2+ modules** → "You" appears ONCE, in the drawer. Never in the per-module
  bottom bar. Repeating it in vaccination's bar, then weighing's bar, then every
  future verifiable feature's bar is the exact defect being banned.
- **Exactly 1 module** → that principal has no meaningful drawer, so "You" stays
  reachable in their bottom bar.
- "You" must ALWAYS be reachable. Deleting it outright is a regression (that was
  the first wrong implementation).
- This is the same shape as the existing nav-chrome rule: drawer/sidebar when
  there are 2+ modules, bottom bar when there is one. See
  `docs/decisions/role-module-nav-composition.md`.

Two traps recorded so the next author does not repeat them:
1. `shared_key: "you"` does NOT enforce this. `shared_key` has exactly one
   reader, `composeNavigationFromModules`; `verificationModuleForFeature` builds
   its nav items by hand and never calls it, and `visibleNavigationFor` returns
   those items directly. Setting it there was mutation-tested — flipping it back
   to `""` passed the entire suite and changed no served payload. Any mechanism
   used for this rule MUST be mutation-tested: break it deliberately and confirm
   a test goes red.
2. The verifier alerts tab is titled just **"Alerts"** in every locale. The alerts
   stay feature-scoped through `?category=` on the href
   (`vaccination_proof`, `weighing_proof`, `shifting_move`); only the LABEL
   stopped naming the module the verifier is already inside. The per-feature
   label keys (`nav.alerts.vaccination` etc.) and their resolver are deleted —
   do not reintroduce them. The Alerts SCREEN title is likewise just "Alerts";
   it was previously hardcoded to "Vaccination alerts" in
   `AlertsViewModel.kt`, which is also a violation of the backend-owns-labels
   rule.

Confirmed leadership vs. verifier surface separation (maintainer decision 2026-08-06, STANDING LOCK, CORRECTED for deceptive-helper defect): Leadership (audit/overview, read-only evidence trail) and Verifier (action queue, verdict casting) are SEPARATE SCREENS on SEPARATE ROUTES with NO shared composables, ViewModels, or UI components.

Incident report: on 2026-08-06 morning, the leadership "Videos" navigation item was repointed to resolve directly to a verifier route (`/verify/...`), coupling leadership to verifier UI changes and creating UX confusion (verdict buttons appeared greyed out in leadership screens because leadership rendered verifier composables). CRITICAL CORRECTION: `leadershipVideosHref()` is a deceptively-named helper — it RETURNS `/verify?module=X&status=all`, still a verifier route, NOT a leadership-owned route. The guard was initially checking helper NAMES, not return values, and thus passed the exact defect it should catch. The rule and guard now prevent a recurrence.

**Three bans, machine-enforced by `check-leadership-verifier-surface-separation.mjs`:**

1. **Backend nav routes:** Leadership navigation hrefs emitted by `bootstrap_copy.go` MUST point to leadership-owned routes (e.g., `/videos/module`), NEVER use `leadershipVideosHref()` (which returns `/verify*`), NEVER call `verifyQueueHref()`, and NEVER point directly to `/verify` routes. Why: Leadership and Verifier have SEPARATE routes on SEPARATE screens. When the `/videos` screen is built, leadership nav will use `/videos/module`, not `/verify*`. (CORRECTED: previously stated `leadershipVideosHref()` was correct; it is not — it returns a verifier route.)

2. **Android leadership imports:** Leadership-owned screen files (under `.../leadership/` in feature modules) MUST NOT import from `sg.mesha.goatos.feature.verify`, use verifier composables (e.g., `VerifyDetailScreen`), or use verifier ViewModels. Why: rendering a verifier composable couples leadership to verifier state (permissions, verdict handlers) and makes leadership a thin wrapper around the action queue.

3. **Android leadership controls:** Leadership-owned screen files MUST NOT render verdict buttons (approve/reject/rework/reassign) or verdict-casting UI. Why: verdict casting is verifier-only. Leadership sees the RESULT of a verdict (a status chip), not the action to cast it.

Adversarial self-test: `check-leadership-verifier-surface-separation.mjs` includes a test case `bootstrap-bad-leadership-videos-href-returns-verify` that calls `leadershipVideosHref()` (looks correct by name) and verifies the guard FAILS it (correct) — because the helper returns a verifier route. This proves the guard catches the deceptive-helper defect that the old guard missed.

Canonical source: `docs/decisions/leadership-vs-verifier-surface-separation.md` and `context/architecture/verifier-app-and-flow.md`. Machine enforcement: `make leadership-verifier-surface-separation-guard`. This decision is FINAL and LOCKED; any future cross-surface wiring MUST address why the 2026-08-06 incident is not a risk.

Confirmed role-scoped UI is capability-gated (maintainer decision, STG incident 2026-08-12): admin-web pages are ROLE-AGNOSTIC single components — the SAME `/verify` component serves both a verifier and CEO/director oversight via `?scope_mode=company`. Role differences MUST come ONLY from (a) permission-gated endpoints and (b) capability-driven page contracts (`controlEnabled(pageContract, "control_id", false)` / `optionGroup(pageContract, ...)`, compiled in `backend/internal/adminui/app/compiler.go` off named permission constants in `backend/internal/permissions/permissions.go`). NEVER a role-string or permission-string conditional inside a component, and NEVER a per-role page copy. Incident: the CEO's oversight filters on `/verify` (module chips, capture-date range) rendered for every role that could open the page, including `RoleVerifier`, because nothing distinguished the caller. Fix: `permissions.VerificationOversee`, gated at both the page contract (`oversight_filters` control) AND the backend query (`ports.ListQueueParams.OversightFiltersEnabled` — ignores `nav_module`/`business_date_from`/`business_date_to` and blanks `filter_options.modules` for non-oversight callers). When asking an agent for role-scoped UI, name BOTH halves (the contract control AND the endpoint enforcement) in one prompt — a pixel-only ask reproduces this incident's inverse. Canonical source: `docs/decisions/role-scoped-ui-is-capability-gated.md`. Machine enforcement: `make role-scoped-ui-contract-guard` (`tools/agent-hooks/check-role-scoped-ui-contract.mjs`).

Confirmed vaccination progress rule (maintainer decision 2026-08-03): drive
progress is **FIELD WORK DONE = completed + submitted**, never completed-only.
The operator vaccinated the animal, so it counts: a drive whose animals are all
vaccinated and whose proofs are submitted reads **100%** and **"4 of 4 sheds
done"**, and the outstanding video review is carried by the
`verification_pending` status and its chip — never by holding the ring below
100%. The backend owns the single number (`progress_basis`,
`progress_completed`, `progress_total`, `progress_pct`, and `sheds_completed`);
admin-web and Android render it verbatim and must not derive their own.
Pinned by `TestDriveSummaryEmitsBackendOwnedProgressContract` and
`TestCalendarDriveSummaryFiveBucketsAreDisjointWhenSubmittedIsLateOrDeferred`.

**Why this is a lock, not a preference:** an earlier session found admin-web and
Android showing different completion numbers for the same drive and resolved the
parity defect by adopting the stricter surface — making the numerator
completed-only. That silently redefined "done" as "verified" and showed an
operator who had finished every animal in every shed a 0% ring with "0 of 4
sheds done". The choice was then written into two tests and a code comment, so it
read to every later author as intentional. Do NOT revert to completed-only.

**General rule this establishes:** a cross-surface disagreement about a business
number is a MAINTAINER QUESTION, not an implementation detail. Both surfaces may
be wrong, and picking the stricter one is still a product decision. When two
surfaces disagree about what a count means, stop and surface the conflict per the
maintainer-lock rule above; fix parity by making the backend own one number, not
by choosing a client's semantics.

Confirmed shifting TYPED-RAISE rule (maintainer decisions 2026-08-20, SUPERSEDING the
2026-08-15 tag-toggle rule below on WHO decides for a raise that names a category, and
superseding the clinical raise-time refusal FOR `health` MOVEMENTS ONLY): **THE SHIFT
TYPE DECIDES THE TAG — the raiser is no longer asked.** Every typed raise carries a
`category` that IS the rule selector: `health` stamps the destination tag on both legs
(the one type allowed to stamp a clinical state — a health shift IS the health team
acting); `growth` stamps the destination tag FORWARD ONLY along the authored lifecycle
ladder (one reverse edge, Pregnant → Non-Pregnant; sexed stages refuse the wrong sex; and
— maintainer decision 2026-09-23 — a live non-clinical RESIDENT already carrying the group's
next stage decides the tag FIRST, ahead of the pen's set stage, so a K2 joins a pen holding a
K3 (beside an ICU-Kid animal, or under a leftover Stage) and becomes K3; into an EMPTY pen growth never stops -- each animal takes its own
next stage, K3/F2 split by sex, and the pen's Stage is set to it; a group needing different
next stages is refused to be split);
`breeding` never changes the tag; `delivery` stamps the destination tag except never the
newborn stage (into an empty untagged recovery shed the mother keeps her tag and the pen
ADOPTS it); `spacing` moves the WHOLE source pen carrying its tag ("half-half is not an
option") into a same-tag or empty destination (an empty pen adopts the tag); `flushing`
moves females onto the Flushing tag into an empty or already-flushing pen; `normal`
(maintainer decision 2026-09-12) is the plain move — ANY selection, the tag NEVER changes, no pen
is re-tagged, and the destination must be EMPTY or already hold a live animal carrying the moving
animals' tag, read from the RESIDENTS and never from the pen's authored tag (Yashoda 3 fattening
males into Yashoda 9, authored F2-Female but holding fattening males, is allowed; into a pen
holding only fattening females is refused). `spacing` stays unchanged beside it. A raise a
rule refuses is rejected at RAISE time with backend-owned farm copy — before approval and
before any video. Pen adoption is snapshotted at raise (`adopt_pen_tag`, migration
000179) and re-validated under the apply row lock, failing the whole apply closed
(`ErrDestinationPenChanged`) when the pen changed underneath the approval. The client
still names no stage of its own — `target_management_stage` stays rejected; the 2026-08-15
toggle below survives ONLY as the legacy path for a category-less raise from an older APK.
Canonical prose: `docs/features/shifting/shifting-rewrite-tag-rules.md`; rulebook:
`backend/internal/counts/domain.ResolveShiftTypeDecision`. Open decisions recorded there:
Mother/Milking/M0/Warmup have no growth edges yet (a growth raise touching them refuses),
and an approver-chooses-tag capability for an untagged spacing source is a follow-up.

Confirmed shifting TAG TOGGLE rule (maintainer decision 2026-08-15, now the LEGACY path
governing only category-less raises per the 2026-08-20 typed-raise rule above; it had
itself SUPERSEDED the
2026-08-03 no-chooser rule below on WHO decides, and its FLUSHING carve-out outright;
the 2026-08-03 rule had itself superseded the 2026-07-29 three-mode operator chooser and
the 2026-07-20 destination `shed_profiles` authority rule): **the raiser chooses again —
but between two BACKEND-OWNED answers, never a stage of their own.**

The raise form shows a two-position toggle:

```text
keep_current      the animals keep the tag they already carry
destination_stage the animals adopt the destination PEN's tag   (DEFAULT)
```

`stage_mode` on `POST /app/counts/shifting-events` carries the choice. ABSENT means
`destination_stage`, so an APK predating the toggle keeps behaving exactly as it does
today; a present-but-invalid value is REJECTED (`invalid_stage_mode`), never rewritten to
the default — silently defaulting would stamp the pen's tag on a movement whose raiser
asked for the opposite.

**What did NOT change, and is the real content of the 2026-08-03 lock:
`target_management_stage` is still rejected as an unknown field.** The client sends a
MODE; the BACKEND still resolves which tag that means, from the same catalog the form
renders. A phone therefore still cannot invent a cohort, cannot name one the relocation
would refuse at the second gate, and cannot disagree with what the park head approved. Do
not "simplify" the toggle into a stage picker — that is the 2026-07-29 chooser, and it was
retired for these reasons.

**FLUSHING IS NOW ADOPTED.** The carve-out (flushing is a nutrition cohort owned by its own
workflow) is retired. The maintainer was shown the consequence — a move into a flushing pen
puts that animal on flushing ration and re-keys her vaccination schedule — and accepted it.
Migration `000171_flushing_stage_is_writable.sql` lists it as writable, which is the other
half: nothing special-cases the string any more, so the WRITABLE VOCABULARY governs it.

**A CLINICAL STATE IS STILL REFUSED, and is now refused EARLIER.** Bare `ICU`,
`Quarantine`, `sick`, `under_treatment`, `recovering` may never be stamped by a movement:
an animal in one of them has her vaccinations POSTPONED, so a placement action must not
make that medical call. This is now enforced at RAISE time in
`counts/domain.resolveConfiguredStage` (via `protocol/domain.IsClinicalManagementStage`,
the ONE implementation, shared with identity's second-gate guard) rather than only at the
second gate. It matters because a tenant really can list `ICU`/`Quarantine` in
`animal_stage_lookup` — `migrations/postgres/stage_age_band_test.go` seeds exactly those —
so the vocabulary check alone would resolve one at raise and then fail in
`identity/adapters/postgres.resolveDestinationTag` AFTER the operator shot the completion
video and the park head approved. The clinical PEN names `ICU-Kid` / `Quarantine kids` are
NOT states and stay writable (migration 000167): a movement may say which pen an animal is
in, never what condition she is in.

Three cases still keep the current stage because the destination cannot be resolved: a pen
holding more than one cohort, an empty pen, and a tag absent from active
`animal_stage_lookup`. Keeping the current stage is the already-shipped empty-target
behaviour, never a fabricated cohort; do not "improve" it into a majority-resident pick,
which stamps a stage on thin evidence and flips as animals move.

**The unavailable option is GREYED OUT WITH A REASON, never silently inert.**
`GET /app/counts/shifting/destinations` carries `destination_stage` and
`destination_stage_reason` per pen, exactly one of which is non-empty. The reasons are
BACKEND-OWNED farm copy rendered verbatim (`counts/domain.StageReason*`): "This destination
has no tag set", "This destination holds a mix of tags", "This destination's tag can only be
set by the health team". The phone must not compose its own reason from a blank tag — a blank tag does not
say WHY it is blank, and the operator is owed that. The catalog and the raise resolve
through the SAME function, so the tag the toggle advertises is the tag the raise stamps.

Canonical rule: `backend/internal/counts/domain.ResolveShiftingDestinationPenStageDetailed`;
resolution happens at RAISE time so the park head approves the same stage the completion
applies. Pinned by `TestPenStageAdoptsFlushingAndRefusesClinicalStates`,
`TestPenStageReasonsAreFarmWordedAndExclusive`,
`TestRecordShiftingEventHonoursTheRaisersTagToggle` and
`TestRecordShiftingEventRejectsAnUnknownTagToggle` (each mutation-tested when written).
Once Park Head approval and operator completion both exist, the
second-gate transaction must atomically update the goat's `shed_id` and, when selected,
`management_stage`, write identity audit, and publish
per-animal `goat.location.changed` plus `goat.stage_changed` when the stage
changed. Vaccination must consume the result twice: rescope open shed-scoped
work while preserving in-progress/completed history, then re-evaluate clinical
eligibility/schedule. Selecting `Mother` changes only `goats.management_stage`; movement must
not create or modify pregnancy or lactation records, and must not fabricate health,
or other clinical facts; those stay on their authoritative workflows. A real
shifting-completion → Vaccination E2E test is mandatory—separate producer and
consumer tests are not closure.

Confirmed shifting APPROVE-FIRST gate (maintainer decision 2026-08-09, SUPERSEDING
the 2026-07-28 "independent, order-free gates" rule, which itself superseded the
2026-07-26 "verifier approval applies the move" rule): **Park Head approval comes
FIRST, always.** A raised movement is NOT in the operator's Actions work list and
CANNOT be completed until an approver authorizes it. This applies to every
movement, low and high priority alike.

Two halves, and both are enforced:

1. **Visibility.** The Actions `all` bucket EXCLUDES `event_status='pending'`. The
   raiser still sees their movement, read-only, under the retained `pending` tab:
   `primary_action_key='none'`, chip "Awaiting Park Head approval", not tappable.
   The `rework` bucket likewise excludes `pending`, so an unapproved movement
   cannot re-enter the work list through an evidence verdict. Every non-canceled
   row still lands in exactly one bucket, and each tab's count equals what that
   tab lists.
2. **Write.** `POST /app/counts/shifting-events/{id}/complete` REFUSES an
   unapproved movement with `ErrShiftingNotAuthorized` (400
   `shifting_not_authorized`) and writes NOTHING — no `proof_ref`, no completion
   stamp, no verification item. Gated on `authorization_state='authorized'`, not
   on `event_status`, because a legacy `pending_verification` row can be unapproved.

Why this replaced the order-free rule: an operator could burn the mandatory video
— every compulsory capture of the pinned SOP card on a high-priority move — on a
movement the park head then rejected, and a verifier could be handed evidence for
a move nobody authorized.

**ACTIONS LEAD TIME (same maintainer decision, 2026-08-09).** An approved movement
awaiting operator work appears in Actions when it is DUE:

- **High priority → due the second it is approved.** No lead time at all.
- **Low priority → planned work.** Raised BEFORE 13:30 IST it is due the NEXT day;
  raised AT OR AFTER 13:30 IST it is due the DAY AFTER THAT. Due means 00:00 IST
  of that day.

A held movement keeps its RAISED business date and is simply absent from the queue
until due — it does not move to a later date bucket. Consequence to know: on its
due day the operator must page back to the raise date to find it, which is what the
previous-dates strip is for. Anchoring on RAISE time also makes a late approval
self-solving: if approval lands after the due instant, `now` is already past it and
the row appears immediately, so approved work is never hidden in the past.

Held rows are ONLY `event_status='authorized'`. An applied movement (completed, or
in evidence rework) is history and is never held — hiding it would erase work an
operator demonstrably did. A `pending` row is never held either, so a raiser always
sees what they just raised.

This is NOT an authority gate: completion is NOT blocked before the due date,
because the animals may genuinely have walked today and refusing to record a real
movement would make the herd register lie. Approval remains the only gate on
completion.

**This binds the ACTIONS QUEUE ONLY.** It does NOT change the feed-direction
shifting projection, which by the 2026-07-27 decision below has NO lead time and NO
priority branch ("forget high priority") and counts every authorized-but-unexecuted
movement immediately. The two rules look alike and are not: this one decides when an
operator is SHOWN work, that one decides how many mouths a shed is fed for. Do not
collapse them, and do not read this as reviving the retired normal-2-day /
high-priority-1-day projection lead.

Canonical rule: `counts/domain.ShiftingActionsDueFrom` (mirrored in SQL by
`shiftingActionsVisibleSQL`, which the page, the status counts, and the
previous-dates strip all share so a tab badge cannot advertise work the tab hides).

The approval-arrives-second apply branch in `authorizeShiftingEventInTx` is KEPT
deliberately as rollout compatibility for rows completed under the retired rule;
it is not a supported new path. Do NOT delete it, and do NOT treat its existence
as permission to complete before approval.

Operator completion still requires a MANDATORY live-camera video
(`shifting_events.proof_ref`; blank is 422). The COMPLETION transaction — now always
the second gate, since approval must already exist — atomically updates canonical
`goats.shed_id` and destination stage, publishes location/stage events, flips the
movement `applied`, and therefore moves Herd Register / Counts. Approval alone still
moves NOTHING. Verification is post-task evidence review only:
APPROVE marks evidence verified; REWORK creates evidence rework/audit without
changing the applied movement or rolling back goat location/count. Generic
Verification enqueue and verdict consumers remain wired, but verdicts do not own
census truth. Canonical source: `docs/decisions/shifting-verification.md`; forward
migrations `000049_shifting_approval_completion_gate.sql` and
`000050_shifting_actions_index.sql`.

Confirmed high-priority shifting feed-evidence rule (maintainer decision 2026-07-29): low-priority
shifting remains the existing one-live-camera-video flow. High-priority shifting embeds feed packing
and feeding inside Shifting, resolves exact feed type/quantity from active destination Feed Config
matched to the movement's EFFECTIVE management stage and moved animals' ration groups, and requires
the captures of the movement's PINNED shifting SOP card: the completion card plus the high-priority
card (SHIFTING SOP, maintainer decision 2026-09-16, `docs/decisions/shifting-sop.md`; the seed is
the three live-camera videos -- shifting, feed packing, and configured feed being given to the
animal(s) -- and an authored card may rename, replace or add captures and questions, each section
keeping at least one compulsory capture). Every capture is reviewed together in ONE `shifting_move`
verification item, named by its slot title. Embedded packing
proof is shifting-scoped only and never creates or completes the separate Feed Packing/Feed
Distribution workflows. Park Head approval + operator completion still apply location/stage/counts
on the second gate; verification remains post-task review and rejection creates operator rework
without rollback. Missing config blocks, and a semantic fingerprint shown to the phone is
revalidated under the shifting row lock so changed config returns `feed_config_changed` rather than
guessing.

EFFECTIVE STAGE (maintainer decision 2026-08-12): the ration is priced against the snapshotted
target stage, or -- when that is BLANK -- against each ANIMAL's own current stage. Blank is the
normal outcome whenever `ResolveShiftingDestinationStage` declines to adopt a destination cohort
(empty pen, mixed pen, clinical state, or a cohort the relocation cannot write); it means "keep each
animal's current stage", NOT a missing input, and the raiser is never asked for a stage. Keying the
ration off the blank target hard-blocked EVERY high-priority movement into an EMPTY PEN with
"selected destination management stage is missing" -- naming a choice the phone does not offer. Do
not restore that key. An animal with no stage on either side still blocks, with a message naming
the herd-data gap rather than blaming the raiser. Canonical source:
`docs/decisions/shifting-verification.md`; migration
`000053_high_priority_shifting_feed_evidence.sql`.

Confirmed feed-distribution verification gate (maintainer decision 2026-07-26,
SUPERSEDING the "operator marks a shed-session fed (optional video), completed at
submit" contract FOR the feed-DIRECTION operator flow ONLY): a feed-direction
shed-session is completed only after a verifier approves the operator's proof.
The operator submits TWO MANDATORY proofs per session — a feed-distribution VIDEO
(`distribution_proof_ref`) and a water-distribution proof that may be PHOTO OR
VIDEO (`water_proof_ref`); a completion missing either is rejected 422
`proof_required`. That flips a NEW `feed_distribution_completions` row to
`pending_verification` and enqueues ONE `feed_distribution` verification item
carrying BOTH proofs — NOTHING is completed yet. ONE verifier APPROVE
(`ApplyVerifiedDistribution`) covers both proofs and flips the session to
`completed` (this is when `feed.distribution.completed` is emitted); a REJECT
(`BounceDistributionForRework`) flips it to `rework` for a re-shoot. Applies to
BOTH `normal` and `experiment` workflows. Feed direction is app-only: the
`/feed/direction` admin-web left-bar leaf is removed (keep `/feed/config`).
Wiring is the generic Verification module: producer `feeddirection`
`CompleteDistribution` + `feeddirection/adapters/verificationbridge` enqueue;
consumer `feeddirection/app.FeedDistributionVerificationHandler` on
`verification.verdict.approved`/`.rework`, filtered to `source.module=feed,
ref_type=feed_distribution_completion`. Canonical source:
`docs/decisions/feed-distribution-verification.md`; migration
`000032_feed_distribution_verification_gate.sql`.

Confirmed TRANSITIONAL feed-stock concentrate merge (maintainer decision
2026-09-06), and it is the rare rule here that is meant to be DELETED: the farm
retired four in-house concentrates (adult goat/sheep, kids goat/sheep) into two
(one adult, one kids), so while the old sacks last the store holds up to three
feeds that are one feed operationally. The Stock cards and the low-stock alert
fold each retired split feed into its successor on BOTH sides of the division --
family stock is the sum of its members' balances, and the family rate is the
family's kg per CALENDAR DAY. On 2026-09-06 CBE read "2 days left" beside "166
days left" for what is one feed; the answer is 12.

THE RATE IS NOT THE SUM OF THE MEMBERS' RATES, and that is the whole trick: the
feeds SUBSTITUTE for each other while the ration grid switches over (CPT fed 84
kg of the successor INSTEAD of the sheep feed on 5-6 Sep), so summing each
member's own 3-day average read 184.9 kg/day against a true family draw of
128.2 -- 40% high. Re-group consumption to the family BEFORE averaging.

A negative member is SUBTRACTED, not floored (that feed came out of a sibling
sack); the retired-vocabulary check reads the FAMILY key, so a retired MEMBER
still contributes while a retired feed with no successor still drops out; and
BOTH the cards and the daily push take the same mapping, because a push saying
"2 days" beside a tab saying 12 is the cross-surface disagreement this file
bans. The per-farm Mesha concentrate table, the 7-day forecast, expenditure and
the whole operational feed chain are deliberately NOT folded -- this is a
reporting fold and no write path reads it.

SEPARATELY AND ALSO TEMPORARY (maintainer instruction 2026-09-06): CBE's
`Concentrate` burn rate is HARD-CODED at 55 kg/day in
`domain.StockRateOverrides`, against a computed 64.9 -- one farm's one feed
answered from the maintainer's knowledge rather than the sheet. It pins the
days-left divisor, the kg/day the card SHOWS, and the daily push, from one
table; it touches no other farm-feed pair and no other surface. A ~10 kg/day
gap on one feed is worth diagnosing, not keeping.

IT EXPIRES ON ITS OWN: once the members' stock reaches zero and the grid names
only the successors, each family is a single feed and `domain.StockFamilyMerge`
plus its two query parameters can be deleted with no visible change. An EMPTY
mapping is the pre-merge behaviour exactly, verified against live STG. Do not
grow this into a general substitution model -- that belongs on
`feed_item_catalog` as a maintainer decision. Canonical prose and the revert
recipe: `docs/decisions/feed-stock-transitional-concentrate-merge.md`.

Confirmed SALE -> FEED DIRECTOR NOTICE rule (maintainer decision 2026-09-07): when animals are
TAGGED to a sale (the sale-allocation confirm, the moment they leave the register), the Feed
Director -- and ONLY the Feed Director; park heads and the CEO were offered and declined -- gets a
push naming every pen, how many animals left it, the sale date and the FEED DAY the reduction lands
on. On that feed day, from 07:00 IST, a second push asks whether feed did reduce for those pens. The
reminder carries NO VALUES ("just a reminder is enough"): no kilograms, no sheet head counts. The
feed day is the park's own correction clock's answer, never a constant: before the normal-workflow
correction cutoff -> D+1, at or after it -> D+2, no clock -> D+1 (`feeddirection/domain.SaleFeedReductionDay`).
The confirm emits ONE `goat.sale_allocated` event per batch beside the per-animal `goat.exited`
(the Feed Director's grain is the pen; do not rebuild it from a hundred goat events); the reminder
is a stage on the shared cadence keyed per confirm, not a cron. The commercial sale row on
`/sales/config` fires nothing -- it knows no pens. Canonical prose:
`docs/decisions/sale-feed-reduce-notification.md`.

Confirmed FEED PURCHASE ENTRY rule (maintainer decision 2026-08-24, SUPERSEDING the READ-ONLY
half — and only that half — of the 2026-08-17 lock recorded in migration `000174`): feed bought
for CBE and CPT is now RECORDED IN THE APP on `/procurement/feed-purchases`, carrying the same
fields the legacy Feed DB sheet's Purchase row keeps. 000174's own comment said "There is no
authoring UI; purchase/vendor entry screens belong to the future Procurement vertical" — that
vertical now exists, so the screen was built where the lock said it belonged.

The other two decisions in 000174 STAND and are enforced on the write path: CURRENT-CATALOG FEEDS
ONLY (an entered feed must resolve to an ACTIVE `feed_item_catalog` row, checked inside the write
transaction; unknown feeds are rejected, never invented into the catalog) and STOCK DEPLETES AT
SHEET LOCK (an app row sets `depletes_from = purchase_date`, `consumed_at_import_kg = 0`, so the
existing stock/days-left read on `/feed/analytics` needed NO change). PROCUREMENT owns the write;
feeddirection keeps the read.

`feed.purchase.read` / `feed.purchase.write` are DEDICATED permissions, never a reuse of
`ProcurementRead` — `operator` and `park_head` hold that for the source-entry screens they work,
and this ledger carries supplier prices and payment state. `feed_director` holds READ ONLY: it
owns what the farm feeds and is accountable for the stock cards these loads are counted from, but
buying is the procurement desk's job. Canonical prose: `docs/decisions/feed-purchase-entry.md`;
migration `000206_feed_purchases_app_entry.sql`. Pinned by
`TestRecordFeedPurchaseControlIsCapabilityGated` (the feed_director row is the mutation test: it
holds every feed permission there is, so enabling the control from a broader key turns it red),
`TestFeedPurchaseRolePermissions` and `TestFeedPurchaseRoutesAreGatedOnTheDedicatedPermissions`.

Confirmed EXPERIMENT FEED IS AUTHORED PER ANIMAL (maintainer decision 2026-09-01,
SUPERSEDING the "absolute_kg is a shed total and head_count is never a multiplier"
rule for every NEWLY authored cell, and only for those): an experiment pen is still
hand-entered cell by cell with no ration grid involved — only the question each cell
answers changed. `grams_per_head` is what ONE animal gets, and the feed sheet
multiplies it by the pen's LIVE projected head count, the same count the ration grid
uses. The pen's shed factor is deliberately NOT applied: an experiment quantity is
grams x head count and nothing else.

`feed_experiment_config.head_count` STAYS INFORMATIONAL and is still never a
multiplier — it records the population the author had in mind, and scaling by it would
freeze a pen's quantity at the count typed on the day it was authored.

THE EXISTING VALUES ARE SEEDED ACROSS (maintainer instruction the same day, REPLACING
an earlier "no previously authored data changes" answer in the same conversation):
migration `000238` converts every legacy cell in place as
`grams = trunc(kg x 1000 / the pen's LIVE resident count, 3)`.

USE LIVE ONLY, FORGET RECORDED (maintainer instruction, same day, and it governs the
whole module). `feed_experiment_config.head_count` — a figure an author once typed beside
the quantity — is no longer read, written, asked for or shown; it disagreed with the
actual population on 15 of 34 pens and nothing maintained it. It survives only as
provenance for what the conversion divided by. Every count anything shows or multiplies
by is the pen's LIVE population: the sheet's, and the Feed Config screen's `live_head_count`,
resolved per request from the herd register. Dividing the conversion by the live count is
also what makes it SAFE — the rate back-multiplies by the number it was divided by, so no
pen's feed moves on conversion day; from tomorrow the total follows the animals. A pen with
NO live animals has no denominator and stays on the legacy basis. The rate is TRUNCATED,
never rounded to nearest: the generator rounds a session quantity UP to a packable 0.1 kg,
so a rate a hair above exact lifts an unchanged pen's sheet by a notch.
`feed_experiment_basis_conversions` keeps every conversion's inputs — including the live
count, which nobody could reconstruct later — so the arithmetic is auditable and the Down
path exact.

The table still carries two figures and a `quantity_basis` naming which one a row holds:
a row with NO usable count cannot be converted and stays legacy, and the basis is per
CELL. The two readings differ by the pen's ENTIRE POPULATION, so a cell whose basis
cannot be read BLOCKS rather than resolving to either number. Every write authors the
per-animal basis; there is no route that writes a pen total any more. The workbook seeder
converts on the way in with the SAME truncated arithmetic and the SAME live denominator (a
migrated database and a seeded one must land on identical rates), falling back to the
workbook's own count only for a pen with no live animals — otherwise the ORDER of two seed
commands would decide whether feed config lands at all. It skips any cell stamped
`source = 'app'`, so a re-seed cannot discard a rate the farm corrected on screen.

Canonical prose: `docs/decisions/feed-experiment-per-animal.md`; schema: migration
`000237_feed_experiment_grams_per_head.sql`; rulebook:
`feeddirection/domain.ExperimentPlanner`. Pinned by
`TestExperimentStrategyMultipliesGramsPerHeadByTheLiveHeadCount`,
`TestExperimentPenMixingBothBasesReadsEachCellOnItsOwnBasis` and
`TestExperimentCellWithUnknownBasisBlocksRatherThanGuessing`, with the legacy
`TestExperimentStrategyUsesAbsoluteKgAndIgnoresHeadCount` kept unchanged beside them.

Confirmed feed-PACKING SHED-SESSION grain (maintainer decision 2026-08-11,
REVERTING the 2026-08-10 PEN-DAY grain in full and restoring the shed-SESSION grain
of the packing gate below): a pen's morning and evening shares are TWO SEPARATE
BAGS. Each is packed on its own, filmed on its own, and verified on its own — TWO
CARDS, TWO VIDEOS, TWO verification items per pen per feed day. Completion grain is
`(tenant, park, shed, partition, session_no, target_date, workflow)`, the natural
key `feed_packing_completions_natural_uq` has always carried (migration `000150`).

Why the pen-day merge was wrong: ONE CLIP CANNOT PROVE TWO BAGS. The two shares are
weighed out at different times, so a single video shows at most one of them, and a
verifier judging it against a day total cannot tell a crew that packed the morning
share twice from one that packed both correctly.

**Do NOT re-merge them.** The specific things that came back, each of which the
merge had removed:

1. `session_no` is REQUIRED on `POST /feed-direction/packing/complete`. A missing or
   `0` value is REJECTED (`ErrInvalidSession`): `0` is not "the whole day" — it is a
   value no worklist line matches, so accepting it would write a row the operator's
   bag never resolves to and leave that bag showing as still owed. The DB agrees —
   `CHECK (session_no >= 1)`.
2. `/feed-packing/worklist` accepts `session` again (0/absent = every session).
   `summary.line_count` counts pen×session lines.
3. The verifier's item is subjected `Session N · Castro 2`. Without the prefix a
   verifier holding a pen's two cards cannot tell which bag each clip proves.
4. The expected-ration context on that item names THAT SESSION's quantities, not the
   day's — one clip proves one bag, so a day total would show twice what the video
   should contain.
5. `packingCompletedKey` is gone; packing shares the session-bearing `completedKey`
   with distribution again.

Two things the merge did NOT touch and that stay as they are:

- **The PEN is part of the key.** Castro 1/2/3 are different animals on different
  rations; `000137` exists because one Castro 1 clip closed out all three. This
  survived the merge and must survive any future change.
- **Feed DISTRIBUTION was never merged** and needs no repair.

WHAT MIGRATION `000150` CAN AND CANNOT UNDO, because a future reader will ask. It
drops `feed_packing_completions_pen_day_uq`, promotes each surviving pen-day row's
`session_no` from the sentinel `0` to `1` (its video and verdict stand as the
MORNING packing; the pen's evening reappears as work still owed), restores the
`>= 1` check, and puts the `Session N · ` prefix back on in-flight verifier labels.
It CANNOT restore the rows `000149` DELETED when it collapsed each pen-day — those
are gone, and those pens' second bags simply reappear unpacked, which is the honest
state. `000149` is NOT amended: it is already applied on STG, and STG records
migration checksums.

The Android outbox needs the same promotion: a packing row queued by the pen-day
build carries no session, decodes as `0`, and `SyncEngine` maps it to session 1 —
the same choice `000150` makes server-side, so phone and database agree on what an
unlabelled pen-day video proves. The Room packing cache namespace is bumped
(`session-v3`); a stale `sessions`-shaped cached row would otherwise deserialize
WITHOUT ERROR into a card with no feed lines at all.

The afternoon correction (rule above) reopens **EVERY SESSION** of a pen whose head
count moved, never just one: head count scales the morning and evening ration alike,
so both videos now prove the wrong quantity and a partial reopen would leave one bag
packed for a head count the farm no longer has. `ReopenPackingForFeedChange` names
pens WITHOUT a session and applies no session predicate. A verdict already CAST is
kept as history (only a still-`pending` item is `withdrawn`), while the completion
row loses `verified_by`/`verified_at` and returns to `rework`.

Canonical prose: `docs/decisions/feed-distribution-verification.md` → "Feed packing
is proved ONCE PER BAG". Pinned by `TestPackingBagIsOnePerPenPerSession`,
`TestPackingLinesKeepPartitionsAndSessionsApart`,
`TestReopenPackingWithdrawsPendingItemsAndKeepsCastVerdicts` (mutation-tested two
ways: a session predicate on the reopen, and withdrawing a cast verdict — each turns
it red) and the `TestKernelStory_FeedAfternoonCorrection` E2E.

Confirmed feed-PACKING verification gate (maintainer decision 2026-07-26,
SUPERSEDING the "FEED PACKING IS DELIBERATELY NOT GATED" rule that the
feed-distribution lock above originally carried; its GRAIN was briefly superseded by
the 2026-08-10 pen-day rule and RESTORED by the 2026-08-11 rule above): feed PACKING
is now gated the same way as feed direction. The operator completes a packing
shed-session with
ONE MANDATORY packing VIDEO (`packing_proof_ref`); a completion missing it is
rejected 422 `proof_required`. That flips a NEW `feed_packing_completions` row to
`pending_verification` and enqueues ONE `feed_packing` verification item carrying
the video — NOTHING is completed yet. ONE verifier APPROVE
(`ApplyVerifiedPacking`) flips the shed-session to `completed` (this is when
`feed.packing.completed` is emitted); a REJECT (`BouncePackingForRework`) flips
it to `rework` for a re-shoot. Applies to BOTH `normal` and `experiment`
workflows; the serve overlay reads `ListPackingCompletionStatuses`. The gated
flow is a SEPARATE record on a NEW table, never an ALTER of the old packing
table. The OLD instant packing path — `feed_direction_session_completions`
(migration `000030`), `POST /feed-direction/complete`, `feed.direction.completed`,
`CompleteSession`, and the mobile `FeedCompleteScreen`/`feedCompleteRoute` — is
left INERT (no longer navigated to from packing) but not deleted; retiring it is
a separate cleanup. Both feed gates share `source.module=feed` and are kept apart
ONLY by `ref_type` (`feed_packing_completion` vs `feed_distribution_completion`).
Wiring: producer `feeddirection` `CompletePacking` +
`feeddirection/adapters/verificationbridge` `NewPacking`; consumer
`feeddirection/app.FeedPackingVerificationHandler`. Route
`POST /feed-direction/packing/complete` (registered in `permissions/routes.go`
alongside the distribution route, which had been unregistered and would 403).
Canonical source: `docs/decisions/feed-distribution-verification.md`; migration
`000033_feed_packing_verification_gate.sql`.

Confirmed Feed Transport daily verification rule (maintainer decisions 2026-07-29
and 2026-08-10, REAFFIRMED 2026-08-12 against a partition grain, SUPERSEDING
transport session/batch/consolidation wording): Feed
Transport is one daily task per active physical shed and is never per feed session.

**NOR PER PARTITION.** A shed's pens are packed and fed as separate bags, but they
are LOADED AND STAGED as one trip, so transport is ONE task and ONE video for the
whole shed. Migration `000143` fanned the materializer out over `shed_partitions`
and a partitioned shed began listing `Castro 1`, `Castro 2`, `Castro 3` as
three transport tasks -- three videos of one load. That was never a recorded
decision; it contradicted this rule and `docs/decisions/feed-transport-verification.md`
at the same time. `000152_feed_transport_restore_shed_grain.sql` is the forward
repair (`000143`/`000146` are NOT amended -- STG records checksums). It retires only
UNSTARTED pen tasks; a pen task already carrying an attempt keeps its status and its
proof, because an operator really filmed it. `partition_label` is kept and stops
being written. **Pen grain belongs to PACKING and DISTRIBUTION** -- those really are
one bag per pen -- and copying their shape onto transport is the specific mistake
this paragraph exists to stop.
The controlling source clock requires packed/diff-corrected feed to be loaded and
staged outside sheds by Day N 15:00 for Day N+1 service. The current 15:30 task
creation is compatibility behavior and a source/runtime defect: materialize and
assign the task early enough to complete by 15:00; a versioned route policy may be
stricter. The operator records one mandatory fresh in-app-camera video; submit
moves the task to
`verification_due`. Verifier APPROVE moves it to `completed`; REJECT moves it to
`rework` assigned to the same operator. Every rework requires a new video and appends
a new proof attempt; rejected proof attempts remain immutable history. Canonical source:
`docs/decisions/feed-transport-verification.md`; migration
`000054_feed_transport_daily_verification.sql`.

Confirmed verifier verdict-exclusivity rule (maintainer decision 2026-08-03, SUPERSEDING the
CEO/CxO `verification.review` override for the DECISION only): approve/reject on a verification
item belongs to the Verifier role ALONE. `verification.verdict` is split out of
`verification.review` and granted to `verifier` only — never `ceo_internal`, `pc_director`,
`growth_director`, `park_head`, or `operator`. Leadership KEEPS `verification.review` (see the
evidence queue, media, and recorded verdicts) and KEEPS `verification.act` (close the work, rework
or reassign the source task); it simply cannot sign the second check itself. Do not "fix" this by
restoring the verdict grant to CEO to satisfy the founder/builder visibility invariant — that
invariant is satisfied by the read, and an independent check the checked party can approve is not
independent. Route `POST /verification/items/{item_id}/verdict` is gated on
`verification.verdict`; `GET /verification/queue` stays on `verification.review`. The admin-web
`record_verdict` control and `isVerifierLensPrincipal` both key on `verification.verdict`.
Canonical source: `context/architecture/verifier-app-and-flow.md` → "Roles (truth table
alignment)"; pinned by `TestVerificationSeparationOfDuty` and
`TestVerdictRouteIsVerifierOnlyWhileQueueReadStaysLeadershipVisible`.
