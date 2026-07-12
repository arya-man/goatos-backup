package sg.mesha.goatos.core.data

import androidx.room.Database
import androidx.room.RoomDatabase
import sg.mesha.goatos.core.data.cache.AdherenceCacheDao
import sg.mesha.goatos.core.data.cache.AdherenceCacheEntity
import sg.mesha.goatos.core.data.cache.CalendarCacheDao
import sg.mesha.goatos.core.data.cache.CalendarCacheEntity
import sg.mesha.goatos.core.data.cache.ControlTowerCacheDao
import sg.mesha.goatos.core.data.cache.ControlTowerCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheDao
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheDao
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheEntity
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheDao
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheDao
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheEntity
import sg.mesha.goatos.core.data.cache.ScanRosterCacheDao
import sg.mesha.goatos.core.data.cache.ScanRosterCacheEntity

/**
 * The on-device SSOT database (docs/decisions/android-offline-first.md). v1 held only the
 * bootstrap cache; v2 (see [MIGRATION_1_2]) adds one JSON-blob-by-scope cache table per
 * screen-facing read model — Calendar, Control Tower, Execution (rows/shed/scan-roster),
 * Adherence, and Insights (gaps/coverage) — so every read screen observes Room instead of a
 * one-shot network call.
 */
@Database(
    entities = [
        BootstrapCacheEntity::class,
        CalendarCacheEntity::class,
        ControlTowerCacheEntity::class,
        ExecutionRowsCacheEntity::class,
        ExecutionShedCacheEntity::class,
        ScanRosterCacheEntity::class,
        AdherenceCacheEntity::class,
        InsightsGapsCacheEntity::class,
        InsightsCoverageCacheEntity::class,
        RosterTimetableCacheEntity::class,
        RosterCoverageCacheEntity::class,
    ],
    version = 2,
    exportSchema = false,
)
abstract class GoatDatabase : RoomDatabase() {
    abstract fun bootstrapCacheDao(): BootstrapCacheDao
    abstract fun calendarCacheDao(): CalendarCacheDao
    abstract fun controlTowerCacheDao(): ControlTowerCacheDao
    abstract fun executionRowsCacheDao(): ExecutionRowsCacheDao
    abstract fun executionShedCacheDao(): ExecutionShedCacheDao
    abstract fun scanRosterCacheDao(): ScanRosterCacheDao
    abstract fun adherenceCacheDao(): AdherenceCacheDao
    abstract fun insightsGapsCacheDao(): InsightsGapsCacheDao
    abstract fun insightsCoverageCacheDao(): InsightsCoverageCacheDao
    abstract fun rosterTimetableCacheDao(): RosterTimetableCacheDao
    abstract fun rosterCoverageCacheDao(): RosterCoverageCacheDao
}
