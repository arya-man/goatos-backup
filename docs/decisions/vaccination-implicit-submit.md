# Vaccination Implicit Submit Contract

Status: implemented and verified on POCO against the local throwaway DB before landing.

## Execution Unit

For vaccination operations, the execution unit is the actual shed work unit shown to the operator, for example `Gandhi 1 - Part 1`.

`Gandhi 1` alone is only grouping/display context when partitions exist. It must not be used as the completion unit for a partitioned operation.

## Operator Flow

For per-goat vaccination proof mode:

1. The operator opens the scan screen for one execution unit.
2. The operator scans each valid animal in that execution unit.
3. The operator records one proof video per animal.
4. When the final valid scan and proof reach backend and backend readiness is true, the shed execution unit is submitted automatically.
5. The scan screen closes after backend accepts the submit.

There is no required `Finalize shed` button and no separate submit screen for this flow.

The scan screen stays open only when the operator still has work or recovery to do:

- expected animals are still unscanned,
- proof upload is pending or failed,
- backend readiness is blocked,
- the device is offline or cannot reach backend,
- backend rejects the submit with a real reason.

## Backend Ownership

Backend owns the submit decision and validation. Android may enqueue the implicit submit only after backend readiness says the exact execution unit is ready.

For an implicit per-goat vaccination submit, Android sends an empty proof/answer payload. Backend reconstructs the submission from server facts:

- `sop_task_scan_captures` for the task and execution unit,
- completed `proof_artifacts` for the same task and animals,
- vaccination assignment membership for the same execution unit.

One animal proof may satisfy one, two, or three vaccine obligations for that animal, but only obligations assigned to that same execution unit may be closed or sent to verification.

Unknown tags and neighboring/wrong execution-unit animals do not count toward readiness and do not create completion for this execution unit.

## Progress Log

- Final root cause from POCO/local-backend E2E: proof video upload succeeded, but scan capture and submit authorization compared `sop_tasks.assigned_to` directly with the JWT actor id. Vaccination tasks are assigned to `workforce_members.workforce_member_id`; the mobile JWT actor is the linked `workforce_members.user_id`. That made backend reject real Sagar scan captures as `task_not_assigned`, so videos existed but no submission or verification rows could be created.
- Backend fix: task assignment checks now accept either direct actor equality or an active `workforce_members` link between the assigned workforce member id and the actor user id. The same resolver is used by scan capture, scan attempt, and submit.
- Android outbox fix: terminal failed `SCAN_ATTEMPT` audit rows no longer hold back real `SCAN_CAPTURE` or `SHED_SUBMIT` rows. Terminal failed scan captures still block submit because a shed must not close with missing scan evidence.
- Android RFID fix: toggling keyboard-wedge capture clears stale buffered characters, so a partial/previous tag cannot contaminate the next debug or physical scan.
- Final POCO proof run: Sagar opened `Gandhi 1 - Part 1`, scanned `G1-901007000504332` and `G1-901007000504418`, recorded both proofs, and the scan screen auto-closed to the shed list after backend submit success.
- Final DB proof for that exact partition: 4 `sop_task_scan_captures`, 2 completed `proof_artifacts`, 1 `sop_submissions`, 2 `sop_submission_items` in `needs_review`, 1 `sop_task_submission_fanouts`, and 4 `vaccination_completions` (`PPR_QA` + `FMD_QA` for each animal).
- Final screenshot evidence: `/tmp/goatos-phone-qa-implicit-submit/after-auto-close-fixed.png`.
- Final focused gates passed: `go test ./internal/sop/app`, `go test ./internal/sop/adapters/postgres ./internal/sop/app`, Android core outbox unit tests, and focused Android scan/submit/debug-aliaser unit tests.
- Status projection fix: the operator shed list must request and cache `cardSummaries`; rows-only fallback can make submitted exact partitions render as `In progress` even though backend already says `status=completed`.
- Completed-card behavior: once a partition is submitted, backend returns `primaryActionKey=none`; the operator card remains visible as done but does not reopen the scan/submit path.
- All-shed POCO E2E after status fix: completed five exact partition sheds covering one-, two-, and three-vaccine proof fanout. Final local DB totals were 27 scan captures, 12 proof artifacts, 5 submissions, 12 submission items, 5 submission fanouts, and 27 vaccination completions. Completion split was `ET_TT_QA=2`, `PPR_QA=10`, `FMD_QA=10`, `HS_QA=5`; all 12 submission items were `needs_review`.
- All-shed screenshot evidence: `/tmp/goatos-phone-qa-implicit-submit/all-sheds-complete-list.png`.

