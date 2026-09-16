# Department flow mapping: existing contracts versus proposed composition

Source: current mainaa057776567c44cb8941974d723aa6ee1efd62b9, inspected with Git objects on2026-09-16. Source evidence is not deployed-state evidence; parent owns real staging/live UI inspection. The latest100→70→subset and15holding examples are user-requested scenarios, not measured records or existing transition guarantees.

## Procurement: two existing domains must not be conflated

**Animal purchases** (`backend/internal/animalpurchase/domain/types.go:4–7`) explicitly stops at CEO accept/reject: an accepted candidate is not a goat, has no RFID and joins no procurement_load. Candidate decisions are pending/accepted/rejected (`:28–30`), with whole-load total/pending/accepted/rejected counts (`:114–120`). Candidate carries questionnaire version, answers/media, field verdict, height/temperature, decision/note and row version (`:165–178`).

The review UI renders recorded answers and proof slots from the backend, including reject-signal annotations and separate field recommendation (`apps/admin-web/features/procurement/animal-purchase-sop.tsx:5–13,47–54`). Accept/Reject applies only to pending rows with backend decide capability; Reject needs a note, writes use idempotency and row version (`animal-purchase-decision-form.tsx:3–14,61–86`). The repository decision transaction is version-fenced, audited and emits outbox effects after commit (`backend/internal/animalpurchase/adapters/postgres/repository.go:695–728`). This is not a generic count-decrement action.

**Source-entry procurement** separately has holding stays, health checks, staged decisions, transit handoffs, arrival reviews and PC handoffs. `backend/internal/procurement/domain/types.go:10–39,100–162,166–183,186–263`. It represents source candidates, predispatch accepted/rejected/deferred/blocked states, warm-up dates/days, loaded/arrived counts and per-goat arrival states. The existence of these contracts does not prove the newer animal-purchase candidate is connected to them. That bridge is explicitly later work in the animalpurchase domain comment.

### Actual questionnaire source

Current repository seed `backend/internal/animalpurchase/domain/inspectionseed/animal_purchase.json` has **5 load questions** (load_ref, vendor, farm, expected_count, notes) and38 animal questions across identity13, face6, body8, udder8 and decision3. The prior reconciled mock baseline has7 load questions based on earlier live UI; do not silently claim its exact45 questions are the current repository seed or a fresh live export.

- Identity: species, vendor goat ID, well_fed, teeth and teeth media, sex, conditionally pregnant, weight/weight media, height, rectal temperature/temperature media and animal media.
- Face/body: observations, suspicious media and optional Other descriptions. Preserve question IDs, option values, only_if conditions, source instructions, units and per-slot proof kinds/limits.
- Udder/sex-specific: udder media, milk yield, udder state, lactating, mastitis, teats, discharge and scrotum measurement with stored visibility rules.
- Decision: field_verdict, breed and notes. This buying-desk recommendation remains separate from the CEO decision.

These are source form fields, not medical recommendations. No new reject threshold, diagnosis, drug, vaccine or dose is inferred here.

### User's proposed flow

| Scenario | Existing support | Proposed/unknown |
|---|---|---|
| Inspect100, select70 | Candidate records, per-candidate answers/recommendation and CEO accept/reject counts exist. | The numbers are an example; distinguish inspected/recommended/approved counts and identity sets. |
| Reinspect a subset | Existing versioned forms and source-health checks supply useful primitives. | No explicit reinspection-attempt/subset transition contract was established by the bounded current-source search. Preserve attempt identity/history rather than overwrite first answers. |
| Send15 to holding | Separate procurement HoldingStay tracks dates/status/health/ownership and warm-up days. | Candidate promotion, subset selection, source/holding identity bridge and quantity reconciliation need explicit mapping.15 is not a current fixed policy. |
| Master Animal procurement→Transit→Warm-up | Existing stages, task prerequisites and domain handoffs provide components. | Selecting/pinning child SOPs and enforcing nested execution is a proposed generic composition contract, not existing free-text automation. |

