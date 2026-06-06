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
  official KPIs query Cube.

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

