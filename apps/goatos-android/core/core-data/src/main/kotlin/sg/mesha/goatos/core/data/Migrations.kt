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

/** v10 -> v11: complete RFID roster rows are task-scoped and store the exact canonical tags used
 * by the reader. Shed-only v10 rows are discarded because their task ownership cannot be inferred. */
val MIGRATION_10_11: Migration = object : Migration(10, 11) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL("DROP TABLE IF EXISTS `scan_roster_row`")
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `scan_roster_row` " +
                "(`id` TEXT NOT NULL, `scopeKey` TEXT NOT NULL, `shedId` TEXT NOT NULL, `taskId` TEXT NOT NULL, " +
                "`goatId` TEXT NOT NULL, `primaryTag` TEXT NOT NULL, `secondaryTag` TEXT, " +
                "`normalizedPrimaryTag` TEXT NOT NULL, `normalizedSecondaryTag` TEXT, " +
                "`vaccineLabel` TEXT NOT NULL, `status` TEXT NOT NULL, `obligationId` TEXT NOT NULL, " +
                "`updatedAt` INTEGER NOT NULL, PRIMARY KEY(`id`))",
        )
        db.execSQL("CREATE INDEX IF NOT EXISTS `index_scan_roster_row_scopeKey` ON `scan_roster_row` (`scopeKey`)")
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_scan_roster_row_scopeKey_normalizedPrimaryTag` " +
                "ON `scan_roster_row` (`scopeKey`, `normalizedPrimaryTag`)",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_scan_roster_row_scopeKey_normalizedSecondaryTag` " +
                "ON `scan_roster_row` (`scopeKey`, `normalizedSecondaryTag`)",
        )
    }
}

/** v11 -> v12: adds the shed-completion-summary cache (JSON-blob-by-task offline-first read for
 * the vaccination shed acknowledgement Submit screen). Additive, non-destructive. */
val MIGRATION_11_12: Migration = object : Migration(11, 12) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `shed_completion_summary_cache` " +
                "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`cacheKey`))",
        )
    }
}

/** v12 -> v13: persist `capture_source` on each proof row. The startup-recovery re-registration
 * path (ProofCaptureRepository's `reconcileRecoverableUploadsNow`) runs with no in-memory
 * `ProofPolicy`, so it previously re-sent the hardcoded default `capture_source`, silently
 * rewriting the metadata of any non-camera source. Persisting the value with the durable row makes
 * the row the single source of truth and keeps recovery faithful. Additive; existing rows backfill
 * the historical `in_app_camera` default (`NOT NULL DEFAULT`). */
val MIGRATION_12_13: Migration = object : Migration(12, 13) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "ALTER TABLE `proof_capture` ADD COLUMN `captureSource` TEXT NOT NULL DEFAULT 'in_app_camera'",
        )
    }
}

/** v13 -> v14: the per-row `scan_roster_row` SSOT becomes the sole source for the shed scan screen.
 *
 *  1. Adds `seq` (backend roster order captured at refresh) so the scan LIST can render a bounded
 *     keyset window (`ORDER BY seq LIMIT n`) advanced by scroll, instead of the app deserializing a
 *     whole-collection JSON blob to draw the list. Existing rows backfill `0` (`NOT NULL DEFAULT 0`)
 *     and are re-seeded with real order on the next roster refresh; the tie-break on `id` keeps the
 *     window stable meanwhile. An index over `(scopeKey, seq)` serves the windowed read.
 *  2. DROPS the now-unused `scan_roster_cache` whole-roster blob table. Nothing reads it anymore —
 *     tag validation, counters, and the submit proof gate all resolve the full roster from
 *     `scan_roster_row`. Dropping it is safe: it only ever held a re-fetchable cache of a network
 *     read, never unsynced operator writes (those live in the outbox / proof_capture, untouched).
 */
