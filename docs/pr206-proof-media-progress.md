# PR 206 Proof Media Progress

Last updated: 2026-09-07 08:47 IST after first receipt gate RED, fixing every named guard failure, and rerunning the failed guard set cleanly.

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
- Throwaway phone-QA seed now redacts the credentialed database URL and fails fast if the fixture loses Pramod PC Care tasks, per-shed feed-water removal rows, eight weighing buckets with both individual/lump-sum categories, Pramod's per-shed weighing fasting card, eight vaccination shed assignments, 32 vaccination obligation member rows, concrete Part labels for partitioned QA animals, per-animal one-proof vaccination policy, two counts cards, or four feed rows.
- Phone-QA runner defaults to Pramod, the visible CBE operator profile used for this E2E.
- Fixed fresh judge blockers from Goodall/Copernicus/James:
  - backend analytics allowlist now durably queues the missing vaccination/weighing/removal/SOP proof-media events and funnel submit events;
  - generic SubmitViewModel SOP proof capture, retry, and submit now emit bounded proof/outbox/server correlation ids;
  - feed distribution capture success and terminal submit sync events now emit canonical proof row/outbox/server refs;
  - shared ProofMediaPreview share action now reports success/failure outcome and failure reason;
  - sync terminal telemetry extracts feature-specific proof outbox keys for feed distribution, packing, and wastage;
  - phone-QA seed now publishes weighing parent campaigns and self-checks that both are live.
- Tightened the follow-up stale judge edges:
  - sync terminal telemetry now also extracts photo-style proof outbox keys such as toxin strip photos and the first value from `proof_outbox_item_ids` maps;
  - PC Care preview analytics now carries the parsed preview `action` separately from outcome/reason for both animal-slot and task-proof previews.
- Fixed fresh Huygens/Averroes/Arendt blockers:
  - backend analytics allowlist now durably queues vaccination auto-submit receipts, vaccination submit success/failure, and sync write attempt failures;
  - PC Care task-proof submit enqueue/sync success/sync failure events include aggregate proof row/upload/server ids from the actual task proof slots;
  - vaccination auto-submit enqueue/sync success/sync failure events include aggregate proof row/upload/server ids from the roster/proof-action rows;
  - sync proof map extraction is deterministic for `proof_outbox_item_ids`;
  - phone-QA seed now has a Pramod-visible Yashoda over-five vaccination path and a PC Care removal rework row;
  - phone-QA runner hard-fails unless serial `143382555G111292` and Android user `10` are active.
- Fixed fresh Darwin/Franklin blockers:
  - Counts pen reconciliation proof-upload outbox observer now emits terminal upload sync success/failure analytics before the completion submit sync observer;
  - debug RFID aliaser now reuses a physical sample card after its cached animal leaves the open roster, making the six-animal Yashoda over-five vaccination path phone-walkable without fake physical RFIDs;
  - seed docs now state that over-five phone coverage depends on this debug-only card reuse.
- Fixed vaccination synced-preview contract gap found during phone E2E:
  - backend scan roster now returns `latestProofId` and `latestProofDownloadUrl` for synced goat proof rows;
  - Android network DTO, Room cache, migration 60->61, repository mapping, and ScanViewModel now map synced proof refs into common `ProofMediaPreview`;
  - scan feed rows and selected-goat cards now render common preview/fullscreen/share/play controls for uploaded vaccination proofs instead of only a compact `Replace` row.
- Fixed fresh judge blockers from Zeno/Gauss:
  - phone-QA runner now hard-fails any non-Pramod app user id in addition to serial/user-profile checks;
  - feed distribution replacement capture no longer clears existing photo/video preview paths before the replacement proof is captured and queued.
- Fixed synced remote proof playback for vaccination scan rows:
  - the service resolves `latestProofId` to a signed stream URL before returning scan-roster rows;
  - Android `ProofMediaPreview` no longer tries to call the auth-gated `/app/proofs/{id}/download` JSON endpoint itself; media playback keeps using the common proof player/telemetry path.
- Fixed vaccination scan retry visibility for server-synced proof rows: proof-needed rows with a resolved preview path now show the same Replace action and arm the existing replacement scan flow.
- Fixed fresh Mill sync-architecture blockers:
  - vaccination scan rows now treat durable backend `latestProofId` as synced proof identity even when only one goat in the shed is done;
  - cached signed proof URLs are only used as preview hints while their `expires` query is still valid, preventing warm Room cache playback from using expired credentials;
  - phone-QA runner now restarts the LaunchAgent when the existing API lacks the required phone-reachable `GOATOS_API_PUBLIC_BASE_URL=http://127.0.0.1:8080`.
- Fixed fresh Hooke analytics blockers in common `ProofMediaPreview`:
  - local share URI/FileProvider failures are caught before bubbling out, so all feature preview handlers receive `share:failure:fileprovider_rejected`;
  - playback errors now emit `playback_failed:failure:<bounded_reason>` instead of a bare preview action.
