#!/usr/bin/env python3
"""Build CBE-goats.json in the same Google-Sheets `values` shape as
docs/runbooks/CPT-Adult-goats (1).json.

Identity/placement truth  : double-tagging-CBE-2026-08-01.csv (fresh 2026-08-01 survey)
Enrichment truth          : goatos-sheets BigQuery (goat_history_complete, stage,
                            adg, health, procurement) joined on real RFID
"""
import csv, json, re, collections, sys

SCRATCH = "/private/tmp/claude-501/-Users-manoharchowdary-Desktop-mesha-goatos/43594e37-47f7-4ccf-a60e-509a9f380b6f/scratchpad"
CSV = "/Users/manoharchowdary/Downloads/double-tagging-CBE-2026-08-01.csv"
REF = "/Users/manoharchowdary/Desktop/mesha/goatos/docs/runbooks/CPT-Adult-goats (1).json"

def load(n):
    return json.load(open(f"{SCRATCH}/{n}.json"))

ghc   = load("ghc_cbe")
rmap  = load("rfidmap")
# "RFID source of truth" sheet (Combined tab) — declared authoritative for old-tag -> RFID
_sot  = load("sot")["values"]
_shdr = _sot[0]
sot   = [dict(zip(_shdr, r + [""] * (len(_shdr) - len(r)))) for r in _sot[1:]
         if (r + [""])[0] == "CBE"]
# "Counting DB" -> FutureDB tab: authoritative shed tag per (shed, partition).
# Every partition carries exactly one tag there (verified: 0 of 63 mixed), so an
# animal simply inherits the tag of the partition it stands in.
_f    = load("futuredb")["values"]
_fh   = _f[0]
futdb = [dict(zip(_fh, r + [""] * (len(_fh) - len(r)))) for r in _f[1:]
         if (r + [""])[1].strip().upper() == "CBE"]

stage = load("stage")
adg   = load("adg")
health= load("health")
proc  = load("proc")

HEADER = json.load(open(REF))["values"][0]          # 61 canonical columns
HEADER = HEADER + ["rfid2", "melatonin"]            # appended, non-breaking

def clean(x):
    """Sources store missing values as '', None, or the literal string 'None'."""
    x = "" if x is None else str(x).strip()
    return "" if x == "None" else x

def g(d, k):
    if not d:
        return ""
    return clean(d.get(k))

sot_by = {clean(r.get("RFID")): r for r in sot if clean(r.get("RFID"))}

# ---------- indexes (all keyed on real RFID / goat_id) ----------
ghc_by  = {r["goat_id"]: r for r in ghc}
rmap_by = {(r.get("RFID") or "").strip(): r for r in rmap}
# old tag -> mapping row, keyed (tag, origin_farm) and bare tag
rmap_old, rmap_old_bare = {}, collections.defaultdict(list)
for r in rmap:
    t = (r.get("Old_Tag_ID") or "").strip()
    if t:
        rmap_old[(t, (r.get("Origin_Farm") or "").strip())] = r
        rmap_old_bare[t].append(r)
def fut_key(shed, part=""):
    s = clean(shed).replace(" - Part ", " Part ")
    if s in ("Ho Chi Minh 1", "Ho Chi Minh 2"):
        s = "Ho Chi Minh"
    p = clean(part)
    m = re.match(r"^(.*?)\s*Part\s*(.+)$", s)
    if m:
        s, p = m.group(1).strip(), m.group(2).strip()
    return f"{s} Part {p}" if p else s

FUT_TAG = {}
for r in futdb:
    k, t = fut_key(r.get("Shed")), clean(r.get("Shed Tag"))
    if t:
        FUT_TAG.setdefault(k, t)

sot_old, sot_old_bare = {}, collections.defaultdict(list)
for r in sot:
    t = clean(r.get("Old ID"))
    if t:
        sot_old[(t, clean(r.get("Old ID Suffix")))] = r
        sot_old_bare[t].append(r)

