# PC Care Module (Deworming / Ticks Removal / Hoof Trimming / Hair Trimming)

Status: accepted (maintainer decision 2026-08-21)
Owner: pc_director (module ownership), CEO (task planning)

## What it is

PC Care is a mobile-first assigned-task module (module_key `pc_care`, label "PC") under the
Preventive Care vertical, beside Vaccination. It carries four work categories, one bottom-bar tab
each: **Deworming**, **Ticks Removal**, **Hoof Trimming**, **Hair Trimming**.

```
CEO plans a task: category + park + shed(/pen) + business date + ONE OR MORE assigned operators
per animal the operators record mandatory live-camera videos, by capture mode:
  deworming / ticks_removal      -> SCAN-AND-RECORD (capture_mode scan_record), ONE video
                                    (slot: video): scan an RFID free-flow (tag stored VERBATIM,
                                    no herd lookup) and the recorder opens immediately — the
                                    2-second jobs
  hoof_trimming / hair_trimming  -> ROSTER-PICK (capture_mode roster_pick), THREE videos
                                    (slots: before_video, during_video ~10s, after_video): the
                                    screen lists the pen's resident RFIDs (GET .../roster,
                                    read-only) and tapping one walks that animal's next missing
                                    clip (the first tap records the same free-flow scan first)
any assignee may fill any missing slot on any scanned animal ("Captured by X" attribution)
any assignee submits the WHOLE task once every animal's slot set is complete
submit -> pending_verification -> ONE verification item per task (all clips)
verifier approve -> completed (pc_care.task.completed emitted HERE only)
verifier reject  -> rework (re-record on the SAME animal rows, resubmit)
```

## Locked rules

1. **Planning is CEO-only** (`pc_care.plan`, the weighing.plan precedent). pc_director holds
   monitor/oversee/execute, never plan. CEO holds plan+monitor, never execute.
2. **Multi-operator assignment is per task** (`pc_care_task_assignees`), deliberately unlike
   weighing's one-operator-per-bucket. `pc_care.execute` alone NEVER authorizes a write — the
   caller must also be an assignee of that task (403 `task_not_assigned`).
3. **Scan is free-flow verbatim.** The tag is stored as scanned, never resolved against the herd.
   The ONE business rule: no duplicate tag in the same task, for the task's whole life — the
   dedup index is deliberately NOT partial on `submitted_at` (weighing 000073 contrast): a PC
   task is one-shot, rework re-records on the same animal row.
4. **Slots are parallel and backend-owned.** The task detail contract carries `expected_slots[]`
   (field_key/label/min_duration_hint_seconds); clients iterate it and never hardcode a
   category→slot map. No slot gates another; any assignee fills any slot. The ~10 s "during"
   hint is recorder guidance, never a client-enforced cap. Pre-submit, a slot re-record REPLACES
   the clip (the phone retires the old clip only after the new one is SYNCED).
5. **Submit grain = the whole task.** Refused while any scanned animal misses a slot
   (422 `proof_incomplete`) or no animal is scanned (422 `no_animals`). Submit locks the task
   (`pending_verification`) for every assignee.
6. **One verification item per task**, module `pc_care`, categories `pc_deworming` /
   `pc_ticks_removal` / `pc_hoof_trimming` / `pc_hair_trimming`, ref_type `pc_care_task`. The
   verifier gets ONE Verify tab (categories are queue page filters). Media refs are the animals'
   clips in scan-then-slot order; every clip's burned-in overlay carries the tag, operator and
   time. Enqueue is keyed `pc-care-verification:<task_id>:<row_version>` — a rework re-submit
   mints a fresh item, a retry collapses onto one.
7. **Two orthogonal state columns** on `pc_care_tasks`: `work_state` (kernel:
   scheduled|delayed|completed|closed|canceled, immutable `planned_business_date`, rolling
   `due_business_date` via the kernel-worker roll-forward) and `status` (gate:
   open|pending_verification|completed|rework). Verifier approval flips BOTH to completed in one
   transaction and is the ONLY producer of `pc_care.task.completed`.
8. **pc_director owns the module**: leadership notifications (`pendingModuleProfiles["pc_care"]`),
   escalation wording, and the verify duty (`position_module_duties.module_code = "pc_care"`,
   derived automatically from the notification profile list).

## Where things live

- Migrations: `000183_pc_care_tasks.sql` (three tables + outbox trigger branch),
  `000184_pc_care_module_grants.sql` (preventive_care → pc_care).
- Backend: `backend/internal/pccare/**` (domain / ports / app / adapters
  http|postgres|proof|verificationbridge), kernel stage
  `backend/internal/kernelstages/pc_care_kernel.go`.
- Registration chain: `permissions/permissions.go` + `routes.go`, `bootstrap/api.go` (four
  RegisterCategory calls + enqueuer/validator wiring), `eventwiring/appliers.go` (verdict
  consumer, the single registration point), `notificationbridge/verification_notify_consumer.go`,
  `workforce/app/bootstrap_copy.go` (module + 4 tabs + planner tab + locales),
  `cmd/seed-roster-real` department defaults.
- Events: `pc_care.task.completed` in `context/architecture/domain-event-registry.json`.
- Proof: existing `/app/proofs` pipeline; every slot ref must resolve to a completed,
  tenant-owned, live-camera VIDEO (mime-aware, `pccare/adapters/proof`).
- Pinned tests: `pccare/adapters/postgres/pc_care_verification_integration_test.go` (full
  lifecycle vs the real schema incl. outbox parity), `pccare/app/service_test.go` (assignee
  gate, fail-closed enqueuer, row_version-keyed enqueue, verdict filtering),
  `eventwiring/appliers_test.go` (eight appliers), workforce bootstrap nav tests.

## Superseded in part (2026-09-02): vaccine stock is director-approved

The `inventory_vaccine` stock check no longer travels to the tenant verifier and is no longer
recorded by the PC Director. Park operators record the fridge proof and the PC Director
approves/rejects it on the module's own stock-verdict route (`pc_care.stock_approve`,
pc_director only — the toxin approval-gate shape). The four work categories above are
unchanged. Canonical prose: `docs/decisions/pc-care-vaccine-stock-director-gate.md`;
migration `000241_pc_care_stock_director_gate.sql`.
