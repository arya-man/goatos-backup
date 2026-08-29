# Deployment Runbook — goatos-dev / goatos-stg / goatos-prod

Status: deploy procedure + config are committed here. Actual cloud provisioning
and deploy execution are **external operator actions** and are blocked from the
build workspace (see "What is blocked" below). This runbook is the contract for
how a Goat OS backend release reaches a shared/staging/production environment in
the correct organization.

Read the org boundary first: `docs/runbooks/google-cloud-environments.md`.

## Current production-facing naming decision

The current public/operator-facing cleanup reuses the existing `goatos-stg`
Google/Firebase project internally, but public surfaces must use production
names:

```text
Android package: sg.mesha.goatos
Dashboard:       https://dashboard.mesha.sg
API:             https://api.goatos.mesha.sg/
Firebase Auth:   goatos-stg issuer/audience internally while this project is reused
```

Do not call the public app, dashboard, release notes, APK filenames, or API URLs
`stg` unless the section is explicitly describing legacy staging operations.
Historical staging runbook sections remain valid for old deployment mechanics
and internal project references.

## Org / project guardrail (run before ANY cloud command)

Goat OS cloud work targets the Mesha/VGoats organization only.

```text
Organization: vgoats.com  (org id 563962826703)
Folder:       goat-os      (folder id 188649904255)
Projects:     goatos-dev | goatos-stg | goatos-prod   (already created, billing linked)
Never:        Heva / Slice orgs, hevaplatform, goatos-sheets (legacy, untouched)
```

Before any create/update/delete/IAM/billing/deploy command, verify and state the
active account, org, folder, project, and target repo:

```bash
gcloud config list --format="text(core.account,core.project)"   # expect ravi@mesha.sg, project explicit
```

If the active context is not Mesha/VGoats, stop and fix context first.

## Environment posture

| Env | Purpose | Data | Auth |
| --- | --- | --- | --- |
| `goatos-dev` | real-data debug clone, non-authoritative | real-shaped clone | `jwks` (real IdP) — HS256 only for throwaway local rehearsal |
| `goatos-stg` | scale rehearsal + load tests | 1M synthetic baseline | `jwks` |
| `goatos-prod` | live truth | production | `jwks` only |

The local dev-token demo (`make dev-local`, `mint-dev-token`, `seed-dev-grant`)
is not a deployment path. `seed-dev-grant` refuses shared/staging/production
targets; goatos-dev Cloud SQL use requires the explicit dev Cloud SQL opt-in
guard, an exact `GOATOS_DEV_CLOUDSQL_CONNECTION_NAME` match, and is for dev
rehearsal only.

## Dev API and budget gate

P5 enabled only the approved `goatos-dev` APIs:

```text
artifactregistry.googleapis.com
billingbudgets.googleapis.com
cloudbuild.googleapis.com
cloudscheduler.googleapis.com
compute.googleapis.com
identitytoolkit.googleapis.com
pubsub.googleapis.com
run.googleapis.com
secretmanager.googleapis.com
sqladmin.googleapis.com
```

Compute API enablement auto-created the default VPC and default firewall rules.
They are unused for the current Cloud Run plus Cloud SQL connector/socket plan;
do not delete or modify them unless explicitly approved.

P6 created the dev-only budget:

```text
Billing account: 01FEDE-96BCB3-76D992
Budget:          Goat OS dev monthly budget
Budget id:       f90ceaf4-efea-4c4f-9059-32b57b349992
Scope:           projects/634659905829 (goatos-dev only)
Amount:          INR 4,750 monthly (billing-account currency; about USD 50)
Alerts:          50%, 80%, 100% current spend
```

## Terraform state

The `goatos-dev` Terraform state bucket was bootstrapped imperatively before
Terraform backend init:

```text
Bucket:       gs://goatos-dev-tf-state
Project:      goatos-dev
Location:     asia-south1
State prefix: terraform/dev
```

The bucket has uniform bucket-level access, public access prevention enforced,
and object versioning enabled. It stores Terraform state only: no goat data,
legacy exports, secrets, app artifacts, container images, or migration payloads.
Do not delete or modify the state bucket unless explicitly approved.

