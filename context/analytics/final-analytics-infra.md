# Goat OS Analytics, BI, AI, Telemetry, And Cost Controls

Status: authoritative.

Analytics is part of Goat OS from the start. It is not a reporting add-on. The stack is complete from the start and tuned under real load.

## Final Stack

```text
Postgres:
  operational truth

Postgres outbox -> Pub/Sub:
  streaming backbone

BigQuery:
  historical warehouse
  clean facts/marts
  genetics/model-training datasets

Tinybird:
  hot telemetry
  live RFID/scale/camera/device APIs
  short-retention live serving

GCS:
  raw telemetry archive
  media proof storage

dbt Core:
  transforms and tests only
  raw -> clean_facts -> marts
  no governed metric definitions

Cube Core:
  single semantic layer
  only home for governed metrics

Metabase OSS:
  internal exploration
  official KPIs through Cube

Next.js/Recharts:
  polished CEO, investor, and admin dashboards

AI analyst:
  governed metrics through Cube only
```

## Single Semantic Door

Cube is the only home for governed metrics:

```text
active_goat
vaccination_due
mortality_rate
ADG
purchase_cost_per_kg
verification_backlog
feed_conversion
operator_completion_rate
```

Rules:

```text
dashboards:
  official leadership/analytics KPI answers query Cube.
  operational product dashboards may read Postgres projections only when the
  KPI is declared dual-served and covered by the parity gate in
  docs/decisions/high-scale-dashboard-projections.md.

Metabase:
  official KPIs query Cube.
  ad-hoc exploration over marts is allowed but cannot define official numbers.

AI:
  governed analytics through Cube only.
  no raw Postgres.
  no raw Tinybird.
  no direct BigQuery clean_facts for official KPIs.

dbt:
  transforms only.
  no metric definitions that duplicate Cube.
```

## AI Analyst Readiness

The AI analyst must not be a raw warehouse chatbot. It becomes useful only after
the governed analytics layer exists.

Required before enabling AI analyst answers for CEO, investor, or operating
decisions:

```text
canonical dbt marts:
  clean facts, dimensions, snapshots, and dashboard marts exist.

Cube metrics:
  official KPI definitions live in Cube with grain, joins, filters, ownership,
  freshness, and valid-value notes.

legacy dashboard parity:
  old dashboard queries and BigQuery table names are converted into metric
  inventory and parity tests, not copied as new truth.

analytics skill:
  the agent is instructed to use Cube first, curated marts second, and raw SQL
  only for debugging or migration investigation.

analytics reference docs:
  per-domain docs exist for the agent's knowledge path. Each doc names the
  canonical metrics, dimensions, key tables, grain, join keys, required filters,
  freshness rules, gotchas, and common query patterns.

analyst workflow skill:
  a separate workflow guide exists for analytics answers: clarify the question,
  find governed sources, compile/query, review assumptions, and return
  provenance. Do not merge workflow guidance into metric definitions.

offline evals:
  fixed questions for active herd, mortality, feed cost, ADG, procurement cost,
  promise risk, vaccination compliance, and sales margin are checked against
  blessed snapshots or dashboards.

eval telemetry:
  every eval run records skill version, git SHA, model ID, per-assertion
  pass/fail, token count, latency, and result timestamp. Stakeholder corrections
  become candidate eval cases after human review.

provenance footer:
  every answer reports source tier, freshness, owner, and whether the answer is
  official, exploratory, or raw/debug.
```

Rules:

```text
AI cannot define official metrics.
AI cannot query raw operational Postgres for official KPI answers.
AI cannot treat legacy dashboard SQL as authoritative.
AI can draft metric docs, column descriptions, and eval cases for human review.
Leadership-bound answers need governed metrics or explicit human sign-off.
Data-model changes must update the matching analytics reference docs and evals.
CI should eventually enforce that dbt/Cube changes touch the related analytics
skill/reference files or explicitly justify why not.
```

## High-Scale Dashboard Serving