- Local phone QA bootstrap rule: do not invent `GOATOS_LOCAL_USER_ID`. The local auth identity is `workforce_members.user_id`, not `people.person_id` and this schema has no `people` table. Use the Android dev runner default field-operator identity unless the seed explicitly prints another valid operator. A bad user id makes `/app/bootstrap` return `403` and wastes time as a fake "offline/queued" phone symptom.
- Local phone QA seed rule: the seed must assign `vaccination_drive_assignments.operator_id` and `sop_tasks.assigned_to` to the same active operator workforce member represented by the installed phone token. For Sagar local QA, install with `GOATOS_LOCAL_USER_ID=90000000-0000-4000-8000-000000000204`; the seed resolves that to `workforce_members.workforce_member_id`. The seed must fail if any QA assignment lands on `partition_label = 'whole'` because this test is specifically for partitioned sheds.
- Local phone QA permission rule: after `pm clear`, grant all operator runtime permissions before judging UI behavior: camera, microphone, fine location, coarse location, notification, `BLUETOOTH_CONNECT`, and `BLUETOOTH_SCAN`. Missing `BLUETOOTH_SCAN` leaves the app's permission gate visible even when the API/auth/seed are correct.
- Added 5-shed disposable phone-QA seed: one, two, and three vaccine obligations per animal, all with partitions.
- Seed now clears its own scan captures, proofs, submissions, submission items, fanouts, and completions before each run.
- Fixed backend proof recovery SQL syntax that caused `POST /app/tasks/{id}/submissions` to fail with SQLSTATE `42601`.
- Fixed Android auto-submit to use the same stable shed-submit idempotency key as manual submit.
- POCO run after those fixes proved backend accepted the auto-submit: one `sop_submissions` row, two `sop_submission_items`, four Gandhi 1 - Part 1 vaccination completions, and zero other-shed completions.
- Android per-goat vaccination scan now hides the finalize CTA and closes the scan screen only after backend submit success.
- Backend now rejects implicit vaccination submit without exact shed scope, so a shared parent task cannot accidentally mean "all sheds".
- Backend proof readiness/recovery now requires the completed proof to be newer than the latest accepted scan for that goat in that exact shed/partition, preventing stale rejected proof reuse.
- Phone QA found a debug-fixture transport artifact: injected physical sample card tags could arrive as `g1tempcptcastro1001`, bypassing the sample-card aliaser and showing as an unknown tag. The debug-only aliaser now accepts sample tags by suffix so the POCO E2E exercises the seeded roster/partition path instead of a fake unknown-tag path.
- Focused Android unit tests passed for the debug sample-card aliaser, scan auto-submit behavior, and submit partition/key behavior.
- The disposable seed is now idempotent against the real `protocol_rules_version_dose_unique` key, so repeated phone E2E runs do not fail when a previous QA seed already inserted `PPR_QA`.
- Judge review tightened the backend contract: readiness and fanout now require `vaccination_drive_assignment_members` for the same assignment shed and normalized partition; the old no-assignment fallback is gone for batch-driven vaccination submissions. Implicit per-goat proof recovery/readiness now requires completed `video` proof artifacts, not any proof type.
- Backend focused tests passed after the stricter assignment-membership and video-proof changes.
- Judge review tightened Android freshness/visibility: auto-submit can only use a shed summary after the server refresh succeeds, not when refresh starts; hidden-CTA per-goat screens now show queued/submitting/retrying/rejected submit state on the scan screen itself.
- Focused Android tests passed again after those judge-driven changes.
- Local phone QA park-scope rule: the installed operator token must have exactly the disposable fixture park active for the operator face. If the same user has another active operator park grant, `/app/vaccination/execution` can ask for park selection and the phone can show stale/empty sheds while the direct fixture query looks correct.
- Phone QA proof before scan: the first hard proof screenshot must be the shed list showing five exact partition cards and the 1/2/3 vaccine mix. Do not continue E2E from an empty list, a `whole` partition, a wrong operator, or a multi-park bootstrap.
- Phone QA navigation note: the bottom `Stock` tab is app chrome from the operator bootstrap, not a seeded vaccination shed/card. It must not be used as evidence that the vaccination fixture changed.
- Backend hardening after judge review: every goat-level vaccination submit now requires exact shed scope, even if an older/manual client sends proof refs or answers. No exact shed scope means `missing_shed_scope`, not a broad parent-task completion.
- Android hardening after judge review: only per-goat-video vaccination suppresses the submit/footer path. Shed-level proof flows keep the old Submit screen route.
- Backend projection rule: operator execution cards must expose vaccine chips from server vaccine codes (`protocol_rule_dimensions.vaccine_code`) at assignment-member obligation grain. Do not infer the visible vaccine mix from a representative dose row; the five-shed phone fixture must show ET+TT, PPR+FMD, and PPR+FMD+HS before scan E2E starts.
- Seed idempotency rule: after upserting protocol rules, the fixture must resolve shed `rule_ids` from the actual persisted `protocol_rules.dose_code` rows. Hardcoded rule UUIDs can drift on repeated local runs and silently drop one vaccine, which creates a fake product bug.
- Seed dimension rule: disposable QA vaccine dimensions are deleted and recreated by QA dose code each run. Duplicate `protocol_rule_dimensions` rows inflate/mask vaccine breakdowns and make API/card proof look wrong even when obligations exist.
