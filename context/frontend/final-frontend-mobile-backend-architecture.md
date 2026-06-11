# Goat OS Frontend, Mobile, And Backend Architecture

This is the build direction after checking the current repos:

- `<mesha-workspace>/dashboard` - CEO-style Next.js dashboard.
- `<mesha-workspace>/vgoats-dashboard` - investor/reduced dashboard, mostly same shape.
- `<mesha-workspace>/procurement_app` - React Native operator/procurement app.
- `<mesha-workspace>/website` - public marketing site, business-context reference only.
- `<mesha-workspace>/slack-automation-scripts` - legacy Slack/Sheets/App Script automation.

The answer is not "throw the UI away." The answer is: keep the useful UI, remove direct Sheets/BigQuery/Firestore coupling, and put every replaceable vendor/tool behind a Goat OS contract.

Dashboard migration rule:

```text
Do not modify the current live dashboard repos for Goat OS rewiring.

<mesha-workspace>/dashboard/
  stays live as-is until the new Goat OS dashboard is validated.

<mesha-workspace>/vgoats-dashboard/
  stays live as-is until the new Goat OS investor/admin view is validated.

Goat OS work happens only inside new cloned/copied app folders under goatos/.
```

## Current Repo Findings

### Dashboard Apps

The dashboard apps are reusable:

- Next.js 14 App Router.
- React 18, TypeScript, Tailwind.
- Recharts chart library.
- TanStack Query hooks.
- Good dark command-center shell with chart/card/table components.
- Existing pages cover counts, mortality, births, feed, MIS, infra, purchase cost, shifting, summary, and the fuller CEO app also has fattening, health, vaccination, sales, parent stock, milk.

Migration approach:

```text
1. Take fresh snapshots/clones of both dashboard frontends into goatos/apps/.
2. Rewire only the goatos copies.
3. Keep old deployed dashboards running during validation.
4. Compare old vs new screens, counts, routes, and role visibility before cutover.
```

The weakness is data coupling:

- Next API routes query BigQuery directly through `@google-cloud/bigquery`.
- Some fallback data is local CSV via `papaparse`.
- Table names and dataset routing are embedded in frontend repo code.
- There is no real auth/RBAC boundary in the dashboard app itself.
- CEO and investor dashboards are separate deployed apps today. Goat OS keeps
  that surface separation as an intentional boundary, but removes duplicated
  data access and shared logic by moving them behind typed Goat OS APIs and
  shared packages.

Keep:

- Page layouts.
- Chart components.
- Sidebar/navigation patterns.
- TanStack Query usage.
- Existing KPI/page inventory.

Replace:

- Direct BigQuery calls from Next routes.
- CSV fallback as product data source.
- Duplicated CEO/investor codebases that copy data logic and route structure.
  Intentional surface deploys stay valid when they consume shared packages and
  typed Goat OS APIs.
- Any unauthenticated dashboard/API access.

### React Native Operator App

The mobile app is reusable as a prototype base:

- React Native 0.85, React 19, TypeScript.
- Firebase Auth/Firestore/Storage/Messaging.
- React Navigation.
- Vision Camera.
- MMKV.
- Zustand.
- Offline-ish upload queue with retry, progress, and local file cleanup.
- Video recorder with SOP overlay.
- Team/user management screens.

The weakness is domain and vendor coupling:

- Procurement-specific types: loads, vendors, procurement roles.
- Firestore/GCS are called directly inside services.
- Permissions are hardcoded in client code.
- SOP is a timed video overlay, not the Goat OS conditional form DSL.
- Upload idempotency is implicit around `goatId`, not a formal submission/event id.

Keep:

- Camera/video recorder UX.
- Upload queue pattern.
- Team/user UI ideas.
- Phone login flow idea.
- SOP overlay visual pattern.

Replace:

- Direct Firestore/GCS write path.
- Procurement-specific app model as the main operator model.
- Client-authoritative permission checks.
- Timed SOP config as the canonical form engine.

### Public Website

The website is out of scope for Goat OS core execution. It was inspected only because it exists in the workspace and the public URL helped explain the business context:

