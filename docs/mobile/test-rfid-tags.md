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

## Operator assignment is required for the scan test (local-seed gap)

The operator scan screen is reached **operator → Drives → shed → scan**, and the
operator sheds list is **assignment-scoped**: it reads
`vaccination_drive_assignments.operator_id = <the operator's workforce_member>`.
Leadership (CEO/Director/Park Head) get an **unscoped, read-only** park view of all
sheds and never reach scan; a field **operator only sees sheds a drive assigned to
them**.

**Consequence for a fresh local/dev seed:** if the source seed generated
obligations but the operator-drive step never ran, `vaccination_drive_assignments`
is **empty**, every drive shows `blocked — operator assignment required`, and
**every** operator's sheds list (and therefore the scan screen) is empty — even
though the CEO oversight view shows the sheds. This is a seeding gap, not an app
bug.

To make the operator scan test work locally you need drive **assignments**:

- Wire the operators as logins (Amit Kumar, Sagar Mahoor, Darshan Talwar are the
  CPT vaccination operators) with a `preventive_care` department binding and an
  `operator_assignment_config` (per-operator daily cap), then run the
  obligation sweeper / `recompute-vaccination-drives` so it plans drives and
  assigns operators. `make seed-vaccination-cpt-operator-drive` seeds this whole
  operator-drive shape.
- **Operator cap matters:** with `active_operators_per_day = 1`, one operator
  (e.g. Darshan) takes every day's assignment, so **Darshan always has the
  assignment** and the shed **switch** flow is exercised on his device. Raise the
  cap (2–3 equal operators, cap ~200) to split sheds/partitions across
  Amit/Sagar/Darshan and test the multi-operator split + per-operator scan roster.
- Seed one of the [Tag IDs](#tag-ids) above as `goat_identifiers.identifier_value`
  (`identifier_type = animal_identifier_1`, `is_primary_for_goat`) on a goat that
  sits in the assigned shed and carries a due vaccination obligation, so the
  scanned tag resolves to that shed's roster row.

## Regression guard note

The scan roster keys each row by its unique `obligationId` (not `goatId`), so a
two-vaccine goat renders as two distinct rows without a duplicate-key crash.
Keep that invariant when editing `ScanScreen.kt`.
