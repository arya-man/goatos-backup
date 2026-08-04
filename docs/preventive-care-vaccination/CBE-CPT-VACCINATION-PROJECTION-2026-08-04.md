# CBE/CPT Vaccination Projection - 2026-08-04

## Purpose

This note documents the latest CBE/CPT animal-count reconciliation and vaccination projection using:

- Manohar seed JSON source: `CBE-CPT-goats (2).json` from local handoff; corrected committed copy is `docs/preventive-care-vaccination/artifacts/2026-08-04-cbe-cpt/CBE-CPT-goats-corrected-2026-08-04.json`
- Aryaman shared files/messages/screenshots
- Vaccination V2 history already reflected in the JSON
- Dashboard count reference from `goatos-sheets`

Use this as input for an HTML visual/table.

## Instructions For Claude HTML Visual

Build a UI with three sections:

1. **Confirmed Drive Plan** - render `Confirmed Vaccination Projection Table` in the exact column format below.
2. **Resolved / No Drive Needed** - show cohorts that already received the vaccine and should not be scheduled again.
3. **Problem / Needs Reconciliation** - render unresolved cohorts separately, with red/amber status badges. Do not mix these into the confirmed drive plan.

Required table columns for the main confirmed projection table:

| Vaccine | Total eligible | Past same-vaccine dates | Projection anchor | History-backed animals | No-history animals | How they are grouped | Frequency | Projected drive dates | Final drive total |
|---|---:|---|---|---:|---:|---|---|---|---:|

For unresolved rows, keep the same visual style but use these columns:

| Problem group | Count | Current source conflict | What is known | What is missing | Drive decision |
|---|---:|---|---|---|---|

## Verification Boundary

This document is source/projection evidence, not a completed live-stg database audit.

| Surface | Status |
|---|---|
| Manohar JSON `(2)` | Parsed and counted locally |
| Aryaman files/messages/screenshots | Captured into counts, anchors, and projection rules |
| GoatOS CPT adult stg fixture/source evidence | Captured for the 324-adult ET+TT booster drive |
| Live `goatos-stg` Cloud SQL | Not yet queried in this pass because gcloud token reauth blocked non-interactive secret access |

Do not claim the `114 / 163 / 47` split is live-DB verified until `goatos-stg` Cloud SQL is queried directly.

## Count Reconciliation

| Source | Total | CBE | CPT | Notes |
|---|---:|---:|---:|---|
| Dashboard | 1673 | 942 | 731 | Census/current dashboard source |
| Aryaman covered evidence | 1484 | 935 | 549 | Evidence coverage only, not full census |
| Manohar JSON `(2)` | 1670 | 935 | 735 | Current seed JSON under review |
| JSON vs Dashboard | -3 | -7 | +4 | Net mismatch remains |

## Aryaman Evidence Breakdown

| Aryaman source | Farm | Count | Notes |
|---|---|---:|---|
| `double-tagging-CBE-2026-08-01.csv` | CBE | 688 | CBE RFID/current sheet |
| Aryaman CBE fattening/count message | CBE | 247 | C1/C2/C3 + G2P6/G2P7/G2P8 |
| `CPT_VaccinationDrive_ShedRoaster.pdf` | CPT | 224 | CPT kids vaccination shed roster |
| CPT July 25 birth WhatsApp message | CPT | 1 | Kid `901007000504378` |
| `CPT Adult RFIDs.pdf` | CPT | 324 | CPT adult RFIDs; exactly matches JSON CPT adults |
| **Total Aryaman covered evidence** |  | **1484** |  |

## CBE Fattening / Yashoda 10 Correction

Manohar message:

> cbe castro y10 parity 3 unayi which is making it match with yashoda 10 in future db, as making count equal just is the task as rfid are temp only for castro 1-3

Current JSON `(2)` implements this as:

