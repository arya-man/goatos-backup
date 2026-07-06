# Pre-Google Review Issue Ledger

This ledger maps the older multi-agent C/H/M/L review findings to the current
Google dev clean-slate decision. It is a handoff document for `goatos-dev`, not
a replacement for the source code, tests, or runbooks.

Source review basis:

- Older attached review snapshot: `05dfe68`.
- Earlier Codex Goat OS review snapshot: `05dfe68`.
- Current recheck basis: `main` after `71e33f8` on 2026-06-29.
- Deployment mode: clean-slate Google dev runtime rehearsal, not legacy cutover.

## Decision

Do not fix every item in the older 32-item review before Google dev. The
pre-Google code blockers are closed or explicitly bounded. Start Google dev only
after `make pre-google-readiness` passes on a clean worktree.

The remaining work is dev evidence or later backlog:

- Prove bounded replay, ICU/quarantine defer, shifted/exited/recovered goats,
  stock-blocked batches, missed work, proof/rework, and completed/verified
  flows in the seeded Google dev environment.
- Keep medium/low backlog out of the Google dev gate unless a local readiness
  check or dev rehearsal exposes a real correctness failure.

## Status Legend

- `closed`: fixed in code or rejected safely at publish/config time.
- `bounded`: safe for Google dev with explicit evidence required during the
  rehearsal.
- `backlog`: not a pre-Google dev blocker; revisit before staging/prod scale or
  when that workflow becomes active.

## Issue Matrix

| ID | Finding | Status | Fix before Google dev? | Notes |
| --- | --- | --- | --- | --- |
| C1 | Calendar cannot show past-due/overdue/missed obligations. | closed | No | Calendar projection/list includes old missed and past-due open exceptions outside the default window. |
| C2 | Missed-dose escalation broken for obligations missed outside projection window. | closed | No | Missed handler refreshes the targeted projection before escalation sweep. |
| H1 | Forward recurrence `yearly`/`every_n_days` was accepted but not expanded. | closed | No | Unsupported repeat policies are rejected at publish until recurrence is materialized. |
| H2 | Death/sale cancellation strands missed rows. | closed | No | Exit/cancel cleanup includes missed rows and records stock repair state. |
| H3 | ICU/quarantine health status unreachable. | bounded | No | Current behavior is fail-closed/defer-safe. Full Preventive Care (PC) health workflow is later; dev seed must prove blocked/deferred visibility and recovery behavior. |
| H4 | Deferred/held work wrongly escalated. | closed | No | Reminder/escalation sweeps exclude held/deferred/blocked work. |
| H5 | Calendar projector source gate diverged from generation. | closed | No | Publish/generation require approved source metadata; calendar reads generated obligations rather than accepting divergent source truth. |
| H6 | Calendar projection coarsened work-state. | closed | No | Calendar statuses preserve in-progress, proof-pending, verification-pending, rework, deferred, blocked, missed, and completed states. |
| H7 | Consumer dedupe not co-transactional with handler side effects. | bounded | No | Durable claims, finalization-loss detection, and retention sweeping are present. Dev rehearsal must prove each handler is deterministic/idempotent before deciding whether a durable progress table is needed. |
| H8 | `domain_event_processed_events` had no cleanup/retention sweeper. | closed | No | Dedicated processed-event retention sweeper exists and is part of the dev worker evidence. |
| H9 | Inventory batch reconciler let one bad batch stall siblings. | closed | No | Reconciler continues past a bad batch and records failed repair state for retry; prove with seeded dev stock cases. |
| H10 | Obligation sweeper isolated stock failures but not all SOP/batch failures. | bounded | No | Stock failure isolation and batch blocking are covered; Google dev must prove SOP task and batch-create failure evidence paths before calling dev done. |
| M1 | `calendarBusinessDateIn` business-date handling diverged from SQL. | closed | No | Calendar Go code now uses the shared fixed `Asia/Kolkata` business calendar, matching the SQL behavior. |
| M2 | `birth_age`/`post_arrival` due calculations used raw 24-hour / UTC date arithmetic. | closed | No | Vaccination due dates, windows, warmup floors, catch-up cycles, sweepers, and preview/reporting date helpers now normalize through the IST business calendar. |
| M3 | Calendar-trigger due embeds `asOf`, creating N-multiply risk. | backlog | No | Not a clean dev blocker while unsupported/unsafe recurrence is rejected. |
| M4 | No `trigger_type` enum validation at publish. | closed | No | Publish validates supported trigger types and rejects unknown/missing values. |
| M5 | Defer-safety config dependent with no safe default. | backlog | No | Seed and protocol config must include defer states; broader default policy hardening can follow. |
| M6 | Domain consumer could `MarkFailed` after handler success if `MarkProcessed` failed. | bounded | No | Finalization-loss detection prevents silent ownership loss; handler idempotency proof remains dev evidence. |
| M7 | `CancelOpenForGoat` stamps `time.Now` instead of event time. | backlog | No | Not a clean dev blocker; revisit for historical import/backfill accuracy. |
| M8 | Vaccination execution completion verification sub-state `as_of` issue. | backlog | No | Not blocking clean seed; verify proof/rework/completion views during dev rehearsal. |
| M9 | Operations audit uses `ILIKE`/free-form metadata heuristic. | backlog | No | Not in the clean-slate vaccination gate. |
| M10 | Escalation fires only the current highest crossed level. | backlog | No | Accept for dev-safe rehearsal; revisit if Preventive Care (PC) wants full escalation waterfall replay semantics. |
| M11 | `in_progress` not covered by missed-deadline partial index. | backlog | No | Not a current seed-scale blocker; revisit with explain-plan evidence before staging scale. |
| M12 | External notification sends at-least-once with no recipient dedup token. | backlog | No | Dev notifications run in dev-safe mode; production delivery dedup belongs before real rollout. |
| M13 | `MarkProcessed`/`MarkFailed` silently no-op when row is not processing. | closed | No | Finalization now returns explicit lost-finalization errors. |
| M14 | Time-spine sweepers are single-tenant per invocation. | backlog | No | Accept for Google dev; Cloud Scheduler/job wiring can run explicit tenant scopes. |
| M15 | `GenerateForGoat` cancel+regen is cross-transaction with no per-goat lease. | backlog | No | Clean seed scale is safe; revisit before high-concurrency backfills. |
| L1 | Booster eligibility gate uses optional type assertion. | backlog | No | Low risk; no Google dev blocker. |
| L2 | FEFO planning/reserve used `CURRENT_DATE` instead of drive/admin date. | closed | No | Reservation validates lots against the planned drive/admin date. |
| L3 | Outbox `ClaimPending` is global/cross-tenant. | backlog | No | Accept for dev; revisit for production fairness/noisy-neighbor limits. |
| L4 | No retention/pruning for published outbox rows. | backlog | No | Not a dev blocker; add to ops retention backlog before long-running staging/prod. |
| L5 | `ResolveProofRefs` only ran when client sent proof refs. | closed | No | SOP proof/verification gates now fail closed unless required proof/submission/completion fanout exists. |