- Fixed fresh Archimedes sync/offline blockers:
  - Android now expires both local `expires` signed proof URLs and GCS `X-Goog-Date` + `X-Goog-Expires` signed URLs before replaying cached preview hints from Room;
  - backend scan roster hides `latestProofId` and `latestProofDownloadUrl` when the animal's latest vaccination verdict is rejected, preventing stale accepted/rejected proof history from satisfying a replacement gate.
- Fixed fresh Dewey OCI seed/E2E blocker:
  - phone-QA seed now creates normal feed distribution completion pending/rework/completed coverage and experiment feed wastage pending/rework/completed coverage for Pramod;
  - seed self-checks now fail if feed distribution completion states, feed wastage states, or experiment feed direction rows are missing.
- Fixed fresh Dirac sync/offline blockers:
  - stale/offline task-detail cache entries without `sopVersion` fall back to `ProofPolicy.Default` instead of crashing task rendering;
  - vaccination `ProofMediaPreview` playback failures now keep the analytics event and trigger a bounded scan-roster refresh so expired signed proof URLs are refreshed.
- Fixed fresh Euler analytics blocker:
  - feed packing, feed transport, and feed wastage proof previews now forward common preview actions into their owning ViewModels;
  - the ViewModels emit feed proof preview analytics with action, outcome, reason, source, kind, field, group key, local proof row id, and proof upload outbox id where available.
- Fixed fresh Singer analytics blockers:
  - PC Care animal-slot proof upload terminal analytics now emit slot field, composite field key, RFID, local proof row id, proof-upload outbox id, server proof id, outcome, source, and failure reason;
  - PC Care animal-slot submit enqueue/sync terminal analytics now emit aggregate slot/RFID/proof/outbox/server ids plus submit outbox id, outcome, source, and reason;
  - PC Care animal-slot reconcile analytics now sends the visible slot as `slotFieldKey` and the composite proof row key as `slotKey`.
  - follow-up re-review from Singer is CLEAN after adding the missing upload terminal `source` key.
- Fixed fresh Epicurus sync blocker:
  - weighing shed/lumpsum replacement at the explicit five-video cap now uses a replacement-aware repository path that can temporarily exceed the cap while preserving the old durable proof until the replacement upload syncs;
  - ordinary new sixth captures remain blocked by the same explicit weighing feature cap.
  - follow-up re-review from Epicurus says the weighing blockers are fixed; only pending note is staging/tracking the new Room schema 61 file before pushing the PR.

## Verification Already Run In This Segment

