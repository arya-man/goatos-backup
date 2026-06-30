# Feed Direction Feature Closure Plan

**Status:** Reset plan for the next Feed Direction goal
**Date:** 2026-06-30
**Purpose:** Keep the next implementation run product-shaped, bounded, and
source-backed so it can finish without adding endless evidence work.

## 1. Source Hierarchy

Use this hierarchy before building or changing Feed Direction behavior:

1. `Goats and Parks.docx`, captured in
   `context/source-findings/goats-and-parks-source-findings.md`, is the base
   source for goat identity, park/shed scope, shed tags, lifecycle/stage,
   pregnancy, lactation, warm-up, fattening, milking, weighing, feed roles, and
   feed-safety semantics.
2. `Feed, Shiftings and Count.docx` v1.1 is the Feed Direction source for Base
   Count, Shifting ledger, one-day projection, Feed Direction clocks, Diff,
   bridge, as-fed quantities, and ration behavior.
3. The Feed Transfer KT, Counting DB, Feed Directions Automation DB, Sheds DB,
   and legacy Apps Script code are evidence for imports, parity, migration,
   validation gaps, and stage behavior. They are not runtime truth unless a
   reviewed protocol/config version publishes the relevant rule.

This means a workbook number such as `80/20`, a KT quantity example, an old
Apps Script trigger time, or a dirty sheet label cannot become product behavior
by being present in source evidence. It must map to reviewed, typed,
effective-dated config or remain draft/rejected evidence.

## 2. Product Boundary

The next build must close the feature in product milestones, not in an
open-ended search for more proofs.

| Order | Product milestone | Completion boundary |
| --- | --- | --- |
| 1 | Safe feed input projection | Feed can ask, for a target date and park/shed scope, which projected shed + breed + feed-relevant cohort rows are safe to calculate and which rows are blocked with an explicit reason. |
| 2 | Ration, template, and session config | Approved parameter rows exist for feed vectors, ration dimensions, eligibility, slot weights, validation, calculation preview, source metadata, and publish authority. Examples remain examples until published. |
| 3 | Draft Feed Direction generation | Resolved input rows become immutable generation rows with as-fed quantities; blocked rows stay blocked and do not silently use averages. |
| 4 | Packing, transport, distribution, and proof | The kernel creates stage work for packing/transport/distribution, captures proof, verifies or rejects proof, and reissues work when proof or quantity is wrong. |
| 5 | Consumption, wastage, refusal, and feed-safety rework | Underfeed, overfeed, moist/stale leftovers, refusal-to-eat, sickness-risk remarks, and high-risk destination shortages create visible exception/rework paths. |
| 6 | Backend APIs, read models, and frontend | API contracts, generated clients, bounded reads, command buckets, admin/operator UI, and mock-fidelity surfaces agree. |
| 7 | Real local E2E and closeout | Seeded Postgres, workers, API, obligations, proof, verification, read models, UI proof, tests, docs, and pushed `main` all agree. |

Gate IDs in the dependency docs are traceability labels for these milestones.
Do not describe feature progress as "G2 is not green" when the real question is
"can Feed safely calculate or block tomorrow's shed/cohort rows?"

## 3. Safe Feed Input Definition

Feed input safety is complete when the system can produce a bounded projection
view with all of the following for each returned row:

- tenant, target date, park, shed, breed, head count, source snapshot/hash, and
  projection horizon;
- reviewed physical source of count truth: accepted Base Count plus the
  applicable ShiftingEvent ledger for the horizon;
- applied future-effective shiftings exactly once for the projection date;
- feed-relevant cohort signals where available or required: shed tag/stage, age
  class, sex, pregnancy, lactation, warm-up, mother/milking/fattening, ICU or
  quarantine risk, and alias review state;
- ration-context resolution state: resolved to one or more reviewed nutrition
  cohort keys, or blocked with an explicit blocker reason;
- visible exception/rework rows for missing Base Count, missing projection,
  unresolved shed tag, alias conflict, destination shortage, stale context,
  unreported shifting, count mismatch, and similar blockers;
- tenant/date/park/shed bounded reads with cursor/keyset behavior where lists
  can grow.

If that predicate passes, the build must move to ration/config and generation.
It must not keep adding workbook scans, trigger inventories, or parity commands
unless the predicate names an exact missing implementation item.

The following are not required to finish safe feed input:

- copying old workbook formulas into GoatOS;
- making every legacy Apps Script trigger a GoatOS schedule;
- RFID-to-shed per-goat derivation for the first slice;
- a full UI or generation worker before the projection contract can tell Feed
  what is safe and what is blocked;
- proving every future workbook row before closing the local product boundary.

## 4. Animal-Safety Rules From Goats And Parks