Staging has Terraform under `infra/envs/stg/` with backend:

```text
Bucket:       gs://goatos-stg-tf-state
Project:      goatos-stg
Location:     asia-south1
State prefix: terraform/stg
```

The bucket exists and stores state only. The source is aligned to live staging
names, including `goatos-stg-media`, `goatos-proof-signer-stg`,
`goatos-vax-generator-stg`, `goatos-notify-stg`, and the GitHub OIDC deployer.
Live resources still need to be imported into state before a broad staging
`terraform apply`; do not apply an empty state against live staging resources.

Current staging keeps Cloud SQL warm with `activation_policy = "ALWAYS"` so
`stg` branch deploys can run migrations and `/readyz` without a manual database
start. Both `goatos-api-stg` and `goatos-admin-web-stg` are publicly invokable
at the Cloud Run layer today; this is the authoritative staging ingress posture
until a separate service-to-service/IAP design exists. Goat OS JWKS/RBAC remains
the authorization boundary for protected app routes.

Staging deployment authority:

```text
Cloud Deploy pipeline: deploy/clouddeploy/stg/clouddeploy.yaml
Release helper:        tools/deploy/stg-clouddeploy-release.sh
Detailed runbook:      docs/runbooks/cloud-deploy-staging.md
Default trigger:       Slack #goatos-stg-deploy -> Cloud Build goatos-stg-deploy-main
```

Staging-backed releases are the unit of change while `goatos-stg` remains the
internal project. A release carries the backend, migration, and admin-web images
for one commit. Cloud Deploy runs the migration job first, then updates API,
backend worker jobs, and admin-web, then verifies image skew and smokes
`/livez`, `/readyz`, `https://api.goatos.mesha.sg/app/bootstrap`, and
`https://dashboard.mesha.sg/login`.

Do not update staging Cloud Run services or jobs directly from GitHub Actions
or a local shell except as documented break-glass. Direct updates are how staging
ends up with API, jobs, and schema from different commits.

Staging deploy automation:

```text
Slack #goatos-stg-deploy button
  -> Cloud Run Slack bot goatos-stg-slack-deploy-bot
  -> Cloud Build trigger goatos-stg-deploy-main on origin/main
  -> cloudbuild.stg.yaml
  -> Cloud Deploy release and rollout
```

Use the latest bottom-most Slack deploy panel. The bot posts a fresh panel again
after each deploy reaches success or failure, so operators should not scroll up
through old deployment history to find the button.
Terminal success, failure, and release-bookkeeping warning cards include the
next deploy controls directly, so the bottom-most relevant deploy message is
always the one to use next.

The panel has two actions. `Deploy backend/web` runs the existing Cloud Deploy
pipeline, optionally followed by Android when the mobile checkbox is selected.
`Distribute Android only` skips Cloud Deploy and publishes the GoatOS Android
release only. The bot allows only one active deployment at a time: Android-only,
backend/web-only, and backend/web+mobile all block each other while Cloud Build
is queued or working. During that time, clicking the panel replaces it with an
"already running" status card and Cloud Build / Cloud Deploy links; the deploy
buttons return only after the running build posts success or failure.

For a backend/web+mobile run, Slack must show the stages in this order:

```text
Backend/web deploy started
Backend/web deploy succeeded
Release bookkeeping completed or warning
Android distribution started
Android distribution succeeded or failed
Deploy button ready
```

Do not treat the combined deploy as fully complete until both the backend/web
rollout and Android distribution have terminal cards. The Android terminal card must
state whether Firebase App Distribution, Google Play Internal Testing, and
`https://mesha.sg/app.apk` all completed. If Android fails after backend/web succeeds,
the status is `Backend/web: SUCCESS` and `Android mobile: FAILED`; do not say the
Firebase/Play/app.apk channels completed.

Release-tag bookkeeping is intentionally separate from deploy status. A
deploy is successful only after Cloud Deploy rollout succeeds and live service
and job images match the commit. The later `stg-release-tag-bookkeeping` Cloud
Build step records the release tag from a clean checkout of the verified commit
using Secret Manager secret `goatos-github-pat`. It may post a yellow Slack
warning if tagging fails, but it must not turn a verified backend/web deploy into a red
failure.

