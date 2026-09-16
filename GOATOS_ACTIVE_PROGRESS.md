# Active progress — PR 287 config/SOP prototype cleanup

Updated: 2026-09-17 03:51 IST
Branch: design/manju-mock-refinement-20260916
PR: #287
Workspace: /Users/raviteja/mesha/tmp/manju-mock-refinement

## User intent

Build a CEO/admin prototype for common CRUD/config without reinventing existing feature screens.

Ravi's latest corrections:
- Do not duplicate existing Weighing, Feed, Procurement, Sales, Health, Vaccination screens inside common config.
- Common config should expose missing reusable business records: animal species/types, breed names, lifecycle stages, shed/stage tags, animal tags/groups, and animal identities.
- CRUD must not be chicken-and-egg. User must be able to create multiple animal types like goat and sheep together and set breed, gender, tags and lifecycle setup together or individually.
- Categories/subcategories/items mean business catalogues like Medicines -> Antibiotics -> item or Animal breeds -> Goat -> Beetal. Modules are not categories.
- K0/K1/K2/K3/warm-up are goat/sheep example values only. Do not use F2; use Fattening.
- Weighing flow already exists. Do not invent Weighing mode as an SOP question. Work item decides individual vs lump-sum. Android flow is scan/weight/video/submit, with duplicate scan guard only.
- Preview must match actual Android/module screens, not a fake phone simulator.
- Keep UI modern, uncluttered, no big lectures, no redundant open buttons if cards/list rows are clickable.

## Done in current cleanup

Pushed earlier commits:
- 18e410457 Remove duplicated existing feature registers
- 7f309b42f Refine common config workspace UX
- c44b39e27 Clarify catalogue hierarchy in config workspace
- f333cf079 Remove attributed catalogue wording
- e739e87e7 Polish config setup hierarchy UX

Uncommitted but now tested green:
- Removed fake preview links from SOP editor surfaces.
- Changed old #Module/Preview route fallback to redirect to module SOP route.
- Removed production shell SOP tabs that showed full analytics/config under Weighing SOP.
- Updated branch/typed judge tests to stop depending on invented Weighing condition.
- Weighing/Feed/Procurement SOP routes now show existing-module summaries instead of opening the generic SOP builder/fake Android preview.
- Config workspace separated into Animal lists, Farm places, Animal identities, Feed setup, Catalogue.
- Added Bulk animal setup dialog so multiple species/breeds/stages/tags can be created together from textarea rows.
- Animal identity form uses identity first and filters breed/stage/shed tag choices by selected species.
- Status badge CSS compacted.
- Long explanatory copy trimmed.

## Test state

Green after latest edits:
- node --check on changed JS files
- sh docs/prototypes/config-sop-studio/run-checks.sh
- git diff --check

Browser checked fresh routes:
- /#/configuration/items shows tabs: Animal lists, Farm places, Animal identities, Feed setup, Catalogue.
- /#/weighing/sops shows Existing Weighing flow with per-animal, lump-sum and verification cards; no Rules Used Here tabs, no full analytics clone, no fake phone preview modal.

## Current pending before final handoff

- Capture/validate final screenshots if needed.
- Commit and push to PR #287.

## Deployment state

No deploy. No main merge. PR branch only.