stage_by = {r["goat_id"]: r for r in stage}
adg_by   = {r["goat_id"]: r for r in adg}
proc_by  = {r["Goat_ID"]: r for r in proc}

# latest health record per goat
health_by = {}
for r in sorted(health, key=lambda x: (x.get("date") or "")):
    health_by[r["goat_id"]] = r

def norm_shed(s):
    s = (s or "").strip()
    if s == "Ho Chi Minh":
        return "Ho Chi Minh 1"
    return s.replace(" Part ", " - Part ")

def split_shed(s):
    """'Sumathi 2 Part 8' -> ('Sumathi 2', '8');  'Gandhi 1' -> ('Gandhi 1','')"""
    s = (s or "").strip()
    m = re.match(r"^(.*?)\s*Part\s*(\d+)$", s)
    if m:
        return m.group(1).strip(), m.group(2)
    return s, ""

def species(breed):
    return "Sheep" if "sheep" in (breed or "").lower() else "Goat"

# ---------- assemble the row set ----------
records = []          # (rfid, rfid2, old_id, suffix, gender, age, shed_label, shed_tag, breed, melatonin, source)
seen = set()

for r in csv.DictReader(open(CSV)):
    rfid = r["RFID"].strip()
    records.append(dict(
        rfid=rfid, rfid2=r["RFID2"].strip(), old_id=r["Old ID"].strip(),
        suffix=r["Old ID Suffix"].strip(), gender=r["Gender"].strip(),
        age=r["Age"].strip(), shed_label=r["Shed"].strip(),
        shed_tag_csv=r["Shed Tag"].strip(), breed=r["Breed"].strip(),
        melatonin=r["Melatonin"].strip(), source="csv"))
    seen.add(rfid)

# ---------- G2P6-8 from the accompanying text ----------
G2 = {
 "Godel 2 Part 6": ["1984 CBE","182 CJB","184 CJB","156 CJB","284 CJB","1950 CBE","220 CJB",
                    "573 BLR","238 CJB","1998 CBE","1972 CBE","901007000506098","901007000506100","901007000506099"],
 "Godel 2 Part 7": ["221 CJB","168 CJB","1919 CBE","1548 CBE","549 CJB","175 CJB","1767 CBE",
                    "866 CJB","174 CJB","157 CJB","901007000506092","901007000506115","299 CJB","204 CJB"],
 "Godel 2 Part 8": ["1905 CBE","550 CJB","502 CJB","355 CJB","507 BLR","535 BLR","568 BLR",
                    "498 CJB","596 CJB","64 CJB","218 CJB","158 CJB","533 BLR","901007000506026"],
}
unresolved = []
for shed, entries in G2.items():
    for e in entries:
        e = e.strip()
        if re.fullmatch(r"\d{15}", e):                 # bare RFID
            rfid, old_id, suffix = e, "", ""
            m = rmap_by.get(rfid)
        else:
            tag, _, suffix = e.partition(" ")
            suffix = suffix.strip()
            # source-of-truth sheet first, then goatsDB_rfid_mapping
            m = sot_old.get((tag, suffix)) or rmap_old.get((tag, suffix))
            if not m:
                cands = sot_old_bare.get(tag) or rmap_old_bare.get(tag, [])
                m = cands[0] if len(cands) == 1 else None
            if m and "RFID" not in m:
                m = None
            rfid, old_id = ((m or {}).get("RFID") or "").strip(), tag
        if not rfid:
            # No RFID anywhere in the mapping sheet or herd history: keep the
            # animal in the file with a blank rfid rather than drop it silently.
            unresolved.append((shed, e))
            records.append(dict(
                rfid="", rfid2="", old_id=old_id, suffix=suffix,
                gender="Female", age="Adult", shed_label=shed,
                shed_tag_csv="", breed="", melatonin="", source="text_g2p6-8_untagged"))
            continue
        if rfid in seen:
            unresolved.append((shed, e + " [dup of CSV row]"))
            continue
        seen.add(rfid)
        gh = ghc_by.get(rfid)
        records.append(dict(
            rfid=rfid, rfid2="", old_id=old_id, suffix=suffix,
            gender=(g(m, "Gender") or g(gh, "gender") or "Female"),
            age=(g(m, "Age") or "Adult"),
            shed_label=shed, shed_tag_csv="",
            breed=(g(m, "Breed") or g(gh, "breed")), melatonin="", source="text_g2p6-8"))

