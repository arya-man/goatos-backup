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
        // Creates the DB from the committed schemas/<db>/4.json and writes Room's identity hash.
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
        const val CURRENT_VERSION = 11
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
