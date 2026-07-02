# Preventive Care (PC) Vaccination Roster Expansion Follow-up

Date: 2026-06-26

## Current baseline

Local/dev is not blocked on a vague Preventive Care (PC) approval item. The selected baseline is
recorded in `context/source-findings/preventive-care-vaccination-roster-stage-proposal.md`:

- K2 is 42 days / six weeks for local/dev.
- The schedule-bearing local/dev protocol is ET / Enterotoxaemia, K1, day 21,
  0.5 ml, 7 day due window, +14 day booster clue.
- PPR, FMD, HS, and BQ are valid SOP/roster labels from source artifacts. For
  this follow-up, each vaccine must end in one of three closed states:
  `schedule-backed` (a real sourced protocol row exists),
  `draft/reviewed publish-rejected` (timing/dose evidence exists but source
  approval metadata is not publishable), or `label-only closed` (sources name
  the vaccine but do not provide schedule math).

This follow-up is optional production roster expansion beyond the ET baseline.
It is mostly backend/config/data work. It is not a new UI/UX feature unless the
existing screens break or become unclear with multiple real protocols.

## Build goal

Classify each of `PPR`, `FMD`, `HS`, `BQ` and add source-backed
schedule-bearing vaccination protocol rows only for the vaccines with sufficient
schedule evidence.

For this follow-up, `schedule-backed` requires these mandatory fields:

- goat applicability: target species, plus any stage/age/lifecycle/sex/breed
  eligibility constraints the source gives
- canonical vaccine name and aliases
- trigger type: `birth_age`, `post_arrival`, `calendar`,
  `after_previous_completion`, or `manual_campaign`
- offset day/date and due window
- dose amount/unit, or an explicit source-backed reason dose is not required for
  the chosen execution path
- booster/repeat/catch-up/missed-dose policy, or an explicit source-backed
  statement that there is no booster/repeat path
- proof requirements and executable SOP binding
- publishable source metadata: `source_system` in `vaccinations_db`, `phc`, or
  `vet`; `source_ref`; `review_status=approved`; `approved_by`; `approved_at`

Other useful source fields to capture when present:

- route/site, withdrawal days if applicable
- inventory item and lot requirements if execution proof consumes stock

If a source only names a vaccine but does not give timing/dose/booster details,
record `label-only closed` for that vaccine and stop. That is a completed
outcome for this pass, not a pending blocker. Do not invent a protocol row.

If a source gives timing/dose values but does not carry publishable approval
metadata, record `draft/reviewed publish-rejected`; create at most a
draft/reviewed candidate row and verify the backend publish gate rejects it. Do
not publish it and do not generate obligations from it until `rule_dsl.source`
satisfies the backend gate.

## Closure rule

This follow-up is done when all four labels have an explicit state:

| Vaccine | Closed state |
| --- | --- |
| PPR | `schedule-backed`, `draft/reviewed publish-rejected`, or `label-only closed` |
| FMD | `schedule-backed`, `draft/reviewed publish-rejected`, or `label-only closed` |
| HS | `schedule-backed`, `draft/reviewed publish-rejected`, or `label-only closed` |
| BQ | `schedule-backed`, `draft/reviewed publish-rejected`, or `label-only closed` |

Only `schedule-backed` entries generate obligations. `draft/reviewed
publish-rejected` entries may exist as non-published candidates with captured
publish-gate evidence, and `label-only closed` entries remain available as
SOP/vocabulary labels; neither creates due work.

## 2026-06-26 source audit closure

This pass queried CRG, Mesha docs Graphify, Mesha visual Graphify, and the
goatos-docs Graphify graph before raw source reads. The source search found
general Preventive Care (PC) vaccination obligations, cold-chain, software logging, and the Goat
OS vaccination schema, but no PPR/FMD/HS/BQ goat schedule row with timing, dose,
booster/repeat policy, proof/SOP binding, and publishable approval metadata.

| Vaccine | Closed state | Source evidence | Engine action |
| --- | --- | --- | --- |
| PPR | `label-only closed` | SOP picker label in `source-material/sop-playground-local/playground.html:1057-1083`; sheep/procurement-history note in `wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md:86`. No goat schedule math or approved Preventive Care (PC) / vet metadata found. | No `inventory_items`, `vaccines`, `protocol_definitions`, `protocol_versions`, or `protocol_rules` row added. No obligations generated. |
| FMD | `label-only closed` | SOP picker label in `source-material/sop-playground-local/playground.html:1057-1083`; sheep/procurement-history note in `wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md:78`. No goat schedule math or approved Preventive Care (PC) / vet metadata found. | No `inventory_items`, `vaccines`, `protocol_definitions`, `protocol_versions`, or `protocol_rules` row added. No obligations generated. |
| HS | `label-only closed` | SOP picker label in `source-material/sop-playground-local/playground.html:1057-1083`. No timing/dose/booster/repeat or approved Preventive Care (PC) / vet metadata found. | No `inventory_items`, `vaccines`, `protocol_definitions`, `protocol_versions`, or `protocol_rules` row added. No obligations generated. |
| BQ | `label-only closed` | SOP picker label in `source-material/sop-playground-local/playground.html:1057-1083`. No timing/dose/booster/repeat or approved Preventive Care (PC) / vet metadata found. | No `inventory_items`, `vaccines`, `protocol_definitions`, `protocol_versions`, or `protocol_rules` row added. No obligations generated. |

