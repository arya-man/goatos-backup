# Goat OS

Goat OS is Mesha's full-stack operating system for managing goats, farm work,
proof, verification, devices, analytics, and AI-assisted decisions.

The goal is simple:

```text
No more scattered truth across Sheets, Firebase, WhatsApp, Slack, and local files.

Every goat, task, proof, health record, movement, sale, device reading, and decision
should eventually live in one governed Goat OS backend.
```

## What We Are Building

Goat OS is not just a dashboard. It is the core system of record for Mesha's
goat operations.

In short:

- Every goat gets one permanent Goat OS passport.
- RFID, old tag, breed, sex, age, status, location, ownership, and custody are
  cleaned and connected.
- Dirty records do not silently become truth; they go into review.
- Every important action creates an audit trail.
- Field work becomes assigned tasks, not loose WhatsApp/Slack messages.
- Proof photos/videos get attached to the right goat or task.
- Dashboards read from clean backend data, not giant live Sheets.
- AI can suggest, but humans approve important identity decisions.

## Current Focus

We are currently building the **Vaccination Process Integrity Slice**.

```text
Admin Config + SOP/protocol rules
        ↓
Preventive Care (PC) vaccination operations
        ↓
Vaccination execution context (park/shed scope inside /vaccination)
        ↓
Control Tower gap summary
```

The identity spine remains the foundation, but the active product build is not
the old dashboard/import-review track. The current slice asks one question:

```text
Is the vaccination process intact for each park/shed, and if not, who owns the
next action?
```

## Operational Kernel: 5k–50k Consolidation (in progress)

Per `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the runtime is
being consolidated for the current 5,000–50,000-animal release envelope: **one
modular kernel worker** instead of the ~17 scheduled Cloud Run Jobs, **zero
screen-projection tables** (screens read canonical indexed SQL), with the
1–5M-animal topology kept as a future certification bar.

Shipped to `main` (additive — the old jobs + projections still run alongside, so
nothing is switched over yet):

- `backend/cmd/kernel-worker` supervisor: crash-safe per-stage advisory lock
  (session-level, dedicated conn), panic isolation, per-cadence startup catch-up,
  HA failover test; all 11 job-stages wired into 5 cadence classes.
- Canonical indexed-SQL reads for process-integrity + vaccination
  shed/execution/operations (calendar flip in progress); FK-drop + derive of
  reminder/escalation state off the calendar projection.
- Query-plan gates proven at the ~500k obligation-row envelope upper bound.
- Docs, review lenses, and the repo router reconciled to this envelope.

Not yet done (gated / in progress):

- Dropping the 5 projection tables + converting the 3 partitioned parents, and
  removing the 18 staging jobs — these are the destructive cutover, done only
  behind an explicit go and recoverable via tag `kernel-split-workers-v1`.
- Full calendar canonical flip, clean-slate data reseed, kernel-story E2E, and
  the single-worker staging deploy.

This is infrastructure consolidation; the active *product* slice remains
Preventive Care (PC) Vaccination process integrity above.

## AI Developer Setup

Goat OS includes portable AI setup for Codex, Claude, Cursor, and other agents.
The repo commits the rules, hooks, scripts, and docs; generated graph databases
are rebuilt locally and must not be pushed.

```bash
make ai-setup
make ai-rebuild AI_BACKEND=auto
make ai-doctor
```

Use `docs/ai/README.md` for the full setup and routing guide. In short:

- CRG handles code structure questions such as callers, imports, tests, impact,
  and review context.
- Graphify handles local docs/product graph queries.
- RTK compresses noisy command output before it reaches agent context.
- `make ai-doctor` verifies the clone is portable and graph artifacts remain
  local-only.

## Android Staging Release Signing

Staging Android release builds are signed with a **staging-only** upload key.
The key material is stored in Google Secret Manager under the Mesha/VGoats
`goatos-stg` project, never in Git.

```text
Package: sg.mesha.goatos.stg
API:     https://stg-api.dashboard.mesha.sg/
Runbook: docs/mobile/stg-signed-release.md
```

Developers who need to build or upload a stg release must have Secret Manager
access to:

```text
android-stg-upload-keystore-jks
android-stg-upload-keystore-password
android-stg-upload-key-alias
android-stg-upload-key-password
```

Restore the `.jks` into the local, gitignored path `.local/android-signing/`
and export the signing passwords from Secret Manager before running
`assembleStgRelease` or Firebase App Distribution upload. Do not paste
keystores or passwords into commits, docs, Slack, tickets, or screenshots.

Use [`docs/mobile/stg-signed-release.md`](docs/mobile/stg-signed-release.md)
for the exact Secret Manager restore, signed build, Firebase App Distribution,
and post-install SSO/bootstrap verification steps.

Production release signing must use a separate production package/key/Secret
Manager set. Do not reuse the stg upload key for prod.

## Operational Kernel

Goat OS is built around a shared operational kernel. Preventive Care (PC) vaccination is the
first reference slice, but the same kernel is the required architecture for feed,
breeding, procurement, health follow-up, HR/people, farmer network, sales, and
future modules.

![Goat OS Operational Kernel](docs/assets/operational-kernel-system-design.svg)

The kernel path is:

```text
business event
  -> canonical transaction + audit + outbox
  -> event fanout + DLQ/replay
  -> trigger/rule evaluation
  -> obligation/work/batch
  -> sweeper/scheduler/Cloud Tasks
  -> SOP execution + proof
  -> verification/rework/completion
  -> notification/escalation waterfall
  -> acknowledgement/resolution
  -> read models and generated APIs
  -> admin web/mobile render backend-owned truth
