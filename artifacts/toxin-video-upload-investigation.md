# Toxin Video Upload Investigation

Started: 2026-09-12 21:47 IST
Branch: fix/toxin-video-upload-analytics
Base: origin/main e77a27dd1

## User Report

Dinkar is unable to upload toxin test videos. Screenshot shows toxin flow:

- Header: "Dry Masoor Bhusa - Sanchit - CBE - 2026-09-08"
- Completed step: "Let it sit"
- Active step: "Dilute and fill the well" with "Record video"
- Later steps disabled: "Place the strip", "Read the strip"

Forwarded text: "I done this part 3 times again it is not showing in app,but downloaded into mobile"

Reported account screenshot: `babureddy315@gmail.com`.

## Work Plan

- Inspect Android toxin proof recording, local persistence, outbox upload, retry, and UI read-back.
- Inspect Firebase/backend analytics event coverage for toxin proof capture/upload lifecycle.
- Inspect backend proof/media registration and serving contracts.
- Reproduce with local/throwaway environment and phone where feasible.
- Patch bug and tighten docs/analytics/tests if evidence supports it.
- Run focused tests and mobile guard.
- Use review agents/judges before PR.

## Evidence Log

- 2026-09-12 21:47 IST: Created clean worktree from `origin/main` at `/Users/raviteja/mesha/goatos-toxin-upload`.
- 2026-09-12 21:50-22:00 IST: Reauthenticated Firebase and gcloud through browser flow as `ravi@mesha.sg`, project `goatos-stg`.
- Firebase app listing works for `goatos-stg`; Android app ids include `sg.mesha.goatos` and `sg.mesha.goatos.dev`.
- BigQuery listing works for `goatos-stg`. Datasets visible: `goatos_billing_export`, `goatos_stg_analytics_rollup`.
- `goatos_stg_analytics_rollup` exists but currently has no queryable tables. Its description says it is a working dataset for the GA4-to-Postgres rollup and that the Firebase-owned GA4 export is separate.
- Cloud Logging query over the last 7 days for toxin/proof/app-analytics strings returned no matching Cloud Run entries.
- Real staging Cloud SQL was queried via Cloud SQL Auth Proxy against `goatos-stg:asia-south1:goatos-stg-core-db` on local port `15433`.
- Staging `analytics.app_events` has 608,015 rows in the last 7 days, 366,760 proof/sync rows in the last 7 days, and 68 toxin rows in the last 30 days.
- `public.workforce_members` maps `babureddy315@gmail.com` to Dinakar, `user_id=69462d72-d1ed-5558-9ffd-88173eba3451`, role `operator`.
- Dinakar's current device trail uses `device_id=9bab13f3-2dbf-442a-88ce-91ba7a412437`, app `1.0.22` / version code `72`, primary park Coimbatore.
- For toxin task `8990de43-dede-4fa1-9412-5015e25540ca` (`Dry Masoor Bhusa`, vendor `Sanchit`, farm `CBE`, purchase date `2026-09-08`), the live task is still `in_progress` with completed steps 1, 2, 3, and 5.
- Step 5 first failed at the camera layer on 2026-09-12 19:12:01 IST (`proof_camera_failed`, reason `finalize_failed`).
- A later step-5 proof on the same task was captured, processed, saved to gallery, registered, uploaded, and then the business write completed successfully at 2026-09-12 19:26:56 IST.
- Another queued/re-shot step-5 proof uploaded successfully later, but its `TOXIN_STEP_COMPLETE` write retried after step 5 was already done and terminalized at 2026-09-12 19:35:29 IST as `sync_write_dead`, reason `conflict`, with proof outbox item `9152c829-0168-4c04-9336-81bed4258d02`.
- Conclusion: for the reported step, the uploaded video was not the final blocker; stale duplicate step completion conflicted after the server already had step 5. The app bug is that toxin did not show the local uploaded/uploading/failed video proof or the completed server proof preview on the step card, so the operator saw "Record video" and reasonably repeated work.
- Existing toxin events (`toxin_step_video_captured`, `toxin_step_submitted`) only carried `step_no`; proof/sync generic events carried the useful proof/outbox/task fields. The fix adds toxin-specific durable analytics with `task_id`, `field_key`, proof id, proof outbox id, server proof id, upload status, source, preview outcome, and failure reason where available.
- Judge review found no blocking backend/sync issue for Dinakar's incident. Backend accepted the first step-5 completion and correctly rejected the later duplicate completion as `step_already_done`/conflict.
- Judge review did flag that a saved media upload can be different from an accepted toxin step business write. The fix now lets the action button attach an already-saved local proof instead of reopening camera; failed uploads still show "Record again".
- Focused Android tests passed after the judge fixes: `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ./gradlew :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.ToxinTaskDetailViewModelTest' --tests 'sg.mesha.goatos.analytics.BackendAnalyticsAdapterTest'`.
