# Weighing / Alerts Consolidated Audit Findings (2026-08-01)

> State of play: four independent audit passes ran this session over the same
> uncommitted working tree (`git status --short` at write time: 112 changed
> paths, none committed) — (1) per-feature/per-role segregation across
> backend+mobile+FCM+i18n+design-system, (2) a deep pass over DB/migrations,
> operational kernel, Android data layer, cross-surface consistency, plus the
> weighing feature-module extraction, (3) an exhaustive alerts/FCM audit, and
> (4) a judge pass on the new `feed_director`/`health_director` role-activation
> diff (verdict: **REDO**, 8 corrections C1-C8). Headline counts: audit 1 —
> 23/44 confirmed (verdict prose says "44 candidates"/"21 rejected"; the
> structured judged lists actually contain 23 confirmed + 16 rejected = 39 —
> recorded as a discrepancy, not smoothed over); audit 2 — 30 ranked findings,
> 10 flagged E2E-blocking; audit 3 — 48 candidates, 26 confirmed, 11
> E2E-blocking (structured counts, authoritative); audit 4 — 8 corrections
> (C1-C8), verdict REDO.
>
> **The single most important conclusion**: the notification system is
> non-functional end to end. `position_module_duties` and
> `notification_requests` both hold **0 rows**, verified live against the
> phone-QA DB (`postgres://postgres:goatos@127.0.0.1:15544/goatos`, container
> `goatos-phone-qa`) on port **15544**. Every verifier push, for every module,
> currently resolves to zero devices. A fix is written
> (`deriveVerifierDuties`/`assertVerifyDutyCoverage` in
> `backend/cmd/seed-position-duties/main.go`) but is **uncommitted and
> unproven** — it has never been run against a DB to confirm it populates the
> table, and audit 4's C2 finding shows the seed currently **aborts before
> inserting anything** under `-strict` because `video_verifier` is unmapped in
> `modulePrefixes`. See §0 and F1/F4 below. Full session context, maintainer
> decisions, and the two-layer alert-testing doctrine live in
> `context/repo-audits/weighing-alerts-session-ledger.md` — this file does not
> restate that content, only cross-references it.

## 0. Source audits and where they live

This session's four audits existed only as transient task-output files, not
repo artifacts, which is why the session ledger above could not verify their
counts. They are:

1. Per-feature/per-role segregation (backend + mobile + FCM + i18n +
   design-system) — task `watsw94vm`. Structured judged result: 23 confirmed,
   16 rejected (verdict prose states different totals — see caveat above).
2. Deep audit (DB/migrations, operational kernel, Android data layer,
   cross-surface consistency) + weighing feature-module extraction verdict —
   task `wz3hscxxt`. 30 ranked findings, 10 E2E-blocking.
3. Exhaustive alerts/FCM audit — task `w7o37w1xc`. 48 candidates, 26
   confirmed, 11 E2E-blocking, per-module notification matrix (§3 below).
4. `feed_director`/`health_director` role-activation judge pass — task
   `wdkapwxor`. Verdict **REDO**, corrections C1-C8 (§4 below).

This file is the durable artifact; the task-output files are not committed
anywhere and will not survive past this session.

## 1. Consolidated findings table (deduplicated, worst first)

Spot-checked rows (marked **[SC]**) were independently reopened at the cited
file:line against the current working tree during this consolidation; all 8
citations checked were accurate. Status reflects the actual working-tree state
at write time (`git status`/`git diff`), not the audits' original text, per
the maintainer's fix-verification rule.

