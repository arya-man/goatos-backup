#!/usr/bin/env python3
"""Stage 4 — the overlay that turns two classifier outputs into the page's dataset.

Neither classifier can do any of this, because each sees one direction and one row
at a time. This pass sees the whole extract at once.

It does four things:

  1. Detects copied shed figures — five or more animals in the same park, shed and
     date carrying an identical weight pair — and rewrites those rows to the fifth
     verdict `copied`. This overrides whatever either classifier decided.
  2. Attributes phantom kilos once per bad reading, not once per spoiled interval.
     (Counts are reported after step 3, so they match what the page renders.)
  3. Drops junk animal ids the extract SQL misses, because its filter is
     case-sensitive.
  4. Emits the row schema the page renders.

Run from this directory, with the stage 1-3 outputs alongside it:

    python3 build_dataset.py --in ./work --out ./work/dataset.json

Reproduces the shipped figures exactly; --check asserts them.
"""
from __future__ import annotations

import argparse
import collections
import json
import pathlib
import re
import sys

# --- stage 4a: copied shed figures -------------------------------------------------
#
# The key is (park, shed, date, previous weight, current weight). PARTITION IS NOT
# PART OF THE KEY — a copied figure is written across a whole shed, and including
# the partition drops the smallest block to one animal and stops reproducing.
COPIED_MIN_BLOCK = 5

# --- stage 4b: phantom kilo attribution --------------------------------------------
#
# One bad reading spoils the interval into it and the interval out of it. Counting
# both double-counts the same mistake. These say which reading each rule condemns.
CONDEMNS_CURRENT = {
    "spike_then_drop", "decimal_shift_up", "added_leading_digit", "keypad_tens_slip",
    "implausible_gain", "decimal_shift_down", "v_shape_outlier", "same_day_conflict",
}
CONDEMNS_PREVIOUS = {"correction_of_prior_error"}

# --- stage 4c: junk ids ------------------------------------------------------------
#
# enrich.sql drops ('', 'No tag', '-'); animals.sql also drops 'NA'. All three are
# case-sensitive, so 'No Tag', 'No Id', 'No ID' and 'No id' survive every one.
JUNK_ID = re.compile(r"^(no\s*id|no\s*tag|na|n/a|-|nil|null|\?)$", re.I)

PART_SUFFIX = re.compile(r"[-\s]*PART\s*\d+$")
PART_NUMBER = re.compile(r"PART\s*(\d+)$")


def normalise_shed(raw: str) -> tuple[str, str | None]:
    """GODEL 1 - PART 3, GODEL 1 PART 3 and Godel 1 - Part 3 -> ('GODEL 1', '3')."""
    s = re.sub(r"\s+", " ", (raw or "").upper().strip())
    part = PART_NUMBER.search(s)
    return PART_SUFFIX.sub("", s).strip(), (part.group(1) if part else None)


def as_float(x):
    try:
        return float(x)
    except (TypeError, ValueError):
        return None


def mark_copied(events: list[dict]) -> int:
    blocks = collections.defaultdict(list)
    for e in events:
        shed, _ = normalise_shed(e["shed"])
        blocks[(e["farm"], shed, e["cur_date"], e["prev_wt"], e["cur_wt"])].append(e)

    marked = 0
    for (farm, shed, date, prev_wt, cur_wt), block in blocks.items():
        if len(block) < COPIED_MIN_BLOCK:
            continue
        for e in block:
            e["verdict"] = "copied"
            e["reason_code"] = "shed_figure_copied"
            e["confidence"] = 0.9
            e["explanation"] = (
                f"{len(block)} animals in this shed carry exactly {cur_wt} kg on {date}, "
                f"and exactly {prev_wt} kg at the weighing before it. That is one shed "
                f"figure written against every animal, not {len(block)} separate "
                "weighings — so it says nothing about this animal."
            )
            marked += 1
    return marked


def attribute_phantom(events: list[dict]) -> int:
    """Charge each bad reading once, to the interval that identifies it best."""
    claims = collections.defaultdict(list)
    for e in events:
        if e["verdict"] != "invalid":
            continue
        if e["reason_code"] in CONDEMNS_CURRENT:
            claims[(e["goat_id"], e["cur_date"])].append(e)
        elif e["reason_code"] in CONDEMNS_PREVIOUS:
            claims[(e["goat_id"], e["prev_date"])].append(e)

    mirrors = 0
    for _, blaming in claims.items():
        if len(blaming) < 2:
            for e in blaming:
                e["phantom"] = 1
            continue
        blaming.sort(key=lambda x: (-float(x["confidence"]), x["cur_date"]))
        blaming[0]["phantom"] = 1
        for e in blaming[1:]:
            e["phantom"] = 0
            mirrors += 1
            e["explanation"] = (
                f"Same bad reading as the {blaming[0]['cur_date']} case on this animal, "
                "seen from the other side. Counted once, there."
            )
    for e in events:
        e.setdefault("phantom", 1)
    return mirrors


