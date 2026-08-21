package sg.mesha.goatos.core.database.outbox

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.room.migration.Migration
import androidx.sqlite.db.SupportSQLiteDatabase

/** The local outbox database — separate from `core-data`'s `GoatDatabase` (bootstrap cache)
 *  by design: the outbox is a distinct, small, high-write-frequency schema and this keeps it
 *  independently testable/migratable without touching the bootstrap cache schema. */
// exportSchema=true: schemas/<db-fqcn>/<version>.json is the golden schema each
// OUTBOX_MIGRATION_* is validated against by MigrationTestHelper, and makes schema
// changes reviewable. The outbox holds not-yet-synced writes, so a silently-wrong
// migration here loses operator submissions — validated migrations are mandatory.
@Database(entities = [OutboxEntity::class], version = 4, exportSchema = true)
abstract class OutboxDatabase : RoomDatabase() {
    abstract fun outboxDao(): OutboxDao
}

/** v1 -> v2: adds the composite `(status, nextAttemptAt)` index backing the drain-eligibility
 *  query. NON-destructive on purpose — the outbox holds not-yet-synced writes, so a destructive
 *  fallback would silently drop pending operator submissions. The index name must match Room's
 *  generated convention (`index_<table>_<col1>_<col2>`) or open-time schema validation fails. */
val OUTBOX_MIGRATION_1_2: Migration = object : Migration(1, 2) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL("CREATE INDEX IF NOT EXISTS `index_outbox_status_nextAttemptAt` ON `outbox` (`status`, `nextAttemptAt`)")
    }
}

/** v2 -> v3: persists a request fingerprint so same-key/different-payload attempts are
 *  rejected instead of being mistaken for an exact idempotent replay. Existing rows get an
 *  empty legacy value; core-data falls back to op/group/payload comparison for those rows. */
val OUTBOX_MIGRATION_2_3: Migration = object : Migration(2, 3) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL("ALTER TABLE `outbox` ADD COLUMN `requestFingerprint` TEXT NOT NULL DEFAULT ''")
    }
}

/**
 * v3 -> v4: puts already-queued vaccination Submit rows into the same ordering lane as
 * their session's scans.
 *
 * Scans have always been grouped by `taskId|partition`, but Submit used to build its own
 * key from the shed id and a differently-normalised label. Those two lanes are not
 * ordered against each other, so a Submit could drain while one of its own animals' scans
 * was still failing, closing the shed one animal short.
 *
 * New rows are fixed at the call sites. This exists for the rows already sitting on a
 * phone when it upgrades: without it, a Submit queued by the old build keeps its old lane
 * and can still outrun a failed scan -- a live risk for any device that has the previous
 * APK, not a theoretical one.
 *
 * DATA-ONLY: no schema change. It touches only non-terminal SHED_SUBMIT rows, rewrites
 * nothing else, and is a no-op on a row already in the new form -- so re-running it cannot
 * corrupt anything. The task id is read from the row's own payload rather than guessed,
 * and a row whose payload cannot be read is LEFT ALONE: an unchanged row keeps today's
 * behaviour, whereas a wrongly-rewritten one would silently strand an operator's
 * submission.
 */
val OUTBOX_MIGRATION_3_4: Migration = object : Migration(3, 4) {
    override fun migrate(db: SupportSQLiteDatabase) {
        val pending = listOf("QUEUED", "IN_FLIGHT", "FAILED")
        val placeholders = pending.joinToString(",") { "?" }
        val rows = mutableListOf<Triple<String, String, String>>()
        db.query(
            "SELECT id, groupKey, payloadJson FROM outbox WHERE opType = 'SHED_SUBMIT' AND status IN ($placeholders)",
            pending.toTypedArray(),
        ).use { cursor ->
            while (cursor.moveToNext()) {
                rows += Triple(cursor.getString(0), cursor.getString(1), cursor.getString(2))
            }
        }
        for ((id, groupKey, payloadJson) in rows) {
            val taskId = TASK_ID_IN_PAYLOAD.find(payloadJson)?.groupValues?.get(1) ?: continue
            // The old key's partition segment is everything after the last separator.
            val partition = groupKey.substringAfterLast('|', "")
            val nextKey = "$taskId|${normalizeOutboxPartition(partition)}"
            if (nextKey == groupKey) continue
            // requestFingerprint is computed over (opType, groupKey, payload). Moving the row
            // to a new lane without clearing it leaves a fingerprint that no longer matches
            // what the app would compute, so the next idempotent re-enqueue or recovery of
            // this same pending Submit is read as a DIFFERENT request and rejected as a
            // conflict -- stranding a submission the operator already made.
            //
            // Cleared to '' rather than recomputed: that is the same legacy sentinel
            // OUTBOX_MIGRATION_2_3 leaves on pre-existing rows, and core-data already falls
            // back to comparing op/group/payload for it. Recomputing here would mean
            // restating the canonical fingerprint (SHA-256 over a NUL-joined envelope, with
            // op-specific payload canonicalisation) inside core-database, which cannot see
            // it -- a second place to drift, for no gain.
            db.execSQL(
                "UPDATE outbox SET groupKey = ?, requestFingerprint = '' WHERE id = ?",
                arrayOf(nextKey, id),
            )
        }
    }
}

private val TASK_ID_IN_PAYLOAD = Regex("\"task_id\"\\s*:\\s*\"([^\"]+)\"")

/**
 * The partition half of an outbox lane key.
 *
 * Deliberately identical to core-data's `executionPartitionKey`, which the running app
 * uses: "Part 2", "part 2" and "2" are one session and must produce one lane. It is
 * restated here because core-database cannot depend on core-data, and
 * `OutboxPartitionNormalisationTest` asserts the two never drift apart.
 */
internal fun normalizeOutboxPartition(raw: String?): String {
    val normalized = raw.orEmpty().trim().lowercase().replace(Regex("^part[\\s]+"), "")
    return normalized.ifBlank { "whole" }
}

/** Builds the outbox database. Callers (DI) supply the application context. */
fun buildOutboxDatabase(context: Context): OutboxDatabase =
    Room.databaseBuilder(context, OutboxDatabase::class.java, "goatos-outbox.db")
        .addMigrations(OUTBOX_MIGRATION_1_2, OUTBOX_MIGRATION_2_3, OUTBOX_MIGRATION_3_4)
        .build()
