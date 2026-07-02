# Feed Direction Next-Session Plan

Status: corrected handoff after source review
Date: 2026-06-29

## Read first

- `docs/feed-direction/PRD.md`
- `docs/feed-direction/TRD.md`
- `docs/protocol-engine/state-machines.md` SM-6
- `context/source-findings/feed-direction-counting-db-reconstruction.md`
- `context/frontend/current-admin-web-scope.md`
- Legacy reference only, workspace-relative outside the GoatOS repo:
  `/Users/ravi/mesha/slack-automation-scripts/feed_automation.js` and
  `/Users/ravi/mesha/slack-automation-scripts/counting_db_automation.js`
- Source document, workspace-relative outside the GoatOS repo:
  `/Users/ravi/mesha/wiki/graphify-out/converted/Feed, Shiftings and Count_27cef16d.md`

## P0 gates

1. Scope reopen: Feed Direction operational UI is still blocked until the owner
   explicitly reopens it after Preventive Care (PC) / Vaccination and generic Config review.
2. Security: legacy Slack/App Script contains committed token/shared-secret
   material. Do not copy it. Rotation ownership must be assigned before any
   Slack bridge is reused.
3. Design ratification: default to the committed `000079` generic-kernel design.
   Do not build the old typed `feed_*` stack unless the owner explicitly reverses
   this decision.
4. Timing sign-off: recommended source model is Day N 09:00 full direction,
   13:30 cutoff, 13:30-13:45 Diff, 15:00 staging, Day N+1 09:00/15:00 serving.
   Keep times configurable and confirm with Feed Director.

## Corrected target

Build the Feed Direction generation and wiring slice around the existing kernel:

```text
published feed_direction protocol rule_dsl
  -> base-count + shifting ledger replay
  -> tomorrow projected count
  -> full direction generation run
  -> shed/session/feed obligations
  -> cutoff Diff generation for affected sheds
  -> explicit cancel/supersede of stale open obligations
  -> packing task starts stock reserve through ReserveForBatch
  -> accepted proof consumes/releases stock through ConsumeForBatch/release
  -> consumption/transport/wastage proof
  -> verification, escalation, projection
```

The true greenfield work is not "reconcile the feed migration." It is generation
and production wiring: scheduler/consumer/job, obligation creation, explicit
Diff cancel/rebuild, reserve/consume integration, API routes, OpenAPI clients,
and projections/read APIs.

## P1 implementation order

1. Refresh repo state:
   - Read `AGENTS.md`, `SKILLS.md`, and the files listed above.
   - Run `git status --short` and inspect the current migration tail before any
     edit.
   - Query CRG/Graphify first, then grep only for exact route/config strings.

2. Backend design patch, if not already done:
   - Verify PRD/TRD still match `000079` and current migrations.
   - Choose next migration number after live tail.
   - Decide the minimal persistence: generation runs, generation snapshot rows,
     bridge events, and/or projection rows.

3. Generation service:
   - Load published `feed_direction` protocol version and rule DSL.
   - Normalize ration keys: adult `breed + shed_tag/stage`; kids
     `weight_band + target_ADG`.
   - Replay base-count + shifting events to tomorrow projected count.
   - Produce full run rows and create obligations at shed/session/feed grain.
   - Add idempotency and source hashes.

<a id="next-session-feed-diff-cancel-helper"></a>

4. Diff and bridge:
   - Generate canonical affected-shed restatement rows for pre-cutoff shiftings;
     source-facing output may still present the net correction operators expect.
   - Explicitly cancel/supersede stale open obligations for affected targets
     before creating replacements; model the feed helper after
     `CancelOpenVaccinationObligationsForGoatExceptVersions`.
   - Do not rely on idempotent insert/no-op alone: a replay key for the new
     direction/run/version does not cancel old open work and can leak stale
     instructions into stock reservation.
   - Apply shifting events idempotently by event id/logical event key, and fail
     closed when cohort/stage impact is unresolved or free-text-only.
   - Log high-priority post-cutoff 2x bridge events with proof links.
   - Do not implement the superseded 07:30 next-morning Diff.

<a id="next-session-feed-inventory-app-anchors"></a>

5. Execution and stock:
   - Wire packing start to generic inventory `ReserveForBatch`.
   - Wire accepted packing proof to `ConsumeForBatch` for actual quantity and
     release the remainder through the inventory movement path.
   - Keep stock-out as blocked/escalation state, not a bare decrement.
   - Keep missed/overdue computed by the sweeper/deadline kernel.
   - Use reviewed transport consolidation config where direction sheds map to
     grouped transport sheds.
   - Carry source-backed packing discrepancy and wastage variance thresholds
     into exception/rework state.

6. API and clients:
   - Add HTTP routes for generation status, direction lists, execution/history,
     and verification.
   - Update OpenAPI and generated clients.
   - Use backend-owned labels/options/filters/disabled reasons.
   - Use cursor pagination for hot read APIs; no unbounded lists.

7. UI only after scope reopen:
   - Update the mock before building active Feed screens, because current feed
     panels are static and stale on timing/nav labels.
   - Keep Feed under the Feed vertical; park/date stay in the top bar.
   - Do not claim current mock has Feed pagination/filters/row drawers. Add those
     only after mock and contract include them.
   - Add Feed paths to `check-mock-fidelity` scan coverage when UI becomes
     active.

## Legacy parity rules to port

- Feed Direction Sheet field shape and processed flags become idempotent
  generation/task/proof state.
- Count-DB and Projected-DB are source evidence and fixtures, not runtime truth.
- Session split is 50/50 by the June source default, but legacy per-farm
  session/feed-set templates must be imported as config or explicitly superseded
  by Feed Director sign-off.
- Rounding grain, K0/K1 exclusions and fail-closed cohort impact,
  F2/Fattening normalization, transport consolidation, discrepancy/wastage
  thresholds, and processed-flag behavior are source-blocked until captured as
  explicit, tested transform/config rules.
- Slack is notification/cutover bridge only and must go through GoatOS APIs.

## Verification gates

- Unit tests for ration key normalization, timing decisions, Diff, and bridge.
- Integration tests for generation idempotency and stale-obligation cancel.
- Import/replay tests for legacy Feed rows, stage proof rows, applied shifting
  events, and duplicate/cross-source overlap cases.
- Integration tests for reserve/consume/release through generic inventory.
- API route tests and OpenAPI generated-client checks.
- SQL plan validation for generation/list queries.
- Local proof through Postgres + API + job/consumer/sweeper + SOP submission +
  verification + read model before visual approval.
- Playwright/mobile screenshots only after UI scope reopens and the mock is
  updated or explicitly superseded for stale labels.
