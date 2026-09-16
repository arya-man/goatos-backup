# Real staging feature inventory

Read-only real GCP Cloud SQL target `goatos-stg:asia-south1:goatos-stg-core-db`, database `goatos`. Schema snapshot2026-09-16 19:54:43IST; focused counts19:55:38IST. Credentials obtained through Secret Manager and Auth Proxy, never printed. Every query has default_transaction_read_only on and15second timeout. No mutations.

## Coverage

`staging-schema-inventory.txt` contains the complete public base-table list, PostgreSQL estimated row counts, and all public columns/types. Estimated rows may be stale or -1; they are NOT proof that a table is empty. `staging-feature-readback.txt` has actual COUNT and bounded business-state aggregates for the paths below. `staging-data-plan.md` and `staging-database.md` cover the earlier detailed configuration/rules observations. This is not an assertion that every operational row or JSON document has been inspected.

| Feature family | Staging evidence | Meaning and verification boundary |
|---|---|---|
| Animal purchase inspection | 2 loads,62 candidates:60accepted/2pending; SOP procurement.animal_purchase publishedv7 | Separate load/candidate/decision tables. Accepted is an inspection decision, not proof of tagging or automatic herd promotion. |
| Procurement herd intake | 8 procurement_loads,348loadgoats allaccepted_herd_intake | Separate tagged-goat lineage with selection/currentstate, identifiers, purpose, warmup, timestamps and context. Does not prove a bridge from animal_purchase_candidates. |
| Seller holding / transit / arrival | source_holding_stays,transit_handoffs,arrival_intake_reviews tables exist; all0rows | Schema support exists. Empty observed data is not proof the API/UI is missing; source and live route evidence must decide implementation status. |
| Procurement vaccine/handoff | procurement_hf_vaccination_evidence and procurement_pc_handoffs exist;0rows | Integration primitives exist; actual published clinical requirements/dispatch need domain inspection. |
| Health |54published protocol versions across27diseasekeys;1012steps;0healthcases | Actual configured coverage differs from illustrative100–200diseases. Protocol steps and diagnosis-run schema exist; no current case volume can prove mobile execution. |
| Vaccination | vaccination.matrixv9published,v10draft,1–8retired; capacity200/day tenant,buffer7days,max3shots | Preserve published/draft distinction. Clinical DSL includes schedule, eligibility, recovery,pregnancy,misseddose,procurement andcompatibility. |
| Inventory |7active vaccine records,doseunits; separate stock/movements/requirements tables | A generic new Needle is a proposed catalogue item, not an existing stock row. Item creation must not fabricate stock. |
| Feed |17catalogue records (4active13retired), ration/effectivedate/schedule/experiment/conversion/packing/direction/transport/wastage tables | Multi-dimensional domain configuration. Familiar and farm/experimental feed remain distinct identities. Carry-quantity and blend policy require existing resolver or explicit proposal. |
| Weighing | observations/campaigns/workitems/removalproofs and publishedsessionv1 | Existing authored capture/removal requirements; 30/35 reporting vs saleeligibility still separate. |
| Counts / movement / births / death | countanchors/projections/approvals/readiness plus birth/location/identity tables; publishedbirth/death/reconcilev1,shiftingv2 | Existing workflow/follow_up requirements are domain-owned, not evidence of arbitrary nested SOPcalls. |
| Milk | milk preparation/feeding publishedv1 with workflow/formfields | Existing SOPowned execution. |
| Sales / markets | deals/lines/payments/tags/weightaudit,marketbenchmarks/observations/leads,calltime08 | Actual transactions/market observations/valuation assumptions must not be conflated. New dimensioned eligibility authoring needs source ownership check. |
| People / permissions / workforce | departments,designations,person/moduleaccess,grants/leave/roster tables in schema snapshot | Department item availability is different from actor authorization. Do not infer rolepower from a label. |
| Tasks / verification / work board | task,proof,verification,obligation,outbox,processed-event families in snapshot | Existing durable execution and review infrastructure must remain authoritative. No runtime latency/E2E certification from schema alone. |
| Herd signals / devices / observations | device/telemetry/derived families in full schema inventory | Existing code/frontends require separate inspection; not part of invented genericclinical policy. |
| Presentation config | admin_ui_config_entries0rows; familyrevision queues/tables exist | Displaycopy store is not arbitrarybusinessrules. |
| Alerts/currentnewmigrations | alert_rule_config andalert_event_rules absent; latestappliedmigration317 | Currentmain has newer features/migrations. Do not label their storage deployed based on code presence. |

## Exact current Procurement form

`procurement-published-v7.json` is a read-only export of the published staging SOP form/proof document, without candidate answers or personal data. Its inspection has7loadfields and40animalquestions in5pages:Identity15,Face6,Body8,Udder8,Decision3. The earlier prototype reference held38animalquestions and is stale. Preserve any edited local drafts; import the current reference explicitly rather than replacing user work.

## Limits that must remain visible

No production settings were saved. User examples15holdingdays,3/4travel,10/14warmup,3hourchecks,100offered/70selected are requirements/examples; this readback does not establish them as currentlivevalues. A schema, draftplan, sourcecommit and liveexecution are different evidence levels. The broad feature matrices must be joined before claiming a capability missing or implemented endtoend.
