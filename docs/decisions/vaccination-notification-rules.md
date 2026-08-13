# ADR: Vaccination notification rules — what to send, when, how often, to whom

Status: **PARTIALLY IMPLEMENTED compatibility catalog.** The D-7/D-6..D0
reminder cadence and a due-age escalation lane exist, but the production
escalation semantics, recipient resolution, and channels do not yet satisfy the
target described here. The accepted cross-module authority is
[`task-timing-alerting-violations-and-appeals.md`](./task-timing-alerting-violations-and-appeals.md).

Maintainer decision, 2026-08-10: a Vaccination drive date is a planning target,
not a personal hard deadline. D+1 and D+2 are an accepted carry-forward band.
That band is always capped by the earliest applicable animal-level clinical
latest-safe boundary. Crossing the drive date or its ordinary extension is not
an employee violation; an animal-level clinical breach is still only a breach
occurrence until attribution and appeal are complete.

Scope note: FCM is the intended named-person first channel. The current reminder
stage can queue FCM, while the legacy escalation lane uses local-stub at L1,
Slack at L2, and incident webhook behavior at L3/L4. SMS, WhatsApp, and voice are
not production channels. Email, Slack, webhook, FCM, and incident gateway cases
exist, but source alone does not prove a deployed provider/secret is live.

---

## 0. Current delivery topology

Goat OS does **not** rebuild SQS and does not use Cloud Scheduler, Cloud Tasks,
or a calendar projection table as the operational reminder authority. Current
production source uses:

| Concern | Current authority |
|---|---|
| Durable domain fan-out | Pub/Sub plus idempotent consumers |
| Future schedule and business clock | Canonical Postgres obligation/calendar facts |
| Reminder materialization | Consolidated `kernel-worker` reminder stage on its five-minute operational cadence |
| Notification queue/retry/lease | Postgres `notification_requests` |
| Named-device last hop | FCM behind `notification/ports.Gateway` |

Far-future “this vaccine is due in six weeks” state remains in Postgres. The
kernel stage reads canonical events/windows, materializes only the fires that are
currently due, and the notification dispatcher leases and sends those durable
requests. Cloud Tasks may be evaluated as a future transport optimization only
through a separate accepted change; it is not the current or required calendar.

---

## 1. Current runtime vs target

Already built (do not rebuild):

- `notification_requests` durable queue with lease/retry/DLQ — mig `000087`.
- `notification-dispatcher` worker + multi-channel gateway routing
  `slack | webhook | email | push_fcm | incident` — `internal/notification/...`.
- Obligation engine with the full status lifecycle + sweeper missed-marking.
- `calendar/domain/types.go:230 NotificationPolicy json.RawMessage` — the empty
  slot this doc's rule schema fills.
- Device registry `workforce_member_devices(push_token_hash, status, …)` +
  routes `POST /app/devices/{register,heartbeat,deregister}` — mig `000050`.
- Android `FirebaseMessagingService`, token registration/refresh, and logout
  deregistration plumbing.
- The five-minute Vaccination reminder cadence stage in the consolidated
  `kernel-worker`.

Deployment credentials, actual device reachability, and external provider
secrets remain deployment proof, not facts source code can establish.

Audited current behavior:

1. The reminder stage implements D-7 at 08:00, D-6..D-1 at
   08:00/13:00/20:30, and D0 at the same three slots, with 21:00-07:00 quiet
   hours and latest-fire catch-up.
2. The due-age escalation lane computes the highest crossed L1/L2/L3/L4 level
   (defaults approximately 0h/4h/24h/48h). It can skip levels and is not the
   required next-human-level-after-unacknowledged-wait state machine.
3. Acknowledging the current escalation row does not reliably stop later levels.
4. Escalation recipients may be role slugs rather than resolved on-duty people.
5. The ordinary obligation missed sweep defaults to +24h and late completion is
   still possible. That row is operational evidence, not a disciplinary finding.

Target work is owned by the operational-task-kernel plan: named owner and clock,
the D+1/D+2 flexible drive band with clinical cap, run/step/contact-attempt
state, acknowledgement fencing, one-level advancement, delivery-failure policy,
and attribution/appeal separation.

---

## 2. Vaccination lifecycle events (the triggers)

Obligation status machine (from `obligation/domain`):

```
scheduled ─▶ due ─▶ missed                       (deadline path)
                └─▶ in_progress ─▶ completed    (execution path)
   open ─▶ deferred ─▶ scheduled                (health block and reschedule)
   open ─▶ waived | canceled | superseded
```