| ID | Area | Severity | file:line | Description | Blocks E2E | Status |
|---|---|---|---|---|---|---|
| F1 | alerts/kernel | Critical | `backend/cmd/seed-position-duties/main.go:282-284`, `workforce/adapters/postgres/roster_repository.go` (`ResolveModuleDutyRecipients`) | `position_module_duties` and `notification_requests` both have 0 rows on the phone-QA DB; `deriveDuties` only ever emits `execute`/`manage`, never `verify`, so every verifier push for every module resolves to zero devices. | Yes | fix-written-unproven — `deriveVerifierDuties`/`assertVerifyDutyCoverage` exist in the uncommitted diff but have never been run against a DB to confirm they populate the table |
| F2 **[SC]** | mobile/alerts | Critical | `apps/goatos-android/feature/feature-auth/.../PermissionGate.kt:62` | `POST_NOTIFICATIONS` is never requested app-wide (`PermissionGateCard` has one repo-wide hit — its own declaration; `areNotificationsEnabled` is used only in `core-permissions/AppPermission.kt`, its test, the manifest, and `CaptureAccessGate.kt`, none of which is a route every role passes). On Android 13+ this defaults denied, so FCM accepts/marks-delivered and the OS silently drops every push to a role that never opens a capture screen. | Yes — and until this lands, every other alert fix tests as a false negative | open — confirmed unwired at write time |
| F3 **[SC]** | roles/backend | Critical | `backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql:5775,5778`; no `000057`-style migration exists for the new roles | `org_role_catalog` has no `feed_director`/`health_director` rows (`grep -rn 'feed_director\|health_director' backend/migrations/` returns nothing this session, confirmed). `user_scope_grants_role_fk`/`auth_pending_email_grants_role_fk` are FKs to `org_role_catalog(role_key)`, so every grant INSERT FK-violates — nobody can actually hold either role. | Yes | open — no migration found; this is audit 4's **C1** |
| F4 | alerts/kernel | Critical | `backend/cmd/seed-position-duties/main.go:75-87,196` (`modulePrefixes`, `-strict` abort) | `video_verifier` has no entry in `modulePrefixes`; `deriveDuties` increments `UnmappedSkipped` and, under `-strict` (both real Makefile invocations use it), the seed returns `"strict position duty seed rejected N unmapped position code(s)"` **before any row is inserted**. This is the mechanism that keeps F1's fix from ever running. | Yes — directly blocks F1's fix | open — audit 4's **C2**; fix proposed (`skip the verifier seat before the matchModule branch`) but not applied |
| F5 | alerts/backend | Critical | `backend/internal/notificationbridge/verification_notify_consumer.go:331,419,699` | `handleVerdictApproved`, `handleItemClosed`, and `handleVerdictRework` all still hardcode `positionPCDirector` and vaccination-worded titles/bodies for every module — only `handleItemPending` (line 542, via `pendingProfileFor`) is module-routed. A feed, weighing, or counts approval/rework/closed event pages the PC Director in vaccination wording instead of that module's own director. | Yes | open **[SC — confirmed at all three cited lines]**; this is audit 3's finding #7 and audit 4's **C5**, merged as one row |
| F6 | roles/backend | High | `backend/internal/workforce/app/bootstrap_copy.go:289-301` (`leadershipModuleKeys`) | `/app/bootstrap` nav is a hardcoded role-name switch covering only `ceo_internal`/`pc_director`/`park_head`/`growth_director`. Both new director roles (and any future composite org role) fall into the leadership branch, get `len(keys)==0`, and see **zero nav** — contradicting the role-activation report's claim that "their module set comes from their granted permissions." | Yes, for the two new roles | open — audit 4's **C3**; same structural root as audit 1's finding on hardcoded per-role nav templates (§ dedup note below) |
| F7 | roles/backend | Critical | `backend/internal/permissions/permissions.go:~443` (`RoleHealthDirector`) | `CountsRead`/`CountsWrite` must NOT be granted to `health_director` — Counts is a deliberate OFF feature per `AGENTS.md`'s "Confirmed module ownership..." block; granting those permissions would light up the Counts nav. An agent granted them once this session by mistake; it was reverted. | No (correctness/compliance, not an E2E blocker) | **fixed-this-session** (reverted) — audit 4's **C4**; re-verify this stays reverted before any future seed/permission change touches `RoleHealthDirector` |
| F8 **[SC]** | alerts/backend | High | `backend/internal/permissions/permissions_orgrole.go:309-326`, `permissions.go:342-367` | `growth_director` does **not** hold `weighing.plan` — confirmed intentional in the current working tree (`permissions.go:367` comment: "NOT WeighingPlan: planning a weighing task is CEO-only", matching the maintainer's 2026-08-01 decision in `AGENTS.md`). Audit 1 originally flagged this as an accidental silent-void bug (`registerRole` collision); it is now a **documented maintainer decision**, not a defect. | No | **reclassified, not a bug** — do not re-open as F8 without a new maintainer decision |
| F9 **[SC]** | weighing/backend | High | `backend/internal/weighing/adapters/postgres/repository.go:1658-1671` (`mapShedUniqueViolation`) | Lump-sum resubmit after `ReopenScope` used to hard-500 on `weighing_shed_observations_one_active_scope_uidx`. Confirmed fixed in the working tree: the unique-violation path now calls `mapShedUniqueViolation(err, "", "")` and maps to a domain conflict instead of an opaque 500. | Yes (was: rework→resubmit is a core operator loop) | **fixed-this-session** |
| F10 **[SC]** | mobile | High | `apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/FeedCompletionLocalStore.kt:57`, `LogoutCoordinator.kt:51` | Prior operator's feed completions leaked across logout permanently within the process (a second operator on the same handset would see another person's feed rounds as already done). Confirmed fixed: `FeedCompletionLocalStore.clear()` now exists and `LogoutCoordinator` holds a reference to `FeedCompletionLocalStore` and calls it on logout. | Yes (all-roles E2E on one device is exactly the switch-user path that corrupts) | **fixed-this-session** |
| F11 **[SC]** | calendar/backend | High | `backend/internal/calendar/adapters/postgres/canonical_read.go:1041-1050` | `submitted_count` used to overlap `due_count`/`overdue_count`/`deferred_count`, so calendar chips double-counted and contradicted OpenAPI's disjointness contract. Confirmed fixed: `due_count`, `overdue_count`, and `deferred_count` all now carry `AND NOT m.submitted_for_verification` in their `FILTER` clauses. | Yes (every count assertion in an E2E run was otherwise untrustworthy) | **fixed-this-session** |
| F12 **[SC]** | weighing/backend | Critical → fixed | `backend/internal/weighing/adapters/verificationbridge/enqueue.go:25-45` | Weighing verification items used to be enqueued with no `ParkID`, so the consumer's blank-park no-op silently dropped every pending-proof push. Confirmed fixed: `ParkID: ptrIfSet(in.ParkID)` is now populated, with a comment explaining it is "the notification routing key." Cross-references the session ledger's "weighing park resolution hoisted before the observation write" note (`weighing/app/service.go:358-372`, a related but distinct fix). | Yes | **fixed-this-session** |
| F13 **[SC]** | alerts/backend | Critical → partially fixed | `backend/internal/notificationbridge/verification_notify_consumer.go:99-185` (`pendingModuleProfiles`, `pendingProfileFor`) | Weighing verification-**pending** proofs used to route through the vaccination consumer with vaccination copy to the vaccination verifier and PC Director (audit 1 finding). Confirmed fixed for the `pending` path only: `pendingModuleProfiles` now has a keyed `moduleWeighing` entry (`leadershipPosition: positionGrowthDirector`). The **approved/rework/closed** paths remain hardcoded to `positionPCDirector` — see F5, which is the surviving half of this exact defect. | Yes, partially (pending fixed; approved/rework/closed still broken) | **fixed-this-session (pending only)** — see F5 for the open remainder |
| F14 | alerts/roles | High | `backend/internal/calendar/adapters/postgres/repository.go:1519` (`escalationRoleRank`), `:1468-1478` (`escalationRole`) | `escalationRoleRank` omits both `feed_director` and `health_director`, so their granted `CalendarAction` permission is inert; `escalationRole` sends every L3 escalation to `pc_director` regardless of module. | Yes, for the two new roles | open — audit 4's **C6** |
| F15 | alerts/mobile | High | `apps/goatos-android/app/.../push/PushTargetResolver.kt:34-56` **[SC — confirmed, no weighing case, `else -> Routes.VACCINATION`]**; `AppNavHost.kt:574-601` (`calendarTargetRoute`) | No weighing branch exists in `resolvePushRoute`; every weighing push (and every unrecognized push shape) falls through to the Vaccination screen. `calendarTargetRoute` only allowlists `/weighing` and `/weighing/scan` hrefs; every other weighing target lands on a vaccination drive/record route. | Yes | open — confirmed unfixed at write time; audit 1's finding #0/#1 |
| F16 | alerts/mobile | High | `PushTargetResolver.kt:39` (early short-circuit) vs `verification_notify_consumer.go:608` (`/verification/items/<id>`) | The role-resolution branch for `screen == "verifier"` is dead for every real pending push: the early short-circuit at line 35 omits `screen == "verification"`, so `target != null` returns via `calendarTargetRoute` first, which doesn't know the `/verification/items/{id}` shape and falls to the drive fallback. Target must be resolved after role, not before. | Yes | open — audit 3's finding #9 |
| F17 | alerts/backend | High | `backend/internal/kernelstages/reminder_cadence.go:44,174` | Reminder ladder targets position codes (`operator`, `park_head`, `phc_manager`) that the real seeder never writes (`preventive_care_manager`, `vaccination_operator_*`); only `park_head` resolves, so the assigned vaccinator never gets T-7/08:00/13:00/due-today reminders individually — audience is park-position, never the drive's own assignment rows. | Yes | open — audit 3's finding #2; same root problem as F1/F4 (duty/position audience resolution) |
| F18 | alerts/backend | Critical | `backend/internal/calendar/app/missed_handler.go:60` | A missed obligation's terminal action is only `SweepEscalations(...)` — there is no `QueueRoleNotifications` on the missed path, so the one lifecycle state the obligation kernel exists to catch is the one state with no delivered message. | Yes | open — audit 3's finding #3 |
| F19 | alerts/backend | Critical | `backend/internal/calendar/adapters/postgres/repository.go:1387,1414-1417`; `canonical_read.go:123,235,444`; `notification/adapters/gateway/gateway.go:342-344` | Escalations are addressed to a role **name**, not a device: `PrimaryChannel` is hardcoded `'local-stub'`, the INSERT binds the literal role string (e.g. `"park_head"`) into `recipient_ref`, and the gateway posts that string to FCM as a registration token — a guaranteed-invalid-token 400 that isn't matched by `isInvalidFCMRecipientResponse`, so it burns all attempts silently. | Yes | open — audit 3's finding #4 |
| F20 | alerts/backend | High | `backend/internal/calendar/adapters/postgres/repository.go:1389` (INSERT omits `recipient_ref`) | Calendar reminders carry no `recipient_ref` at all; `setFCMTarget` falls through to a tenant-wide default topic (broadcasts to everyone) or, if unset, silently drops the reminder with no retry (`ErrChannelNotConfigured` at `service.go:134`). | Yes | open — audit 3's finding #5 |
| F21 | alerts/backend | Medium | `backend/internal/notification/adapters/gateway/gateway.go:378`; `notification/app/service.go:134` | A transient FCM auth failure (ADC/metadata-server hiccup) is wrapped as the same permanent `ErrChannelNotConfigured` sentinel that skips retry, so one Cloud Run token hiccup exhausts every claimed request in the tick permanently. | Yes | open — audit 3's finding #6 |
| F22 | alerts/backend | High | `notificationbridge/verification_notify_consumer.go:406,578,710` and the four non-`pending`/`rework` handlers | Four verification handlers (`handleVerdictApproved`, `handleItemClosed`, `handleVaccinationDriveReady`, `handleVaccinationDriveClosed`) queue to a possibly-empty audience and report success with no length check — silent non-delivery that reads healthy in metrics, unlike `handleItemPending`/`handleVerdictRework` which already warn on empty. | Yes | open — audit 3's finding #10 |
| F23 | alerts/mobile | Medium | `apps/goatos-android/app/.../viewmodel/AlertsViewModel.kt:47` | The in-app Alerts feed is vaccination Control Tower (`observeSummary()` → `ControlTowerResponseDto.alerts`), not notification history; no notification-history endpoint is read anywhere, so weighing/feed pushes exist only as transient tray entries with no recovery if swiped, and per-module scoping is unrepresentable at the data-model level. | No (UX gap, not a delivery blocker) | open — audit 3's finding #11 |
| F24 | design-system/ci | Critical (CI-blocking) | `tools/agent-hooks/design-system-baseline.txt:3-15` | Baseline keys still reference the pre-extraction path (`app/src/main/kotlin/sg/mesha/goatos/feature/weighing/…`); the weighing feature-module extraction moved those files to `feature/feature-weighing/src/main/kotlin/…`, so `make ci-local` fails for every lane in the repo until the 13 keys are rewritten. Session ledger's own §4 records the baseline currently holds 23 entries (not "~65" as previously claimed — that number is wrong and should not be reused). | Yes — blocks every push, not just weighing/alerts | open — audit 2's #1, the top of its ranked list |
| F25 | mobile | High | `apps/goatos-android/feature/feature-weighing/.../WeighingScreen.kt` (staged blob, was :492-497); `app/.../viewmodel/WeighingViewModel.kt:1666` | The Planner render path (`EmptyWorkCard`/`PlannerParkCard`) appears deleted in the weighing-extraction diff while `plannerParks` is still populated upstream — a code-deletion signature, not a cutover signature (a real cutover would also remove the state producer). Unattributed; Park Head/planner role has no screen to test until this is resolved either way. | Yes | open — audit 2's #2 |
| F26 | backend | Critical | `backend/internal/bootstrap/api.go:446` | `POST /feed-direction/complete` still writes an instant `completed` state, bypassing the feed-distribution verifier gate the maintainer decision (`docs/decisions/feed-distribution-verification.md`) requires — the route's own "left inert" comment is factually false; operators hold the permission needed to hit it. | Yes — testing a verifier gate with a live bypass route open is theatre | open — audit 2's #4, escalated from the audit's own "high" rating |
| F27 | backend | Critical | `backend/internal/domainconsumer/wiring/bus.go:52` | A fourth domain-bus builder (`BuildDomainBus`) omits all four verifier-verdict appliers (feed direction, counts approval, weighing, + one more); if any E2E environment is constructed via this builder, verifier verdicts are silently accepted by the UI and applied by nothing — no error, no log. | Yes, if any E2E env uses this builder | open — audit 2's #5, escalated per its own counter-review note |
| F28 | mobile | High | `apps/goatos-android/feature/feature-calendar/.../DriveCardMetrics.kt:43` vs `apps/admin-web/.../calendar-drive-card.tsx` | Android and web render different drive-completion numerators/percentages for the same drive — a cross-surface count-parity violation of the kind `AGENTS.md` explicitly bans. | Yes — cross-surface parity is the point of an all-roles run | open — audit 2's #8 |
| F29 | mobile | High | `apps/goatos-android/.../viewmodel/FeedTransportViewModel.kt:43` | Feed Transport's `LoadMore` loop is unbounded against a frozen 100-row Room window — spin/ANR risk during the transport screen. | Yes | open — audit 2's #9 |
| F30 | weighing/mobile | High | `weighing_lifecycle_notify_consumer.go:363` vs `:678-710` (`handleVerdictRework`) | Weighing rework is notified **twice, contradictorily** — two different event keys produce two rows/two pushes to one device; the generic one deep-links into the vaccination record, not the weighing one, because `legacyHandledVaccination()` evaluates false for weighing and nothing suppresses the duplicate. | No (UX/duplication, not silent loss) | open — audit 3's per-module matrix (§2 below) |
| F31 | weighing/mobile | Medium | `weighing_lifecycle_notify_consumer.go:384`; `gateway.go:266-278` | Weighing's `group_key`/`collapse_key` is tenant-wide (`"weighing:" + tenantID + ":verdict"`), and the Android gateway applies `group_key` as the notification `tag`, which **replaces** rather than stacks, with no summary notification built — a "proof needs redo" push is silently erased by any unrelated approval anywhere in the tenant. | No | open — audit 3's per-module matrix (§2 below) |
| F32 | counts/backend | High | `backend/internal/counts/adapters/postgres/approval_repository.go:298` | Shifting approval is pull-only — the only outbox emitter (`insertDeathApprovalOutbox`) is death-specific by name and guard; no `counts.approval.*` event type exists, so a Park Head is never told a shifting approval is waiting and the operator never hears the verdict. | No (Counts is an OFF feature per maintainer decision — tracked, not urgent) | open — audit 3's per-module matrix (§2 below) |
| F33 | design-system | Medium | `apps/goatos-android/core/core-designsystem/.../MeshaComponents.kt` | Design system is effectively bypassed app-wide: `MeshaPrimaryButton`/`MeshaStatusPill` each used in only 2 files; ~45 files carry raw `fontSize =` vs 15 referencing `MeshaType`; weighing is ~100% hardcoded English (87 raw literals vs 7 `stringResource()` calls, 9 `weighing_*` keys) because it is not its own feature module (no `res/` of its own). | No | open — audit 1's findings #16, #20, #22 (merged; same structural root: weighing was not a feature module before this session's extraction — see F24/F25 for the extraction's own residual issues) |
| F34 | i18n | High | `notificationbridge/verification_notify_consumer.go:438-480`; `feature-counts/.../values-hi/strings.xml:220-229` | All FCM titles/bodies are hardcoded English server-side with no locale field on the notification request, while `bootstrap_copy.go` already localizes nav in en/hi/kn/te; separately, Counts Shifting management-stage strings (including a correctness warning) are byte-identical English copied into `values-hi`, meaning a Kannada/Telugu/Hindi-reading operator cannot read a safety-relevant notice. | No (reach, not correctness, once F1-F22 are fixed) | open — audit 1's findings #17-#18; audit 3's cross-cutting note #5 |
| F35 | roles/mobile | High | `permissions/permissions_orgrole.go:309-326` init() override pattern; `GoatOsShell.kt:603`; `ShedsViewModel.kt:410` | Client-side role-name inference (`primaryRoleHint` string matching, including `endsWith("_director")`) decides drawer contents and hides RFID pairing from real operators; `growth_director` (the weighing director) is misclassified by one of the three instances. This is the same class of defect as F6 (hardcoded per-role templates on both sub-systems), just on the mobile client side. | No, but corrupts role-based UI for any new director role | open — audit 1's finding #13 |
| F36 | backend | High | `backend/internal/processintegrity/adapters/http/handler.go:196`; `vaccinationexecution/adapters/http/handler.go:1645-1662`; `vaccination/adapters/http/handler.go:402-416` | Four vaccination/process-integrity read endpoints resolve park scope from the query string instead of the caller's grants, so a park-bound Park Head who omits `park_id` (or explicitly requests the other park) sees the other park's data — feeds directly into an inflated mobile Alerts count. | No (data leak, not delivery failure) | open — audit 1's findings #8-#10 |

