# Frontend business-setting audit

Source: `origin/main` e06d27bf600b4d5d7c46d178d9341a5f6c942a6a, read through Git objects on 2026-09-16. Scanned all `apps/admin-web/features` using constants/defaults, form limits, comparisons and model searches; 2,400 broad candidate lines, then 706 focused candidate lines. Manually inspected the business-relevant definitions below. This is a source inventory, not a claim every literal or runtime route was exhaustively certified. Backend and real staging companions establish storage/precedence. Technical timeouts, polling, pagination and visual dimensions are excluded.

| Area | Value/rule found | Current meaning / proposed owner | Source |
|---|---|---|---|
| Sales readiness | Base35kg minus tolerance grams /1000; slider step50g, max comes from server | Hardcoded base in UI; tolerance is URL parameter `sale_ready_tolerance_g`, not evidence of a persisted company rule. Sales Config should own the proposed shared rule. | `features/procurement/sales-ready-tolerance-control.tsx:20–21,44–45,70–72`; `sales-farm-value.tsx:200` |
| Sales valuation grouping | Sex breakdown for fattening,K0,K1,K2,K3 | Code-owned presentation group mapping; ensure follows authoritative Sales categories when changing configuration. | `features/procurement/sales-farm-value.tsx:41` |
| Sales records | Maximum20 deal lines | Mirrors backend MaxDealLines; bounded transaction rule, not ₹/kg pricing. Keep aligned if made configurable. | `features/procurement/sales-format.ts:193–194` |
| Sales expected date | Date-picker maximum today+60days | Form rule; Sales owner, validate against backend before treating as business policy. | `features/procurement/sales-record-drawer.tsx:274` |
| Market analytics | Windows30/90/180/365days; default90 | Reporting defaults, separate from sale eligibility and rates. | `features/procurement/market-analytics.tsx:20–21` |
| Feed day | Next-day lead1day | Explicit business workflow assumption: directed/packed day before feeding. Feed schedule owner; cannot change UI alone. | `features/feed/feed-scope.ts:24–33` |
| Feed history | Packing history30days; analytics30/61/92days | Reporting windows, not ration rules. | `features/feed/feed-scope.ts:143`; `feed-analytics.tsx:74` |
| Feed stock display | Concentrate excluded from stock cards | Code-owned visibility rule; not an absent catalogue item or zero stock. | `features/feed/feed-analytics.tsx:1501` |
| Vaccination capacity | Initial/fallback200animals/day; initial operator count1 | Server-backed setting with client fallback, not solely hardcoded. Existing staffing editor owns it. | `features/people/vaccination-operators-screen.tsx:122–123`; `vaccination-operators-scope.ts:114` |
| Individual vaccination cap | Input1–200animals/day | Client-side bound despite common cap being configurable. Needs matching server validation; not a new universal200 limit. | `features/people/vaccination-operators-screen.tsx:905–907` |
| New vaccine defaults | Killed,bacterial,both species,all purposes,single course; first dose84days, booster21, repeat365, late14, repeats=true | Prefilled new-entry values, editable then persisted by specialized plan. Do not apply to all existing vaccines or call these clinical recommendations. | `features/vaccination-plan/plan-editor.tsx:676–687` |
| Vaccine spacing copy | Live/live28days, live/killed14, killed/killed14 | Hardcoded explanation alongside plan-owned rules; copy could drift when resolver/policy changes. Clinical owner required. | `features/vaccination-plan/plan-editor.tsx:702–705` |
| Duration conversion | week7days, month30, year365 | Representation semantics used by plan duration widgets; not an arbitrary farm preference. | `features/vaccination-plan/duration-field.tsx:20`; `duration-format.ts:1` |
| Vaccine schedule view | Minimum year2025 | Reporting/calendar bound. | `features/preventive-care-vaccination/full-vaccine-schedule.tsx:25` |
| Weighing proof structure | Maximum8removal proof slots; lump-sum ceiling5videos | Code ceilings around editable SOP proof policy. Must change validator/mobile together, never global media cap. | `features/sops/weighing-model.ts:25,76` |
| Weighing old-document defaults | Required removal, required individual video; missing rolling days60; legacy dates2026-08-03 /2026-08-01 | Compatibility fallbacks; published SOP fields otherwise supply values. Preserve old document meaning when migrating. | `features/sops/weighing-model.ts:71–72,163–180` |
| Weighing report window | Start2026-08-03, minimum2026-07-05, lump lookback400days; load all-time floor2024-01-01 | Hardcoded report boundaries; distinct from operational task dates. | `features/weighing/landing-window-constants.ts:11–19`; `weights-analytics.tsx:80` |
| Inspection proof | Max-files editor restricted1–10 | Per-field authored value bounded by code; retain feature-specific proof ownership. | `features/sops/inspection-editor.tsx:498` |
| Follow-up draft defaults | Interval240minutes,count10,days1,zero proofcounts,immediate schedule | Defaults for a new editable step, not all saved SOP schedules. | `features/sops/followup-model.ts:96–110` |
| Follow-up editor bounds | Proof count0–10, repeat count1–100 | Bounded SOP authoring contract; not generic configuration inventory. | `features/sops/followup-editor.tsx:337–341,392,404`; `followup-model.ts:363,379` |
| Follow-up engine kinds | weigh,tag,record_pen,feed_colostrum,death_evidence,return_to_pen | Engine-bound step identities deliberately protected; configurable wording/schedule does not permit arbitrary side effects. | `features/sops/followup-model.ts:61–64` |
| Herd identity vocabulary | Female/male; goat/sheep; birth/procured/imported | Contract enums rather than editable display items. A taxonomy UI cannot add a new executable species without backend/mobile support. | `features/counts/herd-actions.ts:34–36` |
| Health/Herd analytics | Health default6months; herd12months; earliest2026-08-01 and max1150days | Report defaults/bounds, separate from diagnosis and treatment policy. | `features/health/health-analytics.tsx:67–75`; `features/counts/herd-analytics.tsx:46–54` |

## Already server/config driven, not missing configuration

Feed editor accepts ration quantities, multipliers, experiment category, item identity and schedule values; backend supplies packing variance decisions (`exceeds_tolerance`). Alerts controls take min/max bounds from backend. Health Config edits versioned protocol rows. Weighing SOP parses persisted form fields; follow-up editor round-trips persisted schedule/proof definitions. Price values in sale lines are transaction inputs. These must not be replaced with generic local defaults merely because source contains a fallback.

Current source and real staging are different states: Alerts source exists while inspected staging remains migration317 without Alerts tables. See `feature-config-value-audit.md` for actual values and two vaccination resolver questions.
