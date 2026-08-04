#!/usr/bin/env python3
"""Add the CPT kids from the "RFID source of truth" sheet into CBE-CPT-goats.json.

Source: spreadsheet 1FulMrlb8_AGwL5nFwoORACnbDstSFMg-GCPaKIZnnF8, tab `Combined`
(Farm, Old ID, Old ID Suffix, RFID, Age, Gender, Breed, Tag, Shed, Partition).

Shed naming
-----------
The sheet writes shed and partition in two columns; this file embeds the
partition in `shed` ("Godel 1 - Part 1"), so they are recombined.

The sheet's `New Yashoda` is CPT `Yashoda <partition>`.  There is no shed called
"New Yashoda" in `locations`, and the Counting-DB FutureDB tab lists CPT
Yashoda 1..4.  The mapping is pinned by Part 3: it holds exactly 2 animals, the
only two `K1` rows in the whole sheet, and FutureDB has `Yashoda 3 = K1 x2`.
Part 1 matches on count as well (2 = 2).

Shed tag / stage
----------------
FutureDB still decides the cohort FAMILY, as it does for the rest of the file.
Two corrections are applied on top, because FutureDB's shed-level tag disagrees
with the actual animals in 8 of the 14 kid sheds:

* FutureDB gives an ADULT tag for three sheds that hold only kids (Godel 2
  Part 4, Mandela 2 Part 7, Mandela 2 Part 9 -> `Non-Pregnant`).  `Non-Pregnant`
  is a female reproductive stage and 33 of those 48 kids are male, so the
  sheet's own `F2` is used instead.
* `F2-Female` / `F2-Male` is a gendered tag, and FutureDB has it backwards for
  whole sheds (Mandela 1 Part 7 is 13 females tagged `F2-Male`).  The F2 family
  is kept from FutureDB; the gender half comes from the animal.

Consequence, stated rather than hidden: three partitions (Mandela 1 Part 1,
Mandela 2 Part 5, Mandela 2 Part 6) genuinely hold both sexes, so they carry
both `F2-Female` and `F2-Male` and the one-tag-per-partition invariant does not
hold for them.  Everything else stays single-tag.

Not invented: dob, origin, purchase, weights and history are absent from the
sheet and stay blank.  `status` is `Alive` per the maintainer instruction that
every animal in these files is live.

Usage:  python3 docs/runbooks/add-cpt-kids.py [--apply]
"""
import json
import sys
import collections

SCRATCH = ("/private/tmp/claude-501/-Users-manoharchowdary-Desktop-mesha-goatos/"
           "43594e37-47f7-4ccf-a60e-509a9f380b6f/scratchpad")
TARGET = "docs/runbooks/CBE-CPT-goats.json"

# stages that describe an adult; a Kid must never carry one
ADULT_TAGS = {"Non-Pregnant", "Mother", "Buck", "warmup"}


def clean(x):
    x = "" if x is None else str(x).strip()
    return "" if x in ("None", "-", "—") else x


