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

/**
 * v4 -> v5: adds the verification-queue cache table — the standalone Verifier section's
 * category-filtered media queue (context/architecture/verifier-app-and-flow.md). Purely
 * additive, same JSON-blob-by-scope shape as [MIGRATION_1_2]'s tables.
 */
val MIGRATION_4_5: Migration = object : Migration(4, 5) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `verification_queue_cache` " +
                "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`cacheKey`))",
        )
    }
}

/** v5 -> v6: enriches RFID scan captures with backend identifiers from the scan roster.
 *  Existing rows keep their RFID tag and are still valid; new rows carry goat/obligation ids
 *  so final Submit can materialize completion items by goat id rather than treating an RFID
 *  string as a UUID. */
val MIGRATION_5_6: Migration = object : Migration(5, 6) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL("ALTER TABLE `scanned_goat_capture` ADD COLUMN `goatId` TEXT")
        db.execSQL("ALTER TABLE `scanned_goat_capture` ADD COLUMN `obligationId` TEXT")
    }
}

/** v6 -> v7: adds append-only RFID scan attempts. These rows audit every physical reader hit
 *  (accepted, duplicate alias, not-due, unknown) while `scanned_goat_capture` remains the
 *  de-duplicated completion/Submit source. Purely additive. */
val MIGRATION_6_7: Migration = object : Migration(6, 7) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `rfid_scan_attempt` " +
                "(`id` TEXT NOT NULL, `taskId` TEXT NOT NULL, `fieldKey` TEXT NOT NULL, " +
                "`tag` TEXT NOT NULL, `normalizedTag` TEXT NOT NULL, `goatId` TEXT, " +
                "`obligationId` TEXT, `outcome` TEXT NOT NULL, `tagRole` TEXT NOT NULL, " +
                "`reason` TEXT, `capturedAtMs` INTEGER NOT NULL, `syncStatus` TEXT NOT NULL, " +
                "`idempotencyKey` TEXT NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE UNIQUE INDEX IF NOT EXISTS `index_rfid_scan_attempt_idempotencyKey` " +
                "ON `rfid_scan_attempt` (`idempotencyKey`)",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_rfid_scan_attempt_taskId_capturedAtMs` " +
                "ON `rfid_scan_attempt` (`taskId`, `capturedAtMs`)",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_rfid_scan_attempt_taskId_goatId_capturedAtMs` " +
                "ON `rfid_scan_attempt` (`taskId`, `goatId`, `capturedAtMs`)",
        )
    }
}

/** v7 -> v8: normalized Room rows plus one opaque backend keyset cursor per
 * monthly Calendar filter scope. Purely additive and independently bounded. */
val MIGRATION_7_8: Migration = object : Migration(7, 8) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `calendar_schedule_items` " +
                "(`queryKey` TEXT NOT NULL, `eventId` TEXT NOT NULL, `dueAt` TEXT NOT NULL, " +
                "`dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`queryKey`, `eventId`))",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_calendar_schedule_items_queryKey_dueAt_eventId` " +
                "ON `calendar_schedule_items` (`queryKey`, `dueAt`, `eventId`)",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `calendar_schedule_remote_keys` " +
                "(`queryKey` TEXT NOT NULL, `nextCursor` TEXT, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`queryKey`))",
        )
    }
}

/** v8 -> v9: adds scan_roster_row entity for R50-007 (full-roster tag lookup via bounded
 *  indexed Room query instead of page-scoped state.value collection). Individual row entities
 *  replace accumulated JSON blobs per offline-first SSOT pattern. Purely additive. */
val MIGRATION_8_9: Migration = object : Migration(8, 9) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `scan_roster_row` " +
                "(`id` TEXT NOT NULL, `shedId` TEXT NOT NULL, `goatId` TEXT NOT NULL, " +
                "`primaryTag` TEXT NOT NULL, `secondaryTag` TEXT, `vaccineLabel` TEXT NOT NULL, " +
                "`status` TEXT NOT NULL, `obligationId` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`id`))",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_scan_roster_row_shedId` " +
                "ON `scan_roster_row` (`shedId`)",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_scan_roster_row_shedId_primaryTag` " +
                "ON `scan_roster_row` (`shedId`, `primaryTag`)",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_scan_roster_row_shedId_secondaryTag` " +
                "ON `scan_roster_row` (`shedId`, `secondaryTag`)",
        )
    }
}

/** v9 -> v10: row-level vaccination proof. Existing captures stay readable as unbound legacy
 * clips; all new vaccination captures persist a goat subject before their outbox write. */
val MIGRATION_9_10: Migration = object : Migration(9, 10) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL("ALTER TABLE `proof_capture` ADD COLUMN `subjectId` TEXT")
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_proof_capture_taskId_subjectId_capturedAtMs` " +
                "ON `proof_capture` (`taskId`, `subjectId`, `capturedAtMs`)",
        )
    }
}
