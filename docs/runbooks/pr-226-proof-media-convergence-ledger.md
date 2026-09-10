# PR 226 Proof Media Convergence Ledger

Status date: 2026-09-10 IST
Branch: `fix/proof-media-egress-guard`
PR: https://github.com/vgoats/goatos/pull/226

## Goal

Stop proof-media billing spikes caused by repeated post-upload preview/download loops without breaking
proof upload, proof visibility, fullscreen playback, share, retry, or fresh-login/another-phone views.

## Non-Negotiable Behavior

- Uploading proof media must continue to work.
- A completed proof must remain visible after back navigation, app restart, logout/relogin, or another
  phone loading the same task.
- A visible remote proof card must not automatically prepare, probe, stream, or refresh video bytes.
- Remote bytes should move only after explicit user intent: play, open fullscreen, or share.
- App/backend list APIs must not bulk-return fresh signed GCS URLs as durable UI state.
- Guards must cover new direct-download patterns in Android, backend, admin, docs, and review workflows.

## Root Cause In Layman Terms

Videos were already uploaded. The expensive part happened later: proof preview screens could keep asking
for the same remote video again and again. Each ask minted or reused a signed GCS URL, then the phone
streamed bytes from GCS. Repeated streaming from one or two phones can create hundreds of GB of GCS
egress even if nobody stores hundreds of GB on the phone.

## Main Fix Shape

- Android proof previews keep a stable proof identity, not a rotating signed URL identity.
- Remote cards are visible but inert until the user taps play/open/share.
- Players are stopped/released on lifecycle/fullscreen/offscreen changes.
- Backend hot list/read/export APIs return stable `/app/proofs/{id}/download` routes instead of bulk
  signed GCS URLs.
- Signed URLs are generated only inside the explicit proof download route and logged with actor/device
  attribution.
- Slack/GCP alert bridge code exists, but live Slack delivery still needs a real webhook/bot token for
  `#goatos-stg-cost-alerts` / `C0C1HLFEYAU`.

## Fixes Found During Convergence

- Shared `ProofMediaPreview` stopped hidden auto-prepare/probe/reload paths.
- PC Care proof preview identity and playback failure recovery hardened.
- Verify/vaccination/weighing/feed/counts/leadership/vendor proof routes audited and moved away from
  bulk signed URL exposure where applicable.
- Backend `/app/proofs/{id}/download` logs route issue/redirect with actor/device/proof attribution.
- Backend `GET /app/proofs/uploads?include_download_urls=true` now returns backend proof routes.
- Weighing CSV export no longer bulk-signs proof video URLs.
- Shared fullscreen close no longer re-arms inline remote playback automatically.
- Shared fullscreen playback now prepares the Media3 player after user opens fullscreen.
- Phone QA seed cleans inherited count-card rows before inserting the intended fixture.
- Phone install helper allows reinstalling PR dev build over a higher local dev versionCode.
- Vaccination scan roster now tolerates proof row creation seconds before scan-capture persistence.
- Vaccination Android proof gate now treats durable server proof id as synced proof after fresh app state.
- Verification/process-integrity shared proof-media resolver no longer calls signer methods during read
  resolution; it returns `/app/proofs/{id}/download` and uses metadata-only proof rows for labels.
- The backend guard now scans the verification proof-media adapter instead of allowlisting the whole
  directory.
- Bootstrap weighing export resolver no longer depends on `proof.Service.DownloadURL`; export links are
  backend proof routes made absolute against the API public base URL.
- Verifier photo preview resets tap-loaded/fullscreen state when a rotated signed URL arrives, so tap
  permission cannot silently survive a new remote URL.
- Admin-web explicit proof open now asks `/app/proofs/{id}/download` for JSON so Node does not follow
  the 307 to GCS and accidentally read the media body server-side.
- Backend proof download logs now include explicit `result`, `field_key`, `client_task_key`,
  `capture_source`, `screen`, and `source` when present in proof metadata.
- Milk feeding, feed packing, feed wastage, feed transport, and health treatment replacement failures
  now preserve old visible proof state instead of flipping the UI back to "no proof".
- Verify photo load failures now gate Approve the same way video playback failures do.
- Shared remote photo fullscreen loading now applies a per-call OkHttp timeout instead of relying on
  the broader proof-media client timeout.

## Evidence So Far

- Focused backend regression passed:
  `cd backend && go test ./internal/vaccinationexecution/adapters/postgres -run 'TestScanRoster(RehydratesProofWhenUploadRowPrecedesScanCapture|OneToManyPageBoundaryExecutionDateParkScopeStatusBucketsReturnsScannedAt)' -count=1`
