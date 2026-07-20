package sg.mesha.goatos.core.data

import androidx.room.Dao
import androidx.room.Database
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
import sg.mesha.goatos.core.database.capture.RfidScanAttemptEntity
import sg.mesha.goatos.core.database.capture.ScannedGoatEntity
import sg.mesha.goatos.core.data.cache.AdherenceCacheEntity
import sg.mesha.goatos.core.data.cache.CalendarCacheEntity
import sg.mesha.goatos.core.data.cache.CalendarScheduleEntity
import sg.mesha.goatos.core.data.cache.CalendarScheduleRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.ControlTowerCacheEntity
import sg.mesha.goatos.core.data.cache.CountsApprovalItemEntity
import sg.mesha.goatos.core.data.cache.CountsApprovalRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.CountsBreakdownItemEntity
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheEntity
import sg.mesha.goatos.core.data.cache.CountsBreakdownRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.CountsShiftingDestinationsCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheEntity
import sg.mesha.goatos.core.data.cache.HerdSummaryCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheEntity
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheEntity
import sg.mesha.goatos.core.data.cache.ScanRosterCacheEntity
import sg.mesha.goatos.core.data.cache.ScanRosterRowDao
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.ShedCompletionSummaryCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.TaskDetailCacheEntity
import sg.mesha.goatos.core.data.cache.VerificationQueueCacheEntity

