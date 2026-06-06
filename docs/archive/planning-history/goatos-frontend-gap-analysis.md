# Goat OS Frontend Gap Analysis

ARCHIVED / NOT AUTHORITATIVE.

This file is historical planning material. Do not use it as build instruction
unless a human explicitly asks for archive comparison.

## Bottom Line

The frontend is not empty. We should keep the existing UI where it is strong, then plug it into the new Goat OS backend.

What exists today is mostly:

```text
analytics dashboards
  CEO/internal charts
  reduced investor/stakeholder charts

procurement mobile app
  load creation
  goat entry inside load
  video recording/upload
  SOP overlay
  team/users
  basic role-based tabs
```

What is missing is the actual Goat OS operational frontend: task execution, vaccination schedules, verification queues, workforce assignment, goat identity/passport, reconciliation, and Slack bridge controls.

## Existing Frontend To Keep

```text
<mesha-workspace>/dashboard
  Canonical analytics dashboard UI.
  Keep layout, dark visual system, chart components, pages, and BigQuery metric learnings.

<mesha-workspace>/vgoats-dashboard
  Older/reduced dashboard.
  Use only as reference for current investor/stakeholder visibility.
  Do not keep as a separate long-term product unless compliance forces it.

<mesha-workspace>/procurement_app
  Strong base for Android field app.
  Keep mobile login pattern, video recorder, SOP overlay, upload queue concept, team screens, profile, and role-based tab pattern.

<mesha-workspace>/website
  Public MESHA website.
  Keep separate from Goat OS operations.
```

## Existing Mobile App Shape

The mobile app currently has these surfaces:

```text
Loads
  AllLoads
  CreateLoad
  EditLoad
  LoadDetail

Goat entry
  AddGoat
  EditGoat
  VideoRecorder
  GoatVideo
  Confirmation

Admin/team
  Users
  AddUser
  EditUser
  SOPEditor
  Profile
```

This is useful, but it is still procurement/load-first. Goat OS needs task-first.

## Missing Android Field App Screens

```text
My Work
  Today's assigned tasks
  This week's schedule
  Pending tasks
  Overdue tasks
  Rework/rejected tasks

Task Detail
  SOP steps
  conditional SOP form
  required goat(s)
  location/shed
  instructions
  required media/proof
  offline status
  submit action

SOP Form Runner
  field types: text, number, date/time, select, multiselect, checkbox, scan, photo, video, signature
  conditional branching: show/hide field or step based on previous answers, goat status, task type, age, sex, pregnancy, location, or vaccine
  required proof rules: video/photo mandatory by step, min/max duration, retake reason, verifier comment
  validations: dose range, batch expiry, duplicate submission, due/not-due, wrong goat, wrong shed, missing proof
  offline drafts: save partial form, resume, sync later, preserve occurred_at vs recorded_at
  form versioning: task remembers the SOP form version used at execution time

Vaccination Execution
  scan/lookup goat
  show due vaccine
  dose/batch/expiry capture
  vaccinator and timestamp
  video/photo proof
  missed/deferred reason
  submit for verification

Entry Logs
  submitted entries
  upload queue
  approved/rejected status
  verifier comments
  personal/team history

Goat Lookup
  RFID/manual tag search
  basic goat passport
  status, age, sex, breed
  due tasks
  warnings: sick, pregnant, blocked, duplicate

Offline Sync
  pending uploads
  failed uploads
  retry controls
  last sync time
  conflict/reconciliation notices
```

Operators do not need analytics dashboards. This Android app is their product.

## Missing Admin / Command Center Screens