| Bucket | Expected / Message | JSON `(2)` |
|---|---:|---:|
| CBE Castro 1 | 63 | 63 |
| CBE Castro 2 | 74 | 74 |
| CBE Castro 3 | 65 | 65 |
| Yashoda 10 parity/temp Castro ICU rows | 3 | 3 |
| Yashoda 10 total | 18 | 18 |

Conclusion: Aryaman CBE evidence matches Manohar JSON CBE total.

## Current JSON `(2)` Population

| Farm / age / species | Count |
|---|---:|
| CBE adult goats | 414 |
| CBE adult sheep | 71 |
| CBE kids goats | 206 |
| CBE kids sheep | 244 |
| CPT adult goats | 95 |
| CPT adult sheep | 229 |
| CPT kids goats | 92 |
| CPT kids sheep | 319 |
| **CBE total** | **935** |
| **CPT total** | **735** |
| **Grand total** | **1670** |

## Animal Coverage Audit For Projection Rows

The vaccination projection table must cover all 1670 animals from `CBE-CPT-goats (2).json`.

| Farm / cohort | Animals in JSON | Projection coverage status |
|---|---:|---|
| CBE adult goats | 414 | Covered |
| CBE adult sheep | 71 | Covered |
| CBE fattening / Castro-Y10 kids | 205 | Covered |
| CBE tagged kids with V2 ET+TT first-dose date | 242 | Covered |
| CBE Yashoda 6 K2 missing from Aryaman CSV/V2 | 3 | ET+TT booster cleanup: missed the 1-2 Aug CBE kids booster round, so schedule this week on 5-6 Aug |
| CPT adult goats | 95 | Covered |
| CPT adult sheep | 229 | Covered |
| CPT kids with V2/roster ET+TT history | 233 | Covered |
| CPT purchased fattening temp/count kids | 140 | Resolved: C1/C2/G2P1/G2P2 purchased fattening have ET+TT, booster, PPR done |
| CPT G2P3 rows needing separate parse | 37 in JSON; 39 scanned in PDF | Vaccine status resolved by Aryaman/PDF: G2P3 were given vaccines and the PDF has real RFIDs in a different format; remaining issue is parser/RFID mapping/count reconciliation |
| CPT July 25 birth `901007000504378` | 1 | Covered separately as no dose yet |
| **Total** | **1670** | Confirmed + resolved + problem rows separated below |

## Vaccination Rules From Aryaman Screenshots

| Rule | Detail |
|---|---|
| ET+TT booster | Booster is 3 weeks after first ET+TT dose |
| Sheep Pox + Blue Tongue compatibility | Can be given on the same day |
| Sheep Pox / Blue Tongue after ET | Can be given 2 weeks from the last ET date |
| Blue Tongue booster | Booster is 3 weeks from first Blue Tongue dose |
| Newborn ET+TT trigger | July 25 CPT birth should get first ET+TT at 3 weeks from birth |
| Missing DOB age assumption | K2 kids assume 8 weeks old; F2 kids assume 16 weeks old |

## Code-Backed Projection Rules To Apply

Use this section to separate dates that GoatOS can compute from configured rules from dates that are Aryaman-entered schedule inputs.

