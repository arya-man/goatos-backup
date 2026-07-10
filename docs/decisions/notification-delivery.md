# Notification & Event Delivery on GCP (the "we used SQS" answer)

Status: **documents existing implementation** (the pipeline is already built and
wired; this doc is the map, not a proposal).

## TL;DR

You do **not** rebuild SQS on GCP. Goat OS follows the operational kernel: a
**transactional Postgres outbox** is the source of truth, an **outbox relay**
moves durable events to **Pub/Sub**, **Cloud Tasks** handles near-term timed
dispatch/retry, **Cloud Scheduler** drives the sweeper that scans indexed
Postgres due-windows, and device push lands via **FCM** behind a replaceable
gateway port. Every hop is idempotent and replay-safe.

See `context/architecture/operational-kernel.md` (line ~118: "Kafka / SQS event
bus → transactional Postgres outbox → outbox relay → Pub/Sub topic/subscription
with DLQ") and `context/architecture/operational-kernel-system-design.md`.

## AWS → GCP mapping

| AWS (SQS-based push) | GCP equivalent | Why |
|---|---|---|
| SQS work queue | **Cloud Tasks** | per-message HTTP dispatch, named-task dedup, retry/backoff, per-queue rate limit — the closest 1:1 to an SQS-driven sender |
| SQS/SNS event bus, fan-out | **Pub/Sub** topic/subscription | event spine, at-least-once, many idempotent consumers |
| SQS FIFO (order + dedup) | Pub/Sub **ordering keys** + exactly-once subscription | per-recipient / per-obligation ordering |
| SQS DLQ | Pub/Sub **dead-letter topic** + parked Postgres row | failed-after-N visibility |
| SQS visibility timeout | ack deadline / **Postgres lease token** | in-flight lease; stale leases reclaimed |
| SQS delay | Cloud Tasks `schedule_time` / row `next_attempt_at` | near-term timers |
| CloudWatch cron | **Cloud Scheduler** → sweeper | due / reminder / escalation ticks |
| SNS → APNs/GCM device push | **FCM** behind `notification/ports.Gateway` | last-hop device push |

## How it is actually built in this repo

```
business txn ──(ONE Postgres txn)──▶ canonical row + audit + OUTBOX event (idempotency key + fingerprint)
outbox relay  cmd/outbox-relay  ──▶ Pub/Sub  (internal/outbox/adapters/publisher/pubsub/)
domain event consumer  cmd/domain-event-consumer  ◀── Pub/Sub sub (internal/domainconsumer/adapters/pubsub)
        │  builds obligations / calendar events / durable notification_requests rows
Cloud Scheduler ──tick──▶ sweeper  cmd/obligation-sweeper  scans indexed Postgres due-windows
        │  near-term dispatch/retry ──▶ Cloud Tasks (internal/platform/taskqueue/cloudtasks.go)
notification dispatcher  cmd/notification-dispatcher  ──▶ notification/app.Service.RunOnce
        │  lease-claim durable rows ──▶ Gateway.Send ──▶ Slack | email | FCM | incident(Opsgenie/PagerDuty)
```

### Component index (source of truth = code)

| Concern | Code |
|---|---|
| Event spine (outbox → Pub/Sub) | `internal/outbox/adapters/publisher/pubsub/{publisher,gcp}.go`, `cmd/outbox-relay` |
| Pub/Sub consumer | `internal/domainconsumer/adapters/pubsub/subscriber.go`, `cmd/domain-event-consumer` |
| Near-term timed dispatch/retry | `internal/platform/taskqueue/cloudtasks.go` |
| Scheduler-driven sweeper | `cmd/obligation-sweeper` |
| Delivery queue + retry + DLQ | `internal/notification/{domain,ports,app,adapters/postgres}` |
| Multi-channel send (incl. FCM) | `internal/notification/adapters/gateway/gateway.go` |
| Worker entrypoint | `cmd/notification-dispatcher` |

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

## Hard rule (do not violate)

**Far-future due state lives in Postgres, never in the queue.** Cloud Tasks is
near-term dispatch/retry only; Cloud Scheduler drives a sweeper that scans
indexed Postgres windows. The queue is transport; Postgres is truth. A queue is
not a calendar database.

## Firebase / FCM status (deferred)

The FCM sender (`gateway.sendFCM`, OAuth2 bearer via a service-account token
source) is **coded**, but the Firebase project + credentials are a **deferred,
org-gated step** (see `docs/mobile/firebase-india-setup.md`; maintainer decision
to defer). Everything up to the last hop — outbox, relay, Pub/Sub, Cloud Tasks,
Scheduler, sweeper, the notification delivery queue, and every non-FCM channel —
needs **zero Firebase** and runs today. When Firebase is unlocked, only the FCM
adapter's credentials get wired; no pipeline change.

## Local / dev

Pub/Sub and Cloud Tasks run in emulator / local-eventbus mode; the dispatcher
supports `GOATOS_NOTIFICATION_DRY_RUN` to mark channels delivered without
external sends. Adapters are env-selected the same way as the observability sink.
