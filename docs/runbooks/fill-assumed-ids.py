#!/usr/bin/env python3
"""Fill sheds that have no identity source with assumed IDs (maintainer, 2026-08-03).

Generalises the CBE Castro fill to any shed where the FutureDB headcount exists
but no per-animal identity does.  Required columns are taken from that shed's
FutureDB row; optional columns stay null.

Sheds filled by this run -- the five CPT sheds holding zero animals in the file:

    CPT Godel 2 - Part 1   38   F2-Male     Anantapur Sheep   Kid
    CPT Godel 2 - Part 2   38   F2-Male     Anantapur Sheep   Kid
    CPT Godel 2 - Part 3   37   F2-Female   Anantapur Sheep   Kid
    CPT Castro 1           33   F2-Male     Anantapur Sheep   Kid
    CPT Castro 2           31   F2-Male     Anantapur Sheep   Kid

Sex comes from the shed tag (`F2-Male` -> Male, `F2-Female` -> Female), age and
breed from the same row.

Breed/species, unlike CBE Castro
--------------------------------
The CBE Castro fill left `breed` blank and forced `species = Goat`, because the
30 known animals in those sheds were 22 goat against 8 sheep and the shed label
would have mislabelled most of them.  That does NOT apply here: every animal the
source-of-truth sheet places in these five CPT sheds is Anantapur Sheep, so the
shed label is corroborated rather than contradicted, and `breed = Anantapur
Sheep` / `species = Sheep` is written directly.

The CPT Castro age/sex contradiction
------------------------------------
The source sheet lists CPT Castro as 50 ADULT FEMALES; FutureDB says KID /
F2-Male.  FutureDB is used, on the same evidence that settled CBE Castro: 45 of
those 50 sheet animals are already in the file under other sheds whose counts
already match FutureDB, so the sheet's Castro placement is stale history.  A
later roster for these sheds should overwrite these placeholders wholesale.

Assumed ID format
-----------------
`<SHEDCODE>-<seq>` in `goat_id`/`old_id`, with `old_id_suffix = TEMP-<FARM>`.
The farm is in the SUFFIX rather than the id because `CASTRO1-001` exists at
both farms and the loader's identity key is (`old_id`, `old_id_suffix`) -- a
bare-tag collision across farms would otherwise merge two different animals.
This run also renormalises the 200 CBE Castro placeholders from `TEMP` to
`TEMP-CBE` so both farms follow one rule.

`rfid` stays blank: these animals are untagged, and minting a fake 15-digit
value would be indistinguishable from a real scan.

Usage:  python3 docs/runbooks/fill-assumed-ids.py [--apply]
"""
import collections
import json
import sys

SCRATCH = ("/private/tmp/claude-501/-Users-manoharchowdary-Desktop-mesha-goatos/"
           "43594e37-47f7-4ccf-a60e-509a9f380b6f/scratchpad")
TARGET = "docs/runbooks/CBE-CPT-goats.json"
FUTUREDB = f"{SCRATCH}/futuredb_fresh.json"

# (farm, shed) -> short code used in the assumed ID
FILL = {
    ("CPT", "Godel 2 - Part 1"): "GODEL2P1",
    ("CPT", "Godel 2 - Part 2"): "GODEL2P2",
    ("CPT", "Godel 2 - Part 3"): "GODEL2P3",
    ("CPT", "Castro 1"): "CASTRO1",
    ("CPT", "Castro 2"): "CASTRO2",
}

SEX_FROM_TAG = {"F2-Male": "Male", "F2-Female": "Female"}
STATUS = "Alive"
HEALTH = "Healthy"

# the CBE Castro placeholders shipped with a bare `TEMP`; bring them onto the
# farm-qualified rule so `CASTRO1-001` cannot mean two animals
RENAME_SUFFIX = {"TEMP": "TEMP-CBE"}


def clean(x):
    x = "" if x is None else str(x).strip()
    return "" if x in ("None", "-", "—") else x


