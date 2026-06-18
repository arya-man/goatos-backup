#!/usr/bin/env python3
"""Generate deterministic local replay inputs from live BQ/Sheets exports.

This intentionally does not talk to Google APIs. The shell harness owns live
export; this script turns those exported files plus the current throwaway DB
identifier snapshot into the two derived artifacts the replay needs:

* bq_latest_goat_locations.json
* safe-old-tag-passport-backfill-candidates.csv
"""

from __future__ import annotations

import argparse
import csv
import json
import re
from collections import Counter, defaultdict
from dataclasses import dataclass, replace
from datetime import date, datetime
from pathlib import Path


NON_ALNUM = re.compile(r"[^A-Z0-9]+")
HEADER = [
    "candidate_source",
    "farm",
    "scope_key",
    "goat_id",
    "goat_key",
    "breed",
    "gender",
    "status",
    "last_event",
    "event_date",
    "last_shed",
    "local_match_count",
    "local_display_ids",
    "recommended_action",
    "reason",
]


def canonical_identifier(raw: str | None) -> str:
    value = (raw or "").strip().replace(",", "").replace(" ", "")
    if not value:
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


def is_rfid_like(value: str) -> bool:
    return len(value) >= 12 and value.isdigit()


def is_placeholder_goat_key(value: str) -> bool:
    return value in {"NOTAG"}


def normalized_farm(value: str | None) -> str:
    farm = NON_ALNUM.sub("", (value or "").strip().upper())
    return farm


def supported_status(value: str | None) -> str:
    raw = (value or "").strip().lower()
    if raw == "active":
        return "Active"
    if raw == "sold":
        return "Sold"
    if raw == "dead":
        return "Dead"
    if raw == "inactive":
        return "Inactive"
    return ""


def status_for_event(value: str | None) -> str:
    raw = (value or "").strip().lower()
    if raw == "death":
        return "Dead"
    if raw == "sale":
        return "Sold"
    return ""


def parse_date(value: str | None) -> date | None:
    text = (value or "").strip()
    if not text:
        return None
    for fmt in ("%Y-%m-%d", "%d/%m/%Y", "%m/%d/%Y"):
        try:
            return datetime.strptime(text, fmt).date()
        except ValueError:
            pass
    return None


def meaningful(value: str | None) -> str:
    text = (value or "").strip()
    if text in {"", "-"}:
        return ""
    return text


def read_csv(path: Path) -> list[dict[str, str]]:
    with path.open(newline="") as f:
        return list(csv.DictReader(f))