`overdue` is a read-time classification for an open row whose clock has crossed;
it is not a persisted obligation status. Rescheduling is an action/reason that
returns the row to `scheduled`; `rescheduled` is not a persisted status.
Blocking is dependency/task context while the obligation remains in an allowed
persisted status; `blocked` is not an obligation status.

Current writer coverage is not universal: some transitions write a status event
without the matching outbox event. The target requires every governed transition
to persist its status/audit and outbox fact in the owning transaction, with a
writer-by-writer production-path closure gate before shared contacts activate.

Business events the vaccination slice raises, and whether they notify:

| Event | Trigger | Notifies? |
|---|---|---|
| Drive planned | `vaccination_shed_event` created (shed × vaccine × date) | yes — heads-up to the park |
| Obligation `scheduled → due` window opens | canonical Postgres clock evaluated by the consolidated five-minute kernel-worker stage | yes — reminder ladder starts |
| Reminder fire becomes due | consolidated kernel-worker reminder stage evaluates the canonical window | yes — the cadence in §3 |
| `due → in_progress` | field batch started | low-priority ack to head/manager |
| `→ proof_pending` | execution done, proof (SOP video) uploaded | no push (interim state) |
| `→ verification_pending` | shed proof/video submitted for review | **yes — notify verifier + park head; PC Director/CEO receive portfolio visibility, not routine per-shed CEO push** |
| `→ rejected` / `rework_due` | **verifier rejects the proof** | **yes — notify the operator who did it (+ park head)** so they redo |
| `→ completed` (verified/approved) | proof accepted (`vaccination_completion`) | rollup only (digest), no push spam |
| planned drive date crossed | drive remains open inside D+1/D+2 carry-forward and below every clinical cap | yes — lightweight owner reminder + manager aggregate; no incident/voice and no violation |
| clinical latest-safe boundary crossed | exact animal obligation remains open | yes — hard clinical breach/contact policy at animal grain; attribution is separate |
| legacy `→ missed` | current sweeper crosses its configured cutoff | operational alert/evidence only; never a final employee violation |
| `→ deferred` | health block (sick / quarantine / ICU / pregnancy window) | manager only |
| `deferred → scheduled` (reschedule action) | health recovery replanned | operator + manager |
| `→ canceled` | animal exit | none (audit only) |
| Cold-chain / stock buffer low | 3-week buffer trigger (PHC SLA) | manager + director |

---

## 3. The reminder cadence (the "1 week before + daily reminders" ask)

Default ladder for an **actionable vaccination obligation** with due date `D`
(all times **IST**, aligned to the documented daily rhythm — Park Head morning
plan + EOD report):

| Offset | Fires | Slot(s) | Type | Priority |
|---|---|---|---|---|
| `D − 7d` | 1× | 08:00 | `advance_notice` | normal |
| `D − 6d … D − 1d` | 3×/day | 08:00, 13:00, 20:30 | `reminder` (with `reminder_number` 1..N) | normal |
| `D − 0` (due day) | 3× | 08:00, 13:00, 20:30 | `due_today` | high |
| `D + 0` EOD not done | 1× | 20:30 | `carry_forward_start` | normal manager aggregate; not a breach |
| `D + 1 … D + 2` | 1×/day | 08:00 | `carry_forward` | normal; no incident/voice |

This is the literal encoding of "1 week before, then daily reminders till the day
arrives," aligned to the current field rhythm of 08:00 / 13:00 / 20:30 local
time. Every offset/slot/count is a **rule parameter**, not a constant — 3×/day
for a full week is deliberately high-touch for low-risk drives, so the ladder is
tunable per vaccine priority (e.g., ET+TT priority-1 keeps the full ladder;
priority-5 HS could collapse to `D-3` + `D-0`). Tuning lives in the rule, §5.

**Stop condition:** the daily reminder ladder applies only while a vaccination
shed is scheduled/open and not yet being executed. Once the shed reaches
`in_progress` because an operator starts scanning, no further daily reminder is
sent for that shed. Submitted/proof/review states are handled by their separate
submission and verification notification paths.

**Quiet hours:** no push between 21:00–07:00 IST; a fire that lands in quiet
hours defers to the next allowed slot (field staff, not on-call).

**Dedup / collapse:** if one operator has 5 sheds due the same day, they get
**one** batched push ("6 drives due today across K1, K2, Fattening-M"), not five.
Batching key = `(recipient, park, due_date, notification_type)`. This is the
mobile "never fetch/notify per-animal" rule applied to pushes.

**De-scheduling:** on completion, defer, cancellation, or a reschedule action,
suppress all pending future reminder/contact rows for the previous schedule.
A recovered obligation returned to `scheduled` starts a new version-fenced
ladder from its new `D`; stale workers cannot send the prior generation.