- Vite React.
- Strong branded public experience.
- Mostly content/media/storytelling.

Do not spend Goat OS core engineering time here. If the public site ever needs Goat OS data, it should talk only through public APIs for approved public stats or leads. It must never read operational databases or analytics tables directly.

### Slack Automation

Slack/App Script is legacy and migration surface:

- Keep Slack notifications and cutover bridges.
- Do not keep Slack forms as canonical SOP execution.
- Any Slack inbound action must pass through Goat OS API validation, permissions, audit, idempotency, and ledger/outbox.

## Final Frontend Direction

Use an SSR-first, surface-separated frontend architecture.

The legacy dashboard surface is already large. The answer is not one giant
client-rendered admin bundle. Goat OS frontends must be split by product surface
and rendered/lazy-loaded so each user pays only for the surface and module they
open.

```text
apps/
  admin-web/             internal SSR command center initialized from dashboard snapshot
  investor-web-shadow/   temporary investor/reduced validation copy
  operator-mobile/       React Native Android app for field operators
  public-web/            future public/partner surface only if Goat OS owns it

packages/
  ui/                    shared buttons, charts, tables, shells, tokens
  api-client/            generated clients for Goat OS app APIs
  auth-client/           auth/session adapter for web + mobile
  rbac/                  role/permission helpers for UI gating only
  forms-dsl/             shared DSL types, validators, preview helpers
  mobile-forms-runner/   RN renderers for Goat OS form fields
  analytics-client/      typed client for dashboard metrics
  media-client/          upload/download/proof helpers
  device-client/         camera/RFID/scale scanner abstractions
  config/                environment config, feature flags, app constants
```

This can live in a monorepo. The important boundary is not the folder name; it
is that each surface can run and deploy independently when needed, while sharing
typed contracts and UI packages.

## Frontend Framework Version Baseline

Freeze the Goat OS admin-web framework baseline before building real Phase 1 UI
screens. Do not build new screens on the copied dashboard's older Next 14 /
React 18 stack.

Version baseline checked on 2026-06-11:

```text
next                16.2.9
react               19.2.7
react-dom           19.2.7
eslint-config-next  16.2.9
typescript          6.0.3
@types/react        19.2.17
@types/react-dom    19.2.3
tailwindcss         4.3.0
@tanstack/react-query 5.101.0
lucide-react        1.17.0
recharts            3.8.1
```

The next admin-web implementation slice must upgrade and pin these framework
packages first, then run the build/typecheck before adding feature screens. If a
package has a peer-compatibility issue during the upgrade, pin the latest
compatible version, document the reason in this section and in BUILD-STATUS, and
do not silently fall back to the copied dashboard versions.

Use the modern Next App Router stack:

- Server Components and server-side data loading for read-heavy dashboard pages.
- server-only backend fetch adapters so bearer tokens never reach the browser.
- Server Actions only for future write workflows after the backend action exists.
- dynamic imports for heavy client-only charts/tables.
- route-level loading/error states.
- backend pagination and shaped summaries instead of client-side full-herd
  transforms.

Client library rules for Goat OS web surfaces:

```text
Next.js App Router
  owns routing, SSR, Server Components, Route Handlers, and Turbopack builds

React
  owns component composition and small interactive client islands

Tailwind CSS
  owns styling tokens/utilities

shadcn-style components
  are local source components, not a runtime framework dependency

TanStack Query
  is for client-interactive API views that need refetching, pagination controls,
  or optimistic UI. It is not the default data-loading path for read-heavy SSR
  pages; fetch those on the server first.

Zustand
  may hold local UI state only: selected tab, open drawer, temporary filters,
  chart/table view mode. Do not store canonical goat lists, passports, auth
  tokens, or backend truth in Zustand.

Recharts
  is for chart components and must be dynamically imported when it would bloat
  the initial route.

Auth.js
  is deferred until production web sessions are designed. Phase 1 local UI uses
  backend bearer auth/dev tokens server-side only. Auth.js must not replace
  backend RBAC or `user_scope_grants`; it can only become a web session adapter
  in front of the same backend authority.
```