val MIGRATION_13_14: Migration = object : Migration(13, 14) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL("ALTER TABLE `scan_roster_row` ADD COLUMN `seq` INTEGER NOT NULL DEFAULT 0")
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_scan_roster_row_scopeKey_seq` " +
                "ON `scan_roster_row` (`scopeKey`, `seq`)",
        )
        db.execSQL("DROP TABLE IF EXISTS `scan_roster_cache`")
    }
}

/**
 * v14 -> v15: adds the Counts vertical's read models in ONE step — all seven tables, no change to
 * any existing table. Purely additive, so an installed APK upgrades in place with every cached row
 * and every unsynced outbox write intact.
 *
 * Renumbered from the branch's original v12 -> v13 during the main merge: main's proof-capture
 * `capture_source` (v13) and scan-roster SSOT (v14) migrations are the integration baseline and
 * keep their numbers, so the Counts tables move to run last, at v15.
 *
 * Three shapes, deliberately different (see `cache/CountsCache.kt`,
 * `cache/CountsApprovalCache.kt`):
 *  - `herd_summary_cache` / `counts_breakdown_meta_cache` / `counts_shifting_destinations_cache`
 *    are JSON-blob-by-scope rollups, the same `(cacheKey, dtoJson, updatedAt)` shape every other
 *    fixed-size read model here uses. The destinations catalog is the bounded park -> sheds
 *    vocabulary behind the shifting screen's cascading dropdowns, so the picker still opens with
 *    real options when the phone is offline in a shed;
 *  - `counts_breakdown_items` / `counts_breakdown_remote_keys` are normalized per-grain rows plus
 *    their page offset, mirroring [MIGRATION_7_8]'s Calendar schedule pair, so the UI observes a
 *    bounded Paging window instead of an ever-growing cached page blob;
 *  - `counts_approval_items` / `counts_approval_remote_keys` are the approver queue's normalized
 *    per-request rows plus one opaque backend keyset cursor per scope — a growable list, so it is
 *    paginated rather than blob-cached.
 *
 * NOTE the Counts OUTBOX op types (COUNTS_SHIFTING / COUNTS_BIRTH / COUNTS_DEATH,
 * COUNTS_APPROVAL_APPROVE / COUNTS_APPROVAL_REJECT) need NO migration of their own and are not
 * touched here: they live in a different database (`goatos-outbox.db`) and are stored as plain
 * TEXT in an existing column.
 */
val MIGRATION_14_15: Migration = object : Migration(14, 15) {
    override fun migrate(db: SupportSQLiteDatabase) {
        // Each CREATE spells its table name out as a literal rather than looping over an
        // interpolated list. That is deliberate: `make room-migration-guard` statically matches
        // every new v15 @Entity table against the CREATEs in this migration, and an interpolated
        // `$table` name is invisible to it — so a loop here would let a genuinely missing table
        // pass the very check that exists to catch the upgrade-crash defect
        // (docs/decisions/room-migration-safety.md).
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `herd_summary_cache` " +
                "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`cacheKey`))",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `counts_breakdown_meta_cache` " +
                "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`cacheKey`))",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `counts_breakdown_items` " +
                "(`queryKey` TEXT NOT NULL, `grainKey` TEXT NOT NULL, `sortIndex` INTEGER NOT NULL, " +
                "`dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`queryKey`, `grainKey`))",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_counts_breakdown_items_queryKey_sortIndex` " +
                "ON `counts_breakdown_items` (`queryKey`, `sortIndex`)",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `counts_breakdown_remote_keys` " +
                "(`queryKey` TEXT NOT NULL, `nextOffset` INTEGER NOT NULL, `endReached` INTEGER NOT NULL, " +
                "`updatedAt` INTEGER NOT NULL, PRIMARY KEY(`queryKey`))",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `counts_approval_items` " +
                "(`queryKey` TEXT NOT NULL, `approvalRequestId` TEXT NOT NULL, " +
                "`sortIndex` INTEGER NOT NULL, `raisedAt` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, " +
                "`updatedAt` INTEGER NOT NULL, PRIMARY KEY(`queryKey`, `approvalRequestId`))",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_counts_approval_items_queryKey_sortIndex` " +
                "ON `counts_approval_items` (`queryKey`, `sortIndex`)",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `counts_approval_remote_keys` " +
                "(`queryKey` TEXT NOT NULL, `nextCursor` TEXT, `endReached` INTEGER NOT NULL, " +
                "`updatedAt` INTEGER NOT NULL, PRIMARY KEY(`queryKey`))",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `counts_shifting_destinations_cache` " +
                "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, " +
                "`updatedAt` INTEGER NOT NULL, PRIMARY KEY(`cacheKey`))",
        )
    }
}

/** v15 -> v16: cache backend scan timestamps on scan_roster_row.
 *
 * The server is the source of truth for RFID captures once they sync. The scan screen previously
 * showed exact scan times only from the phone-local scanned_goat_capture table, so a refreshed or
 * reinstalled operator device could reopen a shed and see 0/N even though the backend had accepted
 * the captures. This nullable column lets the roster refresh carry the server's `scannedAt`
 * timestamp into Room; the UI then renders DONE plus the exact IST timestamp from the row itself.
 */
val MIGRATION_15_16: Migration = object : Migration(15, 16) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL("ALTER TABLE `scan_roster_row` ADD COLUMN `scannedAtMs` INTEGER")
    }
}

/**
 * v16 -> v17: adds the six Feed read-model tables — the Feed Direction sheet and the Feed Packing
 * worklist, each as a summary-envelope blob + normalized paged rows + per-scope remote keys
 * (docs/decisions/android-offline-first.md). Purely additive; no existing table changes.
 *
 * As in [MIGRATION_14_15], each CREATE spells its table name out as a literal (never an
 * interpolated loop) so `make room-migration-guard` can statically match every new v17 @Entity
 * table against a CREATE here — an interpolated name would be invisible to the very check that
 * exists to catch the upgrade-crash defect (docs/decisions/room-migration-safety.md).
 */
val MIGRATION_16_17: Migration = object : Migration(16, 17) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `feed_direction_meta_cache` " +
                "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`cacheKey`))",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `feed_direction_items` " +
                "(`queryKey` TEXT NOT NULL, `grainKey` TEXT NOT NULL, `sortIndex` INTEGER NOT NULL, " +
                "`dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`queryKey`, `grainKey`))",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_feed_direction_items_queryKey_sortIndex` " +
                "ON `feed_direction_items` (`queryKey`, `sortIndex`)",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `feed_direction_remote_keys` " +
                "(`queryKey` TEXT NOT NULL, `nextOffset` INTEGER NOT NULL, `endReached` INTEGER NOT NULL, " +
                "`updatedAt` INTEGER NOT NULL, PRIMARY KEY(`queryKey`))",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `feed_packing_meta_cache` " +
                "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`cacheKey`))",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `feed_packing_items` " +
                "(`queryKey` TEXT NOT NULL, `grainKey` TEXT NOT NULL, `sortIndex` INTEGER NOT NULL, " +
                "`dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`queryKey`, `grainKey`))",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_feed_packing_items_queryKey_sortIndex` " +
                "ON `feed_packing_items` (`queryKey`, `sortIndex`)",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `feed_packing_remote_keys` " +
                "(`queryKey` TEXT NOT NULL, `nextOffset` INTEGER NOT NULL, `endReached` INTEGER NOT NULL, " +
                "`updatedAt` INTEGER NOT NULL, PRIMARY KEY(`queryKey`))",
        )
    }
}

/**
 * v17 -> v18: the shifting pending-execution queue pair — the Shifting "Pending" tab's offline-first
 * read model. One Room row per authorized movement ([ShiftingPendingItemEntity]) plus its opaque
 * keyset remote key ([ShiftingPendingRemoteKeyEntity]), shaped exactly like the Counts APPROVAL
 * queue. Additive and non-destructive: no existing table is touched, so an installed APK carrying an
 * unsynced write outbox upgrades in place without data loss.
 */
val MIGRATION_17_18: Migration = object : Migration(17, 18) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `shifting_pending_items` " +
                "(`queryKey` TEXT NOT NULL, `shiftingEventId` TEXT NOT NULL, `sortIndex` INTEGER NOT NULL, " +
                "`raisedAt` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`queryKey`, `shiftingEventId`))",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_shifting_pending_items_queryKey_sortIndex` " +
                "ON `shifting_pending_items` (`queryKey`, `sortIndex`)",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `shifting_pending_remote_keys` " +
                "(`queryKey` TEXT NOT NULL, `nextCursor` TEXT, `endReached` INTEGER NOT NULL, " +
                "`updatedAt` INTEGER NOT NULL, PRIMARY KEY(`queryKey`))",
        )
    }
}

