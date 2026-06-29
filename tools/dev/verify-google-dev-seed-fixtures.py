#!/usr/bin/env python3
"""Validate committed Google-dev clean-slate seed fixtures."""

from __future__ import annotations

import csv
import json
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
FIXTURE_DIR = ROOT / "fixtures" / "google-dev-clean-slate"

GOAT_HEADERS = [
    "Farm",
    "RFID",
    "Old tag",
    "Temp field ID",
    "Park",
    "Shed",
    "Breed",
    "Sex",
    "DOB",
    "Management stage",
    "Entry date",
    "Weight(kg)",
    "Dam ID",
    "Sire/lot",
    "Origin",
    "Photo URL",
]

SHED_HEADERS = ["Park", "Shed code", "Shed name", "Display order", "Notes"]

REQUIRED_SCENARIOS = {
    "eligible_day21",
    "age_ineligible",
    "stage_profile_mismatch",
    "location_ineligible",
    "missing_dob",
    "shifted",
    "lifecycle_ineligible",
    "exited_cleanup",
    "sick_recovered",
    "stock_blocked",
    "missed",
    "proof_pending",
    "rejected_rework",
    "completed_verified",
}

BAD_BOUNDARY_TOKENS = tuple(
    "".join(parts)
    for parts in (
        ("H", "eva"),
        ("S", "lice"),
        ("system", "-gsuite"),
        ("apps", "-script"),
        ("he", "va", "platform"),
    )
)


def fail(message: str) -> None:
    print(f"FAIL: {message}", file=sys.stderr)
    raise SystemExit(1)


def read_csv(path: Path) -> tuple[list[str], list[dict[str, str]]]:
    if not path.exists():
        fail(f"missing fixture {path.relative_to(ROOT)}")
    with path.open(newline="") as fh:
        reader = csv.DictReader(fh)
        headers = reader.fieldnames or []
        rows = [{key: (value or "").strip() for key, value in row.items()} for row in reader]
    if not rows:
        fail(f"{path.relative_to(ROOT)} must contain at least one data row")
    return headers, rows


def assert_headers(name: str, got: list[str], want: list[str]) -> None:
    if got != want:
        fail(f"{name} headers {got!r} do not match expected import template {want!r}")


def assert_no_boundary_tokens() -> None:
    for path in FIXTURE_DIR.iterdir():
        if not path.is_file():
            continue
        text = path.read_text()
        for token in BAD_BOUNDARY_TOKENS:
            if token in text:
                fail(f"{path.relative_to(ROOT)} contains forbidden boundary token {token!r}")


def validate_sheds() -> set[str]:
    headers, rows = read_csv(FIXTURE_DIR / "sheds.csv")
    assert_headers("sheds.csv", headers, SHED_HEADERS)
    seen_codes: set[str] = set()
    for index, row in enumerate(rows, start=2):
        park = row["Park"]
        code = row["Shed code"]
        name = row["Shed name"]
        if park not in {"CBE", "CPT"}:
            fail(f"sheds.csv row {index} park must be CBE or CPT, got {park!r}")
        if not code.startswith("GDEV-"):
            fail(f"sheds.csv row {index} shed code must start GDEV-, got {code!r}")
        if not name:
            fail(f"sheds.csv row {index} shed name is required")
        if code in seen_codes:
            fail(f"sheds.csv duplicate shed code {code!r}")
        seen_codes.add(code)
        try:
            display_order = int(row["Display order"])
        except ValueError:
            fail(f"sheds.csv row {index} display order must be an integer")
        if display_order < 0:
            fail(f"sheds.csv row {index} display order must be non-negative")
    return seen_codes


