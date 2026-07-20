# RFID Keyboard Reader Integration

Status: V1 implementation rule. This supersedes the earlier SDK-first wording
for the RFID reader in the Android mobile docs.

Runtime sync contract: see
[`docs/runbooks/rfid-scan-capture-sync.md`](../runbooks/rfid-scan-capture-sync.md)
for the implemented Room -> outbox -> backend draft -> Submit merge path.

## Decision

The current field reader is a Bluetooth HID keyboard-wedge RFID reader. It is
paired by Android as an input device and types the scanned tag ID as hardware
key events, usually followed by Enter.

For V1, Goat OS must integrate this reader through hardware keyboard input, not
through a vendor RFID SDK.

The scan screen must stay visually identical to the mock:

```text
no visible keyboard
no visible EditText
no invisible EditText dependency
no manual tag-entry field on the scan surface
```

"Keyboard" means a physical Bluetooth input device. It does not mean showing
the Android soft keyboard.

## Why

The reader already works in WhatsApp, Notes, and any normal text field. That
proves the hardware is emitting standard keyboard input. The lowest-risk field
path is therefore to let Android own the Bluetooth HID connection and let Goat
OS capture the hardware key events while the scan screen is active.

Vendor BLE / SDK integration remains a later adapter option only when we need
reader-owned features such as battery, signal strength, device configuration,
trigger control, or a non-HID protocol.

## Android Version And Permissions

Goat OS mobile supports Android 10+ (`minSdk 29`) and targets the current
compile/target SDK.

Use version-gated Bluetooth permissions:

```xml
<!-- Android 10/11 and below. Enough for paired-device state and ACL broadcasts. -->
<uses-permission
    android:name="android.permission.BLUETOOTH"
    android:maxSdkVersion="30" />

<!-- Only needed on Android 10/11 if the app actively starts discovery/pairing scans. -->
<uses-permission
    android:name="android.permission.BLUETOOTH_ADMIN"
    android:maxSdkVersion="30" />

<!-- Only needed on Android 10/11 if app discovery/scanning is used. Not needed
     for pure InputManager readiness plus system Bluetooth settings. -->
<uses-permission
    android:name="android.permission.ACCESS_FINE_LOCATION"
    android:maxSdkVersion="30" />

<!-- Android 12+. Runtime "Nearby devices" permission for paired-device access. -->
<uses-permission android:name="android.permission.BLUETOOTH_CONNECT" />

<!-- Android 12+. Only needed if the app actively scans for nearby Bluetooth devices. -->
<uses-permission
    android:name="android.permission.BLUETOOTH_SCAN"
    android:usesPermissionFlags="neverForLocation" />
```

V1 should avoid in-app Bluetooth discovery unless product explicitly needs a
branded pairing wizard. The simpler and safer path is:

```text
status/readiness detection in app
system Bluetooth settings/pairing for connection
hardware key capture for tag reads
```

## Readiness Detection

The RFID status button should answer one question: can this phone receive tag
reads from the reader right now?

Primary signal:

- `InputManager` sees an external keyboard/input device whose name/vendor hints
  match the configured RFID reader, for example `IDT RHLS-3`.
- `InputManager.registerInputDeviceListener(...)` refreshes readiness when input
  devices are added, removed, or changed.

Secondary Bluetooth signals:

- `BluetoothAdapter.getBondedDevices()` can tell whether a known reader is
  paired.
- `BluetoothDevice.ACTION_ACL_CONNECTED`,
  `BluetoothDevice.ACTION_ACL_DISCONNECTED`, and
  `BluetoothDevice.ACTION_BOND_STATE_CHANGED` refresh status.

Do not rely on Bluetooth ACL state alone. A device can be paired or briefly
connected without being usable as an input device. The green state should mean
the reader is available as a hardware input source, or a recent successful test
read proved it is working.

Recommended states:

```text
READY
  Input device present, or recent successful test read from known reader.
  Show green "RFID reader ready".

PAIRED_NOT_READY
  Bonded Bluetooth device exists, but no matching input device is active.
  Show amber "Reader paired, not connected".

NOT_PAIRED
  No matching paired reader. Show "Pair reader".

PERMISSION_NEEDED
  Android 12+ Nearby Devices permission is missing for Bluetooth state.
  Ask permission before showing paired-device state.

BLUETOOTH_OFF
  Bluetooth adapter disabled. Prompt operator to enable Bluetooth.
```

## RFID Button Behavior

The Bluetooth/RFID button on the scan or RFID settings surface should behave as
follows:

```text
READY
  show green status
  optional action: run a test-read prompt

PERMISSION_NEEDED
  request the relevant runtime permission

BLUETOOTH_OFF
  launch system Bluetooth enable/settings flow

PAIRED_NOT_READY or NOT_PAIRED
  open Android Bluetooth settings or pairing settings
  explain: pair/connect the RFID reader, then return to Goat OS
```

A normal app should not use hidden/private Android APIs to force-connect a HID
keyboard. Android owns HID keyboard connection and reconnection. Goat OS observes
status, guides the operator to the system pairing flow, and turns green when the
reader is actually ready.