```

Read the detailed system design before building any operational slice:
`context/architecture/operational-kernel-system-design.md`.

## Current Progress

### Built

The local foundation is built and tested against Postgres:

- Goat passport identity read model and contextual goat drilldown.
- RFID / old-tag identifier attach and retire flows with audit/idempotency.
- Auth/RBAC grant plumbing, local dev token helpers, and auth-session audit.
- Audit log and decision records.
- Domain events and outbox foundation.
- Local outbox relay foundation.
- Protocol definitions, protocol versions, scoped Config activation, and SOP
  proof-policy skeletons.
- Vaccination publish/backfill generation with durable run rows, due-window
  Action Center rows, verification queue, accept/reject verification, hard
  stock reservation/blocking for drive work, atomic stock movement/balance
  updates, booster scheduling, and goat
  vaccination passport aggregation.
- Canonical goat move/exit/stage events, outbox relay, Pub/Sub domain consumer,
  obligation re-scope/cancel/recheck handlers, manual campaign trigger,
  notification/incident adapters, escalation ack/resolve, kernel health, and
  Operations DLQ list/replay/discard UI with idempotent repair actions.
- Admin-web current surface: Control Tower shell, Preventive Care (PC) / Vaccination, Admin Config,
  Protocol Adherence, contextual Goat Passport, and mock-fidelity checks.
- Bootstrap bearer auth plus tenant-scope RBAC from `user_scope_grants`.
- Generated OpenAPI TypeScript client package and drift gate.
- Legacy BigQuery migration/replay tooling has been removed from the active
  runtime direction. Historical exports may still be useful as audit/reference
  material, but current product work must not rebuild a BigQuery-backed
  dashboard or import-review loop unless the scope is explicitly reopened.
- The current active admin-web direction is the connected Admin Config + Preventive Care (PC)
  Vaccination + vaccination execution context slice, with Control Tower
  summarizing process gaps instead of legacy dashboard parity.
- Local Docker storage runbook plus read-only report and guarded Goat OS temp
  volume cleanup tooling.
- Contract validation and migration validation.
- Docker-backed backend tests for core invariants.
- Dev Google Cloud bring-up through Layer 1 apply is in progress for
  `goatos-dev`: context gates are verified, required APIs are enabled, a
  dev-only budget alert exists, the Terraform state bucket is bootstrapped, and
  the foundation apply has created the non-SQL Layer 1 resources. Cloud SQL is
  being brought up in a running dev posture for raw Cloud Run dashboard
  verification. No Cloud Run services/jobs, images, migrations, or legacy
  imports are live yet.
- **Observability**: a GCP-native OTel stack is designed and its Terraform
  written for `goatos-stg` (`asia-south1`) — backend metrics/traces/logs to
  Google Managed Prometheus / Cloud Trace / Cloud Logging, a self-hosted
  Grafana pane of glass, Faro browser RUM for admin-web, Firebase
  Analytics/Performance/Crashlytics + a reserved OTLP hook for the Android app,
  a GA4→BigQuery→Postgres funnel-rollup path, and a telemetry CI guardrail
  requiring every new screen/route to wire analytics+crash+funnel signal. **Not
  yet applied**: live stg resources still need Terraform-state import before
  `terraform apply`, prod has no observability rollout at all, most Android
  funnel call sites and admin-web per-route RUM events are still TODO, and the
  GA4→BigQuery link is a manual Firebase-console step nobody has run yet. See
  `docs/observability/README.md`.

In short:

```text
The identity spine and vaccination engine foundation are built.
The active build is Admin Config + Preventive Care (PC) Vaccination + vaccination execution context.
```

### Built — Protocol & Vaccination Backend Foundation (local, tested)

On top of the identity spine, the protocol/obligation engine and the Preventive Care (PC)
vaccination module are built and green against local Postgres (full
`go test ./...`, migration, sqlc, and query-plan gates). This is **backend +
APIs only — not shipped** (see pending list below).

- Generic obligation engine: protocol definitions/versions/rules, scoped active
  ruleset selection plus DB immutability for published/active versions, and
  per-goat obligation generation (SM-1), cancel-on-exit (SM-3), shed-shift
  re-scope/repair for open and planned work (SM-2), drive sweep → batch → SOP
  task → hard FEFO stock reserve/block (SM-4), completion + verification +
  stock consume/release (SM-5), booster scheduling (SM-7), stage-change
  recheck, and deliberate manual-campaign generation. Idempotent throughout;
  outbox/local-eventbus and Pub/Sub domain-consumer paths are wired.
- Two-phase SOP verification: dose recorded at submit, verified at review; a
  task-level SOP verify/rework fans out to one vaccination outcome per recorded
  completion.
- Live impact preview: real eligible/catch-up/obligation/batch counts and doses
  required-vs-available with stock/expiry warnings (no mock math).
- HTTP APIs behind the app boundary (tenant-scoped, paginated, indexed,
  query-plan-checked): protocol config + publish, goat move/exit/stage,
  vaccination manual campaign, vaccination impact-preview, Action Center,
  Verification queue, DLQ operations, kernel health, and the Goat Passport read
  (history + next-due + last dose).

Vaccination Config must now treat the full matrix as one scoped ruleset:
company-wide active version by default, optional active park override per park,
and history for inactive/retired versions. Individual vaccines are matrix cells,
not top-level protocol rows.

### Not Finished Yet

The local foundation is strong, but the product is **not production-launch-ready
yet**.

Still pending for the current vaccination slice:

- Production identity-provider provisioning behind the JWKS mode: real IdP
  endpoint, signing keys, sessions, key rotation, revocation, and secret
  management.
- Richer vaccination execution context: park/shed/stage/defer context, owner
  chain, repair exceptions, stock-block details, and SOP/proof state around each
  vaccination drive. Deep shed detail belongs under
  `/vaccination/execution/sheds/{shedId}`.
- Richer Action Center/Control Tower rendering for kernel health, DLQ links,
  repair exceptions, manual campaign runs, and incident status from backend
  contracts.
- Reviewed Preventive Care (PC) vaccination matrix values before any production activation.
- Applying/dev-verifying Pub/Sub, Cloud Tasks, Scheduler, notification secrets,
  FCM/email/Slack/webhook/incident channels, and continuously running workers in
  the target Google projects.
- Applying the approved `goatos-dev` Layer 1 foundation Terraform plan, then
  pushing images, populating secrets out-of-band, deploying Cloud Run
  services/jobs, running migrations, and importing real legacy data under
  explicit approval gates.
- Deployment/provisioning of shared/staging/prod under `vgoats.com`.

Intentionally not active now:

- Old Import Review, conflict/candidate queues, correction queues, counts,
  mortality, legacy sync, and BigQuery-backed dashboard parity.
- Generic Parks dashboard pages unrelated to vaccination execution.
- Generic all-domain Action Center or Control Tower products before the
  operating Admin / Preventive Care (PC) / Parks / Procurement module surfaces are real.

Immediate next order:

```text
1. Build vaccination execution context (park/shed/stage/defer/blocker/owner)
   around the Preventive Care (PC) vaccination rows, rendered inside /vaccination.
