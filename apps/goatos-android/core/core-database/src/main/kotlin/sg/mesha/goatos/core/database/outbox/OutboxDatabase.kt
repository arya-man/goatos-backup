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
@Database(entities = [OutboxEntity::class], version = 2, exportSchema = false)
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

/** Builds the outbox database. Callers (DI) supply the application context. */
fun buildOutboxDatabase(context: Context): OutboxDatabase =
    Room.databaseBuilder(context, OutboxDatabase::class.java, "goatos-outbox.db")
        .addMigrations(OUTBOX_MIGRATION_1_2)
        .build()
