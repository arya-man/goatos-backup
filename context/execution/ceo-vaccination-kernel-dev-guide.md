# CEO Guide - Goat OS Vaccination Kernel Dev

Status: Goal 1 local closure guide. Google dev rollout, clean-slate seed, and
Google E2E remain Goal 2 and must not be claimed here.

Audience: CEO, COO, and internal Mesha reviewers.

Purpose: show whether the vaccination process is set, whether it is being
followed, and where to look when it breaks.

## What This Dev Dashboard Proves

Goat OS is not trying to copy every old dashboard row into a new screen. The
dev dashboard proves the operating process:

```text
published vaccination config
  -> goat/shed cohort is evaluated
  -> due or deferred work is created
  -> shed-wise execution happens through SOP/proof
  -> verifier accepts, rejects, or asks for rework
  -> stock, booster, missed, reminder, escalation, and read models update
```

The old dashboard was useful for counts and a vaccination-by-shed matrix. Goat
OS keeps that visibility, but the main question changes from "what was entered"
to "is the process being followed, and who owns the next action?"

## Login

Dev access is limited to the approved four-user Mesha CEO/COO/internal-admin
allowlist defined in `infra/envs/dev/config.md`.

Access requires both Google sign-in and a Goat OS database grant. Google sign-in
alone is not enough.

Final dev URL: `https://dev.dashboard.mesha.sg/`

This is the same dev hostname reviewers are already used to. During rollout the
old dashboard must be archived and kept reachable as rollback evidence, then
the new Goat OS dashboard must render on this same URL. Raw Cloud Run,
Firebase, or hosted.app URLs are diagnostic links only; they are not the final
CEO link.

## What Data Is In Dev

The dev environment should be a clean slate except for the approved login
grants. It should then be seeded with a small source-backed sample, not the full
legacy herd.

Target seed size: `<=500` goats, enough to show pagination and process states.

The sample should include:

- multiple parks and sheds.
- adult goats, kids, bucks, does, and representative breeds.
- goats with known DOB and missing DOB.
- due, missed, deferred, blocked, proof-pending, verification-pending, and
  completed vaccination states.
- stock available and stock blocked examples.
- at least one shift, exit, or recovery example if source data supports it.

Seed source summary: Goal 1 local proof uses the source-derived ET dev baseline
seed plus a four-goat procurement matrix. Final Google dev seed source remains a
Goal 2 task.

Seeded count: local proof uses one vaccination chain goat plus four procurement
matrix goats in a throwaway local DB. Final Google dev seed count remains a
Goal 2 task.

## Where To Look

| Need | Where to look in Goat OS |
| --- | --- |
| Overall health of the vaccination process | Control Tower |
| Work due now or broken now | Action Center |
| Date-based view of due, missed, and upcoming work | Calendar |
| Whether the protocol is being followed by park/shed/rule | Protocol Adherence |
| Shed execution and operator work status | PHC -> Vaccination / Vaccination Execution |
| One goat's history and open obligations | Goat Passport / goat detail |
| Real vaccination and SOP configuration | Config / Protocol Rules and SOP Library |
| Worker, outbox, retry, and DLQ problems | Operations / Kernel Health / DLQ |

## How A New Goat Triggers Vaccination Work

1. A goat is added with identity, location/shed, lifecycle status, stage, DOB or
   entry date where available.
2. Goat OS records a canonical `goat.created` event.
3. The outbox relay and domain-event consumer deliver that event.
4. The vaccination generator checks the published PHC vaccination rules.
5. If the goat is eligible, an obligation is created as scheduled/due.
6. If the goat is sick, ICU, quarantine, too young, missing DOB, or missing a
   required date and the rule says to defer, the obligation is visible as
   deferred instead of disappearing.
7. The sweeper groups due work by shed and creates execution work.
8. The operator follows SOP/proof, the verifier accepts or rejects, and the
   kernel updates completion, stock, booster, reminders, escalation, and read
   models.

Final tested add-goat path: local Goal 1 uses the admin/API path exercised by
`tools/dev/vaccination-chain-proof.sh` and the accepted-intake path exercised by
`tools/dev/procurement-vaccination-e2e-matrix.sh`, both called from
`tools/dev/admin-web-e2e-smoke.sh`.

If the add-goat screen is built in dev, it must be modern, mock-aligned,
backend-contract-owned, usable by the target reviewer, and E2E-tested when it is
part of the vaccination acceptance path. If it is not built, say it is
unavailable here and name the tested import/API/admin path used for E2E proof.

## How A New Shed Affects Vaccination Work

A shed by itself does not create vaccination work. Vaccination work is created
when goats in that shed match a published vaccination rule.

Use this to test shed behavior:

1. Add or seed a shed/location.
2. Add or move eligible goats into that shed.
3. Run or wait for the generator/sweeper.
4. Check PHC -> Vaccination / Vaccination Execution for shed-wise work.
5. Check Action Center and Calendar for due, deferred, missed, or blocked rows.

Final tested add-shed or move-shed path: local Goal 1 does not create sheds from
the CEO UI. It uses seeded source locations and proves shed-scoped vaccination
work through the local E2E smoke report.

If the add-shed or move-shed UI is built in dev, it must be modern,
mock-aligned, backend-contract-owned, usable by the target reviewer, and
E2E-tested when it is part of the vaccination acceptance path. If it is not
built, say it is unavailable here and name the tested seed/API/admin path used
for E2E proof.

## What Config And SOP Are Already Set

The production rule is: drafts do not create work. Only published versions
create work.

For dev, document the final seeded config here:

