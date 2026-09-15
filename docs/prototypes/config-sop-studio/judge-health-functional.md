# Health course independent functional judge

**PASS — 16 / 16 executable assertions.**

Command: `node judge-health-functional.cjs` (exit 0).

Verified:
- Exact live-source Fever fixture fidelity for all 16 rows in Adults and Kids, including dosage strings, blanks, denominator, route, treatment, source row.
- Ordered day/session groups contain 6/5/5 steps; unspecified session is not inferred.
- Source medicine numerator units remain explicitly unspecified.
- Invalid day, missing medication item, and unavailable item block draft validity; confirmation revalidates.
- Published course and item details remain independent of draft edits and live item rename; earlier versions remain retained.
- Local add/edit preserves stable step ID; remove does not mutate published snapshot; read-only roles cannot save/remove.
- No live network calls or dosage calculation in course code.

Source was read through the Google Drive/Sheets connector as ravi@mesha.sg on 2026-09-15. Exact source fixture: health-fever-source.json, Adults SOP gid582297317 and Kids SOP gid541230517, C2:J18.

SHA-256:
- health-course.js: `8c7cc3996ce83663398fb5af4b0c629c262280b6fad326cd67d7528893655d29`
- health-fever-source.json: `e665a3368542c35e80a7a2e2072e34edb40bda5d3a5fa89c1cb33402d487a2b9`

This is a source-faithful local course authoring preview, not a clinical validity review, dose recommendation, or live treatment scheduler. Browser visual/click validation remains root-owned.

## Final course-item integration receipt

**PASS — 21 / 21 assertions** after course item-usage wrappers and unspecified-unit option changes.

Added checks: exact course draft/published step usages; frozen published item label; item archival impact acknowledgement; archival retains course reference while invalidating draft; item editor preserves `source unspecified` unit.

Final SHA-256:
- health-course.js: `7b11159f5d215d26d26bffecc0e255a9a2660e09d96cb4a8ec788e66612278bc`
- items.js: `9cb03ac36443dccf76f17f0b227e02ee243408c84fb68280b1cf183c15bf4609`

Command: `node judge-health-functional.cjs` (exit 0; `HEALTH RECEIPT: 21 passed`). No remaining reported functional defects.