| Projection item | Code / approved-matrix basis | How to use here |
|---|---|---|
| ET+TT kid first dose | Approved matrix: 4 weeks / 28 days from DOB or trusted assumed age |
| ET+TT kid booster | Approved matrix and code: 7 weeks / 49 days from DOB, minimum 21 days after first dose |
| ET+TT adult dose 2 / booster | Code-backed: `et_tt_adult_w2` is `after_previous_completion`, offset 21 days, min gap 21 days |
| ET+TT repeat after completed course | Code-backed: `et_tt_revac` is after previous completion, offset 182 days |
| Newborn `901007000504378` ET+TT | DOB 2026-07-25 + 21 days for the first field action per Aryaman newborn instruction; booster then +21 days from actual dose 1 |
| Pox / Blue Tongue after recent ET+TT | Code safety gap: killed-to-live / killed-to-killed gap is 14 days unless an approved same-day pair is selected; this supports the CPT "from 10 Aug onward" rows after 24-26 Jul ET+TT |
| Same-session limit | Code-backed max two vaccines per animal per session |
| Adult Blue Tongue booster at +3 weeks | Aryaman WhatsApp instruction, not currently the committed automatic adult Blue Tongue matrix rule. The committed matrix has Blue Tongue kid dose 1 / booster at 16w / 20w and adult Blue Tongue yearly repeat from accepted completion. Keep the 26 Aug and 31 Aug rows only as Aryaman-entered schedule inputs unless the protocol config is updated to an adult BT W2 rule. |
| Repeat cycles for PPR / pox / FMD / HS / adult Blue Tongue | Code-backed per-vaccine anchor precedence: latest accepted same-vaccine completion anchors the next repeat. Do not anchor one vaccine from another vaccine's completion except for medical gap constraints. |

## Missing-DOB Kid Booster Anchors

When DOB and first ET+TT date are missing, use Aryaman's age assumption to avoid leaving the booster unplanned. ET+TT first dose is anchored at 3 weeks of age and ET+TT booster is anchored 3 weeks after first dose, i.e. 6 weeks of age.

| Farm / group | Count | Tag | Assumed age as of 2026-08-04 | Assumed first ET+TT anchor | Booster anchor | Projection date |
|---|---:|---|---|---|---|---|
| CBE Yashoda 6 K2 cleanup kids | 3 | K2 | 8 weeks | First ET+TT around 1-2 Jul with other CBE kids | Missed the 1-2 Aug CBE kids ET+TT booster round | **2026-08-05/06** |
| CPT purchased fattening temp/count kids | 140 | F2 | 16 weeks | Already handled by Aryaman confirmation | Already handled by Aryaman confirmation | No drive needed: ET+TT, booster, PPR confirmed done |
| CPT G2P3 tagged animals | 39 scanned in PDF; 37 JSON mock rows | Adult/F2 sheep source conflict | Not a mock-only cohort: `CPT_VaccinationDrive_ShedRoaster (1).pdf` has a Godel 2 Part 3 vaccination-drive log with real RFIDs | Vaccination evidence exists for 20 Jul 2026; JSON still needs count/mapping cleanup | No drive now |

## Historical Vaccine Anchors From Aryaman / JSON