def main(apply_changes):
    doc = json.load(open(TARGET))
    rows = doc["values"]
    header, data = rows[0], rows[1:]
    H = {h: i for i, h in enumerate(header)}

    def jg(row, key):
        v = row[H[key]]
        return "" if v is None else str(v).strip()

    existing = {jg(r, "rfid") for r in data if jg(r, "rfid")}

    sheet = json.load(open(f"{SCRATCH}/sot_combined.json"))["values"]
    S = {h.strip(): i for i, h in enumerate(sheet[0])}

    def sg(row, key):
        i = S.get(key)
        return "" if i is None or i >= len(row) else clean(row[i])

    # FutureDB tag per (farm, shed) -- same source the rest of the file uses
    fut = json.load(open(f"{SCRATCH}/futuredb.json"))["values"]
    fh = fut[0]
    FUT_TAG = {}
    for r in fut[1:]:
        d = dict(zip(fh, r + [""] * (len(fh) - len(r))))
        farm, shed, tag = clean(d.get("Farm")).upper(), clean(d.get("Shed")), clean(d.get("Shed Tag"))
        if farm and shed and tag:
            FUT_TAG.setdefault((farm, shed), tag)

    def resolve_shed(shed, partition):
        shed, partition = clean(shed), clean(partition)
        if shed == "New Yashoda":
            return f"Yashoda {partition}" if partition else shed
        if partition and " Part " not in shed:
            return f"{shed} - Part {partition}"
        return shed

    new_rows, tag_conflicts, skipped = [], collections.Counter(), collections.Counter()
    stats = collections.Counter()

    for r in sheet[1:]:
        if sg(r, "Farm") != "CPT" or sg(r, "Age") != "Kid":
            continue
        rfid = sg(r, "RFID")
        if not rfid:
            skipped["no rfid"] += 1
            continue
        if rfid in existing:
            skipped["already in file"] += 1
            continue

        old_id, suffix = sg(r, "Old ID"), sg(r, "Old ID Suffix")
        breed = sg(r, "Breed")
        shed = resolve_shed(sg(r, "Shed"), sg(r, "Partition"))
        goat_id = old_id or rfid
        gender = sg(r, "Gender")
        fut_tag = FUT_TAG.get(("CPT", shed), "")
        sheet_tag = sg(r, "Tag")

        if fut_tag in ADULT_TAGS:
            # a kid cannot carry an adult reproductive stage
            tag = sheet_tag
            tag_conflicts[f"{shed}: FutureDB {fut_tag!r} is an ADULT tag -> using sheet {sheet_tag!r}"] += 1
        elif fut_tag:
            tag = fut_tag
        else:
            tag = sheet_tag
            stats["no FutureDB tag, used sheet"] += 1

        if tag == "F2" or tag in ("F2-Female", "F2-Male"):
            # F2 is gendered; take the family from above and the sex from the animal
            if gender in ("Female", "Male"):
                corrected = f"F2-{gender}"
                if tag in ("F2-Female", "F2-Male") and tag != corrected:
                    tag_conflicts[f"{shed}: FutureDB {tag!r} but animal is {gender} -> {corrected!r}"] += 1
                tag = corrected
            else:
                stats["** F2 with blank gender, tag left as F2"] += 1
                tag = "F2"

        row = {h: "" for h in header}
        row.update({
            "rfid": rfid,
            "goat_id": goat_id,
            "farm_goat_id": "CPT" + goat_id,
            "old_id": old_id,
            "old_id_suffix": suffix,
            "farm": "CPT",
            "shed": shed,
            "shed_tag": tag,
            "stage": tag,
            "age": "Kid",
            "breed": breed,
            "gender": sg(r, "Gender"),
            "species": "Sheep" if "sheep" in breed.lower() else "Goat",
            "status": "Alive",
        })
        new_rows.append([row[h] for h in header])
        existing.add(rfid)
        stats["added"] += 1
        if not sg(r, "Gender"):
            stats["** blank gender (sex is NOT NULL)"] += 1

    print(f"CPT kids added : {stats['added']}")
    for k, n in skipped.items():
        print(f"  skipped {k:<22}{n}")
    for k, n in stats.most_common():
        if k != "added":
            print(f"  {k:<30}{n}")
    if tag_conflicts:
        print("\n  shed tag: FutureDB kept over the sheet's own Tag ->")
        for k, n in tag_conflicts.most_common():
            print(f"     {n:>4}  {k}")

    by_shed = collections.Counter(r[H["shed"]] for r in new_rows)
    print("\n  placement:")
    for s, n in sorted(by_shed.items()):
        print(f"     {s:<24}{n:>4}")

    if apply_changes:
        doc["values"] = [header] + data + new_rows
        json.dump(doc, open(TARGET, "w"), indent=1)
        print(f"\nwrote {TARGET}: {len(data)} -> {len(data)+len(new_rows)} rows")
    else:
        print("\n(dry run -- pass --apply to write)")


if __name__ == "__main__":
    main("--apply" in sys.argv)