If a future reader exposes a public BLE GATT/SPP/vendor SDK protocol, implement
that as a separate adapter behind `RfidReaderPort`; do not mix SDK calls into
feature screens.

## Scan Capture

Capture RFID tag reads at the activity/screen input layer:

```text
RFID reader scan
  -> Android hardware key events
  -> RfidKeyboardCapture buffers characters
  -> Enter/Tab/newline completes the read
  -> RfidTagScanned(tag)
  -> ScanViewModel matches roster primaryTag or secondaryTag
  -> local scan_event + live feed + FeedbackPort haptic/tone
```

Preferred implementation:

- `MainActivity.dispatchKeyEvent(...)` delegates to a lifecycle-scoped
  `RfidKeyboardCapture` only when the active route accepts RFID input.
- Intercept before calling `super.dispatchKeyEvent(...)`. Do not implement the wedge only through
  `Activity.onKeyDown`/`onKeyUp`: a focused Compose control can consume the Enter terminator during
  view dispatch first, turning a completed RFID read into an unintended click or Back navigation.
- The capture path ignores key events when a real editable field is focused
  (search, remarks, OTP, etc.).
- Buffer only printable tag characters. For current tags, digits are enough;
  keep the parser configurable for future alphanumeric IDs.
- Complete a tag on Enter, Tab, or newline.
- Clear a partial buffer after a short timeout, for example 500-750 ms, so a
  stray manual hardware key does not become a fake RFID scan.
- Do not let the scan screen create or depend on a hidden `EditText`.

Example shape:

```kotlin
override fun dispatchKeyEvent(event: KeyEvent): Boolean {
    if (rfidKeyboardCapture.onKeyEvent(event, activeRoute)) return true
    return super.dispatchKeyEvent(event)
}
```

The capture object emits a `SharedFlow<RfidRead>` or calls a route-scoped
callback. The feature layer receives an intent such as:

```kotlin
data class RfidTagScanned(
    val tag: String,
    val deviceName: String? = null,
    val capturedAtDeviceMs: Long,
)
```

## Matching And Feedback

The device provides only an identifier string. It does not decide eligibility.

The app must match the read tag against the cached backend roster for the active
shed:

```text
primaryTag == scanned tag
or
secondaryTag == scanned tag
```

Then:

- eligible/due: write local `scan_event`, update the unsynced overlay, show green
  feed row, fire confirm haptic/tone.
- already done: treat as an idempotent local duplicate, show non-destructive
  already-scanned feedback.
- known but not due in this shed: show red not-due state using backend-provided
  reason, fire double buzz and alert tone.
- unknown tag: show red unknown-tag state; never create a new animal from a scan.

The backend revalidates on submit. The app's local state is a draft UX overlay,
not business truth.

## Port Boundary

`device-rfid` owns Android APIs and vendor details. Feature modules do not import
Bluetooth, InputManager, SDK, or Android hardware APIs directly.

Recommended port shape:

```kotlin
interface RfidReaderPort {
    val status: StateFlow<RfidReaderStatus>
    val reads: SharedFlow<RfidRead>
    fun refreshStatus()
    fun openSystemPairing()
    fun startTestRead()
    fun stopTestRead()
}
```

Adapters:

```text
KeyboardWedgeRfidReader
  V1 default. Uses InputManager + Activity key-event capture.

FakeRfidReader
  Tests, previews, local demo. Emits configured tag strings.

VendorSdkRfidReader
  Future only. Uses vendor AAR/BLE when a specific reader requires it or when
  we need battery/signal/config/trigger control.
```

## Acceptance Tests

Manual field test on Android 10+:

1. Pair the reader in Android Bluetooth settings.
2. Confirm it types a full tag plus Enter into Notes.
3. Open Goat OS scan screen.
4. Confirm no soft keyboard appears and no edit field is visible.
5. Scan a due animal tag; row updates, count increments, green feedback fires.
6. Scan a second tag for the same animal if present; it matches the same animal.
7. Scan an unknown/not-due tag; red feedback plus alert tone fires.
8. Turn the reader off; status changes away from green.
9. Tap RFID/Bluetooth button; app opens the system Bluetooth pairing/settings
   flow or asks required permission.
10. Reconnect reader; app returns to green without restarting.

Automated tests:

- parser unit tests: digit buffering, Enter completion, timeout reset, duplicate
  Enter, non-printable key ignore.
- ViewModel tests: due, already-done, not-due, unknown tag.
- fake reader tests: emitted tags drive the same scan path as hardware reads.
- route gating test: RFID key events are consumed on scan/test-read screens and
  ignored in OTP/search/remarks fields.
- dispatch-order test: an RFID-consumed Enter never reaches the Compose view tree, while an ordinary
  key that the capture rejects still falls through to normal Activity dispatch.
- device test: `adb shell input text <known-tag>` followed by `adb shell input keyevent 66` completes
  the scan and leaves the operator on the same scan route.
