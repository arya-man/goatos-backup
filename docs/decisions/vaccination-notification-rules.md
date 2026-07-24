# ADR: Vaccination notification rules — what to send, when, how often, to whom

Status: **PROPOSED design + business-rule catalog.** The delivery pipeline it
rides on is already built (see `notification-delivery.md`); this doc defines the
*rule layer* that currently does not exist in code. It is the source of truth for
the vaccination slice's notification cadence and the reusable framework every
future obligation-backed feature (feeding, breeding, procurement, delivery,
health) plugs into.

Scope note: **FCM (push) is the channel we build now.** SMS, email, voice call,
WhatsApp, and Slack are documented here as roadmap and mapped to the same rule
engine, but only `push_fcm` is wired in the first cut.

---

## 0. The GCP "which SQS?" answer (short)

You do **not** rebuild SQS. This is already mapped and implemented — full detail
in [`notification-delivery.md`](./notification-delivery.md). One-line version:

| Amazon (SQS-centric) | GCP equivalent | Role in this system |
|---|---|---|
| SQS work queue | **Cloud Tasks** | near-term timed dispatch + per-message retry/backoff (the closest 1:1 to an SQS sender) |
| SNS/SQS fan-out bus | **Pub/Sub** | event spine, at-least-once, many idempotent consumers |
| SQS delay / visibility timeout | Cloud Tasks `schedule_time` / Postgres `next_attempt_at` + lease token | timers + in-flight lease |
| CloudWatch cron | **Cloud Scheduler** | ticks the sweeper for due / reminder / escalation scans |
| SNS → device push | **FCM** behind `notification/ports.Gateway` | last hop to the phone |

**Hard rule:** far-future "this vaccine is due in 6 weeks" state lives in
**Postgres**, never in the queue. Cloud Scheduler drives a sweeper that scans
indexed Postgres due-windows and *materializes* the near-term reminders into
Cloud Tasks / `notification_requests`. The queue is transport; Postgres is the
calendar. A queue is not a calendar database.

For vaccination specifically, the reminder fan-out ("1 week before, then daily")
is realized two ways, and both are valid:

- **Sweeper-materialized (default):** the scheduler-driven sweeper scans
  `calendar_event_projections` where `reminder_state <> 'not_scheduled'` and
  `due_at` falls inside the active reminder window, and emits the due reminder
  rows for *today's* fires. Simple, self-healing, no far-future queue state.
- **Cloud Tasks pre-scheduled:** when an obligation's `due_at` is set, enqueue
  each future reminder as a Cloud Task with `schedule_time = due_at − offset`.
  Lower latency, but every reschedule must cancel + re-enqueue.

Start with sweeper-materialized (it already has the index — mig `000102`,
`000086:111`); add pre-scheduled tasks only if reminder latency needs to beat the
sweeper tick.

---

## 1. What exists vs what this doc adds

Already built (do not rebuild):

- `notification_requests` durable queue with lease/retry/DLQ — mig `000087`.
- `notification-dispatcher` worker + multi-channel gateway routing
  `slack | webhook | email | push_fcm | incident` — `internal/notification/...`.
- Obligation engine with the full status lifecycle + sweeper missed-marking.
- `calendar_event_projections.reminder_state` / `escalation_state` columns —
  mig `000086:41-43`.
- `calendar/domain/types.go:230 NotificationPolicy json.RawMessage` — the empty
  slot this doc's rule schema fills.
- Device registry `workforce_member_devices(push_token_hash, status, …)` +
  routes `POST /app/devices/{register,heartbeat,deregister}` — mig `000050`.

Missing (this doc specifies it; implementation tracked separately):

1. The **reminder cadence ladder** (T-7d, daily reminders, due-today) — no
   offsets are defined anywhere in code today.
2. The **audience → recipient resolution** rules (park-scoped vs all-park
   leadership).
3. The **escalation ladder** timing for vaccination.
4. The **domain-event-consumer → notification_requests** wiring that reads a
   rule and fans out rows (the seam is identified in `notification-delivery.md`).
5. Device handlers (`internal/devices/` is an empty module) + mobile FCM SDK
   (both already tracked in `fcm-device-lifecycle.md`).

---

