# Goat OS — Generic Verification Module (design / future scope)

Status: implemented foundation for a **generic, cross-vertical media
verification module**. Derived from how Mesha runs today (the Slack Workflow
Engine) + the maintainer's target. Companion to
[`org-role-model.md`](./org-role-model.md) and
[`staff-org-data.md`](./staff-org-data.md). Vaccination proof-video verification
is ONE instance of this; the module must be generic and plug-and-play so every
future vertical/module reuses it with zero verification-specific code.

## 1. How it works today (Slack) — the pattern to generalize
Every farm task, across every vertical, already runs through one generic engine:
- **Unified Workflow DB (Slack Workflow Engine)** with a **Workflow Type Registry**
  (`RT-001..RT-011`): each task type declares a `Template`, `Thread Mode`,
  `Report Format`, and `Form Mapping`.
- Generic **`form_definitions`** + **`form_responses`** tables — one form model,
  many task types (goatOS.docx).
- Operators upload their task form + **media** in Slack for every task they do
  (e.g. "Upload Diagnosis Video", vaccination drive videos, death reports).
- A standing **Video Verification Team** reviews the uploaded media **every day**
  and marks it verified; a Director is penalised if a bad submission slips
  through (Health/PC Director handbook §05).

Takeaway: verification is **already a generic medium**, not per-feature. Goat OS
must model it the same way.

## 2. Goat OS design — a generic verification subsystem

### 2.1 Placement
- **Backend:** a standalone `verification` bounded context (its own service +
  ports), NOT inside vaccination. Modules feed it; it knows nothing about any
  specific vertical.
- **Frontend (admin-web):** a top-level **Admin / Data Ops** command screen
  (`/verification`), category/vertical-filtered — same authority tier as Config
  and SOP Library. It is cross-module, so it lives at Admin Ops, not under any one
  vertical (consistent with the command-lens rule in AGENTS.md).
- **Mobile:** a **standalone Verifier section** (role-gated). A verifier opens the
  app and sees a media queue by category — nothing else. It is separate from the
  operator Capture section; the two never mix on one screen.

### 2.2 Generic model (module-agnostic)
A single `verification_item` shape, independent of the producing module:
```
verification_item {
  id
  tenant_id, park_id                     // scope
  vertical, module, category             // e.g. preventive_care / vaccination / vaccination_proof
  source_ref { module, task_id, submission_id }   // back-pointer to the producer
  media[]  { proof_subject, signed_url, captured_start_ms, captured_end_ms,
             duration_ms, captured_by_principal_id }   // the fresh, attributable capture
  status   pending_verification | approved | rejected
  verdict  { verifier_principal_id, decided_at, reason }   // reason MANDATORY on reject
  action   { actor_principal_id, acted_at, outcome }       // atomic submission close after verdicts
}
```
Producers (vaccination, diagnosis, death report, …) emit a "needs verification"
event with their media refs; the verification service enqueues a generic item.
No producer-specific columns.

### 2.3 Plug-and-play registry (the RT-registry analog)
A **verification type registry**: each module registers a verifiable task type —
`{ vertical, module, category, expected_media[], form_ref, sla }`. Registering an
entry is all a NEW vertical/module needs to appear in the verifier queue,
admin-web screen, and mobile section. No verification code is touched per module.
This mirrors the Slack `Workflow Type Registry (RT-001..011)`.

### 2.4 Roles (introduce a Verifier role)
Three distinct responsibilities — do NOT collapse them:
- **Capture** = ground **operator only**. Mobile Capture
  section only. Records media, submits.
- **Verify** = **Verifier** (NEW role) — a human whose sole job is watching the
  uploaded media and marking **approved / rejected + reason**. This is the digital
  Video Verification Team. Sees a queue filtered to the categories/verticals they
  are assigned. On web AND as the standalone mobile Verifier section. Adds a new
  RBAC role + permission (`verification.review`).
- **Act** = **Park Head / Director / CEO/CxO** — take the real action based on the
  verifier's verdict (accept the drive, escalate, penalise, re-assign). The
  verifier's verdict is advisory input; the authority decides. Uses the existing
  `verification.act` permission and scoped leadership action queue.

Flow: `operator captures → verification_item (pending) → Verifier approves/rejects
+ reason → authority (park head/director/ceo) acts`.

### 2.5 Scale + extensibility (hard requirements)
- Queue is **keyset-paginated**, filtered by category/vertical/park; never a
  full-table scan (1–5M scale). Media is served via **signed URLs streamed**, not
  proxied through the API.
- Verifier throughput matters: assign-by-category, bounded pages (~20), prefetch.
- Plug-play: adding a vertical/module = one registry row + emitting the event.
  Backend, admin-web screen, and mobile Verifier section are all driven by the
  registry + the generic model — zero per-module UI/logic.

## 3. Current vaccination slice
- Vaccination operator screens capture/upload only; other roles can view.
- The standalone mobile Verifier section owns approve/reject. Its Vaccination
  tab is active; future categories register into the same module.
- Leadership sees only submissions where every goat item is approved and closes
  the drive atomically. Rejection reopens only that goat for rework.

## 4. Scope note
Vaccination is the first active category. Counts, Feed Direction, Diagnosis,
Death Report, Breeding, and other modules remain category registrations and
producer integrations, not separate verification implementations.