Cloud Build builds/pushes backend, migration, and admin-web images, then
creates a Cloud Deploy release. Cloud Deploy owns all Cloud Run mutations.
Never push a local branch, `HEAD`, `main`, or refspec directly to remote `stg`;
local/agent hooks block it, and the `stg` branch is not deployment authority.

If the deploy includes publishing an Android employee build, follow
`docs/mobile/production-facing-release.md` as the active release gate. Firebase
App Distribution alone is not complete: publish Google Play Internal Testing
from the same source/version identity and mirror the exact Firebase APK bytes to
`gs://goatos-stg-public-downloads/operator/latest/app.apk`, which backs
`https://mesha.sg/app.apk`. Do not rebuild or redeploy the Mesha marketing
website to update the APK.

The Slack deploy card has an `Also distribute Android mobile` checkbox for this
case. Leaving it unchecked deploys only the backend/web Cloud Run surfaces.
Checking it runs the backend/web deploy first and then treats mobile as an
all-or-nothing release: Firebase App Distribution upload, Google Play Internal
Testing upload to package `sg.mesha.goatos`, and the `mesha.sg/app.apk`
Storage mirror must all pass or the Cloud Build is failed and Slack reports the
mobile distribution as failed.

Each user-visible Android release must advance the Android `versionName`
and `versionCode`, for example `0.1.20 (21)` -> `0.1.21 (22)`. Slack
mobile deploy clicks bump the checked-in defaults in
`apps/goatos-android/app/build.gradle.kts` on `main` before starting Cloud
Build, so every click publishes a new human-readable Firebase version. Manual
non-Slack repair runs may set `GOATOS_ANDROID_VERSION_CODE` /
`GOATOS_ANDROID_VERSION_NAME` only when the intended version identity is
explicit.

The Cloud Build timeout for this combined path is 2 hours. If Cloud Build times
out during `android-mobile-distribution`, assume the Android release did not
finish unless the logs independently show Firebase, Play Internal, and direct
APK success.

## Dev Layer 1 foundation plan

P7-preapply adds Terraform for Layer 1 foundation only and stops before apply.
The validated plan is expected to create:

```text
Artifact Registry Docker repo: asia-south1-docker.pkg.dev/goatos-dev/goatos
Cloud SQL Postgres shell:      goatos-dev-core-db / database goatos
Cloud SQL tier:                db-f1-micro
Cloud SQL activation policy:   ALWAYS (running while raw dev dashboard is live)
Cloud SQL connectivity:        public IPv4 + future /cloudsql connector socket
Pub/Sub:                       outbox topic, analytics subscription, DLQ
Secret Manager:                regional containers only, no secret versions
IAM:                           runtime SAs, Cloud SQL client, secret access,
                               topic publisher, Pub/Sub service-agent DLQ IAM
```

The plan must not include Cloud Run services/jobs, Scheduler jobs, load
balancing, DNS, VPC connectors, Cloud SQL users/passwords, authorized networks,
Secret Manager secret versions, Firebase Hosting/App Hosting, image pushes,
migrations, or legacy imports.

## Dev Cloud Run invocation decision

For the `goatos-dev` bring-up, deploy the backend Cloud Run service as publicly
invokable at the Cloud Run layer and enforce authentication/authorization inside
Goat OS with `GOATOS_AUTH_MODE=jwks` and DB-backed RBAC grants. Live admin-web
uses Firebase Auth client persistence plus an HTTP-only Firebase ID-token cookie
so server-side calls can forward the signed-in user's
`Authorization: Bearer <id_token>` to the backend. Admin-web does not currently
mint a separate Cloud Run IAM identity token. Making the backend service
IAM-private before adding a separate service-to-service auth design will fail as
a Cloud Run 403 before the request reaches Goat OS app auth.

This is a dev/staging bring-up posture. `goatos-stg` keeps the same public Cloud
Run layer because the Android app, admin-web SSR, and staging smoke checks need a
reachable app API before a separate service-to-service/IAM or IAP design exists.
Do not copy it to `goatos-prod` without an explicit production ingress decision
that preserves Goat OS app auth rather than replacing the user's app bearer token.

