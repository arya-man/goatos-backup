# Leadership vs. Verifier Surface Separation (Do-Not-Reopen)

**Incident:** 2026-08-06  
**Decision:** Maintain strict architectural separation between Leadership (audit/overview) and Verifier (action/verdict) surfaces.  
**Status:** BANNED — cross-surface contamination is a do-not-reopen defect.

## What Happened (Incident Summary)

On 2026-08-06, the leadership "Videos" navigation item (shown to CEO/Directors) was repointed to resolve directly to a verifier queue route (`/verify/...`). This created two problems:

1. **UI coupling:** Every time a verifier component changed (filters, verdicts, layout), the leadership screen instantly reflected the change without intentional sync. Leadership was no longer independent.
2. **UX confusion:** Verifier screens render controls like "Approve" and "Reject" gated on the `verification.verdict` permission (verifier only). Leadership has only `verification.review` (read). When repointed at a verifier route, leadership saw verdict buttons that were greyed out or disabled, creating confusion about what was "readable vs. actionable."

Example: A CEO clicking the "Videos" tab opened the verifier's action queue (`/verify/action`) instead of the leadership audit trail. Changes to the verifier's UI—a new filter option, a reordering of the queue—instantly appeared in the CEO's view.

## Why Separation Matters

Goat OS enforces a clear separation of concerns:

| Dimension | Leadership | Verifier |
|-----------|-----------|----------|
| **Permission** | `verification.review` (read) | `verification.verdict` (approve/reject) |
| **Route** | `/videos/module` (leadership-owned, when built) | `/verify/action` or `/verify?module=X` (action queue) |
| **Purpose** | Audit trail + evidence review (read-only) | Cast verdicts on evidence (approvals, rejections) |
| **UI** | Shows the complete history (pending, approved, rejected, closed) | Shows open items only; carries verdict buttons |
| **Navigation** | Built by leadership contributions in bootstrap registry | Built by verifier contributions (reviewContributions) |

The leadership audit surface must never be coupled to the verifier's action queue:
- A CEO reviewing historical verdicts needs to see EVERYTHING—approved, rejected, and already-closed—with no partial view.
- A verifier casting a verdict needs a focused ACTION QUEUE—open items only, with verdict buttons.
- Leadership UI changes must not affect verifier behavior, and vice versa.

## The Rule (Enforced by Guard)

**Leadership and Verifier are SEPARATE SCREENS on separate routes with no shared composables, ViewModels, or UI components.**

Three specific bans:

### 1. Backend Navigation: No Verify Routes in Leadership Nav

Leadership navigation hrefs emitted by `backend/internal/workforce/app/bootstrap_copy.go` must:
- Point to leadership-owned routes starting with `/videos` (e.g., `/videos/vaccination`)
- NEVER point directly to `/verify/...` routes
- NEVER call `verifyQueueHref()` (verifier-only helper)
- NEVER call `leadershipVideosHref()` (this helper RETURNS `/verify*`, still a verifier route)

**Why (Corrected 2026-08-06 incident):** `leadershipVideosHref()` is named as if it's the correct way, but it RETURNS `/verify?module=X&status=all` — still a verifier route. The name is deceptive. Leadership and Verifier must be on SEPARATE routes. When the `/videos` screen is built, leadership nav will use `/videos/module`, not any `/verify*` route.

### 2. Android/Mobile: No Verifier Imports in Leadership Screens

Leadership-owned screen files (under `.../leadership/` in feature modules):
- Must NOT import from `sg.mesha.goatos.feature.verify`
- Must NOT use verifier composables (e.g., `VerifyDetailScreen`)
- Must NOT use verifier ViewModels (e.g., `VerifyDetailViewModel`)

**Why:** If leadership renders a verifier composable, that composable carries its own state (verdicts, rejection reasons, permissions checks), making leadership just a thin wrapper around the action queue. Changes to verifier UI instantly break or confuse leadership.

### 3. Android/Mobile: No Verdict Controls in Leadership Screens

Leadership-owned screen files must NOT render:
- "Approve" / "Reject" buttons or controls
- "Rework" or "Reassign" handlers
- Verdict-casting UI of any kind

**Why:** Verdict casting is verifier-only. Leadership sees the RESULT of a verdict (a chip showing "approved by: name"), not the action to cast it.

## Machine Enforcement

The guard `check-leadership-verifier-surface-separation.mjs` enforces:

1. **backend-nav-no-verify-route** — scans `bootstrap_copy.go` leadership contributions for direct `/verify` hrefs.
2. **mobile-leadership-no-verifier-imports** — scans `.../leadership/**/*.kt` files for imports from `feature.verify`.
3. **mobile-leadership-no-verdict-controls** — scans `.../leadership/**/*.kt` files for approve/reject/rework buttons.

The guard runs on every commit (part of `make ci-local`). A violation blocks the build.

## Self-Test: Catching the 2026-08-06 Incident

The guard includes an adversarial self-test proving it catches the deceptive helper defect:

- **`bootstrap-bad-leadership-videos-href-returns-verify`**: Calls `leadershipVideosHref()` (looks correct by name) but the guard now FAILS it (correct) because leadershipVideosHref() returns `/verify*`. This test proves the guard catches what the old guard missed.

A guard that never fails in its self-test is not accepted.

## Blind Spots

The guard is textual and has known blind spots:

1. Routes assembled at runtime (string concatenation, query param builders) are not caught.
2. Imports hidden behind wildcard imports or intermediate modules are not visible.
3. Verdict controls defined in base classes outside the scanned file are not detected.
4. Leadership screen files not under a `.../leadership/` directory are not recognized as leadership-owned.
5. Deceptively named helper functions that return the wrong route are caught ONLY if explicitly listed in the guard (e.g., `leadershipVideosHref`). Other helpers with misleading names may slip through.

Grep/Read review is still required for these cases.

## Related Decisions

- `context/architecture/verifier-app-and-flow.md` — the full verifier and leadership architecture.
- `AGENTS.md` section "Confirmed verifier verdict-exclusivity rule" — the split between `verification.review` (read) and `verification.verdict` (approve/reject).
- `docs/decisions/role-module-nav-composition.md` — why leadership has 2+ modules and needs a drawer, not per-module bottom bars.

## References

- Incident report: 2026-08-06 morning standup (leadership Videos repointing defect)
- Guard source: `tools/agent-hooks/check-leadership-verifier-surface-separation.mjs`
- Manifest entry: `tools/ci/guardrail-manifest.json` (leadership-verifier-surface-separation)
- Makefile target: `make leadership-verifier-surface-separation-guard`
