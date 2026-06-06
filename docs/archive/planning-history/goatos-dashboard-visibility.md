# Goat OS Dashboard Visibility

ARCHIVED / NOT AUTHORITATIVE.

This file is historical planning material. Do not use it as build instruction
unless a human explicitly asks for archive comparison.

## Decision

Use one analytics/dashboard product behind login. Do not keep two separate dashboard products long-term just because Firebase currently hosts two URLs.

```text
one dashboard app
  login
  -> auth provider verifies user
  -> Goat OS permissions resolves role + scope
  -> UI renders allowed navigation
  -> API returns only allowed metrics
```

Two Firebase URLs are acceptable as temporary deployments while we migrate. The target is role-based visibility, not separate codebases for each audience.

Operators are not dashboard users. Operators use the Android Field App for task execution, SOP entry, proof upload, entry logs, and proof status.

## Immediate Security Status

As of June 5, 2026, the current Firebase dashboard deployments should be treated as publicly reachable unless an external access control layer is confirmed.

```text
observed:
  dashboard pages render without a login gate
  dashboard API routes return JSON without an app login
  CEO/internal dashboard includes internal operational views
  reduced/investor dashboard includes Purchase Cost today

action:
  put both dashboard deployments behind Firebase Auth, Google Identity-Aware Proxy, or equivalent access control before sharing links further
  server-side APIs must reject unauthenticated requests
  hiding routes in the sidebar is not security
```

## Current Deployed Meaning

```text
dashboard--goatos-sheets.us-central1.hosted.app
  Current meaning: CEO/internal dashboard.
  Source repo: <mesha-workspace>/dashboard
  More detailed internal ops view.

vgoats-dashboard--goatos-sheets.us-central1.hosted.app
  Current meaning: reduced external/stakeholder dashboard.
  Source repo: <mesha-workspace>/vgoats-dashboard
  Team calls it investor dashboard, but code only says Dashboard/VGoats Dashboard.
```

## Current Visibility Snapshot

| Area | CEO/internal dashboard | Investor/reduced dashboard |
| --- | --- | --- |
| Counts | visible | visible |
| Mortality | visible | visible |
| Births | visible | visible |
| Fattening | visible | hidden today |
| Feed | visible | visible |
| Sales | visible | hidden today |
| MIS | visible | visible |
| Infra | visible | visible |
| Goats Health | visible | hidden today |
| Vaccination | visible | hidden today |
| Purchase Cost | visible | visible today, should be reviewed before external sharing |
| Shiftings | visible | visible |
| Parent Stock | visible | hidden today |
| Milk | visible | hidden today |
| Summary | visible | visible |

## Target Roles

```text
operator_field
  Uses Android Field App only.
  Sees assigned tasks, pending work, submitted entries, upload/proof status, rejection reasons, and limited own/team work history.
  Does not see CEO analytics, investor dashboards, company-wide finance, vendor details, or broad farm performance.

ceo
  Sees full internal analytics and ops health.
  Can drill into farm, shed, load, vendor, disease, feed, vaccination, sales, parent stock, milk.

admin_ops
  Sees operational command center and task/verification queues.
  Writes only through Goat Ops APIs, not analytics tables.

park_head
  Sees own park/shed/team data and ground verification queues.
  No company-wide finance/vendor drilldowns unless granted.

central_verifier
  Sees verification queues, proof videos, task status, rejection reasons.
  No finance/vendor/investor views.

investor_external
  Sees curated farm performance, herd growth, mortality trend, aggregate health, high-level feed/infra status, proof summaries, and last-updated time.
  Does not see raw vendor names, per-goat medical records, worker data, internal task queues, detailed purchase cost, or operational exceptions.
```

## Operator Surface Rule

Operators currently use Slack SOP/forms to submit work. Goat OS should replace the primary data-entry path with Android, while keeping Slack tightly integrated during migration.

```text
operator primary surface:
  Android Field App
  assigned tasks
  SOP form execution
  scan/RFID/manual goat lookup
  photo/video proof upload
  pending upload queue
  submitted entry log
  approval/rejection status
  comments/rework reasons

Slack bridge:
  Goat OS -> Slack: assignments, reminders, approvals, rejections, summaries
  Slack -> Goat OS: migration-period SOP/form submissions and admin actions only after Android-first flow is stable
  all inbound Slack actions go through Goat OS APIs, permissions, validation, audit, dedup, and idempotency

not for operators:
  analytics dashboard
  CEO dashboard
  investor/reduced dashboard
  company-wide reporting
```

## Investor View Rule

Investor dashboard is not "same dashboard with fewer menu items." It must be curated and permissioned.

```text
allowed:
  aggregate active goat count
  farm/park performance summaries
  mortality trends
  birth/growth trends
  feed trend summary
  infra/capacity summary
  high-level proof/verification completion
  ownership/payout view later
  last updated timestamp

review carefully:
  purchase cost
  shiftings
  MIS
  farm value

not allowed:
  vendor-level purchase cost
  individual goat health cases unless tied to owned asset and sanitized
  worker/team performance
  task execution queues
  raw videos
  internal disease details by shed if sensitive
  finance/payable internals
  Slack/form migration artifacts
```

## CEO View Rule

CEO/internal view should keep everything visible today and add missing Goat OS surfaces as they become real modules.

```text
keep from existing dashboard:
  Counts
  Mortality
  Births
  Fattening
  Feed
  Sales
  MIS
  Infra
  Goats Health
  Vaccination
  Purchase Cost
  Shiftings
  Parent Stock
  Milk
  Summary

add as Goat OS matures:
  task completion and overdue queues
  verification throughput and rejection reasons
  weekly vaccination schedule compliance
  worker/park assignment coverage
  vaccine stock and expiry
  goat passport search
  genetics/breeding performance
  device health and offline sync status
  reconciliation/missing-data reports
  data freshness and import health
```

## Implementation Direction

```text
frontend:
  Keep dashboard UI/components from <mesha-workspace>/dashboard as canonical.
  Do not fork investor UI into a second product unless compliance later forces it.
  Use route config + permissions to hide/show pages.
  Do not add operator dashboard navigation; build operator work history inside Android Field App.

backend:
  Auth provider verifies login token.
  Goat OS permissions decides roles and data scopes.
  Analytics API enforces role visibility server-side.
  UI hiding is convenience only; API must still block unauthorized metrics.
  Slack bridge uses the same permissions and audit model as Android; it is not a privileged backdoor.

analytics:
  Use one semantic metric definition source.
  Publish different read models for internal vs external users.
  Investor dashboards read external-safe snapshots, not raw operational tables.
```

## Migration Plan

```text
phase 1:
  Keep both Firebase URLs running.
  Gate both dashboard deployments with auth before sharing links further.
  Document current visibility.
  Add auth + role claims to the canonical dashboard.
  Keep Slack outbound updates flowing while Android field flows are introduced.

phase 2:
  Move CEO/internal users to canonical dashboard login.
  Recreate investor/reduced nav from role config.
  API filters data by role and scope.
  Move operator SOP entry from Slack-first to Android-first; Slack remains mirror/alerts.
  Add inbound Slack ingest only if migration still requires it, with task-level dedup.

phase 3:
  Retire vgoats-dashboard as a separate app.
  Keep it only as historical reference or fallback.
```
