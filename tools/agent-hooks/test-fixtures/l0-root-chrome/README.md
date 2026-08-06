# L0 Root Chrome Guard Test Fixtures

This directory contains test fixtures demonstrating the `check-l0-root-chrome.mjs` guard.

## Fixtures

### HistoricalDefect_VaccinationLeadershipVideosScreen.kt

**Status:** Guard FAILS (as intended)

This fixture represents the historical defect from 2026-08-03. The `/vaccination/videos` screen was shipped without:
- `MeshaScreenHeader` (shell-owned chrome with drawer affordance)
- `RefreshOnResume` (offline-first refresh on screen return)

The maintainer opened it on his phone and found a bare list floating above the bottom bar with no title, no way to navigate to another module, and no way to refresh.

**Guard violations:**
```
- HistoricalDefect_VaccinationLeadershipVideosScreen.kt: 
  L0 root screen must render MeshaScreenHeader (shell-owned chrome)
- HistoricalDefect_VaccinationLeadershipVideosScreen.kt: 
  read screen must call RefreshOnResume { onEvent(...Refresh) }
```

### Correct_VaccinationLeadershipVideosScreen.kt

**Status:** Guard PASSES (as intended)

This fixture shows the correct implementation with all required chrome:
- Renders `MeshaScreenHeader` (derives drawer affordance from shell state)
- Calls `RefreshOnResume` at the top (stale-while-revalidate pattern)
- Uses `SyncIconButton` for manual refresh (shared icon state, auto-disable while syncing)

### BadRefresh_HandRolledIconButton.kt

**Status:** Guard FAILS (as intended)

This fixture shows what happens when a developer hand-rolls a refresh IconButton instead of using the shared `SyncIconButton`.

**Guard violation:**
```
- BadRefresh_HandRolledIconButton.kt:
  refresh button must use SyncIconButton (shared icon state),
  not hand-rolled IconButton; this ensures consistent UX and auto-disable while syncing
```

### Exempt_ScanScreen.kt

**Status:** Guard PASSES (via ignore directive)

This fixture shows an exemption: scan/capture screens where a resume refresh would disrupt in-progress recording.

The `// chrome-guard:ignore: capture screen, resume refresh would disrupt recording` comment exempts this screen from the `MeshaScreenHeader` and `RefreshOnResume` requirements.

## Running the Guard

```bash
# Self-test (validates the guard itself)
node tools/agent-hooks/check-l0-root-chrome.mjs --self-test

# Full check (against real tree)
node tools/agent-hooks/check-l0-root-chrome.mjs

# Via Makefile
make l0-root-chrome-guard
```

## Implementation Details

The guard checks all L0 root screen composables (those rendered at backend-composed bootstrap routes) for:

1. **MeshaScreenHeader**: The shell-owned chrome primitive that:
   - Provides drawer affordance (hamburger) at L0 roots
   - Provides Up/Back affordance at L1+ drills
   - Never requires the screen to manage drawer state directly

2. **RefreshOnResume**: Offline-first "refresh on open" trigger for read screens:
   - Shows cached data instantly
   - Triggers background refresh on every return to screen
   - Never requires manual user sync tap

3. **SyncIconButton**: Shared refresh button for all read screens:
   - Continuously rotates icon while syncing
   - Auto-disables while refresh is in-flight
   - Prevents duplicate refresh requests

**Exemptions:** Screens that handle in-progress user input (scan, capture, forms) can use the `// chrome-guard:ignore: <reason>` comment to skip these checks.

## Related Documentation

- `docs/decisions/android-navigation-stack.md` — L0/L1 chrome rules
- `docs/decisions/android-offline-first.md` — RefreshOnResume pattern
- `apps/goatos-android/core/core-ui/SyncIconButton.kt` — Shared refresh button
- `apps/goatos-android/core/core-designsystem/component/MeshaScreenHeader.kt` — Shell chrome primitive
- `AGENTS.md` — Mobile chrome and navigation rules