2. Keep the top-level Action Center work-state lens fed by Preventive Care (PC) / vaccination without
   turning it into a generic all-domain product yet.
3. Provision production auth (IdP/JWKS + secrets), event egress, and cloud
   deploy under vgoats.com without calling the local dev-token demo shippable.
```

Important:

```text
Local identity spine is built.
User-facing product is not complete yet.
```

## Local Dev Start

Use this command for day-to-day local UI work:

```bash
make dev-local
```

It starts or reuses the local API on `127.0.0.1:8080`, seeds the local
`ceo_internal` grant idempotently, mints a fresh server-side bearer token, and
starts Mesha admin-web on `127.0.0.1:3300`. If the browser shows
`401 invalid_bearer_token`, restart through this command instead of reusing an
old shell token.

## Leadership Assistant — Local & Staging Setup

The Leadership Assistant ("CEO AI") is a read-only, tenant-scoped natural-language
layer over Goat OS. **Status: local-runnable foundation; the backend service,
Cube metric layer, and MCP Toolbox are being built, and the staging read-only DB
roles are a pending deploy step.** Do not read this as "shipped" — it is the
config/secrets on-ramp so any developer can run the pieces that exist.

### Routing model (one paragraph)

Every leadership question is planned server-side by **Vertex/Gemini**
(`gemini-2.5-flash`, project `goatos-stg`, region `asia-south1`, authenticated
via ADC — no key in env). The planner never touches the database; it only picks a
read path in a governed hierarchy: **(1) Cube** — the governed metric layer, for
official KPIs (active animals, vaccination due/overdue, compliance, mortality,
feed/procurement cost); **(2) Mesha read APIs**; **(3) MCP Toolbox** curated
`ceo_ai.*` views; **(4) validated read-only SQL** only when nothing else fits.
Cube and Toolbox are called **only by the backend**, never by the browser. All
credentials come from Google Secret Manager; the assistant is read-only and
derives tenant + role from the server session, never from user text.

### Prerequisites

```bash
gcloud auth login ravi@mesha.sg          # CLI creds (Mesha/VGoats org)
gcloud auth application-default login    # ADC for Vertex + Secret Manager
gcloud config set project goatos-stg
docker info >/dev/null                   # Cube + Toolbox + local Postgres run in Docker
```

### Fetch config/secrets

Credential VALUES live in Google Secret Manager (`goatos-stg`); config is plain.
Pull them into a gitignored `.env.ceo-ai.local`:

```bash
tools/dev/fetch-ceo-ai-secrets.sh        # pulls Cube API secret + toolset from Secret Manager
```

For a purely-local stack (no staging access), mint throwaway read-only roles
instead:

```bash
tools/dev/setup-ceo-ai-local-role.sh     # local mesha_ceo_readonly / mesha_cube_readonly + DSNs
```

Copy `.env.ceo-ai.local.example` if you prefer to fill values by hand. Full
details, secret names, rotation, and the pending-staging note live in
[`docs/runbooks/leadership-assistant-secrets.md`](docs/runbooks/leadership-assistant-secrets.md).

### Start the local stack

```bash
# 1. Postgres (the canonical local DB on 127.0.0.1:5433) + migrations + seed
make dev-local                              # API :8080, admin-web :3300, local ceo_internal grant
# 2. Cube Core governed metric layer
tools/dev/run-cube-local.sh                 # :4000  (MESHA_CUBE_URL)
# 3. MCP Toolbox curated ceo_ai.* tools
tools/dev/run-mcp-toolbox-local.sh          # :5001  (MESHA_MCP_TOOLBOX_URL)
```

The backend (`:8080`) is the only process that calls Vertex, Cube (`:4000`), and
Toolbox (`:5001`); admin-web (`:3300`) renders the assistant contract. Health
checks: `run-cube-local.sh status` and `run-mcp-toolbox-local.sh` (no-ops if
already healthy).

### Environment variables

| Variable | Purpose | Secret vs config | Source |
| --- | --- | --- | --- |
| `MESHA_AI_PROVIDER` | AI provider (`vertex`) | config | env / example |
| `MESHA_VERTEX_PROJECT` | Vertex project (`goatos-stg`) | config | env / example |
| `MESHA_VERTEX_LOCATION` | Vertex region (`asia-south1`) | config | env / example |
| `MESHA_VERTEX_MODEL` | Gemini model (`gemini-2.5-flash`) | config | env / example |
| `MESHA_AI_MAX_STEPS` | bounded agent step loop | config | env / example |
| `MESHA_AI_REVIEW` | enable self-review pass | config | env / example |
| `MESHA_CUBE_URL` | Cube endpoint (local `127.0.0.1:4000`) | config | env / example |
| `MESHA_CUBE_API_SECRET` | Cube JWT signing secret | **secret** | Secret Manager `mesha-cube-api-secret` |
| `MESHA_CUBE_DB_DSN` / `MESHA_CUBE_DB_*` | Cube's readonly Postgres DSN | **secret** | Secret Manager `mesha-cube-readonly-db-url` |
| `MESHA_MCP_TOOLBOX_URL` | Toolbox endpoint (local `127.0.0.1:5001`) | config | env / example |
| `MESHA_MCP_TOOLSET` | curated toolset name | config | Secret Manager `mesha-mcp-toolset` |
| `MESHA_MCP_DB_DSN` / `MESHA_MCP_DB_*` | Toolbox/SQL-fallback readonly DSN | **secret** | Secret Manager `mesha-ceo-readonly-db-url` |

Vertex uses ADC — there is no Vertex API key in env.

### Staging deploy pointers

Staging is Cloud Run (org `vgoats.com`, project `goatos-stg`): **`mesha-cube-stg`**
(Cube Core, internal-only) and **`mesha-mcp-toolbox-stg`** (MCP Toolbox,
internal-only), each reading its credentials from Secret Manager with a
least-privilege accessor grant. The live staging Cloud SQL read-only roles
(`mesha_ceo_readonly`, `mesha_cube_readonly`) and their DSNs are **still to be
provisioned** — the DB-url secrets currently hold clearly-marked placeholders.
Seed / rotate the Secret Manager entries with
`tools/dev/setup-ceo-ai-secrets.sh` (org-guarded to `ravi@mesha.sg` /
`goatos-stg` / `vgoats.com`).

## Pushing To The Repo (Git Auth)

This repo lives at `github.com/vgoats/goatos` under the Mesha/VGoats org. Push
with a personal access token in an env var, not whatever `gh` account happens to
be logged in (a machine may also carry unrelated org accounts).

1. Create your own GitHub PAT with write access to `vgoats/goatos`
   (fine-grained: repo contents read/write on `vgoats/goatos`; or a classic
   token with `repo` scope). Each dev uses their own token — never share one.
2. Export it from your shell profile (`~/.zshrc` / `~/.bashrc`) so it is present
   in interactive shells. Never commit or echo the token value.

   ```bash
   export MESHA_GITHUB_PAT="<your-token>"
   ```

3. Add the `mesha-push` git alias once (injects the token at push time and
   accepts the explicit refspec used by `make land-main`):

   ```bash
   git config --global alias.mesha-push '!f() { \
     url="https://x-access-token:${MESHA_GITHUB_PAT}@github.com/vgoats/goatos.git"; \
     git push "$url" "${1:-HEAD:main}"; }; f'
   ```

4. Land a committed change on fresh `main` (the required Codex/Claude path):

   ```bash
   make land-main
   ```

   This single command fetches and rebases onto current `origin/main`, runs
   `make ci-local` on the rebased SHA, checks main again, and only then invokes
   the `mesha-push` credential path. It refuses dirty worktrees; use a clean
   isolated worktree when other work is in progress.

If `MESHA_GITHUB_PAT` is unset the alias fails fast. The plain `origin` URL will
404/401 without the token — that is expected. `make land-main` calls the
`git mesha-push HEAD:main` alias after the rebase and CI gates pass.

## Local Development Storage

Daily Goat OS development runs locally with Docker Postgres, tests, and small
synthetic data. GCP is not required for normal coding. Future `goatos-dev`
Cloud SQL is for explicit cloud rehearsal, and future `goatos-stg` Cloud SQL is
where the persistent 1M-goat benchmark belongs. Local 1M tests must be
temporary: create an explicit temp volume, run the test, export the summary, and
delete the temp volume.

Use the local Docker storage runbook before large local tests:

```text
docs/runbooks/local-docker-storage.md
```

Use the local full-stack rehearsal runbook before touching real RFID exports:

```text
docs/runbooks/local-full-stack-rehearsal.md
```

On Docker Desktop for Mac, deleting Docker containers/images/volumes frees space
inside Docker's Linux VM first. The host-side `Docker.raw` or VM disk image may
still need Docker Desktop reclaim/reset workflow before macOS shows the space as
available.

Later Goat OS cloud defaults are Mumbai-first (`asia-south1`), never US by
default.

## What "Without Sheets Or Firebase" Means

Sheets and existing files can still be used as **input sources** during
migration.

But they should not remain the long-term truth.

The target architecture is:

```text
Old Sheets / XLSX / Firebase / Slack records
        ↓
