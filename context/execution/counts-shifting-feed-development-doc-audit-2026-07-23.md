# Counts, Shifting, and Feed Development Documentation Audit

**Branch:** `feat/counts-shifting-feed`
**Audit date:** 2026-07-23
**Commit window:** 2026-07-19 through 2026-07-23 (four calendar days)
**Scope inspected:** 197 non-merge commits present in the window at audit time

## Purpose

This is the closeout map for the unusually large four-day development window on
this branch. It groups commits by delivered capability, records the canonical
documentation that already covers each capability, and closes documentation
gaps without turning individual commit messages into competing product truth.

The OpenAPI files, migrations, generated clients, and tests remain the exact
implemented contract. The documents linked below explain the product and
architecture intent.

## Capability and documentation map

| Delivered capability | Representative commits | Canonical documentation | Audit result |
| --- | --- | --- | --- |
| Feed Direction backend, admin-web, config, packing, lifecycle, source-backed ration setup, and Counts inputs | `9eb2429f`, `fc4759d0` | `docs/feed-direction/PRD.md`, `docs/feed-direction/TRD.md`, `docs/feed-direction/SHEETS-APP-SCRIPT-REPLACEMENT-PRD.md`, `docs/feed-direction/SOP-MOBILE-CUTOVER-PRD.md`, `docs/feed-direction/SOP-MOBILE-CUTOVER-TRD.md` | Covered; the PRD now also records the shipped mobile surface and session-filter contract. |
| Feed Session 1/2 filtering on direction and packing | `bea1c0f9`, `6cdbe4d7` | `docs/feed-direction/PRD.md` and OpenAPI `FeedFilterOptions`/packing worklist contract | Gap closed in this audit. Labels and available sessions are backend-owned; clients do not carry a private Session 1/2 vocabulary. |
| Counts birth/death, count breakdowns, shifting request/approval/execution, and Feed projection inputs | `9eb2429f`, `8e9a2383`, `4bb4701f` | `docs/feed-direction/COUNTS-SHIFTING-CLOSURE-PRD.md`, `docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md`, `context/source-findings/goats-and-parks-source-findings.md`, domain-event registry | Covered at architecture level; the PRD now records the shipped web-approval/mobile-execution split. |
| Temporary tag at birth, Awaiting-RFID queue, and atomic promotion to one required plus one optional permanent RFID | `b6d9ff27`, `156cecf9`, `acc5fad6`, `7a270a6d`, `0c83c824`, `d0abd396`, `e2fe05f8` | OpenAPI identity/counts contracts and `context/product/glossary.md` | Implementation was not documented as one workflow. Gap closed here and in the Counts/Shifting PRD, but a business-rule conflict remains: the glossary says every accepted animal already has `animal_identifier_1`. Maintainer confirmation is required before changing that canonical invariant. |
| Vaccination shed-level video proof and acknowledgement/verification closure | `05889b83`, `3651d741`, `eb16092d`, `8d1f97b4` | `docs/preventive-care-vaccination/TRD.md`, `docs/decisions/vaccination-shed-ack-not-form.md`, `docs/mobile/proof-capture-sync-and-e2e.md`, Goat OS build skill | Covered. Current proof grain is shed-level video plus per-goat scan timestamps; shed completion is an acknowledgement, not a manual form. |
| Vaccination operator-capacity planning and operator-grain assignments | `34bade4f` and follow-up assignment fixes | `docs/preventive-care-vaccination/operator-drive-planner-PRD.md`, `docs/decisions/vaccination-work-session-bundle.md`, vaccination E2E stories, Goat OS build skill | Covered. |
| Vaccination drive-date move/revert semantics | `d161af85`, `9da57ded`, `beaca9ca` | Goat OS build skill business rule, assistant context/coverage, raw assignment integration tests | Covered as a permanent kernel rule: moving one vaccine splits raw assignment membership; selecting the original date cancels the override and restores membership; canceled overrides are ignored by reads. |
| Vaccination Calendar/full-schedule/admin-web correctness and bounded hot reads | multiple 2026-07-20/21 fixes including `095f5663`, `c1454e47`, `2b8abf28`, `dcebc668`, `a54dc817` | Calendar ADR, scale anti-patterns, admin-web E2E checklist, vaccination execution docs | Covered. Most commits are regression fixes against existing contracts, not new product rules. |
| Android offline proof/outbox reliability, bounded roster paging, WorkManager recovery, Room migration safety, navigation, and role chrome | multiple 2026-07-20/21 commits including `4d989283`, `3edb9a8e`, `837d2119`, `804799f7` | `docs/decisions/mobile-data-fetch-anti-patterns.md`, `docs/mobile/proof-capture-sync-and-e2e.md`, `docs/decisions/android-navigation-stack.md`, mobile anti-pattern skill | Covered. |
| Leadership assistant: Cube-first runtime, safety, persistence, streaming, UI, evaluations, operator vaccination context, and coverage guard | `c722249b`, `357e4b2d`, `13afb053` and documentation commits around them | `docs/ceo-ai/ceo-chatbot-purpose-and-build-plan.md`, `docs/ceo-ai/mcp-toolbox-plan.md`, `docs/ceo-ai/coverage-matrix.md`, `context/agents/ceo-bot-analytics-context.md`, `analytics/cube/METRICS.md` | Covered, including known coverage gaps. |
| Auth/RBAC founder grant claim, CEO-role vocabulary, backend-owned navigation/copy | multiple 2026-07-20/21/22 fixes | `context/architecture/org-role-model.md`, role/nav composition ADR, Goat OS build skill | Covered. |
| Local exact-main stack, authenticated admin-web startup, shared media secret, staging Android signing/Firebase push | multiple 2026-07-20/21/22 fixes | `docs/runbooks/local-full-stack-rehearsal.md`, `docs/runbooks/android-dev-device.md`, staging signing docs, Cloud Deploy runbook, Goat OS build skill | Covered. |
| Review-lens ledger, closed-decisions registry, guardrail/CI hardening, migration and scale fixes | multiple 2026-07-20 commits | consolidated defect ledger/program, scale and migration ADRs/skills, guardrail manifest | Covered. |