`investor-web-shadow` is a migration safety surface, not a forever architecture
decision. It lets the team validate the investor/reduced dashboard separately
while current live URLs continue running. After parity is proven, investor can
become a separate Next.js zone/deploy target from the shared packages if its
security/cadence differs from internal admin.

## Surface Separation Decision

Goat OS uses microfrontend discipline at the **surface** boundary, not per small
feature. The surface boundaries are:

- internal admin/CEO command center
- investor/external read surface
- operator/device field surface
- public/partner surface, only when Goat OS owns product data there

For web surfaces, prefer **Next.js Multi-Zones** or separate Next apps routed by
path/domain when a surface needs an independent deploy/security boundary.
Multi-zones fit this surface granularity because each surface is a full app and
does not require runtime shared-dependency negotiation.

Use **Module Federation** only when a surface truly needs runtime
module-into-host remotes. Do not federate every feature tab. Per-feature
federation would add version skew, singleton negotiation, routing, auth, and
runtime failure tax without solving the legacy dashboard lag by itself.

Phase 1 has one web surface: `admin-web`. Build it with the same boundary
discipline now:

- SSR/server components for read-heavy pages wherever possible.
- route-level modules for goat passport, import review, analytics counts, and
  future devices/workforce areas.
- standalone module entrypoints for local/demo testing.
- dynamic imports for heavy charts/tables and client-only widgets.
- backend pagination and shaped summaries; no client-side full-herd scans.
- no cross-module deep imports; shared code moves through `packages/ui`,
  generated API clients, auth, RBAC, and typed public module interfaces.
- the first slice that creates real admin feature modules must add a
  `check-boundaries.sh` guard or equivalent CI check so one feature module
  cannot deep-import another feature module's internals.

When surface #2 lands, split at the surface boundary with Next zones/separate
apps first. Reach for Module Federation only if a measured need appears inside a
surface.

## No-Rewrite Surface Migration Rule

Phase 1 `admin-web` is the future `/admin` zone. Build it that way from the
start. A later investor/public/operator web split must be additive:

```text
apps/admin-web      -> future /admin surface
apps/investor-web   -> future /investor surface
apps/public-web     -> future /public or partner surface, only if needed
```

Adding `apps/investor-web` later is not a rewrite of `admin-web` if these rules
are followed now:

- feature modules live behind public entrypoints;
- no cross-feature deep imports;
- reusable UI lives in `packages/ui` or shared components, not copied between
  surfaces;
- API access goes through generated clients and server-side adapters;
- auth/session/RBAC helpers are shared packages/adapters;
- frontend code never reads Sheets, BigQuery, CSVs, local files, or databases
  directly;
- backend data is paginated/shaped before it reaches UI modules.

The rewrite failure mode is a giant client-rendered dashboard page with random
cross-imports, direct data access, and business/data logic buried inside React
components. Do not build that. If a future surface requires its own deploy,
create a new Next app/zone beside `admin-web` and reuse the shared packages.

## Dashboard Role Model

The internal admin surface should serve internal roles from one SSR shell.

```text
same app + same routes + same components
  -> user logs in
  -> app-api returns role, farm scope, feature grants
  -> sidebar/cards/filters/data are filtered by grants
```

The target product does not require separate codebases per role:

- CEO/internal: full internal command center, richer operational detail.
- Investor/external: sanitized read model, less detail, no internal operator names, no raw proof unless explicitly granted.
- Park head: farm-scoped health/tasks/verification/workforce.
- Verifier: verification queues and proof review.
- Operator: not a dashboard user by default; mobile task/form app only.

The existing two live URLs stay untouched until the new Goat OS dashboard is
validated. New preview/staging URLs must be separate so old live dashboards can
continue serving users during the rewrite.

## Frontend Data Access Rule

No frontend reads a database or warehouse directly.

```text
Next/RN client
  -> Goat OS app-api / analytics-api
  -> domain services / Cube / Tinybird / signed media APIs
```

For dashboards:

```text
current:
  React page -> /api/counts -> BigQuery SQL

target:
  React page -> analytics-client -> analytics-api -> Cube metric -> BigQuery/Tinybird underneath
```