/**
 * Upgrade-crash E2E for [GoatDatabase]: simulates an already-installed APK whose on-device DB was
 * created by an OLD app version, then upgraded in place — the exact path that crashed before the
 * roster-cache migration fix. This is NOT a structural comparison (that is GoatDatabaseMigrationTest);
 * it drives Room's REAL production open path — detect old user_version, run the registered migrations,
 * validate the migrated schema against the @Entity identity, open — on a real on-disk file with real
 * seeded rows. A mishandled migration throws `IllegalStateException` here, failing the test, exactly
 * as it would crash a user's app on first launch after update.
 *
 * The old-version schema is produced by a real Room database ([OldGoatDatabaseV1]) so the file carries
 * Room's own room_master_table / identity metadata — precisely what a shipped old APK would have on
 * disk, and what a hand-rolled raw-SQL DB would lack.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class GoatDatabaseUpgradeCrashTest {

    private val context = ApplicationProvider.getApplicationContext<android.content.Context>()

    @Before
    fun clean() = context.deleteDatabase(DB_NAME).let {}

    @After
    fun cleanup() = context.deleteDatabase(DB_NAME).let {}

    @Test
    fun `installed v1 db upgrades to current without crashing and keeps its data`() = runTest {
        // 1. An old APK: create the real v1 (bootstrap-only) database on disk and seed a row, as a
        //    user who has been running the app offline would already have.
        val oldDb = Room.databaseBuilder(context, OldGoatDatabaseV1::class.java, DB_NAME).build()
        oldDb.bootstrapCacheDao().upsert(
            BootstrapCacheEntity(id = 0, dtoJson = SEEDED_BOOTSTRAP_JSON, updatedAt = SEEDED_AT),
        )
        oldDb.close() // closes the file at user_version = 1

        // 2. The app update: open the SAME file with the current schema + the real migration chain.
        //    If any migration is wrong (e.g. the missing roster-cache tables), Room throws here —
        //    the crash an updated APK would hit on first launch.
        val upgraded = Room.databaseBuilder(context, GoatDatabase::class.java, DB_NAME)
            .addMigrations(
                MIGRATION_1_2,
                MIGRATION_2_3,
                MIGRATION_3_4,
                MIGRATION_4_5,
                MIGRATION_5_6,
                MIGRATION_6_7,
                MIGRATION_7_8,
                MIGRATION_8_9,
                MIGRATION_9_10,
                MIGRATION_10_11,
                MIGRATION_11_12,
                MIGRATION_12_13,
            )
            .build()
        try {
            // Force Room to actually open + migrate + validate (builders are lazy).
            upgraded.openHelper.writableDatabase

            // 3. Existing data survived the migration.
            val kept = upgraded.bootstrapCacheDao().get()
            assertNotNull("bootstrap row must survive the upgrade", kept)
            assertEquals(SEEDED_BOOTSTRAP_JSON, kept?.dtoJson)
            assertEquals(SEEDED_AT, kept?.updatedAt)

            // 4. The table that was missing a migration is present and usable post-upgrade
            //    (regression lock for the roster-cache crash). A write+read round-trip proves the
            //    table exists with the expected shape — not just that open didn't throw.
            val dao = upgraded.rosterTimetableCacheDao()
            dao.upsert(RosterTimetableCacheEntity(cacheKey = "center-1", dtoJson = "{}", updatedAt = 7L))
            val row = dao.observe("center-1").first()
            assertEquals(7L, row?.updatedAt)

            // 5. The v11 scan_roster_row table (R50-007/task-scoped R50 rework) is present and
            //    usable post-upgrade — a write + indexed tag lookup + GROUP BY aggregate round-trip
            //    proves the table AND its indices came out of MIGRATION_10_11 with the shape Room
            //    expects.
            val rosterDao = upgraded.scanRosterRowDao()
            val scopeKey = cacheKey("shed-1", "task-1")
            rosterDao.upsert(
                ScanRosterRowEntity(
                    id = "shed-1#obl-1",
                    scopeKey = scopeKey,
                    shedId = "shed-1",
                    taskId = "task-1",
                    goatId = "goat-1",
                    primaryTag = "IN-1234",
                    secondaryTag = null,
                    normalizedPrimaryTag = "in1234",
                    normalizedSecondaryTag = null,
                    vaccineLabel = "ET+TT",
                    status = "pending",
                    obligationId = "obl-1",
                    updatedAt = 9L,
                ),
            )
            val byTag = rosterDao.findByTag(scopeKey, "in1234")
            assertEquals("obl-1", byTag?.obligationId)
            val counts = rosterDao.countByStatus(scopeKey)
            assertEquals(1, counts.single().count)
            assertEquals("pending", counts.single().status)

            // 6. The v12 shed_completion_summary_cache table (vaccination shed acknowledgement) is
            //    present and usable post-upgrade — a write + read round-trip proves MIGRATION_11_12
            //    created the table with the shape Room expects.
            val summaryDao = upgraded.shedCompletionSummaryCacheDao()
            summaryDao.upsert(
                ShedCompletionSummaryCacheEntity(cacheKey = "task-1", dtoJson = "{}", updatedAt = 12L),
            )
            assertEquals(12L, summaryDao.observe("task-1").first()?.updatedAt)

            // 7. Same proof for the seven v13 Counts tables (MIGRATION_12_13). Open-without-crash
            //    only proves Room ACCEPTED the migrated schema; a write+read round-trip proves each
            //    new table actually exists with the shape its @Entity declares. All three table
            //    shapes in that single migration are exercised: the JSON-blob rollups, the
            //    normalized per-grain rows + their page offset, and the keyset approval queue.
            val herdDao = upgraded.herdSummaryCacheDao()
            herdDao.upsert(HerdSummaryCacheEntity(cacheKey = "all", dtoJson = "{}", updatedAt = 11L))
            assertEquals(11L, herdDao.observe("all").first()?.updatedAt)

            val metaDao = upgraded.countsBreakdownMetaCacheDao()
            metaDao.upsert(CountsBreakdownMetaCacheEntity(cacheKey = "all", dtoJson = "{}", updatedAt = 12L))
            assertEquals(12L, metaDao.observe("all").first()?.updatedAt)

            upgraded.countsBreakdownItemDao().upsertAll(
                listOf(
                    CountsBreakdownItemEntity(
                        queryKey = "all",
                        grainKey = "park|shed|stage|breed|female",
                        sortIndex = 0,
                        dtoJson = "{}",
                        updatedAt = 13L,
                    ),
                ),
            )
            val remoteKeyDao = upgraded.countsBreakdownRemoteKeyDao()
            remoteKeyDao.upsert(
                CountsBreakdownRemoteKeyEntity(
                    queryKey = "all",
                    nextOffset = 20,
                    endReached = false,
                    updatedAt = 14L,
                ),
            )
            val remoteKey = remoteKeyDao.get("all")
            assertEquals(20, remoteKey?.nextOffset)
            assertEquals(false, remoteKey?.endReached)

            // 8. The Counts APPROVAL tables, also part of MIGRATION_12_13. This is the
            //    shape that actually crashed in MOB-007: an @Entity added to the @Database with
            //    no migration to CREATE its table compiles, passes every fresh-install test, and
            //    throws "Migration didn't properly handle counts_approval_items" on the first
            //    launch of an UPDATED app. Only a real old-file-reopen like this one catches it.
            upgraded.countsApprovalItemDao().upsertAll(
                listOf(
                    CountsApprovalItemEntity(
                        queryKey = "pending",
                        approvalRequestId = "req-1",
                        sortIndex = 0,
                        raisedAt = "2026-07-19T04:00:00Z",
                        dtoJson = "{}",
                        updatedAt = 15L,
                    ),
                ),
            )
            assertEquals(1, upgraded.countsApprovalItemDao().countFor("pending"))

            val approvalKeyDao = upgraded.countsApprovalRemoteKeyDao()
            approvalKeyDao.upsert(
                CountsApprovalRemoteKeyEntity(
                    queryKey = "pending",
                    nextCursor = "cursor-2",
                    endReached = false,
                    updatedAt = 16L,
                ),
            )
            val approvalKey = approvalKeyDao.get("pending")
            assertEquals("cursor-2", approvalKey?.nextCursor)
            assertEquals(false, approvalKey?.endReached)

            // 9. The shifting DESTINATION CATALOG table, also part of MIGRATION_12_13.
            //    The shifting screen's two cascading dropdowns render from this table, so an
            //    upgraded install that could not open it would leave an operator with two empty
            //    menus and no way to record a movement.
            val destinationsDao = upgraded.countsShiftingDestinationsCacheDao()
            destinationsDao.upsert(
                CountsShiftingDestinationsCacheEntity(
                    cacheKey = "shifting-destinations",
                    dtoJson = "{\"parks\":[]}",
                    updatedAt = 17L,
                ),
            )
            assertEquals(17L, destinationsDao.observe("shifting-destinations").first()?.updatedAt)
        } finally {
            upgraded.close()
        }
    }

    /**
     * The upgrade that THIS change actually ships: a device sitting at v10 — the version real
     * installs of `main` carry before the shed-completion + Counts features land — opening the app
     * for the first time after. [MIGRATION_10_11], [MIGRATION_11_12] and [MIGRATION_12_13] all run
     * here, demonstrating the full three-step migration chain: scan_roster_row in v11,
     * shed_completion_summary_cache in v12, then the seven Counts tables in v13. If any migration
     * fails to CREATE its tables, Room throws `IllegalStateException: Migration didn't properly
     * handle …` on this open, exactly as it would crash the operator's phone on first launch
     * after the update.
     */
    @Test
    fun `installed v10 db upgrades to v13 without crashing and keeps its data`() = runTest {
        // 1. A shipped v10 APK's on-disk file, written by a real Room database pinned to the v10
        //    entity set, so it carries Room's own identity metadata just like a user's phone would.
        val oldDb = Room.databaseBuilder(context, OldGoatDatabaseV10::class.java, DB_NAME).build()
        oldDb.bootstrapCacheDao().upsert(
            BootstrapCacheEntity(id = 0, dtoJson = SEEDED_BOOTSTRAP_JSON, updatedAt = SEEDED_AT),
        )
        oldDb.scanRosterRowDao().upsert(
            // The v10-SHAPED row (9 columns, no scopeKey/taskId/normalized* — those are added by
            // MIGRATION_10_11). Constructing the CURRENT ScanRosterRowEntity here would seed a v11+
            // table under a v10 database and make MIGRATION_10_11 add columns that already exist.
            ScanRosterRowEntityV10(
                id = "shed-9#obl-9",
                shedId = "shed-9",
                goatId = "goat-9",
                primaryTag = "IN-9999",
                secondaryTag = null,
                vaccineLabel = "PPR",
                status = "pending",
                obligationId = "obl-9",
                updatedAt = 99L,
            ),
        )
        oldDb.close() // closes the file at user_version = 10

        // 2. The app update: same file, current schema (v13), real migration chain. Room detects
        //    user_version = 10 and runs MIGRATION_10_11 (scan_roster_row restructure),
        //    then MIGRATION_11_12 (shed-completion cache), then MIGRATION_12_13 (seven Counts tables).
        val upgraded = Room.databaseBuilder(context, GoatDatabase::class.java, DB_NAME)
            .addMigrations(
                MIGRATION_1_2,
                MIGRATION_2_3,
                MIGRATION_3_4,
                MIGRATION_4_5,
                MIGRATION_5_6,
                MIGRATION_6_7,
                MIGRATION_7_8,
                MIGRATION_8_9,
                MIGRATION_9_10,
                MIGRATION_10_11,
                MIGRATION_11_12,
                MIGRATION_12_13,
            )
            .build()
        try {
            upgraded.openHelper.writableDatabase // force open + migrate + validate

            // 3. Pre-upgrade data survived — the bootstrap cache is preserved across all versions.
            //    scan_roster_row is recreated by MIGRATION_10_11 (v10->v11 restructures it to add
            //    scopeKey, so old v10 rows are not preserved, but the table exists and is usable).
            val kept = upgraded.bootstrapCacheDao().get()
            assertNotNull("bootstrap row must survive the v10 -> v13 upgrade", kept)
            assertEquals(SEEDED_BOOTSTRAP_JSON, kept?.dtoJson)
            assertEquals(SEEDED_AT, kept?.updatedAt)

            // 4. The v12 shed_completion_summary_cache table (MIGRATION_11_12) exists and round-trips
            //    post-upgrade.
            upgraded.shedCompletionSummaryCacheDao().upsert(
                ShedCompletionSummaryCacheEntity(cacheKey = "task-9", dtoJson = "{}", updatedAt = 20L),
            )
            assertEquals(
                20L,
                upgraded.shedCompletionSummaryCacheDao().observe("task-9").first()?.updatedAt,
            )

            // 5. Every one of the seven v13 Counts tables (from MIGRATION_12_13) exists and round-trips
            //    post-upgrade. These tables are purely additive — no existing table changes — so they
            //    survive as empty tables after the migration.
            upgraded.herdSummaryCacheDao()
                .upsert(HerdSummaryCacheEntity(cacheKey = "all", dtoJson = "{}", updatedAt = 21L))
            assertEquals(21L, upgraded.herdSummaryCacheDao().observe("all").first()?.updatedAt)

            upgraded.countsBreakdownMetaCacheDao()
                .upsert(CountsBreakdownMetaCacheEntity(cacheKey = "all", dtoJson = "{}", updatedAt = 22L))
            assertEquals(22L, upgraded.countsBreakdownMetaCacheDao().observe("all").first()?.updatedAt)

            upgraded.countsBreakdownItemDao().upsertAll(
                listOf(
                    CountsBreakdownItemEntity(
                        queryKey = "all",
                        grainKey = "park|shed|stage|breed|female",
                        sortIndex = 0,
                        dtoJson = "{}",
                        updatedAt = 23L,
                    ),
                ),
            )
            upgraded.countsBreakdownRemoteKeyDao().upsert(
                CountsBreakdownRemoteKeyEntity(
                    queryKey = "all",
                    nextOffset = 20,
                    endReached = false,
                    updatedAt = 24L,
                ),
            )
            assertEquals(20, upgraded.countsBreakdownRemoteKeyDao().get("all")?.nextOffset)

            upgraded.countsApprovalItemDao().upsertAll(
                listOf(
                    CountsApprovalItemEntity(
                        queryKey = "pending",
                        approvalRequestId = "req-1",
                        sortIndex = 0,
                        raisedAt = "2026-07-19T04:00:00Z",
                        dtoJson = "{}",
                        updatedAt = 25L,
                    ),
                ),
            )
            assertEquals(1, upgraded.countsApprovalItemDao().countFor("pending"))

            upgraded.countsApprovalRemoteKeyDao().upsert(
                CountsApprovalRemoteKeyEntity(
                    queryKey = "pending",
                    nextCursor = "cursor-2",
                    endReached = false,
                    updatedAt = 26L,
                ),
            )
            assertEquals("cursor-2", upgraded.countsApprovalRemoteKeyDao().get("pending")?.nextCursor)

            upgraded.countsShiftingDestinationsCacheDao().upsert(
                CountsShiftingDestinationsCacheEntity(
                    cacheKey = "shifting-destinations",
                    dtoJson = "{\"parks\":[]}",
                    updatedAt = 27L,
                ),
            )
            assertEquals(
                27L,
                upgraded.countsShiftingDestinationsCacheDao().observe("shifting-destinations").first()?.updatedAt,
            )
        } finally {
            upgraded.close()
        }
    }

    private companion object {
        const val DB_NAME = "upgrade-crash-goat.db"
        const val SEEDED_BOOTSTRAP_JSON = "{\"probe\":\"pre-upgrade\"}"
        const val SEEDED_AT = 111L
    }
}

