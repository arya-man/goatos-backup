# Goat OS Categories

ARCHIVED / NOT AUTHORITATIVE.

This file is historical planning material. Do not use it as build instruction
unless a human explicitly asks for archive comparison.

## Mental Model

Goat Ops owns goat truth. R&D produces observations, claims, and scores. Commerce owns demand, fulfillment, ownership, and money. Public owns the website. Platform owns shared rails.

## Top-Level Shape

```text
goatos/
├── apps/ [v1]
│   Product surfaces: Android Field App, Admin Command Center, role-based Analytics Dashboard, Investor Portal, Donor/Customer Portal.
│
├── app-api/ [v1]
│   App-facing API layer. Apps call context APIs through this layer and never touch databases directly.
│
├── goat-ops-core/ [v1]
│   Internal Goat OS: goat lifecycle truth, parks, workers, SOPs, proof, health, breeding, genetics, feed, infra, reconciliation.
│
├── commerce/ [v2]
│   External demand and money: bookings, orders, fulfillment, ownership, payments, settlements, meat-yield feedback.
│
├── public-web/ [v2]
│   Mesha website and lead capture: story, parks, technology, exchange, Bakrid goats, logins, contact, SEO.
│
├── platform/ [v1]
│   Shared rails: auth, permissions, audit, notifications, Slack outbound, migration bridges, outbox, context layer, analytics export, legacy import.
│
├── device-gateway/ [v1]
│   Cloud ingestion for RFID, MQTT, scales, cameras, ultrasound, collars, and shed sensors.
│
├── edge-agent/ [v1]
│   Farm-local offline agent for buffering device events and syncing when network returns.
│
├── ai-rnd/ [later]
│   AI/R&D family: FaceID, proof checks, age, weight, pregnancy, disease, genetics, meat yield, experiments.
│
└── analytics/ [v2]
    Business analytics: BigQuery warehouse, semantic metrics, role-based dashboards, AI analyst, model datasets.
```

## Apps / Product Surfaces

```text
apps/
├── android-field-app/ [v1]
│   Private Android app for operators, supervisors, admins, vets, verifiers: assigned work, conditional SOP forms, entry logs, scan, proof upload, approvals.
│   Operators do not need analytics dashboards; they need today's tasks, pending work, submitted entries, and proof status.
│
├── admin-command-center/ [v1]
│   Mobile/web/tablet control room: v1 vaccination ops; later reconciliation, genetics, inventory, fulfillment, device health.
│
├── investor-portal/ [v2]
│   Controlled ownership visibility: assets, payouts, documents, growth, health/proof timeline, last-updated dashboards.
│
├── customer-donor-portal/ [v2]
│   Booking and proof experience for qurbani, aqiqah, sadaqah, general purchase, weekly mutton, donations.
│
└── analytics-dashboard/ [v2]
    Single login-based dashboard surface. CEO/internal users see full ops; investors see curated external-safe summaries.
    Operators are not dashboard users; their surface is the Android Field App.
```

## Build Phases

