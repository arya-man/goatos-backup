# Existing Repos Deep Audit

ARCHIVED / NOT AUTHORITATIVE.

This file is historical planning material. Do not use it as build instruction
unless a human explicitly asks for archive comparison.

This is the concrete repo-by-repo check, beyond cloning.

## Bottom Line

The existing repos already contain a lot of operational knowledge, but no repo is the new Goat OS core.

```text
Use existing repos as:
  requirements
  migration references
  UI references
  analytics references
  workflow rule sources

Do not use them as:
  canonical Goat OS backend
  event ledger
  task engine
  vaccination engine
  verification engine
  HR roster engine
```

## 1. procurement_app

Path:

```text
<mesha-workspace>/procurement_app
```

Stack:

```text
Vanilla React Native CLI
TypeScript
React Native Firebase Auth / Firestore / Storage / Messaging
React Navigation
Vision Camera
react-native-video
MMKV
Zustand
```

It is **not Expo**.

### Screens Present

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

### Tabs Present

```text
Loads
Team
SOP Configuration
Profile
```

Team tab is shown to:

```text
admin
procurement_head
procurement_manager
```

SOP tab is shown to:

```text
admin
```

### Firestore Shape Present

```text
loads
loads/{loadId}/goats
config/sop
users
```

### Types Present

```text
Farm = CBE | CPT

Role =
  admin
  procurement_head
  procurement_manager
  procurement_assistant_manager

LoadStatus =
  ACTIVE
  COMPLETED

VideoStatus =
  NOT_STARTED
  RECORDING
  RECORDED
  UPLOADING
  UPLOADED
  FAILED

Load:
  manualLoadId
  date
  vendor
  source
  destinationFarm
  breed
  gender
  status
  createdBy
  assignedTo
  goatCount
  uploadedCount

Goat:
  manualId
  gender
  videoStatus
  uploadProgress
  videoUrl
  recordedBy
  recordedAt
  sopSteps

SOPStep:
  key
  label
  subChecks
  durationSeconds
  gender
  timestampOffset
```

### Service Functions Present

From `src/services/firestore.ts`:

```text
getSOPConfig
saveSOPConfig
subscribeToSOPConfig
createLoad
subscribeToLoads
subscribeToLoad
updateLoad
deleteLoad
addGoat
subscribeToGoats
updateGoat
deleteGoat
checkDuplicateManualId
checkDuplicateManualLoadId
```

From `src/services/users.ts`:

```text
getUserByPhone
createUser
updateUser
deactivateUser
getAssignableUsers
subscribeToUsers
```

From `src/services/permissions.ts`:

```text
canManageLoadsForUser
canViewAllLoadsForUser
canAddGoatsForUser
canManageGoatsForUser
canViewLoadForUser
canViewTeamsForUser
canCreateUsersForUser
canAssignRole
canManageTeamMember
```

### SOP Config Present

Default SOP steps:

```text
overall_activity
head_mouth
body
extremities
reproductive
rectal_temp
```

Each step has:

```text
label
subChecks
durationSeconds
gender applicability
timestampOffset
```

### Video / Offline Upload Present

Already implemented:

```text
local MMKV upload queue
upload retry backoff
upload concurrency = 2
local file existence recovery
stuck upload recovery
Firebase Storage putFile
download URL writeback
upload progress writeback
goat videoStatus state machine
```

This is important for Goat OS v1 because vaccination proof video upload has the same shape.

### What To Reuse

```text
React Native app structure
VideoRecorderScreen UX
SOPOverlay
SOPEditorScreen patterns
MMKV upload queue idea
Firebase mobile integration lessons
Role/permission UI patterns
Load/goat capture UI flow as reference
```

### What Not To Reuse Directly

```text
Firestore nested shape as canonical data model
loads/{loadId}/goats as Goat OS identity
procurement-specific roles as final HR model
client-side state transitions as source of truth
```

Reason:

```text
Goat OS needs backend-owned typed events, task state, verification state, and canonical goat identity.
```

## 2. dashboard

Path:

```text
<mesha-workspace>/dashboard
```

Stack:

```text
Next.js
TypeScript
Tailwind
React Query
Recharts
BigQuery
```

### App Pages Present

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

### API Routes Present

There are **52 API routes**.

Important groups:

```text
/api/counts
/api/summary
/api/births/*
/api/mortality/*
/api/feed/*
/api/infra/*
/api/shiftings/*
/api/purchase-cost
/api/fattening/*
/api/goats-health
/api/milking-mothers/*
/api/parent-stock
/api/sales
/api/vaccination
```

### BigQuery Project / Datasets

Default project:

```text
goatos-sheets
```

Datasets referenced:

```text
goatsDB
farm
procurement_farm
Shiftings
feedDB
salesDB
crop_season
ceo_dashboard
healthDB
```

### BigQuery Tables / Views Referenced

Counts:

```text
counting_db_with_holding_dev
counting_kpis_daily
daily_summary_dev
core_farm_genderwise
```