Current Goat OS admin-web readiness foundation:

```text
apps/admin-web
  buildable Next shell with disabled Phase 1 tabs
  no executable app/api BigQuery or Sheets routes
  no lib/bigquery.ts data path
  imports @goatos/api-client from packages/api-client

packages/api-client
  generated OpenAPI TypeScript types for app/admin/analytics APIs
  generated-client drift is part of the contract guardrail flow
```

For mobile:

```text
current:
  RN service -> Firestore/GCS directly

target:
  RN feature -> api-client/media-client -> app-api -> Postgres/GCS/outbox
```

## Backend API Shape

The Go backend is a modular monolith with ports/adapters.

```text
cmd/goatos-api/
  HTTP app APIs for web/mobile/admin

internal/
  identity/
  tasks/
  sop/
  forms/
  verification/
  media/
  vaccination/
  health/
  workforce/
  devices/
  analytics_export/
  permissions/

platform/
  auth/
  outbox/
  pubsub/
  storage/
  notifications/
  observability/
```

Each module has:

```text
domain/        entities, value objects, domain rules
app/           use cases, commands, transactions
ports/         interfaces the module needs
adapters/      Postgres, Pub/Sub, GCS, Firebase, Slack, camera vendor, etc.
api/           HTTP handlers, request/response DTOs
repo/          module-owned persistence
```

Dependency rule:

```text
api -> app -> domain
app -> ports
adapters -> ports
domain imports nothing external
```

This is how components stay swappable without turning the code into a mess.

## Adapter Ports To Define

These are the first-class plug points:

```text
AuthProvider
  VerifyToken, ResolveUser, ResolveSession, MapClaimsToPrincipal
  adapters: Firebase Auth, OIDC/Auth0/Keycloak/custom

PermissionPolicy
  Can(principal, action, resource)
  adapters: Postgres policy tables, OPA/Cedar if needed

AnalyticsGateway
  QueryMetric, QueryDataset, QueryLiveSeries
  adapters: Cube, Tinybird, BigQuery, mock

MediaStorage
  CreateSignedUploadURL, CreateSignedReadURL, FinalizeUpload, DeleteObject
  adapters: GCS, S3/minio

NotificationGateway
  NotifyUser, NotifyRole, NotifyChannel
  adapters: FCM, Slack, WhatsApp/SMS, email

DeviceObservationGateway
  IngestObservation, NormalizePayload
  adapters: RFID, camera booth, scale, ultrasound, collar, manual mobile capture

FormDefinitionStore
  GetPublishedForm, PublishForm, ValidateVersion
  adapters: Postgres canonical store, local fixture for tests

SyncQueue
  EnqueueSubmission, Retry, MarkAcked, DeadLetter
  adapters: mobile SQLite queue, server outbox, test memory queue

AIProvider
  RunInference, SubmitForBatch, GetModelVersion
  adapters: Vertex/Gemini, custom model service, offline stub
```

The app code talks to ports. Vendors/tools sit behind adapters.

## Mobile Architecture

The Android app should be task-first:

```text
operator-mobile/
  src/app/                 navigation, boot, session
  src/features/tasks/      assigned tasks, weekly schedule, carry-forward
  src/features/forms/      Goat OS DSL runner
  src/features/media/      camera, proof capture, compression, upload
  src/features/goats/      goat lookup, RFID/manual tag, passport summary
  src/features/sync/       offline queue, idempotency, conflict handling
  src/features/devices/    RFID/scale/camera/ultrasound adapters
  src/features/profile/    operator profile and entry logs
  src/shared/ui/
  src/shared/api/
  src/shared/storage/
```

Recommended local storage:

- SQLite for task/form/submission queues, because this is relational and queryable.
- MMKV for small session/config/cache flags.
- Local filesystem for pending media.

Mobile submission flow:

```text
task opens pinned form_version
  -> form DSL renders offline
  -> proof captured through MediaCapturePort
  -> submission saved locally with idempotency_key
  -> media upload uses signed URL
  -> app-api submit revalidates form_version + permissions + current state
  -> one server tx creates form_submission + typed event + verification record + outbox
  -> mobile marks submission acked
```