- `./apps/goatos-android/gradlew -p apps/goatos-android :app:compileDevDebugKotlin --no-parallel --console=plain`: passed after PC Care + weighing fasting changes.
- `git diff --check`: passed.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareRemovalPenSlotsTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --no-parallel --console=plain`: passed.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PenReconciliationExecuteEvidenceDraftTest' --tests 'sg.mesha.goatos.viewmodel.ScanViewModelTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareRemovalPenSlotsTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --no-parallel --console=plain`: passed.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.WeighingFastingDetailViewModelTest' --tests 'sg.mesha.goatos.viewmodel.WeighingViewModelTest' --no-parallel --console=plain`: passed.
- `go test ./internal/pccare/adapters/postgres ./internal/processintegrity/app -count=1`: passed.
- `GOATOS_ENV=local DATABASE_URL=<OCI throwaway> tools/local/phone-qa-throwaway-seed.sh`: passed after the seed reset stale fasting submissions and stopped deduping multi-vaccine obligation members.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:compileDevDebugKotlin --no-parallel --console=plain`: passed after latest analytics + seed patches.
- Focused Android tests for weighing fasting, weighing, PC Care task/removal gates, counts reconciliation, vaccination, and backend analytics adapter: passed.
- `node tools/agent-hooks/check-android-proof-preview-analytics.mjs --self-test && node tools/agent-hooks/check-android-proof-preview-analytics.mjs --all`: passed.
- `make mobile-guard-audit`: passed.
- `make frontend-foundations-guard`: passed.
- `git diff --check`: passed.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:compileDevDebugKotlin --no-parallel --console=plain`: passed after fresh judge blocker fixes.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.analytics.BackendAnalyticsAdapterTest' --tests 'sg.mesha.goatos.viewmodel.FeedDistributionCompleteViewModelTest' --tests 'sg.mesha.goatos.viewmodel.SubmitViewModelFormTest' --no-parallel --console=plain`: passed.
- `./apps/goatos-android/gradlew -p apps/goatos-android :core:core-data:testDebugUnitTest --tests 'sg.mesha.goatos.core.data.sync.FeedCompletionPayloadContractTest' --no-parallel --console=plain`: passed.
- `GOATOS_ENV=local DATABASE_URL=<OCI throwaway> tools/local/phone-qa-throwaway-seed.sh`: passed after publishing weighing parent campaigns.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:compileDevDebugKotlin --no-parallel --console=plain`: passed after sync extractor and PC Care preview analytics tightening.
- Focused Android proof-media unit suite for backend analytics, feed distribution, generic submit SOP, weighing fasting, weighing, PC Care proof/removal/submit, counts reconciliation, and vaccination scan: passed after updating preview action assertions to require separate `action` and `outcome`.
- `./apps/goatos-android/gradlew -p apps/goatos-android :core:core-data:testDebugUnitTest --tests 'sg.mesha.goatos.core.data.sync.FeedCompletionPayloadContractTest' --no-parallel --console=plain`: passed after widening sync proof outbox id extraction.
- `node tools/agent-hooks/check-android-proof-preview-analytics.mjs --all && git diff --check`: passed.
- `GOATOS_ENV=local DATABASE_URL=<OCI throwaway> tools/local/phone-qa-throwaway-seed.sh`: passed after adding Yashoda 6-animal over-five coverage and PC Care rework validation.
- Focused Android app unit suite for backend analytics, vaccination scan submit receipts, PC Care proof/removal/submit analytics: passed.
- `./apps/goatos-android/gradlew -p apps/goatos-android :core:core-data:testDebugUnitTest --tests 'sg.mesha.goatos.core.data.sync.SyncEngineTelemetryTest' --no-parallel --console=plain`: passed with extractor shape coverage.
- `node tools/agent-hooks/check-android-proof-preview-analytics.mjs --all && git diff --check`: passed after latest judge fixes.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:clean :app:compileDevDebugKotlin --no-parallel --console=plain`: passed after clearing Gradle/Kotlin cache corruption caused by overlapping local Gradle invocations.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.rfid.DebugSampleTagAliaserTest' --no-parallel --console=plain`: passed after debug card-reuse fix.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PenReconciliationExecuteEvidenceDraftTest' --no-parallel --console=plain`: passed after Counts proof-upload terminal analytics fix.
- `node tools/agent-hooks/check-android-proof-preview-analytics.mjs --all && git diff --check`: passed after fresh Darwin/Franklin fixes.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:compileDevDebugKotlin :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.FeedDistributionCompleteViewModelTest' --no-parallel --console=plain`: passed after feed retry preview preservation.
- `GOATOS_ENV=local DATABASE_URL=<OCI throwaway> tools/local/phone-qa-throwaway-seed.sh`: passed after adding Pramod feed packing/transport retry states and counts open/completed/rework states.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareRemovalPenSlotsTest' --tests 'sg.mesha.goatos.viewmodel.WeighingViewModelTest' --no-parallel --console=plain`: passed after PC Care animal-slot analytics and weighing replacement-over-cap fixes.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.analytics.BackendAnalyticsAdapterTest' --tests 'sg.mesha.goatos.viewmodel.FeedDistributionCompleteViewModelTest' --tests 'sg.mesha.goatos.viewmodel.FeedPackingCompleteSubmitGuardTest' --tests 'sg.mesha.goatos.viewmodel.FeedTransportSequenceTest' --tests 'sg.mesha.goatos.viewmodel.FeedWastageCompleteAnalyticsTest' --tests 'sg.mesha.goatos.viewmodel.SubmitViewModelFormTest' --tests 'sg.mesha.goatos.viewmodel.WeighingFastingDetailViewModelTest' --tests 'sg.mesha.goatos.viewmodel.WeighingViewModelTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareRemovalPenSlotsTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PenReconciliationExecuteEvidenceDraftTest' --tests 'sg.mesha.goatos.viewmodel.ScanViewModelTest' --tests 'sg.mesha.goatos.rfid.DebugSampleTagAliaserTest' --no-parallel --console=plain`: passed after PC Care source-key fix.
- `node tools/agent-hooks/check-android-proof-preview-analytics.mjs --all && node tools/agent-hooks/check-android-proof-policy-default.mjs --all && git diff --check`: passed after PC Care source-key fix.
- `make mobile-guard-audit`: passed after PC Care source-key fix.
- `go test ./internal/pccare/adapters/postgres ./internal/processintegrity/app -count=1 && go test ./internal/vaccinationexecution/adapters/postgres ./internal/vaccinationexecution/app -run 'TestScanRoster|TestScanRosterSQL|TestScanRosterUses|TestScanRosterOperator|TestScanRosterOne|TestScanRosterPark|TestScanRosterExcludes|TestScanRosterResolves' -count=1`: passed.
- `DATABASE_URL=<OCI throwaway> GOATOS_ENV=local tools/local/phone-qa-throwaway-seed.sh`: passed after latest local fixes.
- `./apps/goatos-android/gradlew -p apps/goatos-android :core:core-ui:compileDebugKotlin :app:compileDevDebugKotlin :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.FeedDistributionCompleteViewModelTest' --no-parallel --console=plain`: passed after the earlier common remote proof-preview resolver attempt.
- DB/media diagnosis showed Yashoda synced proof rows have completed nonzero MP4 artifacts, while plain `/app/proofs/{id}/download` returns 401 without bearer; fix moved signed URL resolution to the vaccination execution service.
- `go test ./internal/vaccinationexecution/app ./internal/vaccinationexecution/adapters/postgres -run 'TestScanRoster|Test' -count=1`: passed after service-side signed URL resolution.
- `./apps/goatos-android/gradlew -p apps/goatos-android :core:core-ui:compileDebugKotlin :app:compileDevDebugKotlin --no-parallel --console=plain`: passed after removing the broken UI-side auth-gated `/download` resolver.
- Direct API probe confirmed vaccination scan roster now returns phone-reachable signed proof URLs on `http://127.0.0.1:8080/.../download/signed?...` for the allowed phone reverse tunnel.
- `./apps/goatos-android/gradlew -p apps/goatos-android :core:core-ui:compileDebugKotlin :app:compileDevDebugKotlin --no-parallel --console=plain`: passed after fresh signed-url cache and common preview analytics fixes.
- `node tools/agent-hooks/check-android-proof-preview-analytics.mjs --all && git diff --check`: passed after common preview analytics fixes.
- `bash -n tools/local/phone-qa-throwaway-run.sh`: passed after public-base health-check tightening.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.ScanViewModelTest' --no-parallel --console=plain`: passed after adding server-only latest-proof and expired signed-URL regression tests.
- `./apps/goatos-android/gradlew -p apps/goatos-android :core:core-ui:testDebugUnitTest --tests 'sg.mesha.goatos.core.ui.ProofMediaPreviewContractTest' --no-parallel --console=plain`: passed after updating stale playback-failure contract assertions.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.ScanViewModelTest' --no-parallel --console=plain`: passed after adding GCS signed-URL expiry regression coverage.
- `cd backend && go test ./internal/vaccinationexecution/adapters/postgres ./internal/vaccinationexecution/app -run 'TestScanRoster|TestScanRosterSQL|TestScanRosterUses|TestScanRosterOperator|TestScanRosterOne|TestScanRosterPark|TestScanRosterExcludes|TestScanRosterResolves' -count=1`: passed after rejected latest-proof SQL guard.
- `node tools/agent-hooks/check-android-proof-preview-analytics.mjs --all && git diff --check`: passed after Archimedes fixes.
- `bash -n tools/local/phone-qa-throwaway-seed.sh && DATABASE_URL=<OCI throwaway> GOATOS_ENV=local tools/local/phone-qa-throwaway-seed.sh`: passed after adding feed distribution/wastage completion seed coverage.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:compileDevDebugKotlin --no-parallel --console=plain`: passed after task-cache, vaccination playback-failure refresh, and feed preview analytics wiring.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.ScanViewModelTest' --tests 'sg.mesha.goatos.viewmodel.FeedTransportSequenceTest' --tests 'sg.mesha.goatos.viewmodel.FeedPackingCompleteViewModelTest' --tests 'sg.mesha.goatos.viewmodel.FeedWastageCompleteViewModelTest' --no-parallel --console=plain`: passed after adding scan playback-failure refresh and transport preview analytics regression tests. Robolectric printed a temp-directory cleanup warning, but Gradle exited successful.
- `./apps/goatos-android/gradlew -p apps/goatos-android :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.FeedPackingCompleteSubmitGuardTest' --tests 'sg.mesha.goatos.viewmodel.FeedTransportSequenceTest' --tests 'sg.mesha.goatos.viewmodel.FeedWastageCompleteAnalyticsTest' --no-parallel --console=plain`: passed after adding wastage proof-upload terminal analytics coverage.
- `make mobile-guard-audit`: passed after removing the final task-cache `ProofPolicy.Default` fallback and adding proof-policy/default omission coverage.
- `node tools/agent-hooks/check-android-proof-preview-analytics.mjs --all && node tools/agent-hooks/check-android-proof-policy-default.mjs --all && git diff --check`: passed after feed terminal-upload analytics and missing-SOP proof-policy fixes.
- `make frontend-foundations-guard`: passed after latest Android/admin icon and copy guard changes.
- `bash -n tools/local/phone-qa-throwaway-seed.sh && DATABASE_URL=<OCI throwaway> GOATOS_ENV=local tools/local/phone-qa-throwaway-seed.sh`: passed after tomorrow feed packing source rows and widened proof-media fixture checks.
- `cd backend && go test ./internal/pccare/adapters/postgres ./internal/processintegrity/app -count=1`: passed.
- `cd backend && go test ./internal/vaccinationexecution/adapters/postgres ./internal/vaccinationexecution/app -run 'TestScanRoster|TestScanRosterSQL|TestScanRosterUses|TestScanRosterOperator|TestScanRosterOne|TestScanRosterPark|TestScanRosterExcludes|TestScanRosterResolves' -count=1`: passed after orphan-proof roster guard.