| Farm / group | History / anchor |
|---|---|
| CBE adult goats | HS/FMD on 21 Feb 2026; Goat Pox on 19 Mar 2026; FMD booster on 20 Mar 2026; ET+TT dose 1 on 30 Jun / 1 Jul 2026 |
| CBE adult sheep | ET+TT/HS/FMD on 22 Feb 2026; Sheep Pox/Mycoplasma on 19 Mar 2026; FMD booster on 20 Mar 2026 |
| CBE recent ET+TT booster | Updated Aryaman/user anchor: anything previously scheduled for 4 Aug moves to 5 Aug; show remaining CBE Sumathi adult-goat ET+TT booster and adult-sheep Blue Tongue work as 5-6 Aug 2026 with 5 Aug as anchor |
| CBE adult sheep next | Blue Tongue on 5-6 Aug 2026; booster 26 Aug 2026 is the Aryaman-entered +3-week schedule input from the 5 Aug anchor |
| CBE kids ET+TT booster | Aryaman update: CBE kids got first ET+TT around 1-2 Jul and booster on 1-2 Aug 2026, except the 3 Yashoda 6 K2 RFIDs listed in the cleanup row |
| CBE fattening | CBE C1/C2/C3 have ET+TT, ET+TT booster, and PPR done |
| CPT adult goats | HS/FMD on 19 Mar 2026; ET+TT booster on 9 Dec 2025; PPR/FMD booster on 9 Dec 2025; recent ET+TT booster drive on 24-26 Jul 2026 |
| CPT adult sheep | Aryaman adult-history screenshot says none before recent ET+TT drive; recent ET+TT booster drive on 24-26 Jul 2026 |
| CPT adult ET+TT booster execution | GoatOS stg/source fixture has CPT adult target 324; 114 accepted history rows dated 2026-07-24 and remaining 210 scheduled by catch-up override. Operational split to capture for history: 24 Jul = 114, 25 Jul = 163, 26 Jul = 47, total 324. Video verification is still pending, so medical projection should treat the drive as administered history but proof state as not finally verified. |
| CPT kids | Roster/history kids have ET+TT dose 1 on 1 Jul 2026 and booster on 24 Jul 2026 where present |
| CPT July 25 birth | Kid `901007000504378`, born 25 Jul 2026, currently Yashoda 3, no vaccine history |
| Purchased fattening at both farms | Aryaman confirmed purchased fattening animals at both CBE and CPT have ET+TT, ET+TT booster, and PPR done. For CPT this applies only to C1, C2, G2P1, and G2P2. G2P3 is not purchased fattening, but Aryaman separately clarified G2P3 were given vaccines and the confusion was because their PDF/screenshot format was different. |
| CPT G2P3 format evidence | `CPT_VaccinationDrive_ShedRoaster (1).pdf` has `Vaccination drive log — 20 Jul 2026`, total scanned **39**, on record **38**, new **1**, under `Godel 2 Part 3`. Screenshot/PDF format differs from other sheds, so parse G2P3 separately. |
| CPT G2P3 PDF breakdown | 36 adult female sheep on record: Godel 1 Partition 1 = 21, Mandela 2 Partition 10 = 8, Mandela 2 Partition 9 = 7. Plus 2 F2 kids on record: `901007000505113` male in Godel 2 Partition 4, `901007000505085` female in Mandela 2 Partition 5. Plus 1 new animal `901007000504386` old ID `Flock BLR 0702 CPT` with no shed/age in extracted PDF text. |
| CPT G2P3 sample RFIDs | Examples from the PDF: `901007000504036` old ID `981 BLR`; `901007000504117` old ID `797 BLR`; both are CPT Anantapur Sheep, Female, Adult, on record. |

## CPT Adult ET+TT Booster Execution History

| Date | Vaccine | Farm / group | Animals | Source / status |
|---|---|---|---:|---|
| 2026-07-24 | ET+TT booster | CPT adults | 114 | GoatOS stg/source fixture accepted-history count; video verification pending |
| 2026-07-25 | ET+TT booster | CPT adults | 163 | Operational/Aryaman-Manohar drive split; video verification pending |
| 2026-07-26 | ET+TT booster | CPT adults | 47 | Operational/Aryaman-Manohar drive split; video verification pending |
| **Total** | **ET+TT booster** | **CPT adults** | **324** | Matches CPT adult source total |

## Confirmed Vaccination Projection Table

Only show rows here when the group is trusted enough to schedule or to record as completed history.

