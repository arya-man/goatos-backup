# Procurement and Sales integration review

Reviewed parent-authored procurement-practice.js and sales-policy-preview.js independently, then implemented assigned fixes. Initial findings were independent; final patches need a separate judge/browser retest.

## Findings corrected

- Whole-run invalidation after every animal or supply edit destroyed completed selection/holding work. Replaced with stage dependency propagation. Upstream completed stages remain intact. The changed stage reopens completion and approval while retaining its original start time and recorded periodic checks; downstream dependent stages reset. Thus arrival reconciliation no longer erases three days of transit checks. Unchanged animal reviews do not reset progress.
- Feed carry supported one ration only. The local supplies editor now supports multiple named feed/ration rows, explicit candidate reference sets, kg per animal per day and packed quantity per row. Only boarded candidates are accepted, each boarded animal needs coverage, and the same animal cannot be duplicated within one feed. Different feeds may serve the same animal. Every row must have sufficient packed quantity; surplus hay cannot compensate for missing pellets. No ration or wastage factor is invented.
- Feed gate calculations use the running stage plan's pinned travel/warm-up periods rather than mixing current setting values into a prior run. New practices consume new settings.
- A boarded animal's tag cannot silently change during arrival review.
- Sales breed matching now normalizes case/outer whitespace so a specific disabled breed cannot accidentally fall through to an enabled wildcard rule.

## Evidence

Actual-stack procurement judge checks selection/boarding/arrival subsets, exact roles, required tags, exception history, ration math, exact candidate membership, no cross-feed substitution, preserved upstream progress and retained transit evidence. Sales checks common weight/tolerance boundaries, exact breed exclusion, absent groups and normalized breed input. Full `run-checks.sh` passed after edits. Generic item identities and registry data are not modified by these changes.

## Limits

These are explicitly synthetic local candidates, proposed sales settings and practice file metadata. They do not execute production approvals, vaccination, inventory, animal movement or transaction writes. Role labels in practice do not represent a production authentication boundary. Visual verification of the expanded ration-row editor is assigned to the browser judge.
