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
