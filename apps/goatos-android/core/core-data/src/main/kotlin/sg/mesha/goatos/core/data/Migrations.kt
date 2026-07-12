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
