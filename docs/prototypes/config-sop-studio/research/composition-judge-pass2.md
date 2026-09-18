# Integrated composition judge — pass 2

2026-09-16. Independent requirements/code retest after first-pass fixes. **First-pass defects addressed in the local model; independent cross-review of the final adjacent fix remains pending.** This reviewer initially remained read-only, then the parent explicitly assigned the prerequisite-revocation fix described below. This is not self-certification. Current backend/DB business inventory is separately recorded in hardcoded-backend-db-audit.md; it does not certify the prototype implements every proposed configurable setting.

## First-pass finding disposition

| Finding | Independent verification | Disposition |
|---|---|---|
| C01 nested staged child bypass | Latest actual-stack judge-sop-composition executes a child master, keeps parent paused, rejects completion before its1-day wait, then resumes parent after child stages finish. startSim now routes a staged root through stage practice. | Original defect closed in inspected model/runner. |
| C02 changed answers retained approval | childFinished clears approval; compositionRunChild also clears old result and approval before rerun. Independent tests pass. | Original defect closed, adjacent dependent-stage defect below remains. |
| C03 static animal groups | procurement-practice.js now has100 synthetic stable candidate IDs, distinct recommendations/decisions, selected-only boarding, tag guard, boarded-only arrival and exception history. No real records copied. | Materially implemented local demonstration; full clinical/inspection attempts per candidate and production identity bridge remain out of scope, not certified. |
| C04 unused duplicated duration items | Example stages now hold waitDaysRef/repeatCheckHoursRef; compiler resolves department access, active status, unit and stable ID/revision into pinned values. Changed shared15→16 affects next compile, previous stays15; archive/revoke/wrong unit rejected. | Closed for local shared period binding. |
| C05 feed/proof/clinical prose only | Multiple feed groups accept explicit kg/animal/day and candidate references, reject nonboarded or duplicate same-feed assignments, calculate separate requirements and prevent surplus in one feed covering another. Stage completion guards require tagging/truck/shed file metadata, plan acknowledgement, received cohort and transition text. | Improved bounded local demonstration. Clinical execution, genuine uploads, diet resolver and executable published vaccination plan remain explicitly external. |

## Remaining defect: changed upstream approval does not invalidate downstream execution

`sop-composition.js` StagePlanModel.finish checks child completion, own approval/time/proof, but not current prerequisite reason. Generic rerun invalidates the upstream stage's approval without clearing its dependents. Repro: A requires Director approval; B depends on A approved. Start/finish childA, approveA, startB; rerun childA (approval clears); finish childB and B succeeds. Independent actual-stack probe prints `A.approved=false; B.status=completed`. This is not a new product scope: it is the adjacent integrity case of C02/S03. Fix by blocking/reopening affected dependents or preventing upstream reopening while downstream work is active; preserve immutable completed-run history explicitly. Repro `/tmp/composition-pass2-adversarial.cjs`; sent to implementation owner.

## Other acceptance boundaries

Sales-owned settings now expose adult600/500 rates and40/60 assumed weights, K0/K1/K2/K3 rates500 and3/3/8/15 weights, plus fattening450 and30/35 reporting settings. Farm value reads the additional values in its assumptions table, while the sample total still values only fattening sample animals. This is honest when described as a local proposal/table, not proof all real production report consumers were migrated. Additional430 load-wise rate and other business rules remain audit findings rather than silently implemented coverage.

Health remains the existing mock's domain-specific surface and source-linked clinical research; no new100–200-disease catalogue or arbitrary diagnosis engine is claimed. Vaccination reference is publishedv9 metadata and acknowledgement, not a full clinical plan runner. The fresh DB discrepancy (v9drive limit2 versus capacity3; plan-shed proof versus SOPper-goat proof) must remain unresolved until resolver investigation. No new veterinary rule was invented.

The master/stage model supports started/completed/approved and clock/file-metadata practice. Rejection/rework, durable dispatch, real clinician/reviewer grants, offline sync, periodic notifications and actual animal movement remain production integration work. A bounded prototype pass must preserve these limitations. Every older feature in the joined inventories remains an existing domain—not a promise it was reimplemented by this mock.

## Verification receipt

Independently ran all25 packaged judge*.cjs after inspected edits:24 pass, obsolete judge-orchestration fails because the rejected standalone event product is absent. Evidence composition-pass2-checks.txt. Independently ran the expanded composition and procurement suites and the adjacent adversarial probe. Existing tests verify missing/cyclic child references, pinned definitions, elapsed wait, periodic checks, exact role, typed setting accessibility, cohort subset/history and multifeed constraints.

This reviewer did not gather fresh browser screenshots; visual/browser evidence must be separately attributed to the visual reviewer/parent. No production API/mobile failure-string E2E or deployed schema compatibility certification is claimed. No DB writes, push, merge or deployment occurred in this review.

## Final adjacent fix and handoff

At the parent’s explicit assignment, this reviewer added StagePlanModel.invalidateDependents and beginChild. Reopening an existing child result now resets all transitive dependent stages—including previously completed ones—to waiting, clears their approvals/results/checks, and preserves unrelated parallel stages. Direct replacement through childFinished applies the same invalidation. The upstream stage retains its original start time but requires the child and approval again. Regression in judge-sop-composition.cjs covers an approved A→completed B→completed C chain, an independent completed stage, starting B after revocation, and replacing A again while B is started. Focused test passes.

Final `sh run-checks.sh` runs the24 active judges and succeeds; receipt composition-pass2-final-checks.txt. The script explicitly excludes the retired standalone orchestration test by name; this is an intentional product-scope retirement, not a hidden failure. Direct `./run-checks.sh` initially failed because executable permission is absent; invoking `sh` is the verified command.

The original independent findings/reproductions remain above for traceability. Since this reviewer authored the last fix, a separate PR reviewer must cross-check it before final independent approval. Browser/visual and production-integration limits remain unchanged.