## 2. Vaccination lifecycle events (the triggers)

Obligation status machine (from `obligation/domain`):

```
scheduled ─▶ due ─▶ overdue ─▶ missed          (deadline path)
                └─▶ in_progress ─▶ completed    (execution path)
   any ─▶ deferred (health block) ─▶ rescheduled (recovery)
   any ─▶ canceled (animal exit / manual)
```

Each transition is written in one Postgres txn with an `obligation_status_event`
+ outbox row; the domain-event-consumer is the natural place to evaluate
notification rules.

Business events the vaccination slice raises, and whether they notify:

| Event | Trigger | Notifies? |
|---|---|---|
| Drive planned | `vaccination_shed_event` created (shed × vaccine × date) | yes — heads-up to the park |
| Obligation `scheduled → due` window opens | sweeper / projection at window start | yes — reminder ladder starts |
| Reminder tick | scheduler, inside the reminder window | yes — the cadence in §3 |
| `due → in_progress` | field batch started | low-priority ack to head/manager |
| `→ proof_pending` | execution done, proof (SOP video) uploaded | no push (interim state) |
| `→ verification_pending` | shed proof/video submitted for review | **yes — notify the verifier, that park head, PC director, and all CEOs** |
| `→ rejected` / `rework_due` | **verifier rejects the proof** | **yes — notify the operator who did it (+ park head)** so they redo |
| `→ completed` (verified/approved) | proof accepted (`vaccination_completion`) | rollup only (digest), no push spam |
| `due → overdue` | window closed, still open | yes — high priority + escalation start |
| `→ missed` | sweeper `MarkMissedBefore` crosses deadline | yes — escalation |
| `→ deferred` | health block (sick / quarantine / ICU / pregnancy window) | manager only |
| `deferred → rescheduled` | health recovery replanned | operator + manager |
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
| `D − 6d … D − 1d` | 3×/day | 08:00, 13:00, 18:00 | `reminder` (with `reminder_number` 1..N) | normal |
| `D − 0` (due day) | 3× | 08:00, 13:00, 18:00 | `due_today` | high |
| `D + 0` EOD not done | 1× | 18:00 | `overdue` | high → starts escalation |

This is the literal encoding of "1 week before, then daily reminders till the day
arrives," aligned to the current field rhythm of 08:00 / 13:00 / 18:00 local
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

**De-scheduling:** on `completed | deferred | canceled | rescheduled`, cancel all
pending future reminders for that obligation (set `reminder_state` back or drop
queued Cloud Tasks). A recovered/rescheduled obligation restarts the ladder from
its new `D`.

---

## 4. Who gets what (audience & scoping)

Two audience classes, from the org model (COO → Director → Park Head →
Manager → Assistant/Operator) and the position table
(`workforce_positions.scope_type ∈ {tenant, center}`, `position_tier`,
`position_code`):

### 4a. Park-scoped roles — their park only

Operators, Park Heads, and the PHC Manager for a park receive the **operational**
reminder ladder for events in **that park** (`scope_type='center'` AND
`scope_id = event.park_id`). They never see other parks' per-drive reminders.

Recipient query (already validated pattern):

```sql
SELECT DISTINCT d.device_id, d.push_token_hash
FROM workforce_positions p
JOIN workforce_members m  ON p.workforce_member_id = m.workforce_member_id
JOIN workforce_member_devices d ON d.workforce_member_id = m.workforce_member_id
WHERE p.tenant_id = $tenant
  AND p.scope_type = 'center' AND p.scope_id = $park_id
  AND p.position_code IN ('operator','park_head','phc_manager')
  AND p.status = 'active' AND m.status = 'active'
  AND d.status = 'active' AND d.push_token_hash IS NOT NULL;
```

Backup Manager: when the park's PHC Manager is absent (`workforce_absences`
approved, coverage assigned), the **Backup Manager covers that manager's
notifications** for the absence window — same park, same rules, routed to the
covering member. Ownership is never mutated; only the notification recipient is
swapped for the window. (Consistent with the Backup-Manager coverage model —
coverage of *tasks*, not a role swap.)

### 4b. HQ-tier roles — all parks