No `draft/reviewed publish-rejected` candidates were created because the sources
did not provide timing/dose evidence for these four vaccines. The existing
source-derived ET/K1/day-21 local/dev protocol remains the only schedule-backed
vaccination row from this audit.

## Existing system to reuse

Do not build a new feature surface first. Reuse the existing engine:

- `protocol_definitions`, `protocol_versions`, `protocol_rules`
- version-level `rule_dsl.source` publish gate
- version-level executable `sop_version_id`
- optional rule-level `protocol_rules.sop_version_id`
- obligation generation (`birth_age`, `post_arrival`, `calendar`)
- booster generation (`after_previous_completion`)
- sweeper batch/SOP task creation
- proof upload, SOP submission fanout, verification accept/reject/rework
- inventory item/vaccine/stock/FEFO lot tables
- existing admin-web Config list/read/publish path

Minimum implementation should be a source-backed seed/import/config path plus
verification. Do not create new runtime tables unless the current protocol model
cannot represent a sourced schedule.

## Backend/config work

For each source-backed vaccine schedule:

1. Normalize the vaccine name and aliases.
2. Create or update `inventory_items` with `category='vaccine'`.
3. Create or update `vaccines` metadata.
4. Create or update one `protocol_definitions` row per vaccine or protocol
   family.
5. Create a new `protocol_versions` row or a new version of an existing
   protocol.
6. Put source/review metadata in `protocol_versions.rule_dsl.source`.
7. Put multi-dose schedule rows in `rule_dsl.schedule[]`.
8. Expand schedule rows into `protocol_rules`.
9. Bind a real published vaccination SOP version at
   `protocol_versions.sop_version_id`; use rule-level SOP override only when a
   source requires a dose-specific executable SOP.
10. Preserve idempotency: rerunning the seed/import must update the same
    deterministic rows, not duplicate them.
11. Keep tenant and scope explicit. Use tenant scope only when the source is
    genuinely tenant-wide; otherwise use park/shed scope.
12. Seed/import helper commands must pass the shared local/dev target guard
    (`localtarget.ValidateLocalDatabaseTarget`) before opening a DB pool.
13. Never mutate Cloud SQL or prod/stg from this follow-up session unless the
    user explicitly asks and the Google org/project/account are verified first.

## UI/UX expectation

No new UI/UX is required for the first pass.

Existing surfaces should consume the new rows:

- `/config?category=vaccination` lists protocol versions and source-review state.
- `/vaccination` and operations views read generated obligations/protocols.
- Action Center reads due work.
- Workflows reads batch/task chain state.
- Goat Passport reads obligation/completion history.
- SOP Library supplies the executable vaccination SOP.

Do only a small admin-web QA pass after backend/config work:

- multiple protocols render without hardcoded labels
- existing display/sort helpers must not add new hardcoded PPR/FMD/HS/BQ
  vaccine-order lists; prefer backend protocol names and data-driven ordering
- long vaccine names do not overflow tables/cards
- status chips still reflect source-backed/draft/published state
- matrix/table columns come from backend data, not UI constants
- empty, failed-read, and no-published-SOP states remain honest
- existing filters/search do not hide rows accidentally

If the UI looks cramped with many vaccines, that is a polish follow-up. Do not
block backend roster expansion on a redesign.

## Edge cases to handle

Source quality:

- vaccine name exists but no schedule values: `label-only closed`, no obligation
  rule, no pending item
- schedule has timing but no dose: allow generation only if dose is not required
  for the chosen execution path; otherwise keep draft/not source-backed
- schedule has timing/dose but no approved source metadata:
  `draft/reviewed publish-rejected`; verify publish rejection and do not
  generate obligations
- source says "annual" without start age: require explicit start trigger or
  keep draft
- source conflicts with ET baseline or another source: create a new version only
  after choosing one source of truth; do not overwrite silently
- sheep/procurement history appears for goat protocol: do not apply unless a goat
  source confirms applicability

Scheduling:

- same goat has multiple vaccines due same day: separate obligations by
  `rule_id`
- one vaccine has primary plus booster: booster must use
  `after_previous_completion` and actual accepted administration date
- missed primary: use explicit `catch_up`/missed-dose policy, not silent skip
- annual/repeating schedules: use repeat policy and next-due basis from last
  accepted completion where applicable
- manual campaign: do not generate automatically unless campaign scope/date is
  explicit