def validate_goats(shed_codes: set[str]) -> set[str]:
    headers, rows = read_csv(FIXTURE_DIR / "goats.csv")
    assert_headers("goats.csv", headers, GOAT_HEADERS)
    seen_rfids: set[str] = set()
    for index, row in enumerate(rows, start=2):
        rfid = row["RFID"]
        if not rfid.startswith("GDEV-"):
            fail(f"goats.csv row {index} RFID must start GDEV-, got {rfid!r}")
        if rfid in seen_rfids:
            fail(f"goats.csv duplicate RFID {rfid!r}")
        seen_rfids.add(rfid)
        if row["Park"] not in {"CBE", "CPT"}:
            fail(f"goats.csv row {index} park must be CBE or CPT")
        if row["Shed"] not in shed_codes:
            fail(f"goats.csv row {index} references unknown shed {row['Shed']!r}")
        if row["Sex"] not in {"female", "male", "unknown"}:
            fail(f"goats.csv row {index} invalid sex {row['Sex']!r}")
        if row["Origin"] not in {"birth", "procured", "imported", "unknown"}:
            fail(f"goats.csv row {index} invalid origin {row['Origin']!r}")
        if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", row["Entry date"]):
            fail(f"goats.csv row {index} entry date must be YYYY-MM-DD")
        if row["DOB"] and not re.fullmatch(r"\d{4}-\d{2}-\d{2}", row["DOB"]):
            fail(f"goats.csv row {index} DOB must be blank or YYYY-MM-DD")
        if row["Weight(kg)"]:
            try:
                weight = float(row["Weight(kg)"])
            except ValueError:
                fail(f"goats.csv row {index} weight must be numeric")
            if weight < 0:
                fail(f"goats.csv row {index} weight must be non-negative")
    return seen_rfids


def validate_ledger(rfids: set[str]) -> None:
    path = FIXTURE_DIR / "seed-ledger.json"
    if not path.exists():
        fail("missing seed-ledger.json")
    with path.open() as fh:
        ledger = json.load(fh)
    scenarios = ledger.get("scenarios")
    if not isinstance(scenarios, list):
        fail("seed-ledger.json scenarios must be a list")
    scenario_names = {row.get("scenario") for row in scenarios}
    missing = REQUIRED_SCENARIOS - scenario_names
    extra_required = set(ledger.get("required_scenarios", [])) ^ REQUIRED_SCENARIOS
    if missing:
        fail(f"seed-ledger.json missing scenarios: {sorted(missing)}")
    if extra_required:
        fail(f"seed-ledger.json required_scenarios drift: {sorted(extra_required)}")
    for row in scenarios:
        scenario = row.get("scenario")
        rfid = row.get("rfid")
        if scenario not in REQUIRED_SCENARIOS:
            fail(f"seed-ledger.json unknown scenario {scenario!r}")
        if rfid not in rfids:
            fail(f"seed-ledger.json scenario {scenario!r} references RFID {rfid!r} absent from goats.csv")
        if not row.get("expected"):
            fail(f"seed-ledger.json scenario {scenario!r} must document expected validation")


def validate_sql_helpers() -> None:
    for name in ("post-import-shed-profiles.sql", "post-import-inventory-lots.sql"):
        path = FIXTURE_DIR / name
        text = path.read_text()
        if ":'tenant_id'::uuid" not in text:
            fail(f"{name} must be tenant-scoped through psql variable tenant_id")
        if "\\set ON_ERROR_STOP on" not in text:
            fail(f"{name} must fail closed with ON_ERROR_STOP")
        if "COMMIT;" not in text:
            fail(f"{name} must commit explicitly")
        if "DO $$" in text:
            fail(f"{name} must not hide psql variables inside dollar-quoted DO blocks")


def main() -> int:
    if not FIXTURE_DIR.exists():
        fail(f"missing fixture dir {FIXTURE_DIR.relative_to(ROOT)}")
    assert_no_boundary_tokens()
    shed_codes = validate_sheds()
    rfids = validate_goats(shed_codes)
    validate_ledger(rfids)
    validate_sql_helpers()
    print(
        "google-dev clean-slate fixtures OK: "
        f"{len(shed_codes)} sheds, {len(rfids)} goats, {len(REQUIRED_SCENARIOS)} scenarios"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
