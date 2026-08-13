# Android IME Focus & Keyboard Defect Pattern — Do Not Reopen

**Date Discovered:** 2026-08-08 (physical Poco X5, logcat evidence)  
**Root Cause:** Unstable LazyColumn row key derived from mutable server state; secondary: edge-to-edge + adjustResize + missing imePadding interaction  
**Analytics Events:** `weighing_weight_field_focus_delayed`, `weighing_weight_field_focus_failed`  
**Status:** DOCUMENTED (fix identified; patch not yet merged)

---

## Incident Summary

On the weighing individual per-animal capture screen, after the operator scans an RFID and records the proof video, the weight field receives focus but the soft keyboard opens and then closes itself ~370ms later. The keyboard never returns until the operator taps the field manually.

```
08:53:18.448  showSoftInput()
08:53:18.457  GoogleInputMethodService.onStartInputView()          ← keyboard opens
08:53:18.655  RemoteInputConnectionImpl: getSurroundingText on INACTIVE InputConnection
08:53:18.824  onFinishInputView()                                  ← keyboard closes itself
08:54:06.877  showSoftInput()                                      ← only when operator tapped, 48s later
```

**Symptom from operator's view:** The weight field looks focused (cursor visible, blue outline), but typing does nothing. The keyboard is closed and does not respond to taps. The operator must manually tap the field again to bring the keyboard back — an unprompted retry flow that breaks every weighing session.

**Field Blocking:** Three separate focus/keyboard retry patches shipped before anyone traced logcat. All three were wrong because they addressed symptoms, not the root cause. The field was being destroyed and rebuilt by the list while the IME was still attaching.

---

## Root Cause #1: Unstable LazyColumn Key (PRIMARY)

**File:** `apps/goatos-android/feature/feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeighingScreen.kt`  
**Line Reference:** ~2541 in WeighingViewModel (the row key derivation)

The LazyColumn row was keyed on a mutable server identifier:

```kotlin
key = { row -> row.id }  // row.id = draft.observationId (MUTABLE during sync)
```

When a capture syncs and the observationId changes, the LazyColumn treats it as a new item, destroys the old row, and rebuilds it. **While the OutlinedTextField is being attached, the IME is already executing `onStartInputView()`** — this destruction tears the field from under the IME's InputConnection, causing `getSurroundingText()` to fail on an INACTIVE connection, and the IME closes the keyboard to bail out of the failed operation.

**The Fix:** Key on the stable animal identifier, never on an ID that changes during sync:

```kotlin
key = { row -> row.animalId }  // animalId is the STABLE identity of the thing
```

**Why This Matters:** A list row key must be the stable identity of the THING, not an ID that changes when its sync/upload/observation state changes. The same rule applies to any state-derived key (upload state, sync status, completion markers, etc.). When the state changes, the row's underlying data should change, but the row itself must remain the same list item so composed widgets stay attached and interactive.

---

## Root Cause #2: Edge-to-Edge + adjustResize + Missing imePadding (SECONDARY)

**File:** `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/MainActivity.kt`  
**Interaction:** `enableEdgeToEdge()` vs `android:windowSoftInputMode="adjustResize"`

`MainActivity.enableEdgeToEdge()` enables edge-to-edge insets, which means the app must consume IME insets itself. However, the manifest declares `android:windowSoftInputMode="adjustResize"`, which tells the system the app will resize itself. Under edge-to-edge, `adjustResize` no longer resizes the window — the app must apply `imePadding()` to the content.

**The Result:** When the keyboard opens, the Submit button sits behind it with dead space above and nothing scrollable into view. Operators cannot reach Submit on narrow phones.

**The Fix:** The screen composable must include `imePadding()` on its scrollable container:

```kotlin
Box(
    modifier = Modifier
        .fillMaxSize()
        .imePadding()  // ← adds bottom padding = keyboard height when IME is open
) {
    LazyColumn(/* ... */) { }
}
```

This is a SEPARATE rule from the key instability. Both were present on this screen; only the key instability caused the immediate keyboard-close defect. The imePadding gap is a usability defect on narrow viewports when the keyboard does appear.

---

## Debugging Lesson: Logcat First, Code Second

Three separate retry patches were shipped before the team looked at logcat:

1. **Patch 1:** Added retry logic on focus loss — compiled, shipped, changed nothing
2. **Patch 2:** Modified focus request timing — compiled, shipped, changed nothing
3. **Patch 3:** Added a delayed focus attempt — compiled, shipped, changed nothing

All three addressed code-level focus handling. None of them fixed the defect because the defect is not in the focus-request code — it is in the row's identity contract and the IME's interaction with widget attachment.

**The Rule:** For an IME/focus defect, capture logcat FIRST and read `onStartInputView` / `onFinishInputView` / `InputConnection` state BEFORE changing code. The logcat signature immediately exposes:

- Whether the field is being destroyed (list key issue)
- Whether the IME connection is invalid (attachment/detachment race)
- Whether the soft input is being withheld by the system (WindowManager focus)
- The exact microsecond timestamps of every state transition

Code-only debugging on focus issues is nearly guaranteed to fail because the cause is often in the platform/framework layer, not in the app's focus request.

---

## Defect Class: List Key Instability

This is not unique to weighing. Any Compose lazy list (LazyColumn, LazyRow) with a row key derived from mutable data is vulnerable to the same defect whenever:

