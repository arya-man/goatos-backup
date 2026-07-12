# Room migration safety — an installed APK must survive every schema change

**Status:** enforced (`make room-migration-guard`, CI `guardrails` job)
**Applies to:** every Android `@Database` under `apps/goatos-android/**` (currently
`GoatDatabase` in `core/core-data`, `OutboxDatabase` in `core/core-database`).
**Machine gate:** `tools/agent-hooks/check-room-migration-safety.mjs`.
**Regression tests:** `GoatDatabaseMigrationTest`, `OutboxDatabaseMigrationTest`,
`GoatDatabaseUpgradeCrashTest`, `OutboxDatabaseUpgradeCrashTest`.

## The anti-pattern (a real defect this repo shipped)

Room builds a database two completely different ways:

- **Fresh install** → `createAllTables()` creates **every** `@Entity` table from scratch.
- **Upgrade of an already-installed app** → Room runs only the registered `Migration`
  objects from the old `version` to the new one, then **validates** the resulting schema
  against the `@Entity` definitions. Any table/column/index the migrations did not produce
  → `IllegalStateException: Migration didn't properly handle <table>` **on database open**,
  i.e. the app crashes on the first launch after the update.

The trap: **adding an `@Entity` to a `@Database` with no migration to create its table
compiles cleanly and passes every fresh-install test.** MOB-007 did exactly this —
`roster_timetable_cache` and `roster_coverage_cache` were added as entities with no
migration. New installs got them via `createAllTables`; every in-place upgrade crashed.
A plain `Room.inMemoryDatabaseBuilder(...).build()` test only ever exercises the
fresh-install path, so it is **blind** to this. The bug is invisible until a real user
updates the app.

## The required pattern

1. **`exportSchema = true`** on every `@Database`, with `ksp { arg("room.schemaLocation",
   "$projectDir/schemas") }` in the module `build.gradle.kts`. Commit the generated
   `schemas/<db>/<version>.json` — it is reviewable as a diff and is the golden schema
   `MigrationTestHelper` validates against.
2. **Every schema-changing bump ships its migration.** Version `N` requires a
   `Migration(N-1, N)` registered on the builder (`addMigrations(...)`), and that migration
   must `CREATE`/`ALTER` exactly the tables/columns/indices the new `@Entity` set expects.
3. **Non-destructive by default.** No `fallbackToDestructiveMigration()` — the outbox holds
   not-yet-synced operator writes and the cache is the offline SSOT; dropping either loses
   real data. Migrations are additive (`CREATE TABLE IF NOT EXISTS`, `ALTER TABLE ADD COLUMN
   … DEFAULT …`).
4. **Two test layers, both under a plain `testDebugUnitTest` (Robolectric, no device):**
   - **Schema-equivalence** (`*MigrationTest`) — build the real old schema, run every
     migration, assert the result is structurally identical to a fresh Room-created current
     DB (the `@Entity` truth). Catches a migration that produces the wrong shape.
   - **Upgrade-crash E2E** (`*UpgradeCrashTest`) — lay down a real old-version file via a
     test-only old `@Database` (so it carries Room's own identity metadata, exactly like a
     shipped APK), seed real rows, then reopen it with the current schema + real migrations.
     Asserts Room's actual production open path does **not** crash **and** the seeded data
     survives (e.g. a pending outbox write keeps its payload; a new column defaults correctly).
     This is the test that would have caught the roster crash.
   - `MigrationTestHelper` (golden-schema) validation guards the committed baseline and gives
     every *future* migration `runMigrationsAndValidate` coverage for free.

## What the guard enforces (`make room-migration-guard`)

Diff-scoped (a commit touching no Room DB/migration/schema passes instantly); `--all` audits
the whole tree; `--self-test` runs fixtures. Rules:

| Rule | Blocks |
|---|---|
| `export-schema-off` | a `@Database` with `exportSchema = false` or omitted |
| `missing-golden-schema` | no committed `schemas/<pkg.Class>/<version>.json` for the declared version |
| `version-bump-without-migration` | `version` rose vs base with no `Migration(N-1, N)` in the module |
| `entity-without-migration` | a table new in schema `vK+1` that `Migration(K, K+1)` does not `CREATE` — the roster defect |

Escape hatch (justified exceptions only): append `room-migration-guard:ignore: <reason>` on
the `@Database` `version`/`exportSchema` line.

## Relation to the backend

This is the on-device twin of the backend/Postgres migration discipline
(`.agents/skills/db-migration-safety`). Same principle — a schema change must carry a
lock-safe, data-preserving migration and be proven against the *upgrade* path, not just a
fresh create.