## Dashboard hostnames and DNS

Current public dashboard hostname target:

```text
dashboard.mesha.sg -> goatos-stg-backed production-facing dashboard
```

Current public API hostname target:

```text
api.goatos.mesha.sg -> goatos-api-stg-backed production-facing API
```

Legacy/internal dashboard hostnames:

```text
URL:         https://dev.dashboard.mesha.sg/
Project:     goatos-dev
Cloudflare:  mesha.sg zone, Manju@flokx.io Cloudflare account
DNS record:  A dev.dashboard -> 8.232.140.161, DNS-only
LB IP name:  goatos-nonprod-dashboard-ip
LB IP:       8.232.140.161
Certificate: goatos-nonprod-dashboard-cert, ACTIVE for dev.dashboard.mesha.sg
Backend:     goatos-admin-web-dev through serverless NEG goatos-admin-web-dev-neg

URL:         https://stg.dashboard.mesha.sg/ (legacy compatibility only)
Project:     goatos-stg
Cloudflare:  mesha.sg zone, Manju@flokx.io Cloudflare account
DNS record:  A stg.dashboard -> 8.233.143.24, DNS-only
LB IP name:  goatos-stg-dashboard-ip
LB IP:       8.233.143.24
Certificate: goatos-stg-dashboard-cert, ACTIVE for stg.dashboard.mesha.sg
Backend:     goatos-admin-web-stg through serverless NEG goatos-admin-web-stg-neg
```

Legacy/internal staging API hostname:

```text
URL:         https://stg-api.dashboard.mesha.sg/ (legacy compatibility only)
Project:     goatos-stg
Cloudflare:  mesha.sg zone, Manju@flokx.io Cloudflare account
DNS record:  A stg-api.dashboard -> 8.233.143.24, DNS-only
LB IP name:  goatos-stg-dashboard-ip
LB IP:       8.233.143.24
Certificate: goatos-stg-api-cert
Backend:     goatos-api-stg through serverless NEG goatos-api-stg-neg
URL map:     goatos-stg-dashboard-map host rule stg-api.dashboard.mesha.sg -> goatos-api-stg-backend
Android legacy stg: BuildConfig.API_BASE_URL=https://stg-api.dashboard.mesha.sg/
```

The current public cleanup intentionally reuses the `goatos-stg` project and
load balancer while moving user-facing names to production-facing hosts. A later
true prod migration may introduce separate prod infrastructure, but that is not
this rollout.

Cloudflare records for Google-managed certificates should start as DNS-only
while Google provisions or renews the certificate. Do not orange-cloud/proxy the
record unless that behavior has been explicitly tested with the selected Google
certificate and OAuth setup.

If no scoped Cloudflare API token is available, use the logged-in Cloudflare
browser session to add DNS records. Do not store a personal Cloudflare token in
`.zshrc` or commit it. For CI/future automation, create a Cloudflare token
scoped only to the `mesha.sg` zone with `Zone:Read` and `DNS:Edit`, store it as
`CLOUDFLARE_API_TOKEN_MESHA_DNS`, and store the zone id separately as
`CLOUDFLARE_ZONE_ID_MESHA_SG`.

Custom-host auth has two independent allowlists:

```text
Firebase/Auth Platform authorized domains:
- dev.dashboard.mesha.sg
- dashboard.mesha.sg
- stg.dashboard.mesha.sg (legacy compatibility)
- localhost

Google OAuth web client authorized JavaScript origins:
- https://dev.dashboard.mesha.sg
- https://dashboard.mesha.sg
- https://stg.dashboard.mesha.sg (legacy compatibility)
- http://localhost:3000
- http://localhost:3300
- http://localhost:3311

Google Auth Platform Branding:
- App name: Mesha
- App name: GoatOS (`goatos-stg` backing project)
- `goatos-stg` audience: External, In production. Goat OS backend/Firebase
  grants remain the access boundary; do not rely on Google's test-user list for
  staging access control.
```

The Firebase authorized domain was updated through the Identity Toolkit API.
The OAuth web client JavaScript origin and Branding app name were updated in
Google Auth Platform while signed in as a project-authorized Mesha/VGoats
account. If a future custom host is added, update both auth allowlists; missing
JavaScript origins cause `origin_mismatch` even when DNS, TLS, and the
dashboard page load work.

