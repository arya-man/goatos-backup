# Mesha Admin Web

This app is the current Mesha internal admin surface for the vaccination
process-integrity build. It is not the old dashboard, not the old Import Review
console, and not a BigQuery/Sheets runtime UI.

## Current Product Slice

Build and verify these implemented surfaces only:

- `/login`
- `/` Control Tower
- `/calendar`
- `/action-center`
- `/protocol-adherence`
- `/workflows`
- `/workflows/{row_id}`
- `/vaccination`
- `/vaccination/execution/sheds/[shedId]`
- `/vaccination/live-tracker` — Live Drive Tracker: today's drive at administration grain (operator board, shed proof progress, combo doses, live activity feed, attention, verification queue)
- `/procurement/source-entry`
- `/procurement/source-entry/loads/{load_id}`
- `/counts/herd`
- `/operations/audit`
- `/operations/dlq`
- `/config`
- `/sops`
- `/goats/{goat_id}`

The working product model is:

- **Admin / Data Ops** owns generic protocol config and SOP policy.
- **Preventive Care (PC) / Vaccination** owns vaccination operations, proof, and verification.
  Vaccination execution context (park, shed, stage, defer/blocker, owner, SOP/proof
  status) renders INSIDE /vaccination#execution, not as a separate Parks route.
- **Command lenses** are top-level screens over the same backend truth: Control
  Tower, Action Center, Calendar, Protocol Adherence, and Workflows. Calendar
  is reopened only for vaccination-related due work, not all-domain mock content.
- **Control Tower** summarizes only broken or at-risk process. Do not build
  generic KPIs there.

## UI Source Of Truth

The only admin-web UI/UX source of truth is:

```text
../../mock/goatos-dashboard-mock.html
```

Port its layout, table shapes, empty states, icon system, spacing, density, and
interaction model. Do not reuse or recolor the old admin UI. The old dashboard
routes and old primitives have been removed from this app.

Required frontend gates:

```bash
npm run check:mock-fidelity
npm run lint
npm run typecheck
npm run build
```

When a local backend and admin-web are running, also run:

```bash
npm run smoke:visual:live
```

`smoke:visual:live` covers implemented routes only, including Calendar and the
Operations DLQ repair lane.

Open the generated screenshots under
`.codex-goatos-render/admin-web-screenshots/` before claiming visual QA.

## Local Development

From the repo root:

```bash
cd /path/to/goatos
make dev-local
```

Open `http://127.0.0.1:3300/`.

For admin-web-only checks:

```bash
cd /path/to/goatos/apps/admin-web
npm install
npm run dev:local
```

`dev:local` and `start:local` bind to `127.0.0.1:3300`, seed the local grant,
and mint a fresh server-side token unless `GOATOS_LOCAL_DEV_AUTO_AUTH=false` is
set.

## Environment

Backend calls are made from Next server components/adapters only:

```bash
export GOATOS_API_BASE_URL=http://127.0.0.1:8080
export GOATOS_BEARER_TOKEN=<local-dev-token>
export GOATOS_TENANT_ID=<tenant-uuid>
```

Do not put tokens in browser code, `NEXT_PUBLIC_*` variables, localStorage,
rendered HTML, query params, or static assets.

### Web RUM (Grafana Faro)

Frontend Real User Monitoring (`components/observability/faro-provider.tsx`,
OBSERVABILITY_DESIGN.md §2.4) is public browser config, so — unlike the backend
vars above — it is intentionally `NEXT_PUBLIC_*`:

```bash
# Grafana Alloy faro.receiver endpoint (infra lane; see
# docs/observability/OBSERVABILITY_DESIGN.md §1/§4). Leave unset to disable RUM
# entirely — local dev without a collector is a no-op, not an error.
export NEXT_PUBLIC_FARO_COLLECTOR_URL=https://alloy.example/collect

# Environment label attached to every RUM signal (e.g. local, stg, prod).
export NEXT_PUBLIC_GOATOS_ENV=local

# App version label attached to every RUM signal (release tag / build SHA).
export NEXT_PUBLIC_APP_VERSION=dev
```

