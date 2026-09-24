# Local Stack, Ports and Local DB Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## Legacy Local Stack Canonical Ports

For local Goat OS browser/debug work, use one shared local stack unless the user
explicitly asks for an isolated throwaway stack:

```text
Frontend: http://127.0.0.1:3300
Backend:  http://127.0.0.1:8080
Database: postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable
Docker DB container: goatos-local-current
```

This `5433` Docker DB setup is legacy/local-only on Ravi's machine and is not
the default when the OCI tunnel is active.

Before cloning, seeding, importing, or debugging local data, first verify the
running backend's `DATABASE_URL`. On Ravi's laptop, prefer the OCI tunnel above;
use the legacy `5433` Docker DB only after an explicit Docker/local DB request.
Do not infer the local DB from a previous temp worktree, a random Docker port,
or a stale shell variable. If a temp stack is unavoidable, clearly label it as
throwaway and do not call it "the local DB".

When the user says "my local DB" or "local frontend/backend", treat that as:

```text
Chrome -> 127.0.0.1:3300 -> 127.0.0.1:8080 -> 127.0.0.1:5433/goatos
```

For physical Android phone scan/RBAC testing, do not mutate the normal local DB.
Use the reset-first throwaway database and runbook:

```text
Database: postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable
Docker DB container: goatos-phone-qa
Runbook: docs/runbooks/phone-qa-throwaway-rbac.md
```

- After any destructive seed, bulk import, fixture reset, or large canonical
  backfill, refresh Postgres planner statistics for the touched canonical
  tables before projector recompute or latency gates. The normal source seed
  must `ANALYZE` the freshly loaded location, HRMS, goat, protocol, obligation,
  event, and vaccination-completion tables after commit and before read-model
  projection. This prevents projection timeouts caused by stale empty-table
  planner estimates.
- Do not start local, staging, or production app code against a database that is
  behind that build's migrations. Apply migrations first, seed only canonical
  source truth second, run deterministic closeout/projectors third, then start
  API/admin/workers or mark the environment green.
- Do not serve the normal local API/admin-web from temporary worktrees under
  `/tmp`, `/private/tmp`, or `/var/folders`. Local stack wrappers must fail by
  default there so the browser cannot silently exercise a disposable checkout
  while the canonical repo is stale or dirty. Use
  `GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK=1` only for explicit throwaway
  experiments, never for handoff.
- Normal local laptop runtime must resolve exactly one Goat OS app database for
  API, admin-web, and mobile. Use the single detected `goatos-local-current`
  Docker DB or the `127.0.0.1:5433/goatos` fallback; if multiple Goat OS app
  Postgres containers are running, local launchers must fail instead of
  guessing. E2E/proof/load scripts must fail closed unless
  `GOATOS_E2E_DATABASE_URL` or `DATABASE_URL` is explicitly passed. Read-only
  E2E checks may target the normal `5433` app DB, but mutating proof/load
  scripts that create goats/proofs, replay outbox, insert history, or run
  migrations must always refuse `5433`. There is no override for mutating the
  normal app DB from E2E. Destructive/load tests must use an isolated DB with
  its own seed/cleanup, such as the explicit local GCP-kernel stack on `55432`;
  that stack must never become the default laptop runtime DB.

- Shared local-stack identity and isolation (Codex and Claude): the browser-visible
  stack is exactly one atomic trio — admin-web `127.0.0.1:3300`, API
  `127.0.0.1:8080`, and database `goatos` in the named `goatos-local-current`
  container. FE and BE must run from the same clean checkout at **exact origin/main**.
  Start/recover it only through the persistent service wrapper;
  the supervisor owns both ports, discards ambient `DATABASE_URL` /
  `GOATOS_E2E_DATABASE_URL`, includes the `libpq` tools required by closeout,
  pins local query headroom so the canonical Calendar read cannot false-fail at
  the production-oriented 3-second deadline during local build load, and must
  verify an authenticated Calendar data-plane read in addition to `/readyz`.
  It must stop/restart FE+BE together if either child fails or `origin/main`
  advances. Never point the shared UI at a feature-worktree API or an alternate
  database, accept `/readyz` alone as proof that page data works, or start only
  half the shared pair.
