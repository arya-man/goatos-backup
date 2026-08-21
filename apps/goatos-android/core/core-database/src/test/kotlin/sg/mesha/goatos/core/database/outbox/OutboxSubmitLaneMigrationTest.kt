package sg.mesha.goatos.core.database.outbox

import androidx.sqlite.db.SupportSQLiteDatabase
import androidx.sqlite.db.SupportSQLiteOpenHelper
import androidx.sqlite.db.framework.FrameworkSQLiteOpenHelperFactory
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * The upgrade case for the vaccination lane fix.
 *
 * Fixing the two call sites only helps rows enqueued afterwards. A phone that upgrades with
 * a Submit already queued keeps that row's OLD lane, where nothing orders it against the
 * session's scans -- so it can still drain past a failing scan and close the shed one animal
 * short. That is a live risk on any device carrying the previous build, which is what
 * OUTBOX_MIGRATION_3_4 exists to remove.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class OutboxSubmitLaneMigrationTest {

    private fun openV3(): SupportSQLiteDatabase {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        context.getDatabasePath(DB).delete()
        val config = SupportSQLiteOpenHelper.Configuration.builder(context)
            .name(DB)
            .callback(object : SupportSQLiteOpenHelper.Callback(3) {
                override fun onCreate(db: SupportSQLiteDatabase) {
                    db.execSQL(
                        "CREATE TABLE IF NOT EXISTS `outbox` (`id` TEXT NOT NULL, `opType` TEXT NOT NULL, " +
                            "`groupKey` TEXT NOT NULL, `idempotencyKey` TEXT NOT NULL, `payloadJson` TEXT NOT NULL, " +
                            "`status` TEXT NOT NULL, `attemptCount` INTEGER NOT NULL, `maxAttempts` INTEGER NOT NULL, " +
                            "`conflict` INTEGER NOT NULL, `createdAt` INTEGER NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                            "`nextAttemptAt` INTEGER NOT NULL, `lastError` TEXT, `resultJson` TEXT, " +
                            "`requestFingerprint` TEXT NOT NULL DEFAULT '', PRIMARY KEY(`id`))",
                    )
                }
                override fun onUpgrade(db: SupportSQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit
            })
            .build()
        return FrameworkSQLiteOpenHelperFactory().create(config).writableDatabase
    }

    private fun insert(db: SupportSQLiteDatabase, id: String, opType: String, groupKey: String, status: String, payload: String) {
        db.execSQL(
            "INSERT INTO outbox (id, opType, groupKey, idempotencyKey, payloadJson, status, attemptCount, " +
                "maxAttempts, conflict, createdAt, updatedAt, nextAttemptAt, requestFingerprint) " +
                "VALUES (?, ?, ?, ?, ?, ?, 0, 5, 0, 0, 0, 0, '')",
            arrayOf(id, opType, groupKey, "key-$id", payload, status),
        )
    }

    private fun groupKeyOf(db: SupportSQLiteDatabase, id: String): String =
        db.query("SELECT groupKey FROM outbox WHERE id = ?", arrayOf(id)).use {
            it.moveToFirst(); it.getString(0)
        }

    @Test
    fun `a Submit queued under the old lane is moved onto its session's lane`() {
        val db = openV3()
        // How the old build wrote it: shed id, and a partition label it lowercased but did
        // not strip -- so "Part 2" stayed "part 2" while the scans stored "2".
        insert(db, "submit-1", "SHED_SUBMIT", "shed-9|part 2", "QUEUED", """{"task_id":"task-77","request":{}}""")
        OUTBOX_MIGRATION_3_4.migrate(db)

        assertEquals("task-77|2", groupKeyOf(db, "submit-1"))
        db.close()
    }

    @Test
    fun `it leaves everything it should not touch alone`() {
        val db = openV3()
        insert(db, "already", "SHED_SUBMIT", "task-77|2", "QUEUED", """{"task_id":"task-77"}""")
        insert(db, "done", "SHED_SUBMIT", "shed-9|whole", "SUCCEEDED", """{"task_id":"task-77"}""")
        insert(db, "other", "COUNTS_BIRTH", "shed-9|whole", "QUEUED", """{"task_id":"task-77"}""")
        insert(db, "unreadable", "SHED_SUBMIT", "shed-9|whole", "QUEUED", "not json at all")

        OUTBOX_MIGRATION_3_4.migrate(db)

        assertEquals("a row already in the new form is untouched", "task-77|2", groupKeyOf(db, "already"))
        assertEquals("terminal rows are history, not pending work", "shed-9|whole", groupKeyOf(db, "done"))
        assertEquals("other modules' lanes are none of its business", "shed-9|whole", groupKeyOf(db, "other"))
        assertEquals("an unreadable payload is left alone, never guessed", "shed-9|whole", groupKeyOf(db, "unreadable"))
        db.close()
    }

    @Test
    fun `running it twice changes nothing the second time`() {
        val db = openV3()
        insert(db, "submit-1", "SHED_SUBMIT", "shed-9|Part 3", "QUEUED", """{"task_id":"task-77"}""")
        OUTBOX_MIGRATION_3_4.migrate(db)
        val once = groupKeyOf(db, "submit-1")
        OUTBOX_MIGRATION_3_4.migrate(db)
        assertEquals(once, groupKeyOf(db, "submit-1"))
        assertEquals("task-77|3", once)
        db.close()
    }

    private companion object {
        const val DB = "outbox-lane-migration-test.db"
    }
}