The drive extension must stop earlier for any animal whose pinned clinical
`latest_safe_at` is earlier than D+2. That animal becomes an exact clinical
exception; the remaining safe drive work does not inherit a personal violation.

---

## 4. Who gets what (audience & scoping)

Two audience classes, from the org model (COO → Director → Park Head →
Manager → Assistant/Operator) and the position table
(`workforce_positions.scope_type ∈ {tenant, center}`, `position_tier`,
`position_code`):

### 4a. Park-scoped roles — their park only

The park operational audience receives only its park's reminder ladder. Resolve
it through the workforce module-duty contract, never a literal role-code list:

```text
ResolveModuleDutyRecipientsBatch(
  module = "pc.vaccination",
  duties = ["execute", "manage"],
  scope = event.park_id,
  at = contact_time,
)
  -> on-duty member/replacement
  -> active reachable device
  -> raw fcm_token for the gateway
```

The shared-kernel equivalent must preserve module duty, park scope, shift time,
absence/replacement/week-off handling, real member/user identity, and explicit
no-route/no-recipient outcomes. `push_token_hash` is device identity/dedup data;
it is not the FCM delivery address.

Backup Manager: when the park's PHC Manager is absent (`workforce_absences`
approved, coverage assigned), the **Backup Manager covers that manager's
notifications** for the absence window — same park, same rules, routed to the
covering member. Ownership is never mutated; only the notification recipient is
swapped for the window. (Consistent with the Backup-Manager coverage model —
coverage of *tasks*, not a role swap.)

### 4b. HQ-tier roles — all parks

PC Director and CEOs get **all-park read visibility**, but read scope is not the
same as direct notification audience. Routine operator nudges remain
field-owned. The PC Director receives department-level exception aggregates and
owns intervention tasks. The CEO sees tenant-level/systemic summaries and is
contacted directly only for a ratified critical or unresolved systemic risk—not
for every shed, carry-forward, or proof handoff.

Leadership receives:

- PC Director 20:30 portfolio summaries while sheds remain open;
- PC Director follow-up tasks for aged review or repeated rework; and
- CEO aggregate/systemic exception summaries, with drill-down but no routine
  per-shed push.

Directors also get the **operational** ladder for department-wide events (stock
buffer low, biosecurity) per the PHC handbook SLAs, not per-drive reminders.

### 4c. Verification & rework — shed submit goes to reviewer + leadership

Verification is its own loop: after execution the proof (SOP video) goes
`proof_pending → verification_pending`, and the verifier either approves
(`→ completed`) or rejects (`→ rejected` / `rework_due`). Notifications follow
the **"notify the people who must know/action this shed is now waiting"** rule.
Recipients are resolved from the backend org model and active FCM devices, not
hard-coded in clients:

| Event | Who is notified | Why | Priority |
|---|---|---|---|
| `verification_pending` (shed proof/video submitted) | **verifier(s)** with `pc.vaccination` verify duty for that park and that park's **park head**; the **PC director** sees it on the portfolio board/aggregate | verifier can review and operational leadership can track the handoff without pushing every item to the CEO | normal, deduped by shed submission id per device |
| `rejected` / `rework_due` (verifier rejected) | the **operator who performed it** + that park's **park head** | they must redo the drive — this is the one verification event that must reach the field fast | high |
| `→ completed` (approved) | no push | success is the default; shows in the daily rollup only | — |

For `verification_pending`, the event key is submission-scoped when the producer
carries a `submission_id`. That matters because one shed submission can create
multiple goat-level verification items; the notification layer must queue one
push per recipient device for the shed submission, not one push per goat.

Leadership also receives an individual escalation when the loop breaks:

- a proof sits in `verification_pending` past its review SLA (aging — no verifier
  acted), or
- the same drive is rejected repeatedly (rework loop — e.g. ≥ N rejections), or
- `vaccination_config_activation_review` / policy-level approvals that genuinely
  need a leader's sign-off.

So shed submit → verifier + park head, with PC Director portfolio visibility. A
normal reject → operator + park head. A *stuck* or *looping* verification may
create a Director-owned follow-up. CEO receives aggregate/systemic exception
visibility, not one push per shed submission.

### 4d. Notification tap landing

Vaccination push taps do **not** open Scan. Scan is an operator-initiated action
from the Vaccination sheds list only.

| Resolved recipient | Reminder tap | Shed-submitted tap |
|---|---|---|
| `pc.vaccination` `execute`/`manage` duty holder | Vaccination | Vaccination |
| `pc.vaccination` `verify` duty holder | Vaccination | Verify item detail |
| `pc_director`, `ceo_internal` | Vaccination tab | Vaccination tab |