## Required backend config per environment

The API binary (`backend/cmd/api`) is configured entirely through env vars. A
per-env template lives at `infra/envs/<env>/goatos-api.env.example`. Production
values come from Secret Manager, never from committed files.

```text
# HTTP
GOATOS_HTTP_ADDR=:8080

# Database (Cloud SQL connection string / socket)
DATABASE_URL=postgres://.../goatos?sslmode=...
GOATOS_DEV_CLOUDSQL_CONNECTION_NAME=goatos-dev:asia-south1:<instance> # dev guard only

# Auth — shared/staging/prod MUST USE jwks (see docs/runbooks/auth.md)
GOATOS_AUTH_MODE=jwks
GOATOS_AUTH_ISSUER=<idp issuer URL>
GOATOS_AUTH_AUDIENCE=<goat-os api audience>
GOATOS_AUTH_JWKS_URL=<idp JWKS endpoint>
GOATOS_AUTH_CLOCK_SKEW=60s            # optional
GOATOS_AUTH_ALLOWED_ALGS=RS256,ES256  # optional
GOATOS_AUTH_JWKS_CACHE_TTL=10m        # optional
GOATOS_AUTH_MAX_TOKEN_TTL=24h         # optional ceiling
GOATOS_AUTH_ALLOWED_EMAILS=<approved admin email>[,<approved admin email>...]
GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS=<tenant uuid>[,<tenant uuid>...]
GOATOS_AUTH_SESSION_RATE_LIMIT_PER_MINUTE=120  # default; 0 disables for controlled local smoke
GOATOS_APPCHECK_ENFORCE=off|monitor|enforce     # default off; monitor before enforce
GOATOS_APPCHECK_ISSUER=https://firebaseappcheck.googleapis.com/<firebase project number>
GOATOS_APPCHECK_AUDIENCE=projects/<firebase project number>
GOATOS_APPCHECK_JWKS_URL=https://firebaseappcheck.googleapis.com/v1/jwks  # optional default
GOATOS_APPCHECK_CLOCK_SKEW=60s                                             # optional
GOATOS_APPCHECK_JWKS_CACHE_TTL=5m                                          # optional

# Environment + observability
GOATOS_ENV=stg|prod                    # NOT local/dev/test for shared envs
GOATOS_OBS_SINK=gcm                    # stdout_json | otlp | gcm
```

`GOATOS_AUTH_MODE=bearer` (HS256) is rejected unless `GOATOS_ENV` is
`local`, `dev`, or `test`. Shared/staging/prod must run `jwks`.

`goatos-dev` uses Google Identity Platform / Firebase Auth as the JWKS IdP.
Firebase is auth only; do not use Firebase Hosting or Firebase App Hosting. For
dev Firebase tokens, set issuer `https://securetoken.google.com/goatos-dev`,
audience `goatos-dev`, and JWKS URL
`https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com`.
Staging uses the same Firebase/Auth Platform pattern with issuer
`https://securetoken.google.com/goatos-stg` and audience `goatos-stg`.
Dashboard login supports both Google SSO and Firebase email/password, including
password-reset email; both paths still rely on backend
`GOATOS_AUTH_ALLOWED_EMAILS` and DB grants for access.
The admin-web proxy only redirects missing, malformed, or expired Firebase
ID-token cookies; backend JWKS verification remains the trust boundary.
`GOATOS_AUTH_ALLOWED_EMAILS` is the environment-level dashboard email allowlist:
it is required in JWKS mode, and the API fails closed at startup if it is empty.
Sign-in/session audit and protected API requests reject tokens whose verified
email is not present in the list. Google Workspace `hd` hints are not sufficient
access control by themselves.

Firebase App Check is a separate app-attestation layer for Android/backend
traffic. Keep `GOATOS_APPCHECK_ENFORCE=off` until the Android client sends
`X-Firebase-AppCheck` and Firebase Console App Check registration is complete.
Use `monitor` to log missing/invalid/pass without blocking old app versions,
then flip to `enforce` when rollout is healthy.

