# Final Sales ownership, shell and provenance review

2026-09-16. Independent read-only review of sales-owned-settings.js, consumer-surfaces.js, production-shell.js, current integrated stack and V12 park/pen requirement. **Changes requested** for grant/readback and usage completeness; no whole-product approval.

## Findings

1. **P2 — revoked Sales applicability still displays the added value.** `sales-owned-settings.js:13` uses configValueByKey directly for table value and name, while consumerConfigLink checks effective module access. Actual-stack probe removes Sales from adult-female-rate shares, renders Sales Config and sees both600 and “Not linked to this module.” Gate the value with the same effective-access rule. In Farm value, optional assumption rows currently stringify missing consumerNumber as `null kg`/`₹null` (`consumer-surfaces.js:31`); render explicit unavailable state rather than a malformed numeric value.
2. **P2 — new bound settings have no consumer impact record.** Added adult/kid rates and weights are read by the Farm value assumptions table and Sales Config, but `consumerBoundPages` (`consumer-surfaces.js:67`) contains only earlier keys. Actual-stack itemUsages(adult-female-rate) returns an empty list. Add actual page consumers so archive/unlink impact review reflects real bindings. Do not claim they affect the fattening-only total: currently they are displayed assumptions, not applied to invented adult/kid sample headcounts.
3. **P2 — personal login remains in older anonymous research artifacts.** Email-pattern scan found one real organization email each in `HEALTH-SOURCE-REVIEW.md:3`, `judge-health-functional.md:16` and `health-fever-source.json:6`. Redact login provenance while preserving clinical source/version evidence before PR. No email value reproduced here.

## Verified boundaries

The new default rows have stable IDs, explicit Sales sourceOwner, separate currency/weight types and Sales/Weighing shares; existing records are retained instead of reset on every load. Reporting30/35 ownership changes match latest user instruction. Source provenance remains observed code constant versus proposed local editing, not an assertion staging stores these new values. Source sourceRef617–625 is correct for valuation matrix, and actual local consumers read the same records when available.

`restoreEditorHash` is guarded for malformed URI encoding and only restores a known saved module's Editor. Hashchange and DOM-ready paths both call it. Independently executed judge-production-shell.cjs: legacy Procurement/Editor retains saved department/draft, modern aliases preserve intended destination, rejected standalone event screens remain absent. No browser screenshot proof was captured by this reviewer; parent/visual receipts remain separately attributed.

V12 is present in requirements-consolidated.md:77–79: simple customer park/pen setup, names/capacities, reuse real CRUD, inspect actual app locally against OCI. The mock's current fixed CPT/CBE scope is not fulfillment of editable onboarding. Do not label V12 passed from the research paragraph or from three sample pen rows; actual local-server/CRUD evidence is pending the assigned reviewer.

## Privacy/provenance scan limits

Scanned local html/js/json/txt/md/sql artifacts for private-key headers, Google API keys, access tokens, credential-shaped PostgreSQL URLs and email patterns. No key/token secret patterns found. One URL-shaped match in business-literal-candidates.json points to `backend/internal/ceoai/sqlguard/executor.go:72`: a formatting template with placeholders, not a credential. This heuristic is not a guarantee every possible secret format is absent. Actual SQL exports omitted people identities, credentials and candidate answers; observations and aggregate quantities are business data intentionally inspected for this task.

Exact independent probe `/tmp/sales-final-review.cjs` extends the actual integrated procurement harness; outputs GRANT_REPRO true true and USAGE_REPRO[]. Existing procurement/sales model tests still pass, demonstrating those tests do not cover the new grant/usage gaps. No implementation edit or DB mutation performed in this review.

## Assigned fixes and final local receipt

Parent explicitly assigned this reviewer the Sales grant/usage fixes after the independent findings. Sales Config now gates value lookup by effective module applicability; unavailable Farm value assumptions show “Configuration unavailable.” All Sales-owned table keys register their actual Sales Config / Farm value / ADG page consumers, and opening a consumer reference navigates to that page instead of falsely calling it an archived SOP. Other module mappings remain unchanged. Regression checks revocation, readable unavailable output, restored value and both bound-page usage entries. `sh run-checks.sh` passes all24 active judges after the final edit; receipt final-sales-fix-checks.txt. A separate reviewer must cross-review these authored fixes; this report is not self-approval.

Parent owns the legacy-email redaction. Parent separately reports actual local OCI Animal and Pen forms opened; Pen form lacks capacity and no Add park was found in that inspected flow. This is attributed browser/source evidence, not personally re-observed here. V12 therefore remains a concrete gap: reuse the existing Pen form, add supported capacity authoring and identify/provide park creation with the real backend contracts; do not claim the fixed prototype park selector fulfills onboarding.
