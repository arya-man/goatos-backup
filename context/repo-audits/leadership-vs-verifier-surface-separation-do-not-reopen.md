# Leadership vs. Verifier Surface Separation (Do-Not-Reopen Ledger)

**Date:** 2026-08-06  
**Status:** CLOSED / DO-NOT-REOPEN  
**Guard:** `check-leadership-verifier-surface-separation.mjs`

## Incident Report

On 2026-08-06 morning, the leadership "Videos" navigation item was repointed to resolve to a verifier queue route (`/verify/...`) instead of the correct leadership audit route. This created two problems:

1. **UI Coupling:** Leadership's view was no longer independent; every verifier component change instantly reflected in leadership.
2. **UX Confusion:** Verdict buttons appeared in leadership screens (greyed out) because leadership rendered verifier composables.

## Decision: Strict Architectural Separation

Leadership and Verifier are SEPARATE screens on SEPARATE routes with NO shared composables, ViewModels, or UI components.

### The Three Bans

**Ban A: Backend Navigation Routes**
- Leadership nav hrefs MUST point to leadership-owned routes like `/videos/module`
- Leadership nav hrefs MUST NOT use `leadershipVideosHref()` (this helper returns `/verify*`, still a verifier route)
- Leadership nav hrefs MUST NOT call `verifyQueueHref()` (verifier-only)
- Leadership nav hrefs MUST NOT point directly to `/verify/...` routes

**Ban B: Android Leadership Screen Imports**
- Leadership screen files (under `.../leadership/` directories) MUST NOT import from `feature.verify`
- Leadership screen files MUST NOT use verifier composables or ViewModels

**Ban C: Android Leadership Screen Controls**
- Leadership screen files MUST NOT render verdict buttons (approve/reject/rework/reassign)
- Leadership screen files MUST NOT render any verdict-casting UI

## Why This Matters

| Aspect | Leadership | Verifier |
|--------|-----------|----------|
| Permission | `verification.review` (read) | `verification.verdict` (approve/reject) |
| Route | `/videos/module` (leadership-owned, when built) | `/verify/action` or `/verify?module=X` |
| Purpose | Audit trail + read-only review | Cast verdicts |
| UI | Complete history (pending, approved, rejected, closed) | Open items only + verdict buttons |

A CEO reviewing the audit trail needs the COMPLETE history (including closed verdicts). A verifier casting verdicts needs a FOCUSED QUEUE (open items only). These are different surfaces with different semantics.

## Machine Enforcement

The guard `tools/agent-hooks/check-leadership-verifier-surface-separation.mjs` runs on every commit:

1. **backend-nav-no-verify-route** — scans bootstrap_copy.go for direct `/verify` routes in leadership nav.
2. **mobile-leadership-no-verifier-imports** — scans `.../leadership/**/*.kt` for imports from feature.verify.
3. **mobile-leadership-no-verdict-controls** — scans `.../leadership/**/*.kt` for verdict buttons/handlers.

Violations block the build (part of `make ci-local`).

## Known Blind Spots

1. Runtime-assembled routes (string concatenation) are not caught.
2. Imports hidden by wildcard imports are not visible.
3. Verdict controls in base classes are not detected.
4. Non-leadership files (not under `.../leadership/`) are not scanned.

Native Grep/Read review is required for these cases.

## Related Docs

- `docs/decisions/leadership-vs-verifier-surface-separation.md` — full decision
- `context/architecture/verifier-app-and-flow.md` — architecture
- `AGENTS.md` → "Confirmed verifier verdict-exclusivity rule"

## Closure

This decision is FINAL and LOCKED. Any future proposal to share UI/routes/components between leadership and verifier surfaces MUST start with explaining why the 2026-08-06 incident is not a risk. Opening this without answering that question will not move the needle.

Guard: `make leadership-verifier-surface-separation-guard`  
Owned by: Maintainer (STANDING LOCK)