def build(indir: pathlib.Path) -> dict:
    losses = json.loads((indir / "classified.json").read_text())
    gains = json.loads((indir / "classified_pos.json").read_text())
    series = json.loads((indir / "animals.json").read_text())
    shed_level = json.loads((indir / "shedlevel.json").read_text())

    events_raw = losses + gains
    copied = mark_copied(events_raw)
    mirrors = attribute_phantom(events_raw)

    animals: dict[str, dict] = {}
    for a in series:
        gid = (a["goat_id"] or "").strip()
        if not gid or JUNK_ID.match(gid):
            continue
        animals[a["goat_id"]] = dict(
            id=a["goat_id"], farm=a["farm"], shed=a["shed"], part=a["part"],
            breed=a["breed"], gender=a["gender"], type=a["goat_type"],
            n=int(a["n_weighings"]),
            f_d=a["first_date"], f_w=as_float(a["first_wt"]),
            l_d=a["last_date"], l_w=as_float(a["last_wt"]),
            span=int(a["span_days"]), adg=as_float(a["overall_adg"]),
            drop=0, spike=0, bad=0, valid=0, open=0, copied=0,
        )

    bucket = {"invalid": "bad", "valid": "valid", "unexplained": "open", "copied": "copied"}
    events = []
    for source, direction in ((losses, "drop"), (gains, "gain")):
        for e in source:
            if e["goat_id"] not in animals:
                continue
            shed, part = normalise_shed(e["shed"])
            events.append(dict(
                gid=e["goat_id"], farm=e["farm"], shed=shed, part=part, dir=direction,
                pd=e["prev_date"], pw=as_float(e["prev_wt"]),
                cd=e["cur_date"], cw=as_float(e["cur_wt"]),
                nd=e["next_date"], nw=as_float(e["next_wt"]),
                days=int(e["days"]), delta=as_float(e["delta"]),
                adg=as_float(e["adg"]), pct=as_float(e["pct_bw"]),
                v=e["verdict"], r=e["reason_code"], conf=as_float(e["confidence"]),
                why=e["explanation"], idf=e["id_format"],
                sh=(e["shift_detail"] or "")[:150], ph=e["phantom"],
                hn=int(e["health_n"]), kn=int(e["kidding_n"]), pshed=e["pshed"],
            ))
            a = animals[e["goat_id"]]
            a["drop" if direction == "drop" else "spike"] += 1
            a[bucket[e["verdict"]]] += 1

    shed_rows = []
    for r in shed_level:
        shed, part = normalise_shed(r["shed"])
        shed_rows.append(dict(
            date=r["date"], farm=r["farm"], shed=shed, part=part, tag=r["shed_tag"],
            sex=r["gender"], type=r["goat_type"], kg=float(r["avg_kg"]),
        ))

    dropped = len(events_raw) - len(events)
    # report what the page actually shows: after the junk-id rows are dropped
    copied = sum(1 for e in events if e["v"] == "copied")
    mirrors = sum(1 for e in events if e["ph"] == 0)
    print(f"copied shed figures marked : {copied}", file=sys.stderr)
    print(f"mirror intervals zeroed    : {mirrors}", file=sys.stderr)
    print(f"junk-id rows dropped       : {dropped}", file=sys.stderr)
    print(f"animals {len(animals)} · changes {len(events)} · shed-level {len(shed_rows)}",
          file=sys.stderr)


    return {"animals": list(animals.values()), "events": events, "shedlevel": shed_rows,
            "_counts": {"copied": copied, "mirrors": mirrors, "dropped": dropped,
                        "shedlevel": len(shed_rows)}}


# The figures this document quotes. If a rule or an extract changes, these move —
# that is the point of asserting them.
EXPECTED = {"animals": 2099, "events": 13141, "copied": 9650, "mirrors": 71, "dropped": 13,
            "shedlevel": 146}


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--in", dest="indir", default=".", type=pathlib.Path,
                    help="directory holding the stage 1-3 outputs")
    ap.add_argument("--out", dest="outfile", default="dataset.json", type=pathlib.Path)
    ap.add_argument("--check", action="store_true",
                    help="assert the figures quoted in weight-truth-method.md")
    args = ap.parse_args()

    db = build(args.indir)
    counts = db.pop("_counts")
    args.outfile.write_text(json.dumps(db, separators=(",", ":")))
    print(f"wrote {args.outfile}", file=sys.stderr)

    if args.check:
        got = {"animals": len(db["animals"]), "events": len(db["events"]), **counts}
        bad = {k: (v, got[k]) for k, v in EXPECTED.items() if got[k] != v}
        if bad:
            for k, (want, have) in bad.items():
                print(f"MISMATCH {k}: document says {want}, this build says {have}",
                      file=sys.stderr)
            return 1
        print("all documented figures reproduce", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