Controlled import and review
        ↓
Postgres Goat OS backend
        ↓
APIs, frontend, mobile, dashboards, analytics, devices
```

So the system can still read old data, but the clean official truth should live
in Goat OS.

## Execution Roadmap

The old phase ladder is no longer the active build plan. The current roadmap is
ordered around the vaccination process-integrity slice:

```text
1. Admin Config / SOP Policy
   Scoped protocol rulesets, proof policy, version activation, park override
   resolution, and impact preview.

2. Preventive Care (PC) Vaccination Operations
   Vaccination obligations, drives, proof, verification, missed/deferred
   handling, and goat vaccination passport history.

3. Vaccination Execution Context
   Park, shed, animal stage, defer status, blocker, owner chain, SOP/proof
   status, and verification status around each vaccination drive (rendered
   inside /vaccination; deep shed detail belongs under
   /vaccination/execution/sheds/{shed_id}).

4. Action Center Work-State Model
   Due, overdue, blocked, proof-pending, verification-pending, rejected,
   deferred, blocked, and completed/recent states exposed from real
   workflow sources.

5. Control Tower Summary
   Process intact/not intact, where, severity, owner, and next action. Control
   Tower summarizes broken or at-risk vaccination process only; it is not a
   generic KPI dashboard.

6. Production Hardening
   Real IdP/JWKS auth, Pub/Sub event egress, Cloud Run deploy, observability,
   query-plan gates, and reviewed Preventive Care (PC) rule matrix values before
   production activation.
