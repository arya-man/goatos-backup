# Test RFID Tags (throwaway, for scan/BLE screen testing)

Physical RFID ear-tag IDs held by the maintainer for testing the Android
keyboard-wedge RFID reader flow (the scan screen, `feature-scan/ScanScreen.kt`,
driven by `KeyboardWedgeRfidReader` — see
[rfid-keyboard-reader.md](rfid-keyboard-reader.md)).

These are **throwaway test tags only** — not production livestock identity. Use
them to exercise the scan screen end-to-end with a real Bluetooth HID reader, or
to seed a local/throwaway DB so a scanned tag resolves to a roster row.

## Tag IDs

| # | RFID tag |
|---|----------|
| 1 | `901007000504418` |
| 2 | `901007000504332` |
| 3 | `901007000504407` |
| 4 | `901007000504419` |
| 5 | `901007000504392` |

Machine-readable copy (one per line) lives next to this doc at
[`test-rfid-tags.txt`](test-rfid-tags.txt) for seed scripts.

## How they are used

- **Physical scan test:** pair the BT reader, open the operator scan screen for a
  shed drive, and scan a tag. The reader types the digits + Enter;
  `RfidKeyboardCapture` completes the read on Enter and the roster row for that
  tag flips to done.
- **Local seed:** to make a scanned tag resolve, seed one of these IDs as a
  goat's RFID in the throwaway local DB and place it in the shed drive under
  test. A goat carrying **two due vaccines** in one drive (e.g. `ET+TT` + `PPR`)
  is the regression case for the duplicate-`LazyColumn`-key crash fixed in
  `a9c35a1d` (Crashlytics `IllegalArgumentException: Key "<uuid>" was already
  used`) — the roster shows that goat on two rows, so this is the scenario to
  re-scan when verifying the scan screen no longer pops back.

## Regression guard note

The scan roster keys each row by its unique `obligationId` (not `goatId`), so a
two-vaccine goat renders as two distinct rows without a duplicate-key crash.
Keep that invariant when editing `ScanScreen.kt`.