Client checks are for UX. Server validation is authoritative.

## Dashboard Migration Plan

Keep the current dashboard UI and swap the data source underneath it.

Sequence:

1. Keep reusable visual shell/components from the copied dashboard app.
2. Generate TypeScript clients from Goat OS app/admin/analytics OpenAPI.
3. Build real Phase 1 tabs against the generated client package.
4. Keep disabled placeholders for incomplete endpoints instead of wiring stubs
   into UI flows.
5. Gate routes and API responses by server-side auth/RBAC.
6. Build CEO/internal views inside the internal admin surface, and keep
   investor/external views as their own surface when cadence or security differs.
   Both surfaces must share typed packages, generated clients, and governed
   analytics APIs instead of copying data logic.

Do not change every chart at once. The win is moving data access behind contracts, then cutting pages over safely.

## Backend Build Plan

Build the backend with contracts first:

1. OpenAPI contracts for mobile/admin/dashboard APIs.
2. Generated TypeScript clients for Next/RN.
3. Go handlers implement those contracts.
4. Module-owned Postgres tables.
5. Server-side RBAC on every command/query.
6. Outbox on every canonical mutation.
7. Pub/Sub relay for analytics/export/notifications.
8. Signed media upload/read APIs.
9. Observability from day one: request latency, queue lag, failed submissions, outbox lag, media failures.

REST/JSON is the right external API for web/mobile. gRPC/protobuf can be used for internal high-throughput services when that workload exists, but app clients should start with OpenAPI-generated clients because it keeps RN/Next integration simple and typed.

## What We Build From Scratch

Build fresh:

- Go modular monolith.
- Operational Postgres schema.
- App APIs.
- Auth/RBAC server policy.
- Goat OS form DSL and validator.
- Form builder/editor over the DSL.
- Native mobile form runner.
- Analytics semantic API integration.
- Outbox/Pub/Sub relay.
- Media signed URL pipeline.
- Device and AI adapter interfaces.

Reuse:

- Existing dashboard visual shell and components.
- Existing dashboard page inventory.
- Existing mobile camera recorder ideas.
- Existing mobile upload queue idea.
- Existing team/user UI ideas.

## Non-Negotiables

- No direct BigQuery/Sheets/Firestore/GCS access from frontend clients.
- No dashboard route without auth and server-side scope checks.
- No official KPI outside Cube/governed semantic layer.
- No arbitrary JS in form conditions.
- No client-only permission enforcement.
- No media upload through the API server body; use signed upload URLs.
- No vendor SDK spread across app code; SDKs live in adapters.
- No giant client-rendered dashboard bundle. Use SSR, route-level modules,
  dynamic imports for heavy widgets, backend pagination, and surface boundaries.
- No per-feature runtime federation. Surface-level Next zones/separate apps are
  preferred when admin, investor, operator, or public surfaces need independent
  deployment/security. Module Federation requires an explicit decision because
  it adds runtime shared-dependency/versioning complexity.
- No cross-module deep imports; feature modules use public interfaces and shared
  packages. Once feature-module directories exist, enforce this in CI.
- Every command/submission has an idempotency key.
- Every adapter has a fake/mock implementation for tests and local development.

## Immediate Engineering Plan

1. Freeze current dashboard response contracts.
2. Extract shared chart/UI primitives from `dashboard` into a `ui` package or keep them as a source folder until monorepo migration.
3. Add `analytics-client` and replace direct page fetches with typed functions.
4. Create `goatos-core` Go skeleton with modules, ports, adapters, OpenAPI, and Postgres migrations.
5. Create `operator-mobile` skeleton from `procurement_app`, keeping camera/upload code but replacing Firestore services with API/media adapters.
6. Implement auth once at API boundary: token verification, principal, roles/scopes/farm scope.
7. Build the vaccination/task/form loop end to end using the final forms DSL and signed media flow.
8. Convert existing dashboard pages one by one to analytics API/Cube metrics.
9. Keep Slack bridge for notifications and legacy ingestion only.

This gives proper SOLID boundaries without pretending every module must be independently deployed.
