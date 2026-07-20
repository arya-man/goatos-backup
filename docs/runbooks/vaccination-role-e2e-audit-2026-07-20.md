# Vaccination role E2E closure audit — 2026-07-20

Status: **locally verified with explicit external-delivery and verifier-context limitations**
Environment: isolated local backend, throwaway PostgreSQL, physical Infinix, and independent Android emulator
Source baseline at emulator snapshot: `f9e76e6d6f54dae4713bc7a48d82476509d4334b`
Final latest-main rerun baseline: candidate rebased onto `origin/main` `8699e0ea7bea3565f1001b63fc2909431ca6d7f8`

## Decision

The vaccination transaction closed successfully in the isolated physical-device lane: one operator submission containing four goat proof items was created exactly once, all four items were approved, the park-head closure completed, inventory posted four consumption movements, and the originating SOP task, submission, and all four submission items rolled up to `accepted`.

This is not a certification of real Firebase delivery. The committed dev Firebase file contains placeholder project/sender/app identifiers. Role-wise notification routing was therefore verified with synthetic local device tokens and the local event bus only; no external notification dispatcher ran.

The closure also has one material product-context gap: the verifier UI does not expose route/site/adverse-reaction context even though that context exists in the Goat OS submission contract. The successful approval result must not be read as proof that a verifier could inspect those fields on-screen.

## Certification boundary

| Surface | Result | Boundary |
|---|---|---|
| Physical Android + local backend + throwaway DB | **VERIFIED** | Operator scan, proof, submit, verifier approval, park-head close, inventory, SOP rollup, and role landing screens were exercised locally. The final latest-main APK also repeated the affected HID scan, camera, proof-sync, and process-death restore path. |
| Android emulator + isolated backend/DB | **VERIFIED (operator path)** | Permission gate, HID scan, process restoration, camera exclusivity, real MP4 upload, task-scoped deep link, FEFO lot selection, and system insets passed. Emulator intentionally stopped before Submit. |
| Synthetic notification routing | **VERIFIED** | Exactly 4 `verification_pending`, 4 `verification_approved`, and 4 `verification_closed` requests queued to the expected synthetic role tokens. All 12 source events republished; zero failed/dead-letter. |
| Real FCM delivery and notification tap | **NOT CERTIFIED** | Dev APK uses placeholder Firebase configuration. No real device token or external sender was used. |
| Verifier clinical context | **KNOWN GAP** | Current verifier screen does not expose route/site/adverse-reaction context. |
| Staging/production | **NOT RUN** | No staging, production, Firebase, or other cloud data was mutated. |

## Physical role matrix

| Role | Result | What was proved | Limitation |
|---|---|---|---|
| Vaccination operator | **VERIFIED** | Two sheds, four goats, four camera proofs, FEFO batch selection, one submission, duplicate request rejected with HTTP 409 while submission count remained 1. | Real FCM reminder delivery was not exercised. |
| Preventive-care verifier | **VERIFIED — physical role + backend workflow** | All four pending items received approved verdicts and drove the expected notification and SOP events; the final physical queue is clear. | Route/site/adverse context is absent from the verifier detail UI. |
| Park head | **VERIFIED — physical role + backend workflow** | Closed the approved submission; inventory and closure events completed; the final physical overview has no remaining closure work. | The post-close screen proves the queue is empty; the completed action itself is certified by the durable close/audit rows. |
| Preventive-care director | **VERIFIED — role landing** | Authorized director landing screen rendered on the physical device. | Per-proof pushes are intentionally excluded for this role; planned digest delivery is not implemented/certified. |
| CEO/CXO | **VERIFIED — role landing** | Authorized leadership overview rendered on the physical device. | Per-proof pushes are intentionally excluded; this does not certify a role-specific notification delivery. |

Physical role evidence:

- Operator final system insets and human stage label: [01-operator/27-system-insets-and-human-stage-final.png](evidence/vaccination-role-e2e-2026-07-20/01-operator/27-system-insets-and-human-stage-final.png)
- Operator proof sync: [01-operator/13-two-proofs-synced.png](evidence/vaccination-role-e2e-2026-07-20/01-operator/13-two-proofs-synced.png)
- Latest-main compact proof sheet: [01-operator/28-latest-main-bottom-sheet-proof-action.png](evidence/vaccination-role-e2e-2026-07-20/01-operator/28-latest-main-bottom-sheet-proof-action.png)
- Latest-main exclusive camera and recording: [01-operator/29-latest-main-camera-exclusive.png](evidence/vaccination-role-e2e-2026-07-20/01-operator/29-latest-main-camera-exclusive.png), [01-operator/30-latest-main-camera-recording.png](evidence/vaccination-role-e2e-2026-07-20/01-operator/30-latest-main-camera-recording.png)
- Latest-main proof sync and process restore: [01-operator/31-latest-main-scan-proof-synced.png](evidence/vaccination-role-e2e-2026-07-20/01-operator/31-latest-main-scan-proof-synced.png), [01-operator/32-latest-main-proof-restored-after-process-death.png](evidence/vaccination-role-e2e-2026-07-20/01-operator/32-latest-main-proof-restored-after-process-death.png)
- Verifier final queue: [02-verifier/01-role-queue-after-approval.png](evidence/vaccination-role-e2e-2026-07-20/02-verifier/01-role-queue-after-approval.png)
- Park-head final overview: [03-park-head/01-role-overview-after-close.png](evidence/vaccination-role-e2e-2026-07-20/03-park-head/01-role-overview-after-close.png)
- PC director landing: [04-pc-director/01-role-home.png](evidence/vaccination-role-e2e-2026-07-20/04-pc-director/01-role-home.png)
- CEO/CXO landing: [04-ceo-cxo/03-ceo-home-or-gate.png](evidence/vaccination-role-e2e-2026-07-20/04-ceo-cxo/03-ceo-home-or-gate.png)

## Physical throwaway transaction

| Item | Value / result |
|---|---|
| PostgreSQL container | `goatos-vax-role-e2e-db-20260720` on local port `60432` |
| Task | `4e345fc2-5466-49c4-b6a6-fcdff5eef97f` |
| Submission | `30838d22-c51e-4b65-abbe-eaa7d79c3302` |
| Inventory batch | `6eecc5b8-43cc-4b6c-b12a-4031568c8786` |
| Park | `00000000-0000-4000-8000-000000003001` |
| Shed | `f89ab2f7-ac06-4ac1-bbe2-7b3955ff6d89` |
| Stock lot | `00000000-0000-4000-8000-00000000b002` |
| Submission cardinality | 1 submission, 4 goat items, 4 proof clips |
| Idempotency | Duplicate submit returned HTTP 409; submission count stayed 1 |
| Verification | 4/4 approved |
| Closure | Park head closed the complete submission |
| Inventory | 4 consume movements; `996` stock remaining; `0` reserved |
| Vaccination completion | 4 accepted completions; 4 obligations completed |
| SOP rollup | Task `accepted`; submission `accepted` with `accepted_at`; 4/4 items `accepted` |
| Event delivery | All 12 verification events republished; 0 failed/dead-letter |

All IDs are isolated test data. They do not identify staging or production records.

## Notification routing

The local event-bus replay used synthetic device registrations and deliberately did not start an external FCM dispatcher.

| Event | Recipient contract | Query result |
|---|---|---|
| `verification.item.pending` | Center-scoped holder of `pc.vaccination/verify` | **4 queued** to the synthetic verifier token |
| `verification.verdict.approved` | Park head | **4 queued** to the synthetic park-head token |
| `verification.item.closed` | Originating operator | **4 queued** to the synthetic operator token |
| Rework | Originating operator + park head | Contract/integration tested; not emitted by the successful approval path |
| PC director / CEO | Excluded from per-proof pushes | Expected exclusion; planned aggregate digest is not implemented |

The replay also exposed and fixed a user-ID/member-ID recipient-resolution defect. Focused workforce, bridge integration, and notification consumer tests passed after the resolver was corrected.

## Independent emulator lane

Emulator: `emulator-5570`, Android 16/API 36, 1080×2400. Backend and database were isolated at `127.0.0.1:8181` and container `goatos-vax-emulator-e2e-db-YfV5RW` on local port `61432`. The physical Infinix was never addressed by this lane.

Verified in the emulator:

- Dev login and two-shed operator bootstrap.
- Permission denial blocked entry; granting required API-36 permissions allowed entry.
- RFID/HID text plus Enter remained on the scan route after scanner dispatch was moved ahead of focused Compose controls.
- Scan and proof state survived force-stop/relaunch.
- Camera was an exclusive full-screen route and released its client after cancel/stop.
- A real virtual-camera MP4 (`723,879` bytes) uploaded and completed server-side.
- The exact `shed_id` + `task_id` intent opened the correct restored execution.
- The FEFO endpoint returned `ETTT-LOT-001`, 998 doses, rank 1; the lot was shown and selected.
- Safe areas kept the header below the status bar and the Finalize action above gesture navigation.
- Android automated checks: 36 XML suites, 161 tests, zero failures/errors/skips; push resolver 6/6.