## Active Agents

- Closed stale agents Mill, Hooke, Confucius, Peirce, Chandrasekhar, and Banach after fixing their reported blockers.
- Singer `01a079aa-6ccc-7ba0-97ba-389b1c822e55`: fresh read-only analytics journey judge with 1-month regression lens.
- Epicurus `01a079aa-6d52-7510-aa2a-9e92bab486cc`: fresh read-only sync/outbox/offline architecture judge.
- Lovelace `01a079aa-6dd7-7af1-966f-e109e3baf410`: fresh read-only UI/header/icon/common-preview judge.
- Beauvoir `01a079aa-6f02-7121-ae73-072751375101`: fresh read-only OCI seed/E2E readiness judge.

## Current State

- Backend `/app/bootstrap` accepts the same baked Gradle dev bearer and returns Pramod/operator.
- Patched `SessionViewModel` so dev auth can use the baked dev bearer while DataStore warms or has not emitted yet; stg/prod Firebase marker behavior is unchanged.
- Rebuilt/reinstalled through `tools/local/phone-qa-throwaway-run.sh -s 143382555G111292`; runner confirmed Android user `10`, cleared that user's app data, and launched.
- Post-launch reached permission gate, then after granting permissions and relaunching reached Pramod's authenticated shell.
- Latest shell screenshot: `/tmp/goatos-pr206-e2e-screens/78_pramod_shell_after_auth_patch.png`; visible vaccination page shows `13 sheds`, `19 doses`, `13 open`.
- E2E must now be restarted on a freshly reseeded/reinstalled latest APK because Darwin/Franklin fixes landed after the first fasting feed preview capture.
- OCI throwaway DB reseed passed after debug RFID card-reuse documentation.
- Latest APK was reinstalled through the guarded runner on serial `143382555G111292`, Android user `10`; permissions were granted again.
- Fresh latest baseline screenshot: `/tmp/goatos-pr206-e2e-screens/84_fresh_latest_pramod_shell.png`; visible vaccination page shows `13 sheds`, `19 doses`, `13 open`.
- Latest phone E2E is running on the allowed device/profile only: serial `143382555G111292`, Android user `10`, package `sg.mesha.goatos.dev`, OCI throwaway DB.
- Vaccination E2E exposed two synced-preview gaps: backend scan roster initially did not return proof media refs, then it returned auth-gated `/download` URLs that the media player could not stream. Backend now resolves signed URLs server-side and phone E2E captured playable synced preview/fullscreen/share on Yashoda Part 2.
- 2026-09-07 07:33 IST: Feed Packing E2E seed gap fixed. The app queries the feed day (`packing day + 1`), so the OCI throwaway fixture now seeds tomorrow `feed_direction_issues`/`feed_direction_issue_rows` source sheets plus matching `feed_packing_completions`. Self-checks assert tomorrow CBE source rows and Pramod pending/rework completion states.