```text
Config Engine
  vaccination schedule setup
  birth/purchase schedule rules
  SOP template builder
  conditional form builder
  required proof rules
  validation rules
  rejection reason library
  task generation preview
  versioning and publish/rollback

Task Engine
  all tasks
  task state board
  pending/overdue/carry-forward
  bulk assignment
  escalation rules
  manual task creation

HR / Workforce
  weekly roster
  vaccination team assignment
  operator availability
  absence fallback
  park/shed ownership
  team workload

Verification
  Park Head ground verification queue
  central video verification queue
  approve/reject/rework
  rejection reason library
  verifier throughput
  trust score later

Vaccination Ops
  campaign dashboard
  due list by park/shed
  completion rate
  missed/deferred list
  vaccine stock/batch/expiry
  proof status

Legacy Import / Reconciliation
  import 50k goats
  duplicate tag resolution
  missing fields
  invalid goats/statuses
  old Sheets/Slack row mapping
  migration audit

Slack Bridge Admin
  Slack user mapping
  channel mapping
  SOP form mapping
  inbound event monitor
  failed Slack message retries
  permission/audit view
```

Slack Bridge Admin is not part of the Android-first v1 thin slice. v1 can post outbound Slack updates. Inbound Slack form ingestion is a migration/v2 capability because it creates a second data-entry path.

## Missing Analytics Dashboard Additions

Keep the current dashboard pages, then add Goat OS metrics when backend supports them.

```text
Task analytics
  completion %
  overdue tasks
  carry-forward count
  operator/team workload
  park/shed SLA

Verification analytics
  pending proof queue
  approval/rejection rates
  rejection reasons
  verifier throughput
  average verification time

Vaccination analytics
  due vs completed
  campaign progress
  missed/deferred
  stock usage
  batch/expiry risk

Data quality
  stale data
  missing proof
  duplicate goat IDs
  failed imports
  failed uploads
  Slack/API sync failures

Device/offline
  app sync health
  device health
  offline queue depth
  RFID/camera/scale gateway health later

Genetics/breeding later
  pedigree view
  breeding score
  parent stock performance
  embryo/semen records
  offspring performance
```

## Missing Investor View Work

Investor view should not be a separate product long-term. It should be role-based inside the analytics dashboard.

```text
need:
  role-based nav
  external-safe read models
  sanitized metric definitions
  last-updated timestamp
  no vendor-level details
  no operator/team data
  no raw videos
  no internal task queues
```

Current reduced dashboard visibility is documented in `goatos-dashboard-visibility.md`.

## Frontend API Work Needed

```text
Mobile app today:
  Firebase Auth
  Firestore loads/goats/config/users
  Firebase Storage videos
  procurement-style SOP overlay

Target:
  Auth provider verifies login
  Android app calls Goat OS API
  Goat OS API owns tasks, goats, conditional SOP forms, verification, media metadata
  GCS stores media
  Postgres stores canonical truth

Dashboard today:
  Next.js API routes query BigQuery/Sheets-derived tables

Target:
  dashboard reads analytics API / governed BigQuery projections
  role and scope enforced server-side
  charts remain mostly reusable
```

## What Not To Build Now

```text
Do not rebuild dashboard charts from scratch.
Do not create an operator analytics dashboard.
Do not keep CEO and investor as two permanent codebases.
Do not build full investor ownership portal in v1.
Do not build commerce/customer/qurbani frontend in v1.
Do not make Slack the source of truth.
```

## Frontend Build List

### P0 Security

```text
1. Gate current Firebase dashboard deployments behind auth.
2. Server-side dashboard/API routes must reject unauthenticated users.
3. Add role/scope checks before investor/reduced views are shared externally.
```

### V1 Thin Slice

```text
1. Android Field App: My Work
2. Android Field App: Task Detail + conditional SOP form execution
3. Android Field App: Vaccination proof capture
4. Android Field App: Entry Logs + upload/proof status
5. Admin Command Center: minimal vaccination config + conditional SOP form config
6. Admin Command Center: weekly vaccination roster/assignment
7. Admin Command Center: Park Head + central verifier queues
8. Legacy import/reconciliation review for seed herd + vaccine stock
```

### V1.5 / V2

```text
task board
bulk assignment
escalation-rule builder
advanced workforce capacity planning
Slack inbound bridge admin
full data-quality dashboard
device-health dashboard
role-based investor dashboard consolidation
genetics/breeding dashboards
commerce/customer/investor portal depth
```

That is the frontend work. Everything else is either already usable, backend/data work, or later-phase product.