PC Director and CEOs (`scope_type='tenant'`, `position_code IN
('pc_director','ceo_internal')`) get **all-park** visibility for vaccination
drive notifications. The operational reminder ladder includes them because
vaccination execution is a priority-1 daily field workflow, but the event is
still batched/collapsed by recipient, park, date, and notification type so it
does not become per-animal spam.

Leadership receives:

- the same 08:00 / 13:00 / 18:00 reminder ladder while the shed is still
  scheduled/open,
- the immediate shed-submitted-for-review notification, and
- escalation/aging notifications when a drive is overdue, missed, stuck in
  review, or repeatedly rejected.

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
| `verification_pending` (shed proof/video submitted) | **verifier(s)** with `pc.vaccination` verify duty for that park, that park's **park head**, tenant **PC director(s)**, and all tenant **CEOs** | verifier can review, park leadership tracks execution, tenant leadership sees every submitted vaccination shed | normal, deduped by shed submission id per device |
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

So shed submit → verifier + park head + PC director + CEOs. A normal reject →
operator + park head. A *stuck* or *looping* verification → escalation up the
ladder (§5).

### 4d. Notification tap landing

Vaccination push taps do **not** open Scan. Scan is an operator-initiated action
from the Vaccination sheds list only.

| Recipient role | Reminder tap | Shed-submitted tap |
|---|---|---|
| `operator`, `park_head`, `phc_manager` | Vaccination | Vaccination |
| `verifier` | Vaccination | Verify item detail |
| `pc_director`, `ceo_internal` | Leadership vaccination view | Leadership vaccination view |

---

## 5. The reusable rule framework (how future features auto-derive notifs)

Every obligation-backed feature declares a **NotificationPolicy** for its event
types. The domain-event-consumer looks up the policy for an incoming
`obligation_status_event`, resolves audience + cadence, and writes
`notification_requests` rows (scheduling future fires via the sweeper window or
Cloud Tasks). This is the "auto-assume when and what to send" engine — a new
feature ships a policy, not new dispatch code. Policy is stored in
`calendar_event_projections.notification_policy` (the existing
`NotificationPolicy json.RawMessage` slot) and/or a versioned `notification_rules`
config table.

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
        - { offset: [-6d,-1d], at: ["08:00","13:00","18:00"], type: reminder, priority: normal }
        - { offset: 0d, at: ["08:00","13:00","18:00"], type: due_today, priority: high }
  quiet_hours: { from: "21:00", to: "07:00", tz: Asia/Kolkata }
  batch_key: [recipient, park_id, due_date, type]   # collapse per-animal spam
  # audience
  audience:
    - roles: [operator, park_head, phc_manager]
      scope: { by: park, from: event.park_id }       # park-scoped
    - roles: [pc_director, ceo_internal]
      scope: { by: tenant }                           # all parks, batched/collapsed
  # verification loop — shed submit is backend-owned and routed by capability/position
  verification:
    - on: verification_pending
      to:
        - { capability: verify_vaccination, scope: { by: park, from: event.park_id } }
        - { roles: [park_head], scope: { by: park, from: event.park_id } }
        - { roles: [pc_director, ceo_internal], scope: { by: tenant } }
      priority: normal
      idempotency: submission_id_per_device
    - on: [rejected, rework_due]
      to: { actor: event.performed_by, plus_roles: [park_head], scope: park }
      priority: high
    - on: completed                                   # approved
      to: none                                        # digest rollup only
    # leadership sees verification ONLY as aggregate + these exceptions:
    escalate_to_leadership_when:
      - verification_pending aged past review_sla
      - rework_loop: rejections >= 3 on same drive
      - config_activation_review                      # policy-level sign-off
  # escalation ladder (overdue -> missed)
  escalation:
    - after: overdue + 0h   -> { roles: [operator, park_head], scope: park }
    - after: overdue + 4h   -> { roles: [phc_manager, phc_director], scope: [park, tenant] }   # PHC 4h SLA
    - after: overdue + 24h  -> { roles: [coo], scope: tenant, channels: [push_fcm, incident] }
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
- **Escalation is state, not spam** — `escalation_state` + `obligation_escalations`
  (one open row per level) drive one push per level; ack stops the ladder.
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
| **FCM push** | `push_fcm` | **BUILD NOW** (code done, creds + mobile SDK gated) | all in-app reminders, due-today, escalation, digest | `sendFCM` OAuth2 bearer coded; needs `goatos-prod` Firebase + device handlers + mobile `FirebaseMessagingService` (see `fcm-device-lifecycle.md`) |
| **Slack** | `slack` | wired (live ops channel today) | team/park ops threads, escalation mirror, EOD rollups | already the production channel; keep as the ops mirror of every escalation |
| **Email** | `email` | wired (gateway case exists) | leadership digest, formal escalation record, completion certificates | `EmailWebhookURL` + auth token config; document, low effort to enable |
| **SMS** | `sms` | **document only** | critical/overdue for staff with no app or poor connectivity (field) | needs an SMS provider gateway case (e.g. via a webhook to an India SMS/DLT-registered sender); DLT template registration required in India |
| **WhatsApp** | `whatsapp` | **document only** | Director-level comms + reports (per handbooks) | needs WhatsApp Business API (template/HSM approval); handbook-mandated channel for leadership |
| **Voice / IVR call** | `voice` | **document only** | last-resort critical: biosecurity breach, death cluster, disease suspicion (PHC 4h escalation to leadership) | not in any doc today; add only for `priority: critical` escalations that go unacked; needs a telephony provider case |