# ---------- build rows ----------
values = [HEADER]
enriched = 0
tag_unmatched = collections.Counter()
for rec in records:
    rfid = rec["rfid"]
    gh   = ghc_by.get(rfid)
    mp   = rmap_by.get(rfid)
    st   = stage_by.get(rfid)
    ad   = adg_by.get(rfid)
    he   = health_by.get(rfid)
    pr   = proc_by.get(rfid)
    if gh:
        enriched += 1

    so = sot_by.get(rfid)
    # old tag: CSV first, then the "RFID source of truth" sheet (declared
    # authoritative), then goatsDB_rfid_mapping. The two recovery sources agree
    # on all 167 rows they both cover; the mapping sheet adds 5 more.
    old_id = clean(rec["old_id"]) or g(so, "Old ID") or g(mp, "Old_Tag_ID")
    suffix = clean(rec["suffix"]) or g(so, "Old ID Suffix") or g(mp, "Origin_Farm")
    goat_id = old_id or rfid
    shed, partition = split_shed(rec["shed_label"])
    # stage/tag: inherited from the FutureDB tag of the partition the animal
    # stands in. Never from goat_history_complete.current_shedtag -- that tag
    # belongs to the shed BigQuery thinks the animal is in, and pairing it with
    # this file's shed produced 32 of 60 partitions holding a mix of tags
    # (bucks inside doe partitions, etc).
    tag = FUT_TAG.get(fut_key(shed, partition), "")
    if not tag:
        tag_unmatched[fut_key(shed, partition)] += 1

    row = {
      "rfid": rfid,
      "goat_id": goat_id,
      # reference convention: farm_goat_id == farm + goat_id (not BigQuery's
      # own farm_goat_id, which is CBE+rfid because its goat_id IS the rfid)
      "farm_goat_id": ("CBE" + goat_id) if gh else "",
      "old_id": old_id,
      "old_id_suffix": suffix,
      "mapped_farm_goat_id": g(gh, "mapped_farm_goat_id"),
      "has_mapping": g(gh, "has_mapping"),
      "farm": "CBE",
      "shed": shed,
      "shed_tag": tag,
      "partition": partition,
      "stage": tag or g(st, "stage"),
      "stage_entry_date": g(st, "stage_entry_date"),
      "days_in_stage": g(st, "days_in_stage"),
      "age": rec["age"],
      "breed": rec["breed"],
      "gender": rec["gender"],
      "dob": g(gh, "dob") or g(st, "birth_date"),
      "birth_time": g(gh, "birth_time"),
      "birth_weight": g(gh, "birth_weight"),
      "origin_type": g(gh, "origin_type"),
      "mother_id": g(gh, "mother_id"),
      "mother_breed": g(gh, "mother_breed"),
      "mother_origin": g(gh, "mother_origin"),
      "mother_vendor": g(gh, "mother_vendor"),
      "mother_load_id": g(gh, "mother_load_id"),
      "mother_purchase_date": g(gh, "mother_purchase_date"),
      "kids_born_count": g(gh, "kids_born_count"),
      "delivery_date": g(gh, "delivery_date"),
      "has_deliveries": g(gh, "has_deliveries"),
      "colostrum_status": g(gh, "colostrum_status"),
      "is_milking_mother": "",
      "purchase_vendor": g(gh, "purchase_vendor") or g(pr, "Vendor"),
      "purchase_load_id": g(gh, "purchase_load_id") or g(pr, "Load_ID"),
      "purchase_date": g(gh, "purchase_date") or g(pr, "Date"),
      "purchase_weight": g(gh, "purchase_weight") or g(pr, "weight"),
      "status": g(gh, "status"),
      "death_date": g(gh, "death_date") or g(pr, "death_date"),
      "death_reason": g(gh, "death_reason"),
      "sale_date": g(gh, "sale_date"),
      "sale_reason": g(gh, "sale_reason"),
      "abortion_date": g(gh, "abortion_date"),
      "last_event": g(he, "record_type"),
      "last_event_date": g(he, "date"),
      "health_status": g(he, "status"),
      "problem_name": g(he, "problem_name"),
      "diagnosis": g(he, "diagnosis"),
      "medicine": g(he, "medicine"),
      "disease_1": g(gh, "disease_1"),
      "disease_1_date": g(gh, "disease_1_date"),
      "disease_2": g(gh, "disease_2"),
      "disease_2_date": g(gh, "disease_2_date"),
      "disease_3": g(gh, "disease_3"),
      "disease_3_date": g(gh, "disease_3_date"),
      "latest_weight": g(gh, "latest_weight"),
      "latest_weight_date": g(gh, "latest_weight_date"),
      "adg_grams_per_day": g(ad, "adg_grams_per_day") or g(gh, "avg_daily_gain_grams"),
      "total_gain_kg": g(ad, "total_gain_kg"),
      "load_id": g(ad, "load_id") or g(pr, "Load_ID"),
      "animal_status": g(ad, "animal_status") or g(pr, "animal_status"),
      "species": species(rec["breed"]),
      "rfid2": rec["rfid2"],
      "melatonin": rec["melatonin"],
    }
    missing = [h for h in HEADER if h not in row]
    if missing:
        sys.exit("column mismatch: " + str(missing))
    values.append([row[h] for h in HEADER])