- An **isolated E2E** stack is a separate test appliance with its own non-shared
  FE/BE ports and explicit throwaway database. It is intentionally independent
  of the shared exact-main stack. Shared-stack recovery must never stop, reuse,
  migrate, seed, fast-forward, or delete an isolated E2E process/container/DB.
  Conversely, E2E scripts must never claim `3300`, `8080`, or mutate the normal
  `5433/goatos` database. Inspect exact port owners and database targets before
  cleanup; do not infer that every local Goat OS process belongs to the shared
  stack. Enforcement: `make local-stack-service-guard`, required by normal
  `make ci-local`; operating contract:
  `docs/runbooks/local-full-stack-rehearsal.md`.
- Local dev servers (`:3300` admin-web, `:8080` backend): the workspace owner has
  granted agents (Codex and Claude) STANDING authority to stop, restart, re-port,
  or `next build` over them WITHOUT asking — just do it when the work needs it
  (clean build, or an expired local token making routes redirect to `/login`).
  Admin-web must be restarted through the local wrapper:
  `npm --prefix apps/admin-web run dev` / `dev:local`, including custom isolated
  ports like `npm --prefix apps/admin-web run dev -- --port 3318`. Plain
  `next dev` is forbidden because it bypasses `GOATOS_AUTH_*` env and makes
  `/admin-web/bootstrap` fail with `invalid_bearer_token`. Do not pause to ask
  permission for a restart/rebuild. The only
  discipline: restore the server on the SAME port, never silently change ports,
  don't run `next build` concurrently with a live `next dev` on the same `.next`
  (stop it first), and if you break it, restore it. See
  `apps/admin-web/AGENTS.md` → "Local Dev Server Safety" for the full rule. This
  applies to every agent (Codex and Claude).
- Always-on local stack rule (Codex and Claude): when a task needs any local
  frontend, backend, worker, importer, proxy, emulator, database container, or
  other Goat OS service, first check whether it is already running and do not
  stop it just because the immediate command is done. Prefer the persistent
  service wrapper (`make dev-local-service-start`, `make dev-local-service-status`,
  `make dev-local-service-logs`) over foreground one-off terminals for long-lived
  stack work. Leave required services running at the end of the turn/session
  unless the user explicitly asks to stop them or stopping is required to prevent
  machine damage/data loss. If code/env changes require a restart, restart on the
  same ports and health-check before reporting done. Do not finish with a needed
  app stack stopped, and do not leave required servers as active Codex terminal
  sessions that block the final response; use the service wrapper/supervisor and
  logs. Status/final updates must name what is running plus the URL/port. If a
  service cannot be kept running, state the blocker and the exact restore command.

> The HARD RULES (no circular OCI/E2E retries, UI real-surface proof, ADB literal text, UI visual regression + E2E, admin-web must render on a phone, Weights Chrome verification) moved to core `AGENTS.md` because they apply to most UI/E2E work.

## OCI Clone Sync Is Delta-Only (2026-09-24)

Never refresh the OCI clone (or any environment) with a full `pg_dump` of stg:
it cost 13-16GB/day of Cloud SQL egress. Use the delta-only sync: per-table
watermark (`updated_at` or PK), junk tables excluded (`analytics.app_events`,
`outbox_messages`, `audit_log`, delivered `notification_*`), compressed
transfer. Details: `docs/perf/2026-09-24-stg-latency/README.md` "OCI sync"; catalog `.agents/skills/scale-anti-patterns/SKILL.md` ("STG latency catalog", P1-P25) P8. The clone at
`127.0.0.1:15432` is read-only for audits; write only to a throwaway DB.
