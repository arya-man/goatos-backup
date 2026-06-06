# Goat OS Current System Assessment

ARCHIVED / NOT AUTHORITATIVE.

This file is historical planning material. Do not use it as build instruction
unless a human explicitly asks for archive comparison.

Last checked: 2026-06-06

This is based on the cloned repos in `<mesha-workspace>`, especially `slack-automation-scripts`, `dashboard`, `vgoats-dashboard`, `procurement_app`, and `website`.

## 1. Current Form / Workflow Inventory

Exact count needs the live Google Sheets config tabs (`Workflow Type`, `Report Type`, template sheets, form response sheets). From the code/docs alone, the current system has about **20-22 form/workflow families**, with roughly **14-16 directly goat-ops relevant**.

Do not treat every item below as a separate UI screen. Some are Google Forms, some are Slack modal forms, some are generated SOP workflows, and some are automated background jobs.

### External Google Form / Response Sheet Families

1. **Delivery / Birth / Abortion**
   - Sheet: `Delivery Form Responses`
   - Creates Birth or Abortion workflows.
   - Generates delivery actions, colostrum actions, and follow-up shifting.

2. **Shifting**
   - Sheet: `Shiftings Reports Responses`
   - Approval first, then shifting execution/proof.
   - Also receives generated auto-shifting rows.

3. **Death**
   - Sheet: `Death Reports Responses`
   - Captures death event, media, reason, goat metadata.

4. **Farming Tracker**
   - Sheet: `Farming Tracker Responses`
   - Non-core for Goat OS v1 unless farm/crop work stays inside the same product.

5. **Procurement / Purchase Intake**
   - Sheet: `1st June Procurement Responses`
   - Existing purchase/load/goat intake workflow.

6. **Health Manager Attendance**
   - Sheet: `Form Responses`
   - In/out per shed, media proof, optional health diagnosis while observing goats.

7. **Farmer Crop Management**
   - Sheet: `Farmer's Form Responses`
   - Seed/crop/farmer workflow. Keep outside Goat OS v1 unless explicitly required.

8. **Feed Packing**
   - Sheet: `Feed Packing Form`
   - Feed packing proof and tracking.

9. **Feed Consumption & Wastage**
   - Sheet: `Feed Consumption & Wastage Form`
   - Feed distribution, consumption, wastage, water verification.

10. **Feed Transport**
   - Sheet: `Feed Transport Form`
   - Feed unloading/transit proof.

11. **Health Diagnosis**
   - Sheet: `Diagnosis Form`
   - New disease/condition diagnosis.

12. **Health Follow-Up**
   - Sheet: `Follow Up`
   - Follow-up treatment/status updates.

13. **Not Eating**
   - Sheet: `Not Eating Response`
   - Feed/health exception trigger.

### Slack / SOP Workflow Families

14. **Vaccination**
   - Described in automation docs.
   - Schedules next dose after completion.
   - Syncs with Goats DB to identify active animals, births, and purchases.

15. **Treatment / Health SOP**
   - Daily session-wise treatment plan.
   - Video proof uploaded in thread.

16. **Milk Preparation**
   - Boiling, cooling, acid mixing, bottle filling.
   - Slack thread tracks session-wise execution.

17. **Milk Feeding / Milk Refusal SOP**
   - Modal/form response inside Slack.
   - Refusal triggers follow-up SOP.

18. **Colostrum Sessions**
   - Generated from birth workflow/template.
   - Multiple timed sessions after delivery.

19. **Procurement Transit**
   - Loading/transit/unloading proof and reminders.

20. **Video Verification**
   - Central verification for delivery, feed packing, feed distribution, feed transit, and treatment.

21. **History Report**
   - `/history FARM goatID`
   - Generates goat PDF history from delivery, purchase, sales, health, treatment, shifting, etc.

22. **Auto-Shifting / Stage Movement**
   - Example: K3 to F2 shifting.
   - This is automation that writes into the shifting pipeline, not a separate operator form.

## 2. What Android Must Replace

The Android app is not just "upload video". It needs to replace the Slack SOP/form execution layer:

- My tasks / weekly schedule.
- Task detail with goat IDs, shed/location, due time, assignee, proof requirements.
- Conditional SOP form runner.
- Video/photo proof capture.
- Offline drafts and retry-safe sync.
- Entry logs: submitted, uploaded, pending verification, rejected, closed.
- Goat lookup/passport for the operator's permitted scope.

Slack should remain:

- V1: outbound notifications and escalation mirror.
- V2/migration: inbound bridge only for old forms that still exist.
- Long-term: not the source of truth.

## 3. Current Frontend Tech Stack

### `dashboard`

- Next.js `14.2.35`
- React `18`
- TypeScript
- Tailwind CSS
- Recharts
- TanStack Query
- Google BigQuery client
- Lucide icons
- Purpose: full internal/CEO analytics dashboard.

### `vgoats-dashboard`

- Next.js `14.2.35`
- React `18`
- TypeScript
- Tailwind CSS
- Recharts
- TanStack Query
- Google BigQuery client
- Purpose: reduced dashboard, currently treated as investor/external style view.

### `procurement_app`

- React Native CLI `0.85.3`
- React `19.2.3`
- TypeScript
- React Navigation v7
- Firebase Auth / Firestore / Storage / Messaging
- Vision Camera
- React Native Video
- MMKV
- Zustand
- Purpose: mobile procurement/load app. Best salvage for Android video capture, phone auth, navigation, upload patterns, and role-gated UI.

### `website`

- Vite
- React
- TypeScript
- Tailwind CSS
- Framer Motion / GSAP
- React Router
- Lucide icons
- Purpose: public Mesha website, separate from Goat OS ops.