Channel-selection policy (recommended default):

- `normal` reminders → `push_fcm` (+ in-app feed).
- `high` (due-today, overdue) → `push_fcm`; `sms` fallback if no active device.
- `critical` escalation (missed + unacked, biosecurity) → `push_fcm` **and**
  `incident`/`slack`; escalate to `voice` if still unacked past the top SLA.
- leadership `digest` → `push_fcm` + `email` (+ `whatsapp` for directors later).

Everything except the last hop is channel-agnostic: the same
`notification_requests` row can be re-targeted to any channel by the policy, so
enabling SMS/WhatsApp/voice later is additive — a gateway case + a policy edit, no
pipeline change.

---

## 7. Build order (FCM slice)

1. **Device handlers** — fill the empty `internal/devices/` (or fold into
   `workforce`): implement `RegisterAppDevice` / `heartbeat` / `deregister`
   against the existing routes + `workforce_member_devices`.
2. **Rule evaluation in domain-event-consumer** — on `obligation_status_event`,
   load the `notification_policy`, resolve audience (§4 query) + cadence (§3),
   write `notification_requests` rows; schedule future fires via the sweeper
   window (index `000102`) — Cloud Tasks pre-scheduling optional later.
3. **Reminder sweeper pass** — extend the scheduler-driven sweeper to emit
   today's due reminder fires and flip `reminder_state`.
4. **Escalation ladder** — wire `obligation_escalations` level advance + ack to
   the §5 timing; leadership digest job at 18:00 IST.
5. **FCM credentials** — provision `goatos-prod` Firebase in the `vgoats.com`
   org; supply the service-account bearer to the gateway (Secret Manager).
6. **Mobile FCM SDK** — `FirebaseMessagingService`, token couple on launch,
   `onNewToken` re-register, deregister on logout (`fcm-device-lifecycle.md`).

Steps 1–4 need **zero Firebase** and are testable today via
`GOATOS_NOTIFICATION_DRY_RUN` + the `local-stub`/`slack` channels. Steps 5–6 are
the org-gated last hop.

---

## References

- [`notification-delivery.md`](./notification-delivery.md) — the GCP/SQS pipeline (built).
- [`fcm-device-lifecycle.md`](./fcm-device-lifecycle.md) — token couple/decouple (backend done, mobile TODO).
- [`calendar-ownership.md`](./calendar-ownership.md) — projection schema, `reminder_state`/`escalation_state`.
- `context/architecture/operational-kernel.md` — the 13-step kernel this rides.
- `wiki/Vaccination Rules.docx` — schedule, gap rules, tolerances (business truth).
- `wiki/Handbooks/PHC_Director.*`, `Mesha-dept-directors.docx` — org hierarchy + escalation SLAs.
- Migrations: `000050` (devices), `000074` (obligation), `000086` (calendar slice),
  `000087` (notification/escalation kernel), `000090` (escalation ack), `000102`
  (due-reminder index).
