package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingExecutionResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto

/**
 * SHIFTING SOP (2026-09-16): the outbox carries the authored card's captures and answers end to end.
 * A completion names every slot by its PROOF_UPLOAD row; the dispatcher resolves each to its server
 * proof id and sends `proofs` + `answers` beside the legacy `proof_ref` mirror. A raise's optional
 * captures resolve the same way into the raise request's `proofs`. A pre-SOP queued row (no slot
 * map) still sends the legacy triple and no `proofs`.
 */
class SyncEngineShiftingSopDispatchTest {

    private val unconfined = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }
    private val appScope = CoroutineScope(Dispatchers.Unconfined + Job())

    @After
    fun tearDown() = appScope.cancel()

    private class RecordingApi(delegate: AppApi = FakeAppApi()) : AppApi by delegate {
        var proofRef: String? = null
        var packingRef: String? = null
        var proofs: Map<String, String>? = null
        var answers: JsonObject? = null
        var raise: CountsShiftingEventRequestDto? = null

        override suspend fun completeCountsShiftingEvent(
            shiftingEventId: String,
            idempotencyKey: String,
            destinationTag: String?,
            proofRef: String,
            feedPackingProofRef: String?,
            feedGivenProofRef: String?,
            feedConfigFingerprint: String?,
            proofs: Map<String, String>?,
            answers: JsonObject?,
        ): CountsShiftingExecutionResponseDto {
            this.proofRef = proofRef
            this.packingRef = feedPackingProofRef
            this.proofs = proofs
            this.answers = answers
            return CountsShiftingExecutionResponseDto(shiftingEventId = shiftingEventId, eventStatus = "applied")
        }

        override suspend fun recordCountsShiftingEvent(idempotencyKey: String, request: CountsShiftingEventRequestDto): CountsShiftingEventResponseDto {
            raise = request
            return CountsShiftingEventResponseDto(shiftingEventId = "move-new")
        }
    }

    private fun repository(api: AppApi): DefaultSyncRepository {
        val store = FakeOutboxStore()
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true }, dispatchers = unconfined, clock = { 0L })
        return DefaultSyncRepository(store = store, engine = engine, connectivityGate = { true }, appScope = appScope, dispatchers = unconfined, clock = { 0L })
    }

    private suspend fun DefaultSyncRepository.syncedProof(group: String, step: String): String {
        val result = enqueueProofUpload(
            groupKey = group,
            idempotencyKey = "counts-shifting-proof:$group:$step",
            request = ProofUploadRequestDto(proofType = "video", mimeType = "video/mp4", scopeType = "shed", scopeId = "shed-1", subjectType = "shed", subjectId = "shed-1"),
            localFilePath = "/tmp/$group-$step.mp4",
            durationMs = 1000,
        )
        return (result as AppResult.Ok).value
    }

    @Test
    fun `completion sends the slot proofs map answers and the legacy mirror`() = runBlocking {
        val api = RecordingApi()
        val repo = repository(api)
        val walk = repo.syncedProof("move-1", "walk")
        val clip = repo.syncedProof("move-1", "clip")
        val answers = JsonObject(mapOf("calm" to JsonPrimitive("yes")))
        val enqueued = repo.enqueueShiftingComplete(
            groupKey = "move-1",
            idempotencyKey = "counts-shifting-complete:move-1",
            proofOutboxItemId = walk,
            feedConfigFingerprint = "fp",
            slotProofs = linkedMapOf(
                "shifting_shifting_video" to FeedSlotProofSourcePayload(outboxItemId = walk),
                "feed_clip" to FeedSlotProofSourcePayload(outboxItemId = clip),
            ),
            answers = answers,
        )
        assertTrue(enqueued is AppResult.Ok)
        assertEquals(
            mapOf(
                "shifting_shifting_video" to "fake-proof-counts-shifting-proof:move-1:walk",
                "feed_clip" to "fake-proof-counts-shifting-proof:move-1:clip",
            ),
            api.proofs,
        )
        assertEquals(answers, api.answers)
        assertEquals("fake-proof-counts-shifting-proof:move-1:walk", api.proofRef)
        // An authored card's feed capture is a slot, not the legacy packing column.
        assertNull(api.packingRef)
    }

    @Test
    fun `a pre-SOP queued completion still sends only the legacy triple`() = runBlocking {
        val api = RecordingApi()
        val repo = repository(api)
        val walk = repo.syncedProof("move-2", "walk")
        repo.enqueueShiftingComplete(groupKey = "move-2", idempotencyKey = "counts-shifting-complete:move-2", proofOutboxItemId = walk)
        assertEquals("fake-proof-counts-shifting-proof:move-2:walk", api.proofRef)
        assertNull(api.proofs)
        assertNull(api.answers)
    }

    @Test
    fun `a raise resolves its slot proofs into the request`() = runBlocking {
        val api = RecordingApi()
        val repo = repository(api)
        val photo = repo.syncedProof("shed-9", "pen")
        val request = CountsShiftingEventRequestDto(
            destinationParkId = "park-1",
            destinationShedId = "shed-9",
            goatIds = listOf("goat-1"),
            sopVersion = 3,
            answers = JsonObject(mapOf("why" to JsonPrimitive("Overcrowded"))),
        )
        val enqueued = repo.enqueueCountsShifting(
            groupKey = "shed-9",
            idempotencyKey = "counts-shifting:draft-1",
            request = request,
            slotProofs = mapOf("pen_photo" to FeedSlotProofSourcePayload(outboxItemId = photo)),
        )
        assertTrue(enqueued is AppResult.Ok)
        val sent = api.raise!!
        assertEquals(mapOf("pen_photo" to "fake-proof-counts-shifting-proof:shed-9:pen"), sent.proofs)
        assertEquals(3, sent.sopVersion)
        assertEquals(request.answers, sent.answers)
    }
}