```

Later verticals such as feed direction, health, breeding, procurement, sales,
devices, analytics, and AI analyst remain real Goat OS scope, but they should
not pull the current build away from Admin Config + Preventive Care (PC) Vaccination + vaccination
execution context.

## What The CEO Should Know Today

Current status:

```text
We are not just making screens.
We are building the operating workflow first.
```

The current build is focused on the vaccination process-integrity layer:

- Admin Config and scoped vaccination rulesets.
- Preventive Care (PC) vaccination obligations, drives, proof, and verification.
- Vaccination execution context: park, shed, stage, defer state, owner, blocker
  (rendered inside /vaccination; deep shed detail belongs under
  /vaccination/execution/sheds/{shed_id}).
- Action Center work states underneath the operating screens.
- Control Tower summary only after the underlying gaps are real.

This is the right order because Control Tower should summarize real operating
gaps, not decorate incomplete workflows.

When the Google dev deployment is shown, be clear that deployment proves the
tested vaccination kernel is running in Google. It does not automatically mean
every future operating workflow is live. ICU/quarantine guardrails,
shed-owner separation checks, full failed-message replay operations, and the
stock-owner resolution workflow still need complete business workflows before
they should be presented as finished CEO-facing processes.

## Immediate Next Steps

Recommended next execution order:

1. Build vaccination execution context (park/shed/stage/defer/blocker/owner)
   inside /vaccination around the current Preventive Care (PC) vaccination rows, with shed detail
   at /vaccination/execution/sheds/{shed_id}.
2. Finish the top-level Action Center work-state backend model without
   introducing a generic all-domain product yet.
3. Keep Admin Config category-driven while wiring vaccination SOP/proof policy
   from the active scoped vaccination matrix version.
4. Add production identity-provider integration later: JWKS/asymmetric token
   verification, login/session handling, key rotation, revocation, and secret
   management.
5. Prepare production deploy/event work separately: cloud deployment and event
   egress.

## Build Principle

Goat OS follows one rule:

```text
If the fact matters, it must be traceable, reviewable, and stored in the backend.
```

That means:

- No silent merges.
- No hidden spreadsheet truth.
- No AI auto-approving canonical facts.
- No dashboard counts from raw giant table scans.
- No production decisions from unverified dirty data.

## Engineering Status

Backend stack:

```text
Go backend
Postgres database
OpenAPI contracts
sqlc typed SQL
goose-style SQL migrations
explicit wiring
no ORM
no Firebase as source of truth
```

Current validation includes:

```text
backend tests
contract drift checks
migration validation
sqlc generation checks
query plan checks
Docker-backed Postgres integration tests
```

## One-Line Summary

Goat OS is now focused on the vaccination process-integrity slice.

The identity and vaccination foundations are strong, but the product is not
user-ready until vaccination execution context, the full work-state model, production
auth, event egress, and deployment are complete.
