#!/usr/bin/env python3
"""Apply the CPT shiftings reported in Slack #cpt-shifting-reports since 17 Jul 2026.

Source: channel C09BMQVF8E7, 21 workflow messages between 2026-07-19 and
2026-08-02, each carrying Farm/From Shed/To Shed/Goats Count/Goat IDs as
structured attachment fields.

Which workflows are applied
---------------------------
Cancelled ones are ignored (maintainer, 2026-08-03).  The remaining 18 are
applied -- including the four that Slack still shows as un-terminated
("Shifting Workflow — WF-nnnn" with no completion post), because the FutureDB
headcount corroborates all four:

    applied                     CPT absolute error vs FutureDB
    nothing                                84
    completed only (38 animals)            78
    completed + open (70 animals)          22   <- 30 of 42 sheds land exactly

Each open workflow lands its own sheds exactly: WF-1893 puts Mandela 1 Part 2 on
12 and Part 5 on 14; WF-1895 puts Mandela 2 Part 3 on 19 and Part 1 on 2;
WF-1899 puts Yashoda 4 on 28.  A missing completion post is a reporting gap, not
evidence the animals stayed put.  This is historical reconciliation of a file,
NOT the live shifting workflow -- in the product an un-approved movement must
still never relocate an animal.

Order matters: several animals move twice (out to the Yashoda 1 ICU pen and back
once recovered), so workflows are replayed in timestamp order and the last
destination wins.

Stage
-----
Unlike the roster correction, this IS a movement, so the animal adopts the
destination shed's cohort per the confirmed shifting stage-selection rule.  The
destination tag comes from that shed's FutureDB row, which carries exactly one
tag per shed; a destination with no FutureDB row leaves the stage untouched
rather than guessing.

Two guards on that adoption, because a FutureDB shed tag describes the shed's
HEADLINE cohort and is wrong for the animals that were just moved in:

* `Mandela 2 - Part 3` is tagged `Buck` / `Adult`, which describes the single
  sheep buck that lives there -- but its count of 19 includes the weaned kids
  WF at 2026-07-22 20:23 moved in ("Shifting weaned kids. K3->F2").  Adopting it
  verbatim tags 18 female kids as breeding bucks.  So an ADULT reproductive tag
  (`Buck`, `Mother`, `Non-Pregnant`, `warmup`) is never applied to an animal
  whose age is `Kid`, `Buck` is never applied to a female, and a female-only
  stage is never applied to a male: those keep the stage they already carry.
  `Mandela 2 - Part 1` is the mirror case -- FutureDB tags it `Non-Pregnant`,
  but the CPT adult roster shows it is a sheep buck pen, and WF-1895 moves a
  buck into it.
* `F2` is gendered.  `Mandela 1 - Part 2` is tagged `F2-Female`, but the 11
  animals moved there are mixed.  The F2 FAMILY is taken from the destination
  and the sex half from the animal, the same rule the CPT kid import uses.

The Slack comments corroborate the destinations that survive both guards
("K1 -> K2", "K3->F2", "K0 -> Mother").

Identifiers
-----------
The Goat IDs column mixes 15-digit RFIDs with bare old tags.  Tags resolve
within CPT only, and only when exactly one animal carries them.  Anything else
is REPORTED AND SKIPPED, never guessed -- see the unresolved list in the output.

Usage:  python3 docs/runbooks/apply-cpt-slack-shiftings.py [--apply]
"""
import collections
import json
import re
import sys

SCRATCH = ("/private/tmp/claude-501/-Users-manoharchowdary-Desktop-mesha-goatos/"
           "43594e37-47f7-4ccf-a60e-509a9f380b6f/scratchpad")
TARGET = "docs/runbooks/CBE-CPT-goats.json"
SHIFTINGS = f"{SCRATCH}/cpt_shift_parsed.json"
FUTUREDB = f"{SCRATCH}/futuredb_fresh.json"

RFID_RE = re.compile(r"\d{15}")
TAG_RE = re.compile(r"\d{1,6}")

# stages that describe an adult; never applied to an animal aged `Kid`
ADULT_TAGS = {"Buck", "Mother", "Non-Pregnant", "Pregnant", "Lactating", "warmup"}
# reproductive stages that only a female can hold
FEMALE_ONLY = {"Mother", "Non-Pregnant", "Pregnant", "Lactating", "warmup"}


def clean(x):
    x = "" if x is None else str(x).strip()
    return "" if x in ("None", "-", "—") else x


