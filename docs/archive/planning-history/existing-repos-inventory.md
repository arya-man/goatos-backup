# Existing VGoats / MESHA Repos Inventory

ARCHIVED / NOT AUTHORITATIVE.

This file is historical planning material. Do not use it as build instruction
unless a human explicitly asks for archive comparison.

Checked out under `<mesha-workspace>` from the `vgoats` GitHub org.

## Repos Cloned

```text
slack-automation-scripts/
website/
procurement_app/
dashboard/
vgoats-dashboard/
```

## Executive Take

There is useful existing work, but there is no clean Goat OS backend core yet.

What exists today is:

```text
Slack/Sheets automation layer
Public MESHA website
Procurement/video Android app
BigQuery CEO/dashboard layer
Older dashboard variant
```

What does not exist yet:

```text
goat-ops-core
event envelope / ledger
config engine as backend service
task engine as backend service
vaccination v1 backend
verification routing backend
HR roster backend
canonical goat identity service
proper app-api layer
```

So yes: build the new Goat OS core from scratch, but reuse product learnings, UI pieces, workflow details, data mappings, and dashboard queries from these repos.

## Repo: slack-automation-scripts

Path: `<mesha-workspace>/slack-automation-scripts`

Type:

```text
Google Apps Script + Slack automation + Val Town/Deno proxies
```

What exists:

```text
counting_db_automation.js
feed_automation.js
health_db_automation.js
health_manager_attendance.js
history_Automation.js
procurement_db.js
shifting_death_automation.js
unified_automation.js
video_verification_system.js
*_val_town.ts proxy files
```

Useful for Goat OS:

```text
Existing SOP/workflow behavior
Slack task/status vocabulary
Health follow-up logic
Feed automation rules
Shifting/death workflow rules
Video verification process
Counting/reconciliation edge cases
Exception shed logic
Scheduling rules from real operations
```

Do not reuse as production core:

```text
Too coupled to Sheets and Slack
No clean data ownership
No typed event model
No app/API boundary
Hard to test
Hard to scale
```

Security note:

```text
Multiple scripts contain hardcoded Slack tokens/webhooks or similar secrets.
Rotate/remove secrets before any public sharing or migration.
```

## Repo: website

Path: `<mesha-workspace>/website`

Type:

```text
Vite + React + TypeScript + Tailwind + Firebase Hosting
```

Routes:

```text
/
/parks
/technology
/about
```

What exists:

```text
Premium public MESHA website
GoatOS/goatSense public storytelling
Technology page with genetics, FaceID/Aadhar, physical infra, MESHA Exchange
Parks pages for CBE, CPT, Hindupur
Cinematic video/story system
Brand rules and operating manual
```

Useful for Goat OS:

```text
Public-web foundation
Brand language
Website content structure
Investor/customer narrative
Technology positioning
Mesha visual system
```

Do not reuse as core:

```text
This is public-web only.
No internal Goat OS backend.
No operational database.
```

## Repo: procurement_app

Path: `<mesha-workspace>/procurement_app`

Type:

```text
Vanilla React Native CLI app, not Expo
Firebase Auth, Firestore, Storage, Messaging
MMKV local queue
Vision Camera / video playback
Zustand stores
```

Screens:

```text
LoginScreen
AllLoadsScreen
CreateLoadScreen
EditLoadScreen
LoadDetailScreen
AddGoatScreen
EditGoatScreen
VideoRecorderScreen
GoatVideoScreen
ConfirmationScreen
SOPEditorScreen
UsersScreen
AddUserScreen
EditUserScreen
ProfileScreen
```

Data model present:

```text
loads
loads/{loadId}/goats
config/sop
users
```

Important concepts already implemented:

```text
Phone login
Roles and permissions
Farm access: CBE / CPT
Load creation and assignment
Goat creation inside load
Per-goat video status
SOP step config
SOP overlay during recording
Offline-ish local upload queue with retry/backoff
Firebase Storage video upload
Upload progress writeback
Video playback
Admin-only SOP editor
```

