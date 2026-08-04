#!/usr/bin/env python3
"""Add the missing CBE Castro animals and their ICU siblings (maintainer, 2026-08-04).

Maintainer count:

    Castro 2   74, plus 1 more C2 animal sitting in Yashoda 10 ICU
    Castro 3   65, plus 2 more C3 animals sitting in Yashoda 10 ICU

The file held Castro 2 = 73, Castro 3 = 64, and nothing in Yashoda 10
attributable to either, so five rows are added:

    CASTRO2-074   shed = Castro 2      (brings the shed to 74)
    CASTRO2-075   shed = Yashoda 10    (the C2 animal in ICU)
    CASTRO3-065   shed = Castro 3      (brings the shed to 65)
    CASTRO3-066   shed = Yashoda 10    (C3 in ICU)
    CASTRO3-067   shed = Yashoda 10    (C3 in ICU)

This SUPERSEDES the FutureDB 04/08/2026 headcounts of 73/64 hardcoded in
`fill-castro.py`, on the maintainer's direct instruction.

Corroboration: the three ICU rows take Yashoda 10 from 15 to 18, and 18 is
exactly the FutureDB Yashoda 10 headcount that `fill-castro.py` recorded the
file as short of.  Two independently-sourced counts landing on the same number
is the strongest evidence available here that these five animals are real.

Where the field values come from
--------------------------------
Within each Castro shed every existing row is byte-identical except for the ID
triple, so the template is not a judgement call -- it is copied wholesale from
the animal's own shed (C2 rows from Castro 2, C3 rows from Castro 3):

    shed_tag/stage  F2-Male      breed    Anantapur Sheep    species  Sheep
    gender          Male         age      Kid                dob      2026-04-13
    status          Alive        health   Healthy
    vacc_et_tt_dose1 / _booster / vacc_ppr_dose1 = "Done - date unknown"

The ICU rows override exactly two fields -- `shed`, and the `shed_tag`/`stage`
pair, which becomes `ICU-Kid` from the shed they sit in, the same "required
columns come from the shed" rule `fill-castro.py` used.  Nothing else is
invented: no diagnosis, no problem_name, no stage_entry_date, no weights.
`health_status` stays `Healthy` (the Castro template value) because ICU
placement is carried by the `ICU-Kid` stage, which is what the clinical defer
rules key on; marking them `Sick` with no reported problem would be a fabricated
clinical fact.

Why the ICU rows keep a CASTRO<n>- prefix
-----------------------------------------
The schema has NO home-shed / origin-shed column, so "belongs to Castro 3, parked
in Yashoda 10 ICU" is otherwise unrepresentable.  The prefix is the only place
that origin survives.  If they are meant to be counted as plain Yashoda 10
residents instead, rename them and this link is gone.

All five are placeholders in the established convention: blank `rfid`, with
`old_id_suffix = TEMP-CBE` so every assumed identity stays greppable when the
real tags arrive.

Usage:  python3 docs/runbooks/add-c2-y10-icu.py [--apply]
"""
import json
import sys

TARGET = "docs/runbooks/CBE-CPT-goats.json"

FARM = "CBE"

# (goat_id, template shed, destination shed, shed_tag override)
NEW = [
    ("CASTRO2-074", "Castro 2", "Castro 2",   None),
    ("CASTRO2-075", "Castro 2", "Yashoda 10", "ICU-Kid"),
    ("CASTRO3-065", "Castro 3", "Castro 3",   None),
    ("CASTRO3-066", "Castro 3", "Yashoda 10", "ICU-Kid"),
    ("CASTRO3-067", "Castro 3", "Yashoda 10", "ICU-Kid"),
]


def main(apply_changes):
    doc = json.load(open(TARGET))
    header, data = doc["values"][0], doc["values"][1:]
    H = {h: i for i, h in enumerate(header)}

    def g(row, key):
        v = row[H[key]]
        return "" if v is None else str(v).strip()

    # every row in a Castro shed is identical apart from the ID triple, so refuse
    # to run if that stops being true rather than picking one at random
    ident = {"goat_id", "farm_goat_id", "old_id"}
    templates = {}
    for shed in sorted({t for _, t, _, _ in NEW}):
        pool = [r for r in data
                if g(r, "farm").upper() == FARM and g(r, "shed") == shed]
        if not pool:
            raise SystemExit(f"no {shed} rows to copy from")
        for h in header:
            if h in ident:
                continue
            vals = {g(r, h) for r in pool}
            if len(vals) > 1:
                raise SystemExit(
                    f"{shed} is no longer homogeneous on `{h}` ({sorted(vals)}); "
                    "the template is a guess now -- resolve before adding rows")
        templates[shed] = list(pool[0])
        print(f"template: {len(pool):>3} identical {shed} rows")
    print()

    taken = {g(r, "goat_id") for r in data}
    new_rows = []
    for gid, src, shed, tag in NEW:
        if gid in taken:
            raise SystemExit(f"{gid} already exists -- refusing to duplicate")
        taken.add(gid)
        row = list(templates[src])
        row[H["goat_id"]] = gid
        row[H["farm_goat_id"]] = FARM + gid
        row[H["old_id"]] = gid
        row[H["shed"]] = shed
        if tag:
            row[H["shed_tag"]] = tag
            row[H["stage"]] = tag
        new_rows.append(row)
        print(f"   + {gid:<12} shed={shed:<12} tag={row[H['shed_tag']]:<8} "
              f"{row[H['gender']]}/{row[H['age']]} {row[H['breed']]} "
              f"rfid=(blank) suffix={row[H['old_id_suffix']]}")

    before, after = {}, {}
    for r in data:
        if g(r, "farm").upper() == FARM:
            before[g(r, "shed")] = before.get(g(r, "shed"), 0) + 1
    for r in data + new_rows:
        if g(r, "farm").upper() == FARM:
            after[g(r, "shed")] = after.get(g(r, "shed"), 0) + 1
    print()
    for shed in ("Castro 2", "Castro 3", "Yashoda 10"):
        print(f"   CBE {shed:<12} {before.get(shed, 0):>3} -> {after[shed]:>3}")
    print(f"   rows {len(data)} -> {len(data) + len(new_rows)}")

    if apply_changes:
        doc["values"] = [header] + data + new_rows
        json.dump(doc, open(TARGET, "w"), indent=1)
        print(f"wrote {TARGET}")
    else:
        print("   (dry run -- pass --apply to write)")


if __name__ == "__main__":
    main("--apply" in sys.argv)
