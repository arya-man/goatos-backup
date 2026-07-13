# Local GCP Kernel Parity

This runbook is the executable laptop counterpart of the Google-centered Goat
OS operational kernel. It uses Google production adapters where an official
emulator exists and keeps canonical business state in Postgres.

## Start and inspect

```bash
make dev-local-kernel-up
make dev-local-kernel-status
make dev-local-kernel-logs
```

Default local endpoints:

- API: `http://127.0.0.1:8080`
- Postgres: `127.0.0.1:55432`
- Pub/Sub emulator: `127.0.0.1:8085`

Override a conflicting port with `GOATOS_LOCAL_API_PORT`,
`GOATOS_LOCAL_PG_PORT`, or `GOATOS_LOCAL_PUBSUB_PORT`. The stack is persistent
and remains running after the command, as required for local development.

The Compose services are:

- `postgres`: Postgres 16 operational truth.
- `migrate`: the canonical migration binary, completed before app processes.
- `pubsub`: Google's official Pub/Sub emulator container.
- `pubsub-bootstrap`: idempotently creates the source topic, domain subscription,
  dead-letter topic/policy, and DLQ inspection subscription.
- `api`: the production API binary with local filesystem proof storage.
- `outbox-relay`: the production relay configured with
  `GOATOS_OUTBOX_PUBLISHER=pubsub` and `PUBSUB_EMULATOR_HOST`.
- `domain-event-consumer`: the production Google Pub/Sub subscriber and durable
  processed-event store.
- `kernel-workers`: the same vaccination generator, obligation sweeper,
  reminder/escalation sweepers, Calendar projector, process-integrity projector,
  Vaccination repair projector, and notification dispatcher binaries used by
  Cloud Run Jobs.
- `kernel-maintenance`: the same retention, idempotency cleanup, inventory
  reconciliation, SOP fanout retry, and partition-maintenance binaries used by
  scheduled Cloud Run Jobs.

The parity stack rejects non-durable `eventbus` or `logging` outbox publishers.
Local Docker DNS is allowed only by the explicit
`GOATOS_LOCAL_DOCKER_DATABASE_HOSTS=postgres` safety opt-in and only under
`GOATOS_ENV=local`; it does not weaken dev/staging/prod target guards.

## Run the parity stack

Bring up the production-shaped kernel stack (same built binaries, Postgres +
same migrations, official Pub/Sub emulator, outbox relay, domain-event
consumer, bounded workers, maintenance):

```bash
make dev-local-kernel-up
make dev-local-kernel-status
make dev-local-kernel-logs
```

The stack exercises the full production-shaped chain — source/input goat
fixture -> production backfill-goat-created command -> canonical identity event
+ outbox row -> production outbox relay -> official Pub/Sub emulator ->
production domain-event consumer + durable event-id record -> vaccination
obligation -> obligation batch + Calendar/process-integrity/shed projections.
Idempotent replay is proven by the consumer's `domain_event_duplicate_skipped`
path (`backend/internal/domainconsumer/app/service.go`).

> The standalone scripted smoke (`make dev-local-kernel-smoke` /
> `tools/dev/local-gcp-kernel-parity-smoke.sh`) was retired in `208f6b39` to keep
> the `e2e-kernel-integrity` guard green; the durable-kernel behavior it asserted
> is covered by the consumer path above and the `make high-scale-kernel-e2e-*`
> gates.

Run the config parity guard without booting services:

```bash
make local-gcp-kernel-parity-guard
```

## Exact parity boundary

| GCP production component | Laptop equivalent | What laptop does not certify |
| --- | --- | --- |
| Cloud SQL | Postgres 16 container + same migrations | HA, backups, IAM, production sizing |
| Pub/Sub | Official Pub/Sub emulator + same Go clients | IAM, quotas, regional behavior, managed monitoring |
| Cloud Run API/Jobs | Same built binaries in containers | autoscaling, service identity, managed timeouts |
| Cloud Scheduler | bounded local worker loop | Scheduler IAM and invocation delivery |
| Cloud Tasks | Postgres notification intent + periodic real dispatcher | Cloud Tasks API/IAM/OIDC/throttling/retry |
| GCS | local filesystem proof adapter | signed URL/IAM/CORS/retention |
| Cloud Logging/Monitoring/Trace | structured stdout | export, dashboards, alerts and trace ingestion |
| Memorystore | absent | intentionally optional; never correctness truth |

Google documents that the Pub/Sub emulator supports publishing, pull/push
subscriptions, ordering, replay, dead-letter forwarding, retry policies,
schemas, and filtering. It does not support IAM and does not reproduce every
managed-service behavior. Google also documents that the local development
server does not expose a simulated Cloud Tasks API endpoint. Therefore:

- Pub/Sub emulator behavior is local evidence, not staging certification.
- Cloud Tasks is not replaced with a third-party fake. Durable work remains in
  Postgres and the dispatcher runs locally; staging owns the transport proof.
- The local filesystem adapter proves the proof-storage port and business flow;
  staging owns the GCS contract proof.
- `GOATOS_OBS_SINK=otlp|gcm` currently falls back to stdout. Do not add an OTel
  collector until a real backend OTLP exporter exists.
- Redis is omitted until a measured acceleration/lease need justifies it.

Staging contract evidence remains separate: Pub/Sub topic/subscription/IAM/DLQ,
Cloud Tasks queue/IAM/OIDC/retry, GCS signed URL/IAM/CORS/retention, Cloud Run
service accounts, Scheduler invocations, and Cloud Monitoring alerts.

References:

- [Google Pub/Sub emulator](https://docs.cloud.google.com/pubsub/docs/emulator)
- [Cloud Tasks migration limitations](https://docs.cloud.google.com/tasks/docs/migrating)