**Dedup notes**: F5/F13 are the two halves of one defect (pending routed correctly this session; approved/rework/closed still hardcoded) — kept as two rows because one is fixed and one is open, and collapsing them would misreport status. F6/F35 share one root (hardcoded per-role templates instead of grant-driven composition) but live in different files (backend bootstrap vs mobile client) and are both still open. F1/F4/F17 are three faces of the same "duty/position audience resolution" root cause (empty verify-duty table, unmapped video_verifier seat blocking the fix, and a reminder ladder keyed to position codes the seeder never writes) — kept separate because each has an independent, distinct fix location.

## 2. E2E readiness gate — ordered

This is the sequence that must land before an end-to-end run across all roles
and both modules is **meaningful** rather than passing while proving nothing.
Order is preserved from the audits' own stated rationale, not re-derived.

1. **F2 — Android `POST_NOTIFICATIONS` permission gate must land FIRST.**
   Audit 3's own words: "until this lands, every other notification fix tests
   as a false negative." No amount of correct backend routing matters if the
   OS silently drops the push before it reaches the tray.
2. **F24 — design-system baseline path rewrite.** Blocks `make ci-local` for
   every lane in the repo, not just weighing/alerts; nothing can be pushed
   until this is fixed, so it must land before any of the fixes below can even
   be committed.
