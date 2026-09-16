package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.CountsApprovalSubmitResponseDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto

/**
 * SOP capture card on Add birth / Add death (maintainer decisions 4 and 7, 2026-09-16): the outbox
 * row carries the card version, `{slot key: source}` and the answers; the dispatch resolves every
 * slot to its server proof id and sends `sop_capture`. A row without a capture (an older build,
 * or a phone that never loaded the card) sends NO `sop_capture`, so the server applies the
 * older-app rule instead of judging an empty submission strictly.
 */
class SyncEngineCountsCaptureDispatchTest {

    private val dispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    private class CaptureApi(delegate: AppApi = FakeAppApi()) : AppApi by delegate {
        val births = mutableListOf<CountsBirthEventRequestDto>()
        val deaths = mutableListOf<CountsDeathEventRequestDto>()
        override suspend fun recordCountsBirthEvent(idempotencyKey: String, request: CountsBirthEventRequestDto): CountsApprovalSubmitResponseDto {
            births += request
            return CountsApprovalSubmitResponseDto(approvalRequestId = "b-1", requestType = "birth", status = "pending", raisedAt = "2026-09-16T09:00:00Z", idempotentReplay = false)
        }
        override suspend fun recordCountsDeathEvent(idempotencyKey: String, request: CountsDeathEventRequestDto): CountsApprovalSubmitResponseDto {
            deaths += request
            return CountsApprovalSubmitResponseDto(approvalRequestId = "d-1", requestType = "death", status = "pending", raisedAt = "2026-09-16T09:00:00Z", idempotentReplay = false)
        }
    }

    private fun repository(api: AppApi): DefaultSyncRepository {
        val store = FakeOutboxStore()
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true }, dispatchers = dispatchers, clock = { 0L })
        return DefaultSyncRepository(store = store, engine = engine, connectivityGate = { true }, appScope = CoroutineScope(Dispatchers.Unconfined), dispatchers = dispatchers, clock = { 0L })
    }

    private val birth = CountsBirthEventRequestDto(species = "goat", sex = "female", dob = "2026-09-16", entryDate = "2026-09-16", damId = "RFID-M", litterSize = 1)

    @Test
    fun `a birth with a capture sends sop_capture with resolved slot proofs and answers`() = runBlocking {
        val api = CaptureApi()
        val repo = repository(api)
        repo.enqueueCountsBirth(
            groupKey = "RFID-M",
            idempotencyKey = "counts-birth-add:1",
            request = birth,
            capture = CountsCapturePayload(
                sopVersionId = "v-id",
                slotProofs = mapOf("newborns_with_mother" to FeedSlotProofSourcePayload(proofRef = "proof-1")),
                answers = JsonObject(mapOf("delivery_type" to JsonPrimitive("assisted"))),
            ),
        )
        val sent = api.births.single().sopCapture
        assertEquals("v-id", sent?.sopVersionId)
        assertEquals(mapOf("newborns_with_mother" to "proof-1"), sent?.proofs)
        assertEquals(JsonPrimitive("assisted"), sent?.answers?.get("delivery_type"))
    }

    @Test
    fun `a death without a capture sends no sop_capture`() = runBlocking {
        val api = CaptureApi()
        val repo = repository(api)
        repo.enqueueCountsDeath("goat-1", "counts-death-add:1", CountsDeathEventRequestDto(goatId = "goat-1", reason = "found dead", rowVersion = 2))
        assertNull(api.deaths.single().sopCapture)
    }

    @Test
    fun `a queued row written before the capture card still decodes`() {
        val legacy = """{"request":{"goat_id":"goat-1","lifecycle_status":"dead","exit_reason":"died","reason":"x","row_version":1}}"""
        val decoded = syncJson.decodeFromString<CountsDeathPayload>(legacy)
        assertNull(decoded.capture)
    }
}
