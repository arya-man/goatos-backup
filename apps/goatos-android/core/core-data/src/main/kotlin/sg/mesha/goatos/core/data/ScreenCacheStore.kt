package sg.mesha.goatos.core.data

import kotlinx.coroutines.withContext
import sg.mesha.goatos.core.common.DefaultDispatchers
import sg.mesha.goatos.core.common.DispatcherProvider

/**
 * Wipes every screen-facing Room cache table backed by [GoatDatabase] — bootstrap plus
 * Calendar / Control Tower / Execution (rows, shed, scan-roster) / Adherence / Insights
 * (gaps, coverage). A `fun interface` so callers (namely [LogoutCoordinator]) stay unit
 * -testable with a plain fake, without pulling Robolectric into every consuming module; the
 * real Room wipe is proven against a real (Robolectric in-memory) [GoatDatabase] separately
 * (see `GoatDatabaseCacheTest`).
 */
fun interface ScreenCacheStore {
    suspend fun clearAll()
}

/**
 * Production wiring: [androidx.room.RoomDatabase.clearAllTables] deletes every row in every
 * table registered to the database in one shot (docs/decisions/android-offline-first.md) and
 * must not run on the main thread, hence the explicit dispatcher hop.
 */
class RoomScreenCacheStore(
    private val database: GoatDatabase,
    private val dispatchers: DispatcherProvider = DefaultDispatchers,
) : ScreenCacheStore {
    override suspend fun clearAll() {
        withContext(dispatchers.io) {
            database.clearAllTables()
        }
    }
}