## Google Dev Evidence Required

Before calling Google dev complete, capture evidence for:

- `H3`: sick/quarantine/ICU/deferred/recovered goats remain visible and do not
  create wrong active vaccination work.
- `H7` and `M6`: domain consumer replay of seeded events is idempotent and does
  not lose finalization ownership.
- `H9` and `H10`: bad stock, quarantined stock, low stock, SOP/batch failure,
  and healthy sibling batches produce visible blocked/repair state without
  hiding unrelated work.
- Shift/death/recovery scenarios: active/missed/coverage rows are cleaned or
  reopened correctly, and stock repair remains explainable.

## Earlier Goat OS Review Addendum

The earlier Codex review rated the repo as a hardened beta slice and listed ten
top fixes. The first five are now closed in code. The remaining items below are
not reasons to block the start of a clean-slate Google dev environment, but they
must stay visible as dev evidence or later backlog.

| Review item | Status | Fix before starting Google dev? | Current call |
| --- | --- | --- | --- |
| Block SOP verify/rework without valid submission, proof, and fanout. | closed | No | SOP proof/verification now fails closed. |
| Expand or reject `rule_dsl.schedule[]` at publish. | closed | No | Publish materializes executable rules or rejects unsupported input. |
| Add full eligibility age/lifecycle/business-date handling. | closed | No | Generation honors lifecycle, stage, sex, breed, health, reproductive, location, age band, and min/max age gates. |
| Make SM-4 batching windowed, due-transitioning, and per-version executable. | closed | No | Sweepers group by scope, protocol version, rule, planned date, and due window, with rule/version SOP and stock binding. |
| Fix missed projection from changed IDs/status, not default date lookback. | closed | No | Calendar refresh/list keeps old missed and past-due open exceptions visible. |
| Make consumer dedupe/handler side effects transactionally replay-safe. | bounded | No | Durable claims and finalization-loss detection are present; dev must prove handler idempotency and decide if any handler needs a durable progress table. |
| Close batch lifecycle: consume accepted, release no-shows, terminal batch state. | bounded | No | Core stock reserve/consume/reconcile paths are present; dev seed must prove accepted, missed/no-show, blocked, and repair-state evidence. |
| Unify process-state projection across Calendar, Action Center, Passport, and Execution. | backlog | No | Calendar and execution now preserve richer states, but a single shared effective-state projection remains a later simplification before production scale. |
| Harden proof/media object authorization and operator device/session binding. | backlog | No | Not part of the clean-slate vaccination dev gate; keep as pre-production security hardening. |
| Add partition automation, metrics/alerts, and CI gates for critical paths. | deployment gate | No for start, yes before dev is called complete | Local readiness gate is green; Google dev still must prove partition-maintainer, Cloud Logging/alerts, DLQ visibility, and worker evidence. |

Additional older-review notes that remain pending outside the Google-dev start
gate:

- **Single-truth read models:** avoid long-term drift by converging Calendar,
  Action Center, Protocol Adherence, and vaccination execution on one effective
  process-state contract before production scale.
- **Legacy movement parity:** accepted movement commands and historical
  movement imports are not the clean-slate dev source of truth. Future migration
  work must reconcile movement into canonical location truth through a bounded
  import/backfill job.
- **Missing/lost goat parity:** procurement import maps legacy "missing" to
  `lost`; identity APIs need an explicit parity decision before real legacy
  cutover.
- **Proof/media authorization:** Preventive Care (PC) video/proof subject binding is sufficient
  for the seed rehearsal, but object-level access, verifier routing, retention,
  and operator device/session binding need a pre-production security pass.
- **Scale/load evidence:** request-time process-integrity and vaccination
  execution aggregates are likely first scale bottlenecks; prove with staging
  load/EXPLAIN evidence, not the small Google dev seed.
- **Observability:** worker/outbox lag, DLQ backlog, Cloud Run errors, and Cloud
  SQL pressure need real Google dev alert evidence before unattended workers are
  left on.

## Related Docs

- `docs/runbooks/google-dev-clean-slate-seed-strategy.md`
- `fixtures/google-dev-clean-slate/README.md`
- `docs/runbooks/vaccination-local-business-chain.md`
- `docs/proof/calendar-vaccination-backend-proof.md`
