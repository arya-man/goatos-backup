# Chain coverage: producer → bus → consumer, proven both ways

A prior audit of the 57 registered chains in `context/architecture/domain-event-registry.json`
found that all 57 name an E2E proof, but only 22 go red under BOTH mutations — deleting the
producer's emission and no-op'ing the consumer's handler. The repeated shape is a producer-side
test that counts an outbox row beside a consumer-side test that hand-builds the event and calls
the handler directly. Nothing joins them, so the event can stop flowing silently.

`AGENTS.md` already states the rule this restores: *separate producer and consumer tests are not
closure.*

## The seam

`backend/internal/platform/chaintest` reads the rows the producer's own transaction committed to
`outbox_messages`, decodes each with `eventbus.EventFromEnvelope` — the same decode
`internal/domainconsumer/app` uses on a live Pub/Sub message — and publishes it on the bus the
production `RegisterXConsumers` wired. It seeds nothing and knows no event type.

`backend/internal/chainproof` holds the tests. It is its own package because a chain test imports
both ends, and a producer's package usually already imports the wiring that registers the
consumer — putting the test in either end is an import cycle.

## Closed and mutation-proven

| Chain | Test | Emission deleted | Handler no-op'd |
|---|---|---|---|

| `sales.deal.recorded` | `TestSaleRecordedChainOpensTheWorkflow` | RED — `DrainExpecting` finds no `sales.deal.recorded`; the outbox carried only `config.changed` | RED — `WorkflowIDBySubjectRef`: `tasks: not found` |

Under the handler no-op, the chain's previously registered proof
(`TestSaleWorkflowRunsTheSalesSOP`) stays **green**, which is the audit's finding reproduced: it
calls `repo.OpenWorkflow` directly, one layer below the handler and two below the producer. That
test is a good test of the SOP's compiled steps and stays; it simply cannot see whether the event
still flows.

## Running these

They are opt-in Postgres tests against a local throwaway server, never a deployed host:

```
GOATOS_RUN_POSTGRES_TESTS=1 \
GOATOS_PGTEST_ADMIN_DSN='postgres://postgres@127.0.0.1:<port>/postgres?sslmode=disable' \
go test ./internal/chainproof/ -count=1
```