## Phone E2E Checkpoints

- Weighing home/list captured at `/tmp/goatos-pr206-e2e-screens/85_latest_weighing_home.png`.
- Weighing fasting Godel 1:
  - empty feed/water task captured at `/tmp/goatos-pr206-e2e-screens/86_latest_fasting_detail_empty.png`;
  - feed preview captured at `/tmp/goatos-pr206-e2e-screens/87_latest_fasting_feed_preview.png`;
  - feed fullscreen/share controls captured at `/tmp/goatos-pr206-e2e-screens/88_latest_fasting_feed_fullscreen.png`;
  - retry camera and post-retry preview captured at `/tmp/goatos-pr206-e2e-screens/91_latest_fasting_feed_retry_camera.png` and `/tmp/goatos-pr206-e2e-screens/92_latest_fasting_feed_retry_preview.png`;
  - both feed/water videos ready captured at `/tmp/goatos-pr206-e2e-screens/93_latest_fasting_both_videos_ready.png`;
  - submitted/review state captured at `/tmp/goatos-pr206-e2e-screens/95_latest_fasting_submit_result.png`.
- Weighing individual Godel 1:
  - entry and RFID pre-roll captured at `/tmp/goatos-pr206-e2e-screens/97_latest_weighing_individual_entry.png` and `/tmp/goatos-pr206-e2e-screens/98_latest_weighing_individual_after_rfid.png`;
  - saved proof preview captured at `/tmp/goatos-pr206-e2e-screens/99_latest_weighing_individual_proof_preview.png`;
  - retry prompt/cancel-preserves-proof path captured at `/tmp/goatos-pr206-e2e-screens/101_latest_weighing_individual_retry_prompt.png` and `/tmp/goatos-pr206-e2e-screens/102_latest_weighing_individual_after_retry_cancel.png`;
  - weight saved and submit confirmation captured at `/tmp/goatos-pr206-e2e-screens/104_latest_weighing_individual_saved.png`, `/tmp/goatos-pr206-e2e-screens/105_latest_weighing_individual_submitted.png`, and `/tmp/goatos-pr206-e2e-screens/106_latest_weighing_individual_submit_confirmed.png`.