The emulator did not submit the record and did not run verifier or leadership closure because the physical lane owned the single closure chain. See the full local artifact: `/Users/ravi/.codex/results/goatos-vaccination-emulator-e2e-20260720-goatos-emulator-e2e.YfV5RW/EMULATOR-E2E-REPORT.md`.

## Defects found and disposition

| Defect | Disposition | Verification |
|---|---|---|
| Camera rendered above the tag/BLE screen instead of as an exclusive route | **FIXED** | Physical and emulator camera lifecycle observations; no active camera client after stop/cancel. |
| Scanner Enter activated the focused Compose Back control | **FIXED** | HID dispatch now reaches the scanner first; dedicated ordering/fallthrough test and emulator scan passed. |
| Required vaccine-lot picker was empty | **FIXED** | Integrated backend returned and Android selected FEFO lot `ETTT-LOT-001`. |
| Scan screen had duplicated top spacing | **FIXED** | Before/after evidence `23-scan-double-inset-before.png` and `24-scan-single-inset-after.png`; final inset evidence in `27-system-insets-and-human-stage-final.png`. |
| Shed record displayed a park UUID and vague/blank cohort/date | **FIXED for current projection** | Human shed presentation, `Due date`, and backend-owned animal-stage label now render; legacy `K2` resolves to `Milk drinking`. |
| `cohort` UI label actually carried animal stage | **FIXED** | Contract/UI renamed to animal stage; blank stage is hidden rather than showing unrelated drive text. |
| Shed summary said `4 / 4 done` beside 50% | **FIXED** | Denominator now includes done + open; evidence shows `4 / 8 done` with 50%. |
| Compact proof-action row was misaligned on phone widths | **FIXED** | Compact breakpoint uses the Android compact width class and stacks the full-width action; focused screenshot tests pass. |
| `obligation.in_progress` was absent from the canonical event-envelope enum | **FIXED** | Contract regression test and focused outbox/vaccination/obligation tests passed; failed local events replayed. |
| Notification bridge confused user IDs and workforce-member IDs | **FIXED** | Synthetic 4/4/4 queue split and focused bridge/workforce tests passed. |
| Verification closure did not atomically roll SOP items → submission → task | **FIXED** | Throwaway DB shows task/submission and all 4 items accepted after the real event replay. |
| SOP scan timestamps lost Android epoch-millisecond precision | **FIXED** | Scan capture, attempt, and list projections now use `RFC3339Nano`; the full Postgres SOP adapter suite passed with `GOATOS_RUN_POSTGRES_TESTS=1`. |
| Task cleanup paged the global recoverable-upload queue before filtering by task, leaking terminal local clips | **FIXED** | Cleanup now uses a task-scoped, all-status keyset walk. The failing-before regression covers 24+ target rows over multiple pages with interleaved other-task PENDING/SYNCED/FAILED rows; all 17 capture repository tests pass. |
| Vaccination execution aggregates joined 0:N rejected/reworked completion history directly, allowing one obligation to fan out into multiple counted rows | **FIXED** | Completion history is reduced to one as-of-effective row per obligation before the execution join. The Postgres regression seeds rejected-then-accepted history and proves one obligation, accepted effective status, zero stale rejection, correct last dose, and unchanged batch totals. `aggregate-projection-guard` records the obligation-grain/cardinality contract. |
| Restored scan ring can show completed progress while activity feed says `No taps yet` | **OPEN FOLLOW-UP** | Reproduced on emulator and again as physical `2/2` after process death; persistence-to-feed projection needs correction. |
| Local scan/attempt entities can remain `PENDING` after outbox success | **OPEN FOLLOW-UP** | Observed in emulator DB despite corresponding server rows; reconciliation status needs review. |
| Verifier cannot see route/site/adverse-reaction context | **OPEN PRODUCT GAP** | Backend submission context exists, but it is not rendered in the current verifier detail UI. |

## Field provenance: legacy Slack vs Goat OS

A read-only inspection of the actual `slack-automation-scripts` repository found no vaccination form schema, card schema, or source for the labels `cohort`, route/site, administered date, or adverse-reaction notes. The 17-page Slack Modules Training PDF likewise contains no vaccination form and no `vaccin`, route, administered, or adverse terminology. The only vaccination-specific helper found in the repository was `fixVaccinationAccess()`, which grants access to two opaque list IDs.

