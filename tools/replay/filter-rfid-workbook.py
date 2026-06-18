#!/usr/bin/env python3
"""Filter a live RFID workbook to a deterministic seed set.

The RFID source sheet is append-edited by operators. Its canonical seed rows
are ordered in farm blocks; when a farm block reopens later in the sheet, those
rows are a newly appended review batch, not guaranteed fresh goat passports.
The replay imports canonical farm blocks and exports reopened-block rows for
review so live RFID deltas cannot silently inflate the herd.
"""

from __future__ import annotations

import argparse
import csv
import json
import re
from pathlib import Path

from openpyxl import Workbook, load_workbook


NON_ALNUM = re.compile(r"[^A-Z0-9]+")


def canonical_identifier(raw: object) -> str:
    value = str(raw or "").strip().replace(",", "").replace(" ", "")
    if not value or value.lower() == "none":
        return ""
    if "e" in value.lower():
        try:
            value = str(int(float(value)))
        except ValueError:
            pass
    elif "." in value:
        left, _, right = value.partition(".")
        if right.strip("0") == "":
            value = left
    return NON_ALNUM.sub("", value.upper())


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input-xlsx", required=True, type=Path)
    parser.add_argument("--sheet", required=True)
    parser.add_argument("--output-xlsx", required=True, type=Path)
    parser.add_argument("--excluded-csv", required=True, type=Path)
    parser.add_argument("--summary-json", required=True, type=Path)
    args = parser.parse_args()

    source = load_workbook(args.input_xlsx, read_only=True, data_only=True)
    if args.sheet not in source.sheetnames:
        raise SystemExit(f"sheet {args.sheet!r} not found in {args.input_xlsx}")
    ws = source[args.sheet]
    rows = list(ws.iter_rows(values_only=True))
    if not rows:
        raise SystemExit("RFID workbook sheet is empty")

    header = [str(cell or "").strip() for cell in rows[0]]
    normalized_header = [name.strip().lower() for name in header]
    try:
        farm_idx = normalized_header.index("farm")
        rfid_idx = normalized_header.index("rfid")
    except ValueError as exc:
        raise SystemExit("Farm/RFID columns not found") from exc

    out_wb = Workbook()
    out_ws = out_wb.active
    out_ws.title = args.sheet
    out_ws.append(header)

    excluded_rows: list[dict[str, str]] = []
    closed_farms: set[str] = set()
    reopened_farms: set[str] = set()
    current_farm = ""
    included = 0
    considered = 0
    for excel_row_number, row in enumerate(rows[1:], start=2):
        values = list(row)
        rfid = canonical_identifier(values[rfid_idx] if rfid_idx < len(values) else "")
        if not rfid:
            continue
        considered += 1
        farm = canonical_identifier(values[farm_idx] if farm_idx < len(values) else "")
        if farm and farm != current_farm:
            if current_farm:
                closed_farms.add(current_farm)
            if farm in closed_farms:
                reopened_farms.add(farm)
            current_farm = farm
        if farm not in reopened_farms:
            out_ws.append(values)
            included += 1
        else:
            excluded_rows.append(
                {
                    "excel_row_number": str(excel_row_number),
                    "farm": farm,
                    "rfid": rfid,
                    "reason": "reopened_farm_block_review",
                }
            )

    args.output_xlsx.parent.mkdir(parents=True, exist_ok=True)
    out_wb.save(args.output_xlsx)

    args.excluded_csv.parent.mkdir(parents=True, exist_ok=True)
    with args.excluded_csv.open("w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=["excel_row_number", "farm", "rfid", "reason"])
        writer.writeheader()
        writer.writerows(excluded_rows)

    summary = {
        "live_rows_considered": considered,
        "included_rows": included,
        "excluded_reopened_farm_review_rows": len(excluded_rows),
        "reopened_farms": sorted(reopened_farms),
    }
    args.summary_json.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
    print(json.dumps(summary, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