- Weighing lumpsum Yashoda 1:
  - empty entry and first proof preview captured at `/tmp/goatos-pr206-e2e-screens/107_latest_weighing_lumpsum_entry.png` and `/tmp/goatos-pr206-e2e-screens/110_latest_weighing_lumpsum_video_preview.png`;
  - five uploaded/synced proof previews captured at `/tmp/goatos-pr206-e2e-screens/122_latest_weighing_lumpsum_topmost.png`;
  - the five-video stop is the explicit weighing `MAX_SHED_GROUP_VIDEOS` policy (`Maximum 5 videos recorded`), not the removed shared `Maximum 5 proof videos reached for this subject` cap;
  - total weight entered at `/tmp/goatos-pr206-e2e-screens/123_latest_weighing_lumpsum_weight_entered.png`;
  - successful submit/list return captured at `/tmp/goatos-pr206-e2e-screens/125_latest_weighing_lumpsum_submitted_or_dialog.png`.
- Vaccination Yashoda Part 2 synced preview:
  - playable common preview captured at `/tmp/goatos-pr206-e2e-screens/162_vaccination_part2_detail_signed_url.png`;
  - inline play captured at `/tmp/goatos-pr206-e2e-screens/163_vaccination_part2_preview_playing.png`;
  - fullscreen playback captured at `/tmp/goatos-pr206-e2e-screens/164_vaccination_part2_preview_fullscreen.png`;
  - fullscreen share sheet captured at `/tmp/goatos-pr206-e2e-screens/165_vaccination_part2_fullscreen_share_sheet.png`;
  - visible Replace retry control captured at `/tmp/goatos-pr206-e2e-screens/170_vaccination_part2_replace_visible.png`;
  - armed retry state preserving the old proof captured at `/tmp/goatos-pr206-e2e-screens/171_vaccination_part2_replace_armed.png` and `/tmp/goatos-pr206-e2e-screens/172_vaccination_part2_retry_camera_or_state.png`.
- PC Care Deworming feed/water removal:
  - module drawer on Pramod profile with Preventive Care visible captured at `/tmp/goatos-pr206-e2e-screens/174_module_drawer_pramod_after_latest.png`;
  - Preventive Care/Deworming home captured at `/tmp/goatos-pr206-e2e-screens/175_pccare_home_pramod.png`;
  - per-shed/partition removal detail captured at `/tmp/goatos-pr206-e2e-screens/176_pccare_deworming_removal_detail.png`, showing human title `Remove feed & water`, shed/partition rows, and no raw `feed_water_removal` header leak;
  - first feed-removal camera opened correctly and is recording on the allowed phone/profile at `/tmp/goatos-pr206-e2e-screens/177_pccare_deworming_feed_camera.png`.
  - feed-removal saving/preview transition captured at `/tmp/goatos-pr206-e2e-screens/178_pccare_deworming_feed_after_stop.png` and `/tmp/goatos-pr206-e2e-screens/179_pccare_deworming_feed_preview.png`;
  - settled feed-removal synced preview with `Proof sent` and `Replace video` captured at `/tmp/goatos-pr206-e2e-screens/180_pccare_deworming_feed_preview_settled.png`.
  - after fresh judge fixes and reinstall, preserved feed-removal preview was still visible at `/tmp/goatos-pr206-e2e-screens/186_pccare_deworming_detail_after_latest_fix.png`;
  - latest fullscreen playback captured at `/tmp/goatos-pr206-e2e-screens/187_pccare_deworming_feed_fullscreen_latest.png`;
  - latest Android share sheet for the proof video captured at `/tmp/goatos-pr206-e2e-screens/188_pccare_deworming_feed_share_sheet_latest.png`;
  - latest Replace/retry camera captured at `/tmp/goatos-pr206-e2e-screens/189_pccare_deworming_feed_replace_state_latest.png`;
  - replacement proof saved back to durable preview at `/tmp/goatos-pr206-e2e-screens/190_pccare_deworming_feed_after_replace_saved_latest.png`.
  - Gandhi 1 - Part 1 water-removal camera opened at `/tmp/goatos-pr206-e2e-screens/191_pccare_deworming_water_camera_latest.png`;
  - Gandhi 1 - Part 1 water-removal proof saved beside feed-removal proof at `/tmp/goatos-pr206-e2e-screens/192_pccare_deworming_water_preview_latest.png`; submit remains disabled because the multi-shed task still needs the remaining sheds' feed/water slots.
- PC Care tab/title/icon coverage:
  - Protozoa, Ticks, Hoof, and Hair tab screens captured at `/tmp/goatos-pr206-e2e-screens/193_pccare_protozoa_tab_latest.png`, `/tmp/goatos-pr206-e2e-screens/194_pccare_ticks_tab_latest.png`, `/tmp/goatos-pr206-e2e-screens/195_pccare_hoof_tab_latest.png`, and `/tmp/goatos-pr206-e2e-screens/196_pccare_hair_tab_latest.png`; bottom labels fit and use human titles.
  - Hair animal proof detail captured at `/tmp/goatos-pr206-e2e-screens/197_pccare_hair_detail_latest.png`;
  - Hair before-trimming camera and common preview captured at `/tmp/goatos-pr206-e2e-screens/199_pccare_hair_before_camera_latest.png` and `/tmp/goatos-pr206-e2e-screens/200_pccare_hair_preview_latest.png`.
