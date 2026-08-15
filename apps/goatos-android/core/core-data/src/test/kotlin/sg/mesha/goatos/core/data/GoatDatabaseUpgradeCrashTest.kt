package sg.mesha.goatos.core.data

import androidx.room.Dao
import androidx.room.Database
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.sqlite.db.SupportSQLiteDatabase
import androidx.sqlite.db.SupportSQLiteOpenHelper
import androidx.sqlite.db.framework.FrameworkSQLiteOpenHelperFactory
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
import sg.mesha.goatos.core.database.capture.RfidScanAttemptEntity
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
import sg.mesha.goatos.core.data.cache.FeedDirectionItemEntity
import sg.mesha.goatos.core.data.cache.FeedDirectionMetaCacheEntity
import sg.mesha.goatos.core.data.cache.FeedDirectionRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.FeedPackingItemEntity
import sg.mesha.goatos.core.data.cache.FeedPackingMetaCacheEntity
import sg.mesha.goatos.core.data.cache.FeedPackingRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.FeedTransportScopedItemEntity
import sg.mesha.goatos.core.data.cache.HerdSummaryCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheEntity
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheEntity
import sg.mesha.goatos.core.data.cache.CaptureEvidenceDraftEntity
import sg.mesha.goatos.core.data.cache.ScanRosterRowDao
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.ShedCompletionSummaryCacheEntity
import sg.mesha.goatos.core.data.cache.AwaitingRfidItemEntity
import sg.mesha.goatos.core.data.cache.AwaitingRfidRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.ShiftingPendingItemEntity
import sg.mesha.goatos.core.data.cache.ShiftingPendingRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.TaskDetailCacheEntity
import sg.mesha.goatos.core.data.cache.VerificationQueueCacheEntity
import sg.mesha.goatos.core.data.cache.WorkflowCardEntity
import sg.mesha.goatos.core.data.cache.WorkflowChipsCacheEntity
import sg.mesha.goatos.core.data.cache.WorkflowDetailCacheEntity
import sg.mesha.goatos.core.data.cache.WorkflowRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipGalleryRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipRecordEntity
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipRecordRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShedEntity
import sg.mesha.goatos.core.data.weighing.WeighingObservationEntity
import sg.mesha.goatos.core.data.weighing.WeighingPlannerOperatorRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingPlannerRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingPlannerShedRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingShedObservationEntity
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingTaskRemoteKeyEntity
import sg.mesha.goatos.core.data.weighing.WeighingTaskRowEntity

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
            .addMigrations(*ALL_TEST_MIGRATIONS)
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

            upgraded.feedTransportScopedItemDao().upsertAll(
                listOf(
                    FeedTransportScopedItemEntity(
                        scopeKey = "2026-07-29|park-1||",
                        taskId = "task-1",
                        sortIndex = 0,
                        dtoJson = "{}",
                        updatedAt = 22L,
                    ),
                ),
            )
            assertEquals(
                "task-1",
                upgraded.feedTransportScopedItemDao().observe("2026-07-29|park-1||", 20).first().single().taskId,
            )

            // The v26 shared capture evidence draft (MIGRATION_25_26) exists and round-trips post-upgrade.
            // An installed APK opening this file must find the table Room's @Entity set expects —
            // an entity added with no migration compiles, works on a fresh install, and crashes
            // every upgrade (docs/decisions/room-migration-safety.md).
            upgraded.captureEvidenceDraftDao().upsert(
                CaptureEvidenceDraftEntity(
                    flowKey = "shifting",
                    entityId = "move-1",
                    step = "shifting",
                    outboxItemId = "proof-1",
                    updatedAt = 26L,
                ),
            )
            val draft = upgraded.captureEvidenceDraftDao().findFor("shifting", "move-1").single()
            assertEquals("proof-1", draft.outboxItemId)
            // A row written WITHOUT answers (the v26 shape) must read back cleanly after the v27
            // ALTER: an installed phone mid-capture keeps the clip it already recorded and simply
            // has no typed answers yet.
            assertNull(draft.answers)

            // The v27 answers column (MIGRATION_26_27) round-trips, so the typed half of a draft
            // survives Back + re-entry on an UPGRADED install, not only a fresh one.
            upgraded.captureEvidenceDraftDao().upsert(
                CaptureEvidenceDraftEntity(
                    flowKey = "milk_preparation",
                    entityId = "park-1:2026-07-31",
                    step = "__answers__",
                    answers = """{"morning":"12.5"}""",
                    updatedAt = 27L,
                ),
            )
            assertEquals(
                """{"morning":"12.5"}""",
                upgraded.captureEvidenceDraftDao()
                    .findFor("milk_preparation", "park-1:2026-07-31").single().answers,
            )

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
                    seq = 3,
                    updatedAt = 9L,
                ),
            )
            val byTag = rosterDao.findByTag(scopeKey, "in1234")
            assertEquals("obl-1", byTag?.obligationId)
            val counts = rosterDao.countByStatus(scopeKey)
            assertEquals(1, counts.single().count)
            // v14 (MIGRATION_13_14): the added `seq` column round-trips through the bounded window read
            // (so the ordering column Room now validates against actually exists post-upgrade), and the
            // dropped scan_roster_cache blob table is gone.
            assertEquals(3L, rosterDao.observeRowsWindow(scopeKey, 20).first().single().seq)
            assertEquals("due", counts.single().status)

            // 6. The v12 shed_completion_summary_cache table (vaccination shed acknowledgement) is
            //    present and usable post-upgrade — a write + read round-trip proves MIGRATION_11_12
            //    created the table with the shape Room expects.
            val summaryDao = upgraded.shedCompletionSummaryCacheDao()
            summaryDao.upsert(
                ShedCompletionSummaryCacheEntity(cacheKey = "task-1", dtoJson = "{}", updatedAt = 12L),
            )
            assertEquals(12L, summaryDao.observe("task-1").first()?.updatedAt)

            // 7. The v13 proof_capture.captureSource column (persisted capture_source SSOT) is
            //    present and usable post-upgrade — insert a proof with a NON-default source and read
            //    it back verbatim, proving MIGRATION_12_13's ADD COLUMN produced a column Room can
            //    round-trip (not silently coerced to the default).
            val proofDao = upgraded.proofCaptureDao()
            proofDao.insert(
                ProofCaptureEntity(
                    id = "proof-1",
                    taskId = "task-1",
                    fieldKey = "administration_video",
                    proofSubject = "administration",
                    localUri = "file://p.mp4",
                    mimeType = "video/mp4",
                    capturedAtMs = 5L,
                    capturedStartMs = 0L,
                    capturedEndMs = 5L,
                    idempotencyKey = "proof-upload:task-1:proof-1",
                    captureSource = "external_upload",
                ),
            )
            assertEquals("external_upload", proofDao.findById("proof-1")?.captureSource)

            // 8. Same proof for the seven v15 Counts tables (MIGRATION_14_15). Open-without-crash
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

            // 9. The Counts APPROVAL tables, also part of MIGRATION_14_15. This is the
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

            // 10. The shifting DESTINATION CATALOG table, also part of MIGRATION_14_15.
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

            // 11. The six v17 FEED tables (MIGRATION_16_17). Same MOB-007 proof as the Counts
            //     tables: open-without-crash only proves Room accepted the migrated schema; a
            //     write+read round-trip on each of the six proves it actually exists with the shape
            //     its @Entity declares — both summary blobs, both normalized paged-row tables, and
            //     both remote-key tables.
            assertFeedTablesRoundTrip(upgraded, base = 40L)

            // 12. The v22 Weighing tables are separate from Vaccination capture state and round-trip.
            assertWeighingTablesRoundTrip(upgraded, base = 120L)

            // 13. The ten v24 Weighing LEADERSHIP read-model tables (MIGRATION_23_24). Additive, so
            //     an omitted CREATE would still compile and still pass every fresh-install test —
            //     only reopening a real old file like this one catches it.
            assertWeighingLeadershipTablesRoundTrip(upgraded, base = 140L)
        } finally {
            upgraded.close()
        }
    }

    /** Round-trips all six Feed read-model tables so a missing/mismatched CREATE in MIGRATION_16_17
     *  fails here — the MOB-007 upgrade-crash class — rather than on a user's phone. */
    private suspend fun assertFeedTablesRoundTrip(upgraded: GoatDatabase, base: Long) {
        upgraded.feedDirectionMetaCacheDao()
            .upsert(FeedDirectionMetaCacheEntity(cacheKey = "fd", dtoJson = "{}", updatedAt = base))
        assertEquals(base, upgraded.feedDirectionMetaCacheDao().observe("fd").first()?.updatedAt)

        upgraded.feedDirectionItemDao().upsertAll(
            listOf(
                FeedDirectionItemEntity(
                    queryKey = "fd",
                    grainKey = "shed|normal|group|arm|tag|1",
                    sortIndex = 0,
                    dtoJson = "{}",
                    updatedAt = base + 1,
                ),
            ),
        )
        upgraded.feedDirectionRemoteKeyDao().upsert(
            FeedDirectionRemoteKeyEntity(queryKey = "fd", nextOffset = 20, endReached = false, updatedAt = base + 2),
        )
        assertEquals(20, upgraded.feedDirectionRemoteKeyDao().get("fd")?.nextOffset)

        upgraded.feedPackingMetaCacheDao()
            .upsert(FeedPackingMetaCacheEntity(cacheKey = "fp", dtoJson = "{}", updatedAt = base + 3))
        assertEquals(base + 3, upgraded.feedPackingMetaCacheDao().observe("fp").first()?.updatedAt)

        upgraded.feedPackingItemDao().upsertAll(
            listOf(
                FeedPackingItemEntity(
                    queryKey = "fp",
                    grainKey = "shed|normal|1",
                    sortIndex = 0,
                    dtoJson = "{}",
                    updatedAt = base + 4,
                ),
            ),
        )
        upgraded.feedPackingRemoteKeyDao().upsert(
            FeedPackingRemoteKeyEntity(queryKey = "fp", nextOffset = 20, endReached = true, updatedAt = base + 5),
        )
        assertEquals(true, upgraded.feedPackingRemoteKeyDao().get("fp")?.endReached)
    }

    /**
     * The upgrade that THIS change actually ships: a device sitting at v10 — the version real
     * installs of `main` carry before the shed-completion + Counts features land — opening the app
     * for the first time after. [MIGRATION_10_11], [MIGRATION_11_12], [MIGRATION_12_13],
     * [MIGRATION_13_14] and [MIGRATION_14_15] all run here, demonstrating the full five-step
     * migration chain: scan_roster_row in v11, shed_completion_summary_cache in v12,
     * proof_capture.captureSource in v13, scan_roster_row `seq` + scan_roster_cache drop in v14,
     * then the seven Counts tables in v15. If any migration fails to CREATE its tables, Room throws
     * `IllegalStateException: Migration didn't properly handle …` on this open, exactly as it would
     * crash the operator's phone on first launch after the update.
     */
    @Test
    fun `installed v10 db upgrades to current without crashing and keeps its data`() = runTest {
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

        // 2. The app update: same file, current schema (v16), real migration chain. Room detects
        //    user_version = 10 and runs MIGRATION_10_11 (scan_roster_row restructure), then
        //    MIGRATION_11_12 (shed-completion cache), MIGRATION_12_13 (proof_capture.captureSource),
        //    MIGRATION_13_14 (scan_roster_row seq + scan_roster_cache drop), MIGRATION_14_15
        //    (seven Counts tables), then MIGRATION_15_16 (backend scan timestamp cache).
        val upgraded = Room.databaseBuilder(context, GoatDatabase::class.java, DB_NAME)
            .addMigrations(*ALL_TEST_MIGRATIONS)
            .build()
        try {
            upgraded.openHelper.writableDatabase // force open + migrate + validate

            // 3. Pre-upgrade data survived — the bootstrap cache is preserved across all versions.
            //    scan_roster_row is recreated by MIGRATION_10_11 (v10->v11 restructures it to add
            //    scopeKey, so old v10 rows are not preserved, but the table exists and is usable).
            val kept = upgraded.bootstrapCacheDao().get()
            assertNotNull("bootstrap row must survive the v10 -> v15 upgrade", kept)
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

            // 5. Every one of the seven v15 Counts tables (from MIGRATION_14_15) exists and round-trips
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

            // 6. The six v17 Feed tables (MIGRATION_16_17) exist and round-trip post-upgrade.
            assertFeedTablesRoundTrip(upgraded, base = 40L)

            // 7. The two v18 shifting pending-execution tables (MIGRATION_17_18) exist and round-trip.
            assertShiftingPendingTablesRoundTrip(upgraded, base = 60L)

            // 8. The two v19 "Awaiting RFID" tables (MIGRATION_18_19) exist and round-trip.
            assertAwaitingRfidTablesRoundTrip(upgraded, base = 80L)

            // 9. The four v20 Birth/Death workflow tables (MIGRATION_19_20) exist and round-trip.
            assertWorkflowTablesRoundTrip(upgraded, base = 100L)

            // 10. The three v22 Weighing tables (MIGRATION_21_22) exist and round-trip.
            assertWeighingTablesRoundTrip(upgraded, base = 120L)

            // 11. The ten v24 Weighing LEADERSHIP read-model tables (MIGRATION_23_24) round-trip.
            assertWeighingLeadershipTablesRoundTrip(upgraded, base = 140L)
        } finally {
            upgraded.close()
        }
    }

    /**
     * Round-trips every v24 Weighing LEADERSHIP table so an omitted CREATE fails on upgrade.
     *
     * Each of the five read models is exercised through the SAME bounded observed read the screens
     * will use, so a table that exists with the wrong columns/indices fails here rather than on a
     * user's phone after an update.
     */
    private suspend fun assertWeighingLeadershipTablesRoundTrip(upgraded: GoatDatabase, base: Long) {
        val taskDao = upgraded.weighingTaskDao()
        taskDao.upsertAll(
            listOf(
                WeighingTaskRowEntity(
                    queryKey = "weighing-tasks:all:",
                    campaignId = "task-1",
                    sortIndex = 0,
                    dtoJson = "{}",
                    updatedAt = base,
                ),
            ),
        )
        assertEquals("task-1", taskDao.observeWindow("weighing-tasks:all:", 20).first().single().campaignId)

        val taskKeyDao = upgraded.weighingTaskRemoteKeyDao()
        taskKeyDao.upsert(
            WeighingTaskRemoteKeyEntity(
                queryKey = "weighing-tasks:all:",
                nextCursor = "cursor-1",
                endReached = false,
                activeCount = 3,
                completedCount = 1,
                canPublish = true,
                canEnd = false,
                canReopen = false,
                updatedAt = base + 1,
            ),
        )
        assertEquals(3, taskKeyDao.get("weighing-tasks:all:")?.activeCount)

        val bucketDao = upgraded.weighingTaskBucketDao()
        bucketDao.upsertAll(
            listOf(
                WeighingTaskBucketRowEntity(
                    campaignId = "task-1",
                    campaignShedId = "bucket-1",
                    sortIndex = 0,
                    dtoJson = "{}",
                    updatedAt = base + 2,
                ),
            ),
        )
        assertEquals("bucket-1", bucketDao.observeWindow("task-1", 20).first().single().campaignShedId)

        val bucketKeyDao = upgraded.weighingTaskBucketRemoteKeyDao()
        bucketKeyDao.upsert(
            WeighingTaskBucketRemoteKeyEntity(
                campaignId = "task-1",
                nextCursor = null,
                endReached = true,
                totalCount = 76,
                updatedAt = base + 3,
            ),
        )
        // The WHOLE-TASK count survives, not the page length.
        assertEquals(76, bucketKeyDao.get("task-1")?.totalCount)

        val shedDao = upgraded.weighingLeadershipShedDao()
        shedDao.upsert(
            WeighingLeadershipShedEntity(
                shedKey = "task-1:bucket-1",
                campaignId = "task-1",
                campaignShedId = "bucket-1",
                shedName = "Gandhi 1",
                parkName = "Channapatna",
                weighDate = "2026-07-31",
                operatorUserId = "user-1",
                operatorDisplayName = "Amit Kumar",
                category = "individual_animal",
                status = "in_progress",
                periodLabel = "",
                estimatedAnimalCount = 40,
                maxShedVideos = 5,
                lumpSumJson = null,
                galleryQueryKey = "weighing-videos",
                gallerySortIndex = 0,
                updatedAt = base + 4,
            ),
        )
        val cachedShed = shedDao.observe("task-1:bucket-1").first()
        // The deep-link context columns are what let a cold open render without route arguments.
        assertEquals("Channapatna", cachedShed?.parkName)
        assertEquals("2026-07-31", cachedShed?.weighDate)
        assertEquals("Amit Kumar", cachedShed?.operatorDisplayName)
        assertEquals(1, shedDao.observeGalleryWindow("weighing-videos", 20).first().size)

        val recordDao = upgraded.weighingLeadershipRecordDao()
        recordDao.upsertAll(
            listOf(
                WeighingLeadershipRecordEntity(
                    shedKey = "task-1:bucket-1",
                    observationId = "obs-1",
                    sortIndex = 0,
                    dtoJson = "{}",
                    updatedAt = base + 5,
                ),
            ),
        )
        assertEquals("obs-1", recordDao.observeWindow("task-1:bucket-1", 20).first().single().observationId)

        val recordKeyDao = upgraded.weighingLeadershipRecordRemoteKeyDao()
        recordKeyDao.upsert(
            WeighingLeadershipRecordRemoteKeyEntity(
                shedKey = "task-1:bucket-1",
                nextCursor = "obs-cursor",
                endReached = false,
                updatedAt = base + 6,
            ),
        )
        assertEquals("obs-cursor", recordKeyDao.get("task-1:bucket-1")?.nextCursor)

        // v25: the gallery's OWN cursor table. It used to share the task-list remote-key table,
        // where the task list's newest-N-filters prune could evict it mid-scroll.
        val galleryKeyDao = upgraded.weighingLeadershipGalleryRemoteKeyDao()
        galleryKeyDao.upsert(
            WeighingLeadershipGalleryRemoteKeyEntity(
                queryKey = "weighing-videos",
                nextCursor = "gallery-cursor",
                endReached = false,
                updatedAt = base + 6,
            ),
        )
        assertEquals("gallery-cursor", galleryKeyDao.get("weighing-videos")?.nextCursor)

        val plannerDao = upgraded.weighingPlannerCatalogDao()
        plannerDao.upsertSheds(
            listOf(
                WeighingPlannerShedRowEntity(
                    queryKey = "2026-08-03",
                    locationId = "loc-1",
                    partitionKey = "",
                    parkId = "park-1",
                    parkName = "Channapatna",
                    sortIndex = 0,
                    shedJson = "{}",
                    existingCampaignJson = null,
                    updatedAt = base + 7,
                ),
            ),
        )
        assertEquals("park-1", plannerDao.observeShedWindow("2026-08-03", 20).first().single().parkId)

        plannerDao.upsertOperators(
            listOf(
                WeighingPlannerOperatorRowEntity(
                    queryKey = "2026-08-03",
                    userId = "user-1",
                    sortIndex = 0,
                    dtoJson = "{}",
                    updatedAt = base + 8,
                ),
            ),
        )
        assertEquals(1, plannerDao.observeOperators("2026-08-03", 20).first().size)

        val plannerKeyDao = upgraded.weighingPlannerRemoteKeyDao()
        plannerKeyDao.upsert(
            WeighingPlannerRemoteKeyEntity(
                queryKey = "2026-08-03",
                nextCursor = "catalog-cursor",
                endReached = false,
                updatedAt = base + 9,
            ),
        )
        assertEquals("catalog-cursor", plannerKeyDao.get("2026-08-03")?.nextCursor)
    }

    /** Round-trips the v22 Weighing local tables so an omitted migration fails on upgrade. */
    private suspend fun assertWeighingTablesRoundTrip(upgraded: GoatDatabase, base: Long) {
        val scope = "campaign-1:work-1:shed-1"
        upgraded.weighingRosterDao().upsertAll(
            listOf(
                WeighingRosterRowEntity(
                    id = "row-1",
                    scopeKey = scope,
                    tenantId = "tenant-1",
                    campaignId = "campaign-1",
                    workGroupId = "work-1",
                    campaignShedId = "shed-1",
                    expectedLocationId = "loc-1",
                    expectedLocationLabel = "Gandhi 1",
                    actualLocationId = null,
                    actualLocationLabel = null,
                    animalId = "animal-1",
                    displayAnimalId = "G-1",
                    primaryTag = "RFID-1",
                    secondaryTag = null,
                    normalizedPrimaryTag = "rfid1",
                    normalizedSecondaryTag = null,
                    status = "pending",
                    availabilityStatus = "expected_shed",
                    seq = 1L,
                    updatedAt = base,
                ),
            ),
        )
        assertEquals(1, upgraded.weighingRosterDao().observeScopeTotal(scope).first())
        assertEquals("animal-1", upgraded.weighingRosterDao().findByTag(scope, "rfid1")?.animalId)

        upgraded.weighingObservationDao().insert(
            WeighingObservationEntity(
                observationId = "obs-1",
                scopeKey = scope,
                tenantId = "tenant-1",
                campaignId = "campaign-1",
                workGroupId = "work-1",
                campaignShedId = "shed-1",
                expectedLocationId = "loc-1",
                expectedLocationLabel = "Gandhi 1",
                actualLocationId = "loc-1",
                actualLocationLabel = "Gandhi 1",
                scannedIdentifier = "RFID-1",
                weightKg = 12.4,
                proofCaptureId = null,
                serverProofId = null,
                syncStatus = "PENDING_LOCAL",
                idempotencyKey = "obs-key-1",
                capturedAtMs = base + 1,
                lastError = null,
            ),
        )
        // Lookup is keyed on the scanned tag -- free-flow weighing carries no animal identity at all.
        assertEquals(
            "obs-1",
            upgraded.weighingObservationDao().findByScannedIdentifier(scope, "RFID-1")?.observationId,
        )

        upgraded.weighingShedObservationDao().insert(
            WeighingShedObservationEntity(
                shedObservationId = "shed-obs-1",
                scopeKey = scope,
                tenantId = "tenant-1",
                campaignId = "campaign-1",
                workGroupId = "work-1",
                campaignShedId = "shed-1",
                expectedLocationId = "loc-1",
                expectedLocationLabel = "Gandhi 1",
                resultJson = "{}",
                proofCaptureId = null,
                serverProofId = null,
                syncStatus = "PENDING_LOCAL",
                idempotencyKey = "shed-obs-key-1",
                capturedAtMs = base + 2,
                lastError = null,
            ),
        )
        assertEquals("shed-obs-1", upgraded.weighingShedObservationDao().findByScope(scope)?.shedObservationId)
    }

    /** Round-trips the v18 shifting pending-execution queue pair so a missing/mismatched CREATE in
     *  MIGRATION_17_18 fails here — the MOB-007 upgrade-crash class — rather than on a user's phone. */
    private suspend fun assertShiftingPendingTablesRoundTrip(upgraded: GoatDatabase, base: Long) {
        val scope = cacheKey("shifting-pending", "park-1", "shed-1")
        upgraded.shiftingPendingItemDao().upsertAll(
            listOf(
                ShiftingPendingItemEntity(
                    queryKey = scope,
                    shiftingEventId = "move-1",
                    sortIndex = 0,
                    raisedAt = "2026-07-22T04:00:00Z",
                    dtoJson = "{}",
                    updatedAt = base,
                ),
            ),
        )
        assertEquals(1, upgraded.shiftingPendingItemDao().countFor(scope))
        assertNotNull(upgraded.shiftingPendingItemDao().findById("move-1"))
        upgraded.shiftingPendingRemoteKeyDao().upsert(
            ShiftingPendingRemoteKeyEntity(queryKey = scope, nextCursor = "cursor-2", endReached = false, updatedAt = base + 1),
        )
        assertEquals("cursor-2", upgraded.shiftingPendingRemoteKeyDao().get(scope)?.nextCursor)
    }

    /** Round-trips the four v20 Birth/Death workflow tables so a missing/mismatched CREATE in
     *  MIGRATION_19_20 fails here — the MOB-007 upgrade-crash class — rather than on a user's phone. */
    private suspend fun assertWorkflowTablesRoundTrip(upgraded: GoatDatabase, base: Long) {
        val scope = cacheKey("workflows", "birth", "2026-07-27", "all")
        upgraded.workflowCardDao().upsertAll(
            listOf(
                WorkflowCardEntity(
                    queryKey = scope,
                    workflowId = "wf-1",
                    sortIndex = 0,
                    dtoJson = "{}",
                    updatedAt = base,
                ),
            ),
        )
        assertEquals(1, upgraded.workflowCardDao().countFor(scope))
        assertNotNull(upgraded.workflowCardDao().findById("wf-1"))
        upgraded.workflowRemoteKeyDao().upsert(
            WorkflowRemoteKeyEntity(queryKey = scope, nextCursor = "cursor-2", endReached = false, updatedAt = base + 1),
        )
        assertEquals("cursor-2", upgraded.workflowRemoteKeyDao().get(scope)?.nextCursor)
        upgraded.workflowChipsCacheDao().upsert(
            WorkflowChipsCacheEntity(cacheKey = cacheKey("workflow-chips", "birth", "2026-07-27"), dtoJson = "{}", updatedAt = base + 2),
        )
        assertEquals(
            base + 2,
            upgraded.workflowChipsCacheDao().observe(cacheKey("workflow-chips", "birth", "2026-07-27")).first()?.updatedAt,
        )
        upgraded.workflowDetailCacheDao().upsert(
            WorkflowDetailCacheEntity(cacheKey = "wf-1", dtoJson = "{}", updatedAt = base + 3),
        )
        assertEquals(base + 3, upgraded.workflowDetailCacheDao().observe("wf-1").first()?.updatedAt)
    }

    /** Round-trips the v19 "Awaiting RFID" list pair so a missing/mismatched CREATE in
     *  MIGRATION_18_19 fails here — the MOB-007 upgrade-crash class — rather than on a user's phone. */
    private suspend fun assertAwaitingRfidTablesRoundTrip(upgraded: GoatDatabase, base: Long) {
        upgraded.awaitingRfidItemDao().upsertAll(
            listOf(
                AwaitingRfidItemEntity(
                    goatId = "goat-1",
                    sortIndex = 0,
                    displayId = "G-000101",
                    dtoJson = "{}",
                    updatedAt = base,
                ),
            ),
        )
        assertEquals(1, upgraded.awaitingRfidItemDao().countAll())
        assertNotNull(upgraded.awaitingRfidItemDao().findById("goat-1"))
        upgraded.awaitingRfidRemoteKeyDao().upsert(
            AwaitingRfidRemoteKeyEntity(id = AwaitingRfidRemoteKeyEntity.SCOPE, nextCursor = "G-000101", endReached = false, updatedAt = base + 1),
        )
        assertEquals("G-000101", upgraded.awaitingRfidRemoteKeyDao().get(AwaitingRfidRemoteKeyEntity.SCOPE)?.nextCursor)
    }

    /**
     * v25 -> v26: the installed-APK upgrade that drops `weighing_observation.animalId` and re-keys
     * the bucket's unique index onto `scannedIdentifier`.
     *
     * This is the class of bug a plain in-memory Room test is BLIND to: an `@Entity` change with no
     * matching `Migration` compiles, passes every in-memory test and works on a FRESH install, then
     * throws `IllegalStateException: Migration didn't properly handle weighing_observation` on the
     * first launch of every UPGRADED phone. So the v25 file is built on disk from the real
     * migration chain, seeded through raw SQL with the v25 column set (`animalId` present), closed
     * at user_version = 25, and reopened with the CURRENT schema + the real migrations.
     *
     * The rebuild must also be NON-DESTRUCTIVE: `weighing_observation` holds unsynced operator
     * captures, so the seeded PENDING_LOCAL row must still be there afterwards with its weight,
     * idempotency key and proof ids intact.
     */
    @Test
    fun `installed v25 db upgrades to v26 dropping animalId without crashing or losing captures`() = runTest {
        // 1. A real v25 file: v1 schema + every migration up to 24->25, seeded like a phone that
        //    captured two weights offline and has not synced them.
        seedV25DatabaseFile { db ->
            db.execSQL(
                "INSERT INTO `weighing_observation` (`observationId`, `scopeKey`, `tenantId`, `campaignId`, " +
                    "`workGroupId`, `campaignShedId`, `expectedLocationId`, `expectedLocationLabel`, " +
                    "`actualLocationId`, `actualLocationLabel`, `animalId`, `scannedIdentifier`, `weightKg`, " +
                    "`proofCaptureId`, `serverProofId`, `syncStatus`, `idempotencyKey`, `capturedAtMs`, `lastError`) " +
                    "VALUES ('obs-v25-1', 'campaign-1:work-1:shed-1', 'tenant-1', 'campaign-1', 'work-1', " +
                    "'shed-1', 'loc-1', 'Gandhi 1', 'loc-1', 'Gandhi 1', '', 'RFID-V25-1', 12.4, " +
                    "'proof-local-1', 'proof-server-1', 'PENDING_LOCAL', 'weighing:individual:v25-1', 900, NULL)",
            )
            db.execSQL(
                "INSERT INTO `weighing_observation` (`observationId`, `scopeKey`, `tenantId`, `campaignId`, " +
                    "`workGroupId`, `campaignShedId`, `expectedLocationId`, `expectedLocationLabel`, " +
                    "`actualLocationId`, `actualLocationLabel`, `animalId`, `scannedIdentifier`, `weightKg`, " +
                    "`proofCaptureId`, `serverProofId`, `syncStatus`, `idempotencyKey`, `capturedAtMs`, `lastError`) " +
                    "VALUES ('obs-v25-2', 'campaign-1:work-1:shed-1', 'tenant-1', 'campaign-1', 'work-1', " +
                    "'shed-1', 'loc-1', 'Gandhi 1', NULL, NULL, 'legacy-animal-2', 'RFID-V25-2', 13.5, " +
                    "NULL, NULL, 'READY_TO_SUBMIT', 'weighing:individual:v25-2', 950, NULL)",
            )
        }

        // 2. The app update: SAME file, current schema, real migration chain. A missing or wrong
        //    MIGRATION_25_26 throws right here.
        val upgraded = Room.databaseBuilder(context, GoatDatabase::class.java, DB_NAME)
            .addMigrations(*ALL_TEST_MIGRATIONS)
            .build()
        try {
            upgraded.openHelper.writableDatabase // force open + migrate + schema validate

            // 3. Neither unsynced capture was lost, and every field the outbox needs survived.
            val dao = upgraded.weighingObservationDao()
            val kept = dao.observeForScope("campaign-1:work-1:shed-1", 20).first()
            assertEquals(
                listOf("RFID-V25-1", "RFID-V25-2"),
                kept.map { it.scannedIdentifier },
            )
            val first = kept.first()
            assertEquals(12.4, first.weightKg, 0.0)
            assertEquals("PENDING_LOCAL", first.syncStatus)
            assertEquals("weighing:individual:v25-1", first.idempotencyKey)
            assertEquals("proof-local-1", first.proofCaptureId)
            assertEquals("proof-server-1", first.serverProofId)
            assertEquals(900L, first.capturedAtMs)

            // 4. The column really is gone -- not merely ignored by the entity.
            val columns = mutableListOf<String>()
            upgraded.openHelper.writableDatabase
                .query("PRAGMA table_info(`weighing_observation`)").use { c ->
                    while (c.moveToNext()) columns += c.getString(1)
                }
            assertTrue("animalId must be dropped by MIGRATION_25_26, got $columns", "animalId" !in columns)
            assertTrue("scannedIdentifier must survive", "scannedIdentifier" in columns)

            // 5. And the re-keyed unique index is the one that exists.
            val indices = mutableListOf<String>()
            upgraded.openHelper.writableDatabase
                .query("PRAGMA index_list(`weighing_observation`)").use { c ->
                    while (c.moveToNext()) indices += c.getString(1)
                }
            assertTrue(
                "unique index must be re-keyed onto scannedIdentifier, got $indices",
                "index_weighing_observation_campaignId_campaignShedId_scannedIdentifier" in indices,
            )
            assertTrue(
                "the animalId-keyed unique index must be gone, got $indices",
                "index_weighing_observation_campaignId_campaignShedId_animalId" !in indices,
            )

            // 6. THE CAPTURE-DROP DEFECT: two different scanned tags in the SAME bucket must both
            //    persist. Under the old (campaignId, campaignShedId, animalId) unique index both
            //    collided on animalId = "" and the DAO's OnConflictStrategy.IGNORE silently threw
            //    the second capture away -- a weight taken in a shed, gone with no error.
            dao.insert(observationRow("obs-new-a", "RFID-NEW-A", 20.0, capturedAtMs = 1_000))
            dao.insert(observationRow("obs-new-b", "RFID-NEW-B", 21.0, capturedAtMs = 1_001))
            val afterTwoScans = dao.observeForScope("campaign-1:work-1:shed-1", 20).first()
            assertEquals(
                listOf("RFID-V25-1", "RFID-V25-2", "RFID-NEW-A", "RFID-NEW-B"),
                afterTwoScans.map { it.scannedIdentifier },
            )
        } finally {
            upgraded.close()
        }
    }

    /**
     * Builds a genuine on-disk v25 database: the real v1 schema, then every real migration object
     * up to 24->25, then [seed], closed at user_version = 25 with Room's identity row written so
     * the file is indistinguishable from one a shipped v25 APK left behind.
     */
    private fun seedV25DatabaseFile(seed: (SupportSQLiteDatabase) -> Unit) {
        val configuration = SupportSQLiteOpenHelper.Configuration.builder(context)
            .name(DB_NAME)
            .callback(object : SupportSQLiteOpenHelper.Callback(25) {
                override fun onCreate(db: SupportSQLiteDatabase) {
                    db.execSQL(
                        "CREATE TABLE IF NOT EXISTS `bootstrap_cache` " +
                            "(`id` INTEGER NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                            "PRIMARY KEY(`id`))",
                    )
                    V25_MIGRATIONS.forEach { it.migrate(db) }
                    // Room's own bookkeeping, as a shipped APK would have left it.
                    db.execSQL("CREATE TABLE IF NOT EXISTS room_master_table (id INTEGER PRIMARY KEY,identity_hash TEXT)")
                    db.execSQL(
                        "INSERT OR REPLACE INTO room_master_table (id,identity_hash) VALUES(42, '$V25_IDENTITY_HASH')",
                    )
                    seed(db)
                }

                override fun onUpgrade(db: SupportSQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit
            })
            .build()
        FrameworkSQLiteOpenHelperFactory().create(configuration).use { helper ->
            helper.writableDatabase // force onCreate + seed
        }
    }

    private fun observationRow(
        observationId: String,
        scannedIdentifier: String,
        weightKg: Double,
        capturedAtMs: Long,
    ) = WeighingObservationEntity(
        observationId = observationId,
        scopeKey = "campaign-1:work-1:shed-1",
        tenantId = "tenant-1",
        campaignId = "campaign-1",
        workGroupId = "work-1",
        campaignShedId = "shed-1",
        expectedLocationId = "loc-1",
        expectedLocationLabel = "Gandhi 1",
        actualLocationId = "loc-1",
        actualLocationLabel = "Gandhi 1",
        scannedIdentifier = scannedIdentifier,
        weightKg = weightKg,
        proofCaptureId = null,
        serverProofId = null,
        syncStatus = "PENDING_LOCAL",
        idempotencyKey = "weighing:individual:$observationId",
        capturedAtMs = capturedAtMs,
        lastError = null,
    )

    private companion object {
        const val DB_NAME = "upgrade-crash-goat.db"
        const val SEEDED_BOOTSTRAP_JSON = "{\"probe\":\"pre-upgrade\"}"
        const val SEEDED_AT = 111L

        /** The v25 identity hash, from schemas/<db>/25.json — what a shipped v25 APK wrote. */
        const val V25_IDENTITY_HASH = "9caa0d79deccc9a2ea32c22de09aa909"

        /** Every real migration object, in order — the chain DatabaseFactory installs. */
        val ALL_TEST_MIGRATIONS = arrayOf(
            MIGRATION_1_2, MIGRATION_2_3, MIGRATION_3_4, MIGRATION_4_5, MIGRATION_5_6,
            MIGRATION_6_7, MIGRATION_7_8, MIGRATION_8_9, MIGRATION_9_10, MIGRATION_10_11,
            MIGRATION_11_12, MIGRATION_12_13, MIGRATION_13_14, MIGRATION_14_15, MIGRATION_15_16,
            MIGRATION_16_17, MIGRATION_17_18, MIGRATION_18_19, MIGRATION_19_20, MIGRATION_20_21,
            MIGRATION_21_22, MIGRATION_22_23, MIGRATION_23_24, MIGRATION_24_25, MIGRATION_25_26,
            MIGRATION_26_27, MIGRATION_27_28, MIGRATION_28_29, MIGRATION_29_30, MIGRATION_30_31,
            MIGRATION_31_32, MIGRATION_32_33, MIGRATION_33_34, MIGRATION_34_35, MIGRATION_35_36,
            MIGRATION_36_37, MIGRATION_37_38, MIGRATION_38_39, MIGRATION_39_40, MIGRATION_40_41,
            MIGRATION_41_42, MIGRATION_42_43, MIGRATION_43_44, MIGRATION_44_45, MIGRATION_45_46,
        )

        /** The chain that produces a v25 file: everything up to and including MIGRATION_24_25 —
         *  i.e. everything except MIGRATION_25_26 onwards, the last EIGHTEEN entries of
         *  ALL_TEST_MIGRATIONS. Keep this drop count in lockstep with the array above: adding a
         *  migration without bumping it silently writes a wrong-version file. */
        // Version-bounded, not positional: dropLast(N) silently kept extra migrations every time
        // the chain grew, which re-broke this fixture (2026-08-15).
        val V25_MIGRATIONS = ALL_TEST_MIGRATIONS.filter { it.endVersion <= 25 }
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
        ScanRosterCacheEntityV10::class,
        ScanRosterRowEntityV10::class,
        AdherenceCacheEntity::class,
        InsightsGapsCacheEntity::class,
        InsightsCoverageCacheEntity::class,
        RosterTimetableCacheEntity::class,
        RosterCoverageCacheEntity::class,
        TaskDetailCacheEntity::class,
        ScannedGoatEntityV10::class,
        RfidScanAttemptEntity::class,
        ProofCaptureEntityV10::class,
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

@Entity(
    tableName = "scanned_goat_capture",
    indices = [
        Index(value = ["taskId", "fieldKey", "tag"], unique = true),
        Index(value = ["taskId", "fieldKey", "capturedAtMs"]),
    ],
)
data class ScannedGoatEntityV10(
    @PrimaryKey val id: String,
    val taskId: String,
    val fieldKey: String,
    val tag: String,
    val goatId: String?,
    val obligationId: String?,
    val capturedAtMs: Long,
    val syncStatus: String = "PENDING",
)

/**
 * The v10 shape of the `scan_roster_cache` JSON-blob table (schemas/<db>/10.json): the standard
 * `(cacheKey, dtoJson, updatedAt)` cache row. It is a test-only mirror because main's MIGRATION_13_14
 * DROPPED this table and deleted the production `ScanRosterCacheEntity` class — but a faithful v10
 * on-disk file still has to contain it, so the v10 -> v15 upgrade actually exercises that DROP.
 */
@Entity(tableName = "scan_roster_cache")
data class ScanRosterCacheEntityV10(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

/**
 * The v10 shape of `proof_capture` (schemas/<db>/10.json): 17 columns, WITHOUT the `captureSource`
 * column that main's MIGRATION_12_13 adds at v13. It is a test-only mirror because the current
 * [ProofCaptureEntity] has since gained `captureSource`; seeding the v10 file with that production
 * entity would create the column early and make MIGRATION_12_13's `ADD COLUMN` fail with a duplicate.
 * This reproduces the real installed-v10 table so the v10 -> v15 upgrade exercises that ADD COLUMN.
 */
@Entity(
    tableName = "proof_capture",
    indices = [
        Index(value = ["idempotencyKey"], unique = true),
        Index(value = ["taskId", "fieldKey"]),
        Index(value = ["taskId", "subjectId", "capturedAtMs"]),
    ],
)
data class ProofCaptureEntityV10(
    @PrimaryKey val id: String,
    val taskId: String,
    val fieldKey: String,
    val proofSubject: String,
    val subjectId: String? = null,
    val localUri: String,
    val mimeType: String,
    val caption: String? = null,
    val capturedAtMs: Long,
    val capturedStartMs: Long,
    val capturedEndMs: Long,
    val capturedByPrincipalId: String? = null,
    val syncStatus: String,
    val idempotencyKey: String,
    val outboxItemId: String? = null,
    val serverProofId: String? = null,
    val lastError: String? = null,
)
