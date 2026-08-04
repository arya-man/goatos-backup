#!/usr/bin/env python3
"""Apply the CPT vaccination-drive shed rosters to CBE-CPT-goats.json.

Source: CPT_VaccinationDrive_ShedRoaster.pdf -- three per-animal rosters,
New Yashoda (85), Mandela 1 (101) and Mandela 2 (38), each listing identifier,
breed, gender and partition.  All three sections parse to exactly their declared
totals, and all 218 resolvable animals already exist in the file, so this
CORRECTS PLACEMENT ONLY -- it adds nobody.

Why the roster wins over the file's current shed
------------------------------------------------
The CPT kid placement in the file came from the source-of-truth sheet, whose
shed/partition columns are stale: the file had Yashoda 4 holding 82 animals
against FutureDB's 28, while Mandela 1 Parts 2/3/4/5/9 sat empty against
FutureDB's 10-14 each.  Replaying the roster moves total absolute error against
the FutureDB headcount from 241 to 81 and lands eight sheds exactly.  The roster
is per-animal truth; FutureDB is a shed-level count; the sheet is neither.

Shed naming: the roster's `New Yashoda` + `Part N` is CPT `Yashoda N` (the
mapping already used for the kid import).  `Mandela 1/2` + `Part N` becomes
`Mandela 1/2 - Part N`.

Deliberately NOT changed
------------------------
* `shed_tag`/`stage`.  This is a correction of a mis-recorded shed, not a
  movement, so no cohort is being adopted.  The animals carry gendered F2 tags
  that belong to the animal rather than to the destination, and rewriting them
  from the destination would invent a stage change that never happened.  Any
  resulting mixed-tag partition is reported, not silently smoothed.
* `breed`/`gender` where the file already has a value.  The roster agrees with
  the file on every one of the 218 matches (0 disagreements), so there is
  nothing to reconcile; blanks are filled, existing values are left alone.

The 5 `N/A` roster entries carry no identifier and are skipped -- they cannot be
matched, and inventing rows for them would double-count animals that may already
be in the file under a tag the roster omitted.  One duplicated roster line
(`1041 BLR`, listed twice under Mandela 2 Part 4) is applied once.

Usage:  python3 docs/runbooks/apply-cpt-shed-roster.py [--apply]
"""
import collections
import json
import re
import sys

SCRATCH = ("/private/tmp/claude-501/-Users-manoharchowdary-Desktop-mesha-goatos/"
           "43594e37-47f7-4ccf-a60e-509a9f380b6f/scratchpad")
TARGET = "docs/runbooks/CBE-CPT-goats.json"
ROSTER = f"{SCRATCH}/roster_parsed.json"

RFID_RE = re.compile(r"\d{15}")


def shed_of(entry):
    if entry["roster_shed"] == "New Yashoda":
        return f"Yashoda {entry['part']}"
    return f"{entry['roster_shed']} - Part {entry['part']}"


def main(apply_changes):
    doc = json.load(open(TARGET))
    header, data = doc["values"][0], doc["values"][1:]
    H = {h: i for i, h in enumerate(header)}

    def g(row, key):
        v = row[H[key]]
        return "" if v is None else str(v).strip()

    by_rfid = {g(r, "rfid"): r for r in data if g(r, "rfid")}
    by_tag = collections.defaultdict(list)
    for r in data:
        if g(r, "farm").upper() == "CPT" and g(r, "old_id"):
            by_tag[(g(r, "old_id"), g(r, "old_id_suffix"))].append(r)

    roster = json.load(open(ROSTER))

    moves, stats, filled, seen = collections.Counter(), collections.Counter(), collections.Counter(), set()
    for e in roster:
        ident = e["ident"]
        if ident == "N/A":
            stats["skipped: roster row has no identifier"] += 1
            continue
        if ident in seen:
            stats["skipped: duplicate roster line"] += 1
            continue
        seen.add(ident)

        if RFID_RE.fullmatch(ident):
            row = by_rfid.get(ident)
        else:
            p = ident.split()
            hits = by_tag.get((p[0], p[1] if len(p) > 1 else ""), [])
            if len(hits) > 1:
                stats["skipped: tag matches more than one animal"] += 1
                continue
            row = hits[0] if hits else None
        if row is None:
            stats["skipped: not in the file"] += 1
            continue

        want = shed_of(e)
        if g(row, "shed") != want:
            moves[f"{g(row,'shed')} -> {want}"] += 1
            row[H["shed"]] = want
            stats["shed corrected"] += 1
        else:
            stats["shed already correct"] += 1

        for col, val in (("breed", e["breed"]), ("gender", e["gender"])):
            if val and not g(row, col):
                row[H[col]] = val
                filled[col] += 1

    print(f"roster entries: {len(roster)}")
    for k, n in stats.most_common():
        print(f"   {n:>5}  {k}")
    for k, n in filled.items():
        print(f"   {n:>5}  blank {k} filled from the roster")
    print(f"\nshed corrections ({sum(moves.values())}):")
    for k, n in moves.most_common():
        print(f"   {n:>5}  {k}")

    # mixed-tag partitions created or left behind, stated rather than smoothed
    mix = collections.defaultdict(collections.Counter)
    for r in data:
        if g(r, "farm").upper() == "CPT":
            mix[g(r, "shed")][g(r, "shed_tag") or "(blank)"] += 1
    mixed = {s: dict(t) for s, t in mix.items() if len(t) > 1}
    print(f"\nCPT partitions holding more than one shed tag: {len(mixed)}")
    for s, t in sorted(mixed.items()):
        print(f"   {s:<24}{t}")

    if apply_changes:
        json.dump(doc, open(TARGET, "w"), indent=1)
        print(f"\nwrote {TARGET}")
    else:
        print("\n(dry run -- pass --apply to write)")


if __name__ == "__main__":
    main("--apply" in sys.argv)
