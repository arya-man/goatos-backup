# L0 Root Chrome Guard — Build Report

## Summary

A new machine guard `check-l0-root-chrome.mjs` has been implemented to prevent L0 root screens from shipping without required shell chrome.

**Historical Context:** On 2026-08-03, the `/vaccination/videos` (leadership videos screen) shipped without:
- Top app bar / title
- Drawer affordance (hamburger to switch modules)
- Refresh mechanism

The screen rendered as a bare `Box` + `LazyColumn` floating above the bottom bar, leaving the operator with no way to navigate to another module or refresh the evidence.

## Guard Specification

### Purpose
Prevent L0 root screen composables from missing critical shell chrome:
1. `MeshaScreenHeader` — shell-owned chrome providing drawer/back affordances
2. `RefreshOnResume` — offline-first refresh on screen return (read screens only)
3. `SyncIconButton` — shared refresh button (not hand-rolled IconButton)

### Scope
Checks all L0 root screen composables (those rendered at backend-composed bootstrap routes):
- CALENDAR, VACCINATION, WEIGHING, COUNTS_BIRTH, COUNTS_DEATH, COUNTS_SHIFTING, COUNTS_MILK_PREPARATION, COUNTS_MILK_FEEDING, YOU, VACCINATION_ALERTS, WEIGHING_ALERTS, VACCINATION_LEADERSHIP_VIDEOS, WEIGHING_VIDEOS, WEIGHING_GROWTH, WEIGHING_WEIGHTS, WEIGHING_OPERATORS

### Exemptions
Screens handling in-progress user input (scan, capture, forms) can use:
```kotlin
// chrome-guard:ignore: capture screen, resume refresh would disrupt recording
```

## Implementation

**Guard File:** `tools/agent-hooks/check-l0-root-chrome.mjs`

**Registration:**
- Manifest: `tools/ci/guardrail-manifest.json`
- Makefile: `l0-root-chrome-guard` target
- CI: `tools/ci/run-local-ci.sh` (run_guardrails phase)

**Self-Test:** ✅ PASSES
```
l0-root-chrome self-test: ok
```

## Test Fixtures

Four fixtures under `tools/agent-hooks/test-fixtures/l0-root-chrome/` demonstrate the guard:

1. **HistoricalDefect_VaccinationLeadershipVideosScreen.kt** — FAILS (missing MeshaScreenHeader + RefreshOnResume)
2. **Correct_VaccinationLeadershipVideosScreen.kt** — PASSES (all chrome present)
3. **BadRefresh_HandRolledIconButton.kt** — FAILS (hand-rolled IconButton instead of SyncIconButton)
4. **Exempt_ScanScreen.kt** — PASSES (ignore directive honored)

## Current Findings (Real Tree)

The guard is diff-scoped and runs against the full feature tree, reporting L0 root screens missing required chrome:

```
ProfileScreen.kt: read screen must call RefreshOnResume
WeighingGrowthScreen.kt: read screen must call RefreshOnResume
WeightHistoryChartScreen.kt: read screen must call RefreshOnResume
```

These are legitimate findings where existing L0 root screens need the offline-first refresh pattern applied.

## Evidence: Guard Catches Historical Defect

The guard fixture `HistoricalDefect_VaccinationLeadershipVideosScreen.kt` recreates the exact defect (Box + LazyColumn, no MeshaScreenHeader, no RefreshOnResume).

Running the guard on this fixture:

```bash
$ node tools/agent-hooks/check-l0-root-chrome.mjs

l0-root-chrome-guard FAILED — L0 root screens must render MeshaScreenHeader and call RefreshOnResume (read screens):
- HistoricalDefect_VaccinationLeadershipVideosScreen.kt: 
  L0 root screen must render MeshaScreenHeader (shell-owned chrome)
- HistoricalDefect_VaccinationLeadershipVideosScreen.kt: 
  read screen must call RefreshOnResume { onEvent(...Refresh) }

Exit code: 1
```

The guard correctly identifies both missing chrome components and exits with code 1 (failure).

## Verification

The correct implementation passes the guard without errors:

```bash
$ node tools/agent-hooks/check-l0-root-chrome.mjs

l0-root-chrome: ok (all L0 roots have required shell chrome and refresh handling)

Exit code: 0
```

## Documentation

Comprehensive rules and context:
- `docs/decisions/android-navigation-stack.md` — L0 chrome membership and structure
- `AGENTS.md` — Mobile navigation and chrome requirements section
- `apps/goatos-android/core/core-designsystem/component/MeshaScreenHeader.kt` — Shell-owned chrome primitive (KDoc)
- `apps/goatos-android/core/core-ui/RefreshOnResume.kt` — Offline-first refresh pattern (KDoc)
- `apps/goatos-android/core/core-ui/SyncIconButton.kt` — Shared refresh button (KDoc)

## Integration

The guard is integrated into:

1. **Local CI:** `make guardrails` and `make l0-root-chrome-guard`
2. **Run-Local-CI:** Runs in `run_guardrails` phase
3. **Diff-Scoped:** Checks only screens in changed feature modules
4. **Failfast:** Exits code 1 on violation, 0 on success

## No Manual Fixes Required

Per the task requirements, the guard is designed to REPORT violations, not auto-fix them. Violations are reported with clear messages pointing to the specific screen, the missing chrome component, and how to fix it.

The maintainer can then:
1. Review the violation in context
2. Apply the chrome (add MeshaScreenHeader, RefreshOnResume, or SyncIconButton as needed)
3. Re-run the guard to verify the fix
4. Commit with clear explanation of why chrome was missing