`Goats and Parks.docx` makes feed safety business-critical, not cosmetic:

- Pregnant, lactating, warm-up, weaning, fattening, ICU, and quarantine cohorts
  cannot be hidden inside a normal shed average.
- When a high-risk cohort moves to a destination shed, Feed must re-resolve that
  destination shed's nutrition context before calculating quantities.
- Underfeeding can create pregnancy, kid-loss, growth, and herd-scale risk.
- Overfeeding can create waste, moist or stale leftovers, refusal-to-eat,
  digestive upset, and sickness risk.
- Feed changes must be gradual and source-backed; wrong data blocks or creates
  repair work instead of being silently normalized.
- Panels are cleaned before feed is added; packing/distribution proof and
  consumption/wastage proof must preserve that operational shape.

The implementation must prove both sides: not too little feed for a shifted
pregnant/warm-up cohort, and not too much unsafe surplus.

## 5. Admin-Editable Sessions And Templates

The default published session policy starts from the source two-slot model:
`09:00` and `15:00`, with the current documented `50/50` split. This is a
default policy, not a code limit.

Admins with the approved Feed Director/COO/CEO authority path may add, disable,
reorder, or reweight slots only through versioned Feed Direction protocol/config
with:

- active/effective dates;
- slot codes, labels, serving times, sort order, and weights;
- validation that active slot weights cover the full daily as-fed quantity for
  each applicable scope/feed item;
- calculation preview before publish;
- supersession behavior for already-generated dates;
- audit, source metadata, reviewer, approver, and publish capability.

KT or workbook slot examples are candidates only until they pass that path.

## 6. Loop Guard

Before building a milestone, write or identify its checker first:

```text
required inputs present
  + exact blockers absent or visible
  + owner-decision gaps separated from implementation gaps
  -> milestone can pass
```

If the same milestone still fails after two implementation fixes, stop and
inspect the checker/boundary. Do not add a third adjacent proof command just
because the status still says pending.

Use separate states:

- `needs_impl`: code, migration, API, worker, UI, or test is actually missing.
- `needs_decision`: source/owner must approve, reject, or defer a business rule.
- `blocked`: required source/config/proof is missing and Feed must fail closed.
- `ready`: the milestone predicate passes and later work may consume it.

## 7. Remaining Work Projection

This is the realistic planning projection for the full Feed Direction feature,
not only the safe input milestone:

| Product milestone | Current position | Remaining estimate |
| --- | --- | --- |
| Safe feed input projection | Mostly built through Counts/Shifting projection, blockers, preview, evidence ledger, and high-risk fixture proof; needs the bounded finalizer/checker and any exact missing local proof it reports. | 1-3 hours |
| Ration/template/session config | Source docs are strong; runtime typed config/import/review/publish path is not finished. | 2-4 hours |
| Draft generation | Generation preview exists; immutable generated quantity rows and run worker still need implementation. | 3-5 hours |
| Stage obligations and proof | Kernel exists; Feed-specific packing/transport/distribution/bridge stage wiring is still open. | 4-6 hours |
| Consumption, wastage, refusal, sickness-risk rework | Requirements exist; typed exception/rework paths still need implementation and tests. | 2-4 hours |
| APIs/read models/frontend | Some readiness/preview APIs exist; full command buckets, generated clients, and mock-fidelity UI remain open. | 4-8 hours |
| Real local E2E and closeout | Not complete for full Feed Direction. | 3-6 hours |

Total remaining focused build time for full end-to-end closure: about 19-36
hours. The safe input milestone alone should not take another long loop if the
checker is built first and scope stays bounded.

## 8. Updated Goal Text

Use this as the next goal text:

```text
Build GoatOS Feed Direction end-to-end as a product feature, using Goats and Parks.docx as the base goat/park/shed-tag/feed-safety source and Feed, Shiftings and Count.docx as the Feed Direction timing/Diff/ration source. Close the work in product milestones: (1) safe projected shed/cohort feed inputs from Counts/Shifting, bounded to accepted counts, approved shiftings, shed tag/cohort state, and explicit blockers; (2) reviewed ration/template/session configuration where examples such as 80/20 or workbook rows are evidence only until published; (3) draft feed instruction generation for resolved rows and blocked instruction rows for unsafe inputs; (4) packing/transport/distribution obligations with proof, panel-cleaning/session behavior, and bridge/proof refs; (5) consumption, wastage, refusal, moist/stale feed and sickness-risk exception/rework paths; (6) backend-owned APIs/read models, generated clients, mock-fidelity frontend, and real local Postgres/API/UI E2E proof. Do not add more source-proof work unless the milestone checker names an exact missing item; separate implementation gaps from owner-decision gaps.
```
