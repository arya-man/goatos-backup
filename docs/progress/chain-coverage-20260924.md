# Chain coverage: producer → bus → consumer, proven both ways

A prior audit of the 57 registered chains in `context/architecture/domain-event-registry.json`
found that all 57 name an E2E proof, but only 22 go red under BOTH mutations — deleting the
producer's emission and no-op'ing the consumer's handler. The repeated shape is a producer-side
test counting an outbox row beside a consumer-side test that hand-builds the event and calls the
handler directly. Nothing joins them, so the event can stop flowing in silence.

`AGENTS.md` already states the rule this restores: *separate producer and consumer tests are not
closure.*

## The seam

`backend/internal/platform/chaintest` reads the rows the producer's own transaction committed to
`outbox_messages`, decodes each with `eventbus.EventFromEnvelope` — the same decode
`internal/domainconsumer/app` uses on a live Pub/Sub message — and publishes it on the bus the
production wiring registered. It seeds nothing and knows no event type.

`backend/internal/chainproof` holds the tests. It is its own package because a chain test imports
both ends, and a producer's package usually already imports the wiring that registers the
consumer — putting the test in either end is an import cycle.

Where a chain's consumers are not all registered by `eventwiring.RegisterWorkflowConsumers`, the
test drives `domainconsumer/wiring.BuildDomainBus` — the production bus builder — so a consumer
registered nowhere fails the test too.

## Chains closed, and what catches each direction

| Chain | Emission deleted | Handler no-op'd |
|---|---|---|
| `sales.deal.recorded` | no `sales.deal.recorded` in the outbox | no workflow for the recorded sale |
| `goat.sale_allocated` | outbox shows `goat.exited` still going, no `goat.sale_allocated` | tag-animals step stays pending; Feed Director told nothing (2 consumers) |
| `counts.death.reported` | no `counts.death.reported` | no workflow for the reported death |
| `counts.death.rejected` | no `counts.death.rejected` | the workflow is still open after the rejection |
| `procurement.animal_purchase.load_recorded` | no `...load_recorded` | no intake workflow for the load |
| `procurement.animal_purchase.candidate_recorded` | no `...candidate_recorded` | receipt reads pending 0 decided 0 |
| `procurement.animal_purchase.decided` | no `...decided` | (same handler as above) |
| `procurement.feed_purchase.recorded` | no `...feed_purchase.recorded` | no intake workflow for the purchase |
| `procurement.feed_purchase.reached` | no `...feed_purchase.reached` | reached load owes 0 toxin rounds; reached step stays pending (2 consumers) |
| `goat.identity.changed` | story red at the outbox assertion | the obligation is not re-anchored |

Ten chains, every one red under both mutations, each direction caught by a named assertion.

## Defects found on the way

**A production query that could never execute.** `ListRecordedCompletionsByTask` selects
`DISTINCT c.completion_id::text` and ordered by `c.completion_id` — the uuid, not in the select
list. Postgres refuses that (42P10), so every call failed and the SOP verify fan-out could not
list the completions a task review fans out over. Its own dedicated integration test was red at
HEAD; it needs Postgres, and the Postgres gate is opt-in, which is how a query that cannot run
reached main.

**A consumer missing from the bus the story suite relays through.**
`domainconsumer/wiring.BuildDomainBus` carried `FeedPurchaseReachedHandler` on neither half while
three other bus compositions did. That builder's own comment states the consequence. It has no
production caller, so this is a test-bus divergence rather than a live drop — but it is exactly
why the feed-purchase → toxin chain was never measured.

**The whole vaccination SOP E2E path was dead**, for three stacked reasons, each hiding the next:
no park operator (so the sweeper planned a batch with no drive assignment and the shed read as
holding no animals), no shed video (the helper dropped its `shedID` argument and submitted
per-animal clips against a shed-grain SOP), and no shed scope on the submit key. Fixed in the
helper; `TestKernelStoryY_CrossVaccineGap` and both `VaccRev` stories now pass.

**Two clock-rotten proofs.** `goat.identity.changed`'s story pinned absolute dates while
`IdentityGoat` stamps `occurred_at` with SERVER time, so once real time passed DOB+28 the
corrected anchor fell into the past and was clamped. It also asserted the obsolete obligation was
*superseded*, describing a supersede-and-recreate the kernel does not do — the obligation id is
identical either side of the correction, so that assertion could never pass and hid the two real
proofs behind it.

