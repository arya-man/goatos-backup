package sg.mesha.goatos.core.data

import androidx.room.Database
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
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheEntity
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.ShedCompletionSummaryCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity

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
                ShedCompletionSummaryCacheEntity(cacheKey = "task-1", dtoJson = "{}", updatedAt = 13L),
            )
            val summaryRow = summaryDao.observe("task-1").first()
            assertEquals(13L, summaryRow?.updatedAt)

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
            val proofRow = proofDao.findById("proof-1")
            assertEquals("external_upload", proofRow?.captureSource)
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
