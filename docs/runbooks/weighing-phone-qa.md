# Weighing Phone QA (Throwaway DB)

Use this when testing the Weighing module's Android scan/submit flow on a real
physical phone. This is a Weighing-specific pointer into the shared throwaway
phone-QA fixture; it does not duplicate that fixture. Read
`docs/runbooks/phone-qa-throwaway-rbac.md` first for the full throwaway-DB
setup, seed contents, RBAC test users, and the RFID-transform boundary rule —
this doc only calls out the parts of that flow that matter specifically for
Weighing QA.

## Same Throwaway Stack, No Separate Weighing DB

Weighing QA reuses the exact same disposable database, seed script, and app
run wrapper as the rest of phone QA:

- Database: `goatos-phone-qa` container on `127.0.0.1:15544` (never the normal
  `goatos-local-current` on `5433`).
- Setup, migrate, seed, and app-start commands: see "Start And Seed" in
  `docs/runbooks/phone-qa-throwaway-rbac.md`. There is no separate
  Weighing-only seed script — the shared `tools/local/phone-qa-throwaway-seed.sh`
  seeds both Vaccination and Weighing fixtures together, because a real phone
  session with five physical tags needs to exercise both modules against the
  same goat identities in one reset.

## Coverage This Fixture Must Exercise For Weighing

Per the shared fixture (`docs/runbooks/phone-qa-throwaway-rbac.md` → "Seeded
Work"), Weighing QA on a phone covers:

- **2 parks:** CBE and CPT.
- **>= 2 sheds per park:** CBE has `Godel 1` (whole shed) and `Yashoda 1`
  (partitioned, `Parts 1-3`); CPT has `Mandela 2` (whole shed) and `Castro 1`
  (partitioned, `Parts 1-3`). This deliberately covers both shed shapes (plain
  shed vs. partitioned shed) on both parks.
- **The 5 physical RFID tags:** the maintainer has five physical tags. The
  fixture maps them onto 10 goat identities (5 per park) because
  `goat_identifiers` is unique per tenant on the raw value — see "Seeded Work"
  in the shared runbook for the exact identifier list and why CPT identities
  use a `CPT-` prefixed value rather than a duplicate raw tag.
- **Free-flow behavior:** the Weighing fixture sets `expected_animal_count = 0`
  and inserts no `weighing_expected_animals` rows for any of the four sheds.
  QA must confirm the phone can scan and submit an observation for a tag with
  no pre-expected roster entry, and that the same physical tag scanned in two
  different campaign-shed buckets is accepted in both (never rejected as a
  duplicate across buckets).

## Debug-Only RFID Transformation Must Never Reach Weighing

The shared fixture's Android RFID transform (`901007000504418` →
`CPT-901007000504418` for CPT test sheds) exists ONLY to work around the
`goat_identifiers` uniqueness constraint for Vaccination phone QA with a
limited physical tag set. It is scoped to the Vaccination scan entry point and
lives in a debug-only source set (see "Android RFID Transform Boundary" and
"Production-safe naming rule" in `docs/runbooks/phone-qa-throwaway-rbac.md`).

For Weighing:

- Weighing must always pass the scanned RFID through **unchanged** — release
  and debug builds alike. Weighing free-flow accepts the raw
  `scanned_identifier` directly; it has no herd-identity uniqueness
  constraint to work around, so there is no reason for any transform to touch
  a Weighing scan.
- If a future debug helper is ever added near the Weighing scan path, it must
  live in a debug-only source set (never `main`/release source), must default
  to a no-op in shared/production code, and must never use a `QA`, `test`, or
  `alias` name in a production-facing symbol. Release builds must pass the
  scanned tag through unchanged with zero code-path difference from debug for
  the transform step.
- QA sign-off for a Weighing phone session must explicitly confirm: the value
  recorded in `weighing_observations.scanned_identifier` for a CPT scan
  matches the raw physical tag actually scanned, not a `CPT-`-prefixed or
  otherwise transformed value.

## What This Doc Does Not Cover

Test user roles/scopes (`operator`, `growth_director`, `ceo_internal`,
`verifier`), dev-token bootstrap, `adb reverse` wiring, and the full seeded
table are documented once in `docs/runbooks/phone-qa-throwaway-rbac.md` and
are not repeated here. Update that file, not this one, if the shared fixture's
users/seed/transform-boundary rules change; update this file only when a
Weighing-specific QA expectation changes.
