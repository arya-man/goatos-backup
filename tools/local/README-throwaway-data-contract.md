# Throwaway phone-QA database: data contract

## The contract

The current phone-QA throwaway database (port **15544**) is the role/device
fixture used by `tools/local/phone-qa-throwaway-run.sh` and
`tools/local/phone-qa-throwaway-seed.sh`.

The older `15546` scripts are a separate multi-device E2E stack. Do not cite
`15546` as the phone-QA role-test port.

The `15544` phone-QA fixture contains:

| Thing | Count |
|---|---|
| Parks | **2** — CBE, CPT |
| Sheds | **4 per park = 8** |
| Goats | **5 per shed = 40** (20 per park) |
| Anything else | **nothing** |

Each goat carries one RFID tag with a shed prefix, so the same physical tag can be
re-used across sheds during testing.

`tools/local/phone-qa-throwaway-seed.sh` enforces the role and identity
invariants and fails loudly if the fixture drifts. `tools/local/e2e-seed-15546.sh`
belongs only to the older `15546` E2E stack.

## Why a prune step exists

`backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql` is **not** a
clean slate. Despite the name it embeds a production-shaped location dump —
158 rows (Coimbatore 78 sheds, Channapatna 76, plus parks and farms) with
hardcoded `2026-07-16` timestamps, at lines ~5568 and ~5669.

Every database built from migrations inherits them, throwaway included. They are
inert (no goat, task or campaign attaches to them) but they leak into any screen
that lists locations without a scope filter, and they make "how many sheds are
there?" unanswerable during a test.

**Migrations are NOT modified.** The schema must match `origin/main` exactly,
because the throwaway DB runs against backend code built from `origin/main`. We
run every migration, then delete the DATA that the test does not need.

## What the prune keeps

Every location that owns a goat, an SOP task, or a weighing campaign — plus the
ancestors of anything kept, so a shed never loses its park. Everything else goes,
along with its `location_aliases`, `location_operational_attributes` and
`farm_profiles` rows.

Deleting a seeded CBE/CPT/HF scope row trips `location_seeded_scope_guard()`, so
the prune declares `goatos.approved_location_migration_plan` for the session.
That is legitimate here and ONLY here: the database is disposable by definition,
and the phone-QA seed script already refuses any port other than `15544`.

## Rules

1. The phone-QA throwaway DB is **never** the local app DB. The seed refuses to
   run outside port `15544`; the local stack lives on `5433` and must not be
   touched.
2. Do not "fix" the baseline migration by deleting its seed data — that changes
   schema history shared with `origin/main`. Prune after migrating instead.
3. If a test needs more data (more sheds, more animals), change the contract in
   the seed script AND this document together, so the assertion keeps matching
   the intent.
4. If the assertion fails, stop. A drifted database makes every subsequent
   "the app shows the wrong thing" investigation start from a false premise.