```text
[v1]    Thin slice: vaccination end-to-end, one park/shed workflow, offline upload, proof, verification, reports.
[v2]    Expand goat lifecycle: health, breeding, genetics, growth, feed, procurement, reconciliation, commerce basics.
[later] Mature company/R&D scale: portals depth, advanced AI, production yield, settlements, support, park expansion tooling.

v1 loop:
  android-field-app -> app-api -> goat-ops-core -> verification -> reporting
  device-gateway/edge-agent -> goat-ops-core.devices -> goat-ops-core.vaccination

v1 seed data:
  goat population = legacy_import seed from existing herd
  vaccine stock = legacy_import seed or manual admin entry
  no live procurement intake in v1
  no live breeding/birth intake in v1
  verification is human-first; AI pre-check stays optional/later

analytics phasing:
  v1 analytics = goat-ops-core.reporting for operational vaccination/count reports
  v2 analytics = analytics/ for BigQuery facts, dashboards, semantic metrics, AI analyst

dashboard visibility:
  target = one dashboard app, role-based navigation and server-side metric filtering
  current CEO/internal pages = counts, mortality, births, fattening, feed, sales, MIS, infra, goats health, vaccination, purchase cost, shiftings, parent stock, milk, summary
  current investor/reduced pages = counts, mortality, births, feed, MIS, infra, purchase cost, shiftings, summary
  operator pages = none; operators use Android Field App task list and entry logs
  source of detail = goatos-dashboard-visibility.md

frontend gaps:
  existing FE is analytics + procurement/mobile workflow
  missing FE is task-first Goat OS: My Work, conditional SOP form runner, task detail, vaccination execution, verification queues, weekly roster, config engine, reconciliation
  source of detail = goatos-frontend-gap-analysis.md
```

## Goat Ops Kernel

```text
kernel/ [v1 product language, not a separate deployable]
├── config_engine/
│   Admin-configured vaccination schedules, conditional SOP form templates, intervals, assignment rules. Maps to sop + vaccination.
│
├── task_engine/
│   Creates, assigns, executes, reschedules, carries forward, and closes work. Maps to tasks.
│
├── video_verification_engine/
│   Proof review flow: Park Head ground verification + central office video verifier. Maps to media + verification.
│
└── hr_engine/
    Park HR roster, vaccination team weekly schedule, team ownership, absence fallback. Maps to workforce.

v1 vaccination flow:
  config_engine defines vaccine schedule
  config_engine defines the Android SOP form: conditional fields, required proof, validations, and rejection reasons
  legacy_import/manual entry seeds goats and stock
  vaccination generates due tasks
  hr_engine assigns next-week vaccination team schedule
  task_engine creates and tracks task lifecycle
  operator executes task and uploads proof
  Park Head verifies on ground
  central video verifier reviews proof and closes task
  pending/missed tasks carry forward to a future schedule

birth/purchase trigger:
  lifetime vaccine schedule is generated on birth or purchase/intake
  v1 uses legacy_import/manual intake; live birth/purchase intake starts in v2
```

## App API

```text
app-api/
├── field_app_api/ [v1]
│   Operator/mobile API surface for tasks, scans, media uploads, offline sync, and quick goat lookup.
│
├── admin_api/ [v1]
│   Admin command API surface for v1 vaccination ops; later read-only views across commerce/analytics.
│
├── portal_api/ [v2]
│   Customer/investor/donor API surface for bookings, ownership, proof, payouts, and support.
│
└── ceo_api/ [v2]
    Executive API surface over analytics projections and permissioned operational summaries.
```

## Public Web

```text
public-web/
├── web_cms/ [v2]
│   Website content: story, parks, technology, exchange, Bakrid goats, logins, who-we-are, contact, SEO.
│
├── lead_capture/ [v2]
│   Public interest forms for investors, buyers, donors, partners, and farm visitors. Emits events to commerce.
│
├── seo_routes/ [later]
│   Public pages and metadata for Mesha, goat genetics, goat farming, Bakrid goats, parks, and technology.
│
└── auth_entrypoints/ [v2]
    Login/portal routing only. Public Web never owns worker/customer identity.
```

## Goat Ops Core