- Counts/Reconcile:
  - Reconcile list with open, rework, and completed seeded states captured at `/tmp/goatos-pr206-e2e-screens/204_counts_reconcile_tab_latest.png`;
  - rework detail with required live video captured at `/tmp/goatos-pr206-e2e-screens/205_counts_reconcile_rework_detail_latest.png`;
  - pen-return camera captured at `/tmp/goatos-pr206-e2e-screens/206_counts_reconcile_camera_latest.png`;
  - synced common preview with fullscreen/share/play and Re-record captured at `/tmp/goatos-pr206-e2e-screens/207_counts_reconcile_preview_latest.png`.
- Feed Direction:
  - Feed Distribution home and detail captured at `/tmp/goatos-pr206-e2e-screens/208_feed_home_latest.png` and `/tmp/goatos-pr206-e2e-screens/209_feed_direction_detail_latest.png`;
  - feed weight photo camera/preview captured at `/tmp/goatos-pr206-e2e-screens/210_feed_photo_camera_latest.png` and `/tmp/goatos-pr206-e2e-screens/211_feed_photo_preview_latest.png`;
  - feed distribution video camera/preview captured at `/tmp/goatos-pr206-e2e-screens/212_feed_video_camera_latest.png`, `/tmp/goatos-pr206-e2e-screens/213_feed_video_preview_latest.png`, and `/tmp/goatos-pr206-e2e-screens/214_current_feed_state.png`;
  - water distribution slot/camera/preview captured at `/tmp/goatos-pr206-e2e-screens/215_feed_water_slot_latest.png`, `/tmp/goatos-pr206-e2e-screens/216_feed_water_camera_latest.png`, and `/tmp/goatos-pr206-e2e-screens/217_feed_water_preview_latest.png`;
  - all three feed proofs ready in common preview UI captured at `/tmp/goatos-pr206-e2e-screens/218_feed_all_three_ready_submit_latest.png`, `/tmp/goatos-pr206-e2e-screens/219_feed_submit_button_latest.png`, and `/tmp/goatos-pr206-e2e-screens/220_feed_after_water_scroll_latest.png`.
- Fresh post-seed/reinstall proof:
  - Pramod/dev drawer after guarded reinstall captured at `/tmp/goatos-pr206-e2e-screens/232_drawer_after_packing_seed_fix.png` and `/tmp/goatos-pr206-e2e-screens/235_drawer_after_tomorrow_packing_seed.png`;
  - Feed Direction after fresh seed captured at `/tmp/goatos-pr206-e2e-screens/233_feed_after_packing_seed_fix.png`;
  - Feed Packing initially proved completion-only seed was insufficient at `/tmp/goatos-pr206-e2e-screens/234_feed_packing_visible_after_seed_fix.png`;
  - after adding tomorrow source feed rows, Feed Packing shows visible Godel/Yashoda rows at `/tmp/goatos-pr206-e2e-screens/236_feed_packing_rows_after_tomorrow_seed.png`.
  - Feed Packing pending detail/camera/common preview captured at `/tmp/goatos-pr206-e2e-screens/237_feed_packing_detail_or_row.png`, `/tmp/goatos-pr206-e2e-screens/238_feed_packing_camera.png`, and `/tmp/goatos-pr206-e2e-screens/239_feed_packing_preview_after_record.png`;
  - Feed Packing fullscreen/share/retry replacement captured at `/tmp/goatos-pr206-e2e-screens/240_feed_packing_fullscreen.png`, `/tmp/goatos-pr206-e2e-screens/241_feed_packing_fullscreen_share.png`, `/tmp/goatos-pr206-e2e-screens/244_feed_packing_retry_camera_correct.png`, and `/tmp/goatos-pr206-e2e-screens/245_feed_packing_after_retry_saved.png`.