def load_futuredb():
    rows = json.load(open(FUTUREDB))["values"]
    head = rows[0]
    out = {}
    for r in rows[1:]:
        d = dict(zip(head, list(r) + [""] * (len(head) - len(r))))
        key = (clean(d["Farm"]).upper(), clean(d["Shed"]))
        if key not in FILL:
            continue
        if key in out:
            raise SystemExit(f"{key} has more than one FutureDB row; the shed is "
                             f"not homogeneous and cannot be filled from its label")
        out[key] = {
            "tag": clean(d["Shed Tag"]),
            "breed": clean(d["Breed"]),
            "age": clean(d["Age"]),
            "count": int(float(clean(d["Count"]) or 0)),
        }
    missing = set(FILL) - set(out)
    if missing:
        raise SystemExit(f"no FutureDB row for {sorted(missing)}")
    return out


def main(apply_changes):
    spec = load_futuredb()
    doc = json.load(open(TARGET))
    header, data = doc["values"][0], doc["values"][1:]
    H = {h: i for i, h in enumerate(header)}

    def g(row, key):
        v = row[H[key]]
        return "" if v is None else str(v).strip()

    renamed = collections.Counter()
    for r in data:
        new = RENAME_SUFFIX.get(g(r, "old_id_suffix"))
        if new:
            r[H["old_id_suffix"]] = new
            renamed[f"{g(r,'farm')} suffix -> {new}"] += 1

    present = collections.Counter((g(r, "farm").upper(), g(r, "shed")) for r in data)
    taken = {(g(r, "old_id"), g(r, "old_id_suffix")) for r in data if g(r, "old_id")}

    new_rows, generated = [], collections.Counter()
    for (farm, shed), code in sorted(FILL.items()):
        s = spec[(farm, shed)]
        sex = SEX_FROM_TAG.get(s["tag"])
        if not sex:
            raise SystemExit(f"{farm} {shed}: shed tag {s['tag']!r} does not imply a sex")
        species = "Sheep" if "sheep" in s["breed"].lower() else "Goat"
        suffix = f"TEMP-{farm}"
        short = s["count"] - present[(farm, shed)]
        if short <= 0:
            continue
        seq = 0
        for _ in range(short):
            while True:
                seq += 1
                gid = f"{code}-{seq:03d}"
                if (gid, suffix) not in taken:
                    break
            taken.add((gid, suffix))
            row = {h: "" for h in header}
            row.update({
                "goat_id": gid, "farm_goat_id": farm + gid,
                "old_id": gid, "old_id_suffix": suffix,
                "farm": farm, "shed": shed,
                "shed_tag": s["tag"], "stage": s["tag"],
                "age": s["age"], "gender": sex,
                "breed": s["breed"], "species": species,
                "status": STATUS, "health_status": HEALTH,
            })
            new_rows.append([row[h] for h in header])
            generated[(farm, shed)] += 1

    for k, n in renamed.most_common():
        print(f"   {n:>5}  renormalised: {k}")
    print(f"\nassumed-ID animals generated: {sum(generated.values())}")
    print(f"   {'farm':<5}{'shed':<20}{'have':>5}{'new':>5}{'total':>7}{'future':>8}  fields")
    for (farm, shed), code in sorted(FILL.items()):
        s = spec[(farm, shed)]
        n = generated[(farm, shed)]
        have = present[(farm, shed)]
        species = "Sheep" if "sheep" in s["breed"].lower() else "Goat"
        ok = "ok" if have + n == s["count"] else "MISMATCH"
        print(f"   {farm:<5}{shed:<20}{have:>5}{n:>5}{have+n:>7}{s['count']:>8}  "
              f"{SEX_FROM_TAG[s['tag']]}/{species}/{s['age']}/{s['tag']}  {ok}")
        print(f"        ids {code}-001 .. -{n:03d}  suffix TEMP-{farm}")
    print(f"\n   rows {len(data)} -> {len(data)+len(new_rows)}")

    if apply_changes:
        doc["values"] = [header] + data + new_rows
        json.dump(doc, open(TARGET, "w"), indent=1)
        print(f"   wrote {TARGET}")
    else:
        print("   (dry run -- pass --apply to write)")


if __name__ == "__main__":
    main("--apply" in sys.argv)