```text
goat-ops-core/
├── identity/ [v1]
│   Canonical goat identity: Goat OS ID, RFID, QR/tag, FaceID reference, old tags, duplicates, missing/no-tag resolution.
│
├── ledger/ [v1]
│   Event contract, append-only envelope, idempotency, and unified goat timeline projection.
│
├── passport/ [v1]
│   Rebuildable goat read model: current state, timeline, family, location, proof, next actions.
│
├── locations/ [v1]
│   Parks, farms, sheds, pens, cohorts, shed tags, park ownership/managed_by, physical placement.
│
├── movement/ [v2]
│   Shifting/transfer workflow: source/destination shed, approvals, handover, pre-shift checks, vaccination blocks.
│
├── park_infrastructure/ [later]
│   Shed design, water/feed systems, ventilation, cameras, gates, power, capacity planning, maintenance, expansion.
│
├── workforce/ [v1]
│   Operators, supervisors, vets, verifiers, HR roster, weekly team schedule, ownership, absence fallback.
│
├── training/ [later]
│   SOP training, worker certifications, role readiness, verifier/vet qualification, retraining after incidents.
│
├── sop/ [v1]
│   Execution templates, form schemas, generation rules, step definitions, escalation configuration.
│
├── tasks/ [v1]
│   Runtime work: assignee, status, priority, SLA, comments, proof, handover, escalation, carry-forward.
│
├── media/ [v1]
│   Internal proof metadata: photos, videos, documents, storage paths, uploader, device, timestamp, task links.
│
├── verification/ [v1]
│   Proof routing engine: Park Head ground check, central video review, approve/reject, trust score, human queue.
│
├── devices/ [v1]
│   Normalized observations after gateway processing: RFID read, weight, ultrasound, camera, collar, shed sensor.
│
├── procurement/ [v2]
│   Goat/feed/medicine intake: vendors/source farms, loads, purchase records, quarantine, arrival inspection, payable trigger.
│
├── inventory/ [v1]
│   Vaccine, medicine, feed, equipment stock: batches, expiry, usage, reorder, stock-linked SOPs.
│
├── biosecurity/ [later]
│   Quarantine, disinfection, visitor/vehicle control, isolation rules, hygiene, disease-prevention SOPs.
│
├── health/ [v2]
│   Symptoms, observations, diagnosis, problems, treatment plans, medicines/alternatives, recovery, death workflow.
│
├── vaccination/ [v1]
│   Lifetime vaccine schedules, campaigns, weekly assignment, batch linkage, dose records, missed/blocked goats.
│
├── breeding/ [v2]
│   Heat, mating, AI, embryo transfer, pregnancy checks, delivery, abortion, kid creation, mother/kidding metrics.
│
├── lactation_milking/ [later]
│   Internal milk use: lactating mothers, colostrum, milk fed to kids, kid-feeding support, milk-feeding dashboard.
│
├── genetics/ [v2]
│   Breed registry, pedigree, DNA/tissue/genomics, germplasm, embryo/semen source, inbreeding risk, breeder scoring.
│
├── growth/ [v2]
│   Birth weight, periodic weights, ADG, teeth/age, body condition, growth curves, breeder/sale readiness.
│
├── feed/ [v2]
│   Feed master, feed patterns, sessions, packing, transport, consumption, water, FCR, supply planning.
│
├── production/ [later]
│   Sellable surplus yield such as milk, manure, and future animal/product outputs. Commerce owns the sale.
│
├── reconciliation/ [v2]
│   System count vs physical count: shedwise, breedwise, tagwise, DB-vs-physical, duplicate/difference resolution.
│
└── reporting/ [v1]
    Rebuildable internal reports. v1: active goats, vaccination status, proof status, shed counts. Grows with phases.
```

## Commerce

