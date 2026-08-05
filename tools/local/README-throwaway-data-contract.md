# Throwaway phone-QA database: data contract

## The contract

A throwaway phone-QA database (port **15546**) contains EXACTLY:

| Thing | Count |
|---|---|
| Parks | **2** — CBE, CPT |
| Sheds | **4 per park = 8** |
| Goats | **5 per shed = 40** (20 per park) |
| Anything else | **nothing** |

Each goat carries one RFID tag with a shed prefix, so the same physical tag can be
re-used across sheds during testing.

`tools/local/e2e-seed-15546.sh` enforces this and **fails loudly** if the shape
drifts, printing `parks/sheds/goats/sheds-with-goats/min-per-shed/max-per-shed`
against the expected `2/8/40/8/5/5`.

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
and the seed script already refuses any port other than 15546.

## Rules

1. The throwaway DB is **never** the local app DB. The seed refuses to run
   outside port 15546; the local stack lives on 5433 and must not be touched.
2. Do not "fix" the baseline migration by deleting its seed data — that changes
   schema history shared with `origin/main`. Prune after migrating instead.
3. If a test needs more data (more sheds, more animals), change the contract in
   the seed script AND this document together, so the assertion keeps matching
   the intent.
4. If the assertion fails, stop. A drifted database makes every subsequent
   "the app shows the wrong thing" investigation start from a false premise.
