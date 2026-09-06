# PR 206 Proof Media Progress

Last updated: 2026-09-07.

Current branch: `fix/remove-client-proof-cap`.

Current purpose:
- Remove client-wide proof caps and keep caps feature/field-owned only.
- Use common `ProofMediaPreview` for proof image/video preview with play, pause, fullscreen, share, and playback-failure analytics.
- Preserve retry/re-record durability through Room, outbox, and backend registration states.
- Seed and run phone E2E only on the maintainer-visible Android user/profile/package.

Feature surfaces wired to shared proof preview analytics:
- Feed distribution image and video proofs.
- Weighing fasting feed-removal and water-removal videos.
- Weighing shed/lumpsum videos.
- Vaccination per-animal proof videos.
- PC Care task/removal/stock image and video proofs.
- PC Care animal-slot image and video proofs.
- Counts pen reconciliation proof videos.
- Leadership attachments use shared preview UI but are intentionally excluded from proof/outbox analytics guard because they are not proof media.

Committed fixes already pushed:
- Backend/client proof policy separation and no common proof cap.
- PC Care removal per-pen rework isolation.
- Human task title guard against raw identifiers such as `feed_water_removal`.
- Shared proof preview UI and preview action analytics.
- Dev-build update prompt bypass.
- OCI throwaway seed for operations modules, vaccination, weighing, counts, feed, PC Care deworming, and fasting/removal tasks.

Latest judge finding fixed locally:
- Weighing fasting read-only server previews were not preserved across repeated Room emissions.
- Fix: keep slots with `remoteUrl` or `serverProofId` during `mergedSlot()`.
- Regression: `server backed read-only preview survives repeated Room emissions`.

Latest verification:
- `:app:testStgDebugUnitTest --tests sg.mesha.goatos.viewmodel.WeighingFastingDetailViewModelTest` passed.
- `node tools/agent-hooks/check-android-proof-preview-analytics.mjs --all` passed.
- `node tools/agent-hooks/check-android-proof-policy-default.mjs --all` passed.
- `make mobile-guard` passed.
- `make mobile-guard-audit` passed.
- Focused compile/UI/analytics Gradle run passed.

Next steps:
- Commit/rebase/push latest judge fix.
- Spin/read final judge review with one-month sync architecture lens.
- Start phone E2E against the seeded throwaway database on device `143382555G111292`, Android user `10`, package `sg.mesha.goatos.dev`.
- Capture screenshots for sidebar, fasting/removal preview, retry state, fullscreen/play/share, vaccination preview, weighing individual/shed/lumpsum, and analytics evidence.
