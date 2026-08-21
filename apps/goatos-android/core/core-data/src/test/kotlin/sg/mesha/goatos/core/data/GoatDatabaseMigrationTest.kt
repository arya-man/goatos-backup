package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.room.testing.MigrationTestHelper
import androidx.sqlite.db.SupportSQLiteDatabase
import androidx.sqlite.db.SupportSQLiteOpenHelper
import androidx.sqlite.db.framework.FrameworkSQLiteOpenHelperFactory
import androidx.test.core.app.ApplicationProvider
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Schema-validated migration coverage for [GoatDatabase] (docs/decisions/android-offline-first.md).
 *
 * Two complementary checks, both under a plain `testDebugUnitTest` (Robolectric, @Config sdk 34) —
 * no device/emulator, thanks to `isIncludeAndroidResources = true` + the schemas dir on the test
 * sourceSet assets (build.gradle.kts):
 *
 *  1. [current schema is loadable via MigrationTestHelper] — proves `exportSchema = true`, the
 *     committed golden schema JSON, and the MigrationTestHelper asset wiring all work end to end.
 *     This is the guard the NEXT schema bump (v5+) inherits for free: its migration gets
 *     `createDatabase(old)` + `runMigrationsAndValidate(new, …)` golden-schema validation. Room only
 *     exports the schema of the version that is current when `exportSchema` is enabled, so the
 *     historical v1–v3 JSON does not exist and cannot validate the already-shipped chain — check 2
 *     covers that.
 *
 *  2. [every migration produces the entity-matching schema] — builds the real historical v1 schema
 *     with raw SQL, runs the actual MIGRATION_1_2 / 2_3 / 3_4 objects, and asserts the migrated
 *     schema is structurally identical to a fresh Room-created v4 database (the @Entity truth). This
 *     catches the failure the table-existence check in [GoatDatabaseCacheTest] cannot: a migration
 *     that creates a table/column/index that does not match what Room expects, which otherwise
 *     surfaces only as `IllegalStateException: Migration didn't properly handle` on a real device.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class GoatDatabaseMigrationTest {

    @get:Rule
    val helper: MigrationTestHelper = MigrationTestHelper(
        InstrumentationRegistry.getInstrumentation(),
        GoatDatabase::class.java,
    )

    @Test
    fun `current schema is loadable via MigrationTestHelper golden JSON`() {
        // Creates the DB from the committed schemas/<db>/16.json and writes Room's identity hash.
        // Fails loudly if exportSchema/asset wiring regresses, so the future-migration guard stays live.
        helper.createDatabase(DB_NAME, CURRENT_VERSION).close()
    }

    @Test
    fun `every migration produces the entity-matching schema`() {
        val migrated = buildV1ThenMigrate().use { it.schemaSnapshot() }
        val fresh = freshCurrentSchema()
        assertEquals(
            "migrated v1->v$CURRENT_VERSION schema must equal a fresh Room-created v$CURRENT_VERSION schema",
            fresh,
            migrated,
        )
    }

    @Test
    fun `migration 35 to 36 backfills weighing planner partition key from shed json`() {
        helper.createDatabase(DB_NAME, 35).apply {
            execSQL(
                "INSERT INTO `weighing_planner_shed_row` " +
                    "(`queryKey`, `locationId`, `parkId`, `parkName`, `sortIndex`, `shedJson`, `existingCampaignJson`, `updatedAt`) " +
                    "VALUES ('2026-08-10|park-1', 'castro-parent', 'park-1', 'CPT', 0, " +
                    "'{\"location_id\":\"castro-parent\",\"partition_label\":\"Part 3\"}', NULL, 1)",
            )
            close()
        }

        val db = helper.runMigrationsAndValidate(DB_NAME, 36, true, MIGRATION_35_36)
        db.query("PRAGMA table_info(`weighing_planner_shed_row`)").use { cursor ->
            val primaryKeyColumns = mutableListOf<String>()
            while (cursor.moveToNext()) {
                if (cursor.getInt(5) > 0) {
                    primaryKeyColumns += cursor.getString(1)
                }
            }
            assertEquals(
                listOf("queryKey", "locationId", "partitionKey"),
                primaryKeyColumns,
            )
        }
        db.query("SELECT `partitionKey`, `shedJson` FROM `weighing_planner_shed_row` WHERE `queryKey`='2026-08-10|park-1' AND `locationId`='castro-parent'").use { cursor ->
            assertEquals(true, cursor.moveToFirst())
            assertEquals("part 3", cursor.getString(0))
            assertEquals(
                "{\"location_id\":\"castro-parent\",\"partition_label\":\"Part 3\"}",
                cursor.getString(1),
            )
        }
        db.close()
    }

    @Test
    fun `migration 36 to 37 keeps legacy capture evidence fail closed in whole scope`() {
        helper.createDatabase(DB_NAME, 36).apply {
            execSQL(
                "INSERT INTO `scanned_goat_capture` " +
                    "(`id`, `taskId`, `fieldKey`, `tag`, `goatId`, `obligationId`, `capturedAtMs`, `syncStatus`) " +
                    "VALUES ('scan-1', 'task-1', '__scan_roster__', 'TAG-1', 'goat-1', 'obl-1', 1, 'PENDING')",
            )
            execSQL(
                "INSERT INTO `proof_capture` " +
                    "(`id`, `taskId`, `fieldKey`, `proofSubject`, `subjectId`, `localUri`, `mimeType`, `caption`, " +
                    "`capturedAtMs`, `capturedStartMs`, `capturedEndMs`, `capturedByPrincipalId`, `syncStatus`, " +
                    "`idempotencyKey`, `outboxItemId`, `serverProofId`, `lastError`, `captureSource`) " +
                    "VALUES ('proof-1', 'task-1', 'shed_video', 'shed', 'shed-1', 'file://proof.mp4', " +
                    "'video/mp4', NULL, 1, 1, 2, 'operator-1', 'PENDING', 'proof-key-1', NULL, NULL, NULL, 'in_app_camera')",
            )
            close()
        }

        val db = helper.runMigrationsAndValidate(DB_NAME, 37, true, MIGRATION_36_37)
        db.query("SELECT `partitionKey` FROM `scanned_goat_capture` WHERE `id`='scan-1'").use { cursor ->
            assertEquals(true, cursor.moveToFirst())
            assertEquals("whole", cursor.getString(0))
        }
        db.query("SELECT `partitionKey` FROM `proof_capture` WHERE `id`='proof-1'").use { cursor ->
            assertEquals(true, cursor.moveToFirst())
            assertEquals("whole", cursor.getString(0))
        }
        db.close()
    }

    @Test
    fun `migration 45 to 46 adds nullable supersedesRowId defaulting to null for existing rows`() {
        helper.createDatabase(DB_NAME, 45).apply {
            execSQL(
                "INSERT INTO `proof_capture` " +
                    "(`id`, `taskId`, `partitionKey`, `fieldKey`, `proofSubject`, `subjectId`, `localUri`, `mimeType`, `caption`, " +
                    "`capturedAtMs`, `capturedStartMs`, `capturedEndMs`, `capturedByPrincipalId`, `syncStatus`, " +
                    "`idempotencyKey`, `outboxItemId`, `serverProofId`, `lastError`, `captureSource`, " +
                    "`scopeType`, `scopeId`, `slotRequired`, `processingState`, `processingAttempted`, " +
                    "`stateAttempt`, `uploadOriginal`, `updatedAtMs`) " +
                    "VALUES ('proof-pre-46', 'task-1', 'whole', 'shed_video', 'shed', 'shed-1', 'file://proof.mp4', " +
                    "'video/mp4', NULL, 1, 1, 2, 'operator-1', 'PENDING', 'proof-key-pre-46', NULL, NULL, NULL, " +
                    "'in_app_camera', 'shed', 'shed-1', 0, 'CAPTURED_ORIGINAL', 0, 0, 0, 1)",
            )
            close()
        }

        val db = helper.runMigrationsAndValidate(DB_NAME, 46, true, MIGRATION_45_46)
        db.query("SELECT `supersedesRowId` FROM `proof_capture` WHERE `id`='proof-pre-46'").use { cursor ->
            assertEquals(true, cursor.moveToFirst())
            assertEquals(true, cursor.isNull(0))
        }
        db.close()
    }

    @Test
    fun `migration 47 to 48 creates the three feed wastage tables and preserves existing feed rows`() {
        helper.createDatabase(DB_NAME, 47).apply {
            // A pre-upgrade packing row proves the additive migration touches nothing existing.
            execSQL(
                "INSERT INTO `feed_packing_items` " +
                    "(`queryKey`, `grainKey`, `sortIndex`, `dtoJson`, `updatedAt`) " +
                    "VALUES ('scope-1', 'shed-1|2|experiment|1', 0, '{}', 1)",
            )
            close()
        }

        val db = helper.runMigrationsAndValidate(DB_NAME, 48, true, MIGRATION_47_48)
        db.query("SELECT COUNT(*) FROM `feed_wastage_meta_cache`").use { cursor ->
            assertEquals(true, cursor.moveToFirst())
            assertEquals(0, cursor.getInt(0))
        }
        db.query("SELECT COUNT(*) FROM `feed_wastage_items`").use { cursor ->
            assertEquals(true, cursor.moveToFirst())
            assertEquals(0, cursor.getInt(0))
        }
        db.query("SELECT COUNT(*) FROM `feed_wastage_remote_keys`").use { cursor ->
            assertEquals(true, cursor.moveToFirst())
            assertEquals(0, cursor.getInt(0))
        }
        db.query("SELECT `dtoJson` FROM `feed_packing_items` WHERE `queryKey`='scope-1'").use { cursor ->
            assertEquals(true, cursor.moveToFirst())
            assertEquals("{}", cursor.getString(0))
        }
        db.close()
    }

    @Test
    fun `migration 48 to 49 creates the four pc care tables and preserves existing feed rows`() {
        helper.createDatabase(DB_NAME, 48).apply {
            // A pre-upgrade wastage row proves the additive migration touches nothing existing.
            execSQL(
                "INSERT INTO `feed_wastage_items` " +
                    "(`queryKey`, `grainKey`, `sortIndex`, `dtoJson`, `updatedAt`) " +
                    "VALUES ('scope-1', 'shed-1|2|experiment', 0, '{}', 1)",
            )
            close()
        }

        val db = helper.runMigrationsAndValidate(DB_NAME, 49, true, MIGRATION_48_49)
        listOf(
            "pc_care_task_items",
            "pc_care_task_remote_keys",
            "pc_care_task_detail_cache",
            "pc_care_animal_rows",
        ).forEach { table ->
            db.query("SELECT COUNT(*) FROM `$table`").use { cursor ->
                assertEquals(true, cursor.moveToFirst())
                assertEquals(0, cursor.getInt(0))
            }
        }
        db.query("SELECT `dtoJson` FROM `feed_wastage_items` WHERE `queryKey`='scope-1'").use { cursor ->
            assertEquals(true, cursor.moveToFirst())
            assertEquals("{}", cursor.getString(0))
        }
        db.close()
    }

    /** The real v1 (bootstrap-cache-only) schema, then the actual migration objects applied in order. */
    private fun buildV1ThenMigrate(): SupportSQLiteDatabase {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val configuration = SupportSQLiteOpenHelper.Configuration.builder(context)
            .name(null) // in-memory
            .callback(object : SupportSQLiteOpenHelper.Callback(1) {
                override fun onCreate(db: SupportSQLiteDatabase) {
                    db.execSQL(
                        "CREATE TABLE IF NOT EXISTS `bootstrap_cache` " +
                            "(`id` INTEGER NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                            "PRIMARY KEY(`id`))",
                    )
                }

                override fun onUpgrade(db: SupportSQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit
            })
            .build()
        val db = FrameworkSQLiteOpenHelperFactory().create(configuration).writableDatabase
        MIGRATION_1_2.migrate(db)
        MIGRATION_2_3.migrate(db)
        MIGRATION_3_4.migrate(db)
        MIGRATION_4_5.migrate(db)
        MIGRATION_5_6.migrate(db)
        MIGRATION_6_7.migrate(db)
        MIGRATION_7_8.migrate(db)
        MIGRATION_8_9.migrate(db)
        MIGRATION_9_10.migrate(db)
        MIGRATION_10_11.migrate(db)
        MIGRATION_11_12.migrate(db)
        MIGRATION_12_13.migrate(db)
        MIGRATION_13_14.migrate(db)
        MIGRATION_14_15.migrate(db)
        MIGRATION_15_16.migrate(db)
        MIGRATION_16_17.migrate(db)
        MIGRATION_17_18.migrate(db)
        MIGRATION_18_19.migrate(db)
        MIGRATION_19_20.migrate(db)
        MIGRATION_20_21.migrate(db)
        MIGRATION_21_22.migrate(db)
        MIGRATION_22_23.migrate(db)
        MIGRATION_23_24.migrate(db)
        MIGRATION_24_25.migrate(db)
        MIGRATION_25_26.migrate(db)
        MIGRATION_26_27.migrate(db)
        MIGRATION_27_28.migrate(db)
        MIGRATION_28_29.migrate(db)
        MIGRATION_29_30.migrate(db)
        MIGRATION_30_31.migrate(db)
        MIGRATION_31_32.migrate(db)
        MIGRATION_32_33.migrate(db)
        MIGRATION_33_34.migrate(db)
        MIGRATION_34_35.migrate(db)
        MIGRATION_35_36.migrate(db)
        MIGRATION_36_37.migrate(db)
        MIGRATION_37_38.migrate(db)
        MIGRATION_38_39.migrate(db)
        MIGRATION_39_40.migrate(db)
        MIGRATION_40_41.migrate(db)
        MIGRATION_41_42.migrate(db)
        MIGRATION_42_43.migrate(db)
        MIGRATION_43_44.migrate(db)
        MIGRATION_44_45.migrate(db)
        MIGRATION_45_46.migrate(db)
        MIGRATION_46_47.migrate(db)
        MIGRATION_47_48.migrate(db)
        MIGRATION_48_49.migrate(db)
        return db
    }

    /** A fresh Room-created database is the @Entity source of truth for the current schema. */
    private fun freshCurrentSchema(): Map<String, String> {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val db = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        return try {
            db.openHelper.writableDatabase.schemaSnapshot()
        } finally {
            db.close()
        }
    }

    private companion object {
        const val DB_NAME = "goat-migration-test.db"
        const val CURRENT_VERSION = 49
    }
}

