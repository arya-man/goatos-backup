# PR 206 Proof Media Progress

Last updated: 2026-09-07 04:58 IST.

## Goal

Finish PR 206 end to end: proof caps, common proof preview/retry UX, analytics traceability, backend payload correctness, OCI throwaway DB E2E screenshots, rebase/push, local CI landing receipt, merge to main, staging and mobile deploy, and Firebase Remote Config force-update verification.

## Hard Constraints

- Android app: `/Users/raviteja/mesha/goatos-proof-cap-fix/apps/goatos-android`.
- Allowed phone/profile only: serial `143382555G111292`, Android user `10`, package `sg.mesha.goatos.dev`.
- Do not switch to any other connected device/profile.
- E2E DB: OCI throwaway DSN from `/tmp/goatos-pr206-oci-db.dsn`; do not print secrets.
- For goatos-stg requests, use GCP Cloud SQL only, but this E2E goal is OCI throwaway until deployment validation.
- No client-wide proof caps. Any cap must be explicit per feature/field and tested.
- No raw backend tokens in Android/frontend headers, tabs, nav titles, or icons. Explicit mapping or fail-fast guard only.

## Completed Local Fixes

- Android PC Care task/removal proof analytics now carry local proof row id, proof upload outbox id, server proof id, submit outbox id, feature surface, category, capture mode, source/outcome/reason where available.
- PC Care removal reconcile re-registration now emits registration analytics with proof row/outbox/server ids.
- Weighing fasting analytics now carry task/campaign/shed/scope, field/action/source/outcome/reason, proof row/outbox/server ids, submit outbox id, and feed/water proof outbox ids.
- Backend PC Care removal rework payload cleanup:
  - completed sibling pens are not reset;
  - completed sibling pens are not included in `removal_pens`;
  - completed sibling proof refs do not leak into top-level `media_refs`.
- Counts pen reconciliation now traces capture/re-record attempt, cancel/failure/success, preview action, submit enqueue, submit sync success/failure with card/task id, shed id, goat id/RFID, field/action/source/outcome/reason, local proof row id, proof upload outbox id, submit outbox id, and server proof id when available.
- Vaccination now traces preview action, capture attempt/cancel/failure/success, retry attempt/success/failure, and auto-submit enqueue/sync success/failure with task/campaign, shed, partition, goat/tag, field/action/source/outcome/reason, proof row/outbox/server ids, and submit outbox id. Vaccination proof capture waits for upload enqueue so the success event can include the proof-upload outbox id.
- Weighing individual and shed/lumpsum now trace submit, capture, preview, retry/remove/replace, and upload trouble with scope, campaign/shed/partition, animal where applicable, proof row/outbox/server ids, source/outcome/reason.
- Feed distribution terminal proof upload success/failure now emits proof row id, proof outbox id, server proof id, and terminal failure reason.
- PC Care animal-slot reconcile/repair now emits registration success/failure with tag/slot/proof row/proof outbox/registration outbox/server ids.
- Existing pushed fixes already include shared `ProofMediaPreview`, preview analytics guard, proof policy guard, Android/admin nav icon guards, bottom bar fit, vaccination stock icon mapping, and seed fixture expansion.
- Throwaway phone-QA seed now redacts the credentialed database URL and fails fast if the fixture loses Pramod PC Care tasks, per-shed feed-water removal rows, eight weighing buckets with both individual/lump-sum categories, eight vaccination shed assignments, 20 vaccination animals, per-animal one-proof vaccination policy, two counts cards, or four feed rows.

## Verification Already Run In This Segment

- `./apps/goatos-android/gradlew -p apps/goatos-android :app:compileDevDebugKotlin --no-parallel --console=plain`: passed after PC Care + weighing fasting changes.
- `git diff --check`: passed.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareRemovalPenSlotsTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --no-parallel --console=plain`: passed.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PenReconciliationExecuteEvidenceDraftTest' --tests 'sg.mesha.goatos.viewmodel.ScanViewModelTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareRemovalPenSlotsTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --no-parallel --console=plain`: passed.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.WeighingFastingDetailViewModelTest' --tests 'sg.mesha.goatos.viewmodel.WeighingViewModelTest' --no-parallel --console=plain`: passed.
- `go test ./internal/pccare/adapters/postgres ./internal/processintegrity/app -count=1`: passed.

## Active Agents

- Poincare `01a078f3-4ba8-7d12-82a9-a180c0553ad5`: read-only analytics/sync judge with one-month regression lens, completed and findings fixed locally except stronger seed retry/rework variants still pending.
- Hilbert `01a078f3-7164-7313-9c70-5ceee9135b71`: Counts + Vaccination analytics implementation, completed and integrated.
- Ohm `01a078f3-71d8-7690-bcc5-8971893dfe87`: Weighing analytics implementation, changes visible/integrated; final agent summary still pending.
- Hypatia `01a078f3-724a-75d3-b2ba-561c17867f4f`: backend PC Care removal payload cleanup, completed successfully.

## Pending Implementation

- Strengthen seed data with failed/retry/rework/outbox-state variants across proof features before phone E2E.
- Rerun a fresh analytics/sync judge after guard/test pass and seed strengthening.

## Pending Gates Before E2E

- `make mobile-guard-audit`
- `make frontend-foundations-guard`
- Backend analytics adapter focused test.
- Focused Go tests for PC Care and process integrity after final backend changes.
- `git diff --check`
- Rerun throwaway seed against OCI after seed self-check patch.

## E2E Plan

- Reseed OCI throwaway DB with `tools/local/phone-qa-throwaway-seed.sh`.
- Launch backend from laptop against OCI throwaway DB.
- Install/run dev package on allowed device/profile only.
- Grant camera/notification/Bluetooth/location permissions on Android user `10`.
- Capture screenshots inline for:
  - module drawer visible profile;
  - vaccination list/stock icon and per-animal proof preview/retry;
  - weighing fasting feed and water removal preview/retry;
  - weighing individual proof preview/retry;
  - weighing shed/lumpsum up to 5 videos without client-wide cap leak;
  - PC Care deworming, protozoa, ticks, hoof, hair, feed/water removal titles and preview/fullscreen/share/retry;
  - counts pen reconciliation preview/fullscreen/share/retry;
  - bottom bar title fit.

## Landing And Deploy

- Rebase on `origin/main`.
- Rerun local CI/landing receipt found in repo docs/scripts before merge.
- Push PR branch.
- Merge/land main only after receipt passes.
- Run staging deployment and mobile deployment.
- Verify Firebase Remote Config force-update flow:
  - dev builds skip update prompt;
  - staging/prod config update works as intended.
