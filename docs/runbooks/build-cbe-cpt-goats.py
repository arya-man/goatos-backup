#!/usr/bin/env python3
"""Combine CPT-Adult-goats (1).json + CBE-goats.json into one file.

Shed convention: CPT's. The partition lives INSIDE `shed` ("Godel 1 - Part 1").
CPT's own `partition` column is NOT a shed partition (values 1-10, no relation to
kids/deliveries -- an animal attribute we can't identify), so it is carried
through verbatim for CPT and left blank for CBE, whose partition information is
fully preserved inside `shed`.

shed_tag/stage come from the Counting-DB FutureDB tab for BOTH farms, by shed.
Every FutureDB shed carries exactly one tag, which is what makes the
one-tag-per-partition invariant hold.
"""
import json, re, collections

SCRATCH = "/private/tmp/claude-501/-Users-manoharchowdary-Desktop-mesha-goatos/43594e37-47f7-4ccf-a60e-509a9f380b6f/scratchpad"
DOCS = "/Users/manoharchowdary/Desktop/mesha/goatos/docs/runbooks"

cpt = json.load(open(f"{DOCS}/CPT-Adult-goats (1).json"))["values"]
cbe = json.load(open(f"{DOCS}/CBE-goats.json"))["values"]
fut = json.load(open(f"{SCRATCH}/futuredb.json"))["values"]

HEADER = cbe[0]                      # 61 canonical + rfid2 + melatonin
Hc = {h: i for i, h in enumerate(cpt[0])}
Hb = {h: i for i, h in enumerate(cbe[0])}

def clean(x):
    x = "" if x is None else str(x).strip()
    return "" if x == "None" else x

# ---- FutureDB tag per (farm, shed) -------------------------------------
fh = fut[0]
FUT_TAG = {}
for r in fut[1:]:
    d = dict(zip(fh, r + [""] * (len(fh) - len(r))))
    farm, shed, tag = clean(d.get("Farm")).upper(), clean(d.get("Shed")), clean(d.get("Shed Tag"))
    if farm and shed and tag:
        FUT_TAG.setdefault((farm, shed), tag)

def full_shed(shed, partition):
    """CBE stores ('Godel 1','3'); CPT stores ('Godel 1 - Part 3',''). Emit CPT form."""
    shed, partition = clean(shed), clean(partition)
    if partition and " Part " not in shed:
        return f"{shed} - Part {partition}"
    return shed

rows = []
stats = collections.Counter()
retag = collections.Counter()

# ---- CPT: verbatim, shed already in CPT form ---------------------------
for r in cpt[1:]:
    row = {h: (r[Hc[h]] if h in Hc else "") for h in HEADER}
    shed = clean(row["shed"])
    tag = FUT_TAG.get(("CPT", shed))
    if tag and tag != clean(row["shed_tag"]):
        retag[f"CPT {shed}: {clean(row['shed_tag'])!r} -> {tag!r}"] += 1
    if tag:
        row["shed_tag"] = row["stage"] = tag
    row["farm"] = "CPT"
    row["rfid2"] = row["melatonin"] = ""
    rows.append([row[h] for h in HEADER])
    stats["CPT"] += 1

# ---- CBE: reassemble shed to CPT form, blank the partition column ------
for r in cbe[1:]:
    row = {h: r[Hb[h]] for h in HEADER}
    shed = full_shed(row["shed"], row["partition"])
    row["shed"] = shed
    row["partition"] = ""            # partition now lives inside `shed`
    tag = FUT_TAG.get(("CBE", shed))
    if tag:
        row["shed_tag"] = row["stage"] = tag
    row["farm"] = "CBE"
    rows.append([row[h] for h in HEADER])
    stats["CBE"] += 1

out = {"values": [HEADER] + rows}
json.dump(out, open(f"{SCRATCH}/CBE-CPT-goats.json", "w"), indent=1)

# ---- report ------------------------------------------------------------
H = {h: i for i, h in enumerate(HEADER)}
print(f"rows: {len(rows)}  (CPT {stats['CPT']} + CBE {stats['CBE']})")
rf = [r[H["rfid"]] for r in rows if r[H["rfid"]]]
print(f"rfid non-blank {len(rf)} | unique {len(set(rf))} | duplicates {len(rf)-len(set(rf))}")
print(f"columns: {len(HEADER)} | all rows same width: {sorted({len(r) for r in rows})}")

mix = collections.defaultdict(collections.Counter)
for r in rows:
    mix[(r[H["farm"]], r[H["shed"]])][clean(r[H["shed_tag"]]) or "(blank)"] += 1
bad = [(k, dict(t)) for k, t in mix.items() if len([x for x in t if x != "(blank)"]) > 1]
print(f"\nshed groups: {len(mix)} | groups holding >1 tag: {len(bad)}")
for k, t in bad:
    print("   ", k, t)
blank = sum(1 for r in rows if not clean(r[H["shed_tag"]]))
print(f"rows with blank shed_tag: {blank}")
if retag:
    print("\nCPT rows retagged from FutureDB:")
    for k, n in retag.most_common():
        print(f"   {n:3d}  {k}")