Most reusable pieces:

```text
Video recorder UX
SOP overlay/editor patterns
Upload queue idea
Role/permission UI idea
Load/goat capture flow
Firebase mobile integration lessons
```

Do not reuse directly as final Goat OS:

```text
Firestore shape is procurement-specific
Goats are nested under loads, not canonical goat identity
No event envelope
No vaccination/task/verification domain model
No bitemporal event records
No proper backend-owned state machine
```

Security note:

```text
Firebase google-services.json is checked in.
Usually client Firebase config is not a private secret, but review project exposure/rules before reuse.
```

## Repo: dashboard

Path: `<mesha-workspace>/dashboard`

Type:

```text
Next.js + TypeScript + Tailwind + React Query + Recharts + BigQuery
```

Current app areas:

```text
counts
summary
births
mortality
feed
infra
shiftings
purchase-cost
fattening
goats-health
milking-mothers
parent-stock
sales
vaccination
mis
```

BigQuery integration:

```text
Project default: goatos-sheets
Dataset defaults/overrides include:
goatsDB
farm
procurement_farm
Shiftings
feedDB
salesDB
crop_season
ceo_dashboard
```

Useful for Goat OS:

```text
Existing analytics vocabulary
BigQuery table/view names
Shed capacities
Breed list
CBE/CPT farm constants
Counts/reconciliation dashboard logic
Vaccination dashboard endpoint
Feed, mortality, birth, growth, shiftings, sales query examples
CEO dashboard UX/components
```

Important note:

```text
This is analytics over existing Sheets/BigQuery data.
It is not the operational source of truth.
Use it to design analytics/semantic layer and migration checks.
```

## Repo: vgoats-dashboard

Path: `<mesha-workspace>/vgoats-dashboard`

Type:

```text
Older Next.js dashboard variant
```

Overlap:

```text
Very similar to dashboard/
dashboard/ is newer and has extra pages/APIs:
fattening
goats-health
milking-mothers
parent-stock
sales
vaccination
more feed/count APIs
```

Recommendation:

```text
Treat dashboard/ as the current dashboard source.
Use vgoats-dashboard only for history/reference if something is missing.
```

## What We Should Reuse

Reuse as requirements/reference:

```text
slack-automation-scripts workflow rules
procurement_app mobile proof/video/upload UX
dashboard BigQuery tables/views and metrics
website brand/public-web
```

Reuse as code cautiously:

```text
procurement_app upload queue patterns
procurement_app SOP editor/overlay UX
dashboard BigQuery helper/table mapping
dashboard chart components
website public-web components
```

Do not use as foundation for new core:

```text
Apps Script as backend
Firestore nested procurement shape as canonical goat model
Sheets as database
Dashboard as operational backend
Slack workflows as task engine
```

## Mapping To New Goat OS

```text
slack-automation-scripts -> legacy_import + SOP requirements + task/verification behavior
website -> public-web
procurement_app -> android-field-app reference + proof/video UX
dashboard -> analytics
vgoats-dashboard -> old analytics reference
```

## Immediate Next Move

Build new v1 from scratch around:

```text
goat-ops-core
app-api
android-field-app
platform auth/permissions/outbox
task engine
config engine
verification engine
HR roster
vaccination module
legacy import
basic reporting
```

Start with:

```text
goatos-v1-schema.sql
event envelope
identity tables
vaccination schedule/dose tables
task tables
media/proof tables
verification tables
workforce/weekly roster tables
legacy import staging
```

## Cleanup Before Reuse

```text
Rotate/remove hardcoded Slack secrets from slack-automation-scripts.
Review Firebase rules/config in procurement_app before reuse.
Decide whether dashboard or vgoats-dashboard remains canonical; likely dashboard.
Keep existing repos as reference, not as the new monorepo/core.
```