---

## 5. The reusable rule framework (how future features auto-derive notifs)

Every obligation-backed feature declares a versioned **NotificationPolicy** for
its governed events. The shared task/contact engine snapshots that policy on the
task/contact run, resolves the named on-duty audience and cadence, and writes
durable `notification_requests`/contact-attempt rows when each step is due. A
new feature ships a versioned policy and adapter, not private dispatch code.
Calendar projections and queue schedule times are never the policy authority.
Until the versioned policy registry and shared contact engine land, the current
Vaccination cadence remains compatibility behavior governed by the cutover
requirements in §7.

Declarative shape (vaccination example):

```yaml
notification_policy:
  event_type: obligation.vaccination
  slice_key: vaccination
  # cadence ladder — every value is a knob
  ladder:
    - trigger: due_window_open        # obligation scheduled -> due
      fires:
        - { offset: -7d, at: ["08:00"], type: advance_notice, priority: normal }
        - { offset: [-6d,-1d], at: ["08:00","13:00","20:30"], type: reminder, priority: normal }
        - { offset: 0d, at: ["08:00","13:00","20:30"], type: due_today, priority: high }
  quiet_hours: { from: "21:00", to: "07:00", tz: Asia/Kolkata }
  batch_key: [recipient, park_id, due_date, type]   # collapse per-animal spam
  # audience
  audience:
    - module: pc.vaccination
      duties: [execute, manage]
      scope: { by: park, from: event.park_id }
      effective_at: contact_time
      delivery_address: active_device.fcm_token
  portfolio_visibility:
    - { roles: [pc_director, ceo_internal], scope: tenant }
  direct_leadership_contact:
    pc_director: [aged_review, repeated_rework, clinical_breach, systemic_capacity]
    ceo_internal: [critical_unacknowledged, systemic_cross_park_risk]
  # verification loop — shed submit is backend-owned and routed by capability/position
  verification:
    - on: verification_pending
      to:
        - { module: pc.vaccination, duties: [verify], scope: { by: park, from: event.park_id } }
        - { module: pc.vaccination, duties: [manage], scope: { by: park, from: event.park_id } }
      portfolio_visibility: [pc_director, ceo_internal]
      direct_ceo_contact: false
      priority: normal
      idempotency: submission_id_per_device
    - on: [rejected, rework_due]
      to: { actor: event.performed_by, plus_module_duties: [manage], scope: park }
      priority: high
    - on: completed                                   # approved
      to: none                                        # digest rollup only
    # leadership sees verification ONLY as aggregate + these exceptions:
    escalate_to_leadership_when:
      - verification_pending aged past review_sla
      - rework_loop: rejections >= 3 on same drive
      - config_activation_review                      # policy-level sign-off
  # Drive date is flexible through D+2; only an exact clinical boundary is hard.
  flexible_carry_forward:
    through: D+2
    contacts: [owner_push, manager_digest]
    forbidden_channels: [incident, voice]
    never_employee_violation: true
  # Hard clinical escalation runs at exact animal obligation grain.
  escalation:
    trigger: animal_clinical_latest_safe_breached
    advance: exactly_one_human_level_after_unacknowledged_wait
    stop_contacts_on_ack: true
    attribution_and_appeal_required_for_violation: true
  channels: [push_fcm]        # roadmap: sms, email, whatsapp, slack, voice (§6)
  per_priority_override:      # tune the ladder by vaccine priority (Vaccination Rules table)
    "1": {}                            # ET+TT — full ladder
    "5": { ladder_offsets: [-3d, 0d] } # HS/FMD — collapsed
```

Framework guarantees, feature-agnostic:

- **Idempotent** — a reminder fire keyed by `(obligation_id, offset_slot)` is
  written once; replays are no-ops (rides the outbox idempotency key).
- **Self-healing** — sweeper re-derives today's fires from Postgres state, so a
  crashed dispatcher or missed tick auto-recovers.
- **Target escalation is state, not spam** — the shared kernel's versioned
  run/step/contact-attempt model advances one human level at a time and fences
  every future send after acknowledgement. The current
  `obligation_escalations` implementation does not yet meet that statement.
- **Scope resolution is one query** — role × park via `workforce_positions`;
  reused verbatim by feeding/breeding/etc. by swapping `position_code` + `roles`.

To add a future feature's notifications you write a `notification_policy` block
and (if new roles) extend the audience role list. No new dispatch, queue, retry,
or channel code.

---

## 6. Channel roadmap (FCM now; the rest documented)

