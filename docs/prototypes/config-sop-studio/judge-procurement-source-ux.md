# Animal Purchase Inspection source-fidelity UX acceptance

Status: source acquisition and implementation pending; previous canvas proof does not certify this imported questionnaire.

## Fidelity

- Display exact observed published SOP name/version and recorded source provenance. Preserve immutable source snapshot; imported changes are a separate local draft.
- Preserve page/step grouping, stable question IDs, order, wording, help text, answer types, complete option sets, required flags, units/bounds, proof requirements and conditions.
- Do not replace source outcomes with generic completion labels or invent business actions absent from the source.
- Distinguish question options copied from source versus dynamic shared datasets. Expose owner/source and unavailable-source states.
- Unknown/unsupported source features remain visible and block misleading publication/simulation rather than silently becoming text inputs or linear paths.

## List and Flow parity

- Both views render one saved definition. Selecting a question in one view reveals same item/properties in other.
- Edits to wording, options, required flags and condition values survive view changes/reload.
- Generated decision nodes explicitly state their source visibility condition. True branch asks the conditional question; fallback skips it and rejoins the correct next step.
- Multi-clause conditions retain ALL/ANY semantics and exact comparator/value, not a guessed boolean substitute.
- Branch endpoints remain editable through direct canvas controls already accepted; new source integration must not regress ports/insertion/undo.

## Browser judge scenarios

1. Open actual imported source title/version; compare first/last question plus every answer type, representative full choices and grouped source pages against read-only production evidence.
2. Switch List→Flow→List preserving selected field and content.
3. Change a copied draft question/choice, verify both views and reload; unchanged source snapshot remains inspectable.
4. Test conditional match and nonmatch, correct skip/rejoin, required response and proof handling. Unsupported feature must state its limit.
5. Inspect a shared-source dropdown showing owner and full permitted options.
6. Desktop long-list scroll, canvas fit/selection and narrow operator questionnaire layout receive visual check after final edits.

No production mutation/deployment or live clinical authority is covered by this mock UX receipt.

## Final desktop visual receipt

Scoped PASS after final questionnaire/proof controls and dynamic canvas extent edits. Independent native Chrome inspection on http://127.0.0.1:4318/#Procurement/Editor only; production tab untouched.

- Verified List displays Load number, Vendor and Farm with original Farm choices CBE/CPT.
- Found section selector changed inspector without scrolling List. Fixed page-focus.js to scroll the actual canvas container explicitly, then verified Your verdict jumps to Decision, Breed and Note, with Selected/On Hold choices and optional Breed/Note.
- Verified same final-section questions in Flow at85% with visible connections after final dynamicSVG refresh.
- Verified Udder or testicles flow with source-derived Gender Female visibility gate, Milk yield match path and Otherwise rejoin.
- Verified proof properties within bounded scrolling inspector: Photo or video either accepted, max1, min1, source slot udder.
- Nav count now calculated from source question nodes, rather than fixed45.

This is visual/source-authoring signoff, not exact production DSL export certification. Source provenance remains reconciled rendered production UI plus repository baseline, and root/functional judge own the complete source and185-test receipts. No new narrow viewport usability certification was performed for this large imported editor. Left local Chrome in readable Udder Flow at85%, proof inspector. Root owns tab deliverable marking.
