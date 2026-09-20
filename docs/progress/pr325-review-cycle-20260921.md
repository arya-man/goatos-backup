# PR 325 iterative review and repair — 2026-09-21

## Scope
PR #325 only, branch feat/procurement-sop-driven; no main promotion or STG deployment. Isolated checkout /Users/raviteja/mesha/pr325-review, starting SHA 1ac8644609536d3867065f54a44901e5aac62405.

## Done
- Reviewed last-month relevant history and current SOP, outbox, and mobile sync paths.
- Confirmed three defects: supplier form provenance, feed purchase answer readback, incremental animal decision completion.
- Assigned independent implementation agents to each defect. Root owns integration, live stack, gates, progress, and delivery.

## Pending
- Implement and test all three fixes.
- Validate affected real web routes on laptop/mobile and compile/test Android changes.
- Run independent cross-review agents on integrated changes; fix and repeat until no actionable findings remain.
- Push tested fixes only to PR branch and verify remote SHA.

## Tests and evidence
Baseline focused Go procurement/tasks/toxin/SOP/workforce/adminui tests PASS (opt-in DB tests skipped). Baseline 18 web tests PASS. Whole Android media-egress guard PASS (555 files). New cycle tests not yet run.

## Known failures
Baseline responsive guard could not import @axe-core/playwright in the fresh checkout. Installing checkout-local dependencies. Existing isolated :3315/:18095 stack is stale at e2442c9c; not accepted as current-head proof.

## Before/after metrics
Behavioral before states listed above; after evidence pending. No performance improvement claimed; affected API timing checks pending.

## Judge status
Implementation agents active; independent review rounds pending.

## Current SHA and deployment
Starting SHA 1ac8644609536d3867065f54a44901e5aac62405. No commit or push yet in this cycle. Not merged or deployed.

## First integration and counter-review
- Initial three implementations complete. Agent red/green tests reproduce historical supplier lookup, missing feed wire fields, and decision incremental/out-of-order delivery on baseline and candidate. Actual PostgreSQL roundtrips pass (root repeat pending).
- Root live baseline API proof: published feed form v2 with required lorry_number, created purchase with TN42 ABC, then published v3 retiring that question. Baseline GET feed-purchases omits the stored answer and original label. Fixture retained in isolated goatos_pr325_cycle for after-proof.
- Integrated focused Go suites PASS. Typecheck exposed an optional options array indexed without NonNullable; fixed, rerun pending.
- Judge R1 additional fixes accepted: preserve pre-upgrade vendor update idempotency hashes; retain feed arrival/toxin hook facts that arrive before workflow opener. New animal candidate event registration/relay validation also required.
- Judge state: counter-reviews active, fixes in progress. No cycle push yet.

## Second counter-review and integrated proof
- Root independently reran baseline historical supplier and feed wire regressions: both FAIL as expected. Root actual PostgreSQL candidate tests PASS for supplier provenance, pre-upgrade vendor replay, feed answer roundtrip, animal incremental sync, and feed event-before-opener ordering.
- Android compile + FeedPurchaseFormAnswersTest, VendorFormAnswersTest, PushTargetResolverTest PASS on first integrated edits (conditional follow-up rerun pending).
- Typecheck and full mock-fidelity PASS after fixing optional-array type and missing hint.other production copy.
- Root fallback test red then green: missing historical form must preserve Other explanation and deterministic ordering for vendor/feed. Shared helper fixed both.
- Real HTTP baseline: original historical label absent. Updated API: label Original lorry label and answer TN42 ABC returned after v3 retired the v2 question. Three purchase rows, response 2016 bytes, ten warm requests 2.02–7.91ms (small local fixture only; no scale/performance claim).
- Real browser detail readback PASS at laptop 1440x1100 and mobile 390x844; screenshots opened and visually validated. Exact forbidden failure strings absent. Evidence .codex-goatos-render/pr325-cycle/detail-e2e.json and laptop/mobile-feed-historical-detail.png. Final-commit replay pending.
- Responsive gate initially refused unstamped API build; final commit will be rebuilt/stamped and gate rerun. Guardrails initially stopped at generated-client uncommitted diff; generated output staged and rerun in progress.
- R2 fixes: engine facts must apply to all matching hook steps and respect/reconcile answer branches; transitive conditional-question visibility must agree on Android, backend and web. These follow-ups remain in progress.
- No PR push yet; no main/stg promotion.

## Repair checkpoint
All accepted R1/R2 fixes implemented: supplier SOP provenance + legacy replay, feed answer readback + request identity, monotonic animal decision reconciliation, durable feed hook receipts, multiple/conditional hook actions, toxin source failure propagation, Market installed-client routing, transitive form visibility, historical Other fallback, and missing UI copy/type repairs. Existing normal Go/Node/Android suites contain regressions; strongest controls are transaction receipts/version fencing and shared visibility projection rather than literal source guards.

Fresh integrated Go suites PASS (procurement/tasks/animalpurchase/toxin/SOP/workforce/notificationbridge/adminui). Web typecheck and six visibility/feed tests PASS. Android compile and FeedPurchaseFormAnswersTest, VendorFormAnswersTest, PushTargetResolverTest PASS after conditional edits. Final PostgreSQL hook/branch/opener race repeat and independent R3 reviews in progress.

No latency improvement claimed. No STG/OCI/shared-local data changed. Local browser fixture only. Final stamped-commit responsive run and PR push pending.