out = {"values": values}
with open(f"{SCRATCH}/CBE-goats.json", "w") as f:
    json.dump(out, f, indent=1)

# ---------- report ----------
ages = collections.Counter(r[HEADER.index("age")] for r in values[1:])
print(f"rows written      : {len(values)-1}  (+1 header)")
print(f"  from CSV        : {sum(1 for r in records if r['source']=='csv')}")
print(f"  from G2P6-8 text: {sum(1 for r in records if r['source'].startswith('text'))}")
print(f"age split         : {dict(ages)}")
print(f"enriched from goat_history_complete : {enriched}/{len(records)}")
rec_sot = sum(1 for rec in records if not clean(rec["old_id"]) and g(sot_by.get(rec["rfid"]), "Old ID"))
rec_map = sum(1 for rec in records if not clean(rec["old_id"])
              and not g(sot_by.get(rec["rfid"]), "Old ID")
              and g(rmap_by.get(rec["rfid"]), "Old_Tag_ID"))
print(f"old tag recovered  : {rec_sot} from source-of-truth sheet + {rec_map} from goatsDB_rfid_mapping")
blank = sum(1 for r in values[1:] if str(r[HEADER.index('old_id')]).strip() in ('','None'))
print(f"rows still with no old tag : {blank}")
print(f"literal 'None' leaks       : {sum(1 for r in values[1:] for c in r if str(c).strip()=='None')}")
print(f"columns           : {len(HEADER)} (61 canonical + rfid2 + melatonin)")
tagged = sum(1 for r in values[1:] if str(r[HEADER.index("shed_tag")]).strip())
print(f"shed_tag from FutureDB : {tagged}/{len(values)-1}")
if tag_unmatched:
    print("  partitions with NO FutureDB tag:", dict(tag_unmatched))
mix = collections.defaultdict(set)
for r in values[1:]:
    k = (r[HEADER.index("shed")], str(r[HEADER.index("partition")]))
    if str(r[HEADER.index("shed_tag")]).strip():
        mix[k].add(r[HEADER.index("shed_tag")])
print(f"  partitions holding >1 tag: {sum(1 for v in mix.values() if len(v)>1)} of {len(mix)}")
if unresolved:
    print(f"\nUNRESOLVED G2P6-8 entries ({len(unresolved)}):")
    for s, e in unresolved:
        print(f"   {s}: {e}")
