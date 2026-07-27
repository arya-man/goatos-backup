package sg.mesha.goatos.core.data.weighing

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.WeighingRosterResponseDto
import sg.mesha.goatos.core.network.dto.WeighingRosterRowDto

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class WeighingRepositoryTest {
    private lateinit var db: GoatDatabase
    private lateinit var repository: WeighingRepository
    private val scopeKey = weighingScopeKey("campaign-1", "group-1", "campaign-shed-1")

    @Before
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        db = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        repository = DefaultWeighingRepository(
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            clock = { 1000L },
            idGenerator = stableIds().iterator()::next,
        )
    }

    @After
    fun tearDown() {
        db.close()
    }

    @Test
    fun `off-window RFID lookup updates state without loading all animals`() = runTest {
        repository.replaceRoster(scopeKey, (1..5_001).map { index ->
            rosterRow(
                animalId = "animal-$index",
                tag = "TAG-$index",
                seq = index.toLong(),
            )
        })

        val initial = repository.observeScope(scopeKey, windowSize = 20).first()
        assertEquals(20, initial.rosterWindow.size)
        assertEquals(5_001, initial.totalExpected)

        val match = repository.matchTag(scopeKey, "TAG-5001")
        assertEquals("animal-5001", match.row?.animalId)

        val recorded = repository.recordIndividual(
            IndividualWeighingCapture(
                tenantId = "tenant",
                campaignId = "campaign-1",
                workGroupId = "group-1",
                campaignShedId = "campaign-shed-1",
                animalId = "animal-5001",
                scannedIdentifier = "TAG-5001",
                weightKg = 12.4,
            ),
        )
        assertTrue(recorded is AppResult.Ok)

        val after = repository.observeScope(scopeKey, windowSize = 20).first()
        assertEquals(20, after.rosterWindow.size)
        assertEquals(listOf("animal-5001"), after.individualDrafts.map { it.animalId })
    }

    @Test
    fun `refresh scope hydrates Room roster from backend rows`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingRoster(
                campaignId: String,
                campaignShedId: String,
                limit: Int,
            ): WeighingRosterResponseDto = WeighingRosterResponseDto(
                items = listOf(
                    WeighingRosterRowDto(
                        campaignId = campaignId,
                        campaignShedId = campaignShedId,
                        animalId = "animal-9",
                        displayAnimalId = "KID-A-009",
                        primaryIdentifier = "WG-RFID-0009",
                        expectedLocationId = "shed-a",
                        expectedLocationLabel = "Kid Shed A",
                        currentLocationId = "shed-b",
                        currentLocationLabel = "Kid Shed B",
                        status = "pending",
                        availabilityStatus = "moved_other_shed",
                        seq = 9,
                    ),
                ),
            )
        }
        repository = DefaultWeighingRepository(
            api = api,
            tenantId = "tenant-live",
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val refreshed = repository.refreshScope("campaign-1", "group-1", "campaign-shed-1")
        assertEquals(1, (refreshed as AppResult.Ok).value)

        val match = repository.matchTag(scopeKey, "WG-RFID-0009")
        assertEquals("animal-9", match.row?.animalId)
        assertEquals("tenant-live", match.row?.tenantId)
        assertEquals("wrong_shed", match.outcome)
    }

    @Test
    fun `wrong shed scan preserves expected and actual location labels`() = runTest {
        repository.replaceRoster(
            scopeKey,
            listOf(
                rosterRow(
                    animalId = "animal-1",
                    tag = "TAG-1",
                    expectedLocationId = "shed-expected",
                    expectedLocationLabel = "Gandhi 1",
                    actualLocationId = "shed-actual",
                    actualLocationLabel = "Mandela 2",
                ),
            ),
        )

        val match = repository.matchTag(scopeKey, "tag-1")

        assertEquals("wrong_shed", match.outcome)
        assertEquals("Gandhi 1", match.expectedLocationLabel)
        assertEquals("Mandela 2", match.actualLocationLabel)
    }

    @Test
    fun `individual animal observation is idempotent and proof gated`() = runTest {
        repository.replaceRoster(scopeKey, listOf(rosterRow(animalId = "animal-1", tag = "TAG-1")))

        val first = repository.recordIndividual(individualCapture("animal-1", "TAG-1", weightKg = 10.2))
        val replay = repository.recordIndividual(individualCapture("animal-1", "TAG-1", weightKg = 10.2))
        assertTrue(first is AppResult.Ok)
        assertTrue(replay is AppResult.Ok)
        assertEquals(
            (first as AppResult.Ok).value.observationId,
            (replay as AppResult.Ok).value.observationId,
        )
        assertFalse(first.value.readyToSubmit)

        repository.attachIndividualProof(scopeKey, "animal-1", proofCaptureId = "proof-local-1", serverProofId = "proof-server-1")
        val state = repository.observeScope(scopeKey, windowSize = 20).first()

        assertEquals(1, state.individualDrafts.size)
        assertTrue(state.individualDrafts.single().readyToSubmit)
        assertEquals(
            "weighing:individual:campaign-1:group-1:campaign-shed-1:animal-1",
            state.individualDrafts.single().idempotencyKey,
        )
    }

    @Test
    fun `per shed partition observation does not create individual animal drafts`() = runTest {
        repository.replaceRoster(scopeKey, listOf(rosterRow(animalId = "animal-1", tag = "TAG-1")))

        val result = repository.recordShedPartition(
            ShedPartitionWeighingCapture(
                tenantId = "tenant",
                campaignId = "campaign-1",
                workGroupId = "group-1",
                campaignShedId = "campaign-shed-1",
                expectedLocationId = "shed-1",
                expectedLocationLabel = "Gandhi 1",
                resultJson = """{"weight": 180.5, "unit": "kg"}""",
            ),
        )
        assertTrue(result is AppResult.Ok)
        repository.attachShedPartitionProof(scopeKey, proofCaptureId = "shed-proof-local", serverProofId = "shed-proof-server")

        val state = repository.observeScope(scopeKey, windowSize = 20).first()
        assertEquals(emptyList<IndividualWeighingDraft>(), state.individualDrafts)
        assertEquals(1, state.shedDrafts.size)
        assertTrue(state.shedDrafts.single().readyToSubmit)
        assertEquals("weighing:shed:campaign-1:group-1:campaign-shed-1", state.shedDrafts.single().idempotencyKey)
    }

    private fun individualCapture(animalId: String, tag: String, weightKg: Double) =
        IndividualWeighingCapture(
            tenantId = "tenant",
            campaignId = "campaign-1",
            workGroupId = "group-1",
            campaignShedId = "campaign-shed-1",
            animalId = animalId,
            scannedIdentifier = tag,
            weightKg = weightKg,
        )

    private fun rosterRow(
        animalId: String,
        tag: String,
        seq: Long = 1L,
        expectedLocationId: String = "shed-1",
        expectedLocationLabel: String = "Gandhi 1",
        actualLocationId: String? = expectedLocationId,
        actualLocationLabel: String? = expectedLocationLabel,
    ) = WeighingRosterRowEntity(
        id = "row-$animalId",
        scopeKey = scopeKey,
        tenantId = "tenant",
        campaignId = "campaign-1",
        workGroupId = "group-1",
        campaignShedId = "campaign-shed-1",
        expectedLocationId = expectedLocationId,
        expectedLocationLabel = expectedLocationLabel,
        actualLocationId = actualLocationId,
        actualLocationLabel = actualLocationLabel,
        animalId = animalId,
        displayAnimalId = animalId,
        primaryTag = tag,
        secondaryTag = null,
        normalizedPrimaryTag = normalizeWeighingTag(tag),
        normalizedSecondaryTag = null,
        status = "pending",
        availabilityStatus = null,
        seq = seq,
        updatedAt = 1L,
    )

    private fun stableIds(): Sequence<String> = sequence {
        var index = 0
        while (true) yield("local-${++index}")
    }
}