/**
 * v19 -> v20: the four Birth/Death workflow read-model tables
 * (docs/decisions/birth-death-workflows.md): the keyset-paginated card list + its per-scope remote
 * keys (shaped exactly like the shifting Pending pair), the per-day chips rollup blob, and the
 * drill-in detail blob. Additive and non-destructive: no existing table is touched, so an installed
 * APK carrying an unsynced write outbox upgrades in place without data loss.
 *
 * As in [MIGRATION_14_15], each CREATE spells its table name out as a literal (never an
 * interpolated loop) so `make room-migration-guard` can statically match every new v20 @Entity
 * table against a CREATE here (docs/decisions/room-migration-safety.md).
 */
val MIGRATION_19_20: Migration = object : Migration(19, 20) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `workflow_cards` " +
                "(`queryKey` TEXT NOT NULL, `workflowId` TEXT NOT NULL, `sortIndex` INTEGER NOT NULL, " +
                "`dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`queryKey`, `workflowId`))",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_workflow_cards_queryKey_sortIndex` " +
                "ON `workflow_cards` (`queryKey`, `sortIndex`)",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `workflow_remote_keys` " +
                "(`queryKey` TEXT NOT NULL, `nextCursor` TEXT, `endReached` INTEGER NOT NULL, " +
                "`updatedAt` INTEGER NOT NULL, PRIMARY KEY(`queryKey`))",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `workflow_chips_cache` " +
                "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`cacheKey`))",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `workflow_detail_cache` " +
                "(`cacheKey` TEXT NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                "PRIMARY KEY(`cacheKey`))",
        )
    }
}

/**
 * v18 -> v19: the "Awaiting RFID" list pair — the Counts promote flow's offline-first read model. One
 * Room row per temporary-tagged goat ([AwaitingRfidItemEntity]) plus its keyset remote key
 * ([AwaitingRfidRemoteKeyEntity]). Additive and non-destructive: no existing table is touched, so an
 * installed APK carrying an unsynced write outbox upgrades in place without data loss.
 */
val MIGRATION_18_19: Migration = object : Migration(18, 19) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `awaiting_rfid_items` " +
                "(`goatId` TEXT NOT NULL, `sortIndex` INTEGER NOT NULL, `displayId` TEXT NOT NULL, " +
                "`dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, PRIMARY KEY(`goatId`))",
        )
        db.execSQL(
            "CREATE INDEX IF NOT EXISTS `index_awaiting_rfid_items_sortIndex` " +
                "ON `awaiting_rfid_items` (`sortIndex`)",
        )
        db.execSQL(
            "CREATE TABLE IF NOT EXISTS `awaiting_rfid_remote_keys` " +
                "(`id` TEXT NOT NULL, `nextCursor` TEXT, `endReached` INTEGER NOT NULL, " +
                "`updatedAt` INTEGER NOT NULL, PRIMARY KEY(`id`))",
        )
    }
}
