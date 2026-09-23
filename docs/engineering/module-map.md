# Goat OS module map — what each backend module owns, emits, consumes, and whether it is tested

Derived, not remembered. Every figure below was counted from a file this run actually READ;
a file that could not be read is an error, never a zero. Re-derive with:

```
python3 tools/module-map/derive-module-map.py --json out.json
```

**This run read 2318 distinct Go files (6932 read calls
across the passes) with 0 read errors.** The two numbers are kept apart on
purpose: a re-read is not a second file, and reporting the larger one would be a count of work
done rather than of ground covered.

## The scale, re-derived

| measured | value | matches brief |
|---|---|---|
| directories under `backend/internal/` | 73 | yes (73) |
| …of those, **carrying no Go code at all** (`.gitkeep` only) | 7 | not previously stated |
| …of those, **package-doc stub only** (`doc.go`, no funcs) | 2 | not previously stated |
| **modules with working Go code** | 64 | — |
| admin-web routes (`page.tsx`) | 67 | yes |
| Android feature modules | 22 | yes |
| OpenAPI paths | 343 | yes |
| migrations | 363 | yes |
| registered domain events | 81 | yes |
| …with ≥1 consumer | 57 | yes |
| route patterns in `permissions/routes.go` | 485 | — |

**The first correction the map makes: 73 is a count of directories, not of modules.** Seven are
`.gitkeep` placeholders (`analytics_export`, `breeding`, `devices`, `forms`, `genetics`, `growth`,
`media`) and two are package-doc stubs with no functions (`movement`, `submissions`). Quoting 73
as the number of things that can break overstates it by nine.

## How coupling was decided

On the graph, never on a module's own prose. A module is **coupled** when at least one of these
is true, and the row names which: it publishes or subscribes to a domain event; it imports
another module's package; another module imports it. A module is **independent** only when none
of the three holds.

**63 coupled, 10 independent** — and every one of the 10 independent
modules is one of the nine empty/stub directories plus `seedrun` (1 file, 1 test, imported by
nobody). **No module that carries real code is independent.** That is the answer to the
coordinator's "weighing, sales, procurement": there is no module here you can reason about alone.

## What is tested, and what that word is worth here

Per the contract, *tested* means a named test shown to FAIL when the behaviour is reverted.
**This agent mutation-proved nothing**, so every status below is `claimed`: a test file exists,
or the registry names an `e2eProof` file. Whether any of it bites is unmeasured. Reported as
`claimed` throughout rather than rounded up to `tested`.

Modules with **zero `_test.go` files**: 11 — of which 2 carry real code: `feedsop`, `pccaresop`.

## The 73 rows

`owns` = tables it writes (INSERT/UPDATE/DELETE). `reads elsewhere` = tables another module
writes. `emits`/`consumes` = event types, from code and registry together.

