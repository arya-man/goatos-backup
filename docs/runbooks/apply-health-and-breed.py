#!/usr/bin/env python3
"""Set `health_status` from the Health DB sheet, and fix reversed breed names.

Maintainer instruction 2026-08-03: an animal with an ACTIVE problem in the Health
DB in the last month is Sick; everything else is fine.

Health source: spreadsheet 1uvDO_vipNsLcB4S0O7Bj-L8VCSMCd0eX5U9cJS8F-QE, tab `DB`
(Date, Problem ID, Problem Name, Farm, Record Type, Goat ID, ..., Status).
Only `Record Type = Problem` rows are used -- Treatment/Medicine/Follow Up rows
are steps within a problem, not the problem's own state.  A Problem ID carries
exactly one status for its whole life (verified: 0 of 1519 groups have more than
one), so the animal's state is the status of its most recent problem inside the
window.  `Closed` is resolved; `Open` and `Extended` are not.

Window: 30 days back from the business date below.

Value note: the schema's word for "fine" is `healthy` --
`goats_health_status_check` allows only healthy|sick|under_treatment|recovering|
quarantine|icu, so a literal `Fine` is exactly the blocker this change removes.
`Healthy` is written in the file's Title Case, which the loader lower-cases like
every other enum.

Matching: the sheet's `Goat ID` is usually the RFID; where it is not, an old tag
is accepted only when it resolves to exactly one animal on the same farm.
Ambiguous or unmatched IDs are reported, never guessed.

Breed: `breeds` stores one canonical order per cross, so `Malai x Beetal` and
`Sojat x Malai` are rewritten to the catalogued `Beetal x Malai` / `Malai x Sojat`.

Usage:  python3 docs/runbooks/apply-health-and-breed.py [--apply]
"""
import collections
import datetime
import json
import sys

SCRATCH = ("/private/tmp/claude-501/-Users-manoharchowdary-Desktop-mesha-goatos/"
           "43594e37-47f7-4ccf-a60e-509a9f380b6f/scratchpad")
TARGET = "docs/runbooks/CBE-CPT-goats.json"

BUSINESS_DATE = datetime.date(2026, 8, 3)
WINDOW_DAYS = 30

# a problem in one of these states is still running
ACTIVE_STATUSES = {"Open", "Extended"}

# breeds catalogued in the other order
BREED_FIX = {
    "Malai x Beetal": "Beetal x Malai",
    "Sojat x Malai": "Malai x Sojat",
}

# Two Goat IDs on OPEN problems are RFIDs with digits dropped.  Each is a
# subsequence of exactly one real RFID in the herd -- no second candidate to
# choose between -- and both resolve to a CBE Yashoda 10 ICU kid, which is where
# the reported Fever/Diarrhea belongs.  Written out rather than fuzzy-matched at
# run time so a future typo cannot silently attach to the wrong animal.
RFID_TYPO = {
    "90100700504472": "901007000504472",
    "9010070004300": "901007000504300",
}


def clean(x):
    x = "" if x is None else str(x).strip()
    return "" if x in ("None", "-") else x


def parse_date(s):
    for fmt in ("%d/%m/%Y", "%Y-%m-%d", "%d-%m-%Y"):
        try:
            return datetime.datetime.strptime(s, fmt).date()
        except ValueError:
            pass
    return None


def main(apply_changes):
    cutoff = BUSINESS_DATE - datetime.timedelta(days=WINDOW_DAYS)

    doc = json.load(open(TARGET))
    rows = doc["values"]
    header, data = rows[0], rows[1:]
    H = {h: i for i, h in enumerate(header)}

    def jg(row, key):
        v = row[H[key]]
        return "" if v is None else str(v).strip()

    # --- index the herd both ways -------------------------------------
    by_rfid = {}
    by_tag = collections.defaultdict(list)
    for row in data:
        if jg(row, "rfid"):
            by_rfid[jg(row, "rfid")] = row
        if jg(row, "old_id"):
            by_tag[(jg(row, "farm"), jg(row, "old_id"))].append(row)

    # --- latest problem per goat inside the window --------------------
    sheet = json.load(open(f"{SCRATCH}/health_db.json"))["values"]
    S = {h.strip(): i for i, h in enumerate(sheet[0])}

    def sg(row, key):
        i = S.get(key)
        return "" if i is None or i >= len(row) else clean(row[i])

    latest = {}
    for r in sheet[1:]:
        if sg(r, "Record Type") != "Problem":
            continue
        when = parse_date(sg(r, "Date"))
        if not when or when < cutoff:
            continue
        gid, farm = sg(r, "Goat ID"), sg(r, "Farm")
        gid = RFID_TYPO.get(gid, gid)
        if not gid:
            continue
        prev = latest.get(gid)
        if prev is None or when >= prev[0]:
            latest[gid] = (when, sg(r, "Status"), sg(r, "Problem Name"), farm)

    # --- resolve each sheet ID to one animal --------------------------
    sick, unmatched, ambiguous = {}, [], []
    for gid, (when, status, name, farm) in latest.items():
        if gid in by_rfid:
            target = by_rfid[gid]
        else:
            hits = by_tag.get((farm, gid), [])
            if len(hits) == 1:
                target = hits[0]
            else:
                (ambiguous if hits else unmatched).append((gid, farm, status, name))
                continue
        if status in ACTIVE_STATUSES:
            sick[id(target)] = (gid, when, status, name)

    # --- write --------------------------------------------------------
    stats = collections.Counter()
    icu_marked_healthy = []
    for row in data:
        if id(row) in sick:
            row[H["health_status"]] = "Sick"
            stats["health_status -> Sick"] += 1
        else:
            if jg(row, "health_status") != "Healthy":
                stats["health_status -> Healthy"] += 1
            row[H["health_status"]] = "Healthy"
            if "icu" in jg(row, "stage").lower():
                icu_marked_healthy.append(row)

        fixed = BREED_FIX.get(jg(row, "breed"))
        if fixed:
            row[H["breed"]] = fixed
            stats[f"breed {jg(row,'breed')}"] += 1

    print(f"health window: {cutoff} .. {BUSINESS_DATE}  ({WINDOW_DAYS} days)")
    print(f"problems in window: {len(latest)} goat IDs")
    for k, n in stats.most_common():
        print(f"   {n:>5}  {k}")
    print(f"\n   unmatched sheet IDs : {len(unmatched)}")
    for gid, farm, status, name in unmatched[:12]:
        print(f"        {farm:<5}{gid:<24}{status:<10}{name}")
    if ambiguous:
        print(f"   ambiguous old tags  : {len(ambiguous)}")
        for gid, farm, status, name in ambiguous:
            print(f"        {farm:<5}{gid:<24}{status:<10}{name}")
    if icu_marked_healthy:
        print(f"\n   ** {len(icu_marked_healthy)} animals in an ICU stage are now `Healthy` "
              f"(no active problem in the window)")

    if apply_changes:
        json.dump(doc, open(TARGET, "w"), indent=1)
        print(f"\nwrote {TARGET}")
    else:
        print("\n(dry run -- pass --apply to write)")


if __name__ == "__main__":
    main("--apply" in sys.argv)
