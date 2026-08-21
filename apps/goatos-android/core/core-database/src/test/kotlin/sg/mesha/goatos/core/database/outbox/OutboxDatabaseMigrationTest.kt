package sg.mesha.goatos.core.database.outbox

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
 * Schema-validated migration coverage for [OutboxDatabase]. The outbox holds not-yet-synced operator
 * writes, so a silently-wrong migration loses submissions — this is the highest-risk DB to leave
 * untested, and until now it had ZERO migration tests.
 *
 * Two checks, both under a plain `testDebugUnitTest` (Robolectric, @Config sdk 34):
 *
 *  1. [current schema is loadable via MigrationTestHelper] — proves `exportSchema = true`, the
 *     committed golden schema JSON, and MigrationTestHelper asset wiring work end to end, so the
 *     next schema bump (v4+) inherits golden-schema `runMigrationsAndValidate` coverage for free.
 *     Room only exports the version current when exportSchema is enabled, so historical v1/v2 JSON
 *     does not exist and cannot validate the shipped chain — check 2 covers that.
 *
 *  2. [every migration produces the entity-matching schema] — builds the real v1 outbox schema with
 *     raw SQL (no status/nextAttemptAt index, no requestFingerprint column), runs the actual
 *     OUTBOX_MIGRATION_1_2 / 2_3 objects, and asserts the migrated schema is structurally identical
 *     to a fresh Room-created v3 database (the @Entity truth) — index name convention and the added
 *     column included.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class OutboxDatabaseMigrationTest {

    @get:Rule
    val helper: MigrationTestHelper = MigrationTestHelper(
        InstrumentationRegistry.getInstrumentation(),
        OutboxDatabase::class.java,
    )

    @Test
    fun `current schema is loadable via MigrationTestHelper golden JSON`() {
        helper.createDatabase(DB_NAME, CURRENT_VERSION).close()
    }

    @Test
    fun `every migration produces the entity-matching schema`() {
        val migrated = buildV1ThenMigrate().schemaSnapshot()
        val fresh = freshCurrentSchema()
        assertEquals(
            "migrated v1->v$CURRENT_VERSION outbox schema must equal a fresh Room-created v$CURRENT_VERSION schema",
            fresh,
            migrated,
        )
    }

    /** The real v1 outbox schema (pre-index, pre-fingerprint), then the actual migrations in order. */
    private fun buildV1ThenMigrate(): SupportSQLiteDatabase {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val configuration = SupportSQLiteOpenHelper.Configuration.builder(context)
            .name(null) // in-memory
            .callback(object : SupportSQLiteOpenHelper.Callback(1) {
                override fun onCreate(db: SupportSQLiteDatabase) {
                    db.execSQL(
                        "CREATE TABLE IF NOT EXISTS `outbox` " +
                            "(`id` TEXT NOT NULL, `opType` TEXT NOT NULL, `groupKey` TEXT NOT NULL, " +
                            "`idempotencyKey` TEXT NOT NULL, `payloadJson` TEXT NOT NULL, `status` TEXT NOT NULL, " +
                            "`attemptCount` INTEGER NOT NULL, `maxAttempts` INTEGER NOT NULL, " +
                            "`conflict` INTEGER NOT NULL, `createdAt` INTEGER NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                            "`nextAttemptAt` INTEGER NOT NULL, `lastError` TEXT, `resultJson` TEXT, " +
                            "PRIMARY KEY(`id`))",
                    )
                    db.execSQL(
                        "CREATE UNIQUE INDEX IF NOT EXISTS `index_outbox_idempotencyKey` " +
                            "ON `outbox` (`idempotencyKey`)",
                    )
                }

                override fun onUpgrade(db: SupportSQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit
            })
            .build()
        val db = FrameworkSQLiteOpenHelperFactory().create(configuration).writableDatabase
        OUTBOX_MIGRATION_1_2.migrate(db)
        OUTBOX_MIGRATION_2_3.migrate(db)
        return db
    }

    private fun freshCurrentSchema(): Map<String, String> {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val db = Room.inMemoryDatabaseBuilder(context, OutboxDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        return try {
            db.openHelper.writableDatabase.schemaSnapshot()
        } finally {
            db.close()
        }
    }

    private companion object {
        const val DB_NAME = "outbox-migration-test.db"
        const val CURRENT_VERSION = 4
    }
}

/**
 * Structural fingerprint of every user table: sorted column defs (name/type/nullability/pk) and
 * sorted explicitly-created indices, ignoring Room's bookkeeping tables. Order-insensitive, mirroring
 * how Room validates a schema.
 *
 * SQL column DEFAULT is deliberately NOT compared: `ALTER TABLE ADD COLUMN … NOT NULL DEFAULT ''`
 * (the requestFingerprint migration) carries a SQL default a fresh Room create does not, and Room's
 * own TableInfo equality ignores that — comparing it would false-fail on a migration Room accepts.
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