## Vaccination / Preventive Care

Current plan editor preserves unknown stored rule_dsl fields and edits only owned fields; it must not invent defaults. `apps/admin-web/features/vaccination-plan/editor-model.ts:1–15`. Known configuration includes vaccine code/name/class/disease, enabled schedule, kid/drive dose offsets/triggers, late window, repeat identity, new-row pathogen/course/species/procurement-purpose and scoped anchors (`:19–89`). Capacity, pregnancy policy, compatibility gaps, dose amounts, vial sizes and route are specifically preserved even if this editor does not render them.

New-vaccine input supports live/killed type, bacterial/viral class, goat/sheep/both, purpose, first-dose age, booster gap, repeat and late allowance. These are schema options, not permission to invent values. Existing stored protocol/rule owner and publish/version validators remain authoritative. A generic child SOP may refer to a selected published vaccination procedure; it cannot replace eligibility/schedule/dose/stock/verification rules with an untyped task.

Parent's earlier real staging observations: vaccination matrixv9 and capacity200 tenant/buffer7/maxshots3. Latest screenshot-specific vaccination values must be recorded by the live reviewer; this source inspection does not verify those screenshot details. Any unclear spoken term, including the reported uncertain “chocolate,” remains unresolved; do not substitute a disease or vaccine name.

## Health

Health Config authors separate disease×adult/kid protocols and draft/publish/retire lifecycle; active cases pin their original version. `apps/admin-web/features/health/health-config.tsx:20–37`. Protocol steps carry medicine name, route, dose text/denominator and actions; domain duration bounds/defaults are structural constraints, not one universal treatment (`backend/internal/health/domain/types.go:13–14,53–75`). A catalogue medicine identity is distinct from the clinical course. Nested examples should reference existing approved procedures or use neutral instruction names, never fabricate a clinical regimen.

## Sales

Current frontend sales surfaces live under `apps/admin-web/features/procurement/sales-*.tsx`, including config, record drawer, farm value, loads and analytics. Earlier verified mapping remains: Sales Config is transaction-register context, not one universal eligibility editor; realized revenue/weight is derived, while farm valuation constants are separate from actual transactions. `backend/internal/sales/domain/overview_build.go:134–149` and `adapters/postgres/overview_repository.go:617–625` remain unchanged since the prior source snapshot. The35kg reporting threshold belongs to Weighing and is not proof of a blanket sales ban. A generic Sales master SOP is a new authoring example, not an existing sale execution contract.

## Cross-department composition contract

Use stable procedure/version IDs, per-step selected subject sets and explicit prerequisites. Published child procedures, their item/config snapshots and real subject identities must remain pinned. Counts alone cannot authorize transfer of an unspecified subset. “Started,” “operator recorded,” “verified,” “completed” and “accepted” are distinct states: current birth sequencing already demonstrates template-specific interpretation. Existing domain APIs/permissions, idempotency, row versions and audit/outbox effects remain authoritative; no new live writes are performed by this mock.

## Later live publication and state supersede seed assumptions

Parent independently exported real staging published procurement v7 to procurement-published-v7.json and exact aggregate readback to staging-feature-readback.txt at 19:55 IST. The served form has **7 load questions and 40 animal questions** (Identity 15, Face 6, Body 8, Udder 8, Decision 3). The source seed described above remains a different, older reference; neither it nor the old mock’s 38 questions should replace the current publication. There are 62 candidates (60 accepted, 2 pending), separate 8 procurement loads/348 accepted-herd-intake goats, and zero observed holding/transit/arrival/HF-vaccine/PC-handoff records. Health has 54 published versions across 27 disease keys and 1,012 steps, with zero cases. Vaccination v9 is published and v10 draft. These counts prove observed data, not complete execution or the candidate-to-herd bridge.