3. **F3 — apply the `feed_director`/`health_director` role migration (C1).**
   Without it no principal can hold either role and the whole role-activation
   test is unfalsifiable.
4. **F4 — fix the `video_verifier` unmapped-seat abort (C2).** Otherwise
   `seed-position-duties -strict` aborts before F1's fix can run at all.
5. **F1 — run the verify-duty seeder and confirm `position_module_duties`
   actually goes non-zero for every module in
   `PendingNotificationDutyModules()`.** Do not report this closed until the
   count is re-checked after F3+F4 land — this is unproven, not proven, per
   the session ledger.
6. **Materialize `feed_director`/`health_director` grants in the seed** (audit
   4 §5 point 3): `seed-roster-real/main.go` currently hardcodes
   `primary_role_hint='pc_director'` with no position/grant for these roles;
   without this the leadership leg resolves to zero devices — the same
   pending-vs-materialized failure already recorded in memory
   (`goatos-stg-9person-login-grants`).
7. **F6 — fix `/app/bootstrap` nav for the two new director roles (C3).**
   Otherwise they see zero nav and cannot even reach their own module's
   screens to generate the proofs the E2E run is supposed to verify.
8. **F5 — route `handleVerdictApproved`/`handleItemClosed`/`handleVerdictRework`
   through `pendingModuleProfiles` (C5), not hardcoded `positionPCDirector`.**
   Otherwise every module except vaccination's pending event pages the wrong
   director in the wrong words for 3 of 4 lifecycle events.