- Android ScanViewModel suite passed:
  `cd apps/goatos-android && ./gradlew :app:testDevDebugUnitTest --tests sg.mesha.goatos.viewmodel.ScanViewModelTest --no-daemon`
- Earlier judge-reported Android guard/test pass:
  `node tools/agent-hooks/check-android-proof-media-egress.mjs --all`
  `cd apps/goatos-android && ./gradlew :core:core-ui:testDebugUnitTest --tests sg.mesha.goatos.core.ui.ProofMediaPreviewContractTest`
- Backend/admin judge at earlier head found runtime backend/admin/infra egress clean; comments were cleaned
  afterward where misleading.
- Physical phone uploaded one Gandhi vaccination proof successfully. Backend logged upload create/store/
  complete and no proof-download redirect while the preview was merely visible.
- Physical phone fresh-state pass after app data clear/reinstall: the Gandhi completed proof card remained
  visible from server proof state, did not ask for a rescan, and produced exactly one proof download route
  hit only after the user tapped play.
- Physical phone Yashoda vaccination pass found one test-harness mistake and one real UX gap:
  - Bad harness command: an unquoted `&` in the debug navigation route truncated the route before
    `taskId`, so the scan ViewModel had no task context and ignored RFID reads. Correct quoted route
    consumed `Y1-901007000504418` and opened the proof camera.
  - Real evidence: the phone recorded a vaccination proof, backend created proof
    `832dba97-5d4e-4788-836f-6a4c928a7ec1`, and backend stored two scan captures for the same goat's
    two due obligations.
  - Real UX gap still being closed: after recording stops, the visible preview/feed can appear late
    because vaccination waits for proof processing/upload enqueue before marking the local row/feed.
    The intended fix is immediate local preview from the just-recorded phone file, with submit still
    gated on Room/outbox/server sync.
- Backend/admin/infra judge pass on 2026-09-10 found and fixed one stale backend resolver/export signing
  risk, then passed:
  `cd backend && go test ./internal/proof/app ./internal/verification/adapters/proofmedia ./internal/verification/app ./internal/processintegrity/app ./internal/proof/adapters/http ./internal/weighing/adapters/postgres ./cmd/cost-alert-bridge`
  `node tools/agent-hooks/check-backend-proof-media-egress.mjs --all`
  `node tools/agent-hooks/check-admin-web-proof-media-egress.mjs --all`
  `node tools/ci/check-guardrail-registration.mjs`
- Terraform validation was not run locally because `terraform` is not installed in this shell.

## 2026-09-10 Whole-Feature Judge Round

Billing-spike/repeated-egress status by feature:

| Feature area | Surface type | Result |
| --- | --- | --- |
| Vaccination scan/proof | Shared `ProofMediaPreview` | Protected; no remote bytes on card visibility. Phone proved upload for Yashoda/Gandhi; late local preview was found and patched. |
| PC Care | Shared `ProofMediaPreview` | Protected; this was the original spike area. Stable slot/proof identities and tap-gated playback are in place. |
| Verify | Custom guarded photo/video player | Protected from hidden video egress; fixed photo Approve gating and admin explicit-open JSON handling. |
| Vaccination leadership | Custom player | Tap-gated by proof id and `LocalProofPlayerFactory`; no hidden prepare/probe finding. |
| Weighing and fasting removals | Shared `ProofMediaPreview` | Protected; backend export/list routes no longer bulk-sign proof URLs. |
| Feed distribution/packing/transport/wastage | Shared `ProofMediaPreview` where preview exists | Protected from repeated GCS egress; replacement failure state fixed for packing/wastage/transport. |
| Counts pen reconciliation / pen visits | Shared `ProofMediaPreview` | Protected from repeated GCS egress. |
| Vendor voice note / leadership attachments | Explicit tap/download path | No hidden remote prepare/probe finding; uses proof media auth/telemetry path. |
| Toxin strip photo | Local `AsyncImage` | Local-only captured image preview; no GCS egress finding. |
| Birth/death workflow videos, shifting, milk prep/feeding, health/toxin step videos | Capture-only UI | No repeated GCS egress found, but post-capture preview/action telemetry is missing or partial. This is a proof-review UX/observability gap, not evidence of the September GCS billing loop. |

Judge-found issues fixed in this round:

- Backend guard coverage now includes `verification/adapters/proofmedia/resolver.go` and
  `bootstrap/api.go`.
- Admin-web egress guard now scans `apps/admin-web/lib/api/server.ts` instead of skipping it.
- Admin explicit proof open sets `Accept: application/json`.
- Backend download attribution now includes success result and proof metadata keys.
- Replacement failure/cancel no longer hides old valid proof in milk/feed/health paths.
- Verify photo render failures now disable Approve.
- Remote photo fullscreen loader now has a bounded per-call timeout.

