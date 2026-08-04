#!/usr/bin/env python3
"""Populate CBE Castro 1/2/3 with assumed IDs (maintainer, 2026-08-03).

Castro is the largest hole between the FutureDB headcount (63/73/64 = 200) and
the identity file, which holds zero Castro animals.  The instruction is to use
assumed IDs and fill what the schema requires from the shed, leaving optional
columns null.

Why every one of the 200 is a placeholder
-----------------------------------------
The source-of-truth sheet DOES list 30 CBE animals under Castro 1/2, and 26 of
them are already in the file under other sheds (Yashoda 1/2/3/4/10, Godel 2
Part 3).  Moving them into Castro was tested and REJECTED -- their current
sheds already match FutureDB, so the move would empty out sheds that are
currently correct:

    source shed        json  future  would lose  after
    Yashoda 2            19      20           9     10
    Godel 2 - Part 3      9       9           8      1
    Yashoda 4            17      17           6     11
    Yashoda 10           15      18           1     14
    Yashoda 3            11      11           1     10

So the sheet's Castro column is stale history and the snapshot's placement is
current: those 26 animals have left Castro.  The remaining 4 sheet identities
are absent from the file entirely; they are NOT imported here either, because
the same staleness applies to their shed and placing them in Castro on that
evidence would be a guess.  They stay with the separate 86-row import question.

Assumed ID format
-----------------
`CASTRO<shed>-<seq>` in `goat_id`/`old_id`, with `old_id_suffix = TEMP`.
Deliberately NOT a fabricated 15-digit RFID -- `rfid` is left blank exactly like
the 36 untagged animals already in the file, so nothing downstream mistakes an
invented number for a scanned tag, and the `TEMP` suffix makes every placeholder
greppable when real tags arrive.

Required columns, from the shed
-------------------------------
All three Castro rows in FutureDB are homogeneous (`F2-Male` / `Anantapur Sheep`
/ `Kid`), so:

    gender  Male     (from the F2-Male shed tag)
    age     Kid
    stage   F2-Male
    status  Alive
    health  Healthy

BREED AND SPECIES ARE THE ONE EXCEPTION, called out rather than assumed
silently.  FutureDB labels the whole shed `Anantapur Sheep`, but the 30 Castro
animals whose identity IS known are mixed -- 22 goat (Beetal, Sojat, Malai,
Beetal x Sojat) against 8 Anantapur Sheep.  Taking the shed label would file
~145 goats as sheep, and species drives vaccination rules.  So `breed` is left
BLANK (nullable) and `species` -- NOT NULL, with no "unknown" value available --
takes the observed majority, `Goat`.  Flip SPECIES_OVERRIDE if the field team
says the shed really is sheep.

Everything optional stays blank: dob, origin_type, purchase/vendor/load, mother,
weights, deliveries, disease and event history.

Usage:  python3 docs/runbooks/fill-castro.py [--apply]
"""
import collections
import json
import sys

TARGET = "docs/runbooks/CBE-CPT-goats.json"

FARM = "CBE"
# FutureDB 04/08/2026 headcount per Castro shed
TARGET_COUNT = {"Castro 1": 63, "Castro 2": 73, "Castro 3": 64}

# shed-derived required values (identical across all three FutureDB Castro rows)
SHED_TAG = "F2-Male"
AGE = "Kid"
GENDER = "Male"
SPECIES_OVERRIDE = "Goat"   # see docstring; `Sheep` if the shed label wins
STATUS = "Alive"
HEALTH = "Healthy"

PLACEHOLDER_SUFFIX = "TEMP"


def main(apply_changes):
    doc = json.load(open(TARGET))
    header, data = doc["values"][0], doc["values"][1:]
    H = {h: i for i, h in enumerate(header)}

    def g(row, key):
        v = row[H[key]]
        return "" if v is None else str(v).strip()

    present = collections.Counter()
    for r in data:
        if g(r, "farm").upper() == FARM and g(r, "shed") in TARGET_COUNT:
            present[g(r, "shed")] += 1

    taken = {(g(r, "old_id"), g(r, "old_id_suffix")) for r in data}
    new_rows, generated = [], collections.Counter()

    for shed, want in sorted(TARGET_COUNT.items()):
        short = want - present[shed]
        if short <= 0:
            continue
        stub = "CASTRO" + shed.split()[-1]
        seq = 0
        for _ in range(short):
            while True:
                seq += 1
                gid = f"{stub}-{seq:03d}"
                if (gid, PLACEHOLDER_SUFFIX) not in taken:
                    break
            taken.add((gid, PLACEHOLDER_SUFFIX))
            row = {h: "" for h in header}
            row.update({
                "goat_id": gid, "farm_goat_id": FARM + gid,
                "old_id": gid, "old_id_suffix": PLACEHOLDER_SUFFIX,
                "farm": FARM, "shed": shed,
                "shed_tag": SHED_TAG, "stage": SHED_TAG,
                "age": AGE, "gender": GENDER, "species": SPECIES_OVERRIDE,
                "status": STATUS, "health_status": HEALTH,
            })
            new_rows.append([row[h] for h in header])
            generated[shed] += 1

    print(f"assumed-ID animals generated: {sum(generated.values())}")
    for shed in sorted(TARGET_COUNT):
        n = generated[shed]
        rng = f"CASTRO{shed.split()[-1]}-001 .. -{n:03d}" if n else "-"
        print(f"   {shed:<12}already {present[shed]:>3}  + new {n:>3}"
              f"  = {present[shed]+n:>3} / {TARGET_COUNT[shed]:<4}"
              f"{'ok' if present[shed]+n == TARGET_COUNT[shed] else 'MISMATCH'}   {rng}")
    print(f"\n   every generated row: gender={GENDER} species={SPECIES_OVERRIDE} "
          f"breed=(blank) age={AGE} stage={SHED_TAG} status={STATUS} health={HEALTH}")
    print(f"   rows {len(data)} -> {len(data)+len(new_rows)}")

    if apply_changes:
        doc["values"] = [header] + data + new_rows
        json.dump(doc, open(TARGET, "w"), indent=1)
        print(f"wrote {TARGET}")
    else:
        print("   (dry run -- pass --apply to write)")


if __name__ == "__main__":
    main("--apply" in sys.argv)