Admin-web supports canonical-host redirects:

```text
GOATOS_CANONICAL_DASHBOARD_HOST=dev.dashboard.mesha.sg
```

When set, requests for raw Cloud Run `*.run.app` dashboard hosts redirect to the
canonical dashboard hostname before auth. Keep the Cloud Run default URL disabled
for shared environments once the load balancer hostname is healthy; the
canonical redirect is a user-friendly fallback, not the primary exposure model.

## Release steps

For staging, the normal release path is now:

```text
press Deploy backend/web in #goatos-stg-deploy
watch the Cloud Build link posted by Slack
open the linked Cloud Deploy rollout if the deploy step fails
if Android was checked, inspect the mobile step for Firebase/Play/app.apk status
```

Manual steps below remain the reference for dev/prod and for break-glass staging
operations only.

## Manual release steps

1. **Build + publish images.** Build the three image families in
   `docs/runbooks/containers.md`: backend multi-binary, migration job, and
   admin-web dashboard. Tag by git SHA, push to the project's asia-south1
   Artifact Registry, and record the SHA — it is the rollback handle. For
   admin-web fixes intended to be visible on the raw dev Cloud Run URL, follow
   the admin-web dev deploy checklist in `docs/runbooks/containers.md`; a Git
   push alone does not update Google dev.
2. **Apply migrations.** Migrations live in `backend/migrations/postgres/`
   (`000001`..`000022`, forward-only, never edit an applied migration).
   `make validate-migrations` validates the goose-style SQL locally. For a fresh
   `goatos-dev` Cloud SQL database, run the migration image from
   `docs/runbooks/containers.md`; it is the sole shared-DB applier and records
   applied files in `goatos_schema_migrations`. Do not mix it with a separate
   goose/psql/local validator applier on the same Cloud SQL database. Migrations
   must run to completion before the new image serves traffic. The `goatos-dev`
   migration job must set `GOATOS_ENV=dev`,
   `GOATOS_ALLOW_DEV_CLOUDSQL_TARGET=true`,
   `GOATOS_DEV_CLOUDSQL_CONNECTION_NAME=goatos-dev:asia-south1:<instance>`, and
   a socket-form `DATABASE_URL` whose host is exactly
   `/cloudsql/goatos-dev:asia-south1:<instance>`.
3. **Seed approved admin emails (first deploy / access changes).** Production
   authorization comes from active `user_scope_grants` rows, not from token
   claims. Shared Firebase/JWKS environments should pre-seed
   `auth_pending_email_grants` for approved verified emails and roles. On first
   verified `auth.sign_in`, the backend converts the matching email policy into
   the real tenant-scope `user_scope_grants` row and records
   `auth.pending_email_grant_claimed` in `audit_log`. This avoids fake Firebase
   UIDs and avoids manual waiting after first sign-in. `seed-dev-grant` remains
   a local/dev direct-UID helper only.
4. **Deploy.** Roll the new image. Keep the previous revision available for
   rollback.
5. **Smoke.** See "Smoke checks" — must pass before announcing the release.
6. **Process integrity smoke.** Confirm the current Admin Config + Preventive Care (PC)
   Vaccination + vaccination execution APIs are healthy before announcing
   the release.

## Smoke checks

```text
GET /livez     -> 204 (no auth; use this for Cloud Run liveness smoke)
GET /readyz    -> 204 (DB reachable AND not migration-drifted; see below)
GET /version   -> 200, "migration_drift": false (no auth; build SHA + migration levels)
A real bearer token (from the IdP) on a read route (e.g. GET /goats/search) -> 200
A token with wrong issuer/audience/alg -> 401
GET /action-center/obligations -> 200 for an authorized internal role
GET /protocols/versions/{version_id} -> 200 for an authorized config role
```

`/readyz` and `/version` both compare the database's applied migration level
against this binary's embedded migration ceiling
(`internal/platform/migrationguard`,
`docs/decisions/stale-binary-migration-drift-guard.md`). If a deploy's
migrate step ran but the app image is stale (or vice versa), `/readyz` goes
`503` with a `migration drift: ...` body instead of serving requests against
a schema it disagrees with, and `/version` shows exactly which version each
side is at plus `migration_drift`/`migration_drift_reason`. Check `/version`
first when a post-deploy smoke check looks like an unrelated 500/503 - it is
the fastest way to rule out (or confirm) a stale binary vs. a real
regression.