Optional, only needed if the browser ever fetches the backend origin directly
(it does not today — all backend calls are proxied through Next server
components/route handlers per this doc's "Backend calls" note above). Faro's
fetch instrumentation always attaches `traceparent` to same-origin requests
without this:

```bash
export NEXT_PUBLIC_GOATOS_API_BASE_ORIGIN=https://api.example.com
```

When `NEXT_PUBLIC_FARO_COLLECTOR_URL` is unset, `FaroProvider` no-ops (no SDK
initialization, no network calls) — safe for local dev and any environment
without a deployed collector.

### Web Performance Monitoring

Firebase Performance Monitoring for Web is enabled in staging and uses the same
Firebase Web App config served by `/api/auth/firebase-config`; do not create a
separate project just for browser perf. Keep it on per environment:

```bash
export NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED=1
```

Set `NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED=0` only when intentionally
disabling collection for a local or diagnostic run.

The shell records a custom `admin_route_navigation` trace for route switches,
including sidebar switches, with low-cardinality attributes plus duration
metrics. The existing Faro + `/api/admin-web/performance-events` path remains
the richer high-cardinality event stream for exact from/to URLs and backend
correlation.

For lab reports:

```bash
npm run perf:lighthouse
PAGESPEED_URL=https://stg.dashboard.mesha.sg npm run perf:pagespeed
```

Lighthouse works against local authenticated admin-web runs. PageSpeed Insights
only works for deployed URLs Google can reach.

## Generated Client

The app imports `@goatos/api-client` from the repo package:

```json
"@goatos/api-client": "file:../../packages/api-client"
```

Regenerate and check drift from the repo root:

```bash
make api-client-generate
make api-client-check
./tools/agent-hooks/check-contract-drift.sh
```

## Hard No

- No direct BigQuery, Sheets, GCS, Firestore, or database reads from frontend.
- No old Import Review, Data Quality, Legacy Sync, Counts, Mortality, Operators,
  Tasks, old generic SOP builder, or old Herd routes as product screens.
- No nested command-lens routes such as `/vaccination/adherence`,
  `/vaccination/calendar`, or `/vaccination/workflows`.
- No old cyan/slate/admin-primitives visual system.
- No global Goat Passport search as the primary workflow.

## Browser (Chrome) web push

The CEOs work in Chrome, so browser push is a primary delivery channel for admin-web, not an
extra. **It needs no configuration to work.** The Firebase JS SDK ships its own default VAPID key
pair and `getToken()` uses it when none is supplied; FCM holds the matching private key, so the
token is fully deliverable with nothing set. Proven end to end in real Chrome on this branch: a
real 142-char registration token minted with the variable UNSET, registered through the real
backend endpoint, and a real push rendered by the real service worker with the correct deep link.

One OPTIONAL environment variable switches it onto this project's own key:

```bash
# OPTIONAL. The PUBLIC VAPID key pair from Firebase console -> Project settings -> Cloud
# Messaging -> Web Push certificates. Public by construction (the browser transmits it to the push
# service on every subscribe); the matching private key never leaves the Firebase project and is
# never handled by this repo. NEVER commit a service-account JSON for this -- FCM sending uses the
# worker's own service account (see backend/cmd/notification-dispatcher).
#
# Set it when this project wants its own key for PROVENANCE and INDEPENDENT ROTATION; leave it
# blank to use the SDK default. It is not required for delivery.
GOATOS_FIREBASE_WEB_PUSH_VAPID_KEY=
```

Read at RUNTIME by the `getWebPushVapidKey` server action in `lib/web-push-actions.ts`, not
inlined as a `NEXT_PUBLIC_*` build arg, for the same reason `/api/auth/firebase-config` resolves
the Firebase config at runtime: one image is deployed to more than one environment, and a key
baked in at build time would be the wrong project's key in the other one.

**Empty is safe, is the default, and OFFERS the control.** An absent key omits `vapidKey` from
the `getToken()` call so the SDK's default applies; the browser is asked for permission and
registers normally. It used to report "browser notifications are not configured for this
environment yet" and offer nothing, which left push dead in staging waiting on a console step for
a key that is not needed to deliver.

**A key that IS set but malformed fails loudly** and is never silently replaced by the default:
that is the real trap the old wording was about, because a wrong key yields a token FCM accepts
and can never deliver to, and a silent fallback would turn a typo in Terraform into a channel
that reports "enabled" and delivers nothing. The three-state rule lives in one pure, tested
place, `resolveVapidKeyConfig` in `lib/web-push-state.ts`:

| `GOATOS_FIREBASE_WEB_PUSH_VAPID_KEY` | behaviour |
| --- | --- |
| absent / empty | `vapidKey` omitted; the SDK default applies; the control is offered |
| present, well-formed | used verbatim |
| present, malformed | refused; the control reports the unusable key and offers nothing |

Relying on the SDK default costs nothing operationally: every FCM registration, refresh and
delete is `POST`/`DELETE https://fcmregistrations.googleapis.com/v1/projects/{projectId}/registrations`
authenticated by this project's API key and its own Firebase Installations token, so revocation
and rotation are project-scoped either way. The VAPID key travels only as a `web.applicationPubKey`
field on the registration body and confers no authority over the subscription. Setting a project
key later is a clean migration: the SDK compares the stored key against the requested one
(`isTokenValid`), deletes the old token and mints a new one on the next load.

Delivery also needs the backend's existing FCM config (`GOATOS_FCM_PROJECT_ID` plus the
dispatcher's service-account credentials, already provisioned in
`infra/envs/{dev,stg}/cloud_run_worker.tf`). Browser push reuses that send path verbatim: a
Firebase-JS-SDK web registration token is the same opaque shape the `push_fcm` channel already
addresses.

Web push also requires a **secure context** (https, or localhost). On a plain-http host the
control reports it and nothing subscribes.