9. **F14 — add both new roles to `escalationRoleRank`/`escalationRole` (C6).**
   Otherwise their granted `CalendarAction` permission is inert and L3
   escalations still misroute to `pc_director`.
10. **F15/F16 — fix weighing push routing and role-before-target resolution
    order.** Both are needed together: fixing the target allowlist alone still
    leaves the verifier role branch dead because of the ordering bug in F16.
11. **F17/F18/F19/F20/F21/F22 — the remaining alerts E2E blockers** (reminder
    ladder audience, missed-obligation silence, escalation-to-role-string,
    recipient-less reminder INSERT, transient-auth-error misclassification,
    empty-audience silent success). None of these individually gate the
    others, but all 6 must land before any count from the alerts side of the
    E2E run can be trusted.
12. **F25 — attribute/restore the deleted weighing Planner render path**
    before exercising the Park Head/planner role.
13. **A verifier seat must exist at every park under test**, not just one
    (audit 4 §5 point 4) — otherwise the closeout query passes while pushes at
    other parks still resolve to zero.
14. **FCM tokens must be registered for the seeded director and verifier**
    (audit 4 §5 point 6) — otherwise "routing broken" and "no device" are
    indistinguishable failure modes in the run's output.
15. **Assert on all four lifecycle events (pending, approved, rework, closed)
    and on wording** (audit 4 §5 point 7) — a feed push must never contain the
    word "vaccination"; today it will (F5).

