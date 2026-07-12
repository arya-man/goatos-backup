package sg.mesha.goatos.core.database.outbox

import androidx.room.Dao
import androidx.room.Database
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.PrimaryKey
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Upgrade-crash E2E for [OutboxDatabase]: the highest-stakes case, because the outbox holds
 * not-yet-synced operator writes — a mishandled migration that crashed on open, or silently dropped
 * rows, would lose a field worker's submissions on app update.
 *
 * Simulates an already-installed APK: lay down a real v1 outbox file (no requestFingerprint column,
 * no status/nextAttemptAt index) via [OldOutboxDatabaseV1], seed a QUEUED pending write, then open
 * the SAME file with the current schema + the real OUTBOX_MIGRATION_1_2 / 2_3 chain — Room's actual
 * production migrate-and-open path. Asserts it does not crash AND the pending write survives intact,
 * with requestFingerprint defaulted to "" (as the migration's ADD COLUMN … DEFAULT '' specifies).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class OutboxDatabaseUpgradeCrashTest {

    private val context = ApplicationProvider.getApplicationContext<android.content.Context>()

    @Before
    fun clean() = context.deleteDatabase(DB_NAME).let {}

    @After
    fun cleanup() = context.deleteDatabase(DB_NAME).let {}

    @Test
    fun `installed v1 outbox upgrades without crashing and keeps pending writes`() = runTest {
        // 1. Old APK: real v1 outbox on disk with one pending (QUEUED) operator write.
        val oldDb = Room.databaseBuilder(context, OldOutboxDatabaseV1::class.java, DB_NAME).build()
        oldDb.dao().insert(
            OutboxV1Entity(
                id = PENDING_ID,
                opType = "SHED_SUBMIT",
                groupKey = "shed-42",
                idempotencyKey = "idem-1",
                payloadJson = "{\"shed\":42}",
                status = "QUEUED",
                attemptCount = 0,
                maxAttempts = 8,
                conflict = false,
                createdAt = 1L,
                updatedAt = 1L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        oldDb.close()

        // 2. App update: open the same file with current schema + real migrations. Crashes here if
        //    a migration is wrong.
        val upgraded = Room.databaseBuilder(context, OutboxDatabase::class.java, DB_NAME)
            .addMigrations(OUTBOX_MIGRATION_1_2, OUTBOX_MIGRATION_2_3)
            .build()
        try {
            upgraded.openHelper.writableDatabase // force open + migrate + validate

            // 3. The pending write survived, unchanged, with the new column defaulted.
            val kept = upgraded.outboxDao().findById(PENDING_ID)
            assertNotNull("pending outbox write must survive the upgrade", kept)
            assertEquals("QUEUED", kept?.status)
            assertEquals("idem-1", kept?.idempotencyKey)
            assertEquals("{\"shed\":42}", kept?.payloadJson)
            assertEquals("legacy rows get the migration's DEFAULT ''", "", kept?.requestFingerprint)
        } finally {
            upgraded.close()
        }
    }

    private companion object {
        const val DB_NAME = "upgrade-crash-outbox.db"
        const val PENDING_ID = "op-1"
    }
}

/**
 * The app's original v1 outbox row shape: the current columns minus `requestFingerprint` (added by
 * OUTBOX_MIGRATION_2_3) and with only the idempotencyKey unique index (the status/nextAttemptAt
 * index is added by OUTBOX_MIGRATION_1_2). Test scope only — used purely to write an authentic
 * old-version file for the upgrade-crash test.
 */
@Entity(
    tableName = "outbox",
    indices = [Index(value = ["idempotencyKey"], unique = true)],
)
data class OutboxV1Entity(
    @PrimaryKey val id: String,
    val opType: String,
    val groupKey: String,
    val idempotencyKey: String,
    val payloadJson: String,
    val status: String,
    val attemptCount: Int,
    val maxAttempts: Int,
    val conflict: Boolean,
    val createdAt: Long,
    val updatedAt: Long,
    val nextAttemptAt: Long,
    val lastError: String?,
    val resultJson: String?,
)

@Dao
interface OutboxV1Dao {
    @Insert
    suspend fun insert(entity: OutboxV1Entity)
}

@Database(entities = [OutboxV1Entity::class], version = 1, exportSchema = false)
abstract class OldOutboxDatabaseV1 : RoomDatabase() {
    abstract fun dao(): OutboxV1Dao
}
