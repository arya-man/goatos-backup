# Proof Capture Authorization

Status: accepted (2026-08-03).

## Decision

**Proof/evidence capture is authorized by the SAME execution right that authorizes
the work it proves — never by another vertical's task permission.**

A write that mandates evidence and the upload of that evidence are one indivisible
act. If a role may perform the write, it may complete the proof handshake for it.
Anything else produces a role that can do the work and can never prove it, which in
a proof-gated flow means the work can never be submitted at all.

When a vertical grows its own execute permission (`weighing.execute`, and any
future `breeding.execute` / `feed.execute`), the proof routes must accept it. The
correct lever is the **route**: widen `/app/proofs` writes via `AnyPermissions`
(ORed) to include the vertical's execute permission.

The wrong lever is the **role**: granting `task.execute` to a module role looks
like a one-line fix but `task.execute` is the operator's *general* task-execution
grant and carries SOP submission and scan capture for every vertical with it
(`POST /app/tasks/{task_id}/submissions`). That is a privilege escalation dressed
as a bug fix.

## Worked example — the incident this rule came from

Phone-QA, 2026-08-03. A Growth Director opened lump-sum weighing on Mandela 2,
entered a valid total weight and animal count, and recorded the mandatory group
video.

- The role holds `weighing.execute` (it executes weighing exactly like an operator
  *and* oversees others — deliberate) and deliberately **not** `task.execute`.
- `appRecordWeighingShedObservation` is gated on `weighing.execute`: allowed.
- Every `/app/proofs` write route was gated on `task.execute` alone, inherited from
  the vaccination/SOP era where proof capture was born: **403 permission_denied**.

The client retried the upload for minutes, no `proof_artifacts` row was ever
created, the proof never reached `SYNCED`, and `canRecordShedPartition` — which
requires one synced proof — kept Submit permanently disabled. The operator saw only
the word "uploading". Six thousand lines of logcat contained **not one** app-side
line about the failure; diagnosis required the server log and manual DB forensics.

Each half was individually correct. Only the join between the role map and the
route table was wrong, and nothing in CI looked at that join.

## Enforcement

`make proof-capture-authorization-guard`
(`tools/agent-hooks/check-proof-capture-authorization.mjs`, registered in
`tools/ci/guardrail-manifest.json` and wired into `make guardrails` and
`tools/ci/run-local-ci.sh`).

It parses the role→permission maps and the route table and re-implements
`AuthorizeRoute`'s AND/OR semantics over them: **every role holding any `*Execute`
permission must be authorized for every `/app/proofs` write route.** A new execute
permission, role, or proof route is picked up with no edit to the guard. Its
self-test includes an adversarial reconstruction of the exact defect above and a
future-vertical case. Blind spots are enumerated in the script header.

Route-layer behaviour is additionally pinned by
`backend/internal/permissions/weighing_proof_route_test.go`, including the
escalation guard that `weighing.execute` never authorizes task submission or any
vaccination surface.

## Consequences

- `growth_director` keeps exactly one module and gains no vaccination reach.
- `permissions_test.go`'s `{RoleGrowthDirector, TaskExecute, false}` stays false and
  stays correct. The defect was never a missing permission on the role; it was a
  route requiring the wrong one.
- A permanently rejected proof upload must also surface a farm-language reason on
  the operator's screen rather than an endless "uploading" — a proof-gated Submit
  that is disabled with no stated cause is a dead end in a shed.