| Vaccine | Total eligible | Past same-vaccine dates | Projection anchor | History-backed animals | No-history animals | How they are grouped | Frequency | Projected drive dates | Final drive total |
|---|---:|---|---|---:|---:|---|---|---|---:|
| CBE adult goats - ET+TT booster | 414 goats total; 134 pending Sumathi goats for this drive | Dose 1: 30 Jun / 1 Jul 2026; non-Sumathi adults done 1-2 Aug per Aryaman | Dose 1 + 3 weeks | 280 done by Aryaman message | 134 pending Sumathi goats | Sumathi 1 Parts 1-8; Sumathi 2 Parts 1-6 | Booster 3 weeks after first dose | **5-6 Aug 2026**, with 5 Aug as anchor | **134** |
| CBE adult sheep - Blue Tongue | 71 sheep | None in JSON | Recent ET+TT booster / updated Aryaman 5 Aug anchor | 0 | 71 | Adult sheep drive | Adult Blue Tongue dose 1 now; code-backed future repeat is yearly from accepted completion | **5-6 Aug 2026** | 71 |
| CBE adult sheep - Blue Tongue booster | 71 sheep | Blue Tongue dose 1 projected 5-6 Aug 2026 | Aryaman +3-week booster instruction from 5 Aug anchor | 0 | 71 | Same 71 adult sheep | **Aryaman schedule input:** +3 weeks after first Blue Tongue; current code matrix does not auto-generate adult BT W2 unless configured | **26 Aug 2026** | 71 |
| CBE normal tagged kids - ET+TT booster | 242 kids | First ET+TT around 1-2 Jul / JSON dose1: 3 Jul 2026 | Dose 1 + 3 weeks | 242 | 0 | Normal CBE kids excluding Castro/Y10 fattening mock rows and excluding the 3 Yashoda 6 K2 cleanup kids listed below | Booster 3 weeks after first dose | **Done 1-2 Aug 2026**, per Aryaman; except the 3 cleanup RFIDs | 242 |
| CBE Yashoda 6 K2 cleanup - ET+TT booster | 3 kids | First ET+TT around 1-2 Jul with other CBE kids; blank in JSON/V2 for these 3 | Missed the 1-2 Aug CBE kids ET+TT booster round | 0 | 3 | RFIDs `901007000504784`, `901007000504725`, `901007000504736` | Cleanup ET+TT booster this week | **5-6 Aug 2026** | **3** |
| CPT adult goats - ET+TT booster history | 95 goats | Booster drive: 24 Jul = 114 adult total first-day slice; 25 Jul = 163; 26 Jul = 47 across all CPT adults | CPT adult source/stg drive execution | 95 | 0 | Included in 324-adult CPT ET+TT booster drive | Booster 3 weeks after first dose; proof/video verification remains separate | Administered 24-26 Jul 2026 | 95 |
| CPT adult sheep - ET+TT booster history | 229 sheep | Booster drive: 24 Jul = 114 adult total first-day slice; 25 Jul = 163; 26 Jul = 47 across all CPT adults | CPT adult source/stg drive execution | 229 | 0 | Included in 324-adult CPT ET+TT booster drive | Booster 3 weeks after first dose; proof/video verification remains separate | Administered 24-26 Jul 2026 | 229 |
| CPT adult goats - Goat Pox | 95 goats | None | ET+TT booster administered 24-26 Jul 2026 | 0 | 95 | CPT adult goat-only drive | Yearly from actual Goat Pox date afterward | From 10 Aug 2026 onward | 95 |
| CPT adult sheep - Sheep Pox | 229 sheep | None | ET+TT booster administered 24-26 Jul 2026 | 0 | 229 | CPT adult sheep drive; can pair with Blue Tongue | Yearly from actual Sheep Pox date afterward | From 10 Aug 2026 onward | 229 |
| CPT adult sheep - Blue Tongue | 229 sheep | None | ET+TT booster administered 24-26 Jul 2026; 14-day safety gap supports 10 Aug onward | 0 | 229 | Same 229 sheep; can give with Sheep Pox | Code-backed future repeat is yearly from accepted Blue Tongue completion | From 10 Aug 2026 onward | 229 |
| CPT adult sheep - Blue Tongue booster | 229 sheep | Blue Tongue dose 1 not yet done | Aryaman +3-week booster instruction if dose 1 is done on 10 Aug | 0 | 229 | Same adult sheep cohort | **Aryaman schedule input:** +3 weeks after first Blue Tongue; current code matrix does not auto-generate adult BT W2 unless configured | If dose 1 on 10 Aug, booster 31 Aug 2026 | 229 |
| CPT kids with history - PPR | 233 kids | ET+TT dose 1: 1 Jul; booster: 24 Jul | ET+TT booster complete | 233 | 0 | CPT kids from vaccination roster/history | PPR after ET+TT course | Next pending drive | 233 |
| CPT July 25 birth - ET+TT dose 1 | 1 kid | None | Birth date: 25 Jul 2026 | 0 | 1 | Single newborn: `901007000504378`, Yashoda 3 | First ET+TT at 3 weeks from birth | **15 Aug 2026** | **1** |
| CPT July 25 birth - ET+TT booster | 1 kid | ET+TT dose 1 due 15 Aug 2026 | Dose 1 + 3 weeks | 0 | 1 | Same newborn `901007000504378` | Booster 3 weeks after first dose | **5 Sep 2026**, if dose 1 given 15 Aug | **1** |