Births / breeding:

```text
mother_kid_facts
birth_analysis_view
breedwise_kidding_8m
v_birth_count_last_10_days
kidding_frequency
parent_stock_table
```

Fattening / growth:

```text
growth_farmwise_weighing
adg_summary_age_shed
kids_counting_vs_weighing
adg_goat_last2
weighingprogression_loadwise
loadwise_summary
fattening_load_sales_comparison
```

Feed:

```text
last_10_loads_feedwise
feed_daily_spend
feed_daily_expense_feedwise
feed_breed_age_daily
last_7_days_feed_per_animal
last_7_days_feed_per_animal_shedwise
feedDB_clean
feedDB_load_summary
monthly_animals_vs_feed
seasons_clean
```

Mortality / health:

```text
mortality_overall_breedwise_dev
overall_farmwise_mortality_dev
load_Wise_pct_data
deaths_monthly_trend_v
mortality_genderwise
mother_litter_size_dev_breedwise
mother_litter_size_dev_overall
mortality_by_litter_size_overall_dev
mortality_this_month_dev
mortality_trend_dev
deaths_fact_dev
health_db_clean_dev
```

Infra:

```text
shed_capacity_count_dev
counting_shed_capacity_status_dev
```

Sales:

```text
salesDB_clean
monthly_feed_vs_sales
```

Vaccination:

```text
vaccination_dashboard
```

### Domain Constants Present

```text
Breeds:
  Beetal
  Anantapur
  Kenguri
  Sojat
  Malai
  Osmanabadi
  Boer

Farm tabs:
  overall
  core-farms
  cbe
  cpt
  holdings

Shed capacities:
  many named sheds including Gandhi, Godel, Mandela, Yashoda, Sumathi, Castro, Ho Chi Minh, Q sheds
```

### What To Reuse

```text
Analytics vocabulary
BigQuery table/view mapping
Dashboard components
Metric definitions as seed for analytics semantic layer
Shed capacity constants
Breed/farm naming conventions
CEO dashboard patterns
```

### What Not To Reuse Directly

```text
Do not make dashboard the operational backend.
Do not let BigQuery become canonical Goat OS truth.
Do not copy query APIs as app-api.
```

Dashboard is an analytics consumer over Sheets/BigQuery, not Goat Ops Core.

## 3. vgoats-dashboard

Path:

```text
<mesha-workspace>/vgoats-dashboard
```

Stack:

```text
Next.js
TypeScript
BigQuery
```

It has **34 API routes**.

It is an older/lighter version of `dashboard`.

Missing compared with `dashboard`:

```text
fattening
goats-health
milking-mothers
parent-stock
sales
vaccination
extra feed APIs
extra counts APIs
extra shiftings/cpt route
```

Recommendation:

```text
Use dashboard/ as canonical analytics reference.
Use vgoats-dashboard/ only as historical fallback.
```

## 4. slack-automation-scripts

Path:

```text
<mesha-workspace>/slack-automation-scripts
```

Stack:

```text
Google Apps Script
Slack workflows/lists/buttons
Google Sheets
Val Town / Deno proxy files
Some BigQuery/PDF generation
```

### Docs Present

```text
Automations Overview.docx
Dashboard Charts - BigQuery Mapping.docx
```

Automations Overview mentions:

```text
Unified Automation:
  Shifting
  Delivery
  Milk Preparation
  Deaths
  Procurement Transit
  Farming
  Harvesting

Birth/Abortion:
  user fills form
  report sent to birth-report channel

Farmer Crop Management:
  schedule-based seed sowing / crop work

Video Verification:
  deliveries
  feed packing
  feed distribution
  feed transit
  treatment

Feed Transit:
  video evidence uploaded in message thread during feed unloading for each shed
```

Dashboard mapping doc contains detailed BigQuery chart mappings for:

```text
Counts
Mortality
Births
Fattening
```

### Automation Files Present

```text
counting_db_automation.js
farmer_crops_automation.js
feed_automation.js
health_db_automation.js
health_manager_attendance.js
history_Automation.js
procurement_db.js
shifting_death_automation.js
unified_automation.js
video_verification_system.js
farming_unified_val_town.ts
history_automation_val_town.ts
unified_automation_val_town.ts
```

### What Each Major Script Contains

`counting_db_automation.js`:

```text
Shed tag loading
K0/exception shed rules
No-tag variants
Breed resolution
Applied-event dedup
Birth increment logic
Shifting application
FutureDB projection
Next-day counts
DB vs physical logic
```

`feed_automation.js`:

```text
Shed tag transforms
Count DB/FutureDB diffing
Feed direction generation
Feed supply planning
Feed packing/consumption triggers
Thread tracking
Experiment shed support
Daily archives
```

`health_db_automation.js`:

```text
Symptoms intake
Problem IDs
Diagnosis
Treatment lists
Follow-up IDs
Adult/kid split
Slack workflow triggers
Health channels
Status handling
```