For admin-web dashboard smoke, use the canonical dashboard host and the
committed auth routes:

```text
GET https://dev.dashboard.mesha.sg/login -> 200
GET https://dev.dashboard.mesha.sg/api/auth/firebase-config -> 200
```

Do not use `/api/config/firebase`; that route does not exist. Raw
Cloud Run `*.run.app` dashboard hosts are not the app source of truth after
canonical-host redirect/custom-domain setup.

`GET /healthz` remains a local/container liveness route, but raw Cloud Run/GFE
can reserve or intercept that exact path before it reaches the container. Use
`/livez` for public Cloud Run smoke checks.

Local smoke should use `make dev-local`, backend `go test ./...`, and the
admin-web visual smoke for the current active routes.

## Rollback

```text
1. Redeploy the previous image SHA (fast path; no data change).
2. If a migration caused the failure: a forward-only fix migration is preferred.
   Only use a down-migration if the change is provably reversible and no rows
   depend on it. Never hand-edit data to "undo" — write a corrective migration.
3. If a rollback touched process state, run the relevant forward repair or
   projection rebuild job for that module. Do not reintroduce the deleted
   identity-counter commands.
```

Rolling back the app image alone (without a matching down-migration) puts the
previous, older binary in front of an already-forward-migrated database. As
of `docs/decisions/stale-binary-migration-drift-guard.md`, that older binary
will refuse to start (or go `503` on `/readyz` if already running) rather
than silently serving requests against a schema it doesn't recognize. If a
rollback needs the old binary to run anyway, it must roll the database back
too (down-migration), not just the image.

## Monitoring / alerts (per env)

Wire these before calling an environment production-ready:

```text
API latency (p50/p95/p99) and error rate per route
DB pressure: connections, slow queries, Cloud SQL CPU/mem
Outbox lag: pending/oldest-unpublished age, dead_letter count
Process projection staleness for obligation/vaccination/control surfaces
Import/sync run failures and conflict volume
```

Observability uses `GOATOS_OBS_SINK=gcm` (Google Cloud Monitoring/Logging/Trace)
per `docs/decisions/observability.md`. OpenTelemetry spans/metrics exporters are
a deferred hardening item.

For `goatos-dev`, `infra/envs/dev/monitoring.tf` provides the current baseline
alert policies: Cloud Run `ERROR` logs, outbox relay `dead_letter > 0`, Pub/Sub
DLQ backlog, and Cloud SQL CPU pressure. Goal 2 must verify these policies in
Cloud Monitoring, wire approved notification channels through private
`monitoring_alert_email_addresses`, and attach alert screenshots or
`gcloud monitoring policies list` evidence to the deployment record.

## What is blocked (external operator action, not Phase 1 code)

The following require cloud access in the verified `vgoats.com` context and are
**not** performed from the build workspace:

```text
- Running terraform apply for the P7 Layer 1 foundation plan or authoring later
  app-resource modules beyond Layer 1.
- Provisioning Cloud SQL, GCS, Pub/Sub topics, Secret Manager secrets, service
  accounts, and Artifact Registry per project.
- Standing up a production IdP/JWKS endpoint and loading signing keys/secrets.
- Wiring the production-safe legacy sync executor as a manual RBAC-protected
  command first. Do not create Cloud Scheduler polling for BQ/Sheets until the
  replay harness proves old loaded state converges to current live BQ/Sheets
  with zero goat-level drift and idempotent reruns. Any future scheduler must
  call the same audited backend job rather than reading legacy sources directly.
- Wiring the Pub/Sub outbox publisher + worker deploy (see event egress
  follow-up in BUILD-STATUS).
- Running the migration apply, image deploy, and smoke against real projects.
```

These are production-launch tasks. The Phase 1 backend, migrations, auth modes
(including `jwks`), and worker entrypoints are built and locally verified; what
remains is provisioning + deploy execution under the correct org.