**A package of proofs that could not run at all.** `internal/identity/adapters/postgres` ran its
own `docker run` instead of `platform/pgtest`, so it ignored `GOATOS_PGTEST_ADMIN_DSN` — the
sanctioned no-Docker path `pgtest` exists for, in its own words because "on machines where
Docker/Colima is disallowed the alternative is not 'run it another way' but 'the gate never
runs'". Worse, those tests call `pgtest.SkipIfNoDocker`, which deliberately does NOT skip once a
database is supplied, so with the DSN set they FAIL rather than skip. The package is a named proof
for FOUR registered chains (`goat.created`, `goat.stage_changed`, `goat.identity.changed`,
`goat.identifier.added`). Its `TestMain` already called `pgtest.RunMain`, so the conversion had
been begun and abandoned.

Converting it surfaced two defects it had been hiding:

- **`FindIdentifierMatches` was invalid SQL.** It embeds `goatSummaryColumns()`, which renders
  `gsp.partition_label` and `gsp.source_shed_name`, but does not join `goat_shed_partitions`. The
  join was added to the shared column list and to the goat-summary query beside it and not here,
  so every call failed 42P01 `missing FROM-clause entry for table "gsp"`. This is the RFID resolve
  path.
- **A fixture asserted a banned shape.** It demanded `operational_location_display == "Castro - 1"`.
  A bare numeric partition joins with a SPACE — `Castro 1` is the name painted on the building —
  and `AGENTS.md` lists the dash-separated numeric pen among the shapes that are NEVER rendered.
  The code was right; the fixture demanded the banned form. That is the defect the same rule names
  in its own words: *a fixture that asserts a shape the farm does not have is a defect even when
  the assertion passes.*

Four more packages carry the same own-docker harness and are reported rather than converted:
`outbox`, `permissions`, `locations`, `bulkstatus`, plus `tests/scale`.

**Six proofs red at HEAD in `counts/adapters/postgres`**, verified identical at the base commit so
they are pre-existing and untouched: the five `TestCompleteShifting*` cases (named proofs for
`goat.stage_changed`) and `TestCountsBreakdownStageLabelsParkScopeHierarchyKeepsRawCodesScoped`.
`TestCompleteShiftingIntoUnconfiguredShedFailsClosed` reports "completion into an unconfigured
shed succeeded, want a fail-closed error", which is a fail-open on a placement rule and worth
reading first.

## Live-herd predicates: the sweep

A sweep flagged 20 packages whose production code filters the live herd but whose fixtures build
only alive animals. That flag is a keyword heuristic, so each was checked properly: neutralise the
predicate, run the package, see whether anything goes red.

| Package | Result |
|---|---|
| `animalpurchase` | **UNPROVEN** — all green; now covered (breed picklist) |
| `penroutines` | **UNPROVEN** — all green; now covered (occupied-pen flag) |
| `feedconfig` | proven — `TestRetiredPartitionsAreNeitherListedNorWritable` bites |
| `protocol` | proven — `TestPublishVersionRetiresPreviousPlanPublishedAnchors` bites |
| `pccare` | proven — four tests bite |
| `sop` | proven — four tests bite |

Two of seven were genuinely unmeasured; the heuristic over-flagged the rest, which is why each was
mutated rather than taken on the flag.

## E2E suite, before and after

Run in full on a local throwaway Postgres, both at the base commit and on this branch:

```
base commit   31 failing stories
this branch   28 failing stories
regressions    0
```

The three fixed are `TestKernelStoryY_CrossVaccineGap`,
`TestKernelStoryVaccRev_IdentityCorrectionRecompute` and
`TestKernelStoryVaccRev_HistoryOutranksDOBCorrection`. The remaining 28 are pre-existing and
untouched by this work.

## Running these

Opt-in Postgres tests against a local throwaway server, never a deployed host:

```
GOATOS_RUN_POSTGRES_TESTS=1 \
GOATOS_PGTEST_ADMIN_DSN='postgres://postgres@127.0.0.1:<port>/postgres?sslmode=disable' \
go test ./internal/chainproof/ -count=1
```