| # | module | kind | coupling | owns | reads elsewhere | emits | consumes | tests | evidence |
|---|---|---|---|---|---|---|---|---|---|
| 1 | `adminui` | domain | coupled | 11 | 19 | 0 | 0 | 35 | imports |
| 2 | `alerts` | domain | coupled | 6 | 14 | 0 | 0 | 6 | imports |
| 3 | `analytics_export` | EMPTY | independent | 0 | 0 | 0 | 0 | 0 | — |
| 4 | `animalpurchase` | domain | coupled | 4 | 4 | 6 | 0 | 9 | emits,imports |
| 5 | `appanalytics` | domain | coupled | 1 | 0 | 0 | 0 | 2 | imported_by |
| 6 | `appconfig` | domain | coupled | 0 | 0 | 0 | 0 | 2 | imported_by |
| 7 | `bootstrap` | domain | coupled | 0 | 6 | 1 | 0 | 3 | emits,imports |
| 8 | `breeding` | EMPTY | independent | 0 | 0 | 0 | 0 | 0 | — |
| 9 | `browserpush` | domain | coupled | 2 | 5 | 0 | 0 | 3 | imported_by |
| 10 | `bulkstatus` | domain | coupled | 6 | 5 | 1 | 0 | 3 | emits,imports |
| 11 | `calendar` | domain | coupled | 11 | 29 | 18 | 1 | 17 | emits,consumes,imports |
| 12 | `ceoai` | domain | coupled | 6 | 12 | 5 | 0 | 63 | emits,imports |
| 13 | `configuration` | domain | coupled | 29 | 13 | 3 | 0 | 12 | emits,imports |
| 14 | `counts` | domain | coupled | 27 | 38 | 13 | 6 | 76 | emits,consumes,imports |
| 15 | `countsbridge` | bridge | coupled | 0 | 0 | 0 | 0 | 2 | imports |
| 16 | `countssop` | domain | coupled | 0 | 2 | 0 | 0 | 2 | imports |
| 17 | `devices` | EMPTY | independent | 0 | 0 | 0 | 0 | 0 | — |
| 18 | `domainconsumer` | bridge | coupled | 2 | 1 | 2 | 0 | 8 | emits,imports |
| 19 | `eventwiring` | bridge | coupled | 0 | 2 | 0 | 0 | 2 | imports |
| 20 | `feed` | domain | coupled | 1 | 1 | 3 | 0 | 4 | emits,imports |
| 21 | `feedconfig` | domain | coupled | 15 | 14 | 0 | 0 | 6 | imported_by |
| 22 | `feeddirection` | domain | coupled | 17 | 33 | 11 | 2 | 77 | emits,consumes,imports |
| 23 | `feedsop` | domain | coupled | 0 | 2 | 0 | 0 | 0 | imports |
| 24 | `feedwaterremoval` | domain | coupled | 0 | 2 | 0 | 0 | 3 | imported_by |
| 25 | `forms` | EMPTY | independent | 0 | 0 | 0 | 0 | 0 | — |
| 26 | `genetics` | EMPTY | independent | 0 | 0 | 0 | 0 | 0 | — |
| 27 | `growth` | EMPTY | independent | 0 | 0 | 0 | 0 | 0 | — |
| 28 | `growthdirector` | domain | coupled | 7 | 21 | 4 | 0 | 9 | emits,imports |
| 29 | `health` | domain | coupled | 20 | 19 | 18 | 5 | 42 | emits,consumes,imports |
| 30 | `herdsignals` | domain | coupled | 6 | 12 | 4 | 0 | 11 | emits |
| 31 | `identity` | domain | coupled | 31 | 16 | 16 | 0 | 23 | emits,imports |
| 32 | `inventory` | domain | coupled | 8 | 3 | 2 | 0 | 4 | emits,imports |
| 33 | `kernelstages` | bridge | coupled | 4 | 7 | 6 | 0 | 15 | emits,imports |
| 34 | `leadershiptasks` | domain | coupled | 10 | 4 | 5 | 0 | 12 | emits,imports |
| 35 | `locations` | domain | coupled | 8 | 3 | 4 | 0 | 2 | emits |
| 36 | `market` | domain | coupled | 5 | 2 | 1 | 0 | 4 | emits,imports |
| 37 | `media` | EMPTY | independent | 0 | 0 | 0 | 0 | 0 | — |
| 38 | `movement` | doc-stub | independent | 0 | 0 | 0 | 0 | 0 | — |
| 39 | `notification` | domain | coupled | 9 | 5 | 0 | 0 | 7 | imported_by |
| 40 | `notificationaudience` | domain | coupled | 3 | 1 | 0 | 0 | 5 | imports |
| 41 | `notificationbridge` | bridge | coupled | 2 | 12 | 21 | 32 | 38 | emits,consumes,imports |
| 42 | `notificationcentre` | domain | coupled | 4 | 4 | 0 | 0 | 4 | imported_by |
| 43 | `obligation` | domain | coupled | 42 | 27 | 19 | 6 | 72 | emits,consumes,imports |
| 44 | `operationsaudit` | domain | coupled | 0 | 2 | 0 | 0 | 2 | imported_by |
| 45 | `outbox` | domain | coupled | 5 | 2 | 3 | 0 | 10 | emits |
| 46 | `parkscope` | domain | coupled | 4 | 1 | 0 | 0 | 1 | imported_by |
| 47 | `passport` | domain | coupled | 0 | 5 | 0 | 0 | 2 | imports |
| 48 | `pccare` | domain | coupled | 15 | 25 | 9 | 3 | 31 | emits,consumes,imports |
| 49 | `pccaresop` | domain | coupled | 0 | 2 | 0 | 0 | 0 | imports |
| 50 | `penroutines` | domain | coupled | 14 | 12 | 7 | 3 | 5 | emits,consumes,imports |
| 51 | `penvisits` | domain | coupled | 6 | 9 | 7 | 4 | 8 | emits,consumes,imports |
| 52 | `permissions` | domain | coupled | 5 | 8 | 0 | 0 | 32 | imports |
| 53 | `platform` | infrastructure | coupled | 1 | 10 | 2 | 0 | 56 | emits,imports |
| 54 | `processintegrity` | domain | coupled | 0 | 34 | 0 | 0 | 16 | imports |
| 55 | `procurement` | domain | coupled | 44 | 12 | 19 | 0 | 40 | emits,imports |
| 56 | `proof` | domain | coupled | 2 | 11 | 0 | 0 | 7 | imports |
| 57 | `protocol` | domain | coupled | 10 | 11 | 1 | 0 | 12 | emits |
| 58 | `sales` | domain | coupled | 10 | 8 | 4 | 0 | 13 | emits |
| 59 | `seedrun` | domain | independent | 0 | 0 | 0 | 0 | 1 | — |
| 60 | `shiftingsop` | domain | coupled | 0 | 2 | 0 | 0 | 3 | imports |
| 61 | `sop` | domain | coupled | 13 | 16 | 2 | 0 | 20 | emits,imports |
| 62 | `sopbridge` | bridge | coupled | 0 | 1 | 3 | 0 | 4 | emits,imports |
| 63 | `submissions` | doc-stub | independent | 0 | 0 | 0 | 0 | 0 | — |
| 64 | `tasks` | domain | coupled | 7 | 22 | 0 | 15 | 43 | consumes,imports |
| 65 | `toxin` | domain | coupled | 4 | 2 | 4 | 1 | 11 | emits,consumes,imports |
| 66 | `vaccination` | domain | coupled | 15 | 37 | 8 | 13 | 34 | emits,consumes,imports |
| 67 | `vaccinationexecution` | domain | coupled | 10 | 51 | 2 | 0 | 52 | emits,imports |
| 68 | `verification` | domain | coupled | 20 | 14 | 9 | 0 | 44 | emits,imports |
| 69 | `verificationcatalog` | domain | coupled | 0 | 2 | 0 | 0 | 1 | imports |
| 70 | `weighing` | domain | coupled | 44 | 23 | 24 | 2 | 121 | emits,consumes,imports |
| 71 | `weighingsop` | domain | coupled | 0 | 2 | 0 | 0 | 3 | imports |
| 72 | `workboard` | domain | coupled | 0 | 7 | 0 | 0 | 8 | imports |
| 73 | `workforce` | domain | coupled | 28 | 16 | 9 | 0 | 42 | emits,imports |

