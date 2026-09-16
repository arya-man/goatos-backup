# Current judge contract

Use requirements-consolidated.md, feature-requirement-matrix.md, all 11 voice-note summaries, supplied screenshots, and the full written procurement/health/sales/vaccination explanation. Cross-check backend, Android, frontend and real staging inventories. Treat examples as illustrative, never exhaustive.

1. Generic items/settings: category, subcategory and direct sharing union; arbitrary departments; stable identities; typed settings; actual SOP consumption; revoke/archive impact; frozen published values.
2. SOP composition: actual smaller SOP references, prerequisite start/completion/approval, parallel work, missing references/cycles, child completion, immutable child versions, role guards, context isolation and editable time/evidence requirements.
3. Procurement: all offered candidates versus approved subset versus boarding/received subsets; separate operator recommendation/reviewer decisions; tagging/holding/vaccination; repeat inspection; feed for travel plus warm-up; interval evidence; parallel destination preparation; gradual feed transition. Do not invent clinical rules or a feed ratio.
4. Existing domain ownership: Health assessment/protocols, Vaccination plan and inventory, Sales group rules and Weighing consumption. Clearly label observed existing, partial, proposed and unverified behaviour. Empty staging tables do not prove a feature absent.
5. CEO usability and visual regression: familiar shell/menu/accordions/buttons/titles; two generic configuration destinations; no standalone event engine; desktop and narrow layouts; keyboard/dialog behaviour; no hidden critical controls.
6. Current architecture: last month source history and sync/outbox contracts; prototype does not claim live integrations. Production failure strings/backend availability and media egress are retained as integration review requirements, not claimed solved by a local HTML mock.

Record each finding with reproducible evidence and verify its fix. Perform two design review/fix passes, then review the PR against main again after creation. No historical PASS is a substitute for final integrated evidence.