- Final guarded post-fix reinstall on Pramod/dev package:
  - app home after reinstall and permission gate captured at `/tmp/goatos-pr206-e2e-screens/246_after_reinstall_home.png` and `/tmp/goatos-pr206-e2e-screens/247_after_permission_gate_tap.png`;
  - Pramod drawer with dev build and all expected modules captured at `/tmp/goatos-pr206-e2e-screens/248_drawer_fresh_final.png`;
  - vaccination home/detail and debug RFID camera launch captured at `/tmp/goatos-pr206-e2e-screens/249_vaccination_home_final.png`, `/tmp/goatos-pr206-e2e-screens/250_vaccination_detail_final.png`, and `/tmp/goatos-pr206-e2e-screens/251_vaccination_after_debug_rfid_final.png`;
  - weighing home shows separate feed/water fasting cards plus individual and lump-sum tasks at `/tmp/goatos-pr206-e2e-screens/253_weighing_home_final.png`;
  - weighing fasting detail/camera/common preview/retry camera captured at `/tmp/goatos-pr206-e2e-screens/254_weighing_fasting_detail_final.png`, `/tmp/goatos-pr206-e2e-screens/255_weighing_fasting_camera_final.png`, `/tmp/goatos-pr206-e2e-screens/256_weighing_fasting_preview_final.png`, and `/tmp/goatos-pr206-e2e-screens/257_weighing_fasting_retry_camera_final.png`;
  - weighing lump-sum card/detail/0kg guard/recording/common preview captured at `/tmp/goatos-pr206-e2e-screens/258_weighing_lumpsum_card_final.png`, `/tmp/goatos-pr206-e2e-screens/259_weighing_lumpsum_detail_final.png`, `/tmp/goatos-pr206-e2e-screens/261_weighing_lumpsum_preview_final.png`, `/tmp/goatos-pr206-e2e-screens/262_weighing_lumpsum_recording_final.png`, and `/tmp/goatos-pr206-e2e-screens/263_weighing_lumpsum_saved_final.png`;
  - PC Care home/tabs, feed-water detail, camera, and common preview captured at `/tmp/goatos-pr206-e2e-screens/264_pccare_home_final.png`, `/tmp/goatos-pr206-e2e-screens/265_pccare_feed_water_detail_final.png`, `/tmp/goatos-pr206-e2e-screens/266_pccare_feed_water_camera_final.png`, and `/tmp/goatos-pr206-e2e-screens/267_pccare_feed_water_preview_final.png`.

## Current State

- OCI throwaway DB E2E screenshots have been collected on the allowed device/profile only: serial `143382555G111292`, Android user `10`, package `sg.mesha.goatos.dev`, visible operator Pramod.
- Dev-build force-update skip was visually verified after guarded reinstall: no upgrade gate appeared before the module screens.
- Branch is locally amended/rebased at `c3afb533be102a3ff0449e5d074b3a70399093c1`; remote branch is stale and must be reconciled after the local receipt gate passes.
- A second `make ci-local-screenshots` run is in progress with log `/tmp/pr206-ci-local-screenshots-2.log`; Android screenshot/Paparazzi verification is the active long-running leg.

## Pending Gates Before Push/Merge

- Wait for the running `make ci-local-screenshots` command to finish.
- If RED, fix only the named failures, amend the current commit, and rerun the exact receipt gate.
- If GREEN, verify the repo-local receipt SHA matches HEAD, push PR 206, then continue through the landing gate.
- 2026-09-07: second receipt gate was RED only on Android screenshots; all non-screenshot CI legs passed. The failing PNGs were expected Paparazzi golden drift from intentional nav/form UI changes, so the nine named goldens from `RoleChromeScreenshotTest` and `ScreenshotTest.form_runner` were refreshed before rerunning the screenshot leg.
- Fresh judges after golden refresh:
  - Hume UI/header/icon/common-preview judge returned CLEAN.
  - Plato seed/E2E judge found a runner guard gap: `phone-qa-throwaway-run.sh` enforced Pramod/user 10, but `android-dev-run.sh` installed device-wide. Fixed by adding `GOATOS_ANDROID_INSTALL_USER`, installing with `adb install --user`, and exporting user `10` from the throwaway runner.
  - Noether analytics/offline-sync judge found missing analytics-visible outbox enqueue/attempt/retry/success events and missing enqueue IDs. Fixed centrally in outbox telemetry with `sync_write_enqueued`, `sync_write_attempt_started`, `sync_write_retry_scheduled`, and `sync_write_succeeded`, with group/idempotency/proof outbox IDs.
  - Focused telemetry tests passed after these fixes:
    `FailureReportingOutboxTelemetryReporterTest`, `SyncEngineTelemetryTest`, and `BackendAnalyticsAdapterTest`.

## Landing Receipt Loop

- First `make ci-local-screenshots` on rebased SHA `93885d4a5eaa609c46cb968644db26ed697f68eb` was RED.
- Second `make ci-local-screenshots` on amended SHA `c3afb533be102a3ff0449e5d074b3a70399093c1` was RED only on `android screenshots`; the actual rendered screens were valid and the checked-in goldens were stale.
- Fixed every named PR-caused blocker:
  - registered new admin/mobile nav/proof guards in `tools/ci/guardrail-manifest.json`;
  - updated CEO coverage matrix for the vaccination proof URL resolver surface;
  - added explicit exception exemptions for signed proof URL parsing helpers;
  - ran gofmt on vaccination execution Go files;
  - added changed scan-roster projection test lens marker;
  - updated vaccine/weighing proof-context guard to require Yashoda over-five plus 3-animal coverage;
  - annotated ViewModel-lifetime terminal-event tracking sets for bounded-memory guard.
- Individual rerun of the previously failing guard set passed on amended SHA `1093ddb6b`.

## E2E Plan

- Completed against OCI throwaway DB using `tools/local/phone-qa-throwaway-seed.sh` and guarded runner `tools/local/phone-qa-throwaway-run.sh`.
- Completed install/run on allowed device/profile only.
- Completed camera/notification/Bluetooth/location permission grants on Android user `10`.
- Captured screenshots for:
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
