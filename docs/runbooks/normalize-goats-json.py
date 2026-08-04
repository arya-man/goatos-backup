#!/usr/bin/env python3
"""Normalize the CBE/CPT herd JSON toward DB-loadable values.

Maintainer instruction 2026-08-03: every animal in these files is live, and
`Purchase` is the source label for what the schema calls `procured`.

Two changes, both idempotent:

1. blank `status` -> `Alive`.  `goats.lifecycle_status` is NOT NULL, so a blank
   status cannot be inserted at all.
2. `origin_type` `Purchase` -> `Procured`.  `goats_origin_type_check` allows
   only birth|procured|imported.
3. `Sale`/`Death` -> `Alive`, clearing `death_date`/`death_reason`/`sale_date`/
   `sale_reason` with them (maintainer, 2026-08-03).  The exit columns are stale
   on the file's own evidence: 1706 CBE "died" 2025-11-03 yet was weighed
   2026-07-06 and treated for fever 2026-07-23 with `animal_status ACTIVE`, 830
   CJB "died" 2025-12-10 yet delivered kids 2025-12-28, and 847 CJB was "sold"
   2025-08-28 yet delivered kids 2026-04-28.  Leaving the dates behind while the
   status says Alive would let a loader materialize `exited_at`/`exit_reason`
   and trip `goats_exited_lifecycle_check` from the opposite direction.  Prior
   values stay recoverable in git history.
4. clear `rfid` when it is not a 15-digit RFID.  Godel 2 Parts 6/7/8 hold
   animals that are not RFID-tagged yet and legitimately carry only an old tag
   (maintainer, 2026-08-03), but the old tag had been copied into the `rfid`
   column.  Left there, the loader mints a 3-digit value as a real RFID
   identifier.  Nothing is lost: those rows already carry the tag in `goat_id`,
   `old_id` (+ `old_id_suffix`) and `farm_goat_id`.

Old tags are only unique as `old_id` + `old_id_suffix` (bare `old_id` is shared
by 81 animals), so the loader must qualify them with the suffix.

Case note: these files are Title Case throughout (`Female`, `Goat`, `Alive`),
and the DB CHECKs are lower case, so the loader lower-cases every enum on the
way in.  `Procured` keeps that one convention rather than mixing a lone
lower-case token into an otherwise Title Case column.

Usage:  python3 docs/runbooks/normalize-goats-json.py FILE [FILE ...]
"""
import json
import re
import sys
import collections

EXIT_STATUSES = {"sale", "death"}
EXIT_COLUMNS = ("death_date", "death_reason", "sale_date", "sale_reason")
RFID_RE = re.compile(r"\d{15}")


def normalize(path):
    doc = json.load(open(path))
    rows = doc["values"]
    header, data = rows[0], rows[1:]
    H = {h: i for i, h in enumerate(header)}

    def get(row, key):
        v = row[H[key]]
        return "" if v is None else str(v).strip()

    changed = collections.Counter()
    flipped_exits = []

    for row in data:
        # 1. blank status -> Alive
        if not get(row, "status"):
            row[H["status"]] = "Alive"
            changed["status blank -> Alive"] += 1

        # 3. explicit Sale/Death -> Alive, and drop the stale exit facts
        elif get(row, "status").lower() in EXIT_STATUSES:
            was = get(row, "status")
            cleared = {c: get(row, c) for c in EXIT_COLUMNS if c in H and get(row, c)}
            row[H["status"]] = "Alive"
            for c in cleared:
                row[H[c]] = ""
            flipped_exits.append((was, get(row, "farm"), get(row, "shed"),
                                  get(row, "rfid") or get(row, "old_id"), cleared))
            changed[f"status {was} -> Alive"] += 1

        # 2. Purchase -> Procured (applies to every row, exit or not)
        if get(row, "origin_type").lower() == "purchase":
            row[H["origin_type"]] = "Procured"
            changed["origin_type Purchase -> Procured"] += 1

        # 4. an old tag parked in the rfid column is not an RFID
        rfid = get(row, "rfid")
        if rfid and not RFID_RE.fullmatch(rfid):
            if get(row, "goat_id") != rfid or get(row, "old_id") != rfid:
                raise SystemExit(
                    f"refusing to clear rfid {rfid!r}: goat_id/old_id do not "
                    f"already carry it, so the identity would be lost"
                )
            row[H["rfid"]] = ""
            changed["non-RFID cleared from rfid column"] += 1

        # Deliberately NOT filling blank `farm_goat_id` from farm + goat_id.
        # For old-tag animals `goat_id` is the bare tag, and bare tags are not
        # unique -- `344CJB` and `344BLR` are two different animals that both
        # render as `CBE344`.  Doing that fill took duplicate farm_goat_id from
        # 2 to 9.  Any future fill has to carry `old_id_suffix`.

    json.dump(doc, open(path, "w"), indent=1)

    print(f"\n{path}  ({len(data)} rows)")
    for k, n in changed.most_common():
        print(f"   {n:>5}  {k}")
    if not changed:
        print("   (already normalized)")
    for was, farm, shed, ident, cleared in flipped_exits:
        print(f"      {was:<6}{farm:<5}{shed:<24}{ident:<18}cleared {cleared}")
    return changed


if __name__ == "__main__":
    for p in sys.argv[1:]:
        normalize(p)
