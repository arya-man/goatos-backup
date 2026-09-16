# Round 5 repeated review

PASS for the reviewed local mock corrections, with independent code cross-review and bounded desktop/mobile visual checks.

Three independent reviewers challenged the previous pass against the eleven anonymous transcript records, screenshots, source architecture and the real staging storage mapping. New findings: saved catalogue type conversion broke SOP consumers; invalid optional numeric inputs skipped; multiple-choice branch testing used substring semantics; mobile active tab was offscreen. Parent also corrected the overly broad earlier name-first claim.

Code fixes lock saved item type with an explanation, retain numeric badInput through step/page previews, and use typed choice controls with stable IDs and exact membership. Both code reviewers independently checked each other's corrections. All 20 suites pass; see round5-final-checks.txt and judge-round5-cross-review.md.

Parent fresh browser confirmed saved Reusable needle with two dependent SOPs has its type locked. Fresh new item form starts with Item name both for Catalogue and after switching to Configuration.

The real goatos-stg data plan remains based on the 18:23 IST read-only Cloud SQL inspection, not a fresh query in this repeat. See staging-data-plan.md. No database writes, commit, push, merge or deployment. This is a local mock with illustrative/reference routes, not full production visual or integration certification.

Final visual closure: synchronous tab-strip reveal plus resize handling corrected hidden active tabs. Versioned shell loading removed stale browser script reuse. Parent fresh 390px IAB reload measured active Run insights at x302.68–376 within strip76–376, scrollLeft292, document width390; screenshot visually confirmed. Independent Chrome screenshot also passes. All20 suites and git diff --check pass after final changes.
