# Android UI Preview And Screenshot Workflow

This is the Goat OS Android visual-review rule for Codex, Claude, and local
developers. Use it whenever a change touches Compose UI, navigation chrome,
bottom sheets, proof capture, scan execution, or a role-specific mobile surface.

## Fast Review

Use Showkase for quick mock-state review on device or emulator. Showkase is a
debug-only Compose catalog; it must never be required by release runtime.

Add or update a `@ShowkaseComposable` fixture before arguing about UI quality.
Fixtures should render the real production composable with stable sample state,
not a separate fake screen. Cover the awkward cases first:

- empty, loading, offline, retry, and blocked-submit states
- long text, large numbers, two RFID tags, multiple vaccines, and mixed labels
- duplicate tag, unknown tag, wrong shed, already completed, missing proof
- proof missing, uploading, failed, retrying, synced, and background sync states
- role-specific shell/chrome differences for CEO/director/operator
- weighing individual versus lumpsum capture states

Launch the debug catalog after installing a dev debug build:

```bash
adb shell am start -n sg.mesha.goatos.dev/sg.mesha.goatos.ShowkaseLauncherActivity
```

Capture the current screen:

```bash
adb exec-out screencap -p > /tmp/goatos-showkase.png
```

## PR Regression

Use Paparazzi for committed screenshot evidence. Showkase is for fast browsing;
Paparazzi is the PR gate.

Record intentional Android UI changes:

```bash
./gradlew :app:recordPaparazziDevDebug --console=plain
```

Verify before push:

```bash
./gradlew :app:verifyPaparazziDevDebug --console=plain
```

For focused review, run a single test class or method:

```bash
./gradlew :app:recordPaparazziDevDebug --tests 'sg.mesha.goatos.ui.ScanEdgeCaseScreenshotTest' --console=plain
```

## Copy Firewall

Visible mobile UI is for farm operators and leaders, not engineers. Showkase
fixtures, Paparazzi goldens, strings, snackbars, empty states, cards, rows,
drawers, and bottom sheets must not show internal implementation words such as
`debug`, `mock`, `fixture`, `Paparazzi`, `Room`, `outbox`, `idempotency`,
`groupKey`, `backend`, `API`, `route`, `local`, or `localhost`.

Use product language instead: "Waiting for network", "Proof uploads in
background", "Already scanned", "Wrong shed", "Needs proof", "Cannot submit
yet", and "Try again".

## Handoff Bar

A mobile UI handoff is incomplete until it includes:

- the Showkase fixture names that cover the changed states
- the Paparazzi command that was run
- screenshots or snapshot paths for the important edge cases
- explicit confirmation that production copy contains no internal/debug terms