| Config item | Dev value |
| --- | --- |
| Vaccination protocol/version | Local source-derived ET baseline, version `b011`, rule `b012` |
| SOP version linked to vaccination | Local published vaccination SOP `b0..0002` |
| Proof required | Local proof tokens: shed, vial or lot, administration |
| Stock rule | FEFO vaccine lot required; cold-chain and expiry checked before acceptance |
| Reminder/escalation policy | Local sweeper/reminder/escalation paths verified by package tests and E2E smoke; production channels are Goal 2 |

## What Happens When SOP Or Vaccination Config Changes

- A published version is immutable. It is not edited in place.
- A change creates a new draft version.
- CEO/COO publishes the new version when it is approved.
- New goats and future generation use the effective published version.
- Completed work remains tied to the old version for audit.
- Already-open work needs an explicit policy: continue, cancel, supersede, or
  regenerate. Goat OS must not silently rewrite completed history.

## How To See If The Process Is Broken

Use these questions:

- Is work due or missed? Check Action Center and Calendar.
- Is work blocked by stock? Check Vaccination Execution and stock-block state.
- Is proof missing or rejected? Check Vaccination Execution / Workflow detail.
- Is verification pending too long? Check Action Center and Protocol Adherence.
- Did an event or worker fail? Check Operations / Kernel Health / DLQ.
- Did an alert or escalation fire? Check reminder/escalation rows and dev-safe
  notification output.

If a deadline crosses, the work should become visible as missed/overdue and
escalation should remain visible until the issue is resolved.

## What Maps From The Legacy Dashboard

| Legacy dashboard habit | Goat OS place |
| --- | --- |
| Active goat count and herd cards | Control Tower plus Goat Passport/search |
| Import review and data-quality queues | Admin/Data Ops import surfaces, if enabled for this dev pass |
| Old vaccination shed matrix | PHC -> Vaccination, Calendar, and Protocol Adherence |
| Shed/date vaccination cells | Vaccination Execution and Calendar detail |
| Knowing whether process broke | Action Center, Workflow detail, Kernel Health, DLQ |
| CEO escalation visibility | Action Center, Calendar, Protocol Adherence, escalation/notification state |

The old dashboard can remain as archived evidence or rollback reference, but it
must not be treated as the new source of truth after Goat OS dev is accepted.

## Known Gaps To State Honestly

Update this list after Goal 2. Do not delete a gap unless the tested dev flow
proves it is closed.

- Full production vaccine schedules/rules may still require final PHC-approved
  data.
- Operator proof upload UI is either built as a modern, mock-aligned,
  backend-contract-owned, target-reviewer usable, E2E-tested flow or explicitly
  unavailable; if unavailable, the guide must name the tested admin/API helper
  used for E2E proof.
- Real notification channels may be dev-safe stubs, not production delivery.
- Stock issue resolution UI is either built as a modern, mock-aligned,
  backend-contract-owned, target-reviewer usable, E2E-tested owner flow or
  explicitly unavailable; visible stock-block state is still required.
- Critical death/ICU/quarantine guardrail flows may be blocked or limited until
  the critical-action pack is implemented.
- XLSX import may remain out of scope if CSV/import API is the tested seed path.

## What Google Dev Deployment Will Not Automatically Finish

When the system is live in Google, say this plainly: deployment proves Goat OS is
running in the right cloud environment, with the tested seed data, login,
workers, and dashboard path. It does not by itself mean every future operating
workflow is complete.

The following items need full workflow implementation before they should be
presented as live business processes:

| Area | Non-technical CEO wording |
| --- | --- |
| ICU and quarantine actions | Goat OS can safely stop unsafe ICU/quarantine changes today. The full approval workflow still needs the evidence checklist, reviewer steps, release criteria, and operator screen before it is a live workflow. |
| Critical health/death guardrails beyond the approved death path | The system blocks dangerous shortcuts. To make the full guardrail product live, we still need the complete policy pack that decides what evidence is required, who approves, and how exceptions are closed. |
| Shed owner separation checks | Some approvals depend on knowing the correct shed owner or manager. Before this is live, the owner data and reviewer separation rules must be populated and tested. |
| DLQ/replay operations | Goat OS has the repair screen concept and local recovery paths. In Google, we still need to prove failed background messages can be found, replayed, or discarded through the intended operator path. |
| Stock issue resolution | Stock blocks are visible. The full owner workflow still needs the person responsible, action buttons, proof of resolution, and escalation behavior tested in Google. |

Use this wording if asked whether these are "bugs":

```text
These are not hidden failures in the deployed vaccination kernel. The current
system blocks unsafe shortcuts. They are future operating workflows that need
their own evidence, approval, owner, and exception-handling screens before we
call them live.
```

## Final Acceptance Notes

Goal 1 local acceptance note:

```text
Goal 1 local kernel closure: local OCK/RVF closeout ledger complete; final state depends on verification and senior-review gates.
Google dev deployment: Goal 2, not started.
Old dashboard archive/rollback: Goal 2, not started.
Preserved users/grants: Goal 2, not started.
Seed source and count: local throwaway proof DB only; Google dev seed pending Goal 2.
Vaccination E2E result: `GOAL1-E2E-FINAL-20260629-022731` passed locally; report `.codex-goatos-render/e2e-smoke/GOAL1-E2E-FINAL-20260629-022731`.
Known incomplete flows: production PHC roster, production notification channels, critical ICU/quarantine guardrail workflows, shed-owner separation checks, full DLQ/replay operations, stock owner workflow, and Google dev deployment remain Goal 2/product work.
```