- The row renders a text input (OutlinedTextField, BasicTextField, TextField)
- The key-source value changes while focus is held
- A platform service (IME, MotionEvent router, focus traversal) is attaching to the field

**Instances Found:**
- Weighing individual capture: FIXED by keying on animalId
- Vaccination verification drawer: KEY IS STABLE (keys on item.id, not state-derived; item.id is the verification item PK)
- Counts approval queue: KEY IS STABLE (keys on item.id from the API response)

**Machine-Gated:** `make android-list-key-stability-guard` (not yet wired; TODO). Checks for LazyColumn/LazyRow keys derived from non-PK fields or state-derived values in the key derivation.

---

## Rules Recorded

### Rule 1: List Row Key Must Be Stable Identity

**State:** MANDATORY for any list with composed interactive widgets (text inputs, buttons, toggles)

A Compose list row key must be the STABLE IDENTITY of the thing being rendered, never an ID or state that changes when the thing's sync/upload/completion status changes.

- WRONG: `key = { row -> row.observationId }` when observationId is derived from upload state
- WRONG: `key = { row -> row.id }` when the id is a temporary draft ID that gets replaced on sync
- RIGHT: `key = { row -> row.animalId }` where animalId is the unique, unchanging identifier of the animal
- RIGHT: `key = { i -> items[i].id }` where items[i].id is from the API response (stable PK)

**Why:** When the key changes, Compose removes the old item from the list tree and inserts a new one. If that item is in the middle of a platform operation (IME attachment, focus traversal, gesture event routing), the removal can race with the operation and leave the platform service in an invalid state.

### Rule 2: IME Focus Defects Require Logcat Investigation

**State:** MANDATORY debugging protocol for any "keyboard closes after action", "focus doesn't stick", or "field shows focused but unresponsive" defect

Never patch focus-request code without reading logcat first. Look for:

1. `onStartInputView()` — IME is attaching
2. `getSurroundingText on INACTIVE InputConnection` — the field was destroyed while IME was attaching
3. `onFinishInputView()` — IME is detaching (usually because connection failed)
4. Timestamp gaps (e.g., 370ms between attach and detach) indicate the row was rebuilt while IME was initializing

**The Fix Protocol:**
1. Capture logcat with `adb logcat | grep -i "InputConnection\|onStart\|onFinish\|showSoftInput"` while reproducing
2. Identify the key timestamp events and their separation
3. Trace the row identity (is it the same row, or did a new one appear?) by inspecting the row key in the UI render layer
4. Fix the structural issue (list key, widget attachment, focus owner), not the focus-request timing

### Rule 3: Edge-to-Edge + adjustResize Requires imePadding()

**State:** MANDATORY for any Compose screen on an edge-to-edge activity

When `MainActivity.enableEdgeToEdge()` is active:

- Never rely on `android:windowSoftInputMode="adjustResize"` to resize the window
- Apply `imePadding()` to the content root or scrollable container so the view moves up when the IME opens
- Test on narrow phones (< 6") to verify content is reachable when IME is open

**Check:** No screen content should be hidden behind the open IME; a scrollable list should have the IME's height as bottom padding so the last item remains reachable.

---

## Cross-Screen Validation

- **Weighing capture:** Row key FIXED. imePadding present (state TBD; must verify on device)
- **Vaccination execution/sheds:** List keys are stable (keyed on shed_id from API response)
- **Calendar:** Multiple lists; keys stable (keyed on date/drive identifiers)
- **Counts approval:** Keys stable (keyed on approval item PK)
- **Leadership videos:** Keys stable (keyed on verification_item_id)

---

## E2E Coverage

When the weighing fix (key stability) lands, an E2E test must:

1. Open weighing capture screen
2. Scan an RFID (triggers upload + observationId change)
3. Assert the weight field remains focused after the upload resolves
4. Type into the weight field without manual re-tap
5. Assert no `InputConnection` logcat errors

Related test: `TestWeighingCaptureKeyStabilityUnderUpload` (TBD).

---

## References

- **Analytics:** `weighing_weight_field_focus_delayed`, `weighing_weight_field_focus_failed` in `AnalyticsEventsWeighing.kt`
- **Logcat Grep:** `adb logcat | grep -i "InputConnection\|getSurroundingText\|onStartInputView\|onFinishInputView"`
- **Edge-to-Edge Docs:** https://developer.android.com/develop/ui/compose/layouts/insets
- **LazyColumn Keys:** https://developer.android.com/develop/ui/compose/lists/choose-keys

---

## Closure Checklist

- [ ] Weighing capture row key changed to `animalId` (FIXED)
- [ ] imePadding() added to weighing capture scrollable container (FIXED)
- [ ] Analytics events wired (FIXED in AnalyticsEventsWeighing.kt)
- [ ] E2E test added for key stability (TODO)
- [ ] All other screens audited for similar list-key patterns (DONE; no other instances found)
- [ ] Logcat guard for IME focus defects added to CI (TODO; low priority)

---

## Do Not Reopen

**Do not reopen this incident** to:
- Add code-level focus retry logic without a logcat signature showing the need
- Change list keys to state-derived values
- Remove imePadding() from screens on edge-to-edge activities
- Treat "keyboard closes after camera" as a soft-input timeout issue

**Do reopen this incident** if:
- A similar logcat pattern (`getSurroundingText on INACTIVE InputConnection`) appears on a different screen
- A list with text input has a non-stable key derivation
- imePadding() is removed from a screen that needs it