Not required before the run, but will generate false "stale data"/spin bug
reports during it if skipped: F29 (`FeedTransportViewModel` LoadMore loop),
missing `RefreshOnResume` on Scan/Record screens (audit 2 #16), and Paparazzi
golden re-recording (audit 2 #10, sequence strictly after F25 is resolved).

## 3. Per-module notification matrix (from audit 3)

### Vaccination — the most built, still the most broken

| Event | Notifies? | Who actually receives |
|---|---|---|
| Reminder T-7 / 08:00 / 13:00 / due-today | Partial | `park_head` only; `operator`/`phc_manager` don't resolve (F17). Body is an anonymous roll-up, never addressed to the drive's actual assignee. |
| Post-due / overdue | Silent | Nobody — all three reminder rungs are non-positive offsets, all claimed by D+1; the `"overdue"` copy path is unreachable dead code. |
| Obligation missed | Silent | Nobody (F18). |
| Escalation L1/L2/L3 | Silent | Nobody — L1 is always the `'local-stub'` channel; the role-name-as-FCM-token bug means the FCM attempt itself is wasted (F19). |
| Proof pending → verifier | Yes | Tenant verifier — but the tap dead-ends (F16), and a person holding both verify duty and a leadership seat loses the leadership copy (one `eventKey` reused for both writes, `ON CONFLICT DO NOTHING`). |
| Verdict rework | Silent on the retry path | The durable retry bus and the live registration diverge (audit 2 finding, not separately re-listed above — see audit 2 raw text for detail). |
| Verdict approved / item closed | Yes, hardcoded | `pc_director`, vaccination copy, no empty-audience warning (F22). |
| Drive ready / drive closed | Yes | Leadership, no empty-audience warning (F22). |
| Leave / backup-manager cover | Silent | Nobody — `vaccination.leave.changed` has exactly one subscriber (a replanner), no notification to the backup manager, the displaced operator, or the park head. |

### Weighing — delivers, but doubled, mis-routed and un-tappable

| Event | Notifies? | Who |
|---|---|---|
| Plan published / shed submitted / reopened / campaign closed | Yes | `growth_director` + CEO, tenant scope only — **Park Head is absent from the entire weighing lifecycle**, unlike vaccination verification which includes `park_head`. |
| Delayed / running-late cadence | Yes | Correctly grouped `byOperator`, names each operator's own sheds — the best audience logic in the codebase; the model the vaccination reminder ladder should copy. |
| Verdict rework | Twice, contradictorily | Two different event keys → two rows → two pushes to one device; the generic one deep-links into the vaccination record (F30). |
| Any tap | Un-routable | Producers emit `campaign_id`/`campaign_shed_id`/`observation_id`/`shed_ids`/`business_date`, none of which are in `PushExtras.ROUTE_KEYS`; even after F16 lands, the specific bucket stays unaddressable. |
| Tray survival | No | Tenant-wide `group_key`/`collapse_key` means the Android `tag` gets replaced by any unrelated approval; no summary notification built (F31). |

### Feed — routed for pending, wrong for everything after

Feed gets a correct module-profiled **pending** push (part of F13's fix).
Approved, rework, and closed all page the PC Director in vaccination wording
(F5). No feed-specific lifecycle consumer exists, so there is no feed analogue
of weighing's delayed cadence — `feed_director` learns nothing about their own
module's verdicts.

### Counts — effectively silent

Shifting approval is pull-only (F32); death approvals emit, shifting does not.
The Park Head is never told an approval is waiting; the operator who did the
physical move never hears the verdict. Counts remains an OFF feature per
maintainer decision, so this is tracked but not currently urgent.

## 4. The 8 numbered corrections (C1-C8) — feed_director/health_director role activation

Verbatim in substance from audit 4 (task `wdkapwxor`), since a redo is queued
against this diff. Cross-reference F3, F4, F6, F7, F5, F14 above for the same
findings expressed as consolidated table rows.

- **C1** — Roles are not grantable; add a migration. `org_role_catalog` (via
  `000001_goatos_clean_slate_baseline.sql`) seeds `director_health`/
  `director_feed`, not `health_director`/`feed_director`; no migration for the
  new keys exists. Model the fix on `000057_growth_director_role.sql`: insert
  both role rows and extend `workforce_members_role_hint_check` in the same
  migration, or `service.go`'s `validRoleHint` will accept a value the DB
  rejects at commit.
- **C2** — `video_verifier` is unmapped in `modulePrefixes`, so
  `seed-position-duties -strict` aborts before any insert (both real
  invocations use `-strict`). Fix: skip the verifier seat before the
  `matchModule` branch in `deriveDuties` — do not add it to `modulePrefixes`,
  which would mint a bogus execute duty instead.
- **C3** — `/app/bootstrap` returns zero nav for both new roles;
  `bootstrap_copy.go`'s role-name switch covers only 4 named roles and both
  new roles fall into the leadership branch with `len(keys)==0`. The
  implementation report's claim that nav "comes from granted permissions" is
  false for this path. Fix: add explicit branches (`feed_director` →
  `"feed_direction"`, `health_director` → `"counts"`) plus a non-empty-nav
  test per director role.
- **C4** — Remove `CountsRead`/`CountsWrite` from `RoleHealthDirector`. This
  was not stray text — it restates the `AGENTS.md` maintainer lock verbatim
  (Counts is OFF; `health_director` gets ownership, not access). The agent
  should have stopped and surfaced the conflict, not silently reverted it
  without flagging. **Status: fixed this session** (see F7) — re-verify this
  stays reverted on any future touch to `RoleHealthDirector`.
- **C5** — Verdict/rework/closed pushes still hardcode `pc_director` for
  every module (see F5, confirmed still open). Fix: route all three lifecycle
  handlers through `pendingModuleProfiles` (proposed rename:
  `moduleProfiles`).
- **C6** — `escalationRoleRank` omits both new roles, so their granted
  `CalendarAction` is inert (see F14). Also module-route `escalationRole`,
  which currently sends every L3 escalation to `pc_director` regardless of
  module.
- **C7** — `roleLensForRole` (`backend/internal/adminui/app/compiler.go:1025`)
  labels `pc_director` as "Health Director"/"HD" — a user-facing conflation
  now that `health_director` is a distinct role. Fix the label and add both
  new roles to `highestRole` (`:1008`).
- **C8** — Fix the stale comment at `permissions.go:255`, which claims the
  `FeedDirectionRead` permission split narrows nothing; it demonstrably
  removes `pc_director`'s access to `/feed-direction/preview`. The code change
  is correct; only the comment is wrong.

**Handbook support for `health_director` owning Counts** (audit 4 §4, recorded
honestly, not restated as fact): full-text search of `Health_Director.pdf`,
`Feed_Director.pdf`, `Mesha-dept-directors.pdf`, and `COO.pdf` for
census/counting/headcount/roll-call/stock-take/reconcile returns **zero**
animal-census hits. What *is* documented: Health and Preventive Care are
separate departments, so `health_director ≠ pc_director` is genuinely
handbook-backed. Nearest written anchors for counts-adjacent responsibility
are Responsibility 6 (Tagging) and Responsibility 9 (death assessment) —
identity at the population-changing boundary, never enumeration. Shifting is
assigned to the Breeding Director by the handbooks; mortality is filed under
Growth in `COO.pdf`. This is an extension of the handbook, not a restatement,
and the runbook language must not imply otherwise. The shifting/mortality
ownership question must be answered before Counts is switched on.

## 5. What was rejected and why (do not re-raise)

**Audit 1 rejected 16 candidates** (verdict prose separately claims "21" —
recorded as a discrepancy between the audit's summary text and its structured
`rejected` list; the structured list of 16 is what actually exists and is what
this section describes):

- Blue (`MeshaColors.Info`) as non-semantic notice colour, `Color.White`/
  `Color.Black` literals outside the design system, five incompatible filter-chip
  geometries, ~35 bespoke card surfaces instead of `MeshaCard`, and two
  coexisting screen-header conventions — none independently verified; folded
  into the broader confirmed design-system-bypass finding (F33) rather than
  counted as separate defects.
- Alerts-as-unpaginated-blob and "Alerts has no weighing source at all" —
  plausible and adjacent to confirmed defects (F23), but `AlertsViewModel`/
  `ControlTowerCache` were not opened in this pass; do not treat as confirmed
  until independently read.
- WeighingScreen importing 11 unused vaccination-scan symbols; non-executor
  write actions on `/weighing`; `VerifyQueueViewModel`'s hardcoded
  module→proof-category map; weighing rework double-notify's second cause;
  generic verification-type-string collision risk; `RoleOperator` carrying
  `WeighingExecute` for every operator — all unopened/unverified in this pass,
  not confirmed defects.
- Park-scope-agnostic `AuthorizedParkIDs` and weighing enforcing no park scope
  — plausible and important, but `auth.go` was not traced in this pass.
- A grab-bag of untranslated auth/profile strings, push-channel-description
  jargon, scan-tile widths, loose "Retry"/"Filter" copy — low-severity,
  unverified, and subsumed by the i18n/design-system findings already
  confirmed (F33/F34).

**Audit 3 rejected 22 of 48 candidates** (48 total − 26 confirmed = 22;
authoritative per the structured result, matching the task description):

- "Sixteen merges and known-item dedups" — correctly folded into confirmed
  findings rather than double-counted (e.g. the reminder-ladder audience issue
  and blocker #2 are one root cause, not two).
- Rejecting the counts-nav-access item on the settled ownership≠access
  decision (matches F7/C4 above) — correctly rejected as already-decided, not
  an open finding.
- Rejecting duplicate restatements of the same lens (e.g. restating the
  weighing tap-routing gap once per producer) — correct; these were lens
  artifacts, not distinct defects.
- Two findings the reviewer explicitly flagged as **rejected too eagerly**
  (do not treat as settled): "Leadership recipients depend on a `user_id` link
  the seeder never writes" was rejected on the strength of the grant CTE for
  the code half only — the seed half was conceded as "not verifiable without
  running a seed," which is unproven, not disproven. Add a seed-closeout
  assertion (ties into F1's fix) before treating this as closed.

## 6. What could not be verified this session

- The exact wording/severity calibration disagreements noted by audits 2-4's
  self-critique sections (e.g. "too lenient"/"too harsh" notes) were not
  independently re-adjudicated here; they are recorded as each audit's own
  stated position, not re-derived.
- Audit 2's items 12-30 (module-boundary lint, migration/schema hygiene,
  reaper error logging, roster pagination, etc.) were not individually
  spot-checked in this consolidation pass — they are lower-severity and
  non-E2E-blocking per audit 2's own ranking, and are omitted from the
  headline table above to keep it focused on E2E-relevant and highest-blast-
  radius rows; the full list remains in the audit 2 source text if needed.
- Whether `assertVerifyDutyCoverage`'s scope/temporal predicates (audit 4 §2:
  missing `scope_type='center'`, `scope_id`, `valid_from`/`valid_to` filters)
  have been added — not checked in this pass; flagged as a known gap in
  audit 4's own analysis, separate from C1-C8.

## 7. Cross-reference

- Session ledger (maintainer decisions, environment facts, two-layer alert-test
  doctrine): `context/repo-audits/weighing-alerts-session-ledger.md`
- Maintainer decision text (module ownership, weighing planning authority):
  `AGENTS.md` → "Confirmed module ownership and weighing planning authority
  (maintainer decision 2026-08-01)"
- Consolidated ledger closure process (for prioritization/format precedent):
  `context/repo-audits/last-35-commits-consolidated-bug-ledger.md`,
  `context/repo-audits/consolidated-ledger-defect-closure-program.md`
