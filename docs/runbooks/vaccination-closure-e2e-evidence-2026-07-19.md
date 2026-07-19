# Vaccination closure E2E evidence — 2026-07-19

Pushed SHA: `1d39d21cd9a0ac41ea2557075fb600216b4bf34b`

Scope: operator → verifier → leadership vaccination closure path, using local/isolated infrastructure only. No production database, GCS bucket, or cloud runtime was touched.

## Role flow proven

- Operator opens vaccination work, scans/taps the goat row, records camera-only proof on that goat row, and submits already-synced goat proof.
- Proof upload is per goat, not per vaccine. One clear camera clip can cover all vaccines administered to that goat in the same handling; multiple clips per goat are allowed.
- The proof API stores local object-storage metadata in dev and keeps the GCS abstraction boundary for production.
- Verifier queue receives the proof-backed vaccination item and accepts it.
- Accepted verification preserves the operator `administered_at` as the medical vaccination date.
- Completion closes the obligation, publishes `vaccination.completed`, updates read models, and remains idempotent on replay.
- Leadership close UI is present as the drive-level operational approval surface.

## Local isolated backend proof

Command run against throwaway local API/Postgres:

```bash
GOATOS_API_BASE_URL=http://127.0.0.1:60191 \
GOATOS_E2E_DATABASE_URL='postgres://postgres:goatos@127.0.0.1:60190/goatos?sslmode=disable' \
DATABASE_URL='postgres://postgres:goatos@127.0.0.1:60190/goatos?sslmode=disable' \
GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 \
GOATOS_AUTH_HS256_SECRET='goatos-local-dev-secret-32-bytes-min' \
GOATOS_KEEP_VACCINATION_PROOF_FIXTURES=1 \
bash tools/dev/vaccination-chain-proof.sh
```

Result: PASS.

Representative terminal proof:

```text
PROOF goat=67afd0d0-6cc7-4af4-9f08-250e878fbe48 clip1=c9dca92e-1b6d-48cd-ab55-fa9a4512b1c9
verification-queue contains completion: HIT
vaccination.completed delivery: vaccination.completed published
Shed drilldown (API):      batch-drive workState=completed
Control Tower (API+SQL):  verification_backlog=0
Operations (API):         my-shed accepted=1
completions for goat after replay (expect 1): 1
## CLOSED goat=67afd0d0-6cc7-4af4-9f08-250e878fbe48
```

## Guard and CI proof

Pushed through the repo landing path:

```bash
make land-main
```

Result: PASS and pushed `1d39d21cd9a0ac41ea2557075fb600216b4bf34b` to `main`.

Additional focused checks run before landing:

```bash
node tools/agent-hooks/check-android-ui-copy-layout.mjs --self-test
node tools/agent-hooks/check-android-ui-copy-layout.mjs
GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 docker compose -f compose.local-kernel.yml config
bash -n tools/dev/vaccination-chain-proof.sh
GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 make mobile-guard android-bounded-memory-guard guardrail-registration-guard
bash backend/tests/integration/validate-postgres-migrations.sh
```

Result: PASS.

Focused screenshot check:

```bash
cd apps/goatos-android
./gradlew :app:verifyPaparazziDevDebug \
  --tests 'sg.mesha.goatos.ui.ScreenshotTest.vaccination_*' \
  --tests 'sg.mesha.goatos.ui.ScreenshotTest.calendar_coverage_banner'
```

Result: PASS.

## Screenshot evidence

- Operator scan/goat proof: `apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_vaccination_scan_row_proof_vaccination_scan_row_proof.png`
- Operator shed review/finalize: `apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_vaccination_shed_review_vaccination_shed_review.png`
- Verifier queue: `apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_vaccination_verifier_queue_vaccination_verifier_queue.png`
- Leadership close: `apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_vaccination_leadership_close_vaccination_leadership_close.png`
- Calendar coverage banner/date chips: `apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_calendar_coverage_banner_calendar_coverage_banner.png`

## UI/UX guard update

The mobile UI guard is app-wide, not vaccination-specific. It now blocks:

- internal implementation labels/copy on production UI,
- UUID/debug-like leakage,
- absolute/negative alignment offsets,
- one-letter date labels,
- direct state-dependent geometry in modifiers,
- variable-computed state-dependent height/width/size/padding/spacing/inset/offset,
- multiline state-dependent geometry.

This is meant to catch future “selected card/chip/button has different block height than siblings” regressions across all mobile screens.

## Remaining manual follow-up

The automated proof covers the backend state transitions and the committed screenshot states. A human emulator walkthrough can still be run for touch-by-touch UX review on physical-device dimensions, but the pushed automated gates and local isolated chain are green for this slice.
