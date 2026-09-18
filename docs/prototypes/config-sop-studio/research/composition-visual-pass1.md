# Composition visual/browser pass 1

2026-09-16. Independent bounded review of integrated local prototype, not production certification. Read requirements-consolidated.md and feature-requirement-matrix.md in full. No implementation edits. No live data writes, publishing, migration or deployment.

## Freshness and coverage

Chrome dedicated tab at `http://127.0.0.1:4320/?review=composition-1#/configuration/work-instructions`. DOM script list confirmed foundation, shell, generic items, typed config, composition, business examples and sales policy scripts version `composition-1`; orchestration.js absent. Desktop1280×800 and mobile390×844; viewport restored. Compared inspected saved production procurement-sop.png and prior frontend baseline. All five new screenshots below were emitted and visually inspected, not merely captured.

Performed: neutral landing → View examples → Procurement example → Stages and approvals → Start stage practice → attempt destination preparation before Transit. The prerequisite attempt stayed waiting and displayed “Waiting for Transit to be started.” Then visited Sales Config → Review proposed Sales settings on mobile. Escape closed modal. Returned to Work instructions. No generic create/save/reload/try path or full procurement cohort completion was certified in this visual pass; those remain required functional coverage elsewhere.

## Findings requiring refinement

1. **P1 mobile Sales policy controls unusably narrow.** At390px the six-column editable table compresses species to G/S, breed to An, sex to an unlabeled arrow and clips price beyond the modal edge. Common inputs above remain readable. Use responsive labelled group cards or a clearly scrollable minimum-width table with usable field widths. Evidence `composition-pass1/sales-policy-mobile.png`.
2. **P2 composed SOP not visible in its primary Flow view.** Optional Procurement example opens an ordinary three-node Start→instruction→Finish flow. Its six actual stages/parallel dependencies live behind a separate modal. This conceals the requested master/sub-SOP concept at the very point a CEO reviews it. Show an honest stage overview in the existing editor, including Transit and destination preparation in parallel, without inventing a second product.
3. **P2 stage practice buried after six expanded editor forms.** Desktop modal initially fits only first stage. The Start stage practice control is below all six forms; starting practice adds another long list underneath. Collapse stage editing and foreground overview/practice mode. Evidence `composition-pass1/stages-desktop.png`.
4. **P2 inaccurate practice explanations.** Non-approval stages show “approval not recorded” although Requires approval is unchecked. Blocked stages also offer enabled Start/Complete; approval is offered before child completion. The attempted premature destination start was correctly blocked, so this is verified clarity/affordance failure, not a demonstrated prerequisite bypass. Explain only applicable outstanding conditions; disable or clarify unavailable actions.
5. **P2 modal navigation on mobile.** At deep practice scroll, modal title and close button disappear completely; screenshot shows neither, leaving touch users a long scroll back to close. Keep title/close reachable while body scrolls. Evidence `composition-pass1/practice-mobile.png`. Dialog370px fits390px viewport; measured document scrollWidth390, so no document horizontal overflow in this state.
6. **P2 reference link contrast.** Work instructions reference link uses browser-default dark blue against dark green background, inconsistent with production’s muted/green text links and difficult to read. Evidence work-instructions-desktop.png and mobile.png.
7. **P3 stale generic footer.** Procurement editor footer mentions103°F and veterinary treatment even for this composition; contextual copy is needed rather than carrying an unrelated scenario everywhere.

## What passed within bounds

- Neutral department chooser, optional examples, exactly two Configuration leaves. No forced Procurement default or standalone event engine.
- Existing accordion sidebar/header visual family is retained: green active state, dark surfaces, compact outlined buttons. Source-present Alerts stays a reference rather than implying deployed staging data. This is not pixel parity for every route.
- Explicit local-only practice/proposal language is visible in stages, examples and Sales. Sales separates eligibility proposal from transactions/reporting threshold.
- Examples modal offers Procurement, Health and Sales; stage chooser binds named existing SOPs rather than free-text child titles.
- Destination preparation before Transit started was rejected with a specific prerequisite message.
- Modal Escape works in inspected paths; desktop/mobile viewport restored at end.

## Verdict and remaining coverage

**Needs fixes; not CEO/visual accepted.** G05 partial/fail because landing is clearer but composite flow and mobile policy authoring remain unclear. S01–S04 partial demonstration, P07 premature-start negative check passed only; full parallel/arrival proof remains unverified here. A01 has clear proposed boundary but mobile form fails usability. X03 bounded screenshots captured; no production availability or phone execution claim. Repeat pass2 after fixes, checking original screenshots/states plus adjacent generic authoring and Sales controls.