Known open items after this round:

- Live Slack/GCP delivery is still blocked until a real webhook/bot token for `C0C1HLFEYAU` is stored
  and Terraform/deploy is applied.
- Terraform validation still needs a machine with `terraform` installed.
- Several capture-only flows still lack post-capture preview UI and preview analytics. They are not
  currently repeated-egress risks, but they should be scheduled as a follow-up UX/observability PR.

## Final Closeout Receipt

Completed on 2026-09-10:

- Android repeated-egress guard, including self-tests and replacement-state regression check:
  `node tools/agent-hooks/check-android-proof-media-egress.mjs --self-test && node tools/agent-hooks/check-android-proof-media-egress.mjs --all`
- Backend/admin/registration guards:
  `node tools/agent-hooks/check-backend-proof-media-egress.mjs --self-test && node tools/agent-hooks/check-backend-proof-media-egress.mjs --all`
  `node tools/agent-hooks/check-admin-web-proof-media-egress.mjs --self-test && node tools/agent-hooks/check-admin-web-proof-media-egress.mjs --all`
  `node tools/ci/check-guardrail-registration.mjs`
- Backend focused tests:
  `cd backend && go test ./internal/proof/adapters/http ./internal/verification/adapters/proofmedia ./internal/weighing/adapters/postgres ./cmd/cost-alert-bridge`
- Android focused tests:
  `cd apps/goatos-android && ./gradlew :app:testDevDebugUnitTest --tests sg.mesha.goatos.viewmodel.ScanViewModelTest --tests sg.mesha.goatos.viewmodel.VerifyDetailViewModelAnalyticsTest :core:core-ui:testDebugUnitTest --tests sg.mesha.goatos.core.ui.ProofMediaPreviewContractTest --no-daemon --no-build-cache`
- Full mobile guard:
  `make mobile-guard`
- Full mobile audit:
  `make mobile-guard-audit`
- Diff whitespace check:
  `git diff --check`

Post-rebase receipt on 2026-09-10 after `git fetch origin main && git rebase origin/main`:

- Full mobile audit:
  `make mobile-guard-audit`
- Backend focused tests:
  `cd backend && go test ./internal/proof/adapters/http ./internal/verification/adapters/proofmedia ./internal/weighing/adapters/postgres ./cmd/cost-alert-bridge`
- Android focused tests:
  `cd apps/goatos-android && ./gradlew :app:testDevDebugUnitTest --tests sg.mesha.goatos.viewmodel.ScanViewModelTest --tests sg.mesha.goatos.viewmodel.VerifyDetailViewModelAnalyticsTest :core:core-ui:testDebugUnitTest --tests sg.mesha.goatos.core.ui.ProofMediaPreviewContractTest --no-daemon --no-build-cache`

Final judge sweep:

- Android egress/replacement judge: no new code blockers; residual manual E2E gaps remain for broad
  phone replay/replacement confirmation across every feature.
- Backend/admin/infra judge: no new code blockers; live Slack/GCP alerting still requires a real
  reusable Slack webhook/bot token and Terraform/deploy apply.
- Documentation/guardrail judge: found and fixed non-root runnable command wording in this ledger.

Remaining before merge/deploy:

- Push the final PR head.
- Live Slack/GCP alert delivery is not complete until the secret and Terraform/deploy steps below are
  performed.

Landing-gate follow-up on 2026-09-10:

- `make land-main` found old `stgRelease` test expectations that still assumed rotating signed proof
  URLs. Those tests now assert the intended stable backend proof route contract for feed, PC Care,
  removal pen slots, and weighing.
- `apps/admin-web` mock-fidelity found a serial-await marker issue in the explicit proof-media API
  route; the route dependency is now documented for the guard.
- `scale-guard` found verification proof metadata fanout in `resolver.go`; production now uses
  `ArtifactMetadataByIDs` so list reads batch proof-row metadata without signing, statting, opening, or
  streaming storage objects. The single-proof metadata call remains only as a legacy fallback.
- Leadership assistant coverage was updated to exclude the proof-media plumbing surfaces from CEO
  assistant coverage because they are transport and billing-attribution controls, not new leadership
  read surfaces.

## Slack Alert Live Gap

Code exists for the GoatOS cost alert bridge and Terraform wiring, but live Slack delivery is not complete
until a reusable Slack webhook/bot token for channel `C0C1HLFEYAU` is stored as the expected GCP secret
and Terraform/deploy is applied. ChatGPT connector test messages do not count as live GoatOS/GCP alerts.
