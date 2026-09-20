# PR 325 review fixes — 2026-09-21

## Scope and state

Base SHA: `07798da85da032148bd07dc66e85c6174c435f1a`. The commit containing this document is the reviewed fix candidate; see `git log -1` for its SHA. Target: `feat/procurement-sop-driven` / PR #325 only. No main merge or staging deployment.

Done:
- Migration 000377 expands toxin completion step numbers from 1..7 to 1..20, matching the authoring validator. Down refuses to discard evidence from longer rounds.
- Web submissions preserve manual batch numbers as ledger columns while sending only the typed questions present in the rendered form version. Tests include an authored batch question and a retired typed question.
- Responsive smoke now opens the real toxin List and Flow editors, selects a step, and checks all node titles for clipping.
- Fixed the reported Start title clipping with taller toxin nodes and non-shrinking titles; enlarged mobile insert buttons to 40px. Scroll-container clipping is included in overlap geometry, so off-canvas nodes do not falsely overlap the properties panel.
- Fixed two existing PR copy-contract violations and two existing gofmt failures exposed by required gates.

## Exact verification

- `GOATOS_RUN_POSTGRES_TESTS=1 GOATOS_PGTEST_ADMIN_DSN=<local Postgres admin DSN> go test ./internal/toxin/adapters/postgres -run 'TestAuthoredTwentyStepRoundPostgres|TestToxinTaskLifecyclePostgresPaths' -count=1 -v`: PASS on local Postgres 15499, isolated harness databases. New test completes 19 video steps plus the reading at step 20, checks pending review and all 20 saved completions, and rejects step 0/21. Existing seven-step lifecycle passes.
- `go test ./internal/procurement/... ./internal/toxin/... ./internal/tasks/... ./internal/sop/... ./migrations/postgres`: PASS (ordinary run skips opt-in database tests).
- `go test ./internal/adminui/app ./internal/health/adapters/postgres ./internal/vaccination/adapters/http`: PASS.
- Node 24 `node --test` over feed-purchase-form-answers, toxin-model, sop-card-counts, and smoke-visual-route-coverage tests: 23 PASS.
- `npm --prefix apps/admin-web run typecheck`: PASS.
- `npm --prefix apps/admin-web run check:mock-fidelity`: PASS.
- `node tools/agent-hooks/check-procurement-sop-guard.mjs --all`: PASS.
- `GOATOS_SMOKE_ONLY_ROUTES=procurement-toxin-list,procurement-toxin-flow npm --prefix apps/admin-web run responsive:guard`: PASS. List and Flow at laptop/mobile, selected-step properties, title bounds, layout, accessibility, and exact failure-string checks. Its additional Sales tolerance check passed widths 390, 1081, 1280, 1366, 1440 and 1920.
- Real Playwright feed-purchase submissions at 1440px and 390px: success feedback `action.purchase_recorded`; database readback confirms manual batches 100440 and 99390, questionnaire version 1. Only the fresh local database was written.
- Final screenshot directory: `.codex-goatos-render/admin-web-screenshots/2026-09-20T20-05-57-176Z`. Laptop/mobile Flow screenshots visually inspected after the title fix; the full Start title is visible on both. Earlier List screenshots also inspected. An immediate post-submit purchase screenshot caught the closing transition and was not used as visual proof.
- `git diff --check`: PASS.

## Failures and limitations

`make guardrails` is NOT green: after repairing its gofmt findings, the aggregate run reached `local-gcp-kernel-parity-guard` and stopped because `docker` is absent (`check-local-gcp-kernel-parity.sh:58: docker: command not found`). Full `make ci-local` was not run. This is a PR-branch update, not a landing receipt.

First database test fixture attempted a second published version; corrected the fixture to retire v1 first, then both lifecycle tests passed. First responsive runs exposed the undersized insert buttons, clipped-canvas overlap false positives, and the user-reported clipped title; final responsive run passes after fixes.

Before/after: a valid manual batch answer was rejected before and saves through the real UI now; valid steps above seven were rejected by storage before and all twenty persist now; Start title was vertically clipped before and its rendered glyph bounds fit now. This is not a performance change; no latency improvement is claimed.

Judge: self-review of diff, runtime outputs and screenshots complete. No delegated judges requested.

Local test stack remains available: admin-web `http://127.0.0.1:3315`, API `http://127.0.0.1:18095`, fresh database `goatos_pr325_fix` on local Postgres 15499. No shared stack, OCI or STG data was mutated.

Pending: commit and push the validated candidate to PR #325, then verify remote head. Deployment state: not deployed.
