package sg.mesha.goatos.core.data.weighing

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
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
import sg.mesha.goatos.core.data.sync.DefaultSyncRepository
import sg.mesha.goatos.core.data.sync.FakeOutboxStore
import sg.mesha.goatos.core.data.sync.SyncEngine
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.WeighingSopOptionDto
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto

/**
 * THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): the repository queues a
 * per-animal weigh only once every compulsory slot and required answer of the task's PINNED rules
 * is in, sends the slot map primary-first, and keeps the seeded shape's request byte-identical.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class WeighingSlotProofDispatchTest {
    private lateinit var db: GoatDatabase
    private lateinit var appScope: CoroutineScope
    private val scopeKey = weighingScopeKey("campaign-1", "group-1", "campaign-shed-1")

    private val authored = WeighingSopRules.Seeded.copy(
        version = 3,
        individualProofs = listOf(
            WeighingRemovalProofSlot("animal_video", "Weighing video", "", "video", true),
            WeighingRemovalProofSlot("scale_photo", "Scale display", "", "photo", true),
        ),
        individualQuestions = listOf(
            WeighingSopQuestionDto(id = "limp", kind = "choice", title = "Limping?", required = true, options = listOf(WeighingSopOptionDto("yes", "Yes"), WeighingSopOptionDto("no", "No"))),
        ),
        lumpSumSlots = listOf(
            WeighingCountedProofSlot("pen_video", "Weighing video", "", "video", 1, 3),
            WeighingCountedProofSlot("scale_photo", "Scale display photo", "", "photo", 1, 1),
        ),
    )

    @Before
    fun setUp() {
        appScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
    }

    @After
    fun tearDown() {
        appScope.cancel()
        db.close()
    }

    private fun repository(store: FakeOutboxStore, rules: WeighingSopRules?, idPrefix: String = "local"): DefaultWeighingRepository {
        var n = 0
        return DefaultWeighingRepository(
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            syncRepository = DefaultSyncRepository(
                store = store,
                engine = SyncEngine(store, FakeAppApi(), connectivityGate = { false }, clock = { 1000L }),
                connectivityGate = { false },
                appScope = appScope,
                clock = { 1000L },
            ),
            clock = { 1000L },
            idGenerator = { "$idPrefix-${++n}" },
            captureRules = { rules },
        )
    }

    private fun capture(tag: String) = IndividualWeighingCapture(
        tenantId = "tenant", campaignId = "campaign-1", workGroupId = "group-1", campaignShedId = "campaign-shed-1",
        scannedIdentifier = tag, weightKg = 20.5,
    )

    @Test
    fun `an authored animal is not queued until every compulsory slot and required answer is in`() = runTest {
        val store = FakeOutboxStore()
        val repo = repository(store, authored)
        repo.recordIndividual(capture("TAG-1"))
        repo.attachIndividualProof(scopeKey, "TAG-1", "cap-video", "srv-video")
        assertEquals("video alone: the photo and the answer are still owed", 0, store.observeActiveWindow(100).first().size)
        assertFalse(repo.observeScope(scopeKey, 20).first().individualDrafts.single().readyToSubmit)

        repo.setIndividualSlotProof(scopeKey, "TAG-1", "scale_photo", "srv-photo")
        assertEquals("the answer is still owed", 0, store.observeActiveWindow(100).first().size)

        repo.setIndividualAnswers(scopeKey, "TAG-1", buildJsonObject { put("limp", JsonPrimitive("no")) })
        val queued = store.observeActiveWindow(100).first()
        assertEquals(1, queued.size)
        val payload = queued.single().payloadJson
        assertTrue(payload, payload.contains("\"proof_artifact_id\":\"srv-video\""))
        assertTrue(payload, payload.contains("\"proofs\":{\"animal_video\":\"srv-video\",\"scale_photo\":\"srv-photo\"}"))
        assertTrue(payload, payload.contains("\"answers\":{\"limp\":\"no\"}"))
        assertTrue(queued.single().idempotencyKey, queued.single().idempotencyKey.contains(":slots:"))
        val draft = repo.observeScope(scopeKey, 20).first().individualDrafts.single()
        assertTrue(draft.readyToSubmit)
        assertEquals(mapOf("scale_photo" to "srv-photo"), draft.slotProofs)
        assertEquals("no", (draft.answers["limp"] as JsonPrimitive).content)
    }

    @Test
    fun `changing only the secondary capture queues a new request under a new key`() = runTest {
        val store = FakeOutboxStore()
        val repo = repository(store, authored)
        repo.recordIndividual(capture("TAG-1"))
        repo.setIndividualAnswers(scopeKey, "TAG-1", buildJsonObject { put("limp", JsonPrimitive("no")) })
        repo.setIndividualSlotProof(scopeKey, "TAG-1", "scale_photo", "srv-photo")
        repo.attachIndividualProof(scopeKey, "TAG-1", "cap-video", "srv-video")
        val firstKey = store.observeActiveWindow(100).first().single().idempotencyKey
        repo.setIndividualSlotProof(scopeKey, "TAG-1", "scale_photo", "srv-photo-2")
        val keys = store.observeActiveWindow(100).first().map { it.idempotencyKey }
        assertTrue("a new key for the new evidence: $keys", keys.any { it != firstKey })
        // A SECOND secondary-only change (same weight, same primary video) is a new request too:
        // the key must carry the evidence, not merely the primary proof id.
        repo.setIndividualSlotProof(scopeKey, "TAG-1", "scale_photo", "srv-photo-3")
        val keysAfter = store.observeActiveWindow(100).first().map { it.idempotencyKey }
        assertTrue("a third capture must not reuse an earlier key: $keys -> $keysAfter", keysAfter.any { it !in keys })
        // Re-delivering the SAME secondary capture is not a new request.
        val before = store.observeActiveWindow(100).first().size
        repo.setIndividualSlotProof(scopeKey, "TAG-1", "scale_photo", "srv-photo-2")
        assertEquals(before, store.observeActiveWindow(100).first().size)
    }

    @Test
    fun `the seeded shape keeps the legacy request and key`() = runTest {
        val store = FakeOutboxStore()
        val repo = repository(store, WeighingSopRules.Seeded)
        val draft = repo.recordIndividual(capture("TAG-1")) as AppResult.Ok
        repo.attachIndividualProof(scopeKey, "TAG-1", "cap-video", "srv-video")
        val queued = store.observeActiveWindow(100).first().single()
        assertEquals(draft.value.idempotencyKey, queued.idempotencyKey)
        assertFalse(queued.payloadJson, queued.payloadJson.contains("\"proofs\""))
        assertFalse(queued.payloadJson, queued.payloadJson.contains("\"answers\""))
        // Unknown rules (no cache yet) behave as the seed: never block an older task.
        val store2 = FakeOutboxStore()
        val repo2 = repository(store2, null, idPrefix = "other")
        repo2.recordIndividual(capture("TAG-2"))
        repo2.attachIndividualProof(scopeKey, "TAG-2", "cap-2", "srv-2")
        assertEquals(1, store2.observeActiveWindow(100).first().size)
    }

    @Test
    fun `a whole-pen submit sends its slot map and flat list in slot order`() = runTest {
        val store = FakeOutboxStore()
        val repo = repository(store, authored)
        val draft = repo.recordShedPartition(
            ShedPartitionWeighingCapture(
                tenantId = "tenant", campaignId = "campaign-1", workGroupId = "group-1", campaignShedId = "campaign-shed-1",
                expectedLocationId = "shed-1", expectedLocationLabel = "Gandhi 1",
                resultJson = """{"total_weight_kg": 180.5}""",
                proofSlots = mapOf("scale_photo" to listOf("srv-p"), "pen_video" to listOf("srv-a", "srv-b")),
                answers = JsonObject(emptyMap()),
            ),
        ) as AppResult.Ok
        val queued = store.findByIdempotencyKey(draft.value.idempotencyKey)
        val payload = queued?.payloadJson.orEmpty()
        assertTrue(payload, payload.contains("\"proof_artifact_ids\":[\"srv-a\",\"srv-b\",\"srv-p\"]"))
        assertTrue(payload, payload.contains("\"proof_artifact_id\":\"srv-a\""))
        assertTrue(payload, payload.contains("\"proofs\":{\"pen_video\":[\"srv-a\",\"srv-b\"],\"scale_photo\":[\"srv-p\"]}"))
    }
}