Full per-row detail — every table name, every event type, every test path — is the JSON the
script writes. The table above is the index into it.

## The six connector modules

They matter because **a per-screen check can never reach them**: they have no screen, no route
and no table. If what they carry breaks, nothing anywhere fails.

They are not one kind of thing. Two shapes, and the second is the more dangerous:

### Event wirers — they register handlers they do not own

The handler's `Register` method lives in the owning module; the bridge only calls it. So the
coupling is invisible both to a scan of the bridge and to a scan of the owner's wiring.

| bridge | distinct events carried | for modules |
|---|---|---|
| `eventwiring` | 17 | `counts`, `feeddirection`, `health`, `pccare`, `penroutines`, `penvisits`, `tasks`, `weighing` |
| `domainconsumer` | 46 | `calendar`, `counts`, `health`, `notificationbridge`, `obligation`, `vaccination` |
| `kernelstages` | 54 | `calendar`, `counts`, `health`, `notificationbridge`, `obligation`, `pccare`, `penvisits`, `toxin`, `vaccination` |

`kernelstages` is the widest single point of failure in the backend: **54 event types across 9
modules**, plus imports of 33 modules. `domainconsumer` carries 46 across 6.

- `domainconsumer`: could not resolve the events behind `eventwiring.NewWorkflowConsumerService` — **not zero, unread.**
- `kernelstages`: could not resolve the events behind `eventwiring.NewWorkflowConsumerService` — **not zero, unread.**

### Synchronous port adapters — no events at all