Dashboards that slice large data by month, date, breed, farm, shed, load,
category, status, gender, operator, or source must follow
`docs/decisions/high-scale-dashboard-projections.md`. That decision is the
canonical source for projection serving, dual-served KPI gates, standard
freshness envelopes, and legacy visual/numeric parity rules.

Reference doc template for each analytics domain:

```text
Quick reference:
  canonical metric names and when to use them.

Dimensions:
  allowed cuts, valid values, aliases, and default filters.

Key tables/marts:
  grain, primary keys, join keys, partition/freshness columns, owner.

Required hygiene:
  tenant/scope filters, date windows, deleted/merged/excluded-state rules,
  safe division, and cost guardrails.

Gotchas:
  similarly named tables, legacy naming traps, stale/deprecated sources, and
  source-specific caveats.

Common query patterns:
  worked patterns for the domain's normal questions.

Cross-references:
  related dashboards, source docs, Cube metrics, dbt models, and eval cases.
```

Analytics answer review:

```text
For official or leadership-bound answers, run an adversarial review pass over
metric choice, grain, filters, date window, joins, freshness, and exclusions.
This review may be a second agent later, but it must be bounded by latency and
cost budgets before becoming a default product behavior.
```

Slack correction harvesting:

```text
Slack and WhatsApp corrections are useful source material, not automatic truth.
A future analytics-maintenance worker can watch approved channels for correction
language, draft reference-doc or eval-case changes, and route them to the metric
owner. It must never silently change metric definitions or canonical data.
```

## Event Flow

```text
domain mutation
  -> Postgres transaction
      canonical rows
      typed event
      outbox row
  -> outbox relay
  -> Pub/Sub
      -> BigQuery
      -> Tinybird when live/hot telemetry relevant
      -> GCS raw archive
      -> notifications/workers
```

Pub/Sub is the bus. Consumers remain idempotent. Pub/Sub delivery features do not replace business idempotency.

## Outbox Relay

```text
Go poller:
  reads unsent outbox rows in chunks
  publishes to Pub/Sub
  marks sent with publish metadata
  retries safely
  writes failures to relay error table
```

Required:

```text
event_id
event_type
schema_version
aggregate_type
aggregate_id
occurred_at
recorded_at
producer
idempotency_key
payload
trace_id
```

## Telemetry Flow

```text
device/RFID/scale/camera/ultrasound/collar
  -> edge-agent/device-gateway
  -> structural validation
  -> Pub/Sub
  -> Tinybird hot/live APIs
  -> BigQuery historical facts
  -> GCS raw archive
  -> Postgres only after domain validation as a confirmed observation
```

Raw telemetry is never the operational write path.

## Legacy Analytics Source Catalog

Existing dashboards and Sheets/BigQuery code are analytics references only. They
must not become the Goat OS operational backend, but their table/view names are
useful seed material for dbt marts, Cube metrics, dashboard parity checks, and
migration QA.

Legacy distillation inputs:

```text
goatos/apps/admin-web/app/api:
  52 copied legacy dashboard API route files. First source for SQL inventory,
  parity tests, and dashboard rewiring because it lives inside the Goat OS repo.

dashboard/app/api:
  52 live dashboard API route files. Use for cross-checking the copied snapshot,
  not as a write target.

vgoats-dashboard/app/api:
  34 legacy dashboard API route files. Use after admin-web/dashboard coverage to
  fill any missing investor or alternate-dashboard metrics.

slack-automation-scripts/Dashboard Charts - BigQuery Mapping.docx:
  chart-to-BigQuery mapping with metric, dimension, source, and filter notes.
  Distill into metric inventory and reference docs; do not keep it as the
  governed definition.

source-material/goatOS.docx:
  legacy schema/table/grain notes and lineage inventory. Distill useful grain,
  ownership, and lineage facts after review; do not commit private/raw content.

General/Goats and Parks plus Slack operating docs:
  business terminology, breed/location/status gotchas, and correction patterns.
  Distill into domain reference docs and eval cases with human review.
```

Legacy BigQuery project observed:

```text
goatos-sheets
```

Legacy datasets referenced:

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

Legacy table/view families:

```text
Counts:
  counting_db_with_holding_dev
  counting_kpis_daily
  daily_summary_dev
  core_farm_genderwise

Birth / breeding:
  mother_kid_facts
  birth_analysis_view
  breedwise_kidding_8m
  v_birth_count_last_10_days
  kidding_frequency
  parent_stock_table

Fattening / growth:
  growth_farmwise_weighing
  adg_summary_age_shed
  kids_counting_vs_weighing
  adg_goat_last2
  weighingprogression_loadwise
  loadwise_summary
  fattening_load_sales_comparison

Feed:
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

Mortality / health:
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

Infra:
  shed_capacity_count_dev
  counting_shed_capacity_status_dev

Sales:
  salesDB_clean
  monthly_feed_vs_sales

Vaccination:
  vaccination_dashboard
```

Migration rule:

```text
legacy table names can seed parity tests and metric inventory.
new Goat OS metrics live in Cube and dbt marts.
dashboards cannot keep direct hidden formulas against old BigQuery tables.
```

## BigQuery Cost Controls

BigQuery must not become an unbounded scan engine. Controls are mandatory.

```text
table design:
  partition large fact tables by event_date/recorded_date.
  cluster by farm_id, shed_id, goat_id, event_type where useful.
  require partition filters on large partitioned tables.
  dashboards query marts/materialized views, not raw facts.

query controls:
  set maximum bytes billed for dashboard/API queries.
  create custom query quotas for projects/service accounts.
  dry-run heavy ad-hoc queries where possible.
  block SELECT * in reviewed dashboard queries.

dashboard acceleration:
  use marts and cached API responses.
  consider BI Engine for stable dashboard workloads.

monitoring:
  export BigQuery jobs metadata.
  track bytes processed by user/service/dashboard.
  alert on scan spikes.
```

Google documents maximum bytes billed as a best practice for limiting query costs, supports custom query quotas, and partition filters allow BigQuery to scan only matching partitions instead of entire tables. BI Engine can accelerate BI/dashboard-style queries. These are not optional guardrails. 

## Tinybird Cost Controls

Tinybird is for hot telemetry/live serving, not infinite history.

```text
retention:
  keep only live/hot window needed for operational dashboards.
  long-term history goes to BigQuery/GCS.

region:
  co-locate Tinybird with the GCP region where telemetry exits.
  avoid paying egress on the highest-volume stream.

portability:
  hide Tinybird behind analytics ports.
  retain raw data in GCS/BigQuery so ClickHouse Cloud or self-hosted ClickHouse remains possible if cost changes.
```

## Media Cost Controls

Video can exceed the rest of infra cost.

```text
capture:
  compress on device.
  enforce max duration/resolution by SOP proof policy.

upload:
  direct signed GCS upload.
  API never proxies video bytes.

retention:
  keep proof artifact, hash, metadata, and audit link.
  expire raw video after verification/legal retention window.

delivery:
  signed URLs for direct viewing.
  CDN/cache for frequently watched proof.
  investor views use sanitized, curated media only.
```

## Verification Labor Cost

Human validation is the largest cost risk.

```text
verification engine:
  AI pre-check
  confidence routing
  random sampling
  operator/team trust scores
  queue prioritization
  rejection/rework workflow
```

The objective is to reduce full human review without allowing AI to mutate canonical truth directly.

## Environment Isolation

```text
dev:
  local or isolated cloud resources.
  no prod goat data.

staging:
  production-like.
  separate Postgres, BigQuery datasets, GCS buckets, Tinybird workspace, Cube/Metabase config.
  stopped when idle where possible.
  used for migration rehearsal and load tests.

prod:
  isolated datasets, buckets, topics, DBs, service accounts.
  least privilege.
```

Dev/staging analytics must never read production PII/raw goat data.

## Rejected

```text
Snowflake:
  redundant beside BigQuery.

Kafka/Redpanda:
  redundant beside Pub/Sub for this GCP-centered system.

Long-retention Tinybird:
  wrong cost shape.

Raw table AI:
  forbidden.
```
