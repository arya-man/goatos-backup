# PR325 review/fix cycle — current receipt

## Current state
- Target: `feat/procurement-sop-driven`, PR325 only. No main merge or staging deploy.
- Final production-code SHA: `ddefb30ef` (following `653b9304e` and `a4bd5fa62`). This receipt/decision update is documentation-only; the containing commit identifies the review tip.
- All accepted findings fixed. Final pushed-head independent confirmation follows the branch push; readback is retained locally in `.codex-goatos-render/pr325-cycle/push-receipt.json`.
- Relevant Go, real PostgreSQL, Android compile/unit, web typecheck/mock-fidelity/form tests, media egress, and laptop/mobile browser gates PASS.
- `make guardrails` passed all preceding targets and stopped at the final `local-gcp-kernel-parity-guard`: Docker CLI is absent (`check-local-gcp-kernel-parity.sh:58: docker: command not found`). No guard bypass; full `make ci-local` not run. This is not a main/stg promotion receipt.

## Final real-surface evidence
On source SHA `ddefb30ef`, `responsive:guard` PASS for feed purchases (actual 29+ row fixture and pagination), toxin SOP List and Flow, laptop and mobile; adjacent Sales tolerance layout PASS at six widths. Forbidden failure strings include `backend_down`, `Admin-web contract unavailable`, `The board could not be loaded`, and `Weights could not be loaded`.

Real browser entry selected a parent/child/grandchild branch, entered a child answer, hid the parent, then saved a valid `_other` question with an explanation. Both viewport saves succeeded. Independent API readback confirmed absent hidden descendants and retained explanation; subsequent detail routes displayed Review carrier / Independent carrier laptop or mobile. Screenshots were opened and visually validated. Historical retired-question readback was independently proven before this last domain-only ID repair and after source SHA a4bd5fa62; all corresponding regression tests pass on ddefb30ef.

Evidence: `.codex-goatos-render/pr325-cycle/{responsive-r4.log,conditional-e2e.json,conditional-readback.json,conditional-detail-e2e.json,guardrails-r4.log}`; screenshot manifest `.codex-goatos-render/admin-web-screenshots/2026-09-20T21-36-31-266Z/`. All fixtures are isolated local PostgreSQL, not STG/OCI. No performance improvement or release-scale certification claimed.

## Final tests
- `go test ./internal/procurement/... ./internal/tasks/... ./internal/animalpurchase/... ./internal/toxin/... ./internal/sop/... ./internal/workforce/app ./internal/notificationbridge ./internal/adminui/app` PASS; procurement/SOP rerun after final ID fix PASS.
- Actual PostgreSQL suite repeat with GOATOS_RUN_POSTGRES_TESTS=1: vendor history/legacy replay, feed historical readback/idempotency, early source events, incremental animal decisions, branches/reanswers, repeated hooks and opener races PASS (procurement10.243s; tasks14.302s).
- Android `:app:compileDevDebugKotlin :app:testDevDebugUnitTest` focused FeedPurchaseFormAnswersTest, VendorFormAnswersTest, PushTargetResolverTest PASS. No new physical-device E2E claimed in this cycle.
- Web typecheck, full mock-fidelity, seven shared visibility/feed tests and prior focused24 tests PASS. Whole Android media-egress guard555 files PASS; full guardrail execution also passes all mobile/exception/telemetry ratchets.
- Repowise risk scanned this repair range (wide diff requires scrutiny; not an approval). CRG/Graphify unavailable here; reviews used source, SQL, current contracts and preceding-month history.

## Review history (chronological)

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

## Third review fixes and final source checkpoint
R3 found blank conditional gate values diverging across platforms. Publication now rejects them and Go/Kotlin/TypeScript defensively never activate them; baseline regression red, candidate green. Final focused Go suites, Android compile and the three focused unit classes, web typecheck and seven form tests PASS. PostgreSQL branch/reanswer, repeated hooks, early event, stale snapshot and concurrent opener regressions PASS. Supplier/toxin and workflow judges report no further actionable findings.

Broad guardrails found one new inline SQL count in repository.go. The receipt queries are now package-level named constants; scale-guard and actual PostgreSQL animal/feed receipt tests PASS. Full guardrails rerun in progress. Source checkpoint follows 653b9304e; final stamped local browser proof and branch push still pending. No merge/deploy.

## Live-entry fourth-round finding
Final-SHA laptop/mobile historical detail readback PASS at a4bd5fa62. Live authored feed submission then exposed valid question ID review_other being mistaken for a sidecar; HTTP400 reproduced. Exact authored IDs now take precedence, genuinely ambiguous sidecar/question collisions fail publication, and vendor/feed answer labels respect allow_other. Two baseline-red regressions and procurement domain/app/HTTP suites PASS. Full focused procurement/SOP repeat pending.

Responsive guard correctly required an actual paginated fixture; initial 3-row local fixture had no pager. Expanding isolated local data for pagination proof, not weakening the gate. Broader guardrails reached vaccination fixture coupling: only378 triggered, and its module-owned vendor DDL plus supplier SOP copy qualifies for the guard's existing documented opt-out. Added precise migration annotation and adversarial tests proving goat-table DDL still requires source companions. Exact fixture guard PASS; full rerun pending.
