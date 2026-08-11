# Notification & Event Delivery on GCP (the "we used SQS" answer)

Status: **documents the current 5k-to-50k runtime and future scale-out boundary**.

## TL;DR

You do **not** rebuild SQS on GCP. Goat OS follows the operational kernel: a
**transactional Postgres outbox** is the source of truth, an **outbox relay**
moves durable events to **Pub/Sub**, the consolidated **kernel worker** runs
bounded cadence stages, durable **`notification_requests`** rows own
dispatch/retry/leases, and device push lands via **FCM** behind a replaceable
gateway port. Cloud Scheduler/Cloud Run Jobs and Cloud Tasks are future
per-hotspot scale-out options, not the current normal topology.

See `context/architecture/operational-kernel.md` (line ~118: "Kafka / SQS event
bus → transactional Postgres outbox → outbox relay → Pub/Sub topic/subscription
with DLQ") and `context/architecture/operational-kernel-system-design.md`.

## AWS → GCP mapping

| AWS (SQS-based push) | GCP equivalent | Why |
|---|---|---|
| SQS work queue | Postgres **`notification_requests`** today; Cloud Tasks only as future scale-out | durable queue, idempotency, lease, retry/backoff and exhaustion evidence |
| SQS/SNS event bus, fan-out | **Pub/Sub** topic/subscription | event spine, at-least-once, many idempotent consumers |
| SQS FIFO (order + dedup) | Pub/Sub **ordering keys** + exactly-once subscription | per-recipient / per-obligation ordering |
| SQS DLQ | Pub/Sub **dead-letter topic** + parked Postgres row | failed-after-N visibility |
| SQS visibility timeout | ack deadline / **Postgres lease token** | in-flight lease; stale leases reclaimed |
| SQS delay | Postgres row `next_attempt_at` / contact timer | near-term timers |
| CloudWatch cron | Consolidated **kernel-worker cadence stage** today; Cloud Scheduler → isolated worker only after measured scale-out | due / reminder / escalation scans |
| SNS → APNs/GCM device push | **FCM** behind `notification/ports.Gateway` | last-hop device push |

## How it is actually built in this repo

```
business txn ──(ONE Postgres txn)──▶ canonical row + audit + OUTBOX event (idempotency key + fingerprint)
outbox relay  cmd/outbox-relay  ──▶ Pub/Sub  (internal/outbox/adapters/publisher/pubsub/)
domain event consumer  ◀── Pub/Sub sub (internal/domainconsumer/adapters/pubsub)
        │  builds canonical obligations/events and durable notification_requests rows
kernel-worker cadence stages ──▶ bounded indexed Postgres due/reminder/escalation scans
notification-dispatcher stage  ──▶ notification/app.Service.RunOnce
        │  lease-claim durable rows ──▶ Gateway.Send ──▶ Slack | email | FCM | incident(Opsgenie/PagerDuty)
```

### Component index (source of truth = code)

| Concern | Code |
|---|---|
| Event spine (outbox → Pub/Sub) | `internal/outbox/adapters/publisher/pubsub/{publisher,gcp}.go`, `cmd/outbox-relay` |
| Pub/Sub consumer | `internal/domainconsumer/adapters/pubsub/subscriber.go`, `cmd/domain-event-consumer` |
| Current cadence/time spine | `cmd/kernel-worker`, `internal/kernelstages` |
| Future optional queue adapter | `internal/platform/taskqueue/cloudtasks.go` (not current normal authority) |
| Delivery queue + retry + DLQ | `internal/notification/{domain,ports,app,adapters/postgres}` |
| Multi-channel send (incl. FCM) | `internal/notification/adapters/gateway/gateway.go` |
| Current worker entrypoint | `cmd/kernel-worker` notification-dispatcher stage |

## Your mechanisms, mapped to the real code

- **Channels** — `domain.Request.Channel`; the gateway routes per channel
  (`sendSlack`, `sendEmail`, `sendFCM`, `sendIncident`). New channels = new
  `Gateway` cases; Pub/Sub topic-per-channel + FCM `android_channel_id` on the
  device.
- **Idempotency** — the outbox event carries a stable idempotency key +
  fingerprint written in the SAME transaction as the business state; consumers
  are idempotent; delivery uses a per-row **lease token** so only one dispatcher
  owns a send at a time (`MarkSent`/`MarkFailed` are lease-checked).
- **Retry / backoff** — `Service.backoff(attempt)` + `MarkFailed(..., nextAttemptAt)`;
  `ReclaimStaleSending` returns a crashed dispatcher's in-flight rows to the
  queue after the lease times out. Bounded by `ClaimParams.MaxAttempts`.
- **DLQ** — after `MaxAttempts` a row becomes `StatusExhausted` and
  `insertNotificationExhaustedEvidence` records dead-letter evidence; Pub/Sub
  hops use a dead-letter topic. Alert on DLQ count + outbox oldest-unsent age
  (`operational-kernel-system-design.md` ~line 199).
- **Statuses** (`domain`): `queued → sending → sent | failed → exhausted`,
  plus `suppressed`, `read`.
- **FCM display text is mandatory.** Every `push_fcm` payload carries nonblank
  `data.title` and `data.body`, including message-key pushes that are otherwise
  data-only for client localization. Any FCM `notification` block emitted by the
  gateway must also have nonblank `title` and `body`. Blank producer copy is
  repaired at the gateway boundary (`Mesha` plus a notification-type label) so
  Android foreground handling and OS background auto-display cannot render an
  empty notification shell.

## Attempt Semantics and Retry Isolation

- **Claiming work is NOT an attempt.** When a dispatcher leases a notification
  row (marks it `sending` with a `lease_token` and `lease_until`), the row is
  reserved for this worker, but no attempt counter is incremented yet. A lease is
  a concurrency guard: only one dispatcher owns a send at a time. An *attempt* is
  when the dispatcher actually tries to send (calls the Gateway, issues the HTTP
  POST, enqueues the SMS/FCM). If a dispatcher crashes after leasing but before
  sending, the row remains unmodified when the lease times out; it is then
  reclaimed and another worker can lease and try again — without burning a retry
  budget.

- **Charge an attempt only when delivery is actually attempted.** Call
  `Service.backoff(attempt)` and increment attempt count only after the send
  gateway is invoked, not after the row is leased. If the send fails (network
  timeout, invalid token, rate limit), it is one attempt. If the send succeeds,
  mark as `sent`. If the send fails after all retries, mark as `exhausted` and
  record DLQ evidence. An attempt counter that increments on lease instead of on
  send silently wastes the retry budget on nothing — a crashed/slow dispatcher
  can burn all retries by leasing rows without sending.

- **Cancellation must release untouched work without consuming retries.** When a
  notification is cancelled (the triggering obligation is cancelled, the user is
  no longer interested, or a guard blocks the send), the row must transition to
  `suppressed` or `cancelled` without incrementing the attempt counter. A cancelled
  row that has never been leased or sent burns no retries and does not count toward
  DLQ quota. A row that was already in-flight (leased or partially sent) when
  cancellation fires must still be handled: if the lease is still active, the
  dispatcher receives a `MarkCancelled` which releases the lease and changes status
  to `cancelled` without a send. If the send was already delivered, the row is
  marked `sent` with a cancellation flag for audit. Never roll-back an in-flight
  send attempt or re-count a delivered notification as a retry.

## Hard rule (do not violate)

**Far-future due state lives in Postgres, never in a transport queue.** Current
kernel-worker stages scan indexed Postgres windows and materialize durable
notification/contact rows. Cloud Tasks or independently scheduled jobs may be
introduced only as measured scale-out adapters; they never become the calendar.

## Firebase / FCM status

The FCM sender, backend device lifecycle, Android `FirebaseMessagingService`,
token registration/refresh, and logout deregistration plumbing are coded. Source
and infrastructure configuration do not prove that production credentials,
tokens, provider reachability, or a real device delivery are live. Treat those
as deployment certification. The outbox, event bus, cadence stages, Postgres
delivery queue, and non-FCM gateways remain testable without Firebase.

## Local / dev

Pub/Sub runs in emulator/local-eventbus mode; the dispatcher supports
`GOATOS_NOTIFICATION_DRY_RUN` to mark channels delivered without external sends.
Adapters are env-selected the same way as the observability sink.