- missing DOB for `birth_age`: skip/defer honestly; do not guess
- missing entry date for `post_arrival`: skip/defer honestly; do not guess
- goat exited/dead/sold: no open due obligation should remain active

Eligibility and animal state:

- use `animal_stage_lookup`/goat stage data, not frontend literals
- K2 default remains 42 for local/dev unless a later source-backed config version
  overrides it
- sick/quarantine/ICU goats should be deferred/excluded per source and current
  generation rules
- pregnant/lactating or reproductive exclusions must be represented only if the
  source says so
- per-park or per-shed variants should become scoped protocol versions, not
  `if park == ...` code

Inventory and execution:

- no stock or expired stock: obligation may exist, but execution should surface
  stock blocker instead of consuming fake stock
- FEFO lot selection must remain stock-ledger based
- lot consumption/release must be idempotent
- dose unit must match inventory unit or be convertible by explicit config
- cold-chain proof requirement must come from source/SOP policy, not hardcoded
- adverse reaction fields remain SOP/execution data, not protocol identity

SOP/proof:

- executable SOP must be a real published SOP version UUID
- per-dose `sop_label` is display only
- proof policy must contain real proof tokens under a recognized array key
- failed SOP list/read must block Save/Publish, not masquerade as an empty SOP
  library
- failed stage list/read must block Save/Publish, not fallback to hardcoded K1

Versioning and publish:

- never edit a published protocol in place for a semantic change
- use a new version for changed schedule, dose, eligibility, SOP, proof, or
  source metadata
- overlapping effective windows must obey the existing DB constraints
- retired versions should not generate new obligations
- drafts without source backing must remain not publishable
- source metadata must survive list/read/publish paths

Idempotency and scale:

- seed/import reruns must not duplicate definitions, versions, rules, inventory
  items, or stock
- generation replay must not duplicate obligations
- sweeper replay must not duplicate batches/tasks
- proof/SOP replay must not duplicate completions
- queries must stay tenant-scoped, indexed, paginated/bounded, and safe for large
  herds

UI/data integrity:

- do not hardcode PPR/FMD/HS/BQ columns in React
- do not fake rows to match mock density
- backend empty state and backend read failure must stay distinct
- local dirty DBs may contain older test protocols; verification should scope to
  the protocol/rule being tested

## Verification checklist

Minimum checks for a roster expansion session:

- `go test ./internal/protocol/... ./internal/vaccination/... ./internal/obligation/... ./internal/inventory/...`
- seed/import command reruns twice without duplicates
- `GET /protocols?category=vaccination` shows the new source-backed version(s)
- generation creates obligations only for eligible goats
- replay keeps obligation count stable
- sweeper creates one expected batch/task per scope
- SOP/proof submission completes one obligation and verification accepts it
- Passport, Action Center, Workflows, Protocol Adherence, Operations, shed
  drilldown, and Control Tower read the same truth
- admin-web typecheck/build if frontend contracts or generated clients change
- `git diff --check`
- roster expansion doc/table updated with `schedule-backed`,
  `draft/reviewed publish-rejected`, or `label-only closed` for PPR, FMD, HS,
  and BQ
- any `draft/reviewed publish-rejected` vaccine includes the missing publish
  metadata and the publish-gate rejection evidence

## Non-goals

- no new Config screen unless the existing one cannot represent a sourced row
- no new vaccine tables unless `protocol_rules` cannot represent a real source
- no browser E2E unless explicitly requested for that session
- no Cloud SQL/prod/stg mutation without verified Mesha/VGoats account, org,
  project, and target repo
- no production PPR/FMD/HS/BQ schedules from labels alone
- no dangling external-source state after the pass

## Small next-session prompt

```text
In /Users/ravi/mesha/goatos, close the optional Preventive Care (PC) vaccination roster expansion
for PPR/FMD/HS/BQ. Follow AGENTS.md graph-first lookup before raw reads: CRG,
Mesha docs/visual Graphify, and goatos-docs Graphify. Then read AGENTS.md,
SKILLS.md, context/README.md, docs/phases/README.md,
context/source-findings/preventive-care-vaccination-roster-stage-proposal.md, and
context/execution/vaccination-roster-expansion-followup.md. Use Graphify/wiki/
SOP/legacy sources first; cite exact source paths. For each vaccine, end in one
closed state: schedule-backed only if source evidence gives real timing/dose/
booster values and publishable approval metadata, draft/reviewed publish-rejected
if timing/dose exists without approval metadata, or label-only closed if sources
only name the vaccine. For publish-rejected candidates, verify rejection and do
not generate obligations. Reuse the existing protocol engine, publish gate, SOP
binding, generation, sweeper, inventory, proof, and verification path. Do not
hardcode UI vaccine columns or invent schedules from labels. Add idempotent
source-backed seed/import/config rows only for schedule-backed vaccines, verify
generation/replay/sweeper/proof/read-models, and update docs with the final
state for all four vaccines.
```
