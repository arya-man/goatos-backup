# Meaningful notification copy (never abstract)

Status: **maintainer decision, 2026-08-02** — permanent Goat OS rule.

## Rule

Every user-facing notification — push, in-app, banner, or leadership escalation —
must be **meaningful**: a person must be able to decide what to do without
opening the app. This applies to **every notification type** (vaccination,
weighing, feed, counts), not just vaccination.

A meaningful notification carries:

- **Park name.**
- **Shed/partition label.**
- **Vaccine or work-item name in human form** — e.g. `ET+TT`, `PPR · Booster`.
  Never a raw config token such as `et_tt_adult_w2`.
- **Animal/shed counts.**
- **A farm-readable due date in IST.**

Leadership escalations must **name which sheds are outstanding**, not just
report a count.

## The failure this prevents

Before (defective — the motivating real example):

> **Title:** Vaccination(s) due soon
> **Body:** · 3

This tells nobody anything actionable: which park, which shed, which vaccine,
due when. A director scanning ten of these across parks cannot triage.

After (corrected):

> **Title:** Gandhi Nagar · Shed 4 · ET+TT due
> **Body:** 3 goats in Shed 4 (Gandhi Nagar) need ET+TT by 2026-08-04, 6:00 PM IST.

Same shape applies to a leadership escalation:

Before (defective):

> **Title:** Vaccination drive ready to close
> **Body:** All proof videos for this vaccination drive are verified.

After (corrected — names the outstanding sheds, not just a count):

> **Title:** Gandhi Nagar drive: Shed 2, Shed 5 still outstanding
> **Body:** 2 of 6 sheds unverified — Shed 2 (4 goats), Shed 5 (2 goats). Rest closed.

## Real example already in the tree (found during this change)

`backend/internal/notificationbridge/weighing_lifecycle_notify_consumer.go:285`
currently reads:

```go
Body: fmt.Sprintf("A weighing plan with %d sheds is now live.", len(payload.Buckets)),
```

This is exactly the abstract count-only pattern the rule prohibits — it names
neither the park nor which sheds. It is flagged by
`notification-specificity-guard` as of this decision and is tracked as a live
finding pending a fix in `backend/internal/notificationbridge` (out of scope
for this documentation/guardrail change — see file-ownership note below).

## Enforcement

`make notification-specificity-guard`
(`tools/agent-hooks/check-notification-specificity.mjs`) scans
`backend/internal/notificationbridge/**/*.go` (excluding tests) for:

1. **Abstract count/generic-noun copy** — a `Title:`/`Body:` literal or
   `fmt.Sprintf(...)` feeding one that reads as a bare count/generic-noun
   sentence (`"... due soon"`, `"%d sheds is now live"`, `"N items pending"`)
   with no park/shed/date/name/count specificity nearby.
2. **Raw config tokens leaking into notification copy** — a narrower,
   defensive check inside notification Title/Body strings specifically; it
   does not replace `check-ui-vaccine-labels.mjs`, which remains the primary
   authority for raw vaccine-token leaks across all of admin-web/Android UI.

Escape hatch: `// notification-copy:ignore: <reason>` on the line, for
genuinely non-farm-entity notifications (e.g. a system health-check push).

The guard is registered in `tools/ci/guardrail-manifest.json`
(id `notification-specificity`), wired into the `guardrails` Make target and
`tools/ci/run-local-ci.sh`, and is diff-scoped like its sibling guards so
unrelated commits pass instantly while the fixed target
(`backend/internal/notificationbridge/`) is always audited.

## Ownership note

This decision doc, the guard, and CI wiring were added under
`tools/agent-hooks/**`, `tools/ci/**`, `Makefile`, `docs/**`, `AGENTS.md`, and
`.agents/skills/**`. Fixing the flagged `weighing_lifecycle_notify_consumer.go`
copy itself is **out of scope** here: another agent is live-editing
`backend/internal/notificationbridge/**` and that surface was explicitly
off-limits for this change.

## Addendum 2026-09-12: verification lifecycle pushes are TASK-level, and every one leads with the item's own subject

Maintainer decision 2026-09-12, from the phone: for one pen move the pending
push read *"Counts video pending · Pen move · Gandhi 2 · from Ho Chi Minh 1 ·
1 animals recorded; counts video verification is pending."* and the approved
push read *"Counts proof verified · counts proof for Sumathi 1 (Coimbatore) is
verified."* The maintainer's words: "counts proof what? That's a shifting one
verified" — and: "if you don't make it module level, keep it task level."

Two code paths composed copy for the same verification item. Pending and
withdrawn led with the producer's `SubjectLabel`; approved rebuilt a sentence
from module + shed and threw the subject away; closed used a static per-module
string with no location at all. The module name ("counts", "pc_care", "feed")
is an internal grouping — four different jobs for feed, five for counts — and
never the farm's word for the work.

Rule, now the only composition for pending / approved / rework / closed:

```text
Title:  <task noun> <state>            Pen move verified · Hoof trimming video pending
Body:   <item subject> (<park>) — <state sentence>
        Pen move · Sumathi 1 · from Ho Chi Minh 1 · 1 animal (Coimbatore) — video verified.
```

- The **task noun** comes from the item's CATEGORY (the task type a producer
  registers in `verificationcatalog`), via `verificationTaskNouns` in
  `backend/internal/notificationbridge/verification_task_copy.go`.
  `TestEveryVerificationCategoryHasATaskNoun` fails the build when a category
  ships without one; the module-word fallback exists only for an unregistered
  category and is never the normal path.
- The **subject** is the producer's own `SubjectLabel`, verbatim — the same
  line the verifier's queue shows. A subject-less item (feed transport, by
  design) degrades to `<task noun> · <pen>`; a subject-less vaccination
  approval still names its dose from the sop task (`ET+TT · Shed A (CPT)`).
- **Approve says "completed" where approve is the last step** (maintainer, same
  day: "rather than showing it's verified show it's completed, something like
  shifting completed"). A pen move, a packed bag, a treatment, a hoof trimming
  is DONE once the verifier accepts the video, so the push reads
  `Pen move completed · … — video verified, work complete.` Two modules keep
  `… verified`: **weighing** (the bucket is CLOSED separately, and the weighing
  consumer already announces "Weighing pen complete") and **vaccination**
  (leadership closes after approve and the operator gets "… closed"). Rule:
  `notificationbridge.approvedCopy`.
- `pendingModuleProfile` keeps routing only (duty module, director seat,
  screens/targets, `message_key` prefix). It carries no copy any more, so the
  approved/closed wording cannot drift from pending again.
- Same change: the counts subject says `1 animal`, not `1 animals`
  (`countsdomain.PluralAnimals`), and the weighing rolled-forward push writes
  the planned date as `07/09/2026`, not `2026-09-07`.

Pinned by `verification_task_copy_test.go` (all four lifecycle pushes for the
screenshot's pen move; task-vs-module titles for pc_care, health, feed,
weighing; the subject-less and dose fallbacks).
