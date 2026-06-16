# Phase 1 Closeout Scope

Status: closeout decisions locked.

This file is the single source of truth for what "finish Phase 1" means. It
exists so no future session has to infer Phase 1 in/out scope from chat memory.
Every item below is classified as one of:

- **implement** — build it now as local Phase 1 code.
- **verify** — prove it is already done; no behavior change.
- **reclassify** — keep it intentionally blocked/deferred and make docs + UI/API
  copy agree that it is not unfinished Phase 1 code.

"Local Phase 1" is the internal identity/import/read/admin-review spine that runs
against local Postgres with bootstrap auth. "Production Phase 1 launch" adds real
IdP/JWKS provisioning, secrets, event egress to a real broker, and cloud deploy.

## Decisions

| Item | Subject | Decision | Rationale |
| --- | --- | --- | --- |
| A | Candidate approve (attach / merge) | **implement** | Listed as Phase 1B-required. Non-create outcomes reuse existing identifier-attach and merge invariants. Approve-to-create stays blocked on conflict `create_goat`. |
| B | Import Review row actions (reject / fix / re-apply) | **implement** | Listed as Phase 1B-required. Needs a new migration for `legacy_import_rows.row_version` + row-action audit. Re-apply reuses the existing RFID apply path. New-goat-from-row stays blocked on conflict `create_goat`. |
| C | Data Quality queue proof | **verify** | 232 open conflicts are all `status_mismatch` BQ review workload (ambiguous sex/breed/lifecycle), not missing code. No auto-resolve. Confirm bulk actions are current-page-only and non-actionable rows cannot be selected. Match Candidates UI WIP validated and preserved. |
| D | Conflict `create_goat` | **reclassify** | No product-approved operator-entered new-goat field set exists (confirmed across PRD/TRD/BUILD-STATUS). Stays typed-blocked and is removed from Phase 1 required work. Not a Phase 1 code blocker. |
| E | Legacy Sync real executor | **reclassify** | Docs define Legacy Sync v1 as the control/status surface; the real configured executor is a production sync follow-up that needs the production-safe scheduled-query inventory + config. Honest "Not Synced / Blocked" is correct when no watermark exists. No fake watermarks. |
| F | `POST /admin/import-runs`, `POST /admin/goats`, `PATCH /admin/goats/{goat_id}` | **reclassify** | Phase 2+/admin production workflow. Current import is the CLI/apply path; manual admin goat create/update requires the same approved duplicate-check + required-field set as conflict `create_goat`. Not required for local Phase 1 identity closeout. |
| G | Production auth foundation | **implement** | Provider-agnostic JWKS / asymmetric (RS256/ES256) bearer verification mode behind the existing `TokenVerifier` port, with issuer/audience/kid/alg validation, key cache/refresh, and exp/nbf/skew handling. HS256 dev mode stays local-only. Required env/secrets documented. Not wired to Heva/Slice or any specific IdP. |
| H | Production event egress (Pub/Sub) | **reclassify** | The outbox `Publisher` port plus the local fake/logging publisher with retry/DLQ semantics is the complete local path. The Google Pub/Sub adapter + worker deploy is production-readiness follow-up with a documented integration contract; the heavy cloud SDK is not pulled in until provisioning is real and CI-fakeable wiring is needed. |
| I | Shared/staging/prod deploy readiness | **implement (docs/config)** | Commit deploy runbook + environment config scaffold for `goatos-dev/stg/prod` under `vgoats.com`. No cloud command is run from this workspace; external provisioning (project/IAM/Cloud SQL/secret creation) is explicitly marked blocked on operator action in the correct org. |

## Final Phase 1 status target

- **Local Phase 1: complete.** Identity/import/read/admin-review spine, candidate
  approve (attach/merge), Import Review row actions, and a provider-agnostic
  production-auth verification mode are implemented and tested against local
  Postgres.
- **Production Phase 1 launch-ready: no.** Remaining work is genuinely external
  launch work, not unfinished Phase 1 code:
  - real IdP/JWKS endpoint + signing keys + secret management provisioning,
  - Pub/Sub (or chosen broker) event egress adapter + worker deploy,
  - cloud deploy/provisioning of `goatos-dev/stg/prod` under `vgoats.com`.

## Deliberately-blocked surfaces (not Phase 1 code gaps)

- Conflict `create_goat` and approve-to-create: blocked until the operator-entered
  goat creation field set is product-approved (item D).
- `POST /admin/import-runs`, `POST /admin/goats`, `PATCH /admin/goats/{goat_id}`:
  deferred to Phase 2+/admin production workflow (item F).
- Legacy Sync execute/nightly: blocked until the production-safe executor + live
  scheduled-query inventory are configured (item E).