/**
 * A structural fingerprint of every user table: sorted column defs (name/type/nullability/pk) and
 * sorted explicitly-created indices (name/uniqueness/columns). Order-insensitive to mirror how Room
 * validates a schema, and it ignores Room's bookkeeping tables (room_master_table, android_metadata).
 *
 * SQL column DEFAULT is deliberately NOT compared: an `ALTER TABLE ADD COLUMN … NOT NULL DEFAULT x`
 * migration legitimately carries a SQL default that a fresh Room create (Kotlin-side property default)
 * does not, and Room's own TableInfo equality ignores that difference — comparing it here would
 * false-fail on migrations Room accepts. Two databases with equal snapshots are interchangeable to Room.
 */
internal fun SupportSQLiteDatabase.schemaSnapshot(): Map<String, String> {
    val tables = buildList {
        query(
            "SELECT name FROM sqlite_master WHERE type='table' " +
                "AND name NOT IN ('room_master_table','android_metadata','sqlite_sequence') ORDER BY name",
        ).use { c -> while (c.moveToNext()) add(c.getString(0)) }
    }
    val out = linkedMapOf<String, String>()
    for (t in tables) {
        val cols = buildList {
            query("PRAGMA table_info(`$t`)").use { c ->
                while (c.moveToNext()) {
                    val name = c.getString(1)
                    val type = c.getString(2)
                    val notnull = c.getInt(3)
                    val pk = c.getInt(5)
                    add("$name:$type notnull=$notnull pk=$pk")
                }
            }
        }.sorted()
        val indexNames = buildList {
            query("PRAGMA index_list(`$t`)").use { c ->
                while (c.moveToNext()) {
                    // seq, name, unique, origin, partial — origin 'c' = explicitly CREATE'd index
                    // (excludes the pk/unique autoindexes, which table_info already captures).
                    if (c.getString(3) == "c") add(c.getString(1) to c.getInt(2))
                }
            }
        }
        val idx = indexNames.map { (iname, unique) ->
            val icols = buildList {
                query("PRAGMA index_info(`$iname`)").use { ic ->
                    while (ic.moveToNext()) add(ic.getString(2))
                }
            }
            "$iname unique=$unique (${icols.joinToString(",")})"
        }.sorted()
        out[t] = "cols=[${cols.joinToString("; ")}] idx=[${idx.joinToString("; ")}]"
    }
    return out
}
