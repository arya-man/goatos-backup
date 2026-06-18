#!/usr/bin/env python3
"""Compare two Goat OS identity snapshots exported by live-replay.sh."""

from __future__ import annotations

import argparse
import csv
import json
from collections import Counter
from decimal import Decimal, InvalidOperation
from pathlib import Path


FIELDS = [
    "display_id",
    "rfid",
    "old_tag",
    "lifecycle_status",
    "park",
    "breed",
    "sex",
    "age_band",
    "identity_state",
]
COMPARE_FIELDS = [
    "lifecycle_status",
    "park",
    "breed",
    "sex",
    "age_band",
    "identity_state",
]


def canonical_value(value: str) -> str:
    text = (value or "").strip()
    if not text:
        return ""
    try:
        if "e" in text.lower():
            text = str(int(Decimal(text)))
        elif "." in text:
            left, _, right = text.partition(".")
            if right.strip("0") == "":
                text = left
    except (InvalidOperation, ValueError):
        pass
    return "".join(ch for ch in text.upper() if ch.isalnum())


def canonical_old_tag(value: str) -> str:
    text = (value or "").strip()
    if not text:
        return ""
    parts = text.split(":")
    if len(parts) >= 3:
        scope = ":".join(parts[:-1]).lower()
        tag = canonical_value(parts[-1])
        return f"{scope}:{tag}" if tag else ""
    return canonical_value(text)


def identity_tokens(row: dict[str, str]) -> set[str]:
    tokens: set[str] = set()
    rfid = canonical_value(row.get("rfid", ""))
    old_tag = canonical_old_tag(row.get("old_tag", ""))
    if rfid:
        tokens.add(f"rfid:{rfid}")
    if old_tag:
        tokens.add(f"old:{old_tag}")
    return tokens


def load_snapshot(path: Path) -> list[dict[str, str]]:
    rows: list[dict[str, str]] = []
    with path.open(newline="") as f:
        reader = csv.DictReader(f, fieldnames=FIELDS, delimiter="\t")
        for row in reader:
            tokens = sorted(identity_tokens(row))
            key = "|".join(tokens) if tokens else f"display:{row.get('display_id', '')}"
            rows.append({**row, "identity_key": key, "_tokens": tokens})
    return rows


def match_rows(
    oracle: list[dict[str, str]],
    replay: list[dict[str, str]],
) -> tuple[list[tuple[dict[str, str], dict[str, str], str]], list[dict[str, str]], list[dict[str, str]]]:
    replay_by_token: dict[str, list[int]] = {}
    for idx, row in enumerate(replay):
        for token in row["_tokens"]:
            replay_by_token.setdefault(token, []).append(idx)

    matched_replay: set[int] = set()
    matches: list[tuple[dict[str, str], dict[str, str], str]] = []
    missing: list[dict[str, str]] = []
    for oracle_row in oracle:
        candidates: list[int] = []
        for token in oracle_row["_tokens"]:
            candidates.extend(replay_by_token.get(token, []))
        unique_candidates = sorted({idx for idx in candidates if idx not in matched_replay})
        if len(unique_candidates) == 1:
            replay_idx = unique_candidates[0]
            matched_replay.add(replay_idx)
            shared = sorted(set(oracle_row["_tokens"]) & set(replay[replay_idx]["_tokens"]))
            match_key = "|".join(shared) if shared else oracle_row["identity_key"]
            matches.append((oracle_row, replay[replay_idx], match_key))
        else:
            missing.append(oracle_row)

    extra = [row for idx, row in enumerate(replay) if idx not in matched_replay]
    return matches, missing, extra


def write_rows(path: Path, rows: list[dict[str, str]], fieldnames: list[str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        for row in rows:
            writer.writerow({field: row.get(field, "") for field in fieldnames})


def counter(rows: list[dict[str, str]], *keys: str) -> dict[str, int]:
    counts = Counter(tuple(item.get(key, "") for key in keys) for item in rows)
    return {"|".join(value or "(blank)" for value in values): count for values, count in counts.items()}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--oracle-tsv", required=True, type=Path)
    parser.add_argument("--replay-tsv", required=True, type=Path)
    parser.add_argument("--out-dir", required=True, type=Path)
    parser.add_argument("--summary-json", required=True, type=Path)
    args = parser.parse_args()

    oracle = load_snapshot(args.oracle_tsv)
    replay = load_snapshot(args.replay_tsv)
    matches, missing, extra = match_rows(oracle, replay)
    changed: list[dict[str, str]] = []
    for old, new, match_key in sorted(matches, key=lambda item: item[2]):
        diffs = {
            field: f"{old.get(field, '')} -> {new.get(field, '')}"
            for field in COMPARE_FIELDS
            if old.get(field, "") != new.get(field, "")
        }
        if diffs:
            changed.append(
                {
                    "identity_key": match_key,
                    "oracle_display_id": old.get("display_id", ""),
                    "replay_display_id": new.get("display_id", ""),
                    **diffs,
                }
            )

    write_rows(args.out_dir / "missing_in_replay.csv", missing, ["identity_key", *FIELDS])
    write_rows(args.out_dir / "extra_in_replay.csv", extra, ["identity_key", *FIELDS])
    changed_fields = ["identity_key", "oracle_display_id", "replay_display_id", *COMPARE_FIELDS]
    write_rows(args.out_dir / "changed_common_keys.csv", changed, changed_fields)

    summary = {
        "oracle_rows": len(oracle),
        "replay_rows": len(replay),
        "common_keys": len(matches),
        "missing_in_replay": len(missing),
        "extra_in_replay": len(extra),
        "changed_common_keys": len(changed),
        "missing_by_lifecycle": counter(missing, "lifecycle_status"),
        "extra_by_lifecycle": counter(extra, "lifecycle_status"),
        "missing_by_source_park_lifecycle": counter(missing, "park", "lifecycle_status"),
        "extra_by_source_park_lifecycle": counter(extra, "park", "lifecycle_status"),
    }
    args.summary_json.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
    print(json.dumps(summary, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