## Resolved / No Drive Needed

| Group | Count | Vaccine status | Reason |
|---|---:|---|---|
| CBE purchased fattening: C1/C2/C3 plus Yashoda 10 parity rows | 205 | ET+TT done, ET+TT booster done, PPR done | Aryaman confirmed purchased fattening animals at both CBE and CPT have all three done |
| CPT purchased fattening: C1/C2/G2P1/G2P2 | 140 | ET+TT done, ET+TT booster done, PPR done | Aryaman confirmed only C1/C2/G2P1/G2P2 are purchased fattening in CPT; no drive now |
| CPT G2P3 tagged animals | 39 scanned in PDF; 37 JSON mock rows | Vaccines given on 20 Jul 2026; no drive now | PDF says total scanned 39, on record 38, new 1. JSON still has 37 mock rows, so mapping/count reconciliation is needed. |

## Problem / Needs Reconciliation

Do not put these into the confirmed drive plan until the missing source issue is resolved.

| Problem group | Count | Current source conflict | What is known | What is missing | Drive decision |
|---|---:|---|---|---|---|
| CBE Yashoda 6 K2 RFIDs: `901007000504784`, `901007000504725`, `901007000504736` | 3 | Present in Manohar JSON but missing from Aryaman CBE CSV, V2 Combined, Unified Workflow DB Action search, and Goats DB event search | These 3 missed the CBE kids ET+TT booster round that happened 1-2 Aug | Exact past ET+TT dose date is still missing in JSON, but latest user/Aryaman clarification says schedule ET+TT booster now | Schedule cleanup ET+TT booster on **2026-08-05/06** |
| CPT G2P3 rows needing separate parse | 37 in JSON; 39 scanned in PDF | Manohar JSON currently uses `GODEL2P3-001..037`, but `CPT_VaccinationDrive_ShedRoaster (1).pdf` has a real-RFID Godel 2 Part 3 vaccination log on 20 Jul 2026 | Aryaman/PDF vaccine status is resolved as done: PDF total scanned 39, on record 38, new 1 | Need parse/import of the 39 PDF rows, map/replace JSON mock IDs, and reconcile why JSON has 37 while PDF scanned 39. Carry 20 Jul 2026 vaccine evidence onto those records. | No drive now; parse G2P3 separately and clean up mapping/history |

## Special Must-Not-Miss Animal

| Field | Value |
|---|---|
| RFID | `901007000504378` |
| Farm | CPT |
| Current shed | Yashoda 3 |
| Shed tag in JSON | K1 |
| Species | Goat |
| Breed | Sojat |
| Gender | Male |
| Date of birth | 2026-07-25 |
| Birth time | 11:58 am |
| Birth weight | 4.1 kg |
| Mother RFID | `901007000504141` |
| ET+TT dose 1 due | **2026-08-15** |
| ET+TT booster due | **2026-09-05**, if dose 1 is given on 2026-08-15 |
| Source note | Separate Aryaman message: "in addition to this one kid was born on the 25th July - `901007000504378`; its in Y3 CPT right now". This newborn is not part of the 20 Jul G2P3 vaccination-drive PDF log. |

## Remaining Data Quality Notes