```text
commerce/
├── bookings/ [v2]
│   Consumer/occasion demand: qurbani, aqiqah, sadaqah, general purchase, weekly mutton subscriptions, event orders.
│
├── orders/ [v2]
│   B2B/direct buyer demand: buyers, sale orders, order items, buyer requirements, order status, commercial terms.
│
├── occasion_rules/ [v2]
│   Commerce policy per occasion: eligibility, slaughter window, allocation rules, distribution, pricing/proof requirements.
│
├── fulfillment/ [v2]
│   Supply execution: reserve, allocate goat, dispatch, slaughter/process/freeze, proof, distribute, deliver, close.
│
├── ownership/ [v2]
│   Investor/customer ownership records for goats/herds, locks, documents, value timeline, audit.
│
├── engagement/ [later]
│   Rebuildable customer-facing projection over goat passport/growth/media/proof plus commerce bookings.
│
├── payments/ [v2]
│   Money in/out: buyer collections, subscriptions, receipts, refunds, vendor payables, payouts, payment schedules.
│
├── settlements/ [later]
│   Buyer/vendor/investor/donor settlement records, payout status, payment reconciliation references.
│
├── support/ [later]
│   Commerce support tickets. If field action is needed, emit support_escalation_requested; Goat Ops creates the task.
│
└── meat_yield/ [v2]
    Slaughter feedback: live weight, carcass weight, dressing percentage, cut weights, meat quality.
```

```text
bookings.occasion_type:
  qurbani | aqiqah | sadaqah | general_purchase | weekly_mutton | event

qurbani:
  occasion_type = qurbani
  occasion_rules = commerce policy
  execution_sop_template = goat-ops-core.sop.qurbani_slaughter_and_proof_v1

can_allocate_goat =
  goat_ops.sellable?(goat_id)
  AND commerce.not_already_booked(goat_id)
  AND commerce.not_locked_by_ownership(goat_id)
```

## Platform

```text
platform/
├── auth/ [v1]
│   Authentication for separate realms: farm workers/admins and commerce customers/investors/donors.
│
├── permissions/ [v1]
│   Authorization model: roles, scopes, park/shed access, user groups, verifier/admin/customer boundaries.
│
├── audit/ [v1]
│   System-wide audit trail: actor, action, before/after, device, IP/location, reason.
│
├── notifications/ [v1]
│   FCM/SMS/WhatsApp/email delivery for task alerts, escalations, proof decisions, customer updates.
│
├── slack_outbound/ [v1]
│   Goat OS to Slack notifications: assignments, reminders, approvals, rejections, and summaries.
│   This is a notifier, not a data-entry path.
│
├── slack_ingest_bridge/ [v2 migration]
│   Controlled migration bridge for legacy Slack SOP/form submissions into Goat OS APIs.
│   Not part of the Android-first vaccination thin slice; enable only with dedup, audit, and a clear cutover owner.
│
├── outbox/ [v1]
│   Reliable cross-context event publishing, retry, idempotency, dead-letter, event versioning.
│
├── context_layer/ [v2]
│   Source definitions for domain terms, relationships, metrics, SOP meanings, and permission-aware AI context.
│
├── analytics_export/ [v2]
│   Clean projections into BigQuery: fact tables, snapshots, dashboards, model-training datasets.
│
└── legacy_import/ [v1]
    Temporary migration tools for old Sheets/Slack data. Quarantine and remove after migration.
```

```text
worker realm != customer realm
customer tokens can never reach worker/admin scopes
all cross-context contracts are versioned
```

## Device Gateway

```text
device-gateway/
├── rfid_adapter/ [v1]
│   RFID readers, gates, handheld scanners, duplicate/debounce logic.
│
├── mqtt_adapter/ [v2]
│   MQTT broker integration for IoT devices and shed telemetry.
│
├── scale_adapter/ [v2]
│   Weighing scales, weight stations, load-level and goat-level weights.
│
├── camera_adapter/ [v2]
│   Shed cameras, proof cameras, FaceID cameras, camera booths, camera registry.
│
├── ultrasound_adapter/ [later]
│   Pregnancy ultrasound device capture.
│
├── collar_adapter/ [later]
│   Collar/tag telemetry for selected goats or R&D cohorts.
│
└── sensor_adapter/ [later]
    Feed, water, temperature, humidity, ammonia, activity, and other shed sensors.
```

```text
raw device event -> structural validation -> dedup/debounce -> normalized observation
normalized observation -> Goat Ops domain validation -> goat event
```

## Edge Agent