`administered_at`, `adverse_reaction`, and `reaction_notes` originate in the local source-material vaccination template referenced by `context/source-findings/preventive-care-vaccination-roster-stage-proposal.md`. They were not copied from a legacy Slack vaccination form. `route_site` is absent from Slack and from the approved vaccination rules; it came from a Goat OS draft SOP skeleton/general model. Its meaning is administration route/body site, but its requirement is a **source-provenance gap** that product must approve or remove. The incorrect visible `cohort` label was a Goat OS semantic mismatch and was replaced with the actual animal-stage meaning.

## Origin/main change handling

An origin watcher remained active during closure. The candidate was rebased onto `origin/main` `8699e0ea`, which includes the streamed scan-roster paging and proof repository changes. No later main commit existed at the final device rerun.

Only the affected paths were repeated:

- `ExecutionRepositoryPaginationTest` passed for the multi-page streamed roster.
- `CaptureRepositoryTest` passed all 17 tests, including the new task-scoped all-status cleanup regression; it was also run together with the pagination suite after an existing Room-invalidation sampling race was made deterministic.
- `make mobile-guard` passed.
- `backend/tests/integration/validate-postgres-migrations.sh` passed fresh-baseline and old-baseline upgrade convergence.
- A newly queued throwaway task, `24cc8a96-8a3e-4262-9216-9797c9fa8538`, exercised two physical HID scans, exclusive camera capture, one completed goat proof, and force-stop/relaunch restoration on the rebuilt APK. Backend proof `e818d978-e3be-4e37-916b-f9e062d276ea` is `completed` for the exact task/goat with an 11,483 ms video.

Unrelated verifier, park-head, director, and CEO lanes were not repeated because these main changes did not affect their contracts. The final landing command must still refetch main and rerun the repository gate on the exact pushed candidate.

## Evidence index

Physical evidence root: `docs/runbooks/evidence/vaccination-role-e2e-2026-07-20/`

- `01-operator/02-permission-gate.png` — permission gate.
- `01-operator/07-two-rfid-scans.png` — two scans.
- `01-operator/09-camera-fullscreen-before-record.png` and `10-camera-recording.png` — exclusive camera route and recording.
- `01-operator/13-two-proofs-synced.png` and `22-second-shed-proofs-synced.png` — proof upload/sync.
- `01-operator/14-finalize-enabled.png` and `15-after-finalize.png` — finalize and generated record.
- `01-operator/17-park-lot-picker-fixed.png` — populated vaccine-lot picker.
- `01-operator/23-scan-double-inset-before.png` and `24-scan-single-inset-after.png` — safe-area correction.
- `01-operator/25-vaccination-drives-semantic-copy-fixed.png` — shed summary copy and progress correction.
- `01-operator/27-system-insets-and-human-stage-final.png` — final physical UI with system insets and human animal stage.
- `01-operator/28-latest-main-bottom-sheet-proof-action.png` — compact phone proof action is full-width and aligned.
- `01-operator/29-latest-main-camera-exclusive.png` and `30-latest-main-camera-recording.png` — latest-main exclusive camera lifecycle.
- `01-operator/31-latest-main-scan-proof-synced.png` and `32-latest-main-proof-restored-after-process-death.png` — latest-main proof sync and process restoration.
- `02-verifier/01-role-queue-after-approval.png` — physical verifier role with the completed queue clear.
- `03-park-head/01-role-overview-after-close.png` — physical park-head role with no remaining closure work.
- `04-pc-director/01-role-home.png` — PC director physical role landing.
- `04-ceo-cxo/03-ceo-home-or-gate.png` — CEO/CXO physical role landing.

Independent emulator evidence root: `/Users/ravi/.codex/results/goatos-vaccination-emulator-e2e-20260720-goatos-emulator-e2e.YfV5RW/`

- `screenshots/15-camera-exclusive-overlay.png` and `17-camera-recording.png`.
- `screenshots/20-proof-restored-after-process-death.png`.
- `screenshots/22-hid-enter-fixed-scan-stays.png` and `23-hid-scan-restored-after-process-death.png`.
- `screenshots/27-integrated-task-scoped-scan.png` and `28-integrated-restored-done-proof.png`.
- `screenshots/31-integrated-fefo-picker-populated.png` and `32-integrated-fefo-picker-selected.png`.
- `api/integrated-task-option-values.json`, `db/`, `logs/`, and matching `ui-dumps/`.

## Remaining release work

1. Add valid non-production Firebase configuration and certify real role-wise FCM delivery and notification-tap routing.
2. Obtain product approval or removal of draft-derived `route_site`; expose the approved administration/adverse context to the verifier and run a physical verifier UI decision check.
3. Fix restored-scan activity-feed hydration and reconcile local entity status after successful outbox delivery.
4. For any commit that lands after the recorded `8699e0ea` baseline, rerun only the impacted E2E slice before release certification.
