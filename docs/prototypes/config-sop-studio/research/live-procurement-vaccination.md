# Live Procurement SOP and vaccination plan reference

Read-only authenticated Chrome observations,2026-09-16. Source inventory separately pinned in frontend-feature-inventory.md. Served SHA not established. No field changed, saved, published, toggled, or added. Opened existing Change SOP editor only; it showed published inspection document and Save as draft/Publish controls untouched.

## Procurement

Route `/procurement/sops?scope_mode=company`: Procurement SOP, subtitle explaining animal purchase phone inspection; New SOP, Search SOPs, Animal Purchase Inspection card:40questions,7steps,publishedv7,video/photo proof min3.

Detail modal: domain, trigger(blank), code procurement.animal_purchase, version/status, proof gates. Explanation distinguishes inspector's per-animal verdict from CEO/CXO acceptance/rejection on web. Load form has7questions asked once per purchase load. Per-animal form has40questions across5pages: Page1(15), Face visual productivity check(6), Body visual productivity check(8), Udder or testicles(8), Your verdict(3). Questions show kind, compulsory, media limits and conditional visibility. Female/lactating/male-dependent fields are explicit.

Change SOP opens `/procurement/sops?compose=1&edit=9b03b4bd-5f60-4008-83bb-09f8efd29043`. Title Animal purchase inspection; description says future animals use publication while already-open phone form submits against rendered version. Load vendor/farm/loadnumber remain compulsory. Editor has question label/instruction, kind, choices, compulsory, Ask only when, number Min/Max/Unit, media capture type/filelimit, move up/down/to page, Add question/page and page accordion. Some core kind/removal controls disabled; labels/instructions remain editable. Bottom5pages40questions, Save as draft, Publish SOP. These controls were observed only.

No100candidate question set was observed in this live published SOP. Source searches in features/sops found100as followup-series round limit, not a confirmed100-question procurement scope. Treat100as a proposed/user-specified candidate set until its own source is identified; do not merge it into the live40+7 count.

Notable live source-content mismatch worth preserving as observation, not correcting: load question “How much quantity was given per day per animal?” is currently Pick one Yes/No. A generic editor must allow authoring better business semantics without silently claiming published source differs.

## Vaccination

Exact requested route `/vaccination/plan/edit?version=0790415c-b791-4251-a47e-5317e76cbeb3` loads Company vaccination plan, EditingV10 based onV9, Draft—not live yet, bothparks,9of10vaccines on. Clear explanation says publication schedules future tasks; currently assigned/running work is untouched.

Left vaccine list with human-readable timing summaries; Plan settings entries Procurement holding and Automatic safety rules. Selected ET+TT panel: inclusion switch, optional anchor/base-date table, doses from birth, drive first/next visit, deadline, repeat interval and commoninterval choices. Every control is contextual sentence UI, not raw JSON. Proof section read-only explains pen versus animal video grain. Impact shows324animals,5pens,2operator-days at200/day; warning explains split across safe window. Sticky footer Reset(disabled),Save draft(disabled),Publish plan. None invoked.

## Visual evidence and implications

Images `live-procurement-vaccination/vaccination-editor.png` and `procurement-detail.png` captured and visually inspected. Both loaded intended authenticated screens. Editor DOM inspected extensively; no additional editor screenshot claimed.

Generic SOP composition should extend existing library/editor and preserve page/question/conditional/proof scope and pinned-version behavior. Vaccination demonstrates CEO-readable sentence controls and impact-before-publication, but retains a specialized scheduling model. Neither live screen establishes generic sub-SOP composition, so composition remains proposed until runtime contracts are verified. Existing source gates and field locks must not be flattened away.