def write_csv(path: Path, rows: list[dict[str, str]], header: list[str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=header)
        writer.writeheader()
        for row in rows:
            writer.writerow({key: row.get(key, "") for key in header})


@dataclass
class ExistingIdentifiers:
    old_tags: dict[str, list[str]]
    rfids: set[str]


def read_existing_identifiers(path: Path | None) -> ExistingIdentifiers:
    if path is None or not path.exists():
        return ExistingIdentifiers(old_tags={}, rfids=set())
    old_tags: dict[str, list[str]] = defaultdict(list)
    rfids: set[str] = set()
    for row in read_csv(path):
        identifier_type = (row.get("identifier_type") or "old_tag").strip().lower()
        scope = (row.get("scope_key") or "").strip().lower()
        value = canonical_identifier(row.get("normalized_value") or row.get("identifier_value"))
        if not value:
            continue
        if identifier_type == "rfid":
            rfids.add(value)
            continue
        if identifier_type != "old_tag" or not scope:
            continue
        display = (row.get("display_id") or "").strip()
        old_tags[f"{scope}|{value}"].append(display)
    return ExistingIdentifiers(old_tags=old_tags, rfids=rfids)


@dataclass(frozen=True)
class CurrentIdentity:
    farm: str
    goat_id: str
    goat_key: str
    breed: str
    gender: str
    status: str
    last_event: str
    event_date: str
    last_shed: str
    source_event_count: str

    @property
    def scope_key(self) -> str:
        return f"park:{self.farm}"

    @property
    def identity_key(self) -> str:
        return f"{self.scope_key.lower()}|{self.goat_key}"

    @property
    def attr_fingerprint(self) -> str:
        return "|".join(
            [
                self.breed.strip().lower(),
                self.gender.strip().lower(),
                self.status.strip().lower(),
            ]
        )


def load_current_identities(path: Path) -> list[CurrentIdentity]:
    identities: list[CurrentIdentity] = []
    for row in read_csv(path):
        farm = normalized_farm(row.get("farm"))
        goat_key = canonical_identifier(row.get("goat_key") or row.get("goat_id"))
        status = supported_status(row.get("current_status") or row.get("status"))
        if not farm or not goat_key:
            continue
        identities.append(
            CurrentIdentity(
                farm=farm,
                goat_id=(row.get("goat_id") or "").strip(),
                goat_key=goat_key,
                breed=(row.get("breed") or "").strip(),
                gender=(row.get("gender") or "").strip(),
                status=status,
                last_event=(row.get("last_event") or "").strip(),
                event_date=(row.get("last_event_date") or row.get("event_date") or "").strip(),
                last_shed=(row.get("last_shed") or "").strip(),
                source_event_count=(row.get("source_event_count") or "").strip(),
            )
        )
    identities.sort(key=lambda r: (r.farm, r.goat_key, r.goat_id))
    return identities


def load_terminal_statuses(path: Path | None) -> dict[str, tuple[date, str, str]]:
    if path is None:
        return {}
    terminal: dict[str, tuple[date, str, str]] = {}
    for row in read_csv(path):
        farm = normalized_farm(row.get("Farm") or row.get("farm"))
        goat_key = canonical_identifier(row.get("Goat ID") or row.get("goat_id"))
        status = status_for_event(row.get("Event") or row.get("event"))
        event_date = parse_date(row.get("Date") or row.get("date"))
        if not farm or not goat_key or not status or event_date is None:
            continue
        key = f"{farm}|{goat_key}"
        current = terminal.get(key)
        if current is None or event_date > current[0] or (event_date == current[0] and status == "Dead"):
            terminal[key] = (event_date, status, row.get("Event") or row.get("event") or status)
    return terminal


def apply_terminal_status_overrides(
    identities: list[CurrentIdentity],
    terminal_statuses: dict[str, tuple[date, str, str]],
) -> tuple[list[CurrentIdentity], Counter[str]]:
    overrides: Counter[str] = Counter()
    out: list[CurrentIdentity] = []
    for row in identities:
        terminal = terminal_statuses.get(f"{row.farm}|{row.goat_key}")
        if terminal is None:
            out.append(row)
            continue
        if row.status == "Inactive":
            out.append(row)
            continue
        terminal_date, terminal_status, terminal_event = terminal
        current_date = parse_date(row.event_date)
        if current_date is not None and terminal_date < current_date:
            out.append(row)
            continue
        if row.status != terminal_status:
            overrides[f"{row.status or '(blank)'}->{terminal_status}"] += 1
        out.append(
            replace(
                row,
                status=terminal_status,
                last_event=terminal_event,
                event_date=terminal_date.isoformat(),
            )
        )
    return out, overrides


def build_locations(identities: list[CurrentIdentity]) -> list[dict[str, str]]:
    out = []
    for row in identities:
        event = row.last_event
        if row.status == "Sold":
            event = "Sale"
        elif row.status == "Dead":
            event = "Death"
        elif row.status == "Active" and not event:
            event = "Shifting"
        out.append(
            {
                "age": "",
                "breed": row.breed,
                "current_shed": row.last_shed,
                "date": row.event_date,
                "dst_shed": row.last_shed,
                "event": event,
                "farm": row.farm,
                "farm_goat_id": f"{row.farm}{row.goat_key}",
                "gender": row.gender,
                "goat_id": row.goat_id,
                "src_shed": "",
            }
        )
    return out


def build_bq_candidates(
    identities: list[CurrentIdentity],
    existing: ExistingIdentifiers,
) -> tuple[list[dict[str, str]], list[dict[str, str]], Counter[str], set[str]]:
    groups: dict[str, list[CurrentIdentity]] = defaultdict(list)
    for row in identities:
        groups[row.identity_key].append(row)

    candidates: list[dict[str, str]] = []
    skipped: list[dict[str, str]] = []
    reasons: Counter[str] = Counter()
    emitted: set[str] = set()

    def skip(row: CurrentIdentity, reason: str, detail: str = "") -> None:
        reasons[reason] += 1
        skipped.append(
            {
                "source": "bq_current_latest",
                "farm": row.farm,
                "scope_key": row.scope_key,
                "goat_id": row.goat_id,
                "goat_key": row.goat_key,
                "breed": row.breed,
                "gender": row.gender,
                "status": row.status,
                "last_event": row.last_event,
                "event_date": row.event_date,
                "last_shed": row.last_shed,
                "reason": reason,
                "detail": detail,
            }
        )

    for row in identities:
        if row.farm not in {"CBE", "CPT"}:
            skip(row, "unsupported_farm")
            continue
        if is_placeholder_goat_key(row.goat_key):
            skip(row, "placeholder_goat_id")
            continue
        if is_rfid_like(row.goat_key) and row.goat_key in existing.rfids:
            skip(row, "already_present_rfid_identity")
            continue
        if not row.status:
            skip(row, "unsupported_status")
            continue
        if row.status == "Inactive":
            skip(row, "inactive_bq_rows_are_not_auto_backfilled")
            continue
        if row.last_event.strip().lower() == "abortion":
            skip(row, "manual_review_abortion_event")
            continue
        if row.identity_key in existing.old_tags:
            skip(row, "already_present", "|".join(existing.old_tags[row.identity_key]))
            continue
        if not meaningful(row.breed):
            skip(row, "missing_breed")
            continue
        if not meaningful(row.gender):
            skip(row, "missing_gender")
            continue
        conflicting = {candidate.attr_fingerprint for candidate in groups[row.identity_key]}
        if len(conflicting) > 1:
            skip(row, "ambiguous_duplicate", f"{len(conflicting)} attribute fingerprints")
            continue
        if row.identity_key in emitted:
            skip(row, "duplicate_in_bq")
            continue
        emitted.add(row.identity_key)
        candidates.append(
            {
                "candidate_source": "bq_current_latest",
                "farm": row.farm,
                "scope_key": row.scope_key,
                "goat_id": row.goat_id,
                "goat_key": row.goat_key,
                "breed": row.breed,
                "gender": row.gender,
                "status": row.status,
                "last_event": row.last_event,
                "event_date": row.event_date,
                "last_shed": row.last_shed or "-",
                "local_match_count": "0",
                "local_display_ids": "",
                "recommended_action": "auto_backfill_old_tag_passport",
                "reason": "deterministic farm+old_tag+breed+gender+status from BQ latest event",
            }
        )
    return candidates, skipped, reasons, emitted


def load_census_rows(path: Path | None) -> list[dict[str, str]]:
    if path is None:
        return []
    return read_csv(path)


def build_census_candidates(
    census_rows: list[dict[str, str]],
    identities: list[CurrentIdentity],
    existing: ExistingIdentifiers,
    emitted_keys: set[str],
) -> tuple[list[dict[str, str]], list[dict[str, str]], Counter[str]]:
    by_goat_key: dict[str, list[CurrentIdentity]] = defaultdict(list)
    for row in identities:
        by_goat_key[row.goat_key].append(row)

    candidates: list[dict[str, str]] = []
    skipped: list[dict[str, str]] = []
    reasons: Counter[str] = Counter()

    def get(row: dict[str, str], *names: str) -> str:
        for name in names:
            if name in row:
                return row.get(name, "")
        return ""

    def skip(row: dict[str, str], goat_key: str, reason: str, detail: str = "") -> None:
        reasons[reason] += 1
        skipped.append(
            {
                "source": "census_plus_bq_unique_farm",
                "farm": "",
                "scope_key": "",
                "goat_id": get(row, "ID", "goat_id"),
                "goat_key": goat_key,
                "breed": get(row, "Breed", "breed"),
                "gender": get(row, "Gender", "gender"),
                "status": "",
                "last_event": "",
                "event_date": "",
                "last_shed": get(row, "Shed", "shed"),
                "reason": reason,
                "detail": detail,
            }
        )

    for source_row in census_rows:
        goat_id = get(source_row, "ID", "goat_id")
        goat_key = canonical_identifier(goat_id)
        if not goat_key:
            skip(source_row, goat_key, "missing_goat_id")
            continue
        if is_placeholder_goat_key(goat_key):
            skip(source_row, goat_key, "placeholder_goat_id")
            continue
        breed = meaningful(get(source_row, "Breed", "breed"))
        gender = meaningful(get(source_row, "Gender", "gender"))
        shed = meaningful(get(source_row, "Shed", "shed"))
        if not breed or not gender or not shed:
            skip(source_row, goat_key, "missing_census_breed_gender_or_shed")
            continue

        all_bq_farms = sorted({row.farm for row in by_goat_key.get(goat_key, []) if row.farm})
        if len(all_bq_farms) > 1:
            skip(source_row, goat_key, "ambiguous_cross_farm_bq_farm", "|".join(all_bq_farms))
            continue

        matches = [row for row in by_goat_key.get(goat_key, []) if row.farm in {"CBE", "CPT"}]
        farms = sorted({row.farm for row in matches})
        if len(farms) != 1:
            skip(source_row, goat_key, "ambiguous_or_missing_bq_farm", "|".join(farms))
            continue
        farm = farms[0]
        statuses = sorted({row.status for row in matches if row.status})
        if len(statuses) != 1:
            skip(source_row, goat_key, "ambiguous_or_missing_bq_status", "|".join(statuses))
            continue
        status = statuses[0]
        if status not in {"Active", "Sold", "Inactive"}:
            skip(source_row, goat_key, "unsupported_bq_status", status)
            continue

        identity_key = f"park:{farm}".lower() + f"|{goat_key}"
        if identity_key in existing.old_tags:
            skip(source_row, goat_key, "already_present", "|".join(existing.old_tags[identity_key]))
            continue
        if identity_key in emitted_keys:
            skip(source_row, goat_key, "already_emitted_from_bq")
            continue

        emitted_keys.add(identity_key)
        candidates.append(
            {
                "candidate_source": "census_plus_bq_unique_farm",
                "farm": farm,
                "scope_key": f"park:{farm}",
                "goat_id": goat_id.strip(),
                "goat_key": goat_key,
                "breed": breed,
                "gender": gender,
                "status": status,
                "last_event": "",
                "event_date": "",
                "last_shed": shed,
                "local_match_count": "0",
                "local_display_ids": "",
                "recommended_action": "auto_backfill_old_tag_passport",
                "reason": "Census supplies breed/gender/shed; BQ uniquely supplies farm/status for this old tag; missing in Goat OS",
            }
        )

    return candidates, skipped, reasons


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--current-identities-csv", required=True, type=Path)
    parser.add_argument("--census-csv", type=Path)
    parser.add_argument("--goats-db-events-csv", type=Path)
    parser.add_argument("--existing-old-tags-csv", type=Path)
    parser.add_argument("--existing-identifiers-csv", type=Path)
    parser.add_argument("--locations-json-out", required=True, type=Path)
    parser.add_argument("--candidates-csv-out", required=True, type=Path)
    parser.add_argument("--skipped-csv-out", required=True, type=Path)
    parser.add_argument("--summary-json-out", required=True, type=Path)
    args = parser.parse_args()

    identities = load_current_identities(args.current_identities_csv)
    terminal_statuses = load_terminal_statuses(args.goats_db_events_csv)
    identities, terminal_overrides = apply_terminal_status_overrides(identities, terminal_statuses)
    existing = read_existing_identifiers(args.existing_identifiers_csv or args.existing_old_tags_csv)
    census_rows = load_census_rows(args.census_csv)
    locations = build_locations(identities)
    candidates, skipped, skip_reasons, emitted = build_bq_candidates(identities, existing)
    census_candidates, census_skipped, census_skip_reasons = build_census_candidates(
        census_rows,
        identities,
        existing,
        emitted,
    )
    candidates.extend(census_candidates)
    skipped.extend(census_skipped)
    skip_reasons.update(census_skip_reasons)

    args.locations_json_out.parent.mkdir(parents=True, exist_ok=True)
    args.locations_json_out.write_text(json.dumps(locations, indent=2, sort_keys=True) + "\n")
    write_csv(args.candidates_csv_out, candidates, HEADER)
    write_csv(
        args.skipped_csv_out,
        skipped,
        [
            "source",
            "farm",
            "scope_key",
            "goat_id",
            "goat_key",
            "breed",
            "gender",
            "status",
            "last_event",
            "event_date",
            "last_shed",
            "reason",
            "detail",
        ],
    )
    summary = {
        "current_identities": len(identities),
        "census_rows": len(census_rows),
        "existing_old_tag_keys": len(existing.old_tags),
        "existing_rfid_keys": len(existing.rfids),
        "locations_written": len(locations),
        "terminal_status_keys": len(terminal_statuses),
        "terminal_status_overrides": dict(sorted(terminal_overrides.items())),
        "candidate_rows": len(candidates),
        "skipped_rows": len(skipped),
        "skipped_by_reason": dict(sorted(skip_reasons.items())),
        "candidates_by_source": dict(sorted(Counter(row["candidate_source"] for row in candidates).items())),
        "candidates_by_farm": dict(sorted(Counter(row["farm"] for row in candidates).items())),
        "candidates_by_status": dict(sorted(Counter(row["status"] for row in candidates).items())),
    }
    args.summary_json_out.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
    print(json.dumps(summary, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