The gateway (`internal/notification/adapters/gateway/gateway.go`) already routes
by `domain.Request.Channel`. Adding a channel = a new gateway case + a policy
listing it. Channel is chosen by **priority × recipient reachability**, not
per-feature.

| Channel | `Channel` key | Status | Use for | Notes / seam |
|---|---|---|---|---|
| **FCM push** | `push_fcm` | gateway + reminder request path exist; deployed credential/device reachability must be proven | named-person reminders, contacts, digest | provider configuration and device resolution are deployment facts, not source-code assumptions |
| **Slack** | `slack` | gateway case exists; legacy L2 escalation selects it | shared operations mirror/digest | it is not proof that the named owner was contacted or that a live webhook is configured |
| **Email** | `email` | gateway case exists | leadership digest and formal case copy where configured | deployed webhook/auth/recipient configuration is unproven from source |
| **SMS** | `sms` | **document only** | critical/overdue for staff with no app or poor connectivity (field) | needs an SMS provider gateway case (e.g. via a webhook to an India SMS/DLT-registered sender); DLT template registration required in India |
| **WhatsApp** | `whatsapp` | **document only** | Director-level comms + reports (per handbooks) | needs WhatsApp Business API (template/HSM approval); handbook-mandated channel for leadership |
| **Voice / IVR call** | `voice` | **document only** | last-resort critical: biosecurity breach, death cluster, disease suspicion (PHC 4h escalation to leadership) | not in any doc today; add only for `priority: critical` escalations that go unacked; needs a telephony provider case |

Channel-selection policy (recommended default):

- `normal` reminders → `push_fcm` (+ in-app feed).
- Vaccination `carry_forward` through D+2 → in-app/`push_fcm` plus manager
  aggregate only; no incident, SMS, or voice merely because the drive moved.
- `critical` clinical escalation (animal latest-safe breach, biosecurity) →
  `push_fcm` and configured incident/operations channel; future voice only if
  still unacknowledged and the provider/policy is actually activated.
- leadership `digest` → `push_fcm` + `email` (+ `whatsapp` for directors later).

Everything except the last hop is channel-agnostic: the same
`notification_requests` row can be re-targeted to any channel by the policy, so
enabling SMS/WhatsApp/voice later is additive — a gateway case + a policy edit, no
pipeline change.

---

## 7. Remaining closure order

Device lifecycle handlers, the Android FCM service, durable notification queue,
dispatcher, gateway switch, and five-minute reminder stage are built. Do not
rebuild them. Close the remaining gap in this order:

1. **Canonical transition coverage** — inventory every obligation/proof/verdict
   writer and add the missing same-transaction status/audit/outbox facts.
2. **Named-person resolution** — resolve the real on-duty owner, replacement,
   verifier, and manager before queuing a contact; persist explicit no-route and
   no-recipient outcomes.
3. **Run/step/contact state** — replace highest-crossed due-age escalation with
   acknowledgement-fenced, exactly-one-human-level advancement and durable
   delivery-failure behavior.
4. **Audience cutover** — shadow and then suppress the current D0 20:30 direct
   PC Director/CEO reminder and other per-item leadership fan-out. Replace it
   with Director-owned interventions and CEO aggregate/systemic exceptions;
   update the production tests that currently require the old audiences.
5. **Deployment certification** — prove Firebase/project credentials, device
   reachability, retry/DLQ, notification tap, and real staging delivery at the
   exact deployed revision. Source configuration alone is not proof.
6. **Optional future channels** — add SMS, WhatsApp, or voice only through an
   accepted provider/policy change with consent, regional, delivery-receipt,
   retry, and acknowledgement proof.

---

## References

- [`notification-delivery.md`](./notification-delivery.md) — the GCP/SQS pipeline (built).
- [`fcm-device-lifecycle.md`](./fcm-device-lifecycle.md) — token couple/decouple
  contract; backend and mobile plumbing exist, while deployed credential/device
  reachability still requires exact-environment proof.
- [`calendar-ownership.md`](./calendar-ownership.md) — projection schema, `reminder_state`/`escalation_state`.
- `context/architecture/operational-kernel.md` — the 13-step kernel this rides.
- `wiki/Vaccination Rules.docx` — schedule, gap rules, tolerances (business truth).
- `wiki/Handbooks/PHC_Director.*`, `Mesha-dept-directors.docx` — org hierarchy + escalation SLAs.
- Migrations: `000050` (devices), `000074` (obligation), `000086` (calendar slice),
  `000087` (notification/escalation kernel), `000090` (escalation ack), `000102`
  (due-reminder index).