### `slack-automation-scripts`

- Google Apps Script JavaScript
- Google Sheets
- Slack API
- Val Town TypeScript/Deno proxy scripts
- Some Redis-style queue/state usage in the unified engine
- Purpose: current SOP/workflow automation layer.

## 4. Capacity: How Many Goats Can The Target Architecture Hold?

The limiting unit is not "number of goats". The limiting units are:

- domain events per goat,
- videos/photos per task,
- dashboard query patterns,
- offline sync burst size,
- raw device telemetry volume.

### Current Sheets/Slack/AppScript Setup

This is not the scalable architecture. It can support the current ops because people are manually validating and Sheets is acting as a database, but it will not safely hold 1M goats or device/video-heavy workflows.

Risk areas:

- Google Sheets as DB.
- Apps Script execution limits and trigger fragility.
- Slack threads as workflow state.
- Human verification volume.
- No strong event/idempotency model.

### Target Go/Postgres/GCS/BigQuery Setup

With the architecture we discussed:

- **1M goats:** very reasonable.
- **5M goats:** still reasonable if event tables are partitioned, read models exist, analytics goes to BigQuery, and raw telemetry stays out of core Postgres.
- **10M+ goats:** possible, but at that point we should expect hot-path extraction, read replicas, table partition strategy, and possibly sharding/Spanner-style decisions later.

Rough math:

- 1M goats x 100 domain events/year = 100M events/year.
- 100M/year = about 274k events/day = about 3.2 events/sec average.
- Even 5M goats at 100 events/year = about 1.37M events/day = about 16 events/sec average.
- Bursts matter more than averages, so design for hundreds/thousands of writes/sec during sync windows.

The real danger is devices:

- 1M goats x 1 collar ping/minute = 1.44B telemetry pings/day.
- That must not enter the core ledger one row at a time.
- Raw device telemetry needs a separate firehose path: edge/device gateway -> Pub/Sub or equivalent -> raw telemetry lake/BigQuery/object storage -> summarized observations into Goat OS core.

## 5. Stress Test Plan

### What To Build For Testing

1. **Seed generator**
   - Go CLI: `cmd/seed`.
   - Creates goats, locations, users, teams, vaccines, task templates, tasks, events, verification records.
   - Must support 50k, 1M, 5M goat profiles.
   - Use Postgres `COPY` or batched inserts, not row-by-row ORM inserts.

2. **Load tests**
   - Use `k6` for HTTP API load.
   - Use `pgbench` for raw database pressure.
   - Use Go benchmarks for module-level performance.

3. **Mock media**
   - Do not upload real videos for every stress test.
   - Most tests should create media metadata + signed upload flow only.
   - Separate GCS/media throughput tests can upload tiny dummy files.

4. **Offline sync simulator**
   - Simulates 500, 5k, 20k Android devices syncing after bad network.
   - Uses idempotency keys so retries never duplicate vaccination/task events.

5. **Verification queue simulator**
   - Creates proof records at high rate.
   - Tests routing to Park Head / central verifier queues.
   - Measures backlog drain time.

6. **Analytics simulator**
   - Tests dashboard read paths from projections/BigQuery, not from raw OLTP tables.

### k6 Scenarios

1. **Operator daily flow**
   - Login/token.
   - Fetch my tasks.
   - Open task.
   - Submit conditional SOP answers.
   - Request media upload URL.
   - Submit vaccination/proof completion.
   - Sync status.

2. **Admin flow**
   - Create/update vaccination config.
   - Generate weekly schedule.
   - Assign team.
   - Reassign absent operator tasks.
   - Review pending proof queue.

3. **Verifier flow**
   - Fetch assigned proof queue.
   - Approve/reject with reason.
   - Close task.

4. **Dashboard flow**
   - Active goat counts.
   - Vaccination due/overdue.
   - Health exceptions.
   - Verification backlog.
   - Location/team filters.

5. **Offline burst flow**
   - Batch submit 50-500 completed task events per device.
   - Retry same batch.
   - Confirm no duplicates.

6. **Device observation flow**
   - Gateway accepts raw observations.
   - Core receives only normalized/domain observations.
   - Raw telemetry is not pushed into core event tables.

### Test Levels

1. **Smoke**
   - 50 users, 5 minutes.
   - Finds obvious bugs.

2. **Load**
   - 500-1,000 virtual users, 30-60 minutes.
   - Normal busy day.

3. **Stress**
   - 5,000-20,000 virtual users or equivalent request rate.
   - Push until p95 latency/error rate breaks.

4. **Soak**
   - 12-24 hours.
   - Finds memory leaks, connection leaks, queue lag, autovacuum issues.

5. **Spike**
   - Sudden 10x traffic for 5-15 minutes.
   - Simulates network returning at farms or Bakrid-style bursts.

### Pass/Fail Metrics

- API p95 read latency under 300 ms for common reads.
- API p95 write latency under 800 ms excluding actual video upload.
- Error rate under 0.1%.
- No duplicate domain events under retry.
- Outbox lag under 30 seconds in normal load.
- Verification queue backlog drains within target SLA.
- Cloud SQL CPU under 70% sustained during normal load.
- DB connection count stable and below pool limits.
- Dashboard APIs never scan raw event tables for normal views.

## 6. Immediate Conclusion

The existing frontend is useful. The existing Slack/Scripts system is an operational blueprint, not the future backend.

Build next:

1. Go modular monolith API.
2. Postgres schema + event envelope.
3. Conditional SOP config model.
4. Android Field App task/SOP runner using pieces from `procurement_app`.
5. Auth/RBAC gate on dashboards.
6. k6 + seed generator from day one.
