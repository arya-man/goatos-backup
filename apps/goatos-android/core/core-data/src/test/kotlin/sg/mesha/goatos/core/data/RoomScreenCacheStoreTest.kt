package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.AdherenceCacheEntity
import sg.mesha.goatos.core.data.cache.CalendarCacheEntity
import sg.mesha.goatos.core.data.cache.ControlTowerCacheEntity
import sg.mesha.goatos.core.data.cache.WeighingAlertsCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheEntity
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheEntity
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingObservationEntity
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingShedObservationEntity
import sg.mesha.goatos.core.data.weighing.normalizeWeighingTag

/**
 * Proof for C35-001 that the production [ScreenCacheStore] wiring ([RoomScreenCacheStore]) does
 * not silently miss a table: seeds a row into EVERY one of the GoatDatabase cache tables
 * (the exact set [LogoutCoordinatorTest] only proves was *invoked* through a fake), then asserts
 * every one is empty after [ScreenCacheStore.clearAll]. A real (Robolectric in-memory) database
 * is required here — [androidx.room.RoomDatabase.clearAllTables] only proves something against
 * real SQLite, not against a fake.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class RoomScreenCacheStoreTest {

    @Test
    fun `clearAll empties every cache table, including bootstrap and roster tables`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val db = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()

        try {
            val key = "k"
            db.bootstrapCacheDao().upsert(BootstrapCacheEntity(dtoJson = "{}", updatedAt = 1L))
            db.calendarCacheDao().upsert(CalendarCacheEntity(cacheKey = key, dtoJson = "{}", updatedAt = 1L))
            db.controlTowerCacheDao().upsert(ControlTowerCacheEntity(cacheKey = key, dtoJson = "{}", updatedAt = 1L))
            db.weighingAlertsCacheDao().upsert(WeighingAlertsCacheEntity(cacheKey = key, dtoJson = "{}", updatedAt = 1L))
            db.executionRowsCacheDao().upsert(ExecutionRowsCacheEntity(cacheKey = key, dtoJson = "{}", updatedAt = 1L))
            db.executionShedCacheDao().upsert(ExecutionShedCacheEntity(cacheKey = key, dtoJson = "{}", updatedAt = 1L))
            db.scanRosterRowDao().upsert(
                ScanRosterRowEntity(
                    id = "$key#obl", scopeKey = key, shedId = "shed", taskId = "task", goatId = "goat",
                    primaryTag = "TAG", secondaryTag = null, normalizedPrimaryTag = "tag",
                    normalizedSecondaryTag = null, vaccineLabel = "ET", status = "pending",
                    obligationId = "obl", seq = 0, updatedAt = 1L,
                ),
            )
            db.adherenceCacheDao().upsert(AdherenceCacheEntity(cacheKey = key, dtoJson = "{}", updatedAt = 1L))
            db.insightsGapsCacheDao().upsert(InsightsGapsCacheEntity(cacheKey = key, dtoJson = "{}", updatedAt = 1L))
            db.insightsCoverageCacheDao().upsert(InsightsCoverageCacheEntity(cacheKey = key, dtoJson = "{}", updatedAt = 1L))
            db.rosterTimetableCacheDao().upsert(RosterTimetableCacheEntity(cacheKey = key, dtoJson = "{}", updatedAt = 1L))
            db.rosterCoverageCacheDao().upsert(RosterCoverageCacheEntity(cacheKey = key, dtoJson = "{}", updatedAt = 1L))
            db.weighingRosterDao().upsertAll(
                listOf(
                    WeighingRosterRowEntity(
                        id = "$key#weighing",
                        scopeKey = key,
                        tenantId = "tenant",
                        campaignId = "campaign",
                        workGroupId = "group",
                        campaignShedId = "campaign-shed",
                        expectedLocationId = "shed",
                        expectedLocationLabel = "Gandhi 1",
                        actualLocationId = "shed",
                        actualLocationLabel = "Gandhi 1",
                        animalId = "animal",
                        displayAnimalId = "animal",
                        primaryTag = "TAG",
                        secondaryTag = null,
                        normalizedPrimaryTag = normalizeWeighingTag("TAG"),
                        normalizedSecondaryTag = null,
                        status = "pending",
                        availabilityStatus = null,
                        seq = 1,
                        updatedAt = 1L,
                    ),
                ),
            )
            db.weighingObservationDao().insert(
                WeighingObservationEntity(
                    observationId = "weighing-observation",
                    scopeKey = key,
                    tenantId = "tenant",
                    campaignId = "campaign",
                    workGroupId = "group",
                    campaignShedId = "campaign-shed",
                    expectedLocationId = "shed",
                    expectedLocationLabel = "Gandhi 1",
                    actualLocationId = "shed",
                    actualLocationLabel = "Gandhi 1",
                    scannedIdentifier = "TAG",
                    weightKg = 12.5,
                    proofCaptureId = null,
                    serverProofId = null,
                    syncStatus = "PENDING_LOCAL",
                    idempotencyKey = "weighing:individual",
                    capturedAtMs = 1L,
                    lastError = null,
                ),
            )
            db.weighingShedObservationDao().insert(
                WeighingShedObservationEntity(
                    shedObservationId = "weighing-shed-observation",
                    scopeKey = key,
                    tenantId = "tenant",
                    campaignId = "campaign",
                    workGroupId = "group",
                    campaignShedId = "campaign-shed-2",
                    expectedLocationId = "shed-2",
                    expectedLocationLabel = "Castro 1",
                    resultJson = """{"total_weight_kg":1560.5}""",
                    proofCaptureId = null,
                    serverProofId = null,
                    syncStatus = "PENDING_LOCAL",
                    idempotencyKey = "weighing:shed",
                    capturedAtMs = 1L,
                    lastError = null,
                ),
            )

            RoomScreenCacheStore(db).clearAll()

            assertNull("bootstrap cache wiped", db.bootstrapCacheDao().get())
            assertNull("calendar cache wiped", db.calendarCacheDao().observe(key).first())
            assertNull("control tower cache wiped", db.controlTowerCacheDao().observe(key).first())
            // Sign-out MUST wipe weighing alerts too: on a shared shed phone the next operator
            // signing in would otherwise open the tab on the previous person's work messages.
            assertNull("weighing alerts cache wiped", db.weighingAlertsCacheDao().observe(key).first())
            assertNull("execution rows cache wiped", db.executionRowsCacheDao().observe(key).first())
            assertNull("execution shed cache wiped", db.executionShedCacheDao().observe(key).first())
            assertEquals("scan roster rows wiped", 0, db.scanRosterRowDao().observeScopeTotal(key).first())
            assertNull("adherence cache wiped", db.adherenceCacheDao().observe(key).first())
            assertNull("insights gaps cache wiped", db.insightsGapsCacheDao().observe(key).first())
            assertNull("insights coverage cache wiped", db.insightsCoverageCacheDao().observe(key).first())
            assertNull("roster timetable cache wiped", db.rosterTimetableCacheDao().observe(key).first())
            assertNull("roster coverage cache wiped", db.rosterCoverageCacheDao().observe(key).first())
            assertEquals("weighing roster rows wiped", 0, db.weighingRosterDao().observeScopeTotal(key).first())
            assertEquals("weighing individual drafts wiped", 0, db.weighingObservationDao().observeForScope(key).first().size)
            assertEquals("weighing shed drafts wiped", 0, db.weighingShedObservationDao().observeForScope(key).first().size)
        } finally {
            db.close()
        }
    }
}