```text
edge-agent/
├── local_buffer/ [v1]
│   Stores device events locally when connectivity is poor.
│
├── device_sync/ [v1]
│   Talks to local RFID readers, scales, cameras, and sensors.
│
├── retry_queue/ [v1]
│   Retries failed uploads with idempotency keys.
│
└── uplink/ [v1]
    Syncs buffered events to device-gateway.
```

## AI / R&D

```text
ai-rnd/
├── faceid/ [later]
│   Goat/sheep identity proposals from images/videos, gated through verification before identity changes.
│
├── video_proof_ai/ [later]
│   Detects whether proof videos match task requirements: goat visible, vaccine visible, action likely done.
│
├── age_estimation/ [later]
│   Teeth/body image models for age estimation.
│
├── weight_estimation/ [later]
│   Camera/video-based weight prediction, validated against scale weights.
│
├── pregnancy_ultrasound_ai/ [later]
│   Ultrasound image/video interpretation for pregnancy and fetal signals.
│
├── disease_gait_behavior/ [later]
│   Gait, posture, cough, activity, fever/stress anomaly models.
│
├── genetics_models/ [later]
│   Breeder scoring, offspring prediction, embryo-line performance, inbreeding risk.
│
├── meat_yield_models/ [later]
│   Predict carcass/meat yield from growth, breed, feed, health, and slaughter feedback.
│
├── data_labeling/ [later]
│   Human labeling pipelines for FaceID, proof videos, teeth, body weight, ultrasound, disease, carcass.
│
├── model_registry/ [later]
│   Versioned models, datasets, evaluation metrics, rollout state, rollback metadata.
│
├── realtime_inference/ [later]
│   Low-latency inference for FaceID, proof checks, device/camera observations.
│
├── batch_scoring/ [later]
│   Offline scoring for genetics, meat yield, disease risk, growth, and R&D experiments.
│
└── rnd_experiments/ [later]
    Controlled trials: imported embryos, collars, sensor pilots, feed experiments, camera booths, farm protocols.
```

```text
AI output is a claim, never canonical truth.
AI writes claims/scores/proposals to verification or devices.
Only Goat Ops domain validation can produce canonical goat events.
AI/R&D is a family of services, not one deployable.
```

## Analytics

```text
analytics/
├── warehouse/ [v2]
│   BigQuery/lakehouse storage for clean facts, snapshots, marts, and long-range analysis.
│
├── raw_events/ [v2]
│   Clean event copies from outbox. Business-event scale, long retention.
│
├── raw_telemetry/ [later]
│   High-volume device firehose: RFID pings, collar streams, camera/device telemetry. Shorter retention.
│
├── clean_facts/ [v2]
│   Curated analytics tables for goats, tasks, health, breeding, growth, feed, commerce, proof.
│
├── snapshots/ [v2]
│   Daily/periodic state snapshots: active goats, shed counts, campaign status, sale readiness.
│
├── semantic_layer/ [v2]
│   Analytics implementation of platform context_layer definitions: active goat, mortality, ADG, sellable goat.
│
├── ai_analyst/ [later]
│   Governed text-to-SQL and question-answering over approved semantic metrics, not raw operational tables.
│
├── dashboards/ [v2]
│   Role-based dashboard views over governed analytics: CEO/internal, investor/reduced, park, finance, R&D, device health.
│
└── model_training_sets/ [later]
    Curated datasets for FaceID, proof AI, growth, pregnancy, genetics, disease, and meat-yield models.
```

## Wires

