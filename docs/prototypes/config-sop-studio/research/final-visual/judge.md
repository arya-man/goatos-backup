# Final visual judge — local prototype

Compared local port 4320 against authenticated production screenshots in `../visual-baseline`, not the earlier standalone mock. Desktop viewport 1728×819; narrow viewport 390×844. Dedicated Chrome tab; no production data writes. This is representative visual validation, not pixel-diff automation or complete frontend parity certification.

## Verified

- Header height, left navigation width, dark surfaces, green accent, accordion groups and exact Sales labels now follow the production shell. Configuration is additive using the same navigation pattern.
- Items Config, ADG Analytics, Farm value and Feed Config render within that shell. Titles, cards, table containers and inline feed rate editing are readable at desktop size.
- Existing SOP editor still renders the graph, toolbar, step selection and property inspector at desktop size.
- Narrow navigation uses the icon rail and opens an accordion drawer. Selecting Items Config closes the drawer. Header scope and account controls fit.
- Item form at desktop and narrow width is readable, with visible title/close action and fixed footer actions. The initial header-over-dialog defect was fixed in production-shell.css by raising modal stacking above the header, then recaptured and visually checked.

## Evidence

All PNGs below were opened visually after capture:

- items-desktop.png
- item-form-desktop.png (recaptured after fix)
- adg-desktop.png
- farm-value-desktop.png
- feed-inline-desktop.png
- sop-editor-desktop.png
- sop-editor-mobile.png
- items-mobile.png
- item-form-mobile.png

## Remaining limitations

- Consumer pages are explicit local sample previews, not full replicas of production datasets, filters and every tab. Other routes retain honest reference-preview content. Do not claim all 35 destinations are implemented.
- ADG capture preceded addition of Download, average-weight filtering and Last weighed date. Parent reports these fixes and park-scope filtering implemented, but this visual pass has not recaptured them. Parent functional browser proof is required for those final changes.
- Mobile SOP toolbar wraps across much of the first viewport. The graph requires scrolling; desktop is the practical authoring surface. This predates the shell and remains a usability refinement opportunity, not a blocked navigation control.
- Hierarchy dialog final recapture was blocked by repeated browser CDP timeouts after Cancel. Item creation form was verified, but hierarchy dialog is not certified by this pass.
- The whole production UI has not been exhaustively reproduced or regression-tested. Baseline report records which live routes were visually inspected versus navigation-only inventory.

Validation: `node judge-production-shell.cjs` passes 35 existing +4 additive routes, Sales label checks, consumer hook, SOP/editor route preservation, reference preview, reusable tools mapping and mobile drawer state. Viewport reset completed before browser timeout. No deployment or push.

Final coverage clarification: all 35 live destinations were visited: 12 visually inspected loaded screens, 22 DOM/accessibility-only observations, and one live Vaccination error. Parent subsequently visually checked final ADG in Chrome and completed inherited-item Health/Preventive Care action-picker journeys; the lower-form inherited-item screenshot alone does not prove those journeys.