| Issue | Impact |
|---|---|
| Dashboard total is 1673 but Manohar JSON `(2)` is 1670 | Overall JSON remains 3 short versus dashboard |
| CBE dashboard 942 vs JSON 935 | CBE remains 7 short versus dashboard, though Aryaman CBE evidence matches JSON |
| CPT dashboard 731 vs JSON 735 | CPT JSON has 4 more than dashboard |
| Aryaman CPT evidence vs JSON CPT | Confirmed sources cover CPT adults, CPT kid roster/history, the July 25 birth, purchased fattening confirmation, and latest G2P3 clarification. Remaining G2P3 issue is data quality: parse the separate G2P3 PDF format and reconcile 37 JSON mock rows vs 39 PDF scans, then replace/map to real tagged RFIDs and 20 Jul vaccine evidence. |
| CBE Castro/Yashoda 10 temp RFIDs | Counts are aligned, and Aryaman confirmed purchased fattening animals got ET+TT, booster, and PPR. These are temporary/count-parity IDs, not real RFID scans. |
| CBE Yashoda 6 K2 status | 3 RFIDs remain present in Manohar JSON only, but latest user/Aryaman clarification gives the ET+TT decision: they missed the 1-2 Aug kids booster round, so schedule cleanup booster on 2026-08-05/06. |
| Missing first-dose dates after latest Aryaman clarification | Do not use the old blanket `3 CBE + 177 CPT` schedule. Updated ET+TT action: CBE kids got first dose around 1-2 Jul and booster on 1-2 Aug except 3 Yashoda 6 K2 cleanup RFIDs; CBE Yashoda 6 K2 = schedule ET+TT booster on 2026-08-05/06; CPT G2P3 = no drive now, vaccines given on 20 Jul per PDF, but parse G2P3 separately and reconcile/import real tagged RFID mapping/history. |

## Small Prompt To Give Claude

Create an HTML dashboard from `docs/preventive-care-vaccination/CBE-CPT-VACCINATION-PROJECTION-2026-08-04.md`.

Use the doc's `Instructions For Claude HTML Visual` exactly. Build four sections:

1. Confirmed Drive Plan using the exact columns from `Confirmed Vaccination Projection Table`.
2. Resolved / No Drive Needed for cohorts already vaccinated.
3. Problem / Needs Reconciliation for unresolved cohorts, with clear amber/red treatment.
4. Code-backed projection rules, copied from `Code-Backed Projection Rules To Apply`.

Critical corrections to preserve exactly:

- CBE adult goats ET+TT booster: 414 total, 280 non-Sumathi done 1-2 Aug, 134 Sumathi pending on 5-6 Aug; sheds are Sumathi 1 Parts 1-8 and Sumathi 2 Parts 1-6.
- CBE normal tagged kids ET+TT booster: 242 done 1-2 Aug.
- CBE Yashoda 6 K2 cleanup ET+TT booster: only RFIDs `901007000504784`, `901007000504725`, `901007000504736`; schedule 5-6 Aug. Do not show 26 Aug for these ET+TT kids.
- CBE adult sheep Blue Tongue: 71 dose 1 on 5-6 Aug; 26 Aug is only the Aryaman-entered Blue Tongue booster date.
- CPT G2P3: no drive now; parse/reconcile separately. PDF says 39 scanned on 20 Jul 2026, 38 on record, 1 new; JSON still has 37 mock rows.
- CPT July 25 newborn `901007000504378`: Yashoda 3, ET+TT dose 1 due 15 Aug 2026, ET+TT booster due 5 Sep 2026 if dose 1 is given 15 Aug.

Preserve all counts, dates, animal IDs, source conflicts, and drive decisions exactly from the markdown. Do not merge unresolved rows into the confirmed drive plan. Make the UI clear that ET+TT dates are code-backed, while adult Blue Tongue +3-week booster rows are Aryaman-entered schedule inputs unless the GoatOS protocol config adds an adult BT W2 rule.