## Newly documented operator workflows

### Temporary tag to permanent RFID

1. Birth capture requires exactly one initial identity mode: a permanent
   `animal_identifier_1` or a provisional `temporary_tag`.
2. Temporary-tagged animals appear in the Counts **Awaiting RFID** work list.
   The API is keyset-paged and capped at approximately 20 rows per page.
3. Operators may filter that work list by park and shed. Filtering is performed
   by the backend query and swaps the bounded Room/Paging cache on mobile.
4. Promotion is an idempotent offline-outbox command. In one database
   transaction it retires the active temporary tag, attaches the required
   permanent RFID, optionally attaches a distinct second RFID, and emits the
   identifier-retired/identifier-added domain events.
5. A replay returns the existing promotion result; a goat without an active
   temporary tag is rejected. There is no intermediate state with no active
   identity or two active primary identifiers.

This describes implemented behavior only. It does not resolve the conflicting
glossary invariant that every accepted animal must already have
`animal_identifier_1`.

### Shifting approval to physical completion

1. A manager reviews and authorizes a shifting request in admin-web.
   Authorization records intent only and does not relocate animals.
2. The operator opens **Shifting -> Pending** on Android and may narrow the
   bounded, Room-backed queue by park and source shed.
3. The operator performs the physical movement and marks the shifting complete.
   The offline outbox uses a stable idempotency key so retries cannot relocate
   an animal twice. Optional video is evidence but does not gate completion.
4. Only verified completion executes the canonical atomic relocation. It
   updates shed and destination-profile-derived management stage and emits
   `goat.location.changed` plus `goat.stage_changed` when applicable.
5. Vaccination consumes the movement result through its registered rescope and
   clinical re-evaluation path.

### Feed session filters

Feed Direction and Feed Packing now expose an **All sessions / Session**
filter. The available session numbers, labels, and ordering come from the
active backend session-template vocabulary returned in `FeedFilterOptions`.
The same selected session narrows rows and summary totals. Packing threads the
session through its worklist query; `0`/unset means all sessions.

## Documentation conflicts and follow-up

One conflict requires a maintainer decision:

| Existing canonical rule | Implemented branch behavior |
| --- | --- |
| `context/product/glossary.md`: every accepted/canonical animal must have `animal_identifier_1`. | Birth capture accepts a provisional `temporary_tag` without an active `animal_identifier_1`, then promotes it through the Awaiting-RFID workflow. |

Confirm whether the glossary rule is retired for newborn field intake, or
whether temporary-tagged newborns are explicitly provisional/non-canonical
until promotion. After confirmation, update the glossary and any import/seed
validation that uses the old invariant in the same change.

## Audit boundary

This audit documents the development visible in this branch and commit window.
It does not certify every commit as production-ready, rerun the full CI suite,
or change the status of open product/readiness gates. Untracked local source
artifacts and fixture files present during the audit were not modified.