`health_manager_attendance.js`:

```text
Health manager mapping
Daily schedule generation
Slack list item creation/update
Media handling
Auto-assignment by timing/window
Attendance DB pointer/replay
```

`history_Automation.js`:

```text
Goat history query
Slack command handling
BigQuery access
PDF generation
Birth/delivery/growth/shift/health sections
Slack PDF upload
Rate limiting
```

`procurement_db.js`:

```text
Procurement Slack messages
Slack file/video handling
Thread tracking
Permalink writing
Video prompt posting
Dedup/debug utilities
```

`shifting_death_automation.js`:

```text
Shifting ID generation
Goat ID parsing
Scheduled date computation
Shed tag lookup
Shifting form to DB push
Death report processing
Hourly status sync
```

`unified_automation.js`:

```text
Config-driven form relay
Workflow/action IDs
Redis-style locking
Queued sheet writes
5-minute drain trigger
Automated shifting config
Report-type config
Milk/feed cascade flows
```

`video_verification_system.js`:

```text
Media correctness defaults
Birth verification
Milk preparation verification
Feed transport verification
Treatment verification
Shiftings verification
Slack rejection notifications
Feed packing checks
Feed DB writeback
Stock automation
```

Val Town proxies:

```text
Slack callback fast path
Modal open within Slack 3-second limit
Redis lookup for action/modal state
Forward slow work to Apps Script web app
Milk-feeding cascade modal flow
Reschedule modal flow
```

### What To Reuse

```text
Actual business rules
Scheduling edge cases
Slack/task vocabulary
Verification rejection paths
Feed/health/shifting/death workflows
Applied-event/dedup concepts
Shed exceptions
Current operational pain points
```

### What Not To Reuse Directly

```text
Apps Script as new backend
Sheets as canonical database
Slack as task engine
Hardcoded secrets
One-off sheet column logic
```

Security:

```text
This repo contains hardcoded Slack tokens/webhooks.
Rotate before wider use.
```

## 5. website

Path:

```text
<mesha-workspace>/website
```

Stack:

```text
Vite
React
TypeScript
Tailwind
Framer Motion
GSAP
Firebase Hosting
```

### Routes Present

```text
/
/parks
/technology
/about
```

### Components Present

```text
Hero
HeroAmbientVideo
HeroTitle
ScrollFilm
StoryOverlay
StoryScrubVideo
StoryGestureHint
GoatOSPanel
OperatingSystemSection
ExchangeCard
ExchangeRewardBurst
InvestMintRewardProvider
Parks video backgrounds
MeshaMark
Nav
Footer
```

### Technology Page Already Says

```text
Genetics creates better animals.
Breeding tech makes biology reproducible.
goatOS and goatSense make it measurable at scale.
FaceID for goats and sheep.
100k+ images and videos.
MESHA Exchange facilitates purchase, managed ownership, payouts, records, and liquidity around real livestock.
```

### Parks Page Already Covers

```text
CBE
CPT
Hindupur Industrial Goat Park
100 acres
1 lakh goats
₹120 Cr+
```

### What To Reuse

```text
Public website
Brand system
Technology positioning
GoatOS/goatSense copy
Parks copy/assets
MESHA Exchange public narrative
```

### What Not To Reuse

```text
No internal ops backend here.
No Goat OS app-api here.
No operational source of truth here.
```

## Cross-Repo Conclusions

### Already Exists

```text
Public MESHA website
Procurement Android app with video proof
SOP editor/overlay concept
Upload queue concept
Slack/Sheets operational automations
Video verification workflows in Slack/Sheets
BigQuery dashboard/analytics layer
BigQuery table/view map
Operational rules for feed, health, shifting, deaths, counting, procurement
```

### Does Not Exist Yet

```text
Goat Ops Core backend
App API layer
Canonical goat identity
Event envelope
Typed event tables
Ledger timeline projection
Vaccination task engine
Config engine backend
HR weekly roster backend
Video verification routing backend
Proper media/proof model
Legacy import pipeline into new schema
```

### Best Reuse Strategy

```text
Build new Goat OS core from scratch.
Use procurement_app as Android UX reference.
Use slack-automation-scripts as business-rule reference.
Use dashboard as analytics/semantic-layer reference.
Use website as public-web reference.
Ignore vgoats-dashboard unless dashboard is missing something.
```

### Direct Input To V1 Schema

The v1 schema should cover:

```text
users / roles / farms
goats canonical identity
legacy import staging
vaccine config
vaccine stock
goat vaccination schedule
vaccination task
task assignment
media proof
verification review
weekly roster
event envelope
ledger projection
operational reporting views
```

### Direct Input To Android V1

The v1 Android app can borrow concepts from procurement_app:

```text
phone login
role-gated tabs
task list
SOP overlay
video recorder
confirmation screen
local upload queue
retry/backoff
video playback
profile/team screens
```

But it should point at new Goat OS app-api, not Firestore `loads/{loadId}/goats` as canonical state.