`countsbridge`, `sopbridge` and `notificationbridge` carry **0** wired events, and that is a
finding rather than a clean result. They couple by direct in-transaction function call:
`countsbridge` adapts verification's `CreateItem` to four counts ports (birth capture, milk
preparation, milk feeding, pen reconciliation); `sopbridge` wires the obligation sweeper's
`TaskCreator` to SOP's `CreateTask` and fans a verified task out to vaccination; 
`notificationbridge` is itself a consumer of **32 event types** rather than a wirer.

**A call-coupling has no event, so it has no registry row and cannot have one.** The domain-event
registry structurally cannot describe these three. Anything that measures coupling by reading the
registry scores them zero and reads that as independence.

## Couplings the prose states, checked against the registry

Each was looked up by event type in `domain-event-registry.json`.

| coupling stated in `AGENTS.md` | registry row | consumers | proof files named |
|---|---|---|---|
| sale allocation → Feed Director notice | **yes** `goat.sale_allocated` | notification, tasks | 3 |
| animal exits (dead/sold) → obligations drop | **yes** `goat.exited` | obligation, tasks, health | 3 |
| goat location changed → vaccination rescope | **yes** `goat.location.changed` | vaccination, obligation | 4 |
| verification verdict → feed & weighing completion | **yes** `verification.verdict.approved` | 13 consumers | 15 |
| pen visit owed the day after a care task | **yes** `pen_visit.verified` | pc_care | 2 |
| shifting counts toward the feed sheet before approval; 14:00 correction reopens packed pens | **NO EVENT** | — | — |
| lump-sum census snapshot taken inside the submit transaction | **NO EVENT** | — | — |
| clinical state defers vaccination rather than cancelling | **NO EVENT** | — | — |

The last three are real farm behaviour with no event and therefore no registry row — by design,
because each happens **inside one SQL transaction**, not across a bus. They are not registry
gaps. They are couplings the registry is the wrong instrument for, which is the same blind spot
as the three port-adapter bridges above, and it is the larger half of this system's coupling.

## Event integrity — five findings

**5 event types are subscribed to in code
with no registry row at all:**

- `counts.base_count_anchor.recorded` — consumed by `counts`
- `counts.shifting_event.recorded` — consumed by `counts`
- `goat.shifted` — consumed by `obligation`
- `vaccination.verify.accepted` — consumed by `notificationbridge`, `vaccination`
- `vaccination.verify.rejected` — consumed by `notificationbridge`, `vaccination`

Two of them, `vaccination.verify.accepted` and `vaccination.verify.rejected`, are **published by
`sopbridge`'s verify fan-out and consumed by two modules** — a live two-module coupling the
registry does not list. It was missed by the first pass because the publisher passes the event
type through a variable rather than a literal.

**`goat.shifted` has a subscriber and no production producer.** `obligation` subscribes; the only
publishers in the tree are its own integration test and an e2e story. The test therefore proves
the handler and never the wiring — it publishes the event it is testing for. The module's own
comment calls it "legacy in-process alias kept for replay/tests while producers migrate"; that is
the module's claim, recorded as such, and it matches what the graph shows.

**24 registered events have zero consumers** (81 registered, 57 with one).

## What could not be derived — stated, not rounded

1. **No coverage figure is mutation-proven.** Every `tests` count is file existence. Whether one
   test fails when its behaviour is reverted is unmeasured for all 73.
2. **Producers written in SQL are invisible to this script.** `counts.base_count_anchor.recorded`
   and `counts.shifting_event.recorded` are produced by an INSERT into `outbox_messages` inside a
   transaction — confirmed by their partial unique indexes in migration 000001 — so they read as
   producer-less here. **This blind spot makes the orphan list LONGER than the truth, never
   shorter**, so the three names above are an upper bound on dead subscriptions, not a floor.
3. **Event types passed through a variable** are resolved only by a weaker string-and-`.Publish(`
   candidate pass, labelled separately in the JSON and never merged into the literal evidence.
4. **Table ownership is inferred from SQL text in Go files**, not from migrations. A table written
   only by a migration or only by sqlc-generated code is missed.
5. **Two bridges wire `eventwiring.NewWorkflowConsumerService`**, whose events this run could not
   resolve. Recorded as unresolved, not as none.
6. **Screens and API paths are not attributed per module.** The 67 admin-web routes, 22 Android
   modules and 343 OpenAPI paths are counted but not mapped to owners; `permissions/routes.go`
   keys routes by operation ID, not by module, so ownership would be a guess.