```text
apps -> app-api
  SYNC only. Apps never access raw databases or backend context tables.

field_app_api -> goat-ops-core
  SYNC Goat Ops API only. Field app does not call commerce or analytics.

admin_api -> goat-ops-core + read-only context APIs
  WRITE: Goat Ops API only.
  READ: may compose read-only context APIs/projections from Goat Ops, Commerce, and analytics.

goat-ops-core -> slack_outbound
  OUTBOUND v1: task assignments, reminders, approvals, rejections, and summaries are posted to Slack from outbox events.

slack_ingest_bridge -> goat-ops-core
  INBOUND v2/migration: legacy Slack SOP/form submissions become controlled commands/events through Goat Ops API.
  During overlap, Android submission is the preferred operator path.
  Dedup by task identity, goat identity, actor, occurrence time, and proof reference; not by random request id alone.
  Slack never writes databases directly and never bypasses permissions.

portal_api -> commerce
  WRITE: Commerce API only.
  READ: commerce-owned customer views; goat/proof data arrives through commerce read models/events.

analytics_dashboard_api -> analytics
  READ: analytics API only. Single dashboard app reads governed projections according to role and scope.

public-web -> commerce
  ASYNC events: leads, investor interest, booking interest. Public Web never writes commerce tables directly.

edge-agent -> device-gateway
  ASYNC uplink: buffered raw device events

device-gateway -> goat-ops-core
  ASYNC: normalized observations

goat-ops-core -> ai-rnd
  ASYNC: media/events/tasks for analysis and training

ai-rnd -> goat-ops-core
  ASYNC best-effort: claims, scores, proposals

commerce -> goat-ops-core
  SYNC API: sellable?, goat summary, health/proof/genetics readiness
  ASYNC events: fulfillment completed, sale exit, meat-yield performance

goat-ops-core -> commerce
  ASYNC events: goat state, proof status, readiness changes

commerce.support -> goat-ops-core
  ASYNC event: support_escalation_requested. Goat Ops decides whether to create a field task.

goat-ops-core.procurement -> goat-ops-core.identity / inventory
  INTERNAL event: procured goats create identity intake; feed/medicine/equipment updates stock

goat-ops-core.procurement -> commerce
  ASYNC event: vendor payable request for goat/feed/medicine procurement

goat-ops-core + commerce -> analytics
  ASYNC: outbox projections

device-gateway/edge-agent -> analytics
  ASYNC: selected raw_telemetry pipe for R&D and observability
```

## Shared Performance Event Contract

```text
performance_event
├── goat_id
├── event_type
├── source_context
├── source_event_id
├── measured_at
├── metric_name
├── metric_value
├── unit
├── confidence
├── proof_refs
└── metadata

writers:
  breeding -> performance_event
  meat_yield -> performance_event
  genetics_models -> performance_event claim

consumer:
  goat-ops-core.genetics
```

## Non-Negotiable Rules

```text
Goat Ops Core owns canonical goat truth.
Each domain owns typed event tables; ledger provides contract and timeline projection.
Commerce cannot read Goat Ops tables directly.
Public Web cannot write Commerce tables directly.
Apps call app-api only. No app touches any DB directly.
App-api may compose read-only views across context APIs/projections.
App-api must not write across contexts, run cross-context transactions, or own canonical state.
Dashboard visibility is role-based behind login; UI hiding is not security, analytics APIs must enforce role and scope.
Operators do not get analytics dashboards; Android Field App shows task list, entry logs, proof status, and limited personal/team work history.
Slack outbound notifications are v1; Slack inbound form ingestion is migration/v2 and must pass through Goat OS APIs, permissions, validation, audit, and idempotency.
AI/R&D cannot write canonical state directly.
AI pre-check is best-effort. Verification degrades to human queue if ai-rnd is unavailable and never blocks proof flow.
Device Gateway cannot enforce goat-domain rules.
Verification is a routing engine: model confidence + worker trust + risk policy decide review path.
Passport, reporting, dashboards, and analytics are rebuildable projections.
Qurbani/Aqiqah/Sadaqah are occasion types governed by occasion_rules.
Ownership/investments stay walled from normal bookings/orders.
Cross-context calls and events are contract-versioned.
Platform context_layer is the definition source for analytics semantic_layer metrics.
Legacy import is temporary and must be deleted after migration.
```
