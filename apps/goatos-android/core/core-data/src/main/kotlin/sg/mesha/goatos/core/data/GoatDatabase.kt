package sg.mesha.goatos.core.data

import androidx.room.Database
import androidx.room.RoomDatabase
import sg.mesha.goatos.core.database.capture.ProofCaptureDao
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
import sg.mesha.goatos.core.database.capture.ScannedGoatDao
import sg.mesha.goatos.core.database.capture.ScannedGoatEntity
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
import sg.mesha.goatos.core.data.cache.TaskDetailCacheDao
import sg.mesha.goatos.core.data.cache.TaskDetailCacheEntity

/**
 * The on-device SSOT database (docs/decisions/android-offline-first.md). v1 held only the
 * bootstrap cache; v2 (see [MIGRATION_1_2]) adds one JSON-blob-by-scope cache table per
 * screen-facing read model — Calendar, Control Tower, Execution (rows/shed/scan-roster),
 * Adherence, and Insights (gaps/coverage) — so every read screen observes Room instead of a
 * one-shot network call. v3 (see [MIGRATION_2_3]) adds the task-detail cache (MOB-001) so the
 * Scan -> Submit operator task read is offline-first too, not a network-only pass-through.
 * v4 (see [MIGRATION_3_4]) adds the scanned-goat + proof-capture tables (MOB-002) — the
 * Room-first SSOT behind Submit's `goat_scan`/`video_proof` recording-form controls.
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
        TaskDetailCacheEntity::class,
        ScannedGoatEntity::class,
        ProofCaptureEntity::class,
    ],
    version = 4,
    // exportSchema=true writes schemas/<db-fqcn>/<version>.json (see build.gradle.kts
    // room.schemaLocation). The committed schema JSON is the golden schema
    // MigrationTestHelper validates each migration against, and it makes every schema
    // change reviewable as a diff. A version bump with no new schema JSON is a red flag.
    exportSchema = true,
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
    abstract fun taskDetailCacheDao(): TaskDetailCacheDao
    abstract fun scannedGoatDao(): ScannedGoatDao
    abstract fun proofCaptureDao(): ProofCaptureDao
}