def destination_tags():
    rows = json.load(open(FUTUREDB))["values"]
    head = rows[0]
    tags = {}
    for r in rows[1:]:
        d = dict(zip(head, list(r) + [""] * (len(head) - len(r))))
        if clean(d["Farm"]).upper() != "CPT":
            continue
        shed, tag = clean(d["Shed"]), clean(d["Shed Tag"])
        if shed and tag:
            tags.setdefault(shed, tag)
    return tags


def main(apply_changes):
    tags = destination_tags()
    doc = json.load(open(TARGET))
    header, data = doc["values"][0], doc["values"][1:]
    H = {h: i for i, h in enumerate(header)}

    def g(row, key):
        v = row[H[key]]
        return "" if v is None else str(v).strip()

    by_rfid = {g(r, "rfid"): r for r in data if g(r, "rfid")}
    by_tag = collections.defaultdict(list)
    for r in data:
        if g(r, "farm") == "CPT" and g(r, "old_id"):
            by_tag[g(r, "old_id")].append(r)

    def resolve(x):
        if RFID_RE.fullmatch(x):
            row = by_rfid.get(x)
            return row, ("" if row is not None else "RFID not in the file")
        if TAG_RE.fullmatch(x):
            hits = by_tag.get(x, [])
            if len(hits) == 1:
                return hits[0], ""
            return None, ("old tag not in the file" if not hits
                          else f"old tag matches {len(hits)} animals")
        if re.fullmatch(r"\d+", x):
            return None, f"not a 15-digit RFID ({len(x)} digits)"
        return None, "free text, not an identifier"

    recs = json.load(open(SHIFTINGS))
    for r in recs:
        t = r["wf"]
        r["status"] = ("COMPLETED" if "Completed" in t
                       else "CANCELLED" if "Cancelled" in t else "OPEN")

    def destination_stage(row, tag):
        """The cohort this animal actually adopts, or None to keep its own."""
        if not tag:
            return None
        if tag.startswith("F2"):
            sex = g(row, "gender")
            return f"F2-{sex}" if sex in ("Female", "Male") else "F2"
        if tag in ADULT_TAGS:
            if g(row, "age") == "Kid":
                return None
            if tag == "Buck" and g(row, "gender") == "Female":
                return None
            if tag in FEMALE_ONLY and g(row, "gender") == "Male":
                return None
        return tag

    moved, tagged, held = collections.Counter(), collections.Counter(), collections.Counter()
    unresolved, skipped = [], collections.Counter()
    for r in sorted(recs, key=lambda x: x["ts"]):
        if r["status"] == "CANCELLED":
            skipped["cancelled workflow"] += 1
            continue
        to = r.get("To Shed", "")
        if not to:
            skipped["workflow with no destination"] += 1
            continue
        for x in r["ids"]:
            row, why = resolve(x)
            if row is None:
                unresolved.append((r["when"], r["status"], x, why,
                                   f"{r.get('From Shed')} -> {to}"))
                continue
            row[H["shed"]] = to
            moved[to] += 1
            tag = tags.get(to)
            want = destination_stage(row, tag)
            if want is None:
                if tag:
                    held[f"{to}: shed tag {tag!r} not applied to a "
                         f"{g(row,'gender')} {g(row,'age')}, kept {g(row,'stage')!r}"] += 1
            elif g(row, "shed_tag") != want:
                row[H["shed_tag"]] = row[H["stage"]] = want
                tagged[f"{to}: {want}"] += 1

    print(f"workflows: {dict(collections.Counter(r['status'] for r in recs))}")
    for k, n in skipped.items():
        print(f"   skipped {n} ({k})")
    print(f"\nanimals relocated: {sum(moved.values())}")
    for k, n in sorted(moved.items()):
        print(f"   {n:>4}  -> {k}")
    print(f"\nstage adopted from destination cohort: {sum(tagged.values())}")
    for k, n in sorted(tagged.items()):
        print(f"   {n:>4}  {k}")
    print(f"\nstage NOT adopted (guard fired): {sum(held.values())}")
    for k, n in sorted(held.items()):
        print(f"   {n:>4}  {k}")
    print(f"\nUNRESOLVED Goat IDs, skipped: {len(unresolved)}")
    for when, st, x, why, route in unresolved:
        print(f"   {when}  {st:<10}{x:<26}{why:<32}{route}")

    if apply_changes:
        json.dump(doc, open(TARGET, "w"), indent=1)
        print(f"\nwrote {TARGET}")
    else:
        print("\n(dry run -- pass --apply to write)")


if __name__ == "__main__":
    main("--apply" in sys.argv)
