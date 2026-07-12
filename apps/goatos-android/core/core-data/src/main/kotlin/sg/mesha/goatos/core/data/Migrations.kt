package sg.mesha.goatos.core.data

import androidx.room.migration.Migration
import androidx.sqlite.db.SupportSQLiteDatabase

/**
 * v1 -> v2: adds the per-read-model cache tables for the offline-first read screens
 * (docs/decisions/android-offline-first.md). Each is a JSON-blob-by-scope cache mirroring
 * `bootstrap_cache`'s shape (cacheKey TEXT PRIMARY KEY, dtoJson TEXT, updatedAt INTEGER).
 * Purely additive — no existing table changes, so no destructive fallback is needed.
 */
val MIGRATION_1_2: Migration = object : Migration(1, 2) {
    override fun migrate(db: SupportSQLiteDatabase) {
        listOf(
            "calendar_cache",
            "control_tower_cache",
            "execution_rows_cache",
            "execution_shed_cache",
            "scan_roster_cache",
            "adherence_cache",
            "insights_gaps_cache",
            "insights_coverage_cache",
        ).forEach { table ->
            db.execSQL(
                "CREATE TABLE IF NOT EXISTS `$table` " +
                    "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                    "PRIMARY KEY(`cacheKey`))",
            )
        }
    }
}

/**
 * v2 -> v3: adds the task-detail cache table (MOB-001 — Scan -> Submit operator task read
 * offline-first). Purely additive, same JSON-blob-by-scope shape as [MIGRATION_1_2]'s tables.
 */
val MIGRATION_2_3: Migration = object : Migration(2, 3) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `task_detail_cache` " +
                "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`cacheKey`))",
        )
    }
}

/**
 * v3 -> v4: adds the scanned-goat + proof-capture tables (MOB-002,
 * docs/mobile/proof-capture-sync-and-e2e.md §3) — Room-first SSOT for Submit's `goat_scan` /
 * `video_proof` recording-form controls. Purely additive.
 *
 * Also repairs a missing-migration defect: MOB-007 (commit 42d961d2) added the
 * `roster_timetable_cache` / `roster_coverage_cache` @Entity tables to [GoatDatabase] but never
 * added a migration to create them, so fresh installs got them via Room's createAllTables while
 * every in-place upgrade crashed on open ("Migration didn't properly handle roster_timetable_cache").
 * These two `IF NOT EXISTS` creates fix all upgrade paths without a version bump because every path
 * to v4 runs this migration; new installs are unaffected (the tables already exist). Caught by
 * GoatDatabaseMigrationTest's schema-equivalence check.
 */
val MIGRATION_3_4: Migration = object : Migration(3, 4) {
    override fun migrate(db: SupportSQLiteDatabase) {
        listOf(
            "roster_timetable_cache",
            "roster_coverage_cache",
        ).forEach { table ->
            db.execSQL(
                "CREATE TABLE IF NOT EXISTS `$table` " +
                    "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                    "PRIMARY KEY(`cacheKey`))",
            )
        }
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `scanned_goat_capture` " +
                "(`id` TEXT NOT NULL, `taskId` TEXT NOT NULL, `fieldKey` TEXT NOT NULL, " +
                "`tag` TEXT NOT NULL, `capturedAtMs` INTEGER NOT NULL, " +
                "`syncStatus` TEXT NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE UNIQUE INDEX IF NOT EXISTS `index_scanned_goat_capture_taskId_fieldKey_tag` " +
                "ON `scanned_goat_capture` (`taskId`, `fieldKey`, `tag`)",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_scanned_goat_capture_taskId_fieldKey_capturedAtMs` " +
                "ON `scanned_goat_capture` (`taskId`, `fieldKey`, `capturedAtMs`)",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `proof_capture` " +
                "(`id` TEXT NOT NULL, `taskId` TEXT NOT NULL, `fieldKey` TEXT NOT NULL, " +
                "`proofSubject` TEXT NOT NULL, `localUri` TEXT NOT NULL, `mimeType` TEXT NOT NULL, " +
                "`caption` TEXT, `capturedAtMs` INTEGER NOT NULL, " +
                "`capturedStartMs` INTEGER NOT NULL DEFAULT 0, `capturedEndMs` INTEGER NOT NULL DEFAULT 0, " +
                "`capturedByPrincipalId` TEXT, `syncStatus` TEXT NOT NULL, " +
                "`idempotencyKey` TEXT NOT NULL, `outboxItemId` TEXT, `serverProofId` TEXT, " +
                "`lastError` TEXT, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE UNIQUE INDEX IF NOT EXISTS `index_proof_capture_idempotencyKey` " +
                "ON `proof_capture` (`idempotencyKey`)",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_proof_capture_taskId_fieldKey` " +
                "ON `proof_capture` (`taskId`, `fieldKey`)",
        )
    }
}
