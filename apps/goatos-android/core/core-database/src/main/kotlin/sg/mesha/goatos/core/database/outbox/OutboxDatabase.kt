package sg.mesha.goatos.core.database.outbox

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase

/** The local outbox database — separate from `core-data`'s `GoatDatabase` (bootstrap cache)
 *  by design: the outbox is a distinct, small, high-write-frequency schema and this keeps it
 *  independently testable/migratable without touching the bootstrap cache schema. */
@Database(entities = [OutboxEntity::class], version = 1, exportSchema = false)
abstract class OutboxDatabase : RoomDatabase() {
    abstract fun outboxDao(): OutboxDao
}

/** Builds the outbox database. Callers (DI) supply the application context. */
fun buildOutboxDatabase(context: Context): OutboxDatabase =
    Room.databaseBuilder(context, OutboxDatabase::class.java, "goatos-outbox.db").build()