/**
 * A minimal real Room database pinned to the app's original v1 schema (bootstrap cache only), used
 * only to lay down an authentic old-version file on disk for [GoatDatabaseUpgradeCrashTest]. Test
 * scope only — never shipped, never registered in DI.
 */
@Database(entities = [BootstrapCacheEntity::class], version = 1, exportSchema = false)
abstract class OldGoatDatabaseV1 : RoomDatabase() {
    abstract fun bootstrapCacheDao(): BootstrapCacheDao
}

/**
 * A minimal real Room database pinned to the v10 entity set — the schema a shipped `main` build
 * leaves on a user's phone — used only to lay down an authentic v10 file on disk for the
 * v10 -> v11 upgrade test. Test scope only: never shipped, never registered in DI. It must stay in
 * lockstep with what v10 actually contained (schemas/<db>/10.json); the point is to reproduce a
 * real installed database, not to mirror the current @Entity set.
 */
@Database(
    entities = [
        BootstrapCacheEntity::class,
        CalendarCacheEntity::class,
        ControlTowerCacheEntity::class,
        ExecutionRowsCacheEntity::class,
        ExecutionShedCacheEntity::class,
        ScanRosterCacheEntity::class,
        ScanRosterRowEntityV10::class,
        AdherenceCacheEntity::class,
        InsightsGapsCacheEntity::class,
        InsightsCoverageCacheEntity::class,
        RosterTimetableCacheEntity::class,
        RosterCoverageCacheEntity::class,
        TaskDetailCacheEntity::class,
        ScannedGoatEntity::class,
        RfidScanAttemptEntity::class,
        ProofCaptureEntity::class,
        VerificationQueueCacheEntity::class,
        CalendarScheduleEntity::class,
        CalendarScheduleRemoteKeyEntity::class,
    ],
    version = 10,
    exportSchema = false,
)
abstract class OldGoatDatabaseV10 : RoomDatabase() {
    abstract fun bootstrapCacheDao(): BootstrapCacheDao
    abstract fun scanRosterRowDao(): ScanRosterRowDaoV10
}

/**
 * The v10 shape of scan_roster_row (schemas/<db>/10.json): nine columns, none of the scopeKey /
 * taskId / normalizedPrimaryTag / normalizedSecondaryTag columns that MIGRATION_10_11 adds. It is a
 * test-only mirror of what a shipped v10 APK actually stored, deliberately separate from the current
 * [ScanRosterRowEntity] so this upgrade test reproduces a real v10 database rather than tracking the
 * live entity as it evolves.
 */
@Entity(tableName = "scan_roster_row")
data class ScanRosterRowEntityV10(
    @PrimaryKey val id: String,
    val shedId: String,
    val goatId: String,
    val primaryTag: String,
    val secondaryTag: String?,
    val vaccineLabel: String,
    val status: String,
    val obligationId: String,
    val updatedAt: Long,
)

@Dao
interface ScanRosterRowDaoV10 {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: ScanRosterRowEntityV10)
}
